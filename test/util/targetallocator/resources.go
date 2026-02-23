// SPDX-FileCopyrightText: Copyright 2025 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package targetallocator

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
	"github.com/dash0hq/dash0-operator/internal/util"
	testutil "github.com/dash0hq/dash0-operator/test/util"

	. "github.com/onsi/gomega"
)

type targetAllocatorExpectedResource struct {
	name          string
	clusterScoped bool
	receiver      client.Object
}

//nolint:lll
var (
	ExpectedTargetAllocatorServiceAccountName     = fmt.Sprintf("%s-opentelemetry-target-allocator-sa", testutil.TargetAllocatorPrefixTest)
	ExpectedTargetAllocatorClusterRoleName        = fmt.Sprintf("%s-opentelemetry-target-allocator-cr", testutil.TargetAllocatorPrefixTest)
	ExpectedTargetAllocatorClusterRoleBindingName = fmt.Sprintf("%s-opentelemetry-target-allocator-crb", testutil.TargetAllocatorPrefixTest)
	ExpectedTargetAllocatorConfigMapName          = fmt.Sprintf("%s-opentelemetry-target-allocator-cm", testutil.TargetAllocatorPrefixTest)
	ExpectedTargetAllocatorDeploymentName         = fmt.Sprintf("%s-opentelemetry-target-allocator-deployment", testutil.TargetAllocatorPrefixTest)
	ExpectedTargetAllocatorServiceName            = fmt.Sprintf("%s-opentelemetry-target-allocator-service", testutil.TargetAllocatorPrefixTest)
)

var (
	expectedTargetAllocatorConfigMap = targetAllocatorExpectedResource{
		name:          ExpectedTargetAllocatorConfigMapName,
		clusterScoped: false,
		receiver:      &corev1.ConfigMap{},
	}
	expectedTargetAllocatorServiceAccount = targetAllocatorExpectedResource{
		name:          ExpectedTargetAllocatorServiceAccountName,
		clusterScoped: false,
		receiver:      &corev1.ServiceAccount{},
	}
	expectedTargetAllocatorClusterRole = targetAllocatorExpectedResource{
		name:          ExpectedTargetAllocatorClusterRoleName,
		clusterScoped: true,
		receiver:      &rbacv1.ClusterRole{},
	}
	expectedTargetAllocatorClusterRoleBinding = targetAllocatorExpectedResource{
		name:          ExpectedTargetAllocatorClusterRoleBindingName,
		clusterScoped: true,
		receiver:      &rbacv1.ClusterRoleBinding{},
	}
	expectedTargetAllocatorService = targetAllocatorExpectedResource{
		name:          ExpectedTargetAllocatorServiceName,
		clusterScoped: false,
		receiver:      &corev1.Service{},
	}
	expectedTargetAllocatorDeployment = targetAllocatorExpectedResource{
		name:          ExpectedTargetAllocatorDeploymentName,
		clusterScoped: false,
		receiver:      &appsv1.Deployment{},
	}

	AllExpectedTargetAllocatorResources = []targetAllocatorExpectedResource{
		expectedTargetAllocatorConfigMap,
		expectedTargetAllocatorServiceAccount,
		expectedTargetAllocatorClusterRole,
		expectedTargetAllocatorClusterRoleBinding,
		expectedTargetAllocatorService,
		expectedTargetAllocatorDeployment,
	}
)

func EnsureMonitoringResourceWithPrometheusScrapingExistsAndIsAvailable(
	ctx context.Context,
	k8sClient client.Client,
) *dash0v1beta1.Dash0Monitoring {
	return EnsureMonitoringResourceWithPrometheusScrapingExistsInNamespaceAndIsAvailable(
		ctx,
		k8sClient,
		testutil.MonitoringResourceQualifiedName,
	)
}

func EnsureMonitoringResourceWithPrometheusScrapingExistsInNamespaceAndIsAvailable(
	ctx context.Context,
	k8sClient client.Client,
	namespacedName types.NamespacedName,
) *dash0v1beta1.Dash0Monitoring {
	spec := dash0v1beta1.Dash0MonitoringSpec{
		PrometheusScraping: common.PrometheusScraping{
			Enabled: new(true),
		},
	}
	return testutil.EnsureMonitoringResourceWithSpecExistsInNamespaceAndIsAvailable(
		ctx,
		k8sClient,
		spec,
		namespacedName,
	)
}

func CreateDefaultOperatorConfigurationResourceWithPrometheusCrdSupport(
	ctx context.Context,
	k8sClient client.Client,
) *dash0v1alpha1.Dash0OperatorConfiguration {
	return testutil.CreateOperatorConfigurationResourceWithSpec(
		ctx,
		k8sClient,
		dash0v1alpha1.Dash0OperatorConfigurationSpec{
			TelemetryCollection: dash0v1alpha1.TelemetryCollection{
				Enabled: new(true),
			},
			PrometheusCrdSupport: dash0v1alpha1.PrometheusCrdSupport{
				Enabled: new(true),
			},
		},
	)
}

func VerifyTargetAllocatorResources(
	ctx context.Context,
	k8sClient client.Client,
	operatorNamespace string,
) {
	VerifyAllTargetAllocatorResourcesExist(ctx, k8sClient, operatorNamespace)

	VerifyTargetAllocatorConfigMap(ctx, k8sClient, operatorNamespace)
	VerifyTargetAllocatorDeployment(ctx, k8sClient, operatorNamespace)
}

