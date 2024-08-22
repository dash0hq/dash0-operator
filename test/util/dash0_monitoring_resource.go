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

	dash0v1alpha1 "github.com/dash0hq/dash0-operator/api/dash0monitoring/v1alpha1"
)

const (
	Dash0MonitoringResourceName = "das0-monitoring-test-resource"
)

var (
	Dash0MonitoringResourceQualifiedName = types.NamespacedName{
		Namespace: TestNamespaceName,
		Name:      Dash0MonitoringResourceName,
	}
)

func EnsureDash0MonitoringResourceExists(
	ctx context.Context,
	k8sClient client.Client,
) *dash0v1alpha1.Dash0Monitoring {
	return EnsureDash0MonitoringResourceExistsWithNamespacedName(
		ctx,
		k8sClient,
		Dash0MonitoringResourceQualifiedName,
		"",
	)
}

func EnsureDash0MonitoringResourceExistsWithInstrumentWorkloadsMode(
	ctx context.Context,
	k8sClient client.Client,
	instrumentWorkloads dash0v1alpha1.InstrumentWorkloadsMode,
) *dash0v1alpha1.Dash0Monitoring {
	return EnsureDash0MonitoringResourceExistsWithNamespacedName(
		ctx,
		k8sClient,
		Dash0MonitoringResourceQualifiedName,
		instrumentWorkloads,
	)
}

func EnsureDash0MonitoringResourceExistsWithNamespacedName(
	ctx context.Context,
	k8sClient client.Client,
	namespacesName types.NamespacedName,
	instrumentWorkloads dash0v1alpha1.InstrumentWorkloadsMode,
) *dash0v1alpha1.Dash0Monitoring {
	By("creating the Dash0 monitoring resource")
	spec := dash0v1alpha1.Dash0MonitoringSpec{
		Export: dash0v1alpha1.Export{
			Dash0: &dash0v1alpha1.Dash0Configuration{
				Endpoint: EndpointDash0Test,
				Authorization: dash0v1alpha1.Authorization{
					Token: &AuthorizationTokenTest,
				},
			},
		},
	}
	if instrumentWorkloads != "" {
		spec.InstrumentWorkloads = instrumentWorkloads
	}
	objectMeta := metav1.ObjectMeta{
		Name:      namespacesName.Name,
		Namespace: namespacesName.Namespace,
	}
	object := EnsureKubernetesObjectExists(
		ctx,
		k8sClient,
		Dash0MonitoringResourceQualifiedName,
		&dash0v1alpha1.Dash0Monitoring{},
		&dash0v1alpha1.Dash0Monitoring{
			ObjectMeta: objectMeta,
			Spec:       spec,
		},
	)
	return object.(*dash0v1alpha1.Dash0Monitoring)
}

func CreateDash0MonitoringResource(
	ctx context.Context,
	k8sClient client.Client,
	dash0MonitoringResourceName types.NamespacedName,
) *dash0v1alpha1.Dash0Monitoring {
	dash0MonitoringResource := &dash0v1alpha1.Dash0Monitoring{
		ObjectMeta: metav1.ObjectMeta{
			Name:      dash0MonitoringResourceName.Name,
			Namespace: dash0MonitoringResourceName.Namespace,
		},
		Spec: dash0v1alpha1.Dash0MonitoringSpec{
			Export: dash0v1alpha1.Export{
				Dash0: &dash0v1alpha1.Dash0Configuration{
					Endpoint: EndpointDash0Test,
					Authorization: dash0v1alpha1.Authorization{
						Token: &AuthorizationTokenTest,
					},
				},
			},
		},
	}
	Expect(k8sClient.Create(ctx, dash0MonitoringResource)).To(Succeed())
	return dash0MonitoringResource
}

func EnsureDash0MonitoringResourceExistsAndIsAvailable(
	ctx context.Context,
	k8sClient client.Client,
) *dash0v1alpha1.Dash0Monitoring {
	return EnsureDash0MonitoringResourceExistsAndIsAvailableInNamespace(
		ctx,
		k8sClient,
		Dash0MonitoringResourceQualifiedName,
	)
}

func EnsureDash0MonitoringResourceExistsAndIsAvailableInNamespace(
	ctx context.Context,
	k8sClient client.Client,
	namespacedName types.NamespacedName,
) *dash0v1alpha1.Dash0Monitoring {
	dash0MonitoringResource := EnsureDash0MonitoringResourceExistsWithNamespacedName(
		ctx,
		k8sClient,
		namespacedName,
		"",
	)
	dash0MonitoringResource.EnsureResourceIsMarkedAsAvailable()
	Expect(k8sClient.Status().Update(ctx, dash0MonitoringResource)).To(Succeed())
	return dash0MonitoringResource
}

