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

	dash0common "github.com/dash0hq/dash0-operator/api/operator/common"
	dash0v1beta1 "github.com/dash0hq/dash0-operator/api/operator/v1beta1"
	"github.com/dash0hq/dash0-operator/internal/util"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

const (
	MonitoringResourceName = "dash0-monitoring-test-resource"
)

var (
	MonitoringResourceQualifiedName = types.NamespacedName{
		Namespace: TestNamespaceName,
		Name:      MonitoringResourceName,
	}
	MonitoringResourceDefaultObjectMeta = metav1.ObjectMeta{
		Namespace: TestNamespaceName,
		Name:      MonitoringResourceName,
	}
	MonitoringResourceObjectMetaWithLabelAndAnnotation = metav1.ObjectMeta{
		Namespace: TestNamespaceName,
		Name:      MonitoringResourceName,
		Labels: map[string]string{
			"test-label": "test-value",
		},
		Annotations: map[string]string{
			"test-annotation": "test-value",
		},
	}
	MonitoringResourceDefaultSpec = dash0v1beta1.Dash0MonitoringSpec{
		InstrumentWorkloads: dash0v1beta1.InstrumentWorkloads{
			LabelSelector: util.DefaultAutoInstrumentationLabelSelector,
		},
		Export: &dash0common.Export{
			Dash0: &dash0common.Dash0Configuration{
				Endpoint: EndpointDash0Test,
				Authorization: dash0common.Authorization{
					Token: &AuthorizationTokenTest,
				},
			},
		},
	}

	DefaultNamespaceInstrumentationConfig = util.NamespaceInstrumentationConfig{
		InstrumentationLabelSelector: util.DefaultAutoInstrumentationLabelSelector,
	}
)

func DefaultMonitoringResource() *dash0v1beta1.Dash0Monitoring {
	return &dash0v1beta1.Dash0Monitoring{
		ObjectMeta: MonitoringResourceDefaultObjectMeta,
		Spec:       MonitoringResourceDefaultSpec,
	}
}

func DefaultMonitoringResourceWithName(monitoringResourceName types.NamespacedName) *dash0v1beta1.Dash0Monitoring {
	return &dash0v1beta1.Dash0Monitoring{
		ObjectMeta: metav1.ObjectMeta{
			Name:      monitoringResourceName.Name,
			Namespace: monitoringResourceName.Namespace,
		},
		Spec: MonitoringResourceDefaultSpec,
	}
}

func CreateDefaultMonitoringResource(
	ctx context.Context,
	k8sClient client.Client,
	monitoringResourceName types.NamespacedName,
) *dash0v1beta1.Dash0Monitoring {
	return CreateMonitoringResource(
		ctx,
		k8sClient,
		DefaultMonitoringResourceWithName(monitoringResourceName),
	)
}

func CreateMonitoringResource(
	ctx context.Context,
	k8sClient client.Client,
	monitoringResource *dash0v1beta1.Dash0Monitoring,
) *dash0v1beta1.Dash0Monitoring {
	resource, err := CreateMonitoringResourceWithPotentialError(ctx, k8sClient, monitoringResource)
	Expect(err).ToNot(HaveOccurred())
	return resource
}

func CreateMonitoringResourceWithPotentialError(
	ctx context.Context,
	k8sClient client.Client,
	monitoringResource *dash0v1beta1.Dash0Monitoring,
) (*dash0v1beta1.Dash0Monitoring, error) {
	err := k8sClient.Create(ctx, monitoringResource)
	return monitoringResource, err
}

func EnsureMonitoringResourceExists(
	ctx context.Context,
	k8sClient client.Client,
) *dash0v1beta1.Dash0Monitoring {
	return EnsureMonitoringResourceWithSpecExistsInNamespace(
		ctx,
		k8sClient,
		MonitoringResourceDefaultSpec,
		MonitoringResourceQualifiedName,
	)
}

