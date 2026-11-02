// SPDX-FileCopyrightText: Copyright 2024 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package util

import (
	"context"
	_ "embed"
	"time"

	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/yaml"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var (
	//go:embed crds/perses.dev_persesdashboards.yaml
	persesDashboardsCrdYaml         []byte
	PersesDashboardCrdQualifiedName = types.NamespacedName{
		Name: "persesdashboards.perses.dev",
	}
	persesDashboardStorageVersionListGvk = schema.GroupVersionKind{
		Group:   "perses.dev",
		Version: "v1alpha2",
		Kind:    "PersesDashboardList",
	}

	//go:embed crds/monitoring.coreos.com_prometheusrules.yaml
	prometheusRulesCrdYaml         []byte
	PrometheusRuleCrdQualifiedName = types.NamespacedName{
		Name: "prometheusrules.monitoring.coreos.com",
	}
	prometheusRuleStorageVersionListGvk = schema.GroupVersionKind{
		Group:   "monitoring.coreos.com",
		Version: "v1",
		Kind:    "PrometheusRuleList",
	}
)

// EnsurePersesDashboardCrdExists creates the PersesDashboard CRD if it does not exist yet and waits until
// PersesDashboard resources are served.
func EnsurePersesDashboardCrdExists(
	ctx context.Context,
	k8sClient client.Client,
) *apiextensionsv1.CustomResourceDefinition {
	return ensureThirdPartyResourceExists(
		ctx,
		k8sClient,
		persesDashboardsCrdYaml,
		PersesDashboardCrdQualifiedName,
		persesDashboardStorageVersionListGvk,
	)
}

// EnsurePrometheusRuleCrdExists creates the PrometheusRule CRD if it does not exist yet and waits until PrometheusRule
// resources are served.
func EnsurePrometheusRuleCrdExists(
	ctx context.Context,
	k8sClient client.Client,
) *apiextensionsv1.CustomResourceDefinition {
	return ensureThirdPartyResourceExists(
		ctx,
		k8sClient,
		prometheusRulesCrdYaml,
		PrometheusRuleCrdQualifiedName,
		prometheusRuleStorageVersionListGvk,
	)
}

// DeletePersesDashboardCrdIfItExists deletes the PersesDashboard CRD and waits until it is gone.
func DeletePersesDashboardCrdIfItExists(ctx context.Context, k8sClient client.Client) {
	GinkgoHelper()
	deleteThirdPartyResourceCrdIfItExists(
		ctx,
		k8sClient,
		PersesDashboardCrdQualifiedName,
		persesDashboardStorageVersionListGvk,
	)
}

// DeletePrometheusRuleCrdIfItExists deletes the PrometheusRule CRD and waits until it is gone.
func DeletePrometheusRuleCrdIfItExists(ctx context.Context, k8sClient client.Client) {
	GinkgoHelper()
	deleteThirdPartyResourceCrdIfItExists(
		ctx,
		k8sClient,
		PrometheusRuleCrdQualifiedName,
		prometheusRuleStorageVersionListGvk,
	)
}

// WaitUntilThirdPartyResourceKindIsServed waits until the API server has established the CRD for the given list kind
// and the k8sClient's REST mapper has discovered it, so that subsequent create/get/delete calls for instances of that
// kind do not fail with a NoKindMatchError. The given GVK must be a list kind (e.g. "PrometheusRuleList"). Listing
// has no side effects but exercises the same REST mapping lookup that creating/getting an instance requires, it
// nudges the lazy dynamic REST mapper to reload its discovery information, and it makes the API server initialize
// its storage for that kind and version.
func WaitUntilThirdPartyResourceKindIsServed(
	ctx context.Context,
	k8sClient client.Client,
	listGvk schema.GroupVersionKind,
) {
	GinkgoHelper()
	Eventually(func(g Gomega) {
		list := &unstructured.UnstructuredList{}
		list.SetGroupVersionKind(listGvk)
		g.Expect(k8sClient.List(ctx, list)).To(Succeed())
	}, 10*time.Second, 100*time.Millisecond).Should(Succeed())
}

