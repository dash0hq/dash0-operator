// SPDX-FileCopyrightText: Copyright 2024 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package util

import (
	"context"
	"fmt"
	"slices"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/dash0hq/dash0-operator/internal/util"
	. "github.com/onsi/gomega"
)

type expectedResource struct {
	name          string
	clusterScoped bool
	receiver      client.Object
}

const (
	NamePrefix = "unit-test"
)

var (
	ExpectedDaemonSetServiceAccountName             = fmt.Sprintf("%s-opentelemetry-collector-sa", NamePrefix)
	ExpectedDaemonSetCollectorConfigMapName         = fmt.Sprintf("%s-opentelemetry-collector-agent-cm", NamePrefix)
	ExpectedDaemonSetFilelogOffsetSyncConfigMapName = fmt.Sprintf("%s-filelogoffsets-cm", NamePrefix)
	ExpectedDaemonSetClusterRoleName                = fmt.Sprintf("%s-opentelemetry-collector-cr", NamePrefix)
	ExpectedDaemonSetClusterRoleBinding             = fmt.Sprintf("%s-opentelemetry-collector-crb", NamePrefix)
	ExpectedDaemonSetRoleName                       = fmt.Sprintf("%s-opentelemetry-collector-role", NamePrefix)
	ExpectedDaemonSetRoleBindingName                = fmt.Sprintf("%s-opentelemetry-collector-rolebinding", NamePrefix)
	ExpectedDaemonSetServiceName                    = fmt.Sprintf("%s-opentelemetry-collector-service", NamePrefix)
	ExpectedDaemonSetName                           = fmt.Sprintf(
		"%s-opentelemetry-collector-agent-daemonset",
		NamePrefix,
	)
	ExpectedDeploymentServiceAccountName     = fmt.Sprintf("%s-cluster-metrics-collector-sa", NamePrefix)
	ExpectedDeploymentClusterRoleName        = fmt.Sprintf("%s-cluster-metrics-collector-cr", NamePrefix)
	ExpectedDeploymentClusterRoleBindingName = fmt.Sprintf("%s-cluster-metrics-collector-crb", NamePrefix)
	ExpectedDeploymentCollectorConfigMapName = fmt.Sprintf("%s-cluster-metrics-collector-cm", NamePrefix)
	ExpectedDeploymentName                   = fmt.Sprintf("%s-cluster-metrics-collector-deployment", NamePrefix)

	expectedResourceDaemonSetConfigMap = expectedResource{
		name:     ExpectedDaemonSetCollectorConfigMapName,
		receiver: &corev1.ConfigMap{},
	}
	expectedResourceDaemonSet = expectedResource{
		name:     ExpectedDaemonSetName,
		receiver: &appsv1.DaemonSet{},
	}
	expectedResourceDeploymentConfigMap = expectedResource{
		name:     ExpectedDeploymentCollectorConfigMapName,
		receiver: &corev1.ConfigMap{},
	}
	expectedResourceDeployment = expectedResource{
		name:     ExpectedDeploymentName,
		receiver: &appsv1.Deployment{},
	}

	AllClusterMetricsRelatedResources = []expectedResource{
		{name: ExpectedDeploymentServiceAccountName, receiver: &corev1.ServiceAccount{}},
		{name: ExpectedDeploymentClusterRoleName, clusterScoped: true, receiver: &rbacv1.ClusterRole{}},
		{name: ExpectedDeploymentClusterRoleBindingName, clusterScoped: true, receiver: &rbacv1.ClusterRoleBinding{}},
		expectedResourceDeploymentConfigMap,
		expectedResourceDeployment,
	}
	AllDaemonSetRelatedResources = []expectedResource{
		{name: ExpectedDaemonSetServiceAccountName, receiver: &corev1.ServiceAccount{}},
		expectedResourceDaemonSetConfigMap,
		{name: ExpectedDaemonSetFilelogOffsetSyncConfigMapName, receiver: &corev1.ConfigMap{}},
		{name: ExpectedDaemonSetClusterRoleName, clusterScoped: true, receiver: &rbacv1.ClusterRole{}},
		{name: ExpectedDaemonSetClusterRoleBinding, clusterScoped: true, receiver: &rbacv1.ClusterRoleBinding{}},
		{name: ExpectedDaemonSetRoleName, receiver: &rbacv1.Role{}},
		{name: ExpectedDaemonSetRoleBindingName, receiver: &rbacv1.RoleBinding{}},
		{name: ExpectedDaemonSetServiceName, receiver: &corev1.Service{}},
		expectedResourceDaemonSet,
	}
	AllDeploymentRelatedResources = []expectedResource{
		{name: ExpectedDeploymentServiceAccountName, receiver: &corev1.ServiceAccount{}},
		{name: ExpectedDeploymentClusterRoleName, clusterScoped: true, receiver: &rbacv1.ClusterRole{}},
		{name: ExpectedDeploymentClusterRoleBindingName, clusterScoped: true, receiver: &rbacv1.ClusterRoleBinding{}},
		expectedResourceDeploymentConfigMap,
		expectedResourceDeployment,
	}
	AllExpectedResources = append(
		AllDaemonSetRelatedResources,
		AllDeploymentRelatedResources...,
	)
)

