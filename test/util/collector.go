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
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"sigs.k8s.io/controller-runtime/pkg/client"

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
	ExpectedDaemonSetServiceAccountName              = fmt.Sprintf("%s-opentelemetry-collector-sa", NamePrefix)
	ExpectedDaemonSetCollectorConfigMapName          = fmt.Sprintf("%s-opentelemetry-collector-agent-cm", NamePrefix)
	ExpectedDaemonSetFilelogOffsetSynchConfigMapName = fmt.Sprintf("%s-filelogoffsets-cm", NamePrefix)
	ExpectedDaemonSetClusterRoleName                 = fmt.Sprintf("%s-opentelemetry-collector-cr", NamePrefix)
	ExpectedDaemonSetClusterRoleBinding              = fmt.Sprintf("%s-opentelemetry-collector-crb", NamePrefix)
	ExpectedDaemonSetRoleName                        = fmt.Sprintf("%s-opentelemetry-collector-role", NamePrefix)
	ExpectedDaemonSetRoleBindingName                 = fmt.Sprintf("%s-opentelemetry-collector-rolebinding", NamePrefix)
	ExpectedDaemonSetServiceName                     = fmt.Sprintf("%s-opentelemetry-collector-service", NamePrefix)
	ExpectedDaemonSetName                            = fmt.Sprintf(
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

	allExpectedResources = []expectedResource{
		{name: ExpectedDaemonSetServiceAccountName, receiver: &corev1.ServiceAccount{}},
		expectedResourceDaemonSetConfigMap,
		{name: ExpectedDaemonSetFilelogOffsetSynchConfigMapName, receiver: &corev1.ConfigMap{}},
		{name: ExpectedDaemonSetClusterRoleName, clusterScoped: true, receiver: &rbacv1.ClusterRole{}},
		{name: ExpectedDaemonSetClusterRoleBinding, clusterScoped: true, receiver: &rbacv1.ClusterRoleBinding{}},
		{name: ExpectedDaemonSetRoleName, receiver: &rbacv1.Role{}},
		{name: ExpectedDaemonSetRoleBindingName, receiver: &rbacv1.RoleBinding{}},
		{name: ExpectedDaemonSetServiceName, receiver: &corev1.Service{}},
		expectedResourceDaemonSet,
		{name: ExpectedDeploymentServiceAccountName, receiver: &corev1.ServiceAccount{}},
		{name: ExpectedDeploymentClusterRoleName, clusterScoped: true, receiver: &rbacv1.ClusterRole{}},
		{name: ExpectedDeploymentClusterRoleBindingName, clusterScoped: true, receiver: &rbacv1.ClusterRoleBinding{}},
		expectedResourceDeploymentConfigMap,
		expectedResourceDeployment,
	}
)

func VerifyCollectorResources(
	ctx context.Context,
	k8sClient client.Client,
	operatorNamespace string,
) {
	// verify that all expected resources exist and have the expected owner reference
	VerifyAllResourcesExist(ctx, k8sClient, operatorNamespace)

	// verify a few arbitrary resource in more detail
	VerifyDaemonSetCollectorConfigMap(ctx, k8sClient, operatorNamespace)
	VerifyCollectorDaemonSet(ctx, k8sClient, operatorNamespace)
	VerifyDeploymentCollectorConfigMap(ctx, k8sClient, operatorNamespace)
	VerifyCollectorDeployment(ctx, k8sClient, operatorNamespace)
}

func VerifyAllResourcesExist(
	ctx context.Context,
	k8sClient client.Client,
	operatorNamespace string,
) {
	for _, expectedRes := range allExpectedResources {
		expectedNamespace := operatorNamespace
		if expectedRes.clusterScoped {
			expectedNamespace = ""
		}
		actualResource := verifyResourceExists(
			ctx,
			k8sClient,
			expectedNamespace,
			expectedRes.name,
			expectedRes.receiver,
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
) {
	cm_ := verifyResourceExists(
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
	Expect(config).To(ContainSubstring("endpoint: \"endpoint.dash0.com:4317\""))
	Expect(config).To(ContainSubstring("\"Authorization\": \"Bearer ${env:AUTH_TOKEN}\""))
}

func VerifyCollectorDaemonSet(
	ctx context.Context,
	k8sClient client.Client,
	operatorNamespace string,
) *appsv1.DaemonSet {
	ds_ := verifyResourceExists(
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

	return ds
}

func VerifyDeploymentCollectorConfigMap(
	ctx context.Context,
	k8sClient client.Client,
	operatorNamespace string,
) {
	cm_ := verifyResourceExists(
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
	Expect(config).To(ContainSubstring("\"Authorization\": \"Bearer ${env:AUTH_TOKEN}\""))
}

func VerifyCollectorDeployment(
	ctx context.Context,
	k8sClient client.Client,
	operatorNamespace string,
) {
	deployment_ := verifyResourceExists(
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

func verifyResourceExists(
	ctx context.Context,
	k8sClient client.Client,
	namespace string,
	expectedName string,
	receiver client.Object,
) client.Object {
	key := client.ObjectKey{Name: expectedName, Namespace: namespace}
	err := k8sClient.Get(ctx, key, receiver)
	Expect(err).ToNot(HaveOccurred())
	Expect(receiver).NotTo(BeNil())
	return receiver
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
	return verifyResourceExists(
		ctx,
		k8sClient,
		expectedNamespace,
		expectedRes.name,
		expectedRes.receiver,
	)
}

func VerifyCollectorResourcesDoNotExist(
	ctx context.Context,
	k8sClient client.Client,
	operatorNamespace string,
) {
	for _, expectedRes := range allExpectedResources {
		expectedNamespace := operatorNamespace
		if expectedRes.clusterScoped {
			expectedNamespace = ""
		}
		VerifyResourceDoesNotExist(
			ctx,
			k8sClient,
			expectedNamespace,
			expectedRes.name,
			expectedRes.receiver,
		)
	}
}

func VerifyResourceDoesNotExist(
	ctx context.Context,
	k8sClient client.Client,
	namespace string,
	expectedName string,
	receiver client.Object,
) {
	key := client.ObjectKey{Name: expectedName, Namespace: namespace}
	err := k8sClient.Get(ctx, key, receiver)
	Expect(err).To(
		HaveOccurred(),
		fmt.Sprintf("the resource %s still exists although it should have been deleted", expectedName),
	)
	Expect(apierrors.IsNotFound(err)).To(BeTrue(),
		fmt.Sprintf("attempting to load the resource %s failed with an unexpected error: %v", expectedName, err))
}

func verifyOwnerReference(object client.Object) {
	ownerReferences := object.GetOwnerReferences()
	Expect(ownerReferences).To(HaveLen(1))
	ownerReference := ownerReferences[0]
	Expect(ownerReference.APIVersion).To(Equal("apps/v1"))
	Expect(ownerReference.Kind).To(Equal("Deployment"))
	Expect(ownerReference.Name).To(Equal(DeploymentSelfReference.Name))
	Expect(*ownerReference.BlockOwnerDeletion).To(BeTrue())
	Expect(*ownerReference.Controller).To(BeTrue())
}
