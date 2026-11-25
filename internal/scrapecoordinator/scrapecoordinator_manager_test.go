// SPDX-FileCopyrightText: Copyright 2026 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package scrapecoordinator

import (
	"context"
	"fmt"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/yaml"

	"github.com/dash0hq/dash0-operator/api/operator/common"
	dash0v1alpha1 "github.com/dash0hq/dash0-operator/api/operator/v1alpha1"
	dash0v1beta1 "github.com/dash0hq/dash0-operator/api/operator/v1beta1"
	"github.com/dash0hq/dash0-operator/internal/scrapecoordinator/scoresources"
	"github.com/dash0hq/dash0-operator/internal/util"
	"github.com/dash0hq/dash0-operator/internal/util/logd"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	. "github.com/dash0hq/dash0-operator/test/util"
	tatest "github.com/dash0hq/dash0-operator/test/util/targetallocator"
)

var (
	operatorNamespace = OperatorNamespace
	testNamespace1    = fmt.Sprintf("%s-1", TestNamespaceName)
)

var _ = Describe("The scrape coordinator manager", Ordered, func() {
	ctx := context.Background()
	logger := logd.FromContext(ctx)

	var createdObjects []client.Object
	var scrapeCoordinatorManager *ScrapeCoordinatorManager

	BeforeAll(func() {
		EnsureOperatorNamespaceExists(ctx, k8sClient)
		EnsureTestNamespaceExists(ctx, k8sClient)
		EnsureNamespaceExists(ctx, k8sClient, testNamespace1)
	})

	BeforeEach(func() {
		createdObjects = make([]client.Object, 0)
		resourceManager := scoresources.NewScrapeCoordinatorResourceManager(
			k8sClient,
			k8sClient.Scheme(),
			OperatorManagerDeployment,
			util.ScrapeCoordinatorConfig{
				Images:             TestImages,
				OperatorNamespace:  operatorNamespace,
				NamePrefix:         ManagedResourceNamePrefixTest,
				CollectorComponent: "agent-collector",
			},
		)
		scrapeCoordinatorManager = NewScrapeCoordinatorManager(
			k8sClient,
			util.ExtraConfigDefaults,
			util.ExtraConfigDefaults,
			true,
			false,
			resourceManager,
		)
	})

	AfterEach(func() {
		DeleteAllOperatorConfigurationResources(ctx, k8sClient)
		createdObjects = DeleteAllCreatedObjects(ctx, k8sClient, createdObjects)
		Expect(k8sClient.DeleteAllOf(ctx, &corev1.ConfigMap{}, client.InNamespace(operatorNamespace))).To(Succeed())
	})

	createOperatorConfiguration := func(
		telemetryCollectionEnabled bool,
		prometheusCrdSupportEnabled bool,
	) *dash0v1alpha1.Dash0OperatorConfiguration {
		return CreateOperatorConfigurationResourceWithSpec(
			ctx,
			k8sClient,
			dash0v1alpha1.Dash0OperatorConfigurationSpec{
				TelemetryCollection:  dash0v1alpha1.TelemetryCollection{Enabled: new(telemetryCollectionEnabled)},
				PrometheusCrdSupport: dash0v1alpha1.PrometheusCrdSupport{Enabled: new(prometheusCrdSupportEnabled)},
			},
		)
	}

	ensureMonitoringResourceWithPrometheusScraping := func() {
		monitoringResource := tatest.EnsureMonitoringResourceWithPrometheusScrapingExistsAndIsAvailable(ctx, k8sClient)
		createdObjects = append(createdObjects, monitoringResource)
	}

	reconcile := func() {
		hasBeenReconciled, err := scrapeCoordinatorManager.ReconcileScrapeCoordinator(
			ctx,
			TriggeredByDash0ResourceReconcile,
		)
		Expect(err).ToNot(HaveOccurred())
		Expect(hasBeenReconciled).To(BeTrue())
	}

	It("should create the scrape coordinator if it is selected and enabled and a monitoring resource has Prometheus "+
		"scraping", func() {
		createOperatorConfiguration(true, true)
		ensureMonitoringResourceWithPrometheusScraping()

		reconcile()

		verifyScrapeCoordinatorResourcesExist(ctx)
		Expect(readCoordinatorConfig(ctx)).To(HaveKeyWithValue("prometheusCRs",
			HaveKeyWithValue("namespaces", ConsistOf(TestNamespaceName))))
	})

	DescribeTable("should not create the scrape coordinator",
		func(enabledViaHelm bool, createOperatorConfigurationResource bool, telemetryCollection bool, prometheusCrdSupport bool) {
			scrapeCoordinatorManager.enabledViaHelm = enabledViaHelm
			if createOperatorConfigurationResource {
				createOperatorConfiguration(telemetryCollection, prometheusCrdSupport)
			}
			ensureMonitoringResourceWithPrometheusScraping()

			reconcile()

			verifyScrapeCoordinatorResourcesDoNotExist(ctx)
		},
		Entry("if the scrape coordinator has not been selected via Helm", false, true, true, true),
		Entry("if the operator configuration is missing", true, false, false, false),
		Entry("if telemetry collection is disabled", true, true, false, true),
		Entry("if Prometheus CRD support is disabled", true, true, true, false),
	)

	It("should not create the scrape coordinator if no monitoring resource has Prometheus scraping enabled", func() {
		createOperatorConfiguration(true, true)
		monitoringResource := EnsureMonitoringResourceWithSpecExistsInNamespaceAndIsAvailable(
			ctx,
			k8sClient,
			dash0v1beta1.Dash0MonitoringSpec{
				PrometheusScraping: common.PrometheusScraping{Enabled: new(false)},
			},
			types.NamespacedName{Namespace: TestNamespaceName, Name: "dash0-monitoring-test-resource-1"},
		)
		createdObjects = append(createdObjects, monitoringResource)

		reconcile()

		verifyScrapeCoordinatorResourcesDoNotExist(ctx)
	})

	It("should delete the scrape coordinator if Prometheus CRD support is disabled", func() {
		operatorConfigurationResource := createOperatorConfiguration(true, true)
		ensureMonitoringResourceWithPrometheusScraping()
		reconcile()
		verifyScrapeCoordinatorResourcesExist(ctx)

		operatorConfigurationResource.Spec.PrometheusCrdSupport.Enabled = new(false)
		Expect(k8sClient.Update(ctx, operatorConfigurationResource)).To(Succeed())
		reconcile()

		verifyScrapeCoordinatorResourcesDoNotExist(ctx)
	})

	It("should delete the scrape coordinator if it is no longer selected via Helm", func() {
		createOperatorConfiguration(true, true)
		ensureMonitoringResourceWithPrometheusScraping()
		reconcile()
		verifyScrapeCoordinatorResourcesExist(ctx)

		scrapeCoordinatorManager.enabledViaHelm = false
		reconcile()

		verifyScrapeCoordinatorResourcesDoNotExist(ctx)
	})

	It("should delete the scrape coordinator if the operator configuration is deleted", func() {
		operatorConfigurationResource := createOperatorConfiguration(true, true)
		ensureMonitoringResourceWithPrometheusScraping()
		reconcile()
		verifyScrapeCoordinatorResourcesExist(ctx)

		Expect(k8sClient.Delete(ctx, operatorConfigurationResource)).To(Succeed())
		reconcile()

		verifyScrapeCoordinatorResourcesDoNotExist(ctx)
	})

	It("should add namespaces with Prometheus scraping to the configuration", func() {
		createOperatorConfiguration(true, true)
		ensureMonitoringResourceWithPrometheusScraping()
		reconcile()

		monitoringResource := tatest.EnsureMonitoringResourceWithPrometheusScrapingExistsInNamespaceAndIsAvailable(
			ctx,
			k8sClient,
			types.NamespacedName{Namespace: testNamespace1, Name: MonitoringResourceName},
		)
		createdObjects = append(createdObjects, monitoringResource)
		reconcile()

		Expect(readCoordinatorConfig(ctx)).To(HaveKeyWithValue("prometheusCRs",
			HaveKeyWithValue("namespaces", ConsistOf(TestNamespaceName, testNamespace1))))
	})

	It("should apply a changed extra config", func() {
		createOperatorConfiguration(true, true)
		ensureMonitoringResourceWithPrometheusScraping()
		reconcile()

		extraConfig := util.ExtraConfigDefaults
		extraConfig.ScrapeCoordinatorAllowInsecureAuthSecrets = true
		scrapeCoordinatorManager.UpdateExtraConfig(ctx, extraConfig, logger)

		Expect(readCoordinatorConfig(ctx)).To(HaveKeyWithValue("server",
			HaveKeyWithValue("allowInsecureAuthSecrets", true)))
	})
})