func EnsureDash0MonitoringResourceExistsAndIsDegraded(
	ctx context.Context,
	k8sClient client.Client,
) *dash0v1alpha1.Dash0Monitoring {
	dash0MonitoringResource := EnsureDash0MonitoringResourceExists(ctx, k8sClient)
	dash0MonitoringResource.EnsureResourceIsMarkedAsDegraded(
		"TestReasonForDegradation",
		"This resource is degraded.",
	)
	Expect(k8sClient.Status().Update(ctx, dash0MonitoringResource)).To(Succeed())
	return dash0MonitoringResource
}

func LoadDash0MonitoringResourceByNameIfItExists(
	ctx context.Context,
	k8sClient client.Client,
	g Gomega,
	dash0MonitoringResourceName types.NamespacedName,
) *dash0v1alpha1.Dash0Monitoring {
	return LoadDash0MonitoringResourceByName(ctx, k8sClient, g, dash0MonitoringResourceName, false)
}

func LoadDash0MonitoringResourceOrFail(
	ctx context.Context,
	k8sClient client.Client,
	g Gomega,
) *dash0v1alpha1.Dash0Monitoring {
	return LoadDash0MonitoringResourceByNameOrFail(ctx, k8sClient, g, Dash0MonitoringResourceQualifiedName)
}

func LoadDash0MonitoringResourceByNameOrFail(
	ctx context.Context,
	k8sClient client.Client,
	g Gomega,
	dash0MonitoringResourceName types.NamespacedName,
) *dash0v1alpha1.Dash0Monitoring {
	return LoadDash0MonitoringResourceByName(ctx, k8sClient, g, dash0MonitoringResourceName, true)
}

func LoadDash0MonitoringResourceByName(
	ctx context.Context,
	k8sClient client.Client,
	g Gomega,
	dash0MonitoringResourceName types.NamespacedName,
	failTestsOnNonExists bool,
) *dash0v1alpha1.Dash0Monitoring {
	dash0MonitoringResource := &dash0v1alpha1.Dash0Monitoring{}
	if err := k8sClient.Get(ctx, dash0MonitoringResourceName, dash0MonitoringResource); err != nil {
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

	return dash0MonitoringResource
}

func VerifyDash0MonitoringResourceByNameDoesNotExist(
	ctx context.Context,
	k8sClient client.Client,
	g Gomega,
	dash0MonitoringResourceName types.NamespacedName,
) {
	g.Expect(LoadDash0MonitoringResourceByNameIfItExists(
		ctx,
		k8sClient,
		g,
		dash0MonitoringResourceName,
	)).To(BeNil())
}

func RemoveDash0MonitoringResource(ctx context.Context, k8sClient client.Client) {
	RemoveDash0MonitoringResourceByName(ctx, k8sClient, Dash0MonitoringResourceQualifiedName, true)
}

func RemoveDash0MonitoringResourceByName(
	ctx context.Context,
	k8sClient client.Client,
	dash0MonitoringResourceName types.NamespacedName,
	failOnErr bool,
) {
	By("Removing the Dash0 monitoring resource instance")
	if dash0MonitoringResource := LoadDash0MonitoringResourceByNameIfItExists(
		ctx,
		k8sClient,
		Default,
		dash0MonitoringResourceName,
	); dash0MonitoringResource != nil {
		// We want to delete the monitoring resource, but we need to remove the finalizer first, otherwise the first
		// reconcile of the next test case will actually run the finalizers.
		removeFinalizerFromDash0MonitoringResource(ctx, k8sClient, dash0MonitoringResource)
		err := k8sClient.Delete(ctx, dash0MonitoringResource)
		if failOnErr {
			// If the test already triggered the deletion of the monitoring resource, but it was blocked by the
			// finalizer; removing the finalizer may immediately delete the monitoring resource. In these cases it is
			// okay to ignore the error from k8sClient.Delete(ctx, dash0MonitoringResource).
			Expect(err).NotTo(HaveOccurred())
		}
	}
}

func removeFinalizerFromDash0MonitoringResource(
	ctx context.Context,
	k8sClient client.Client,
	dash0MonitoringResource *dash0v1alpha1.Dash0Monitoring,
) {
	finalizerHasBeenRemoved := controllerutil.RemoveFinalizer(dash0MonitoringResource, dash0v1alpha1.MonitoringFinalizerId)
	if finalizerHasBeenRemoved {
		Expect(k8sClient.Update(ctx, dash0MonitoringResource)).To(Succeed())
	}
}

func UpdateInstrumentWorkloadsMode(
	ctx context.Context,
	k8sClient client.Client,
	instrumentWorkloads dash0v1alpha1.InstrumentWorkloadsMode,
) {
	dash0MonitoringResource := LoadDash0MonitoringResourceOrFail(ctx, k8sClient, Default)
	dash0MonitoringResource.Spec.InstrumentWorkloads = instrumentWorkloads
	Expect(k8sClient.Update(ctx, dash0MonitoringResource)).To(Succeed())
}