func EnsureMonitoringResourceExistsWithInstrumentWorkloadsMode(
	ctx context.Context,
	k8sClient client.Client,
	instrumentWorkloadsMode dash0common.InstrumentWorkloadsMode,
) *dash0v1beta1.Dash0Monitoring {
	spec := MonitoringResourceDefaultSpec
	if instrumentWorkloadsMode != "" {
		spec.InstrumentWorkloads.Mode = instrumentWorkloadsMode
	}
	return EnsureMonitoringResourceWithSpecExistsInNamespace(
		ctx,
		k8sClient,
		spec,
		MonitoringResourceQualifiedName,
	)
}

func EnsureMonitoringResourceWithSpecExistsInNamespace(
	ctx context.Context,
	k8sClient client.Client,
	spec dash0v1beta1.Dash0MonitoringSpec,
	namespacesName types.NamespacedName,
) *dash0v1beta1.Dash0Monitoring {
	By("creating the Dash0 monitoring resource")

	objectMeta := metav1.ObjectMeta{
		Name:      namespacesName.Name,
		Namespace: namespacesName.Namespace,
	}
	object := EnsureKubernetesObjectExists(
		ctx,
		k8sClient,
		MonitoringResourceQualifiedName,
		&dash0v1beta1.Dash0Monitoring{},
		&dash0v1beta1.Dash0Monitoring{
			ObjectMeta: objectMeta,
			Spec:       spec,
		},
	)
	return object.(*dash0v1beta1.Dash0Monitoring)
}

func EnsureEmptyMonitoringResourceExistsAndIsAvailable(
	ctx context.Context,
	k8sClient client.Client,
) *dash0v1beta1.Dash0Monitoring {
	object := EnsureKubernetesObjectExists(
		ctx,
		k8sClient,
		MonitoringResourceQualifiedName,
		&dash0v1beta1.Dash0Monitoring{},
		&dash0v1beta1.Dash0Monitoring{
			ObjectMeta: metav1.ObjectMeta{
				Name:      MonitoringResourceName,
				Namespace: TestNamespaceName,
			},
			Spec: dash0v1beta1.Dash0MonitoringSpec{},
		},
	)
	monitoringResource := object.(*dash0v1beta1.Dash0Monitoring)
	monitoringResource.EnsureResourceIsMarkedAsAvailable()
	Expect(k8sClient.Status().Update(ctx, monitoringResource)).To(Succeed())
	return monitoringResource
}

func EnsureMonitoringResourceExistsAndIsAvailable(
	ctx context.Context,
	k8sClient client.Client,
) *dash0v1beta1.Dash0Monitoring {
	return EnsureMonitoringResourceWithSpecExistsInNamespaceAndIsAvailable(
		ctx,
		k8sClient,
		MonitoringResourceDefaultSpec,
		MonitoringResourceQualifiedName,
	)
}

func EnsureMonitoringResourceWithSpecExistsAndIsAvailable(
	ctx context.Context,
	k8sClient client.Client,
	spec dash0v1beta1.Dash0MonitoringSpec,
) *dash0v1beta1.Dash0Monitoring {
	return EnsureMonitoringResourceWithSpecExistsInNamespaceAndIsAvailable(
		ctx,
		k8sClient,
		spec,
		MonitoringResourceQualifiedName,
	)
}

func EnsureMonitoringResourceExistsInNamespaceAndIsAvailable(
	ctx context.Context,
	k8sClient client.Client,
	namespacesName types.NamespacedName,
) *dash0v1beta1.Dash0Monitoring {
	return EnsureMonitoringResourceWithSpecExistsInNamespaceAndIsAvailable(
		ctx,
		k8sClient,
		MonitoringResourceDefaultSpec,
		namespacesName,
	)
}

func EnsureMonitoringResourceWithSpecExistsInNamespaceAndIsAvailable(
	ctx context.Context,
	k8sClient client.Client,
	spec dash0v1beta1.Dash0MonitoringSpec,
	namespacesName types.NamespacedName,
) *dash0v1beta1.Dash0Monitoring {
	monitoringResource := EnsureMonitoringResourceWithSpecExistsInNamespace(
		ctx,
		k8sClient,
		spec,
		namespacesName,
	)
	monitoringResource.EnsureResourceIsMarkedAsAvailable()
	Expect(k8sClient.Status().Update(ctx, monitoringResource)).To(Succeed())
	return monitoringResource
}