func expectedScrapeCoordinatorResources() map[client.Object]string {
	return map[client.Object]string{
		&corev1.ServiceAccount{}:     scoresources.ServiceAccountName(ManagedResourceNamePrefixTest),
		&corev1.ConfigMap{}:          scoresources.ConfigMapName(ManagedResourceNamePrefixTest),
		&rbacv1.ClusterRole{}:        scoresources.ClusterRoleName(ManagedResourceNamePrefixTest),
		&rbacv1.ClusterRoleBinding{}: scoresources.ClusterRoleBindingName(ManagedResourceNamePrefixTest),
		&corev1.Service{}:            scoresources.ServiceName(ManagedResourceNamePrefixTest),
		&appsv1.Deployment{}:         scoresources.DeploymentName(ManagedResourceNamePrefixTest),
	}
}

func namespaceFor(receiver client.Object) string {
	switch receiver.(type) {
	case *rbacv1.ClusterRole, *rbacv1.ClusterRoleBinding:
		return ""
	default:
		return operatorNamespace
	}
}

func verifyScrapeCoordinatorResourcesExist(ctx context.Context) {
	for receiver, name := range expectedScrapeCoordinatorResources() {
		VerifyResourceExists(ctx, k8sClient, namespaceFor(receiver), name, receiver)
	}
}

func verifyScrapeCoordinatorResourcesDoNotExist(ctx context.Context) {
	for receiver, name := range expectedScrapeCoordinatorResources() {
		VerifyResourceDoesNotExist(ctx, k8sClient, namespaceFor(receiver), name, receiver)
	}
}

func readCoordinatorConfig(ctx context.Context) map[string]any {
	configMap := &corev1.ConfigMap{}
	Expect(k8sClient.Get(ctx, client.ObjectKey{
		Namespace: operatorNamespace,
		Name:      scoresources.ConfigMapName(ManagedResourceNamePrefixTest),
	}, configMap)).To(Succeed())
	var config map[string]any
	Expect(yaml.Unmarshal([]byte(configMap.Data["config.yaml"]), &config)).To(Succeed())
	return config
}
