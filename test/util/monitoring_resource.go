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
	MonitoringResourceName = "das0-monitoring-test-resource"
)

var (
	MonitoringResourceQualifiedName = types.NamespacedName{
		Namespace: TestNamespaceName,
		Name:      MonitoringResourceName,
	}

	MonitoringResourceDefaultObjectMeta = metav1.ObjectMeta{
		Name:      MonitoringResourceName,
		Namespace: TestNamespaceName,
	}
	MonitoringResourceDefaultSpec = dash0v1alpha1.Dash0MonitoringSpec{
		Export: &dash0v1alpha1.Export{
			Dash0: &dash0v1alpha1.Dash0Configuration{
				Endpoint: EndpointDash0Test,
				Authorization: dash0v1alpha1.Authorization{
					Token: &AuthorizationTokenTest,
				},
			},
		},
	}
)

func EnsureMonitoringResourceExists(
	ctx context.Context,
	k8sClient client.Client,
) *dash0v1alpha1.Dash0Monitoring {
	return EnsureMonitoringResourceExistsWithNamespacedName(
		ctx,
		k8sClient,
		MonitoringResourceQualifiedName,
		"",
	)
}

func EnsureMonitoringResourceExistsWithInstrumentWorkloadsMode(
	ctx context.Context,
	k8sClient client.Client,
	instrumentWorkloads dash0v1alpha1.InstrumentWorkloadsMode,
) *dash0v1alpha1.Dash0Monitoring {
	return EnsureMonitoringResourceExistsWithNamespacedName(
		ctx,
		k8sClient,
		MonitoringResourceQualifiedName,
		instrumentWorkloads,
	)
}

func EnsureMonitoringResourceExistsWithNamespacedName(
	ctx context.Context,
	k8sClient client.Client,
	namespacesName types.NamespacedName,
	instrumentWorkloads dash0v1alpha1.InstrumentWorkloadsMode,
) *dash0v1alpha1.Dash0Monitoring {
	By("creating the Dash0 monitoring resource")
	spec := MonitoringResourceDefaultSpec
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
		MonitoringResourceQualifiedName,
		&dash0v1alpha1.Dash0Monitoring{},
		&dash0v1alpha1.Dash0Monitoring{
			ObjectMeta: objectMeta,
			Spec:       spec,
		},
	)
	return object.(*dash0v1alpha1.Dash0Monitoring)
}

func CreateDefaultMonitoringResource(
	ctx context.Context,
	k8sClient client.Client,
	monitoringResourceName types.NamespacedName,
) *dash0v1alpha1.Dash0Monitoring {
	resource, err := CreateMonitoringResource(
		ctx,
		k8sClient,
		&dash0v1alpha1.Dash0Monitoring{
			ObjectMeta: metav1.ObjectMeta{
				Name:      monitoringResourceName.Name,
				Namespace: monitoringResourceName.Namespace,
			},
			Spec: MonitoringResourceDefaultSpec,
		},
	)
	Expect(err).NotTo(HaveOccurred())
	return resource
}

func CreateMonitoringResource(
	ctx context.Context,
	k8sClient client.Client,
	monitoringResource *dash0v1alpha1.Dash0Monitoring,
) (*dash0v1alpha1.Dash0Monitoring, error) {
	err := k8sClient.Create(ctx, monitoringResource)
	return monitoringResource, err
}

func EnsureMonitoringResourceExistsAndIsAvailable(
	ctx context.Context,
	k8sClient client.Client,
) *dash0v1alpha1.Dash0Monitoring {
	return EnsureMonitoringResourceExistsAndIsAvailableInNamespace(
		ctx,
		k8sClient,
		MonitoringResourceQualifiedName,
	)
}

func EnsureMonitoringResourceExistsAndIsAvailableInNamespace(
	ctx context.Context,
	k8sClient client.Client,
	namespacedName types.NamespacedName,
) *dash0v1alpha1.Dash0Monitoring {
	monitoringResource := EnsureMonitoringResourceExistsWithNamespacedName(
		ctx,
		k8sClient,
		namespacedName,
		"",
	)
	monitoringResource.EnsureResourceIsMarkedAsAvailable()
	Expect(k8sClient.Status().Update(ctx, monitoringResource)).To(Succeed())
	return monitoringResource
}

