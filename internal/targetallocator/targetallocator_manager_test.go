// SPDX-FileCopyrightText: Copyright 2025 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package targetallocator

import (
	"context"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/dash0hq/dash0-operator/api/operator/common"
	dash0v1alpha1 "github.com/dash0hq/dash0-operator/api/operator/v1alpha1"
	dash0v1beta1 "github.com/dash0hq/dash0-operator/api/operator/v1beta1"
	"github.com/dash0hq/dash0-operator/internal/targetallocator/taresources"
	"github.com/dash0hq/dash0-operator/internal/util"
	"github.com/dash0hq/dash0-operator/internal/util/logd"

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
	logger := logd.FromContext(ctx)

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

		It("should still watch all namespaces with PrometheusScraping if allowSecretAccessInNamespaces is set", func() {
			extraConfig := util.ExtraConfigDefaults
			extraConfig.TargetAllocatorAllowSecretAccessInNamespaces = []string{testNamespace1, testNamespace3}
			targetAllocatorManager.extraConfig.Store(&extraConfig)
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

			clusterRole := &rbacv1.ClusterRole{}
			Expect(k8sClient.Get(
				ctx,
				types.NamespacedName{Name: tatest.ExpectedTargetAllocatorClusterRoleName},
				clusterRole,
			)).To(Succeed())
			for _, rule := range clusterRole.Rules {
				Expect(rule.Resources).ToNot(ContainElement("secrets"))
			}
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

	Describe("when component settings are configured in the operator configuration resource", func() {
		BeforeEach(func() {
			CreateOperatorConfigurationResourceWithSpec(
				ctx,
				k8sClient,
				dash0v1alpha1.Dash0OperatorConfigurationSpec{
					TelemetryCollection:  dash0v1alpha1.TelemetryCollection{Enabled: new(true)},
					PrometheusCrdSupport: dash0v1alpha1.PrometheusCrdSupport{Enabled: new(true)},
					Components: &dash0v1alpha1.OperatorConfigurationComponents{
						TargetAllocator: &dash0v1alpha1.TargetAllocatorSettings{
							WorkloadSettings: dash0v1alpha1.WorkloadSettings{
								PodLabels: map[string]string{"target-allocator-label": "resource"},
							},
						},
					},
				},
			)
			monitoringResource := tatest.EnsureMonitoringResourceWithPrometheusScrapingExistsAndIsAvailable(
				ctx,
				k8sClient,
			)
			createdObjectsTargetAllocatorManagerTest = append(createdObjectsTargetAllocatorManagerTest, monitoringResource)
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

		It("should apply the settings of the operator configuration resource instead of the Helm values", func() {
			fileConfig := util.ExtraConfigDefaults
			fileConfig.TargetAllocatorPodAnnotations = map[string]string{"helm-annotation": "helm"}
			targetAllocatorManager.extraConfig.Store(&fileConfig)

			hasBeenReconciled, err := targetAllocatorManager.ReconcileTargetAllocator(
				ctx,
				TriggeredByDash0ResourceReconcile,
			)
			Expect(err).ToNot(HaveOccurred())
			Expect(hasBeenReconciled).To(BeTrue())

			deployment := tatest.VerifyTargetAllocatorDeploymentExists(ctx, k8sClient, operatorNamespace)
			Expect(deployment.Spec.Template.Labels).To(HaveKeyWithValue("target-allocator-label", "resource"))
			Expect(deployment.Spec.Template.Annotations).To(HaveKeyWithValue("helm-annotation", "helm"))
		})
	})

	Describe("when handling concurrent reconciliation requests", func() {
		It("does not reconcile when a reconciliation is already in progress, but does not lose the trigger", func() {
			tatest.CreateDefaultOperatorConfigurationResourceWithPrometheusCrdSupport(
				ctx,
				k8sClient,
			)
			monitoringResource := tatest.EnsureMonitoringResourceWithPrometheusScrapingExistsAndIsAvailable(
				ctx,
				k8sClient,
			)
			createdObjectsTargetAllocatorManagerTest = append(createdObjectsTargetAllocatorManagerTest, monitoringResource)

			// Occupy the manager's reconcile guard and trigger a reconciliation from within it, the way a watch event
			// or an extra config map update would arrive while a reconciliation is running.
			executions := 0
			var skippedHasBeenReconciled bool
			var skippedErr error
			_, err := targetAllocatorManager.reconcileGuard.Run(func() (bool, error) {
				executions++
				if executions == 1 {
					skippedHasBeenReconciled, skippedErr = targetAllocatorManager.ReconcileTargetAllocator(
						ctx,
						TriggeredByWatchEvent,
					)
				}
				return true, nil
			}, nil, nil)

			Expect(err).ToNot(HaveOccurred())
			Expect(skippedErr).ToNot(HaveOccurred())
			Expect(skippedHasBeenReconciled).To(BeFalse())
			// The reconciliation was not executed, so no resources were created ...
			tatest.VerifyTargetAllocatorResourcesDoNotExist(ctx, k8sClient, operatorNamespace)
			// ... but the trigger was recorded and the guard repeated the reconciliation once, instead of dropping it.
			Expect(executions).To(Equal(2))

			hasBeenReconciled, err := targetAllocatorManager.ReconcileTargetAllocator(
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

	Describe("when logging namespaces without secret access", func() {
		expectedMessage := func(namespace string) string {
			return fmt.Sprintf("The Dash0Monitoring resource in namespace \"%s\" has Prometheus scraping enabled, but "+
				"the namespace is not listed in the Helm value operator.targetAllocator.allowSecretAccessInNamespaces. "+
				"The target-allocator cannot read secrets in this namespace. Prometheus CRDs (PodMonitor, ServiceMonitor, "+
				"ScrapeConfig) in this namespace that reference a secret will be rejected by the target-allocator.",
				namespace)
		}

		It("should not log anything if allowSecretAccessInNamespaces is not set", func() {
			capturingLogger, capturingLogSink := NewCapturingLogger()
			logNamespacesWithoutSecretAccess([]string{testNamespace1, testNamespace2}, nil, capturingLogger)
			capturingLogSink.HasNoLogMessages(Default)
		})

		It("should log a message for every namespace that is not listed in allowSecretAccessInNamespaces", func() {
			capturingLogger, capturingLogSink := NewCapturingLogger()
			logNamespacesWithoutSecretAccess(
				[]string{testNamespace1, testNamespace2, testNamespace3},
				[]string{testNamespace1},
				capturingLogger,
			)
			capturingLogSink.HasNoLogMessage(Default, expectedMessage(testNamespace1))
			capturingLogSink.HasLogMessage(Default, expectedMessage(testNamespace2))
			capturingLogSink.HasLogMessage(Default, expectedMessage(testNamespace3))
		})
	})
})
