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
	"sigs.k8s.io/controller-runtime/pkg/client"

	dash0v1alpha1 "github.com/dash0hq/dash0-operator/api/operator/v1alpha1"
	"github.com/dash0hq/dash0-operator/internal/util"
	"github.com/dash0hq/dash0-operator/internal/util/logd"

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
	logger := logd.FromContext(ctx)

	var oTelColResourceManager *OTelColResourceManager

	BeforeAll(func() {
		EnsureOperatorNamespaceExists(ctx, k8sClient)
		EnsureTestNamespaceExists(ctx, k8sClient)
	})

	BeforeEach(func() {
		oTelColResourceManager = NewOTelColResourceManager(
			k8sClient,
			k8sClient.Scheme(),
			OperatorManagerDeployment,
			util.CollectorConfig{
				Images:                  TestImages,
				OperatorNamespace:       OperatorNamespace,
				OTelCollectorNamePrefix: OTelCollectorNamePrefixTest,
				DevelopmentMode:         true,
			},
		)
	})

	AfterEach(func() {
		_, err := oTelColResourceManager.DeleteResources(
			ctx,
			util.ExtraConfigDefaults,
			logger,
		)
		Expect(err).ToNot(HaveOccurred())
		Eventually(func(g Gomega) {
			VerifyCollectorResourcesDoNotExist(ctx, k8sClient, OperatorNamespace)
		}, 500*time.Millisecond, 20*time.Millisecond).Should(Succeed())
		Expect(k8sClient.DeleteAllOf(ctx, &corev1.ConfigMap{}, client.InNamespace(OperatorNamespace))).To(Succeed())
	})

	Describe("when dealing with individual resources", func() {
		It("should create a single resource", func() {
			isNew, isChanged, err := oTelColResourceManager.createOrUpdateResource(ctx, testResource.DeepCopy(), logger)
			Expect(err).ToNot(HaveOccurred())
			Expect(isNew).To(BeTrue())
			Expect(isChanged).To(BeFalse())
			verifyObject(ctx, testResource)
		})

		It("should update a single object", func() {
			err := oTelColResourceManager.createResource(ctx, testResource.DeepCopy(), logger)
			Expect(err).ToNot(HaveOccurred())

			updated := testResource.DeepCopy()
			updated.Data["key"] = "updated value"
			isNew, isChanged, err := oTelColResourceManager.createOrUpdateResource(ctx, updated, logger)

			Expect(err).ToNot(HaveOccurred())
			Expect(isNew).To(BeFalse())
			Expect(isChanged).To(BeTrue())
			verifyObject(ctx, updated)
		})

		It("should report that nothing has changed for a single object", func() {
			err := oTelColResourceManager.createResource(ctx, testResource.DeepCopy(), logger)
			Expect(err).ToNot(HaveOccurred())

			isNew, isChanged, err := oTelColResourceManager.createOrUpdateResource(
				ctx,
				testResource.DeepCopy(),
				logger,
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

		It("should use the provided export settings to create the collectors", func() {
			operatorConfiguration := DefaultOperatorConfigurationResource()
			resourcesHaveBeenCreated, resourcesHaveBeenUpdated, err :=
				oTelColResourceManager.CreateOrUpdateOpenTelemetryCollectorResources(
					ctx,
					util.ExtraConfigDefaults,
					operatorConfiguration,
					nil,
					nil,
					logger,
				)
			Expect(err).ToNot(HaveOccurred())
			Expect(resourcesHaveBeenCreated).To(BeTrue())
			Expect(resourcesHaveBeenUpdated).To(BeFalse())
			VerifyCollectorResources(ctx, k8sClient, OperatorNamespace, EndpointDash0Test, AuthorizationDefaultEnvVar, AuthorizationTokenTest)
		})

		It("should not add self-monitoring if it is disabled", func() {
			operatorConfiguration := &dash0v1alpha1.Dash0OperatorConfiguration{
				ObjectMeta: OperatorConfigurationResourceDefaultObjectMeta,
				Spec:       OperatorConfigurationResourceWithoutSelfMonitoringWithToken,
			}
			resourcesHaveBeenCreated, resourcesHaveBeenUpdated, err :=
				oTelColResourceManager.CreateOrUpdateOpenTelemetryCollectorResources(
					ctx,
					util.ExtraConfigDefaults,
					operatorConfiguration,
					nil,
					nil,
					logger,
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
					ContainElement(MatchEnvVar("SELF_MONITORING_AUTH_TOKEN", AuthorizationTokenTest)))

			}
		})

		It("should respect self-monitoring settings when creating the collectors", func() {
			operatorConfiguration := DefaultOperatorConfigurationResource()
			resourcesHaveBeenCreated, resourcesHaveBeenUpdated, err :=
				oTelColResourceManager.CreateOrUpdateOpenTelemetryCollectorResources(
					ctx,
					util.ExtraConfigDefaults,
					operatorConfiguration,
					nil,
					nil,
					logger,
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
					ContainElement(MatchEnvVar("SELF_MONITORING_AUTH_TOKEN", AuthorizationTokenTest)))

			}
		})

		It("should delete resources that are no longer desired", func() {
			// reconcile once with KubernetesInfrastructureMetricsCollectionEnabled = true
			operatorConfiguration := DefaultOperatorConfigurationResource()
			_, _, err :=
				oTelColResourceManager.CreateOrUpdateOpenTelemetryCollectorResources(
					ctx,
					util.ExtraConfigDefaults,
					operatorConfiguration,
					nil,
					nil,
					logger,
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
			operatorConfiguration.Spec.KubernetesInfrastructureMetricsCollection.Enabled = new(false)
			_, _, err =
				oTelColResourceManager.CreateOrUpdateOpenTelemetryCollectorResources(
					ctx,
					util.ExtraConfigDefaults,
					operatorConfiguration,
					nil,
					nil,
					logger,
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

			operatorConfiguration := DefaultOperatorConfigurationResource()
			_, _, err :=
				oTelColResourceManager.CreateOrUpdateOpenTelemetryCollectorResources(
					ctx,
					util.ExtraConfigDefaults,
					operatorConfiguration,
					nil,
					nil,
					logger,
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
			operatorConfiguration := DefaultOperatorConfigurationResource()
			_, _, err :=
				oTelColResourceManager.CreateOrUpdateOpenTelemetryCollectorResources(
					ctx,
					util.ExtraConfigDefaults,
					operatorConfiguration,
					nil,
					nil,
					logger,
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
					util.ExtraConfigDefaults,
					operatorConfiguration,
					nil,
					nil,
					logger,
				)
			Expect(err).ToNot(HaveOccurred())
			Expect(resourcesHaveBeenCreated).To(BeFalse())
			Expect(resourcesHaveBeenUpdated).To(BeTrue())

			VerifyCollectorResources(ctx, k8sClient, OperatorNamespace, EndpointDash0Test, AuthorizationDefaultEnvVar, AuthorizationTokenTest)
		})
	})

	Describe("when OpenTelemetry collector resources have been deleted externally", func() {
		It("should re-created the resources", func() {
			operatorConfiguration := DefaultOperatorConfigurationResource()
			_, _, err :=
				oTelColResourceManager.CreateOrUpdateOpenTelemetryCollectorResources(
					ctx,
					util.ExtraConfigDefaults,
					operatorConfiguration,
					nil,
					nil,
					logger,
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
					util.ExtraConfigDefaults,
					operatorConfiguration,
					nil,
					nil,
					logger,
				)
			Expect(err).ToNot(HaveOccurred())
			Expect(resourcesHaveBeenCreated).To(BeTrue())

			VerifyCollectorResources(ctx, k8sClient, OperatorNamespace, EndpointDash0Test, AuthorizationDefaultEnvVar, AuthorizationTokenTest)
		})
	})

	Describe("when all OpenTelemetry collector resources are up to date", func() {
		It("should report that nothing has changed", func() {
			operatorConfiguration := DefaultOperatorConfigurationResource()

			// create resources
			_, _, err := oTelColResourceManager.CreateOrUpdateOpenTelemetryCollectorResources(
				ctx,
				util.ExtraConfigDefaults,
				operatorConfiguration,
				nil,
				nil,
				logger,
			)
			Expect(err).ToNot(HaveOccurred())

			// The next run will still report that resources have been updated, since it adds K8S_DAEMONSET_UID and
			// K8S_DEPLOYMENT_UID to the daemon set and deployment respectively (this cannot be done when creating the
			// resources).
			resourcesHaveBeenCreated, resourcesHaveBeenUpdated, err := oTelColResourceManager.CreateOrUpdateOpenTelemetryCollectorResources(
				ctx,
				util.ExtraConfigDefaults,
				operatorConfiguration,
				nil,
				nil,
				logger,
			)
			Expect(err).ToNot(HaveOccurred())
			Expect(resourcesHaveBeenCreated).To(BeFalse())
			Expect(resourcesHaveBeenUpdated).To(BeTrue())

			// Now run a final create/update, to make sure resourcesHaveBeenCreated/resourcesHaveBeenUpdated come back
			// as false and all resources are in their final desired state.
			resourcesHaveBeenCreated, resourcesHaveBeenUpdated, err =
				oTelColResourceManager.CreateOrUpdateOpenTelemetryCollectorResources(
					ctx,
					util.ExtraConfigDefaults,
					operatorConfiguration,
					nil,
					nil,
					logger,
				)
			Expect(err).ToNot(HaveOccurred())
			Expect(resourcesHaveBeenCreated).To(BeFalse())
			Expect(resourcesHaveBeenUpdated).To(BeFalse())

			VerifyCollectorResources(ctx, k8sClient, OperatorNamespace, EndpointDash0Test, AuthorizationDefaultEnvVar, AuthorizationTokenTest)
		})
	})

	Describe("when deleting all OpenTelemetry collector resources", func() {
		It("should delete the resources", func() {
			operatorConfiguration := DefaultOperatorConfigurationResource()

			// create resources (so there is something to delete)
			_, _, err := oTelColResourceManager.CreateOrUpdateOpenTelemetryCollectorResources(
				ctx,
				util.ExtraConfigDefaults,
				operatorConfiguration,
				nil,
				nil,
				logger,
			)
			Expect(err).ToNot(HaveOccurred())
			VerifyCollectorResources(ctx, k8sClient, OperatorNamespace, EndpointDash0Test, AuthorizationDefaultEnvVar, AuthorizationTokenTest)

			// delete everything again
			resourcesHaveBeenDeleted, err := oTelColResourceManager.DeleteResources(
				ctx,
				util.ExtraConfigDefaults,
				logger,
			)
			Expect(err).ToNot(HaveOccurred())
			Expect(resourcesHaveBeenDeleted).To(BeTrue())

			VerifyCollectorResourcesDoNotExist(ctx, k8sClient, OperatorNamespace)

			// make sure deletion is idempotent, deleting again must not create an error, but it must report that no
			// deletion took place
			resourcesHaveBeenDeleted, err = oTelColResourceManager.DeleteResources(
				ctx,
				util.ExtraConfigDefaults,
				logger,
			)
			Expect(err).ToNot(HaveOccurred())
			Expect(resourcesHaveBeenDeleted).To(BeFalse())
		})
	})
})

var _ = Describe("intelligentEdgeConfigFromResource", func() {
	logger := logd.FromContext(context.Background())
	operatorConfig := &dash0v1alpha1.Dash0OperatorConfiguration{
		Spec: OperatorConfigurationResourceDash0ExportWithApiEndpointWithToken,
	}
	operatorNamespace := OperatorNamespace

	It("should return disabled config when resource is nil", func() {
		config := intelligentEdgeConfigFromResource(nil, operatorConfig, operatorNamespace, namePrefix, logger)
		Expect(config.Enabled).To(BeFalse())
	})

	It("should return disabled config when spec.enabled is false", func() {
		resource := &dash0v1alpha1.Dash0IntelligentEdge{
			Spec: dash0v1alpha1.Dash0IntelligentEdgeSpec{
				Enabled: boolPtr(false),
			},
		}
		config := intelligentEdgeConfigFromResource(resource, operatorConfig, operatorNamespace, namePrefix, logger)
		Expect(config.Enabled).To(BeFalse())
	})

	It("should derive endpoints from operator config when no overrides are set", func() {
		resource := &dash0v1alpha1.Dash0IntelligentEdge{
			Spec: dash0v1alpha1.Dash0IntelligentEdgeSpec{
				Enabled: boolPtr(true),
				Barker:  dash0v1alpha1.BarkerConfig{Enabled: boolPtr(false)},
			},
		}
		config := intelligentEdgeConfigFromResource(resource, operatorConfig, operatorNamespace, namePrefix, logger)
		Expect(config.Enabled).To(BeTrue())
		Expect(config.Endpoint).To(Equal(util.DeriveDecisionMakerEndpoint(EndpointDash0Test)))
		Expect(config.ApiEndpoint).To(Equal(deriveCpaEndpoint(ApiEndpointTest)))
	})

	It("should use explicit decision maker endpoint when set", func() {
		resource := &dash0v1alpha1.Dash0IntelligentEdge{
			Spec: dash0v1alpha1.Dash0IntelligentEdgeSpec{
				Enabled: boolPtr(true),
				Barker:  dash0v1alpha1.BarkerConfig{Enabled: boolPtr(false)},
				Sampling: dash0v1alpha1.SamplingConfig{
					DecisionMakerEndpoint: "custom-dm.example.com:443",
				},
			},
		}
		config := intelligentEdgeConfigFromResource(resource, operatorConfig, operatorNamespace, namePrefix, logger)
		Expect(config.Enabled).To(BeTrue())
		Expect(config.Endpoint).To(Equal("custom-dm.example.com:443"))
		// CPA endpoint should still be derived
		Expect(config.ApiEndpoint).To(Equal(deriveCpaEndpoint(ApiEndpointTest)))
	})

	It("should use explicit control plane API endpoint when set", func() {
		resource := &dash0v1alpha1.Dash0IntelligentEdge{
			Spec: dash0v1alpha1.Dash0IntelligentEdgeSpec{
				Enabled:                 boolPtr(true),
				Barker:                  dash0v1alpha1.BarkerConfig{Enabled: boolPtr(false)},
				ControlPlaneApiEndpoint: "https://custom-cpa.example.com",
			},
		}
		config := intelligentEdgeConfigFromResource(resource, operatorConfig, operatorNamespace, namePrefix, logger)
		Expect(config.Enabled).To(BeTrue())
		Expect(config.ApiEndpoint).To(Equal("https://custom-cpa.example.com"))
		// DM endpoint should still be derived
		Expect(config.Endpoint).To(Equal(util.DeriveDecisionMakerEndpoint(EndpointDash0Test)))
	})

	It("should use both explicit endpoints when both are set", func() {
		resource := &dash0v1alpha1.Dash0IntelligentEdge{
			Spec: dash0v1alpha1.Dash0IntelligentEdgeSpec{
				Enabled:                 boolPtr(true),
				Barker:                  dash0v1alpha1.BarkerConfig{Enabled: boolPtr(false)},
				ControlPlaneApiEndpoint: "https://custom-cpa.example.com",
				Sampling: dash0v1alpha1.SamplingConfig{
					DecisionMakerEndpoint: "custom-dm.example.com:443",
				},
			},
		}
		config := intelligentEdgeConfigFromResource(resource, operatorConfig, operatorNamespace, namePrefix, logger)
		Expect(config.Enabled).To(BeTrue())
		Expect(config.Endpoint).To(Equal("custom-dm.example.com:443"))
		Expect(config.ApiEndpoint).To(Equal("https://custom-cpa.example.com"))
	})

	It("should override explicit DM endpoint with barker service when barker is enabled", func() {
		resource := &dash0v1alpha1.Dash0IntelligentEdge{
			Spec: dash0v1alpha1.Dash0IntelligentEdgeSpec{
				Enabled: boolPtr(true),
				Barker: dash0v1alpha1.BarkerConfig{
					Enabled: boolPtr(true),
				},
				Sampling: dash0v1alpha1.SamplingConfig{
					DecisionMakerEndpoint: "custom-dm.example.com:443",
				},
			},
		}
		config := intelligentEdgeConfigFromResource(resource, operatorConfig, operatorNamespace, namePrefix, logger)
		Expect(config.Enabled).To(BeTrue())
		Expect(config.Endpoint).To(Equal(fmt.Sprintf("%s-barker.%s.svc.cluster.local:8011", namePrefix, operatorNamespace)))
		Expect(config.Insecure).To(BeTrue())
	})

	It("should handle nil operator config with explicit endpoints", func() {
		resource := &dash0v1alpha1.Dash0IntelligentEdge{
			Spec: dash0v1alpha1.Dash0IntelligentEdgeSpec{
				Enabled:                 boolPtr(true),
				Barker:                  dash0v1alpha1.BarkerConfig{Enabled: boolPtr(false)},
				ControlPlaneApiEndpoint: "https://custom-cpa.example.com",
				Sampling: dash0v1alpha1.SamplingConfig{
					DecisionMakerEndpoint: "custom-dm.example.com:443",
				},
			},
		}
		config := intelligentEdgeConfigFromResource(resource, nil, operatorNamespace, namePrefix, logger)
		Expect(config.Enabled).To(BeTrue())
		Expect(config.Endpoint).To(Equal("custom-dm.example.com:443"))
		Expect(config.ApiEndpoint).To(Equal("https://custom-cpa.example.com"))
		Expect(config.Dataset).To(Equal(util.DatasetDefault))
	})

	It("should have sampling enabled by default", func() {
		resource := &dash0v1alpha1.Dash0IntelligentEdge{
			Spec: dash0v1alpha1.Dash0IntelligentEdgeSpec{
				Enabled: boolPtr(true),
				Barker:  dash0v1alpha1.BarkerConfig{Enabled: boolPtr(false)},
			},
		}
		config := intelligentEdgeConfigFromResource(resource, operatorConfig, operatorNamespace, namePrefix, logger)
		Expect(config.Enabled).To(BeTrue())
		Expect(config.SamplingEnabled).To(BeTrue())
	})

	It("should allow disabling sampling while keeping IE enabled", func() {
		resource := &dash0v1alpha1.Dash0IntelligentEdge{
			Spec: dash0v1alpha1.Dash0IntelligentEdgeSpec{
				Enabled: boolPtr(true),
				Barker:  dash0v1alpha1.BarkerConfig{Enabled: boolPtr(false)},
				Sampling: dash0v1alpha1.SamplingConfig{
					Enabled: boolPtr(false),
				},
			},
		}
		config := intelligentEdgeConfigFromResource(resource, operatorConfig, operatorNamespace, namePrefix, logger)
		Expect(config.Enabled).To(BeTrue())
		Expect(config.SamplingEnabled).To(BeFalse())
	})

	It("should return disabled config when overall IE is disabled regardless of sampling setting", func() {
		resource := &dash0v1alpha1.Dash0IntelligentEdge{
			Spec: dash0v1alpha1.Dash0IntelligentEdgeSpec{
				Enabled: boolPtr(false),
				Sampling: dash0v1alpha1.SamplingConfig{
					Enabled: boolPtr(true),
				},
			},
		}
		config := intelligentEdgeConfigFromResource(resource, operatorConfig, operatorNamespace, namePrefix, logger)
		Expect(config.Enabled).To(BeFalse())
	})
})

func boolPtr(b bool) *bool {
	return &b
}

func verifyObject(ctx context.Context, testObject *corev1.ConfigMap) {
	object := &corev1.ConfigMap{}
	err := k8sClient.Get(ctx, client.ObjectKeyFromObject(testObject), object)
	Expect(err).ToNot(HaveOccurred())
	Expect(object.Name).To(Equal(testObject.Name))
	Expect(object.Namespace).To(Equal(testObject.Namespace))
	Expect(object.Labels).To(Equal(testObject.Labels))
	Expect(object.Data).To(Equal(testObject.Data))
}
