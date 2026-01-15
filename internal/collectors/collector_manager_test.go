// SPDX-FileCopyrightText: Copyright 2024 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package collectors

import (
	"context"
	"slices"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"

	dash0common "github.com/dash0hq/dash0-operator/api/operator/common"
	dash0v1alpha1 "github.com/dash0hq/dash0-operator/api/operator/v1alpha1"
	dash0v1beta1 "github.com/dash0hq/dash0-operator/api/operator/v1beta1"
	"github.com/dash0hq/dash0-operator/internal/collectors/otelcolresources"
	"github.com/dash0hq/dash0-operator/internal/util"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	. "github.com/dash0hq/dash0-operator/test/util"
)

var (
	operatorNamespace = OperatorNamespace
)

var _ = Describe("The collector manager", Ordered, func() {
	ctx := context.Background()
	logger := log.FromContext(ctx)

	var createdObjectsCollectorManagerTest []client.Object

	var collectorManager *CollectorManager

	BeforeAll(func() {
		EnsureOperatorNamespaceExists(ctx, k8sClient)
		EnsureTestNamespaceExists(ctx, k8sClient)
	})

	BeforeEach(func() {
		createdObjectsCollectorManagerTest = make([]client.Object, 0)
		oTelColResourceManager := otelcolresources.NewOTelColResourceManager(
			k8sClient,
			k8sClient.Scheme(),
			OperatorManagerDeployment,
			util.CollectorConfig{
				Images:                    TestImages,
				OperatorNamespace:         operatorNamespace,
				OTelCollectorNamePrefix:   OTelCollectorNamePrefixTest,
				TargetAllocatorNamePrefix: TargetAllocatorPrefixTest,
			},
		)
		collectorManager = NewCollectorManager(
			k8sClient,
			clientset,
			util.ExtraConfigDefaults,
			false,
			oTelColResourceManager,
		)
	})

	AfterEach(func() {
		createdObjectsCollectorManagerTest = DeleteAllCreatedObjects(ctx, k8sClient, createdObjectsCollectorManagerTest)
		DeleteAllEvents(ctx, clientset, operatorNamespace)
		err := k8sClient.DeleteAllOf(ctx, &corev1.ConfigMap{}, client.InNamespace(operatorNamespace))
		Expect(err).ToNot(HaveOccurred())
	})

	Describe("when validation checks fail", func() {
		BeforeEach(func() {
			CreateDefaultOperatorConfigurationResource(ctx, k8sClient)
		})

		AfterEach(func() {
			DeleteAllOperatorConfigurationResources(ctx, k8sClient)
		})

		It("should fail if no endpoint is provided", func() {
			operatorConfig := LoadOperatorConfigurationResourceOrFail(ctx, k8sClient, Default)
			operatorConfig.Spec.Export = &dash0common.Export{
				Dash0: &dash0common.Dash0Configuration{
					Authorization: dash0common.Authorization{
						Token: &AuthorizationTokenTest,
					},
				},
			}
			Expect(k8sClient.Update(ctx, operatorConfig)).To(HaveOccurred())
		})

		It("should fail if neither authorization token nor secret ref are provided for Dash0 exporter", func() {
			operatorConfig := LoadOperatorConfigurationResourceOrFail(ctx, k8sClient, Default)
			operatorConfig.Spec.Export = &dash0common.Export{
				Dash0: &dash0common.Dash0Configuration{
					Endpoint:      EndpointDash0Test,
					Authorization: dash0common.Authorization{},
				},
			}
			Expect(k8sClient.Update(ctx, operatorConfig)).To(HaveOccurred())
		})
	})

	Describe("when creating OpenTelemetry collector resources", func() {

		AfterEach(func() {
			_, err := collectorManager.oTelColResourceManager.DeleteResources(
				ctx,
				util.ExtraConfigDefaults,
				&logger,
			)
			Expect(err).ToNot(HaveOccurred())

			DeleteAllOperatorConfigurationResources(ctx, k8sClient)
		})

		It("should do nothing if there is no operator configuration resource and also no monitoring resource", func() {
			hasBeenReconciled, err := collectorManager.ReconcileOpenTelemetryCollector(
				ctx,
			)
			Expect(err).ToNot(HaveOccurred())
			Expect(hasBeenReconciled).To(BeTrue())
			VerifyCollectorResourcesDoNotExist(ctx, k8sClient, operatorNamespace)
		})

		It("should create the Dash0 collectors based on the operator configuration's export settings", func() {
			CreateDefaultOperatorConfigurationResource(
				ctx,
				k8sClient,
			)
			hasBeenReconciled, err := collectorManager.ReconcileOpenTelemetryCollector(
				ctx,
			)
			Expect(err).ToNot(HaveOccurred())
			Expect(hasBeenReconciled).To(BeTrue())
			VerifyCollectorResources(ctx, k8sClient, operatorNamespace, EndpointDash0Test, AuthorizationDefaultEnvVar, AuthorizationTokenTest)
		})

		It("should do nothing if there is no operator configuration resource even if there is a monitoring resource with export", func() {
			// With the new implementation, an operator configuration is required for default exporters.
			// Monitoring resources only provide namespaced exporters.
			monitoringResource := EnsureMonitoringResourceWithSpecExistsAndIsAvailable(
				ctx,
				k8sClient,
				dash0v1beta1.Dash0MonitoringSpec{
					Export: &dash0common.Export{
						Dash0: &dash0common.Dash0Configuration{
							Endpoint: EndpointDash0TestAlternative,
							Authorization: dash0common.Authorization{
								Token: &AuthorizationTokenTestAlternative,
							},
						},
					},
				},
			)
			createdObjectsCollectorManagerTest = append(createdObjectsCollectorManagerTest, monitoringResource)

			hasBeenReconciled, err := collectorManager.ReconcileOpenTelemetryCollector(
				ctx,
			)
			Expect(err).ToNot(HaveOccurred())
			Expect(hasBeenReconciled).To(BeTrue())
			// No collector resources should be created without an operator configuration
			VerifyCollectorResourcesDoNotExist(ctx, k8sClient, operatorNamespace)
		})

		It("should do nothing if the operator configuration resource has telemetryCollection.enabled=false", func() {
			CreateOperatorConfigurationResourceWithSpec(
				ctx,
				k8sClient,
				dash0v1alpha1.Dash0OperatorConfigurationSpec{
					TelemetryCollection: dash0v1alpha1.TelemetryCollection{
						Enabled: ptr.To(false),
					},
				},
			)
			hasBeenReconciled, err := collectorManager.ReconcileOpenTelemetryCollector(
				ctx,
			)
			Expect(err).ToNot(HaveOccurred())
			Expect(hasBeenReconciled).To(BeTrue())
			VerifyCollectorResourcesDoNotExist(ctx, k8sClient, operatorNamespace)
		})

		It("should do nothing if the operator configuration resource has no export", func() {
			CreateOperatorConfigurationResourceWithSpec(
				ctx,
				k8sClient,
				dash0v1alpha1.Dash0OperatorConfigurationSpec{},
			)
			hasBeenReconciled, err := collectorManager.ReconcileOpenTelemetryCollector(
				ctx,
			)
			Expect(err).ToNot(HaveOccurred())
			Expect(hasBeenReconciled).To(BeTrue())
			VerifyCollectorResourcesDoNotExist(ctx, k8sClient, operatorNamespace)
		})
	})

	Describe("when updating OpenTelemetry collector resources", func() {

		BeforeEach(func() {
			// Create operator configuration resource - required for collector creation
			CreateDefaultOperatorConfigurationResource(ctx, k8sClient)
			// creating a valid monitoring resource beforehand, just so we get past the
			// m.findAllMonitoringResources step.
			resource := EnsureMonitoringResourceExistsAndIsAvailable(
				ctx,
				k8sClient,
			)
			createdObjectsCollectorManagerTest = append(createdObjectsCollectorManagerTest, resource)
		})

		AfterEach(func() {
			DeleteAllOperatorConfigurationResources(ctx, k8sClient)
		})

		It("should update the resources", func() {
			err := k8sClient.Create(ctx, &corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Name:      ExpectedDaemonSetCollectorConfigMapName,
					Namespace: operatorNamespace,
					Labels: map[string]string{
						"wrong-key": "value",
					},
					Annotations: map[string]string{
						"wrong-key": "value",
					},
				},
				Data: map[string]string{
					"wrong-key": "{}",
				},
			})
			Expect(err).ToNot(HaveOccurred())

			hasBeenReconciled, err := collectorManager.ReconcileOpenTelemetryCollector(
				ctx,
			)
			Expect(err).ToNot(HaveOccurred())
			Expect(hasBeenReconciled).To(BeTrue())
			VerifyCollectorResources(ctx, k8sClient, operatorNamespace, EndpointDash0Test, AuthorizationDefaultEnvVar, AuthorizationTokenTest)

			// verify that all wrong properties that we have set up initially have been removed
			cm := GetOTelColDaemonSetConfigMap(ctx, k8sClient, operatorNamespace)
			Expect(cm.Data).To(HaveKey("config.yaml"))
			Expect(cm.Data).ToNot(HaveKey("wrong-key"))
			Expect(cm.Labels).ToNot(HaveKey("wrong-key"))
			Expect(cm.Annotations).ToNot(HaveKey("wrong-key"))
		})
	})

	Describe("when deciding whether to delete the OpenTelemetry collector resources", func() {

		AfterEach(func() {
			_, err := collectorManager.oTelColResourceManager.DeleteResources(
				ctx,
				util.ExtraConfigDefaults,
				&logger,
			)
			Expect(err).ToNot(HaveOccurred())

			DeleteAllOperatorConfigurationResources(ctx, k8sClient)
		})

		It("should not delete the collector if there is an operator configuration and the Dash0 monitoring resource that is being deleted is the only one left", func() {
			CreateDefaultOperatorConfigurationResource(
				ctx,
				k8sClient,
			)

			resourceName := types.NamespacedName{Namespace: TestNamespaceName, Name: "dash0-monitoring-test-resource-1"}
			monitoringResource := EnsureMonitoringResourceExistsInNamespaceAndIsAvailable(ctx, k8sClient, resourceName)
			createdObjectsCollectorManagerTest = append(createdObjectsCollectorManagerTest, monitoringResource)

			// Let the manager create the collector so there is something to delete.
			_, err := collectorManager.ReconcileOpenTelemetryCollector(
				ctx,
			)
			Expect(err).ToNot(HaveOccurred())
			VerifyCollectorResources(ctx, k8sClient, operatorNamespace, EndpointDash0Test, AuthorizationDefaultEnvVar, AuthorizationTokenTest)

			hasBeenReconciled, err := collectorManager.ReconcileOpenTelemetryCollector(
				ctx,
			)
			Expect(err).ToNot(HaveOccurred())
			Expect(hasBeenReconciled).To(BeTrue())
			// verify the collector is not deleted even if the monitoring resource provided as a parameter is the only
			// one left, when there is still an operator configuration left,
			VerifyCollectorResources(ctx, k8sClient, operatorNamespace, EndpointDash0Test, AuthorizationDefaultEnvVar, AuthorizationTokenTest)
		})

		It("should not delete the collector if there is an operator configuration resource and no monitoring resource exists", func() {
			CreateDefaultOperatorConfigurationResource(
				ctx,
				k8sClient,
			)

			// Let the manager create the collector so there is something to delete.
			monitoringResource := EnsureMonitoringResourceExistsAndIsAvailable(
				ctx,
				k8sClient,
			)
			_, err := collectorManager.ReconcileOpenTelemetryCollector(
				ctx,
			)
			Expect(err).ToNot(HaveOccurred())
			VerifyCollectorResources(ctx, k8sClient, operatorNamespace, EndpointDash0Test, AuthorizationDefaultEnvVar, AuthorizationTokenTest)

			Expect(k8sClient.Delete(ctx, monitoringResource)).To(Succeed())

			hasBeenReconciled, err := collectorManager.ReconcileOpenTelemetryCollector(
				ctx,
			)
			Expect(err).ToNot(HaveOccurred())
			Expect(hasBeenReconciled).To(BeTrue())
			// verify the collector is not deleted even if the last monitoring resource has been deleted, but there is
			// still an operator configuration left,
			VerifyCollectorResources(ctx, k8sClient, operatorNamespace, EndpointDash0Test, AuthorizationDefaultEnvVar, AuthorizationTokenTest)
		})

		It("should not delete the collector if there is an operator configuration resource and multiple monitoring resources exist", func() {
			CreateDefaultOperatorConfigurationResource(
				ctx,
				k8sClient,
			)

			// create multiple monitoring resources
			firstName := types.NamespacedName{Namespace: TestNamespaceName, Name: "dash0-monitoring-test-resource-1"}
			firstDash0MonitoringResource := EnsureMonitoringResourceExistsInNamespaceAndIsAvailable(ctx, k8sClient, firstName)
			createdObjectsCollectorManagerTest = append(createdObjectsCollectorManagerTest, firstDash0MonitoringResource)

			secondName := types.NamespacedName{Namespace: TestNamespaceName, Name: "dash0-monitoring-test-resource-2"}
			secondDash0MonitoringResource := EnsureMonitoringResourceExistsInNamespaceAndIsAvailable(ctx, k8sClient, secondName)
			createdObjectsCollectorManagerTest = append(createdObjectsCollectorManagerTest, secondDash0MonitoringResource)

			thirdName := types.NamespacedName{Namespace: TestNamespaceName, Name: "dash0-monitoring-test-resource-3"}
			thirdDash0MonitoringResource := EnsureMonitoringResourceExistsInNamespaceAndIsAvailable(ctx, k8sClient, thirdName)
			createdObjectsCollectorManagerTest = append(createdObjectsCollectorManagerTest, thirdDash0MonitoringResource)

			// Let the manager create the collector so there is something to delete.
			_, err := collectorManager.ReconcileOpenTelemetryCollector(
				ctx,
			)
			Expect(err).ToNot(HaveOccurred())
			VerifyCollectorResources(ctx, k8sClient, operatorNamespace, EndpointDash0Test, AuthorizationDefaultEnvVar, AuthorizationTokenTest)

			hasBeenReconciled, err := collectorManager.ReconcileOpenTelemetryCollector(
				ctx,
			)
			Expect(err).ToNot(HaveOccurred())
			Expect(hasBeenReconciled).To(BeTrue())
			// since operator configuration exists, the collector resources should not be deleted
			VerifyCollectorResources(ctx, k8sClient, operatorNamespace, EndpointDash0Test, AuthorizationDefaultEnvVar, AuthorizationTokenTest)
		})

		It("should delete the Dash0 collectors if operator configuration has telemetryCollection.enabled=false ", func() {
			operatorConfiguration := CreateDefaultOperatorConfigurationResource(
				ctx,
				k8sClient,
			)
			hasBeenReconciled, err := collectorManager.ReconcileOpenTelemetryCollector(
				ctx,
			)
			Expect(err).ToNot(HaveOccurred())
			Expect(hasBeenReconciled).To(BeTrue())
			VerifyCollectorResources(ctx, k8sClient, operatorNamespace, EndpointDash0Test, AuthorizationDefaultEnvVar, AuthorizationTokenTest)

			operatorConfiguration.Spec.TelemetryCollection.Enabled = ptr.To(false)
			Expect(k8sClient.Update(ctx, operatorConfiguration)).To(Succeed())

			hasBeenReconciled, err = collectorManager.ReconcileOpenTelemetryCollector(
				ctx,
			)
			Expect(err).ToNot(HaveOccurred())
			Expect(hasBeenReconciled).To(BeTrue())
			VerifyCollectorResourcesDoNotExist(ctx, k8sClient, operatorNamespace)
		})

		It("should delete the collector if the operator configuration is deleted", func() {
			operatorConfigurationResource := CreateDefaultOperatorConfigurationResource(
				ctx,
				k8sClient,
			)

			_, err := collectorManager.ReconcileOpenTelemetryCollector(
				ctx,
			)
			Expect(err).ToNot(HaveOccurred())
			VerifyCollectorResources(ctx, k8sClient, operatorNamespace, EndpointDash0Test, AuthorizationDefaultEnvVar, AuthorizationTokenTest)

			Expect(k8sClient.Delete(ctx, operatorConfigurationResource)).To(Succeed())

			hasBeenReconciled, err := collectorManager.ReconcileOpenTelemetryCollector(
				ctx,
			)
			Expect(err).ToNot(HaveOccurred())
			Expect(hasBeenReconciled).To(BeTrue())
			VerifyCollectorResourcesDoNotExist(ctx, k8sClient, operatorNamespace)
		})

		It("should delete the collector if the operator configuration is deleted even if monitoring resources with export exist", func() {
			operatorConfigurationResource := CreateDefaultOperatorConfigurationResource(
				ctx,
				k8sClient,
			)

			resourceName := types.NamespacedName{Namespace: TestNamespaceName, Name: "dash0-monitoring-test-resource-1"}
			monitoringResource := EnsureMonitoringResourceExistsInNamespaceAndIsAvailable(ctx, k8sClient, resourceName)
			createdObjectsCollectorManagerTest = append(createdObjectsCollectorManagerTest, monitoringResource)

			// Let the manager create the collector so there is something to delete.
			hasBeenReconciled, err := collectorManager.ReconcileOpenTelemetryCollector(
				ctx,
			)
			Expect(err).ToNot(HaveOccurred())
			Expect(hasBeenReconciled).To(BeTrue())
			VerifyCollectorResources(ctx, k8sClient, operatorNamespace, EndpointDash0Test, AuthorizationDefaultEnvVar, AuthorizationTokenTest)

			// Delete the operator configuration
			Expect(k8sClient.Delete(ctx, operatorConfigurationResource)).To(Succeed())

			hasBeenReconciled, err = collectorManager.ReconcileOpenTelemetryCollector(
				ctx,
			)
			Expect(err).ToNot(HaveOccurred())
			Expect(hasBeenReconciled).To(BeTrue())
			// verify the collector is deleted when operator config is removed,
			// even if monitoring resources with export still exist
			VerifyCollectorResourcesDoNotExist(ctx, k8sClient, operatorNamespace)
		})
	})

	Describe("when updating the extra config map", func() {
		BeforeEach(func() {
			// Create operator configuration resource - required for collector creation
			CreateDefaultOperatorConfigurationResource(ctx, k8sClient)
			// creating a valid monitoring resource beforehand, just so we get past the
			// m.findAllMonitoringResources step.
			resource := EnsureMonitoringResourceExistsAndIsAvailable(
				ctx,
				k8sClient,
			)
			createdObjectsCollectorManagerTest = append(createdObjectsCollectorManagerTest, resource)
		})

		AfterEach(func() {
			_, err := collectorManager.oTelColResourceManager.DeleteResources(
				ctx,
				util.ExtraConfigDefaults,
				&logger,
			)
			Expect(err).ToNot(HaveOccurred())

			DeleteAllOperatorConfigurationResources(ctx, k8sClient)
		})

		It("should ignore updates if extra config map content has not changed", func() {
			hasBeenReconciled, err := collectorManager.ReconcileOpenTelemetryCollector(
				ctx,
			)
			Expect(err).ToNot(HaveOccurred())
			Expect(hasBeenReconciled).To(BeTrue())
			VerifyCollectorResources(ctx, k8sClient, operatorNamespace, EndpointDash0Test, AuthorizationDefaultEnvVar, AuthorizationTokenTest)

			collectorManager.UpdateExtraConfig(ctx, util.ExtraConfigDefaults, &logger)
			VerifyCollectorResources(ctx, k8sClient, operatorNamespace, EndpointDash0Test, AuthorizationDefaultEnvVar, AuthorizationTokenTest)
		})

		It("should apply updates if extra config map content has changed", func() {
			hasBeenReconciled, err := collectorManager.ReconcileOpenTelemetryCollector(
				ctx,
			)
			Expect(err).ToNot(HaveOccurred())
			Expect(hasBeenReconciled).To(BeTrue())
			VerifyCollectorResources(ctx, k8sClient, operatorNamespace, EndpointDash0Test, AuthorizationDefaultEnvVar, AuthorizationTokenTest)

			changedConfig := util.ExtraConfigDefaults
			changedConfig.CollectorDaemonSetCollectorContainerResources = util.ResourceRequirementsWithGoMemLimit{
				Limits: corev1.ResourceList{
					corev1.ResourceMemory: resource.MustParse("600Mi"),
				},
				GoMemLimit: "500MiB",
				Requests: corev1.ResourceList{
					corev1.ResourceMemory: resource.MustParse("600Mi"),
				},
			}
			changedConfig.CollectorFilelogOffsetStorageVolume = &corev1.Volume{
				Name: "filelogreceiver-offsets",
				VolumeSource: corev1.VolumeSource{
					HostPath: &corev1.HostPathVolumeSource{
						Path: "/data/dash0-operator/offset-storage",
						Type: ptr.To(corev1.HostPathDirectoryOrCreate),
					},
				},
			}
			changedConfig.DaemonSetTolerations = []corev1.Toleration{
				{
					Key:      "test-key",
					Operator: corev1.TolerationOpEqual,
					Value:    "test-value",
					Effect:   corev1.TaintEffectNoSchedule,
				},
			}
			collectorManager.UpdateExtraConfig(ctx, changedConfig, &logger)

			ds_ := VerifyResourceExists(
				ctx,
				k8sClient,
				operatorNamespace,
				ExpectedDaemonSetName,
				&appsv1.DaemonSet{},
			)
			ds := ds_.(*appsv1.DaemonSet)
			podSpec := ds.Spec.Template.Spec
			initContainers := podSpec.InitContainers
			Expect(initContainers).To(HaveLen(1))
			Expect(initContainers[0].Name).To(Equal("filelog-offset-volume-ownership"))
			Expect(initContainers[0].Image).To(Equal(TestImages.FilelogOffsetVolumeOwnershipImage))
			containers := podSpec.Containers
			Expect(containers).To(HaveLen(2))

			collectorContainerIdx := slices.IndexFunc(containers, func(c corev1.Container) bool {
				return c.Name == "opentelemetry-collector"
			})
			collectorContainer := containers[collectorContainerIdx]
			Expect(collectorContainer.Resources.Limits.Memory().String()).To(Equal("600Mi"))
			Expect(collectorContainer.Resources.Requests.Memory().String()).To(Equal("600Mi"))
			Expect(collectorContainer.Env).To(ContainElement(MatchEnvVar("GOMEMLIMIT", "500MiB")))

			Expect(podSpec.Volumes).To(ContainElement(MatchVolume("filelogreceiver-offsets")))
			offsetStorageVolumeIdx := slices.IndexFunc(podSpec.Volumes, func(c corev1.Volume) bool {
				return c.Name == "filelogreceiver-offsets"
			})
			offsetStorageVolume := podSpec.Volumes[offsetStorageVolumeIdx]
			Expect(offsetStorageVolume.HostPath).ToNot(BeNil())
			Expect(offsetStorageVolume.HostPath.Path).To(Equal("/data/dash0-operator/offset-storage"))
			Expect(offsetStorageVolume.HostPath.Type).ToNot(BeNil())
			Expect(*offsetStorageVolume.HostPath.Type).To(Equal(corev1.HostPathDirectoryOrCreate))

			Expect(podSpec.Tolerations).To(HaveLen(1))
			toleration := podSpec.Tolerations[0]
			Expect(toleration.Key).To(Equal("test-key"))
			Expect(toleration.Operator).To(Equal(corev1.TolerationOpEqual))
			Expect(toleration.Value).To(Equal("test-value"))
			Expect(toleration.Effect).To(Equal(corev1.TaintEffectNoSchedule))
		})
	})
})