func VerifyCollectorResources(
	ctx context.Context,
	k8sClient client.Client,
	operatorNamespace string,
	dash0Endpoint string,
	authorizationEnvVar string,
	authorizationToken string,
) {
	// verify that all expected resources exist and have the expected owner reference
	VerifyAllResourcesExist(ctx, k8sClient, operatorNamespace)

	// verify a few arbitrary resource in more detail
	VerifyDaemonSetCollectorConfigMap(ctx, k8sClient, operatorNamespace, dash0Endpoint, authorizationEnvVar)
	VerifyCollectorDaemonSet(ctx, k8sClient, operatorNamespace, authorizationEnvVar, authorizationToken)
	VerifyDeploymentCollectorConfigMap(ctx, k8sClient, operatorNamespace, authorizationEnvVar)
	VerifyCollectorDeployment(ctx, k8sClient, operatorNamespace)
}

func VerifyAllResourcesExist(
	ctx context.Context,
	k8sClient client.Client,
	operatorNamespace string,
) {
	for _, expectedRes := range AllExpectedResources {
		expectedNamespace := operatorNamespace
		if expectedRes.clusterScoped {
			expectedNamespace = ""
		}
		actualResource := VerifyExpectedResourceExists(
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

func VerifyDaemonSetCollectorConfigMap(
	ctx context.Context,
	k8sClient client.Client,
	operatorNamespace string,
	dash0Endpoint string,
	authorizationEnvVar string,
) {
	cm_ := VerifyResourceExists(
		ctx,
		k8sClient,
		operatorNamespace,
		ExpectedDaemonSetCollectorConfigMapName,
		&corev1.ConfigMap{},
	)
	cm := cm_.(*corev1.ConfigMap)
	Expect(cm.Data).To(HaveLen(1))
	Expect(cm.Data).To(HaveKey("config.yaml"))
	config := cm.Data["config.yaml"]
	Expect(config).To(ContainSubstring(fmt.Sprintf("endpoint: \"%s\"", dash0Endpoint)))
	Expect(config).To(ContainSubstring(fmt.Sprintf("\"Authorization\": \"Bearer ${env:%s}\"", authorizationEnvVar)))
}

func VerifyCollectorDaemonSet(
	ctx context.Context,
	k8sClient client.Client,
	operatorNamespace string,
	authorizationEnvVar string,
	authorizationToken string,

) *appsv1.DaemonSet {
	ds_ := VerifyResourceExists(
		ctx,
		k8sClient,
		operatorNamespace,
		ExpectedDaemonSetName,
		&appsv1.DaemonSet{},
	)
	ds := ds_.(*appsv1.DaemonSet)

	// arbitrarily checking a couple of settings for the daemon set
	initContainers := ds.Spec.Template.Spec.InitContainers
	Expect(initContainers).To(HaveLen(1))
	containers := ds.Spec.Template.Spec.Containers
	Expect(containers).To(HaveLen(3))

	collectorContainerIdx := slices.IndexFunc(containers, func(c corev1.Container) bool {
		return c.Name == "opentelemetry-collector"
	})
	collectorContainer := containers[collectorContainerIdx]
	Expect(collectorContainer.Image).To(Equal(CollectorImageTest))
	ports := collectorContainer.Ports
	Expect(ports).To(HaveLen(2))
	Expect(ports[0].ContainerPort).To(Equal(int32(4317)))
	Expect(ports[1].ContainerPort).To(Equal(int32(4318)))

	Expect(collectorContainer.Env).To(ContainElement(MatchEnvVar(authorizationEnvVar, authorizationToken)))

	return ds
}

func VerifyDeploymentCollectorConfigMap(
	ctx context.Context,
	k8sClient client.Client,
	operatorNamespace string,
	authorizationEnvVar string,
) {
	cm_ := VerifyResourceExists(
		ctx,
		k8sClient,
		operatorNamespace,
		ExpectedDeploymentCollectorConfigMapName,
		&corev1.ConfigMap{},
	)
	cm := cm_.(*corev1.ConfigMap)
	Expect(cm.Data).To(HaveLen(1))
	Expect(cm.Data).To(HaveKey("config.yaml"))
	config := cm.Data["config.yaml"]
	Expect(config).To(ContainSubstring("endpoint: \"endpoint.dash0.com:4317\""))
	Expect(config).To(ContainSubstring(fmt.Sprintf("\"Authorization\": \"Bearer ${env:%s}\"", authorizationEnvVar)))
}

func VerifyCollectorDeployment(
	ctx context.Context,
	k8sClient client.Client,
	operatorNamespace string,
) {
	deployment_ := VerifyResourceExists(
		ctx,
		k8sClient,
		operatorNamespace,
		ExpectedDeploymentName,
		&appsv1.Deployment{},
	)
	deployment := deployment_.(*appsv1.Deployment)

	// arbitrarily check a couple of settings for the deployment
	Expect(*deployment.Spec.Replicas).To(Equal(int32(1)))
	containers := deployment.Spec.Template.Spec.Containers
	Expect(containers).To(HaveLen(2))
	collectorContainer := containers[0]
	ports := collectorContainer.Ports
	Expect(ports).To(HaveLen(0))
}

func GetOTelColDaemonSetConfigMap(
	ctx context.Context,
	k8sClient client.Client,
	operatorNamespace string,
) *corev1.ConfigMap {
	return getOTelColResource(ctx, k8sClient, operatorNamespace, expectedResourceDaemonSetConfigMap).(*corev1.ConfigMap)
}

func GetOTelColDaemonSet(
	ctx context.Context,
	k8sClient client.Client,
	operatorNamespace string,
) *appsv1.DaemonSet {
	return getOTelColResource(ctx, k8sClient, operatorNamespace, expectedResourceDaemonSet).(*appsv1.DaemonSet)
}

func GetOTelColDeploymentConfigMap(
	ctx context.Context,
	k8sClient client.Client,
	operatorNamespace string,
) *corev1.ConfigMap {
	return getOTelColResource(
		ctx,
		k8sClient,
		operatorNamespace,
		expectedResourceDeploymentConfigMap,
	).(*corev1.ConfigMap)
}

func GetOTelColDeployment(
	ctx context.Context,
	k8sClient client.Client,
	operatorNamespace string,
) *appsv1.Deployment {
	return getOTelColResource(
		ctx,
		k8sClient,
		operatorNamespace,
		expectedResourceDeployment,
	).(*appsv1.Deployment)
}

func getOTelColResource(
	ctx context.Context,
	k8sClient client.Client,
	operatorNamespace string,
	expectedRes expectedResource,
) client.Object {
	expectedNamespace := operatorNamespace
	if expectedRes.clusterScoped {
		expectedNamespace = ""
	}
	return VerifyExpectedResourceExists(
		ctx,
		k8sClient,
		expectedNamespace,
		expectedRes,
	)
}

func VerifyCollectorResourcesDoNotExist(
	ctx context.Context,
	k8sClient client.Client,
	operatorNamespace string,
) {
	for _, expectedRes := range AllExpectedResources {
		expectedNamespace := operatorNamespace
		if expectedRes.clusterScoped {
			expectedNamespace = ""
		}
		VerifyExpectedResourceDoesNotExist(
			ctx,
			k8sClient,
			expectedNamespace,
			expectedRes,
		)
	}
}

func verifyOwnerReference(object client.Object) {
	ownerReferences := object.GetOwnerReferences()
	Expect(ownerReferences).To(HaveLen(1))
	ownerReference := ownerReferences[0]
	Expect(ownerReference.APIVersion).To(Equal(util.K8sApiVersionAppsV1))
	Expect(ownerReference.Kind).To(Equal("Deployment"))
	Expect(ownerReference.Name).To(Equal(OperatorManagerDeployment.Name))
	Expect(*ownerReference.BlockOwnerDeletion).To(BeTrue())
	Expect(*ownerReference.Controller).To(BeTrue())
}
