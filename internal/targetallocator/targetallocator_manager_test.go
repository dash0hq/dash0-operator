// SPDX-FileCopyrightText: Copyright 2025 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package targetallocator

import (
	"context"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"

	"github.com/dash0hq/dash0-operator/api/operator/common"
	dash0v1alpha1 "github.com/dash0hq/dash0-operator/api/operator/v1alpha1"
	dash0v1beta1 "github.com/dash0hq/dash0-operator/api/operator/v1beta1"
	"github.com/dash0hq/dash0-operator/internal/targetallocator/taresources"
	"github.com/dash0hq/dash0-operator/internal/util"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	. "github.com/dash0hq/dash0-operator/test/util"
	tatest "github.com/dash0hq/dash0-operator/test/util/targetallocator"
)

var (
	operatorNamespace = OperatorNamespace

	testNamespace1 = fmt.Sprintf("%s-1", TestNamespaceName)
	testNamespace2 = fmt.Sprintf("%s-2", TestNamespaceName)
	testNamespace3 = fmt.Sprintf("%s-3", TestNamespaceName)
)

var _ = Describe("The target-allocator manager", Ordered, func() {
	ctx := context.Background()
	logger := log.FromContext(ctx)

	var createdObjectsTargetAllocatorManagerTest []client.Object

	var targetAllocatorManager *TargetAllocatorManager

	BeforeAll(func() {
		EnsureOperatorNamespaceExists(ctx, k8sClient)
		EnsureTestNamespaceExists(ctx, k8sClient)
		EnsureNamespaceExists(ctx, k8sClient, testNamespace1)
		EnsureNamespaceExists(ctx, k8sClient, testNamespace2)
		EnsureNamespaceExists(ctx, k8sClient, testNamespace3)
	})

	BeforeEach(func() {
		createdObjectsTargetAllocatorManagerTest = make([]client.Object, 0)
		targetAllocatorResourceManager := taresources.NewTargetAllocatorResourceManager(
			k8sClient,
			k8sClient.Scheme(),
			OperatorManagerDeployment,
			util.TargetAllocatorConfig{
				Images:                    TestImages,
				OperatorNamespace:         operatorNamespace,
				TargetAllocatorNamePrefix: TargetAllocatorPrefixTest,
			},
		)
		targetAllocatorManager = NewTargetAllocatorManager(
			k8sClient,
			clientset,
			util.ExtraConfigDefaults,
			false,
			targetAllocatorResourceManager,
		)
	})

	AfterEach(func() {
		DeleteAllOperatorConfigurationResources(ctx, k8sClient)
		createdObjectsTargetAllocatorManagerTest = DeleteAllCreatedObjects(ctx, k8sClient, createdObjectsTargetAllocatorManagerTest)
		DeleteAllEvents(ctx, clientset, operatorNamespace)
		err := k8sClient.DeleteAllOf(ctx, &corev1.ConfigMap{}, client.InNamespace(operatorNamespace))
		Expect(err).ToNot(HaveOccurred())
	})

	Describe("when reconciling target-allocator resources", func() {
		It("should create the target-allocator if operator configuration and monitoring resource with PrometheusScraping exist", func() {
			tatest.CreateDefaultOperatorConfigurationResourceWithPrometheusCrdSupport(
				ctx,
				k8sClient,
			)
			monitoringResource := tatest.EnsureMonitoringResourceWithPrometheusScrapingExistsAndIsAvailable(
				ctx,
				k8sClient,
			)
			createdObjectsTargetAllocatorManagerTest = append(createdObjectsTargetAllocatorManagerTest, monitoringResource)

			hasBeenReconciled, err := targetAllocatorManager.ReconcileTargetAllocator(
				ctx,
				TriggeredByDash0ResourceReconcile,
			)
			Expect(err).ToNot(HaveOccurred())
			Expect(hasBeenReconciled).To(BeTrue())
			tatest.VerifyTargetAllocatorResources(ctx, k8sClient, operatorNamespace)
		})

		It("should not create the target-allocator if operator configuration is missing", func() {
			monitoringResource := tatest.EnsureMonitoringResourceWithPrometheusScrapingExistsAndIsAvailable(
				ctx,
				k8sClient,
			)
			createdObjectsTargetAllocatorManagerTest = append(createdObjectsTargetAllocatorManagerTest, monitoringResource)

			hasBeenReconciled, err := targetAllocatorManager.ReconcileTargetAllocator(
				ctx,
				TriggeredByDash0ResourceReconcile,
			)
			Expect(err).ToNot(HaveOccurred())
			Expect(hasBeenReconciled).To(BeTrue())
			tatest.VerifyTargetAllocatorResourcesDoNotExist(ctx, k8sClient, operatorNamespace)
		})

		It("should not create the target-allocator if telemetry collection is disabled", func() {
			CreateOperatorConfigurationResourceWithSpec(
				ctx,
				k8sClient,
				dash0v1alpha1.Dash0OperatorConfigurationSpec{
					TelemetryCollection: dash0v1alpha1.TelemetryCollection{
						Enabled: new(false),
					},
					PrometheusCrdSupport: dash0v1alpha1.PrometheusCrdSupport{
						Enabled: new(true),
					},
				},
			)
			monitoringResource := tatest.EnsureMonitoringResourceWithPrometheusScrapingExistsAndIsAvailable(
				ctx,
				k8sClient,
			)
			createdObjectsTargetAllocatorManagerTest = append(createdObjectsTargetAllocatorManagerTest, monitoringResource)

			hasBeenReconciled, err := targetAllocatorManager.ReconcileTargetAllocator(
				ctx,
				TriggeredByDash0ResourceReconcile,
			)
			Expect(err).ToNot(HaveOccurred())
			Expect(hasBeenReconciled).To(BeTrue())
			tatest.VerifyTargetAllocatorResourcesDoNotExist(ctx, k8sClient, operatorNamespace)
		})

		It("should not create the target-allocator if PrometheusCrdSupport is disabled", func() {
			CreateOperatorConfigurationResourceWithSpec(
				ctx,
				k8sClient,
				dash0v1alpha1.Dash0OperatorConfigurationSpec{
					TelemetryCollection: dash0v1alpha1.TelemetryCollection{
						Enabled: new(true),
					},
					PrometheusCrdSupport: dash0v1alpha1.PrometheusCrdSupport{
						Enabled: new(false),
					},
				},
			)
			monitoringResource := tatest.EnsureMonitoringResourceWithPrometheusScrapingExistsAndIsAvailable(
				ctx,
				k8sClient,
			)
			createdObjectsTargetAllocatorManagerTest = append(createdObjectsTargetAllocatorManagerTest, monitoringResource)

			hasBeenReconciled, err := targetAllocatorManager.ReconcileTargetAllocator(
				ctx,
				TriggeredByDash0ResourceReconcile,
			)
			Expect(err).ToNot(HaveOccurred())
			Expect(hasBeenReconciled).To(BeTrue())
			tatest.VerifyTargetAllocatorResourcesDoNotExist(ctx, k8sClient, operatorNamespace)
		})

		It("should not create the target-allocator if no monitoring resource has PrometheusScraping enabled", func() {
			tatest.CreateDefaultOperatorConfigurationResourceWithPrometheusCrdSupport(
				ctx,
				k8sClient,
			)
			nsName := types.NamespacedName{Namespace: TestNamespaceName, Name: "dash0-monitoring-test-resource-1"}
			monitoringResource := EnsureMonitoringResourceWithSpecExistsInNamespaceAndIsAvailable(
				ctx,
				k8sClient,
				dash0v1beta1.Dash0MonitoringSpec{
					PrometheusScraping: common.PrometheusScraping{
						Enabled: new(false),
					},
				},
				nsName,
			)
			createdObjectsTargetAllocatorManagerTest = append(createdObjectsTargetAllocatorManagerTest, monitoringResource)

			hasBeenReconciled, err := targetAllocatorManager.ReconcileTargetAllocator(
				ctx,
				TriggeredByDash0ResourceReconcile,
			)
			Expect(err).ToNot(HaveOccurred())
			Expect(hasBeenReconciled).To(BeTrue())
			tatest.VerifyTargetAllocatorResourcesDoNotExist(ctx, k8sClient, operatorNamespace)
		})

		It("should delete the target-allocator if the operator configuration is deleted", func() {
			operatorConfigurationResource := tatest.CreateDefaultOperatorConfigurationResourceWithPrometheusCrdSupport(
				ctx,
				k8sClient,
			)
			monitoringResource := tatest.EnsureMonitoringResourceWithPrometheusScrapingExistsAndIsAvailable(
				ctx,
				k8sClient,
			)
			createdObjectsTargetAllocatorManagerTest = append(createdObjectsTargetAllocatorManagerTest, monitoringResource)

			hasBeenReconciled, err := targetAllocatorManager.ReconcileTargetAllocator(
				ctx,
				TriggeredByDash0ResourceReconcile,
			)
			Expect(err).ToNot(HaveOccurred())
			Expect(hasBeenReconciled).To(BeTrue())
			tatest.VerifyTargetAllocatorResources(ctx, k8sClient, operatorNamespace)

			Expect(k8sClient.Delete(ctx, operatorConfigurationResource)).To(Succeed())

			hasBeenReconciled, err = targetAllocatorManager.ReconcileTargetAllocator(
				ctx,
				TriggeredByDash0ResourceReconcile,
			)
			Expect(err).ToNot(HaveOccurred())
			Expect(hasBeenReconciled).To(BeTrue())
			tatest.VerifyTargetAllocatorResourcesDoNotExist(ctx, k8sClient, operatorNamespace)
		})

		It("should delete the target-allocator if telemetry collection is disabled", func() {
			operatorConfigurationResource := tatest.CreateDefaultOperatorConfigurationResourceWithPrometheusCrdSupport(
				ctx,
				k8sClient,
			)
			monitoringResource := tatest.EnsureMonitoringResourceWithPrometheusScrapingExistsAndIsAvailable(
				ctx,
				k8sClient,
			)
			createdObjectsTargetAllocatorManagerTest = append(createdObjectsTargetAllocatorManagerTest, monitoringResource)

			hasBeenReconciled, err := targetAllocatorManager.ReconcileTargetAllocator(
				ctx,
				TriggeredByDash0ResourceReconcile,
			)
			Expect(err).ToNot(HaveOccurred())
			Expect(hasBeenReconciled).To(BeTrue())
			tatest.VerifyTargetAllocatorResources(ctx, k8sClient, operatorNamespace)

			operatorConfigurationResource.Spec.TelemetryCollection.Enabled = new(false)
			Expect(k8sClient.Update(ctx, operatorConfigurationResource)).To(Succeed())

			hasBeenReconciled, err = targetAllocatorManager.ReconcileTargetAllocator(
				ctx,
				TriggeredByDash0ResourceReconcile,
			)
			Expect(err).ToNot(HaveOccurred())
			Expect(hasBeenReconciled).To(BeTrue())
			tatest.VerifyTargetAllocatorResourcesDoNotExist(ctx, k8sClient, operatorNamespace)
		})

		It("should delete the target-allocator if PrometheusCrdSupport is disabled", func() {
			operatorConfigurationResource := tatest.CreateDefaultOperatorConfigurationResourceWithPrometheusCrdSupport(
				ctx,
				k8sClient,
			)
			monitoringResource := tatest.EnsureMonitoringResourceWithPrometheusScrapingExistsAndIsAvailable(
				ctx,
				k8sClient,
			)
			createdObjectsTargetAllocatorManagerTest = append(createdObjectsTargetAllocatorManagerTest, monitoringResource)

			hasBeenReconciled, err := targetAllocatorManager.ReconcileTargetAllocator(
				ctx,
				TriggeredByDash0ResourceReconcile,
			)
			Expect(err).ToNot(HaveOccurred())
			Expect(hasBeenReconciled).To(BeTrue())
			tatest.VerifyTargetAllocatorResources(ctx, k8sClient, operatorNamespace)

			operatorConfigurationResource.Spec.PrometheusCrdSupport.Enabled = new(false)
			Expect(k8sClient.Update(ctx, operatorConfigurationResource)).To(Succeed())

			hasBeenReconciled, err = targetAllocatorManager.ReconcileTargetAllocator(
				ctx,
				TriggeredByDash0ResourceReconcile,
			)
			Expect(err).ToNot(HaveOccurred())
			Expect(hasBeenReconciled).To(BeTrue())
			tatest.VerifyTargetAllocatorResourcesDoNotExist(ctx, k8sClient, operatorNamespace)
		})

		It("should delete the target-allocator if all monitoring resources disable PrometheusScraping", func() {
			tatest.CreateDefaultOperatorConfigurationResourceWithPrometheusCrdSupport(
				ctx,
				k8sClient,
			)
			monitoringResource := tatest.EnsureMonitoringResourceWithPrometheusScrapingExistsAndIsAvailable(
				ctx,
				k8sClient,
			)
			createdObjectsTargetAllocatorManagerTest = append(createdObjectsTargetAllocatorManagerTest, monitoringResource)

			hasBeenReconciled, err := targetAllocatorManager.ReconcileTargetAllocator(
				ctx,
				TriggeredByDash0ResourceReconcile,
			)
			Expect(err).ToNot(HaveOccurred())
			Expect(hasBeenReconciled).To(BeTrue())
			tatest.VerifyTargetAllocatorResources(ctx, k8sClient, operatorNamespace)

			monitoringResource.Spec.PrometheusScraping = common.PrometheusScraping{
				Enabled: new(false),
			}
			Expect(k8sClient.Update(ctx, monitoringResource)).To(Succeed())

			hasBeenReconciled, err = targetAllocatorManager.ReconcileTargetAllocator(
				ctx,
				TriggeredByDash0ResourceReconcile,
			)
			Expect(err).ToNot(HaveOccurred())
			Expect(hasBeenReconciled).To(BeTrue())
			tatest.VerifyTargetAllocatorResourcesDoNotExist(ctx, k8sClient, operatorNamespace)
		})

		It("should create the target-allocator with multiple namespaces when multiple monitoring resources have PrometheusScraping enabled", func() {
			tatest.CreateDefaultOperatorConfigurationResourceWithPrometheusCrdSupport(
				ctx,
				k8sClient,
			)

			firstName := types.NamespacedName{Namespace: testNamespace1, Name: "dash0-monitoring-test-resource-1"}
			firstMonitoringResource := tatest.EnsureMonitoringResourceWithPrometheusScrapingExistsInNamespaceAndIsAvailable(ctx, k8sClient, firstName)
			createdObjectsTargetAllocatorManagerTest = append(createdObjectsTargetAllocatorManagerTest, firstMonitoringResource)

			secondName := types.NamespacedName{Namespace: testNamespace2, Name: "dash0-monitoring-test-resource-2"}
			secondMonitoringResource := tatest.EnsureMonitoringResourceWithPrometheusScrapingExistsInNamespaceAndIsAvailable(ctx, k8sClient, secondName)
			createdObjectsTargetAllocatorManagerTest = append(createdObjectsTargetAllocatorManagerTest, secondMonitoringResource)

			hasBeenReconciled, err := targetAllocatorManager.ReconcileTargetAllocator(
				ctx,
				TriggeredByDash0ResourceReconcile,
			)
			Expect(err).ToNot(HaveOccurred())
			Expect(hasBeenReconciled).To(BeTrue())
			tatest.VerifyTargetAllocatorResources(ctx, k8sClient, operatorNamespace)
			tatest.VerifyTargetAllocatorConfigMapContainsNamespaces(ctx, k8sClient, operatorNamespace, []string{testNamespace1, testNamespace2})
		})

		It("should not delete the target-allocator if some monitoring resources still have PrometheusScraping enabled", func() {
			tatest.CreateDefaultOperatorConfigurationResourceWithPrometheusCrdSupport(
				ctx,
				k8sClient,
			)

			firstName := types.NamespacedName{Namespace: testNamespace1, Name: "dash0-monitoring-test-resource-1"}
			firstMonitoringResource := tatest.EnsureMonitoringResourceWithPrometheusScrapingExistsInNamespaceAndIsAvailable(ctx, k8sClient, firstName)
			createdObjectsTargetAllocatorManagerTest = append(createdObjectsTargetAllocatorManagerTest, firstMonitoringResource)

			secondName := types.NamespacedName{Namespace: testNamespace2, Name: "dash0-monitoring-test-resource-2"}
			secondMonitoringResource := tatest.EnsureMonitoringResourceWithPrometheusScrapingExistsInNamespaceAndIsAvailable(ctx, k8sClient, secondName)
			createdObjectsTargetAllocatorManagerTest = append(createdObjectsTargetAllocatorManagerTest, secondMonitoringResource)

			hasBeenReconciled, err := targetAllocatorManager.ReconcileTargetAllocator(
				ctx,
				TriggeredByDash0ResourceReconcile,
			)
			Expect(err).ToNot(HaveOccurred())
			Expect(hasBeenReconciled).To(BeTrue())
			tatest.VerifyTargetAllocatorResources(ctx, k8sClient, operatorNamespace)

			firstMonitoringResource.Spec.PrometheusScraping = common.PrometheusScraping{
				Enabled: new(false),
			}
			Expect(k8sClient.Update(ctx, firstMonitoringResource)).To(Succeed())

			hasBeenReconciled, err = targetAllocatorManager.ReconcileTargetAllocator(
				ctx,
				TriggeredByDash0ResourceReconcile,
			)
			Expect(err).ToNot(HaveOccurred())
			Expect(hasBeenReconciled).To(BeTrue())
			tatest.VerifyTargetAllocatorResources(ctx, k8sClient, operatorNamespace)
		})

		It("should handle idempotent reconciliation correctly", func() {
			tatest.CreateDefaultOperatorConfigurationResourceWithPrometheusCrdSupport(
				ctx,
				k8sClient,
			)
			monitoringResource := tatest.EnsureMonitoringResourceWithPrometheusScrapingExistsAndIsAvailable(
				ctx,
				k8sClient,
			)
			createdObjectsTargetAllocatorManagerTest = append(createdObjectsTargetAllocatorManagerTest, monitoringResource)

			hasBeenReconciled, err := targetAllocatorManager.ReconcileTargetAllocator(
				ctx,
				TriggeredByDash0ResourceReconcile,
			)
			Expect(err).ToNot(HaveOccurred())
			Expect(hasBeenReconciled).To(BeTrue())
			tatest.VerifyTargetAllocatorResources(ctx, k8sClient, operatorNamespace)

			hasBeenReconciled, err = targetAllocatorManager.ReconcileTargetAllocator(
				ctx,
				TriggeredByDash0ResourceReconcile,
			)
			Expect(err).ToNot(HaveOccurred())
			Expect(hasBeenReconciled).To(BeTrue())
			tatest.VerifyTargetAllocatorResources(ctx, k8sClient, operatorNamespace)
		})
	})

	Describe("when updating the extra config", func() {
		BeforeEach(func() {
			tatest.CreateDefaultOperatorConfigurationResourceWithPrometheusCrdSupport(
				ctx,
				k8sClient,
			)
			resource := tatest.EnsureMonitoringResourceWithPrometheusScrapingExistsAndIsAvailable(
				ctx,
				k8sClient,
			)
			createdObjectsTargetAllocatorManagerTest = append(createdObjectsTargetAllocatorManagerTest, resource)
		})

		AfterEach(func() {
			_, err := targetAllocatorManager.targetAllocatorResourceManager.DeleteResources(
				ctx,
				util.ExtraConfigDefaults,
				logger,
			)
			Expect(err).ToNot(HaveOccurred())

			DeleteAllOperatorConfigurationResources(ctx, k8sClient)
		})

		It("should ignore updates if extra config content has not changed", func() {
			hasBeenReconciled, err := targetAllocatorManager.ReconcileTargetAllocator(
				ctx,
				TriggeredByDash0ResourceReconcile,
			)
			Expect(err).ToNot(HaveOccurred())
			Expect(hasBeenReconciled).To(BeTrue())
			tatest.VerifyTargetAllocatorResources(ctx, k8sClient, operatorNamespace)

			targetAllocatorManager.UpdateExtraConfig(ctx, util.ExtraConfigDefaults, logger)
			tatest.VerifyTargetAllocatorResources(ctx, k8sClient, operatorNamespace)
		})

		It("should apply updates if extra config content has changed", func() {
			hasBeenReconciled, err := targetAllocatorManager.ReconcileTargetAllocator(
				ctx,
				TriggeredByDash0ResourceReconcile,
			)
			Expect(err).ToNot(HaveOccurred())
			Expect(hasBeenReconciled).To(BeTrue())
			tatest.VerifyTargetAllocatorResources(ctx, k8sClient, operatorNamespace)

			changedConfig := util.ExtraConfigDefaults
			changedConfig.TargetAllocatorContainerResources = util.ResourceRequirementsWithGoMemLimit{
				Limits: corev1.ResourceList{
					corev1.ResourceMemory: resource.MustParse("600Mi"),
				},
				Requests: corev1.ResourceList{
					corev1.ResourceMemory: resource.MustParse("600Mi"),
				},
			}
			changedConfig.TargetAllocatorTolerations = []corev1.Toleration{
				{
					Key:      "test-key",
					Operator: corev1.TolerationOpEqual,
					Value:    "test-value",
					Effect:   corev1.TaintEffectNoSchedule,
				},
			}
			targetAllocatorManager.UpdateExtraConfig(ctx, changedConfig, logger)

			deployment := tatest.VerifyTargetAllocatorDeploymentExists(ctx, k8sClient, operatorNamespace)
			Expect(deployment.Spec.Template.Spec.Containers).To(HaveLen(1))
			Expect(deployment.Spec.Template.Spec.Containers[0].Resources.Limits.Memory().String()).To(Equal("600Mi"))
			Expect(deployment.Spec.Template.Spec.Containers[0].Resources.Requests.Memory().String()).To(Equal("600Mi"))
			Expect(deployment.Spec.Template.Spec.Tolerations).To(HaveLen(1))
			Expect(deployment.Spec.Template.Spec.Tolerations[0].Key).To(Equal("test-key"))
		})
	})

	Describe("when handling concurrent reconciliation requests", func() {
		It("should skip reconciliation if another reconciliation is already in progress", func() {
			tatest.CreateDefaultOperatorConfigurationResourceWithPrometheusCrdSupport(
				ctx,
				k8sClient,
			)
			monitoringResource := tatest.EnsureMonitoringResourceWithPrometheusScrapingExistsAndIsAvailable(
				ctx,
				k8sClient,
			)
			createdObjectsTargetAllocatorManagerTest = append(createdObjectsTargetAllocatorManagerTest, monitoringResource)

			targetAllocatorManager.updateInProgress.Store(true)

			hasBeenReconciled, err := targetAllocatorManager.ReconcileTargetAllocator(
				ctx,
				TriggeredByWatchEvent,
			)
			Expect(err).ToNot(HaveOccurred())
			Expect(hasBeenReconciled).To(BeFalse())

			targetAllocatorManager.updateInProgress.Store(false)

			hasBeenReconciled, err = targetAllocatorManager.ReconcileTargetAllocator(
				ctx,
				TriggeredByDash0ResourceReconcile,
			)
			Expect(err).ToNot(HaveOccurred())
			Expect(hasBeenReconciled).To(BeTrue())
			tatest.VerifyTargetAllocatorResources(ctx, k8sClient, operatorNamespace)
		})
	})

	Describe("when handling different reconciliation triggers", func() {
		BeforeEach(func() {
			tatest.CreateDefaultOperatorConfigurationResourceWithPrometheusCrdSupport(
				ctx,
				k8sClient,
			)
			monitoringResource := tatest.EnsureMonitoringResourceWithPrometheusScrapingExistsAndIsAvailable(
				ctx,
				k8sClient,
			)
			createdObjectsTargetAllocatorManagerTest = append(createdObjectsTargetAllocatorManagerTest, monitoringResource)
		})

		It("should handle reconciliation triggered by watch events", func() {
			hasBeenReconciled, err := targetAllocatorManager.ReconcileTargetAllocator(
				ctx,
				TriggeredByWatchEvent,
			)
			Expect(err).ToNot(HaveOccurred())
			Expect(hasBeenReconciled).To(BeTrue())
			tatest.VerifyTargetAllocatorResources(ctx, k8sClient, operatorNamespace)
		})

		It("should handle reconciliation triggered by Dash0 resource reconcile", func() {
			hasBeenReconciled, err := targetAllocatorManager.ReconcileTargetAllocator(
				ctx,
				TriggeredByDash0ResourceReconcile,
			)
			Expect(err).ToNot(HaveOccurred())
			Expect(hasBeenReconciled).To(BeTrue())
			tatest.VerifyTargetAllocatorResources(ctx, k8sClient, operatorNamespace)
		})
	})

	Describe("when PrometheusScraping is enabled by default", func() {
		It("should create the target-allocator when monitoring resource does not explicitly set prometheusScraping.enabled", func() {
			tatest.CreateDefaultOperatorConfigurationResourceWithPrometheusCrdSupport(
				ctx,
				k8sClient,
			)
			monitoringResource := EnsureMonitoringResourceExistsAndIsAvailable(
				ctx,
				k8sClient,
			)
			createdObjectsTargetAllocatorManagerTest = append(createdObjectsTargetAllocatorManagerTest, monitoringResource)

			hasBeenReconciled, err := targetAllocatorManager.ReconcileTargetAllocator(
				ctx,
				TriggeredByDash0ResourceReconcile,
			)
			Expect(err).ToNot(HaveOccurred())
			Expect(hasBeenReconciled).To(BeTrue())
			tatest.VerifyTargetAllocatorResources(ctx, k8sClient, operatorNamespace)
		})
	})
})