func VerifyAllTargetAllocatorResourcesExist(
	ctx context.Context,
	k8sClient client.Client,
	operatorNamespace string,
) {
	for _, expectedRes := range AllExpectedTargetAllocatorResources {
		expectedNamespace := operatorNamespace
		if expectedRes.clusterScoped {
			expectedNamespace = ""
		}
		actualResource := VerifyExpectedTargetAllocatorResourceExists(
			ctx,
			k8sClient,
			expectedNamespace,
			expectedRes,
		)
		if !expectedRes.clusterScoped {
			verifyOwnerReference(actualResource)
		}
	}
}

func VerifyExpectedTargetAllocatorResourceExists(
	ctx context.Context,
	k8sClient client.Client,
	namespace string,
	expectedResource targetAllocatorExpectedResource,
) client.Object {
	return testutil.VerifyResourceExists(
		ctx,
		k8sClient,
		namespace,
		expectedResource.name,
		expectedResource.receiver,
	)
}

func VerifyTargetAllocatorConfigMap(
	ctx context.Context,
	k8sClient client.Client,
	operatorNamespace string,
) *corev1.ConfigMap {
	cm_ := testutil.VerifyResourceExists(
		ctx,
		k8sClient,
		operatorNamespace,
		ExpectedTargetAllocatorConfigMapName,
		&corev1.ConfigMap{},
	)
	cm := cm_.(*corev1.ConfigMap)
	Expect(cm.Data).To(HaveLen(1))
	Expect(cm.Data).To(HaveKey("targetallocator.yaml"))
	config := cm.Data["targetallocator.yaml"]
	Expect(config).To(ContainSubstring("allocation_strategy: per-node"))
	Expect(config).To(ContainSubstring("prometheus_cr:"))
	Expect(config).To(ContainSubstring("enabled: true"))
	return cm
}

func VerifyTargetAllocatorConfigMapContainsNamespaces(
	ctx context.Context,
	k8sClient client.Client,
	operatorNamespace string,
	expectedNamespaces []string,
) {
	cm := VerifyTargetAllocatorConfigMap(ctx, k8sClient, operatorNamespace)
	config := cm.Data["targetallocator.yaml"]

	var configMap map[string]any
	err := yaml.Unmarshal([]byte(config), &configMap)
	Expect(err).ToNot(HaveOccurred())

	prometheusCr, ok := configMap["prometheus_cr"].(map[string]any)
	Expect(ok).To(BeTrue(), "prometheus_cr should be a map")

	if len(expectedNamespaces) > 0 {
		allowNamespacesRaw, exists := prometheusCr["allow_namespaces"]
		Expect(exists).To(BeTrue(), "allow_namespaces should exist when namespaces are expected")

		allowNamespaces, ok := allowNamespacesRaw.([]any)
		Expect(ok).To(BeTrue(), "allow_namespaces should be an array")

		actualNamespaces := make([]string, len(allowNamespaces))
		for i, ns := range allowNamespaces {
			actualNamespaces[i] = ns.(string)
		}

		Expect(actualNamespaces).To(ConsistOf(expectedNamespaces))
	}
}

func VerifyTargetAllocatorDeployment(
	ctx context.Context,
	k8sClient client.Client,
	operatorNamespace string,
) *appsv1.Deployment {
	deployment_ := testutil.VerifyResourceExists(
		ctx,
		k8sClient,
		operatorNamespace,
		ExpectedTargetAllocatorDeploymentName,
		&appsv1.Deployment{},
	)
	deployment := deployment_.(*appsv1.Deployment)
	Expect(deployment.Spec.Template.Spec.Containers).To(HaveLen(1))
	container := deployment.Spec.Template.Spec.Containers[0]
	Expect(container.Name).To(Equal("targetallocator"))
	return deployment
}

func VerifyTargetAllocatorDeploymentExists(
	ctx context.Context,
	k8sClient client.Client,
	operatorNamespace string,
) *appsv1.Deployment {
	return VerifyTargetAllocatorDeployment(ctx, k8sClient, operatorNamespace)
}

func VerifyTargetAllocatorResourcesDoNotExist(
	ctx context.Context,
	k8sClient client.Client,
	operatorNamespace string,
) {
	for _, expectedRes := range AllExpectedTargetAllocatorResources {
		expectedNamespace := operatorNamespace
		if expectedRes.clusterScoped {
			expectedNamespace = ""
		}
		VerifyExpectedTargetAllocatorResourceDoesNotExist(
			ctx,
			k8sClient,
			expectedNamespace,
			expectedRes,
		)
	}
}

func VerifyExpectedTargetAllocatorResourceDoesNotExist(
	ctx context.Context,
	k8sClient client.Client,
	namespace string,
	expectedResource targetAllocatorExpectedResource,
) {
	testutil.VerifyResourceDoesNotExist(
		ctx,
		k8sClient,
		namespace,
		expectedResource.name,
		expectedResource.receiver,
	)
}

func verifyOwnerReference(object client.Object) {
	ownerReferences := object.GetOwnerReferences()
	Expect(ownerReferences).To(HaveLen(1))
	ownerReference := ownerReferences[0]
	Expect(ownerReference.APIVersion).To(Equal(util.K8sApiVersionAppsV1))
	Expect(ownerReference.Kind).To(Equal("Deployment"))
	Expect(ownerReference.Name).To(Equal(testutil.OperatorManagerDeployment.Name))
	Expect(*ownerReference.BlockOwnerDeletion).To(BeTrue())
	Expect(*ownerReference.Controller).To(BeTrue())
}
