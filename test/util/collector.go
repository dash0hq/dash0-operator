// SPDX-FileCopyrightText: Copyright 2024 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package util

import (
	"context"
	"fmt"

	. "github.com/onsi/gomega"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	ExpectedConfigMapName = "unit-test-opentelemetry-collector-agent"
	ExpectedDaemonSetName = "unit-test-opentelemetry-collector-agent"
)

func VerifyCollectorResourcesExist(
	ctx context.Context,
	k8sClient client.Client,
	operatorNamespace string,
) {
	VerifyCollectorConfigMapExists(ctx, k8sClient, operatorNamespace)
	VerifyCollectorDaemonSetExists(ctx, k8sClient, operatorNamespace)
}

func VerifyCollectorConfigMapExists(
	ctx context.Context,
	k8sClient client.Client,
	operatorNamespace string,
) *corev1.ConfigMap {
	cm_ := verifyResourceExists(
		ctx,
		k8sClient,
		operatorNamespace,
		ExpectedConfigMapName,
		&corev1.ConfigMap{},
	)
	cm := cm_.(*corev1.ConfigMap)
	Expect(cm.Data).To(HaveKey("config.yaml"))

	verifyOwnerReference(cm)

	return cm
}

func VerifyCollectorDaemonSetExists(
	ctx context.Context,
	k8sClient client.Client,
	operatorNamespace string,
) *appsv1.DaemonSet {
	ds_ := verifyResourceExists(ctx, k8sClient, operatorNamespace, ExpectedDaemonSetName, &appsv1.DaemonSet{})
	ds := ds_.(*appsv1.DaemonSet)

	// arbitrarily checking a couple of settings for the daemon set
	containers := ds.Spec.Template.Spec.Containers
	Expect(containers).To(HaveLen(2))
	ports := containers[0].Ports
	Expect(ports[0].ContainerPort).To(Equal(int32(4317)))
	Expect(ports[1].ContainerPort).To(Equal(int32(4318)))

	verifyOwnerReference(ds)

	return ds
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

func VerifyCollectorResourcesDoNotExist(
	ctx context.Context,
	k8sClient client.Client,
	operatorNamespace string,
) {
	verifyCollectorConfigMapDoesNotExist(ctx, k8sClient, operatorNamespace)
	verifyCollectorDaemonSetDoesNotExist(ctx, k8sClient, operatorNamespace)
}

func verifyCollectorConfigMapDoesNotExist(
	ctx context.Context,
	k8sClient client.Client,
	operatorNamespace string,
) {
	verifyResourceDoesNotExist(
		ctx,
		k8sClient,
		operatorNamespace,
		ExpectedConfigMapName,
		&corev1.ConfigMap{},
		"config map",
	)
}

func verifyCollectorDaemonSetDoesNotExist(
	ctx context.Context,
	k8sClient client.Client,
	operatorNamespace string,
) {
	verifyResourceDoesNotExist(
		ctx,
		k8sClient,
		operatorNamespace,
		ExpectedDaemonSetName,
		&appsv1.DaemonSet{},
		"daemon set",
	)
}

func verifyResourceDoesNotExist(
	ctx context.Context,
	k8sClient client.Client,
	namespace string,
	expectedName string,
	receiver client.Object,
	resourceLabel string,
) {
	key := client.ObjectKey{Name: expectedName, Namespace: namespace}
	err := k8sClient.Get(ctx, key, receiver)
	Expect(err).To(HaveOccurred(), fmt.Sprintf("the %s still exists although it should have been deleted", resourceLabel))
	Expect(apierrors.IsNotFound(err)).To(BeTrue(),
		fmt.Sprintf("loading the %s failed with an unexpected error: %v", resourceLabel, err))
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
