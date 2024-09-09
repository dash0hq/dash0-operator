// SPDX-FileCopyrightText: Copyright 2024 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package otelcolresources

import (
	"context"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"

	dash0v1alpha1 "github.com/dash0hq/dash0-operator/api/dash0monitoring/v1alpha1"
	"github.com/dash0hq/dash0-operator/internal/dash0/selfmonitoring"

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
			Namespace: Dash0OperatorNamespace,
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
	var dash0MonitoringResource *dash0v1alpha1.Dash0Monitoring

	BeforeAll(func() {
		EnsureDash0OperatorNamespaceExists(ctx, k8sClient)
		EnsureTestNamespaceExists(ctx, k8sClient)
		dash0MonitoringResource = EnsureDash0MonitoringResourceExists(ctx, k8sClient)
	})

	BeforeEach(func() {
		oTelColResourceManager = &OTelColResourceManager{
			Client:                  k8sClient,
			Scheme:                  k8sClient.Scheme(),
			DeploymentSelfReference: DeploymentSelfReference,
			OTelCollectorNamePrefix: "unit-test",
			DevelopmentMode:         true,
		}
	})

	AfterEach(func() {
		err := k8sClient.DeleteAllOf(ctx, &corev1.ConfigMap{}, client.InNamespace(Dash0OperatorNamespace))
		Expect(err).ToNot(HaveOccurred())
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
		It("should create the resources", func() {
			resourcesHaveBeenCreated, resourcesHaveBeenUpdated, err :=
				oTelColResourceManager.CreateOrUpdateOpenTelemetryCollectorResources(
					ctx,
					Dash0OperatorNamespace,
					TestImages,
					dash0MonitoringResource,
					selfmonitoring.SelfMonitoringConfiguration{},
					&logger,
				)
			Expect(err).ToNot(HaveOccurred())
			Expect(resourcesHaveBeenCreated).To(BeTrue())
			Expect(resourcesHaveBeenUpdated).To(BeFalse())

			VerifyCollectorResources(ctx, k8sClient, Dash0OperatorNamespace)
		})
	})

	Describe("when OpenTelemetry collector resources have been modified externally", func() {
		It("should reconcile the resources back into the desired state", func() {
			_, _, err :=
				oTelColResourceManager.CreateOrUpdateOpenTelemetryCollectorResources(
					ctx,
					Dash0OperatorNamespace,
					TestImages,
					dash0MonitoringResource,
					selfmonitoring.SelfMonitoringConfiguration{},
					&logger,
				)
			Expect(err).ToNot(HaveOccurred())

			// Change some arbitrary fields in some resources, then simulate a reconcile cycle and verify that all
			// resources are back in their desired state.

			daemonSetConifgMap := GetOTelColDaemonSetConfigMap(ctx, k8sClient, Dash0OperatorNamespace)
			daemonSetConifgMap.Data["config.yaml"] = "{}"
			daemonSetConifgMap.Data["bogus-key"] = ""
			Expect(k8sClient.Update(ctx, daemonSetConifgMap)).To(Succeed())

			daemonSet := GetOTelColDaemonSet(ctx, k8sClient, Dash0OperatorNamespace)
			daemonSet.Spec.Template.Spec.InitContainers = []corev1.Container{}
			daemonSet.Spec.Template.Spec.Containers[0].Image = "wrong-collector-image:latest"
			daemonSet.Spec.Template.Spec.Containers[0].Ports = []corev1.ContainerPort{
				{ContainerPort: 1234},
				{ContainerPort: 1235},
			}
			Expect(k8sClient.Update(ctx, daemonSet)).To(Succeed())

			deploymentConfigMap := GetOTelColDeploymentConfigMap(ctx, k8sClient, Dash0OperatorNamespace)
			deploymentConfigMap.Data["config.yaml"] = "{}"
			deploymentConfigMap.Data["bogus-key"] = ""
			Expect(k8sClient.Update(ctx, deploymentConfigMap)).To(Succeed())

			deployment := GetOTelColDeployment(ctx, k8sClient, Dash0OperatorNamespace)
			var changedReplicas int32 = 5
			deployment.Spec.Replicas = &changedReplicas
			deployment.Spec.Template.Spec.Containers[0].Ports = []corev1.ContainerPort{
				{ContainerPort: 1234},
			}
			Expect(k8sClient.Update(ctx, deployment)).To(Succeed())

			resourcesHaveBeenCreated, resourcesHaveBeenUpdated, err :=
				oTelColResourceManager.CreateOrUpdateOpenTelemetryCollectorResources(
					ctx,
					Dash0OperatorNamespace,
					TestImages,
					dash0MonitoringResource,
					selfmonitoring.SelfMonitoringConfiguration{},
					&logger,
				)
			Expect(err).ToNot(HaveOccurred())
			Expect(resourcesHaveBeenCreated).To(BeFalse())
			Expect(resourcesHaveBeenUpdated).To(BeTrue())

			VerifyCollectorResources(ctx, k8sClient, Dash0OperatorNamespace)
		})
	})

	Describe("when OpenTelemetry collector resources have been deleted externally", func() {
		It("should re-created the resources", func() {
			_, _, err :=
				oTelColResourceManager.CreateOrUpdateOpenTelemetryCollectorResources(
					ctx,
					Dash0OperatorNamespace,
					TestImages,
					dash0MonitoringResource,
					selfmonitoring.SelfMonitoringConfiguration{},
					&logger,
				)
			Expect(err).ToNot(HaveOccurred())

			// Delete some arbitrary resources, then simulate a reconcile cycle and verify that all resources have been
			// recreated.

			daemonSetConifgMap := GetOTelColDaemonSetConfigMap(ctx, k8sClient, Dash0OperatorNamespace)
			Expect(k8sClient.Delete(ctx, daemonSetConifgMap)).To(Succeed())

			deploymentConfigMap := GetOTelColDeploymentConfigMap(ctx, k8sClient, Dash0OperatorNamespace)
			Expect(k8sClient.Delete(ctx, deploymentConfigMap)).To(Succeed())

			deployment := GetOTelColDeployment(ctx, k8sClient, Dash0OperatorNamespace)
			Expect(k8sClient.Delete(ctx, deployment)).To(Succeed())

			resourcesHaveBeenCreated, resourcesHaveBeenUpdated, err :=
				oTelColResourceManager.CreateOrUpdateOpenTelemetryCollectorResources(
					ctx,
					Dash0OperatorNamespace,
					TestImages,
					dash0MonitoringResource,
					selfmonitoring.SelfMonitoringConfiguration{},
					&logger,
				)
			Expect(err).ToNot(HaveOccurred())
			Expect(resourcesHaveBeenCreated).To(BeTrue())
			Expect(resourcesHaveBeenUpdated).To(BeFalse())

			VerifyCollectorResources(ctx, k8sClient, Dash0OperatorNamespace)
		})
	})

	Describe("when all OpenTelemetry collector resources are up to date", func() {
		It("should report that nothing has changed", func() {
			// create resources (so we are sure that everything is in the desired state)
			_, _, err := oTelColResourceManager.CreateOrUpdateOpenTelemetryCollectorResources(
				ctx,
				Dash0OperatorNamespace,
				TestImages,
				dash0MonitoringResource,
				selfmonitoring.SelfMonitoringConfiguration{},
				&logger,
			)
			Expect(err).ToNot(HaveOccurred())

			// Now run another create/update, to make sure resourcesHaveBeenCreated/resourcesHaveBeenUpdated come back
			// as false.
			resourcesHaveBeenCreated, resourcesHaveBeenUpdated, err :=
				oTelColResourceManager.CreateOrUpdateOpenTelemetryCollectorResources(
					ctx,
					Dash0OperatorNamespace,
					TestImages,
					dash0MonitoringResource,
					selfmonitoring.SelfMonitoringConfiguration{},
					&logger,
				)
			Expect(err).ToNot(HaveOccurred())
			Expect(resourcesHaveBeenCreated).To(BeFalse())
			Expect(resourcesHaveBeenUpdated).To(BeFalse())

			VerifyCollectorResources(ctx, k8sClient, Dash0OperatorNamespace)
		})
	})

	Describe("when deleting all OpenTelemetry collector resources", func() {
		It("should delete the resources", func() {
			// create resources (so there is something to delete)
			_, _, err := oTelColResourceManager.CreateOrUpdateOpenTelemetryCollectorResources(
				ctx,
				Dash0OperatorNamespace,
				TestImages,
				dash0MonitoringResource,
				selfmonitoring.SelfMonitoringConfiguration{},
				&logger,
			)
			Expect(err).ToNot(HaveOccurred())
			VerifyCollectorResources(ctx, k8sClient, Dash0OperatorNamespace)

			// delete everything again
			err = oTelColResourceManager.DeleteResources(
				ctx,
				Dash0OperatorNamespace,
				TestImages,
				dash0MonitoringResource,
				selfmonitoring.SelfMonitoringConfiguration{},
				&logger,
			)
			Expect(err).ToNot(HaveOccurred())

			VerifyCollectorResourcesDoNotExist(ctx, k8sClient, Dash0OperatorNamespace)
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