func EnsureMonitoringResourceExistsAndIsDegraded(
	ctx context.Context,
	k8sClient client.Client,
) *dash0v1beta1.Dash0Monitoring {
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
) *dash0v1beta1.Dash0Monitoring {
	return LoadMonitoringResourceByName(ctx, k8sClient, g, monitoringResourceName, false)
}

func LoadMonitoringResourceOrFail(
	ctx context.Context,
	k8sClient client.Client,
	g Gomega,
) *dash0v1beta1.Dash0Monitoring {
	return LoadMonitoringResourceByNameOrFail(ctx, k8sClient, g, MonitoringResourceQualifiedName)
}

func LoadMonitoringResourceByNameOrFail(
	ctx context.Context,
	k8sClient client.Client,
	g Gomega,
	monitoringResourceName types.NamespacedName,
) *dash0v1beta1.Dash0Monitoring {
	return LoadMonitoringResourceByName(ctx, k8sClient, g, monitoringResourceName, true)
}

func LoadMonitoringResourceByName(
	ctx context.Context,
	k8sClient client.Client,
	g Gomega,
	monitoringResourceName types.NamespacedName,
	failTestsOnNonExists bool,
) *dash0v1beta1.Dash0Monitoring {
	monitoringResource := &dash0v1beta1.Dash0Monitoring{}
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

func VerifyMonitoringResourceDoesNotExist(
	ctx context.Context,
	k8sClient client.Client,
	g Gomega,
) {
	VerifyMonitoringResourceByNameDoesNotExist(ctx, k8sClient, g, MonitoringResourceQualifiedName)
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

func DeleteMonitoringResourceIfItExists(ctx context.Context, k8sClient client.Client) {
	DeleteMonitoringResourceByName(ctx, k8sClient, MonitoringResourceQualifiedName, false)
}

func DeleteMonitoringResource(ctx context.Context, k8sClient client.Client) {
	DeleteMonitoringResourceByName(ctx, k8sClient, MonitoringResourceQualifiedName, true)
}

func DeleteMonitoringResourceByName(
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
	monitoringResource *dash0v1beta1.Dash0Monitoring,
) {
	finalizerHasBeenRemoved := controllerutil.RemoveFinalizer(monitoringResource, dash0common.MonitoringFinalizerId)
	if finalizerHasBeenRemoved {
		Expect(k8sClient.Update(ctx, monitoringResource)).To(Succeed())
	}
}

func UpdateInstrumentWorkloadsMode(
	ctx context.Context,
	k8sClient client.Client,
	instrumentWorkloads dash0common.InstrumentWorkloadsMode,
) {
	monitoringResource := LoadMonitoringResourceOrFail(ctx, k8sClient, Default)
	monitoringResource.Spec.InstrumentWorkloads.Mode = instrumentWorkloads
	Expect(k8sClient.Update(ctx, monitoringResource)).To(Succeed())
}

func UpdateInstrumentWorkloadsLabelSelector(
	ctx context.Context,
	k8sClient client.Client,
	autoInstrumentationLabelSelector string,
) {
	monitoringResource := LoadMonitoringResourceOrFail(ctx, k8sClient, Default)
	monitoringResource.Spec.InstrumentWorkloads.LabelSelector = autoInstrumentationLabelSelector
	Expect(k8sClient.Update(ctx, monitoringResource)).To(Succeed())
}

func UpdateInstrumentWorkloadsTraceContextPropagators(
	ctx context.Context,
	k8sClient client.Client,
	traceContextPropagators *string,
) {
	monitoringResource := LoadMonitoringResourceOrFail(ctx, k8sClient, Default)
	monitoringResource.Spec.InstrumentWorkloads.TraceContext.Propagators = traceContextPropagators
	Expect(k8sClient.Update(ctx, monitoringResource)).To(Succeed())
}
