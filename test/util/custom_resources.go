// SPDX-FileCopyrightText: Copyright 2024 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package util

import (
	"context"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	dash0v1alpha1 "github.com/dash0hq/dash0-operator/api/dash0/v1alpha1"
)

const (
	Dash0CustomResourceName = "dash0-test-resource"
)

var (
	Dash0CustomResourceQualifiedName = types.NamespacedName{
		Namespace: TestNamespaceName,
		Name:      Dash0CustomResourceName,
	}
)

func EnsureDash0CustomResourceExists(
	ctx context.Context,
	k8sClient client.Client,
) *dash0v1alpha1.Dash0 {
	return EnsureDash0CustomResourceExistsWithNamespacedName(
		ctx,
		k8sClient,
		Dash0CustomResourceQualifiedName,
	)
}

func EnsureDash0CustomResourceExistsWithNamespacedName(
	ctx context.Context,
	k8sClient client.Client,
	namespacesName types.NamespacedName,
) *dash0v1alpha1.Dash0 {
	By("creating the Dash0 custom resource")
	object := EnsureKubernetesObjectExists(
		ctx,
		k8sClient,
		Dash0CustomResourceQualifiedName,
		&dash0v1alpha1.Dash0{},
		&dash0v1alpha1.Dash0{
			ObjectMeta: metav1.ObjectMeta{
				Name:      namespacesName.Name,
				Namespace: namespacesName.Namespace,
			},
			Spec: dash0v1alpha1.Dash0Spec{
				IngressEndpoint:    "ingress.endpoint.dash0.com:4317",
				AuthorizationToken: "authorization-token",
				SecretRef:          "secret-ref",
			},
		},
	)
	return object.(*dash0v1alpha1.Dash0)
}

func CreateDash0CustomResource(
	ctx context.Context,
	k8sClient client.Client,
	dash0CustomResourceName types.NamespacedName,
) client.Object {
	dash0CustomResource := &dash0v1alpha1.Dash0{
		ObjectMeta: metav1.ObjectMeta{
			Name:      dash0CustomResourceName.Name,
			Namespace: dash0CustomResourceName.Namespace,
		},
		Spec: dash0v1alpha1.Dash0Spec{
			IngressEndpoint:    "ingress.endpoint.dash0.com:4317",
			AuthorizationToken: "authorization-token",
			SecretRef:          "secret-ref",
		},
	}
	Expect(k8sClient.Create(ctx, dash0CustomResource)).To(Succeed())
	return dash0CustomResource
}

func EnsureDash0CustomResourceExistsAndIsAvailable(
	ctx context.Context,
	k8sClient client.Client,
) *dash0v1alpha1.Dash0 {
	return EnsureDash0CustomResourceExistsAndIsAvailableInNamespace(ctx, k8sClient, Dash0CustomResourceQualifiedName)
}

func EnsureDash0CustomResourceExistsAndIsAvailableInNamespace(
	ctx context.Context,
	k8sClient client.Client,
	namespacedName types.NamespacedName,
) *dash0v1alpha1.Dash0 {
	dash0CustomResource := EnsureDash0CustomResourceExistsWithNamespacedName(
		ctx,
		k8sClient,
		namespacedName,
	)
	dash0CustomResource.EnsureResourceIsMarkedAsAvailable()
	Expect(k8sClient.Status().Update(ctx, dash0CustomResource)).To(Succeed())
	return dash0CustomResource
}

func EnsureDash0CustomResourceExistsAndIsDegraded(
	ctx context.Context,
	k8sClient client.Client,
) *dash0v1alpha1.Dash0 {
	dash0CustomResource := EnsureDash0CustomResourceExists(ctx, k8sClient)
	dash0CustomResource.EnsureResourceIsMarkedAsDegraded(
		"TestReasonForDegradation",
		"This resource is degraded.",
	)
	Expect(k8sClient.Status().Update(ctx, dash0CustomResource)).To(Succeed())
	return dash0CustomResource
}

