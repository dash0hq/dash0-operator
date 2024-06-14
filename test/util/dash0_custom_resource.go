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

	operatorv1alpha1 "github.com/dash0hq/dash0-operator/api/v1alpha1"
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
) *operatorv1alpha1.Dash0 {
	By("creating the Dash0 custom resource")
	object := EnsureKubernetesObjectExists(
		ctx,
		k8sClient,
		Dash0CustomResourceQualifiedName,
		&operatorv1alpha1.Dash0{},
		&operatorv1alpha1.Dash0{
			ObjectMeta: metav1.ObjectMeta{
				Name:      Dash0CustomResourceQualifiedName.Name,
				Namespace: Dash0CustomResourceQualifiedName.Namespace,
			},
		},
	)
	return object.(*operatorv1alpha1.Dash0)
}

func EnsureDash0CustomResourceExistsAndIsAvailable(
	ctx context.Context,
	k8sClient client.Client,
) *operatorv1alpha1.Dash0 {
	dash0CustomResource := EnsureDash0CustomResourceExists(ctx, k8sClient)
	dash0CustomResource.EnsureResourceIsMarkedAsAvailable()
	Expect(k8sClient.Status().Update(ctx, dash0CustomResource)).To(Succeed())
	return dash0CustomResource
}

func EnsureDash0CustomResourceExistsAndIsDegraded(
	ctx context.Context,
	k8sClient client.Client,
) *operatorv1alpha1.Dash0 {
	dash0CustomResource := EnsureDash0CustomResourceExists(ctx, k8sClient)
	dash0CustomResource.EnsureResourceIsMarkedAsUnavailable()
	Expect(k8sClient.Status().Update(ctx, dash0CustomResource)).To(Succeed())
	return dash0CustomResource
}

func LoadDash0CustomResourceIfItExists(ctx context.Context, k8sClient client.Client) *operatorv1alpha1.Dash0 {
	return LoadDash0CustomResource(ctx, k8sClient, Default, false)
}

func LoadDash0CustomResourceOrFail(ctx context.Context, k8sClient client.Client, g Gomega) *operatorv1alpha1.Dash0 {
	return LoadDash0CustomResource(ctx, k8sClient, g, true)
}

func LoadDash0CustomResource(
	ctx context.Context,
	k8sClient client.Client,
	g Gomega,
	failTestsOnNonExists bool,
) *operatorv1alpha1.Dash0 {
	dash0CustomResource := &operatorv1alpha1.Dash0{}
	if err := k8sClient.Get(ctx, Dash0CustomResourceQualifiedName, dash0CustomResource); err != nil {
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
		}
	}

	return dash0CustomResource
}

func RemoveDash0CustomResource(ctx context.Context, k8sClient client.Client) {
	By("Removing the Dash0 custom resource instance")
	if dash0CustomResource := LoadDash0CustomResourceIfItExists(ctx, k8sClient); dash0CustomResource != nil {
		// We want to delete the custom resource, but we need to remove the finalizer first, otherwise the first
		// reconcile of the next test case will actually run the finalizers.
		removeFinalizer(ctx, k8sClient, dash0CustomResource)
		Expect(k8sClient.Delete(ctx, dash0CustomResource)).To(Succeed())
	}
}

func removeFinalizer(ctx context.Context, k8sClient client.Client, dash0CustomResource *operatorv1alpha1.Dash0) {
	finalizerHasBeenRemoved := controllerutil.RemoveFinalizer(dash0CustomResource, operatorv1alpha1.FinalizerId)
	if finalizerHasBeenRemoved {
		Expect(k8sClient.Update(ctx, dash0CustomResource)).To(Succeed())
	}
}
