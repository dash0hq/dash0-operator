// SPDX-FileCopyrightText: Copyright 2024 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package otelcolresources

import (
	"context"
	"fmt"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"

	dash0v1alpha1 "github.com/dash0hq/dash0-operator/api/dash0monitoring/v1alpha1"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	. "github.com/dash0hq/dash0-operator/test/util"
)

var (
	testResource = &corev1.ConfigMap{
		TypeMeta: metav1.TypeMeta{
			Kind:       "ConfigMap",
			APIVersion: "v1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-config-map",
			Namespace: OperatorNamespace,
			Labels: map[string]string{
				"label": "value",
			},
		},
		Data: map[string]string{
			"key": "value",
		},
	}
)

var _ = Describe("The OpenTelemetry Collector resource manager", Ordered, func() {
	ctx := context.Background()
	logger := log.FromContext(ctx)

	var oTelColResourceManager *OTelColResourceManager

	BeforeAll(func() {
		EnsureOperatorNamespaceExists(ctx, k8sClient)
		EnsureTestNamespaceExists(ctx, k8sClient)
	})

	BeforeEach(func() {
		oTelColResourceManager = &OTelColResourceManager{
			Client:                  k8sClient,
			Scheme:                  k8sClient.Scheme(),
			DeploymentSelfReference: DeploymentSelfReference,
			OTelCollectorNamePrefix: OTelCollectorNamePrefixTest,
			OTelColExtraConfig:      &OTelExtraConfigDefaults,
			DevelopmentMode:         true,
		}
	})

	AfterEach(func() {
		_, err := oTelColResourceManager.DeleteResources(
			ctx,
			OperatorNamespace,
			&logger,
		)
		Expect(err).ToNot(HaveOccurred())
		Eventually(func(g Gomega) {
			VerifyCollectorResourcesDoNotExist(ctx, k8sClient, OperatorNamespace)
		}, 500*time.Millisecond, 20*time.Millisecond).Should(Succeed())
		Expect(k8sClient.DeleteAllOf(ctx, &corev1.ConfigMap{}, client.InNamespace(OperatorNamespace))).To(Succeed())
	})

	Describe("when dealing with individual resources", func() {
		It("should create a single resource", func() {
			isNew, isChanged, err := oTelColResourceManager.createOrUpdateResource(ctx, testResource.DeepCopy(), &logger)
			Expect(err).ToNot(HaveOccurred())
			Expect(isNew).To(BeTrue())
			Expect(isChanged).To(BeFalse())
			verifyObject(ctx, testResource)
		})

		It("should update a single object", func() {
			err := oTelColResourceManager.createResource(ctx, testResource.DeepCopy(), &logger)
			Expect(err).ToNot(HaveOccurred())

			updated := testResource.DeepCopy()
			updated.Data["key"] = "updated value"
			isNew, isChanged, err := oTelColResourceManager.createOrUpdateResource(ctx, updated, &logger)

			Expect(err).ToNot(HaveOccurred())
			Expect(isNew).To(BeFalse())
			Expect(isChanged).To(BeTrue())
			verifyObject(ctx, updated)
		})

		It("should report that nothing has changed for a single object", func() {
			err := oTelColResourceManager.createResource(ctx, testResource.DeepCopy(), &logger)
			Expect(err).ToNot(HaveOccurred())

			isNew, isChanged, err := oTelColResourceManager.createOrUpdateResource(
				ctx,
				testResource.DeepCopy(),
				&logger,
			)

			Expect(err).ToNot(HaveOccurred())
			Expect(isNew).To(BeFalse())
			Expect(isChanged).To(BeFalse())
			verifyObject(ctx, testResource)
		})
	})

	Describe("when creating all OpenTelemetry collector resources", func() {
		AfterEach(func() {
			DeleteAllOperatorConfigurationResources(ctx, k8sClient)
		})

		It("should reject calls without export settings", func() {
			_, _, err :=
				oTelColResourceManager.CreateOrUpdateOpenTelemetryCollectorResources(
					ctx,
					OperatorNamespace,
					TestImages,
					nil,
					nil,
					nil,
					&logger,
				)
			Expect(err).To(MatchError("cannot create or update Dash0 OpenTelemetry collectors without export settings"))
		})

		It("should use the provided export settings to create the collectors", func() {
			resourcesHaveBeenCreated, resourcesHaveBeenUpdated, err :=
				oTelColResourceManager.CreateOrUpdateOpenTelemetryCollectorResources(
					ctx,
					OperatorNamespace,
					TestImages,
					nil,
					nil,
					ptr.To(Dash0ExportWithEndpointAndToken()),
					&logger,
				)
			Expect(err).ToNot(HaveOccurred())
			Expect(resourcesHaveBeenCreated).To(BeTrue())
			Expect(resourcesHaveBeenUpdated).To(BeFalse())
			VerifyCollectorResources(ctx, k8sClient, OperatorNamespace, EndpointDash0Test, AuthorizationTokenTest)
		})

		It("should not add self-monitoring if there is no operator configuration resource", func() {
			resourcesHaveBeenCreated, resourcesHaveBeenUpdated, err :=
				oTelColResourceManager.CreateOrUpdateOpenTelemetryCollectorResources(
					ctx,
					OperatorNamespace,
					TestImages,
					nil,
					nil,
					ptr.To(Dash0ExportWithEndpointAndToken()),
					&logger,
				)
			Expect(err).ToNot(HaveOccurred())
			Expect(resourcesHaveBeenCreated).To(BeTrue())
			Expect(resourcesHaveBeenUpdated).To(BeFalse())

			ds_ := VerifyResourceExists(
				ctx,
				k8sClient,
				OperatorNamespace,
				ExpectedDaemonSetName,
				&appsv1.DaemonSet{},
			)
			ds := ds_.(*appsv1.DaemonSet)

			containers := ds.Spec.Template.Spec.Containers
			Expect(containers).To(HaveLen(3))
			for _, container := range containers {
				Expect(container.Env).NotTo(
					ContainElement(MatchEnvVar("SELF_MONITORING_AND_API_AUTH_TOKEN", AuthorizationTokenTest)))

			}
		})

		It("should not add self-monitoring if is is disabled", func() {
			operatorConfiguration := &dash0v1alpha1.Dash0OperatorConfiguration{
				ObjectMeta: OperatorConfigurationResourceDefaultObjectMeta,
				Spec:       OperatorConfigurationResourceWithoutSelfMonitoringWithToken,
			}
			resourcesHaveBeenCreated, resourcesHaveBeenUpdated, err :=
				oTelColResourceManager.CreateOrUpdateOpenTelemetryCollectorResources(
					ctx,
					OperatorNamespace,
					TestImages,
					operatorConfiguration,
					nil,
					ptr.To(Dash0ExportWithEndpointAndToken()),
					&logger,
				)
			Expect(err).ToNot(HaveOccurred())
			Expect(resourcesHaveBeenCreated).To(BeTrue())
			Expect(resourcesHaveBeenUpdated).To(BeFalse())

			ds_ := VerifyResourceExists(
				ctx,
				k8sClient,
				OperatorNamespace,
				ExpectedDaemonSetName,
				&appsv1.DaemonSet{},
			)
			ds := ds_.(*appsv1.DaemonSet)

			containers := ds.Spec.Template.Spec.Containers
			Expect(containers).To(HaveLen(3))
			for _, container := range containers {
				Expect(container.Env).ToNot(
					ContainElement(MatchEnvVar("SELF_MONITORING_AND_API_AUTH_TOKEN", AuthorizationTokenTest)))

			}
		})

		It("should respect self-monitoring settings when creating the collectors", func() {
			operatorConfiguration := DefaultOperatorConfigurationResource()
			resourcesHaveBeenCreated, resourcesHaveBeenUpdated, err :=
				oTelColResourceManager.CreateOrUpdateOpenTelemetryCollectorResources(
					ctx,
					OperatorNamespace,
					TestImages,
					operatorConfiguration,
					nil,
					ptr.To(Dash0ExportWithEndpointAndToken()),
					&logger,
				)
			Expect(err).ToNot(HaveOccurred())
			Expect(resourcesHaveBeenCreated).To(BeTrue())
			Expect(resourcesHaveBeenUpdated).To(BeFalse())

			ds_ := VerifyResourceExists(
				ctx,
				k8sClient,
				OperatorNamespace,
				ExpectedDaemonSetName,
				&appsv1.DaemonSet{},
			)
			ds := ds_.(*appsv1.DaemonSet)

			containers := ds.Spec.Template.Spec.Containers
			Expect(containers).To(HaveLen(3))
			for _, container := range containers {
				Expect(container.Env).To(
					ContainElement(MatchEnvVar("SELF_MONITORING_AND_API_AUTH_TOKEN", AuthorizationTokenTest)))

			}
		})

		It("should delete resources that are no longer desired", func() {
			// reconcile once with KubernetesInfrastructureMetricsCollectionEnabled = true
			operatorConfiguration := DefaultOperatorConfigurationResource()
			_, _, err :=
				oTelColResourceManager.CreateOrUpdateOpenTelemetryCollectorResources(
					ctx,
					OperatorNamespace,
					TestImages,
					operatorConfiguration,
					nil,
					operatorConfiguration.Spec.Export,
					&logger,
				)
			Expect(err).ToNot(HaveOccurred())

			// Verify all resources (in particular also the deployment resources for the cluster metrics) have been
			// created.
			for _, expected := range AllExpectedResources {
				VerifyExpectedResourceExists(
					ctx,
					k8sClient,
					OperatorNamespace,
					expected,
				)
			}

			// reconcile again with KubernetesInfrastructureMetricsCollectionEnabled = false
			operatorConfiguration.Spec.KubernetesInfrastructureMetricsCollectionEnabled = ptr.To(false)
			_, _, err =
				oTelColResourceManager.CreateOrUpdateOpenTelemetryCollectorResources(
					ctx,
					OperatorNamespace,
					TestImages,
					operatorConfiguration,
					nil,
					operatorConfiguration.Spec.Export,
					&logger,
				)
			Expect(err).ToNot(HaveOccurred())

			// Verify that the deployment resources for the cluster metrics have been deleted.
			for _, expected := range AllClusterMetricsRelatedResources {
				VerifyExpectedResourceDoesNotExist(
					ctx,
					k8sClient,
					OperatorNamespace,
					expected,
				)
			}
			// Verify that the daemonset resources still exist.
			for _, expected := range AllDaemonSetRelatedResources {
				VerifyExpectedResourceExists(
					ctx,
					k8sClient,
					OperatorNamespace,
					expected,
				)
			}
		})

		It("should delete outdated resources from older operator versions", func() {
			nameOfOutdatedResources := fmt.Sprintf("%s-opentelemetry-collector-agent", NamePrefix)
			Expect(k8sClient.Create(ctx, &corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Name:      nameOfOutdatedResources,
					Namespace: OperatorNamespace,
				},
			})).To(Succeed())
			Expect(k8sClient.Create(ctx, &appsv1.DaemonSet{
				ObjectMeta: metav1.ObjectMeta{
					Name:      nameOfOutdatedResources,
					Namespace: OperatorNamespace,
				},
				Spec: appsv1.DaemonSetSpec{
					Selector: &metav1.LabelSelector{
						MatchLabels: daemonSetMatchLabels,
					},
					Template: corev1.PodTemplateSpec{
						ObjectMeta: metav1.ObjectMeta{
							Labels: daemonSetMatchLabels,
						},
						Spec: corev1.PodSpec{
							Containers: []corev1.Container{
								{
									Name:  openTelemetryCollector,
									Image: CollectorImageTest,
								},
							},
						},
					},
				},
			})).To(Succeed())

			_, _, err :=
				oTelColResourceManager.CreateOrUpdateOpenTelemetryCollectorResources(
					ctx,
					OperatorNamespace,
					TestImages,
					nil,
					nil,
					ptr.To(Dash0ExportWithEndpointAndToken()),
					&logger,
				)
			Expect(err).ToNot(HaveOccurred())

			VerifyResourceDoesNotExist(
				ctx,
				k8sClient,
				OperatorNamespace,
				nameOfOutdatedResources,
				&corev1.ConfigMap{},
			)
			VerifyResourceDoesNotExist(
				ctx,
				k8sClient,
				OperatorNamespace,
				nameOfOutdatedResources,
				&appsv1.DaemonSet{},
			)
		})
	})

	Describe("when OpenTelemetry collector resources have been modified externally", func() {
		It("should reconcile the resources back into the desired state", func() {
			_, _, err :=
				oTelColResourceManager.CreateOrUpdateOpenTelemetryCollectorResources(
					ctx,
					OperatorNamespace,
					TestImages,
					nil,
					nil,
					ptr.To(Dash0ExportWithEndpointAndToken()),
					&logger,
				)
			Expect(err).ToNot(HaveOccurred())

			// Change some arbitrary fields in some resources, then simulate a reconcile cycle and verify that all
			// resources are back in their desired state.

			daemonSetConifgMap := GetOTelColDaemonSetConfigMap(ctx, k8sClient, OperatorNamespace)
			daemonSetConifgMap.Data["config.yaml"] = "{}"
			daemonSetConifgMap.Data["bogus-key"] = ""
			Expect(k8sClient.Update(ctx, daemonSetConifgMap)).To(Succeed())

			daemonSet := GetOTelColDaemonSet(ctx, k8sClient, OperatorNamespace)
			daemonSet.Spec.Template.Spec.InitContainers = []corev1.Container{}
			daemonSet.Spec.Template.Spec.Containers[0].Image = "wrong-collector-image:latest"
			daemonSet.Spec.Template.Spec.Containers[0].Ports = []corev1.ContainerPort{
				{ContainerPort: 1234},
				{ContainerPort: 1235},
			}
			Expect(k8sClient.Update(ctx, daemonSet)).To(Succeed())

			deploymentConfigMap := GetOTelColDeploymentConfigMap(ctx, k8sClient, OperatorNamespace)
			deploymentConfigMap.Data["config.yaml"] = "{}"
			deploymentConfigMap.Data["bogus-key"] = ""
			Expect(k8sClient.Update(ctx, deploymentConfigMap)).To(Succeed())

			deployment := GetOTelColDeployment(ctx, k8sClient, OperatorNamespace)
			var changedReplicas int32 = 5
			deployment.Spec.Replicas = &changedReplicas
			deployment.Spec.Template.Spec.Containers[0].Ports = []corev1.ContainerPort{
				{ContainerPort: 1234},
			}
			Expect(k8sClient.Update(ctx, deployment)).To(Succeed())

			resourcesHaveBeenCreated, resourcesHaveBeenUpdated, err :=
				oTelColResourceManager.CreateOrUpdateOpenTelemetryCollectorResources(
					ctx,
					OperatorNamespace,
					TestImages,
					nil,
					nil,
					ptr.To(Dash0ExportWithEndpointAndToken()),
					&logger,
				)
			Expect(err).ToNot(HaveOccurred())
			Expect(resourcesHaveBeenCreated).To(BeFalse())
			Expect(resourcesHaveBeenUpdated).To(BeTrue())

			VerifyCollectorResources(ctx, k8sClient, OperatorNamespace, EndpointDash0Test, AuthorizationTokenTest)
		})
	})

	Describe("when OpenTelemetry collector resources have been deleted externally", func() {
		It("should re-created the resources", func() {
			_, _, err :=
				oTelColResourceManager.CreateOrUpdateOpenTelemetryCollectorResources(
					ctx,
					OperatorNamespace,
					TestImages,
					nil,
					nil,
					ptr.To(Dash0ExportWithEndpointAndToken()),
					&logger,
				)
			Expect(err).ToNot(HaveOccurred())

			// Delete some arbitrary resources, then simulate a reconcile cycle and verify that all resources have been
			// recreated.

			daemonSetConifgMap := GetOTelColDaemonSetConfigMap(ctx, k8sClient, OperatorNamespace)
			Expect(k8sClient.Delete(ctx, daemonSetConifgMap)).To(Succeed())

			deploymentConfigMap := GetOTelColDeploymentConfigMap(ctx, k8sClient, OperatorNamespace)
			Expect(k8sClient.Delete(ctx, deploymentConfigMap)).To(Succeed())

			deployment := GetOTelColDeployment(ctx, k8sClient, OperatorNamespace)
			Expect(k8sClient.Delete(ctx, deployment)).To(Succeed())

			resourcesHaveBeenCreated, _, err :=
				oTelColResourceManager.CreateOrUpdateOpenTelemetryCollectorResources(
					ctx,
					OperatorNamespace,
					TestImages,
					nil,
					nil,
					ptr.To(Dash0ExportWithEndpointAndToken()),
					&logger,
				)
			Expect(err).ToNot(HaveOccurred())
			Expect(resourcesHaveBeenCreated).To(BeTrue())

			VerifyCollectorResources(ctx, k8sClient, OperatorNamespace, EndpointDash0Test, AuthorizationTokenTest)
		})
	})

	Describe("when all OpenTelemetry collector resources are up to date", func() {
		It("should report that nothing has changed", func() {
			// create resources
			_, _, err := oTelColResourceManager.CreateOrUpdateOpenTelemetryCollectorResources(
				ctx,
				OperatorNamespace,
				TestImages,
				nil,
				nil,
				ptr.To(Dash0ExportWithEndpointAndToken()),
				&logger,
			)
			Expect(err).ToNot(HaveOccurred())

			// The next run will still report that resources have been updated, since it adds K8S_DAEMONSET_UID and
			// K8S_DEPLOYMENT_UID to the daemon set and deployment respectively (this cannot be done when creating the
			// resources).
			resourcesHaveBeenCreated, resourcesHaveBeenUpdated, err := oTelColResourceManager.CreateOrUpdateOpenTelemetryCollectorResources(
				ctx,
				OperatorNamespace,
				TestImages,
				nil,
				nil,
				ptr.To(Dash0ExportWithEndpointAndToken()),
				&logger,
			)
			Expect(err).ToNot(HaveOccurred())
			Expect(resourcesHaveBeenCreated).To(BeFalse())
			Expect(resourcesHaveBeenUpdated).To(BeTrue())

			// Now run a final create/update, to make sure resourcesHaveBeenCreated/resourcesHaveBeenUpdated come back
			// as false and all resources are in their final desired state.
			resourcesHaveBeenCreated, resourcesHaveBeenUpdated, err =
				oTelColResourceManager.CreateOrUpdateOpenTelemetryCollectorResources(
					ctx,
					OperatorNamespace,
					TestImages,
					nil,
					nil,
					ptr.To(Dash0ExportWithEndpointAndToken()),
					&logger,
				)
			Expect(err).ToNot(HaveOccurred())
			Expect(resourcesHaveBeenCreated).To(BeFalse())
			Expect(resourcesHaveBeenUpdated).To(BeFalse())

			VerifyCollectorResources(ctx, k8sClient, OperatorNamespace, EndpointDash0Test, AuthorizationTokenTest)
		})
	})

	Describe("when deleting all OpenTelemetry collector resources", func() {
		It("should delete the resources", func() {
			// create resources (so there is something to delete)
			_, _, err := oTelColResourceManager.CreateOrUpdateOpenTelemetryCollectorResources(
				ctx,
				OperatorNamespace,
				TestImages,
				nil,
				nil,
				ptr.To(Dash0ExportWithEndpointAndToken()),
				&logger,
			)
			Expect(err).ToNot(HaveOccurred())
			VerifyCollectorResources(ctx, k8sClient, OperatorNamespace, EndpointDash0Test, AuthorizationTokenTest)

			// delete everything again
			resourcesHaveBeenDeleted, err := oTelColResourceManager.DeleteResources(
				ctx,
				OperatorNamespace,
				&logger,
			)
			Expect(err).ToNot(HaveOccurred())
			Expect(resourcesHaveBeenDeleted).To(BeTrue())

			VerifyCollectorResourcesDoNotExist(ctx, k8sClient, OperatorNamespace)

			// make sure deletion is idempotent, deleting again must not create an error, but it must report that no
			// deletion took place
			resourcesHaveBeenDeleted, err = oTelColResourceManager.DeleteResources(
				ctx,
				OperatorNamespace,
				&logger,
			)
			Expect(err).ToNot(HaveOccurred())
			Expect(resourcesHaveBeenDeleted).To(BeFalse())
		})
	})
})

func verifyObject(ctx context.Context, testObject *corev1.ConfigMap) {
	object := &corev1.ConfigMap{}
	err := k8sClient.Get(ctx, client.ObjectKeyFromObject(testObject), object)
	Expect(err).ToNot(HaveOccurred())
	Expect(object.Name).To(Equal(testObject.Name))
	Expect(object.Namespace).To(Equal(testObject.Namespace))
	Expect(object.Labels).To(Equal(testObject.Labels))
	Expect(object.Data).To(Equal(testObject.Data))
}