func LoadDash0CustomResourceByNameIfItExists(
	ctx context.Context,
	k8sClient client.Client,
	g Gomega,
	dash0CustomResourceName types.NamespacedName,
) *dash0v1alpha1.Dash0 {
	return LoadDash0CustomResourceByName(ctx, k8sClient, g, dash0CustomResourceName, false)
}

func LoadDash0CustomResourceOrFail(ctx context.Context, k8sClient client.Client, g Gomega) *dash0v1alpha1.Dash0 {
	return LoadDash0CustomResourceByNameOrFail(ctx, k8sClient, g, Dash0CustomResourceQualifiedName)
}

func LoadDash0CustomResourceByNameOrFail(
	ctx context.Context,
	k8sClient client.Client,
	g Gomega,
	dash0CustomResourceName types.NamespacedName,
) *dash0v1alpha1.Dash0 {
	return LoadDash0CustomResourceByName(ctx, k8sClient, g, dash0CustomResourceName, true)
}

func LoadDash0CustomResourceByName(
	ctx context.Context,
	k8sClient client.Client,
	g Gomega,
	dash0CustomResourceName types.NamespacedName,
	failTestsOnNonExists bool,
) *dash0v1alpha1.Dash0 {
	dash0CustomResource := &dash0v1alpha1.Dash0{}
	if err := k8sClient.Get(ctx, dash0CustomResourceName, dash0CustomResource); err != nil {
		if apierrors.IsNotFound(err) {
			if failTestsOnNonExists {
				g.Expect(err).NotTo(HaveOccurred())
				return nil
			} else {
				return nil
			}
		} else {
			// an error occurred, but it is not an IsNotFound error, fail test immediately
			g.Expect(err).NotTo(HaveOccurred())
			return nil
		}
	}

	return dash0CustomResource
}

func VerifyDash0CustomResourceByNameDoesNotExist(
	ctx context.Context,
	k8sClient client.Client,
	g Gomega,
	dash0CustomResourceName types.NamespacedName,
) {
	g.Expect(LoadDash0CustomResourceByNameIfItExists(ctx, k8sClient, g, dash0CustomResourceName)).To(BeNil())
}

func RemoveDash0CustomResource(ctx context.Context, k8sClient client.Client) {
	RemoveDash0CustomResourceByName(ctx, k8sClient, Dash0CustomResourceQualifiedName, true)
}

func RemoveDash0CustomResourceByName(
	ctx context.Context,
	k8sClient client.Client,
	dash0CustomResourceName types.NamespacedName,
	failOnErr bool,
) {
	By("Removing the Dash0 custom resource instance")
	if dash0CustomResource := LoadDash0CustomResourceByNameIfItExists(
		ctx,
		k8sClient,
		Default,
		dash0CustomResourceName,
	); dash0CustomResource != nil {
		// We want to delete the custom resource, but we need to remove the finalizer first, otherwise the first
		// reconcile of the next test case will actually run the finalizers.
		removeFinalizerFromDash0CustomResource(ctx, k8sClient, dash0CustomResource)
		err := k8sClient.Delete(ctx, dash0CustomResource)
		if failOnErr {
			// If the test already triggered the deletion of the custom resource, but it was blocked by the finalizer
			// removing the finalizer may immediately delete the custom resource. In these cases it is okay to ignore
			// the error from k8sClient.Delete(ctx, dash0CustomResource).
			Expect(err).NotTo(HaveOccurred())
		}
	}
}

func removeFinalizerFromDash0CustomResource(
	ctx context.Context,
	k8sClient client.Client,
	dash0CustomResource *dash0v1alpha1.Dash0,
) {
	finalizerHasBeenRemoved := controllerutil.RemoveFinalizer(dash0CustomResource, dash0v1alpha1.FinalizerId)
	if finalizerHasBeenRemoved {
		Expect(k8sClient.Update(ctx, dash0CustomResource)).To(Succeed())
	}
}