func ensureThirdPartyResourceExists(
	ctx context.Context,
	k8sClient client.Client,
	crdYaml []byte,
	crdQualifiedName types.NamespacedName,
	storageVersionListGvk schema.GroupVersionKind,
) *apiextensionsv1.CustomResourceDefinition {
	GinkgoHelper()
	existingCrd := &apiextensionsv1.CustomResourceDefinition{}
	if err := k8sClient.Get(ctx, crdQualifiedName, existingCrd); err == nil && existingCrd.DeletionTimestamp != nil {
		waitUntilCrdIsGone(ctx, k8sClient, crdQualifiedName)
	}

	crdFromYaml := &apiextensionsv1.CustomResourceDefinition{}
	Expect(yaml.Unmarshal(crdYaml, crdFromYaml)).To(Succeed())
	crdObject := EnsureKubernetesObjectExists(
		ctx,
		k8sClient,
		crdQualifiedName,
		&apiextensionsv1.CustomResourceDefinition{},
		crdFromYaml,
	)
	WaitUntilThirdPartyResourceKindIsServed(ctx, k8sClient, storageVersionListGvk)

	return crdObject.(*apiextensionsv1.CustomResourceDefinition)
}

func deleteThirdPartyResourceCrdIfItExists(
	ctx context.Context,
	k8sClient client.Client,
	crdQualifiedName types.NamespacedName,
	storageVersionListGvk schema.GroupVersionKind,
) {
	GinkgoHelper()
	crd := &apiextensionsv1.CustomResourceDefinition{}
	err := k8sClient.Get(ctx, crdQualifiedName, crd)
	if apierrors.IsNotFound(err) {
		return
	}
	Expect(err).NotTo(HaveOccurred())

	// The API server initializes the storage for custom resources lazily, and every change to the CRD discards it.
	// Deleting a CRD while its storage is not initialized can leave the CRD terminating for minutes.
	WaitUntilThirdPartyResourceKindIsServed(ctx, k8sClient, storageVersionListGvk)

	Expect(
		client.IgnoreNotFound(k8sClient.Delete(ctx, crd, &client.DeleteOptions{GracePeriodSeconds: new(int64)})),
	).To(Succeed())
	waitUntilCrdIsGone(ctx, k8sClient, crdQualifiedName)
}

/*
waitUntilCrdIsGone waits until the given CRD has been deleted. If a retry of the API server's CRD finalizer runs into a
conflict when updating the CRD status, the finalizer drops the CRD from its work queue, and it ignores the update event
for its own status change, so the CRD stays terminating until the finalizer's informer resyncs after five minutes.
Changing an annotation of the CRD while it is terminating makes the finalizer process it again.
*/
func waitUntilCrdIsGone(ctx context.Context, k8sClient client.Client, crdQualifiedName types.NamespacedName) {
	GinkgoHelper()
	lastRequeue := time.Now()
	Eventually(func(g Gomega) {
		crd := &apiextensionsv1.CustomResourceDefinition{}
		err := k8sClient.Get(ctx, crdQualifiedName, crd)
		if err == nil && time.Since(lastRequeue) > time.Second {
			lastRequeue = time.Now()
			requeueCrdFinalizer(ctx, k8sClient, crd)
		}
		g.Expect(apierrors.IsNotFound(err)).To(BeTrue(), "expected CRD %s to be gone, got: %v", crdQualifiedName.Name, err)
	}, 10*time.Second, 20*time.Millisecond).Should(Succeed())
}

func requeueCrdFinalizer(ctx context.Context, k8sClient client.Client, crd *apiextensionsv1.CustomResourceDefinition) {
	patch := client.MergeFrom(crd.DeepCopy())
	if crd.Annotations == nil {
		crd.Annotations = map[string]string{}
	}
	crd.Annotations["dash0.com/test-requeue-crd-finalizer"] = time.Now().Format(time.RFC3339Nano)
	_ = k8sClient.Patch(ctx, crd, patch)
}

type DummyLeaderElectionAware struct {
	Leader bool
}

func (l *DummyLeaderElectionAware) IsLeader() bool {
	return l.Leader
}