func EnsureMonitoringResourceExistsAndIsDegraded(
	ctx context.Context,
	k8sClient client.Client,
) *dash0v1alpha1.Dash0Monitoring {
	monitoringResource := EnsureMonitoringResourceExists(ctx, k8sClient)
	monitoringResource.EnsureResourceIsMarkedAsDegraded(
		"TestReasonForDegradation",
		"This resource is degraded.",
	)
	Expect(k8sClient.Status().Update(ctx, monitoringResource)).To(Succeed())
	return monitoringResource
}

func LoadMonitoringResourceByNameIfItExists(
	ctx context.Context,
	k8sClient client.Client,
	g Gomega,
	monitoringResourceName types.NamespacedName,
) *dash0v1alpha1.Dash0Monitoring {
	return LoadMonitoringResourceByName(ctx, k8sClient, g, monitoringResourceName, false)
}

func LoadMonitoringResourceOrFail(
	ctx context.Context,
	k8sClient client.Client,
	g Gomega,
) *dash0v1alpha1.Dash0Monitoring {
	return LoadMonitoringResourceByNameOrFail(ctx, k8sClient, g, MonitoringResourceQualifiedName)
}

func LoadMonitoringResourceByNameOrFail(
	ctx context.Context,
	k8sClient client.Client,
	g Gomega,
	monitoringResourceName types.NamespacedName,
) *dash0v1alpha1.Dash0Monitoring {
	return LoadMonitoringResourceByName(ctx, k8sClient, g, monitoringResourceName, true)
}

func LoadMonitoringResourceByName(
	ctx context.Context,
	k8sClient client.Client,
	g Gomega,
	monitoringResourceName types.NamespacedName,
	failTestsOnNonExists bool,
) *dash0v1alpha1.Dash0Monitoring {
	monitoringResource := &dash0v1alpha1.Dash0Monitoring{}
	if err := k8sClient.Get(ctx, monitoringResourceName, monitoringResource); err != nil {
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

	return monitoringResource
}

func VerifyMonitoringResourceByNameDoesNotExist(
	ctx context.Context,
	k8sClient client.Client,
	g Gomega,
	MonitoringResourceName types.NamespacedName,
) {
	g.Expect(LoadMonitoringResourceByNameIfItExists(
		ctx,
		k8sClient,
		g,
		MonitoringResourceName,
	)).To(BeNil())
}

func RemoveMonitoringResource(ctx context.Context, k8sClient client.Client) {
	RemoveMonitoringResourceByName(ctx, k8sClient, MonitoringResourceQualifiedName, true)
}

func RemoveMonitoringResourceByName(
	ctx context.Context,
	k8sClient client.Client,
	monitoringResourceName types.NamespacedName,
	failOnErr bool,
) {
	By("Removing the monitoring resource instance")
	if monitoringResource := LoadMonitoringResourceByNameIfItExists(
		ctx,
		k8sClient,
		Default,
		monitoringResourceName,
	); monitoringResource != nil {
		// We want to delete the monitoring resource, but we need to remove the finalizer first, otherwise the first
		// reconcile of the next test case will actually run the finalizers.
		removeFinalizerFromMonitoringResource(ctx, k8sClient, monitoringResource)
		err := k8sClient.Delete(ctx, monitoringResource)
		if failOnErr {
			// If the test already triggered the deletion of the monitoring resource, but it was blocked by the
			// finalizer; removing the finalizer may immediately delete the monitoring resource. In these cases it is
			// okay to ignore the error from k8sClient.Delete(ctx, monitoringResource).
			Expect(err).NotTo(HaveOccurred())
		}
	}
}

func removeFinalizerFromMonitoringResource(
	ctx context.Context,
	k8sClient client.Client,
	monitoringResource *dash0v1alpha1.Dash0Monitoring,
) {
	finalizerHasBeenRemoved := controllerutil.RemoveFinalizer(monitoringResource, dash0v1alpha1.MonitoringFinalizerId)
	if finalizerHasBeenRemoved {
		Expect(k8sClient.Update(ctx, monitoringResource)).To(Succeed())
	}
}

func UpdateInstrumentWorkloadsMode(
	ctx context.Context,
	k8sClient client.Client,
	instrumentWorkloads dash0v1alpha1.InstrumentWorkloadsMode,
) {
	monitoringResource := LoadMonitoringResourceOrFail(ctx, k8sClient, Default)
	monitoringResource.Spec.InstrumentWorkloads = instrumentWorkloads
	Expect(k8sClient.Update(ctx, monitoringResource)).To(Succeed())
}
