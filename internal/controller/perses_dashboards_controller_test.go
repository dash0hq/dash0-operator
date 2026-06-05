// SPDX-FileCopyrightText: Copyright 2024 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package controller

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"os"
	"time"

	persesv1alpha1 "github.com/perses/perses-operator/api/v1alpha1"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/client-go/util/workqueue"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllertest"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	"sigs.k8s.io/yaml"

	dash0common "github.com/dash0hq/dash0-operator/api/operator/common"
	dash0v1beta1 "github.com/dash0hq/dash0-operator/api/operator/v1beta1"
	"github.com/dash0hq/dash0-operator/internal/util"
	"github.com/dash0hq/dash0-operator/internal/util/cluster"
	"github.com/dash0hq/dash0-operator/internal/util/logd"

	"github.com/h2non/gock"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	. "github.com/dash0hq/dash0-operator/test/util"
)

const (
	dashboardOriginPattern            = "dash0-operator_%s_test-dataset_test-namespace_test-dashboard"
	dashboardOriginPatternAlternative = "dash0-operator_%s_test-dataset-alt_test-namespace_test-dashboard"
)

var (
	persesDashboardCrd        *apiextensionsv1.CustomResourceDefinition
	testQueuePersesDashboards = workqueue.NewTypedWithConfig(
		workqueue.TypedQueueConfig[ThirdPartyResourceSyncJob]{
			Name: "dash0-third-party-resource-synchronization-queue",
		},
	)

	dashboardApiBasePath = "/api/dashboards/"

	defaultExpectedPathDashboard = fmt.Sprintf(
		"%s.*%s",
		dashboardApiBasePath,
		"dash0-operator_.*_test-dataset_test-namespace_test-dashboard",
	)
	ExpectedPathDashboardAlternative = fmt.Sprintf(
		"%s.*%s",
		dashboardApiBasePath,
		"dash0-operator_.*_test-dataset-alt_test-namespace_test-dashboard",
	)
)

var _ = Describe("The Perses dashboard controller", Ordered, func() {

	var (
		clusterId    string
		caBundlePath string
		caPEM        []byte
	)

	ctx := context.Background()
	logger := logd.FromContext(ctx)

	BeforeAll(
		func() {
			EnsureTestNamespaceExists(ctx, k8sClient)
			EnsureOperatorNamespaceExists(ctx, k8sClient)
			clusterId = string(cluster.ReadPseudoClusterUid(ctx, k8sClient, logger))

			tmpDir := GinkgoT().TempDir()
			caBundlePath = tmpDir + "/ca.crt"
			caPEM = generateTestCaPem()
			Expect(os.WriteFile(caBundlePath, caPEM, 0o600)).To(Succeed())
		},
	)

	Describe(
		"the Perses dashboard CRD reconciler", func() {

			AfterEach(
				func() {
					deletePersesDashboardCrdIfItExists(ctx)
				},
			)

			It(
				"does not start watching Perses dashboard resources if the CRD does not exist and neither API endpoint nor auth token have been provided",
				func() {
					persesDashboardCrdReconciler := createPersesDashboardCrdReconciler(false, caBundlePath)
					Expect(persesDashboardCrdReconciler.SetupWithManager(ctx, mgr, k8sClient, logger)).To(Succeed())
					Expect(isWatchingPersesDashboardResources(persesDashboardCrdReconciler)).To(BeFalse())
				},
			)

			It(
				"does not start watching Perses dashboard resources if the CRD does not exist and the auth token has not been provided",
				func() {
					persesDashboardCrdReconciler := createPersesDashboardCrdReconciler(false, caBundlePath)
					Expect(persesDashboardCrdReconciler.SetupWithManager(ctx, mgr, k8sClient, logger)).To(Succeed())
					persesDashboardCrdReconciler.SetDefaultApiConfigs(
						ctx, []ApiConfig{
							{
								Endpoint: ApiEndpointTest,
								Dataset:  DatasetCustomTest,
							},
						}, logger,
					)
					Expect(isWatchingPersesDashboardResources(persesDashboardCrdReconciler)).To(BeFalse())
				},
			)

			It(
				"does not start watching Perses dashboards if the CRD does not exist and the API endpoint has not been provided",
				func() {
					persesDashboardCrdReconciler := createPersesDashboardCrdReconciler(false, caBundlePath)
					Expect(persesDashboardCrdReconciler.SetupWithManager(ctx, mgr, k8sClient, logger)).To(Succeed())
					persesDashboardCrdReconciler.SetDefaultApiConfigs(
						ctx, []ApiConfig{
							{
								Token: AuthorizationTokenTest,
							},
						}, logger,
					)
					Expect(isWatchingPersesDashboardResources(persesDashboardCrdReconciler)).To(BeFalse())
				},
			)

			It(
				"does not start watching Perses dashboards if the API endpoint & auth token have been provided but the CRD does not exist",
				func() {
					persesDashboardCrdReconciler := createPersesDashboardCrdReconciler(false, caBundlePath)
					Expect(persesDashboardCrdReconciler.SetupWithManager(ctx, mgr, k8sClient, logger)).To(Succeed())
					persesDashboardCrdReconciler.SetDefaultApiConfigs(
						ctx, []ApiConfig{
							{
								Endpoint: ApiEndpointTest,
								Dataset:  DatasetCustomTest,
								Token:    AuthorizationTokenTest,
							},
						}, logger,
					)
					Expect(isWatchingPersesDashboardResources(persesDashboardCrdReconciler)).To(BeFalse())
				},
			)

			It(
				"does not start watching Perses dashboards if the CRD exists but the auth token has not been provided",
				func() {
					persesDashboardCrdReconciler := createPersesDashboardCrdReconciler(false, caBundlePath)
					ensurePersesDashboardCrdExists(ctx)
					Expect(persesDashboardCrdReconciler.SetupWithManager(ctx, mgr, k8sClient, logger)).To(Succeed())
					persesDashboardCrdReconciler.SetDefaultApiConfigs(
						ctx, []ApiConfig{
							{
								Endpoint: ApiEndpointTest,
								Dataset:  DatasetCustomTest,
							},
						}, logger,
					)
					Expect(isWatchingPersesDashboardResources(persesDashboardCrdReconciler)).To(BeFalse())
				},
			)

			It(
				"does not start watching Perses dashboards if the CRD exists but the API endpoint has not been provided",
				func() {
					persesDashboardCrdReconciler := createPersesDashboardCrdReconciler(false, caBundlePath)
					ensurePersesDashboardCrdExists(ctx)
					Expect(persesDashboardCrdReconciler.SetupWithManager(ctx, mgr, k8sClient, logger)).To(Succeed())
					persesDashboardCrdReconciler.SetDefaultApiConfigs(
						ctx, []ApiConfig{
							{
								Token: AuthorizationTokenTest,
							},
						}, logger,
					)
					Expect(isWatchingPersesDashboardResources(persesDashboardCrdReconciler)).To(BeFalse())
				},
			)

			It(
				"starts watching Perses dashboards if the CRD exists and the API endpoint has been provided", func() {
					ensurePersesDashboardCrdExists(ctx)
					persesDashboardCrdReconciler := createPersesDashboardCrdReconciler(false, caBundlePath)
					Expect(persesDashboardCrdReconciler.SetupWithManager(ctx, mgr, k8sClient, logger)).To(Succeed())
					Expect(isWatchingPersesDashboardResources(persesDashboardCrdReconciler)).To(BeFalse())
					persesDashboardCrdReconciler.SetDefaultApiConfigs(
						ctx, []ApiConfig{
							{
								Endpoint: ApiEndpointTest,
								Dataset:  DatasetCustomTest,
								Token:    AuthorizationTokenTest,
							},
						}, logger,
					)
					Expect(isWatchingPersesDashboardResources(persesDashboardCrdReconciler)).To(BeTrue())
				},
			)

			It(
				"starts watching Perses dashboards if API endpoint is provided and the CRD is created later on", func() {
					persesDashboardCrdReconciler := createPersesDashboardCrdReconciler(false, caBundlePath)
					Expect(persesDashboardCrdReconciler.SetupWithManager(ctx, mgr, k8sClient, logger)).To(Succeed())

					// provide the API endpoint first
					persesDashboardCrdReconciler.SetDefaultApiConfigs(
						ctx, []ApiConfig{
							{
								Endpoint: ApiEndpointTest,
								Dataset:  DatasetCustomTest,
								Token:    AuthorizationTokenTest,
							},
						}, logger,
					)

					// create the CRD a bit later
					time.Sleep(100 * time.Millisecond)
					Expect(isWatchingPersesDashboardResources(persesDashboardCrdReconciler)).To(BeFalse())
					ensurePersesDashboardCrdExists(ctx)
					// watches are not triggered in unit tests
					persesDashboardCrdReconciler.Create(
						ctx,
						event.TypedCreateEvent[client.Object]{
							Object: persesDashboardCrd,
						},
						&controllertest.TypedQueue[reconcile.Request]{},
					)

					// verify that the controller starts watching when it sees the CRD being created
					Eventually(
						func(g Gomega) {
							g.Expect(isWatchingPersesDashboardResources(persesDashboardCrdReconciler)).To(BeTrue())
						},
					).Should(Succeed())
				},
			)

			It(
				"stops watching Perses dashboards if the CRD is deleted", func() {
					persesDashboardCrdReconciler := createPersesDashboardCrdReconciler(false, caBundlePath)
					ensurePersesDashboardCrdExists(ctx)
					Expect(persesDashboardCrdReconciler.SetupWithManager(ctx, mgr, k8sClient, logger)).To(Succeed())
					Expect(isWatchingPersesDashboardResources(persesDashboardCrdReconciler)).To(BeFalse())
					persesDashboardCrdReconciler.SetDefaultApiConfigs(
						ctx, []ApiConfig{
							{
								Endpoint: ApiEndpointTest,
								Dataset:  DatasetCustomTest,
								Token:    AuthorizationTokenTest,
							},
						}, logger,
					)
					Expect(isWatchingPersesDashboardResources(persesDashboardCrdReconciler)).To(BeTrue())

					deletePersesDashboardCrdIfItExists(ctx)
					// watches are not triggered in unit tests
					persesDashboardCrdReconciler.Delete(
						ctx,
						event.TypedDeleteEvent[client.Object]{
							Object: persesDashboardCrd,
						},
						&controllertest.TypedQueue[reconcile.Request]{},
					)
					Eventually(
						func(g Gomega) {
							Expect(isWatchingPersesDashboardResources(persesDashboardCrdReconciler)).To(BeFalse())
						},
					).Should(Succeed())
				},
			)

			It(
				"tracks per-namespace sync-enabled state and clears it on removal",
				func() {
					crdReconciler := createPersesDashboardCrdReconciler(false, caBundlePath)
					Expect(crdReconciler.SetupWithManager(ctx, mgr, k8sClient, logger)).To(Succeed())
					reconciler := crdReconciler.persesDashboardReconciler
					namespace := TestNamespaceName

					syncDisabled := &dash0v1beta1.Dash0Monitoring{}
					syncDisabled.Spec.SynchronizePersesDashboards = new(bool) // false

					syncEnabled := &dash0v1beta1.Dash0Monitoring{}
					// nil means default=true

					By("First call with sync disabled — stores false, no resync")
					crdReconciler.SetSynchronizationEnabled(ctx, namespace, syncDisabled, logger)
					val, ok := reconciler.namespacedSyncEnabled.Load(namespace)
					Expect(ok).To(BeTrue())
					Expect(val).To(BeFalse())

					By("Second call still disabled — no state change")
					crdReconciler.SetSynchronizationEnabled(ctx, namespace, syncDisabled, logger)
					val, _ = reconciler.namespacedSyncEnabled.Load(namespace)
					Expect(val).To(BeFalse())

					By("Transition to enabled — state updated")
					crdReconciler.SetSynchronizationEnabled(ctx, namespace, syncEnabled, logger)
					val, _ = reconciler.namespacedSyncEnabled.Load(namespace)
					Expect(val).To(BeTrue())

					By("Already enabled — no state change")
					crdReconciler.SetSynchronizationEnabled(ctx, namespace, syncEnabled, logger)
					val, _ = reconciler.namespacedSyncEnabled.Load(namespace)
					Expect(val).To(BeTrue())

					By("RemoveNamespacedApiConfigs does not clear the sync-enabled state")
					reconciler.namespacedApiConfigs.Set(namespace, []ApiConfig{{Endpoint: "x", Token: "t"}})
					crdReconciler.RemoveNamespacedApiConfigs(ctx, namespace, logger)
					val, _ = reconciler.namespacedSyncEnabled.Load(namespace)
					Expect(val).To(BeTrue())

					By("RemoveSynchronizationEnabled clears the sync-enabled state")
					crdReconciler.RemoveSynchronizationEnabled(namespace)
					_, ok = reconciler.namespacedSyncEnabled.Load(namespace)
					Expect(ok).To(BeFalse())
				},
			)

			It(
				"can cope with multiple consecutive create & delete events", func() {
					persesDashboardCrdReconciler := createPersesDashboardCrdReconciler(false, caBundlePath)
					Expect(persesDashboardCrdReconciler.SetupWithManager(ctx, mgr, k8sClient, logger)).To(Succeed())
					persesDashboardCrdReconciler.SetDefaultApiConfigs(
						ctx, []ApiConfig{
							{
								Endpoint: ApiEndpointTest,
								Dataset:  DatasetCustomTest,
								Token:    AuthorizationTokenTest,
							},
						}, logger,
					)

					Expect(isWatchingPersesDashboardResources(persesDashboardCrdReconciler)).To(BeFalse())
					ensurePersesDashboardCrdExists(ctx)
					persesDashboardCrdReconciler.Create(
						ctx,
						event.TypedCreateEvent[client.Object]{
							Object: persesDashboardCrd,
						},
						&controllertest.TypedQueue[reconcile.Request]{},
					)
					Eventually(
						func(g Gomega) {
							g.Expect(isWatchingPersesDashboardResources(persesDashboardCrdReconciler)).To(BeTrue())
						},
					).Should(Succeed())

					deletePersesDashboardCrdIfItExists(ctx)
					persesDashboardCrdReconciler.Delete(
						ctx,
						event.TypedDeleteEvent[client.Object]{
							Object: persesDashboardCrd,
						},
						&controllertest.TypedQueue[reconcile.Request]{},
					)
					Eventually(
						func(g Gomega) {
							g.Expect(isWatchingPersesDashboardResources(persesDashboardCrdReconciler)).To(BeFalse())
						},
					).Should(Succeed())

					ensurePersesDashboardCrdExists(ctx)
					persesDashboardCrdReconciler.Create(
						ctx,
						event.TypedCreateEvent[client.Object]{
							Object: persesDashboardCrd,
						},
						&controllertest.TypedQueue[reconcile.Request]{},
					)
					Eventually(
						func(g Gomega) {
							g.Expect(isWatchingPersesDashboardResources(persesDashboardCrdReconciler)).To(BeTrue())
						},
					).Should(Succeed())
				},
			)

			It("opts in to CRD update events", func() {
				Expect(createPersesDashboardCrdReconciler(true, caBundlePath).WantsCrdUpdateEvents()).To(BeTrue())
				Expect(createPersesDashboardCrdReconciler(false, caBundlePath).WantsCrdUpdateEvents()).To(BeTrue())
			})
		},
	)

	Describe(
		"CRD version selection", func() {

			Context("initial version selection", func() {
				It("prefers v1alpha2 when both versions are provided", func() {
					r := createPersesDashboardCrdReconciler(false, caBundlePath)
					r.OnCrdChange(createPersesDashboardCrdCrdWithVersions([]apiextensionsv1.CustomResourceDefinitionVersion{
						{Name: persesDashboardV1Alpha1, Served: true},
						{Name: persesDashboardV1Alpha2, Served: true},
					}), logger)
					Expect(r.Version()).To(Equal(persesDashboardV1Alpha2))
				})

				It("falls back to v1alpha1 when v1alpha2 is not provided", func() {
					r := createPersesDashboardCrdReconciler(false, caBundlePath)
					r.OnCrdChange(createPersesDashboardCrdCrdWithVersions([]apiextensionsv1.CustomResourceDefinitionVersion{
						{Name: persesDashboardV1Alpha1, Served: true},
					}), logger)
					Expect(r.Version()).To(Equal(persesDashboardV1Alpha1))
				})

				It("falls back to v1alpha1 when v1alpha2 is declared but not served", func() {
					r := createPersesDashboardCrdReconciler(false, caBundlePath)
					r.OnCrdChange(createPersesDashboardCrdCrdWithVersions([]apiextensionsv1.CustomResourceDefinitionVersion{
						{Name: persesDashboardV1Alpha1, Served: true},
						{Name: persesDashboardV1Alpha2, Served: false},
					}), logger)
					Expect(r.Version()).To(Equal(persesDashboardV1Alpha1))
				})

				It("does not mark the CRD as usable when no supported version is served", func() {
					r := createPersesDashboardCrdReconciler(false, caBundlePath)
					r.persesDashboardCrdExists.Store(true)
					r.OnCrdChange(createPersesDashboardCrdCrdWithVersions([]apiextensionsv1.CustomResourceDefinitionVersion{
						{Name: "v1beta1", Served: true},
					}), logger)
					Expect(r.persesDashboardCrdExists.Load()).To(BeFalse())
				})
			})

			Context("when the provided version set changes at runtime", func() {
				AfterEach(func() {
					deletePersesDashboardCrdIfItExists(ctx)
				})

				It("stops the existing watch and restarts on the new preferred version", func() {
					r := createPersesDashboardCrdReconciler(false, caBundlePath)

					// start with actual CRD, which includes v1alpha1, v1alpha2
					ensurePersesDashboardCrdExists(ctx)
					Expect(r.SetupWithManager(ctx, mgr, k8sClient, logger)).To(Succeed())
					r.SetDefaultApiConfigs(ctx, []ApiConfig{
						{
							Endpoint: ApiEndpointTest,
							Dataset:  DatasetCustomTest,
							Token:    AuthorizationTokenTest,
						},
					}, logger)
					Eventually(func(g Gomega) {
						g.Expect(isWatchingPersesDashboardResources(r)).To(BeTrue())
					}).Should(Succeed())
					Expect(r.Version()).To(Equal(persesDashboardV1Alpha2))
					stopFnBefore := r.persesDashboardReconciler.GetControllerStopFunction()
					Expect(stopFnBefore).NotTo(BeNil())

					// Synthesize a CRD update that removes the v1alpha2 entry entirely, emulating a  downgrade from a version >= 0.3.0
					// to an older v1alpha1-only CRD.
					newCrd := persesDashboardCrd.DeepCopy()
					remaining := newCrd.Spec.Versions[:0]
					for _, v := range newCrd.Spec.Versions {
						if v.Name != persesDashboardV1Alpha2 {
							remaining = append(remaining, v)
						}
					}
					newCrd.Spec.Versions = remaining
					r.Update(
						ctx,
						event.TypedUpdateEvent[client.Object]{
							ObjectOld: persesDashboardCrd,
							ObjectNew: newCrd,
						},
						&controllertest.TypedQueue[reconcile.Request]{},
					)

					// Eventually the CRD version is removed and the watch is re-established.
					Eventually(func(g Gomega) {
						g.Expect(r.Version()).To(Equal(persesDashboardV1Alpha1))
						g.Expect(isWatchingPersesDashboardResources(r)).To(BeTrue())
						stopFnAfter := r.persesDashboardReconciler.GetControllerStopFunction()
						g.Expect(stopFnAfter).NotTo(BeNil())
						g.Expect(stopFnAfter).NotTo(BeIdenticalTo(stopFnBefore),
							"expected a fresh controller-stop closure after the restart")
					}).Should(Succeed())
				})

				It("does not restart when the chosen version is unchanged", func() {
					r := createPersesDashboardCrdReconciler(false, caBundlePath)
					ensurePersesDashboardCrdExists(ctx)
					Expect(r.SetupWithManager(ctx, mgr, k8sClient, logger)).To(Succeed())
					r.SetDefaultApiConfigs(ctx, []ApiConfig{
						{
							Endpoint: ApiEndpointTest,
							Dataset:  DatasetCustomTest,
							Token:    AuthorizationTokenTest,
						},
					}, logger)
					Eventually(func(g Gomega) {
						g.Expect(isWatchingPersesDashboardResources(r)).To(BeTrue())
					}).Should(Succeed())
					stopFnBefore := r.persesDashboardReconciler.GetControllerStopFunction()

					// Update with the same CRD (no version change).
					r.Update(
						ctx,
						event.TypedUpdateEvent[client.Object]{
							ObjectOld: persesDashboardCrd,
							ObjectNew: persesDashboardCrd.DeepCopy(),
						},
						&controllertest.TypedQueue[reconcile.Request]{},
					)

					Expect(r.Version()).To(Equal(persesDashboardV1Alpha2))
					Expect(isWatchingPersesDashboardResources(r)).To(BeTrue())
					Expect(r.persesDashboardReconciler.GetControllerStopFunction()).To(BeIdenticalTo(stopFnBefore),
						"expected the original controller-stop closure to be preserved (no restart)")
				})
			})
		},
	)

	Describe(
		"conversion webhook auto-patch", func() {

			AfterEach(func() {
				deletePersesDashboardCrdIfItExists(ctx)
			})

			readCrdConversion := func() *apiextensionsv1.CustomResourceConversion {
				crd := &apiextensionsv1.CustomResourceDefinition{}
				Expect(k8sClient.Get(ctx, PersesDashboardCrdQualifiedName, crd)).To(Succeed())
				return crd.Spec.Conversion
			}

			It("patches the CRD on first observation when no conversion webhook is configured", func() {
				ensurePersesDashboardCrdExists(ctx)
				Expect(readCrdConversion().Strategy).NotTo(Equal(apiextensionsv1.WebhookConverter))

				r := createPersesDashboardCrdReconciler(true, caBundlePath)
				r.ensurePersesConversionWebhookConfigured(ctx, persesDashboardCrd, false, logger)

				conv := readCrdConversion()
				Expect(conv.Strategy).To(Equal(apiextensionsv1.WebhookConverter))
				Expect(conv.Webhook).NotTo(BeNil())
				Expect(conv.Webhook.ClientConfig.Service).NotTo(BeNil())
				Expect(conv.Webhook.ClientConfig.Service.Name).To(Equal(OperatorWebhookServiceName))
				Expect(conv.Webhook.ClientConfig.Service.Namespace).To(Equal(OperatorNamespace))
				Expect(*conv.Webhook.ClientConfig.Service.Path).To(Equal(util.PersesDashboardConversionWebhookPath))
				Expect(*conv.Webhook.ClientConfig.Service.Port).To(Equal(OperatorWebhookServicePort))
				Expect(conv.Webhook.ClientConfig.CABundle).To(Equal(caPEM))
			})

			It("leaves the CRD alone when a foreign conversion webhook is already configured", func() {
				ensurePersesDashboardCrdExists(ctx)
				fresh := &apiextensionsv1.CustomResourceDefinition{}
				Expect(k8sClient.Get(ctx, PersesDashboardCrdQualifiedName, fresh)).To(Succeed())
				patched := fresh.DeepCopy()
				foreignPath := "/foreign-convert"
				foreignPort := int32(8443)
				patched.Spec.Conversion = &apiextensionsv1.CustomResourceConversion{
					Strategy: apiextensionsv1.WebhookConverter,
					Webhook: &apiextensionsv1.WebhookConversion{
						ConversionReviewVersions: []string{"v1"},
						ClientConfig: &apiextensionsv1.WebhookClientConfig{
							CABundle: caPEM,
							Service: &apiextensionsv1.ServiceReference{
								Name:      "perses-operator-webhook-service",
								Namespace: "perses-system",
								Path:      &foreignPath,
								Port:      &foreignPort,
							},
						},
					},
				}
				Expect(k8sClient.Patch(ctx, patched, client.MergeFrom(fresh))).To(Succeed())

				r := createPersesDashboardCrdReconciler(true, caBundlePath)
				r.ensurePersesConversionWebhookConfigured(ctx, patched, false, logger)

				conv := readCrdConversion()
				Expect(conv.Webhook.ClientConfig.Service.Name).To(Equal("perses-operator-webhook-service"))
				Expect(*conv.Webhook.ClientConfig.Service.Path).To(Equal("/foreign-convert"))
			})

			It("does not patch the CRD when auto-patch is disabled", func() {
				ensurePersesDashboardCrdExists(ctx)
				r := createPersesDashboardCrdReconciler(false, caBundlePath)
				r.ensurePersesConversionWebhookConfigured(ctx, persesDashboardCrd, false, logger)

				conv := readCrdConversion()
				Expect(conv == nil || conv.Strategy != apiextensionsv1.WebhookConverter).To(BeTrue())
			})

			It("does not patch the CRD when the CA bundle file is empty", func() {
				ensurePersesDashboardCrdExists(ctx)
				emptyCaBundlePath := caBundlePath + ".empty"
				Expect(os.WriteFile(emptyCaBundlePath, []byte(""), 0o600)).To(Succeed())
				DeferCleanup(func() { Expect(os.Remove(emptyCaBundlePath)).To(Succeed()) })

				r := createPersesDashboardCrdReconciler(true, emptyCaBundlePath)
				r.ensurePersesConversionWebhookConfigured(ctx, persesDashboardCrd, false, logger)

				conv := readCrdConversion()
				Expect(conv == nil || conv.Strategy != apiextensionsv1.WebhookConverter).To(BeTrue())
			})

			It("re-patches the CRD on update if our conversion stanza was wiped", func() {
				ensurePersesDashboardCrdExists(ctx)
				r := createPersesDashboardCrdReconciler(true, caBundlePath)

				r.ensurePersesConversionWebhookConfigured(ctx, persesDashboardCrd, false, logger)
				Expect(readCrdConversion().Strategy).To(Equal(apiextensionsv1.WebhookConverter))

				wiped := &apiextensionsv1.CustomResourceDefinition{}
				Expect(k8sClient.Get(ctx, PersesDashboardCrdQualifiedName, wiped)).To(Succeed())
				patched := wiped.DeepCopy()
				patched.Spec.Conversion = nil
				Expect(k8sClient.Patch(ctx, patched, client.MergeFrom(wiped))).To(Succeed())

				updated := &apiextensionsv1.CustomResourceDefinition{}
				Expect(k8sClient.Get(ctx, PersesDashboardCrdQualifiedName, updated)).To(Succeed())
				r.ensurePersesConversionWebhookConfigured(ctx, updated, true, logger)

				conv := readCrdConversion()
				Expect(conv.Strategy).To(Equal(apiextensionsv1.WebhookConverter))
				Expect(conv.Webhook.ClientConfig.Service.Name).To(Equal(OperatorWebhookServiceName))
			})

			It("does nothing on update when our conversion stanza is still intact", func() {
				ensurePersesDashboardCrdExists(ctx)
				r := createPersesDashboardCrdReconciler(true, caBundlePath)
				r.ensurePersesConversionWebhookConfigured(ctx, persesDashboardCrd, false, logger)

				before := &apiextensionsv1.CustomResourceDefinition{}
				Expect(k8sClient.Get(ctx, PersesDashboardCrdQualifiedName, before)).To(Succeed())
				beforeRV := before.ResourceVersion

				r.ensurePersesConversionWebhookConfigured(ctx, before, true, logger)

				after := &apiextensionsv1.CustomResourceDefinition{}
				Expect(k8sClient.Get(ctx, PersesDashboardCrdQualifiedName, after)).To(Succeed())
				Expect(after.ResourceVersion).To(Equal(beforeRV), "expected no patch when stanza already matches ours")
			})

			It("patches the CRD at startup when it already exists", func() {
				// SetupThirdPartyCrdReconcilerWithManager handles pre-existing CRDs without firing
				// a Create event, so SetupWithManager must invoke the patch directly.
				ensurePersesDashboardCrdExists(ctx)
				Expect(readCrdConversion().Strategy).NotTo(Equal(apiextensionsv1.WebhookConverter))

				r := createPersesDashboardCrdReconciler(true, caBundlePath)
				Expect(r.SetupWithManager(ctx, mgr, k8sClient, logger)).To(Succeed())

				conv := readCrdConversion()
				Expect(conv.Strategy).To(Equal(apiextensionsv1.WebhookConverter))
				Expect(conv.Webhook.ClientConfig.Service.Name).To(Equal(OperatorWebhookServiceName))
			})
		},
	)

	Describe(
		"the Perses dashboard resource reconciler", func() {
			var persesDashboardCrdReconciler *PersesDashboardCrdReconciler
			var persesDashboardReconciler *PersesDashboardReconciler

			BeforeAll(
				func() {
					persesDashboardCrdReconciler = createPersesDashboardCrdReconciler(false, caBundlePath)
					ensurePersesDashboardCrdExists(ctx)

					Expect(persesDashboardCrdReconciler.SetupWithManager(ctx, mgr, k8sClient, logger)).To(Succeed())

					StartProcessingThirdPartySynchronizationQueue(testQueuePersesDashboards, logger)
				},
			)

			BeforeEach(
				func() {
					persesDashboardCrdReconciler.SetDefaultApiConfigs(
						ctx, []ApiConfig{
							{
								Endpoint: ApiEndpointTest,
								Dataset:  DatasetCustomTest,
								Token:    AuthorizationTokenTest,
							},
						}, logger,
					)
					Expect(isWatchingPersesDashboardResources(persesDashboardCrdReconciler)).To(BeTrue())
					persesDashboardReconciler = persesDashboardCrdReconciler.persesDashboardReconciler
				},
			)

			AfterEach(
				func() {
					VerifyNoUnmatchedGockRequests()

					// Make sure the work queue is empty before tearing down the monitoring resource so an in-flight synchronization job
					// from a previous test cannot write its (potentially stale) result to the next test's monitoring resource.
					Eventually(func(g Gomega) {
						g.Expect(testQueuePersesDashboards.Len()).To(Equal(0))
					}, 3*time.Second, 20*time.Millisecond).Should(Succeed())

					DeleteMonitoringResourceIfItExists(ctx, k8sClient)
					persesDashboardCrdReconciler.RemoveNamespacedApiConfigs(ctx, TestNamespaceName, logger)
				},
			)

			AfterAll(
				func() {
					deletePersesDashboardCrdIfItExists(ctx)
					StopProcessingThirdPartySynchronizationQueue(testQueuePersesDashboards, logger)
				},
			)

			It(
				"it ignores Perses dashboard resource changes if no Dash0 monitoring resource exists in the namespace",
				func() {
					expectDashboardPutRequest(clusterId, defaultExpectedPathDashboard)
					defer gock.Off()

					dashboardResource := createDashboardResource()
					persesDashboardReconciler.Create(
						ctx,
						event.TypedCreateEvent[*unstructured.Unstructured]{
							Object: &dashboardResource,
						},
						&controllertest.TypedQueue[reconcile.Request]{},
					)

					Expect(gock.IsPending()).To(BeTrue())
				},
			)

			It(
				"it ignores Perses dashboard resource changes if synchronization is disabled via the Dash0 monitoring resource",
				func() {
					monitoringResource := EnsureMonitoringResourceWithoutExportExistsAndIsAvailable(ctx, k8sClient)
					monitoringResource.Spec.SynchronizePersesDashboards = new(false)
					Expect(k8sClient.Update(ctx, monitoringResource)).To(Succeed())

					expectDashboardPutRequest(clusterId, defaultExpectedPathDashboard)
					defer gock.Off()

					dashboardResource := createDashboardResource()
					persesDashboardReconciler.Create(
						ctx,
						event.TypedCreateEvent[*unstructured.Unstructured]{
							Object: &dashboardResource,
						},
						&controllertest.TypedQueue[reconcile.Request]{},
					)

					verifyNoPersesDashboardSynchronizationResultHasBeenWrittenToMonitoringResourceStatus(ctx, k8sClient)
					Expect(gock.IsPending()).To(BeTrue())
				},
			)

			It(
				"it ignores Perses dashboard resource changes if the API endpoint is not configured", func() {
					EnsureMonitoringResourceWithoutExportExistsAndIsAvailable(ctx, k8sClient)

					expectDashboardPutRequest(clusterId, defaultExpectedPathDashboard)
					defer gock.Off()

					persesDashboardCrdReconciler.RemoveDefaultApiConfigs(ctx, logger)

					dashboardResource := createDashboardResource()
					persesDashboardReconciler.Create(
						ctx,
						event.TypedCreateEvent[*unstructured.Unstructured]{
							Object: &dashboardResource,
						},
						&controllertest.TypedQueue[reconcile.Request]{},
					)

					verifyNoPersesDashboardSynchronizationResultHasBeenWrittenToMonitoringResourceStatus(ctx, k8sClient)
					Expect(gock.IsPending()).To(BeTrue())
				},
			)

			It(
				"creates a dashboard", func() {
					EnsureMonitoringResourceWithoutExportExistsAndIsAvailable(ctx, k8sClient)

					expectDashboardPutRequest(clusterId, defaultExpectedPathDashboard)
					defer gock.Off()

					dashboardResource := createDashboardResource()
					persesDashboardReconciler.Create(
						ctx,
						event.TypedCreateEvent[*unstructured.Unstructured]{
							Object: &dashboardResource,
						},
						&controllertest.TypedQueue[reconcile.Request]{},
					)

					verifyPersesDashboardSynchronizationResultHasBeenWrittenToMonitoringResourceStatus(
						ctx,
						k8sClient,
						defaultExpectedPersesSyncResult(clusterId),
					)
					Expect(gock.IsDone()).To(BeTrue())
				},
			)

			It(
				"creates a dashboard with namespaced config from the monitoring resource", func() {
					monitoringResource := DefaultMonitoringResourceWithCustomApiConfigAndToken(
						MonitoringResourceQualifiedName,
						ApiEndpointTestAlternative,
						DatasetCustomTestAlternative,
						AuthorizationTokenTestAlternative,
					)
					EnsureMonitoringResourceWithSpecExistsAndIsAvailable(ctx, k8sClient, monitoringResource.Spec)

					expectDashboardPutRequestCustom(
						clusterId,
						ExpectedPathDashboardAlternative,
						ApiEndpointTestAlternative,
						DatasetCustomTestAlternative,
						AuthorizationHeaderTestAlternative,
						dashboardOriginPatternAlternative,
					)
					defer gock.Off()

					persesDashboardCrdReconciler.SetNamespacedApiConfigs(
						ctx, TestNamespaceName, []ApiConfig{
							{
								Endpoint: ApiEndpointTestAlternative,
								Dataset:  DatasetCustomTestAlternative,
								Token:    AuthorizationTokenTestAlternative,
							},
						}, logger,
					)

					dashboardResource := createDashboardResource()
					persesDashboardReconciler.Create(
						ctx,
						event.TypedCreateEvent[*unstructured.Unstructured]{
							Object: &dashboardResource,
						},
						&controllertest.TypedQueue[reconcile.Request]{},
					)

					verifyPersesDashboardSynchronizationResultHasBeenWrittenToMonitoringResourceStatus(
						ctx,
						k8sClient,
						expectedPersesSyncResult(
							clusterId,
							ApiEndpointStandardizedTestAlternative,
							DatasetCustomTestAlternative,
							dashboardOriginPatternAlternative,
						),
					)
					Expect(gock.IsDone()).To(BeTrue())
				},
			)

			It(
				"updates a dashboard", func() {
					EnsureMonitoringResourceWithoutExportExistsAndIsAvailable(ctx, k8sClient)

					expectDashboardPutRequest(clusterId, defaultExpectedPathDashboard)
					defer gock.Off()

					dashboardResource := createDashboardResource()
					persesDashboardReconciler.Update(
						ctx,
						event.TypedUpdateEvent[*unstructured.Unstructured]{
							ObjectNew: &dashboardResource,
						},
						&controllertest.TypedQueue[reconcile.Request]{},
					)

					verifyPersesDashboardSynchronizationResultHasBeenWrittenToMonitoringResourceStatus(
						ctx,
						k8sClient,
						defaultExpectedPersesSyncResult(clusterId),
					)
					Expect(gock.IsDone()).To(BeTrue())
				},
			)

			It(
				"updates a dashboard if dash0.com/enable is set but not to \"false\"", func() {
					EnsureMonitoringResourceWithoutExportExistsAndIsAvailable(ctx, k8sClient)

					expectDashboardPutRequest(clusterId, defaultExpectedPathDashboard)
					defer gock.Off()

					dashboardResource := createDashboardResourceWithEnableLabel("whatever")
					persesDashboardReconciler.Update(
						ctx,
						event.TypedUpdateEvent[*unstructured.Unstructured]{
							ObjectNew: &dashboardResource,
						},
						&controllertest.TypedQueue[reconcile.Request]{},
					)

					verifyPersesDashboardSynchronizationResultHasBeenWrittenToMonitoringResourceStatus(
						ctx,
						k8sClient,
						defaultExpectedPersesSyncResult(clusterId),
					)
					Expect(gock.IsDone()).To(BeTrue())
				},
			)

			It(
				"deletes a dashboard", func() {
					EnsureMonitoringResourceWithoutExportExistsAndIsAvailable(ctx, k8sClient)

					expectDashboardDeleteRequestWithHttpStatus(defaultExpectedPathDashboard, http.StatusNotFound)
					defer gock.Off()

					dashboardResource := createDashboardResource()
					persesDashboardReconciler.Delete(
						ctx,
						event.TypedDeleteEvent[*unstructured.Unstructured]{
							Object: &dashboardResource,
						},
						&controllertest.TypedQueue[reconcile.Request]{},
					)

					verifyPersesDashboardSynchronizationResultHasBeenWrittenToMonitoringResourceStatus(
						ctx,
						k8sClient,
						defaultExpectedPersesSyncResult(clusterId),
					)
					Expect(gock.IsDone()).To(BeTrue())
				},
			)

			It(
				"deletes a dashboard on Create (and does not try to create it) if labelled with dash0.com/enable=false",
				func() {
					EnsureMonitoringResourceWithoutExportExistsAndIsAvailable(ctx, k8sClient)

					expectDashboardDeleteRequestWithHttpStatus(defaultExpectedPathDashboard, http.StatusNotFound)
					defer gock.Off()

					dashboardResource := createDashboardResourceWithEnableLabel("false")
					persesDashboardReconciler.Create(
						ctx,
						event.TypedCreateEvent[*unstructured.Unstructured]{
							Object: &dashboardResource,
						},
						&controllertest.TypedQueue[reconcile.Request]{},
					)

					verifyPersesDashboardSynchronizationResultHasBeenWrittenToMonitoringResourceStatus(
						ctx,
						k8sClient,
						defaultExpectedPersesSyncResult(clusterId),
					)
					Expect(gock.IsDone()).To(BeTrue())
				},
			)

			It(
				"deletes a dashboard on Update (and does not try to update it) if labelled with dash0.com/enable=false",
				func() {
					EnsureMonitoringResourceWithoutExportExistsAndIsAvailable(ctx, k8sClient)

					expectDashboardDeleteRequest(defaultExpectedPathDashboard)
					defer gock.Off()

					dashboardResource := createDashboardResourceWithEnableLabel("false")
					persesDashboardReconciler.Update(
						ctx,
						event.TypedUpdateEvent[*unstructured.Unstructured]{
							ObjectNew: &dashboardResource,
						},
						&controllertest.TypedQueue[reconcile.Request]{},
					)

					verifyPersesDashboardSynchronizationResultHasBeenWrittenToMonitoringResourceStatus(
						ctx,
						k8sClient,
						defaultExpectedPersesSyncResult(clusterId),
					)
					Expect(gock.IsDone()).To(BeTrue())
				},
			)

			It(
				"reports validation issues for a dashboard", func() {
					EnsureMonitoringResourceWithoutExportExistsAndIsAvailable(ctx, k8sClient)

					dashboardResource := createDashboardResource()
					spec := dashboardResource.Object["spec"].(map[string]any)
					spec["display"] = "not a map"
					persesDashboardReconciler.Create(
						ctx,
						event.TypedCreateEvent[*unstructured.Unstructured]{
							Object: &dashboardResource,
						},
						&controllertest.TypedQueue[reconcile.Request]{},
					)

					verifyPersesDashboardSynchronizationResultHasBeenWrittenToMonitoringResourceStatus(
						ctx,
						k8sClient,
						dash0common.PersesDashboardSynchronizationResults{
							SynchronizationStatus: dash0common.ThirdPartySynchronizationStatusFailed,
							ValidationIssues:      []string{"spec.display is not a map"},
							SynchronizationResults: []dash0common.PersesDashboardSynchronizationResultPerEndpointAndDataset{
								{
									Dash0ApiEndpoint:     ApiEndpointStandardizedTest,
									Dash0Origin:          "",
									Dash0Dataset:         DatasetCustomTest,
									SynchronizationError: "",
								},
							},
						},
					)
				},
			)

			It(
				"reports http errors when synchronizing a dashboard", func() {
					EnsureMonitoringResourceWithoutExportExistsAndIsAvailable(ctx, k8sClient)

					gock.New(ApiEndpointTest).
						Put(defaultExpectedPathDashboard).
						MatchParam("dataset", DatasetCustomTest).
						Times(3).
						Reply(503).
						JSON(map[string]string{})
					defer gock.Off()

					dashboardResource := createDashboardResource()
					persesDashboardReconciler.Create(
						ctx,
						event.TypedCreateEvent[*unstructured.Unstructured]{
							Object: &dashboardResource,
						},
						&controllertest.TypedQueue[reconcile.Request]{},
					)

					verifyPersesDashboardSynchronizationResultHasBeenWrittenToMonitoringResourceStatus(
						ctx,
						k8sClient,
						dash0common.PersesDashboardSynchronizationResults{
							SynchronizationStatus: dash0common.ThirdPartySynchronizationStatusFailed,
							ValidationIssues:      nil,
							SynchronizationResults: []dash0common.PersesDashboardSynchronizationResultPerEndpointAndDataset{
								{
									Dash0ApiEndpoint:     ApiEndpointStandardizedTest,
									Dash0Origin:          "",
									Dash0Dataset:         DatasetCustomTest,
									SynchronizationError: "^unexpected status code 503 when trying to synchronize the dashboard \"test-dashboard\": PUT https://api.dash0.com/api/dashboards/dash0-operator_.*_test-dataset_test-namespace_test-dashboard\\?dataset=test-dataset, response body is {}\n$",
									HttpStatusCode:       503,
								},
							},
						},
					)
					Expect(gock.IsDone()).To(BeTrue())
				},
			)
		},
	)

	Describe(
		"mapping dashboard resources to http requests", func() {

			type dashboardToRequestTestConfig struct {
				dashboard           string
				expectedName        string
				expectedDescription *string
				expectedAnnotations map[string]string
			}

			var persesDashboardReconciler *PersesDashboardReconciler

			BeforeEach(
				func() {
					persesDashboardReconciler = &PersesDashboardReconciler{}
				},
			)

			DescribeTable(
				"maps both CRD versions", func(testConfig dashboardToRequestTestConfig) {
					dashboard := map[string]any{}
					Expect(yaml.Unmarshal([]byte(testConfig.dashboard), &dashboard)).To(Succeed())
					apiConfig := ApiConfig{}
					preconditionValidationResult := &preconditionValidationResult{
						k8sName:             "perses-dashboard",
						k8sNamespace:        TestNamespaceName,
						resource:            dashboard,
						validatedApiConfigs: []ValidatedApiConfigAndToken{{}},
					}
					resourceToRequestsResult :=
						persesDashboardReconciler.MapResourceToHttpRequests(
							preconditionValidationResult,
							apiConfig,
							upsertAction,
							logger,
						)
					Expect(resourceToRequestsResult.TotalProcessed()).To(Equal(1))
					Expect(resourceToRequestsResult.OriginsInResource).To(BeNil())
					Expect(resourceToRequestsResult.ValidationIssues).To(BeNil())
					Expect(resourceToRequestsResult.SynchronizationErrors).To(BeNil())

					Expect(resourceToRequestsResult.ApiRequests).To(HaveLen(1))
					apiRequest := resourceToRequestsResult.ApiRequests[0]
					Expect(apiRequest.ItemName).To(Equal("perses-dashboard"))
					req := apiRequest.Request
					defer func() {
						_ = req.Body.Close()
					}()
					body, err := io.ReadAll(req.Body)
					Expect(err).ToNot(HaveOccurred())
					resultingDashboardInRequest := map[string]any{}
					Expect(json.Unmarshal(body, &resultingDashboardInRequest)).To(Succeed())
					Expect(
						ReadFromMap(
							resultingDashboardInRequest,
							[]string{"spec", "display", "name"},
						),
					).To(Equal(testConfig.expectedName))
					if testConfig.expectedDescription != nil {
						Expect(
							ReadFromMap(
								resultingDashboardInRequest,
								[]string{"spec", "display", "description"},
							),
						).To(Equal(*testConfig.expectedDescription))
					} else {
						Expect(ReadFromMap(resultingDashboardInRequest, []string{"spec", "display", "description"})).To(BeNil())
					}

					Expect(resultingDashboardInRequest["metadata"]).ToNot(BeNil())
					Expect(ReadFromMap(resultingDashboardInRequest, []string{"metadata", "name"})).To(Equal("perses-dashboard"))

					if testConfig.expectedAnnotations != nil {
						annotationsRaw := ReadFromMap(resultingDashboardInRequest, []string{"metadata", "annotations"})
						Expect(annotationsRaw).ToNot(BeNil())
						annotations := annotationsRaw.(map[string]any)
						Expect(annotations).To(HaveLen(len(testConfig.expectedAnnotations)))
						for expectedKey, expectedValue := range testConfig.expectedAnnotations {
							value, ok := annotations[expectedKey]
							Expect(ok).To(BeTrue())
							Expect(value).To(Equal(expectedValue))
						}
					} else {
						Expect(ReadFromMap(resultingDashboardInRequest, []string{"metadata", "annotations"})).To(BeNil())
					}
				},
				Entry(
					"should map v1alpha1", dashboardToRequestTestConfig{
						dashboard: `
apiVersion: perses.dev/v1alpha1
kind: PersesDashboard
metadata:
  name: perses-dashboard
spec:
  display:
    name: Perses Dashboard Example
    description: This is an example dashboard.
  duration: 5m
`,
						expectedName:        "Perses Dashboard Example",
						expectedDescription: new("This is an example dashboard."),
					},
				),
				Entry(
					"should map v1alpha2", dashboardToRequestTestConfig{
						dashboard: `
apiVersion: perses.dev/v1alpha2
kind: PersesDashboard
metadata:
  name: perses-dashboard
spec:
  config:
    display:
      name: Perses Dashboard Example
      description: This is an example dashboard.
    duration: 5m
`,
						expectedName:        "Perses Dashboard Example",
						expectedDescription: new("This is an example dashboard."),
					},
				),
				Entry(
					"should add name to v1alpha1 with display but without name", dashboardToRequestTestConfig{
						dashboard: `
apiVersion: perses.dev/v1alpha1
kind: PersesDashboard
metadata:
  name: perses-dashboard
spec:
  display:
    description: This is an example dashboard.
  duration: 5m
`,
						expectedName:        fmt.Sprintf("%s/perses-dashboard", TestNamespaceName),
						expectedDescription: new("This is an example dashboard."),
					},
				),
				Entry(
					"should add name to v1alpha2 with display but without name", dashboardToRequestTestConfig{
						dashboard: `
apiVersion: perses.dev/v1alpha2
kind: PersesDashboard
metadata:
  name: perses-dashboard
spec:
  config:
    display:
      description: This is an example dashboard.
    duration: 5m
`,
						expectedName:        fmt.Sprintf("%s/perses-dashboard", TestNamespaceName),
						expectedDescription: new("This is an example dashboard."),
					},
				),
				Entry(
					"should add name to v1alpha1 without display", dashboardToRequestTestConfig{
						dashboard: `
apiVersion: perses.dev/v1alpha1
kind: PersesDashboard
metadata:
  name: perses-dashboard
spec:
  duration: 5m
`,
						expectedName:        fmt.Sprintf("%s/perses-dashboard", TestNamespaceName),
						expectedDescription: nil,
					},
				),
				Entry(
					"should add name to v1alpha2 without display", dashboardToRequestTestConfig{
						dashboard: `
apiVersion: perses.dev/v1alpha2
kind: PersesDashboard
metadata:
  name: perses-dashboard
spec:
  config:
    duration: 5m
`,
						expectedName:        fmt.Sprintf("%s/perses-dashboard", TestNamespaceName),
						expectedDescription: nil,
					},
				),
				Entry(
					"should send annotations with v1alpha1", dashboardToRequestTestConfig{
						dashboard: `
apiVersion: perses.dev/v1alpha1
kind: PersesDashboard
metadata:
  name: perses-dashboard
  annotations:
    dash0com/annotation1: value1
    dash0com/annotation2: value2
spec:
  display:
    name: Perses Dashboard Example
    description: This is an example dashboard.
  duration: 5m
`,
						expectedName:        "Perses Dashboard Example",
						expectedDescription: new("This is an example dashboard."),
						expectedAnnotations: map[string]string{
							"dash0com/annotation1": "value1",
							"dash0com/annotation2": "value2",
						},
					},
				),
				Entry(
					"should send annotations with v1alpha2", dashboardToRequestTestConfig{
						dashboard: `
apiVersion: perses.dev/v1alpha2
kind: PersesDashboard
metadata:
  name: perses-dashboard
  annotations:
    dash0com/annotation1: value1
    dash0com/annotation2: value2
spec:
  config:
    display:
      name: Perses Dashboard Example
      description: This is an example dashboard.
    duration: 5m
`,
						expectedName:        "Perses Dashboard Example",
						expectedDescription: new("This is an example dashboard."),
						expectedAnnotations: map[string]string{
							"dash0com/annotation1": "value1",
							"dash0com/annotation2": "value2",
						},
					},
				),
			)
		},
	)
},
)

func createPersesDashboardCrdReconciler(autoPatch bool, caBundlePath string) *PersesDashboardCrdReconciler {
	crdReconciler := NewPersesDashboardCrdReconciler(
		k8sClient,
		testQueuePersesDashboards,
		&DummyLeaderElectionAware{Leader: true},
		TestHTTPClient(),
		PersesDashboardConversionWebhookSettings{
			// Default to disabled in tests; individual tests that exercise the patch logic set this back to true.
			AutoPatchConversionWebhook: autoPatch,
			OperatorNamespace:          OperatorNamespace,
			WebhookServiceName:         OperatorWebhookServiceName,
			WebhookServicePort:         OperatorWebhookServicePort,
			CaBundlePath:               caBundlePath,
		},
	)

	// We create the controller multiple times in tests, this option is required, otherwise the controller
	// runtime will complain.
	crdReconciler.skipNameValidation = true
	return crdReconciler
}

func expectDashboardPutRequestCustom(
	clusterId string,
	expectedPath string,
	endpoint string,
	dataset string,
	authHeader string,
	originPattern string,
) {
	gock.New(endpoint).
		Put(expectedPath).
		MatchHeader("Authorization", authHeader).
		MatchParam("dataset", dataset).
		Times(1).
		Reply(200).
		JSON(dashboardPutResponse(clusterId, dataset, originPattern))
}

func expectDashboardPutRequest(clusterId string, expectedPath string) {
	expectDashboardPutRequestCustom(
		clusterId,
		expectedPath,
		ApiEndpointTest,
		DatasetCustomTest,
		AuthorizationHeaderTest,
		dashboardOriginPattern,
	)
}

func dashboardPutResponse(clusterId string, dataset string, originPattern string) map[string]any {
	return map[string]any{
		"metadata": map[string]any{
			"dash0Extensions": map[string]any{
				"id":      fmt.Sprintf(originPattern, clusterId),
				"dataset": dataset,
			},
		},
	}
}

func expectedPersesSyncResult(
	clusterId string,
	apiEndpoint string,
	dataset string,
	originPattern string,
) dash0common.PersesDashboardSynchronizationResults {
	return dash0common.PersesDashboardSynchronizationResults{
		SynchronizationStatus: dash0common.ThirdPartySynchronizationStatusSuccessful,
		ValidationIssues:      nil,
		SynchronizationResults: []dash0common.PersesDashboardSynchronizationResultPerEndpointAndDataset{
			{
				Dash0ApiEndpoint:     apiEndpoint,
				Dash0Origin:          fmt.Sprintf(originPattern, clusterId),
				Dash0Dataset:         dataset,
				SynchronizationError: "",
			},
		},
	}
}

func defaultExpectedPersesSyncResult(clusterId string) dash0common.PersesDashboardSynchronizationResults {
	return expectedPersesSyncResult(clusterId, ApiEndpointStandardizedTest, DatasetCustomTest, dashboardOriginPattern)
}

func expectDashboardDeleteRequest(expectedPath string) {
	expectDashboardDeleteRequestWithHttpStatus(expectedPath, http.StatusOK)
}

func expectDashboardDeleteRequestWithHttpStatus(expectedPath string, status int) {
	gock.New(ApiEndpointTest).
		Delete(expectedPath).
		MatchHeader("Authorization", AuthorizationHeaderTest).
		MatchParam("dataset", DatasetCustomTest).
		Times(1).
		Reply(status)
}

func createDashboardResource() unstructured.Unstructured {
	return createDashboardResourceWithEnableLabel("")
}

func createDashboardResourceWithEnableLabel(dash0EnableLabelValue string) unstructured.Unstructured {
	objectMeta := metav1.ObjectMeta{
		Name:      "test-dashboard",
		Namespace: TestNamespaceName,
	}
	if dash0EnableLabelValue != "" {
		objectMeta.Labels = map[string]string{
			"dash0.com/enable": dash0EnableLabelValue,
		}
	}
	dashboard := persesv1alpha1.PersesDashboard{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "perses.dev/v1alpha1",
			Kind:       "PersesDashboard",
		},
		ObjectMeta: objectMeta,
		Spec:       persesv1alpha1.Dashboard{},
	}
	marshalled, err := json.Marshal(dashboard)
	Expect(err).NotTo(HaveOccurred())
	unstructuredObject := unstructured.Unstructured{}
	err = json.Unmarshal(marshalled, &unstructuredObject)
	Expect(err).NotTo(HaveOccurred())
	return unstructuredObject
}

func ensurePersesDashboardCrdExists(ctx context.Context) {
	persesDashboardCrd = EnsurePersesDashboardCrdExists(
		ctx,
		k8sClient,
	)
}

func createPersesDashboardCrdCrdWithVersions(versions []apiextensionsv1.CustomResourceDefinitionVersion) *apiextensionsv1.CustomResourceDefinition {
	return &apiextensionsv1.CustomResourceDefinition{
		Spec: apiextensionsv1.CustomResourceDefinitionSpec{
			Versions: versions,
		},
	}
}

func generateTestCaPem() []byte {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	Expect(err).NotTo(HaveOccurred())
	template := &x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: "dash0-operator-test-ca"},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(time.Hour),
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageDigitalSignature,
		BasicConstraintsValid: true,
		IsCA:                  true,
	}
	der, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	Expect(err).NotTo(HaveOccurred())
	return pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
}

func deletePersesDashboardCrdIfItExists(ctx context.Context) {
	if persesDashboardCrd != nil {
		err := k8sClient.Delete(
			ctx, persesDashboardCrd, &client.DeleteOptions{
				GracePeriodSeconds: new(int64),
			},
		)
		if err != nil && apierrors.IsNotFound(err) {
			return
		} else if err != nil {
			Expect(err).NotTo(HaveOccurred())
		}

		Eventually(
			func(g Gomega) {
				err := k8sClient.Get(ctx, PersesDashboardCrdQualifiedName, &apiextensionsv1.CustomResourceDefinition{})
				g.Expect(err).To(HaveOccurred())
				g.Expect(apierrors.IsNotFound(err)).To(BeTrue())
			},
			30*time.Second, 100*time.Millisecond,
		).Should(Succeed())

		persesDashboardCrd = nil
	}
}

func isWatchingPersesDashboardResources(persesDashboardCrdReconciler *PersesDashboardCrdReconciler) bool {
	dashboardReconciler := persesDashboardCrdReconciler.persesDashboardReconciler
	dashboardReconciler.ControllerStopFunctionLock().Lock()
	defer dashboardReconciler.ControllerStopFunctionLock().Unlock()
	return dashboardReconciler.IsWatching()
}

func verifyPersesDashboardSynchronizationResultHasBeenWrittenToMonitoringResourceStatus(
	ctx context.Context,
	k8sClient client.Client,
	expectedResult dash0common.PersesDashboardSynchronizationResults,
) {
	Eventually(
		func(g Gomega) {
			monRes := LoadMonitoringResourceOrFail(ctx, k8sClient, g)
			results := monRes.Status.PersesDashboardSynchronizationResults
			g.Expect(results).NotTo(BeNil())
			g.Expect(results).To(HaveLen(1))
			result := results[fmt.Sprintf("%s/%s", TestNamespaceName, "test-dashboard")]
			g.Expect(result.SynchronizationResults).To(HaveLen(1))
			g.Expect(result).NotTo(BeNil())
			actualSyncResult := &result.SynchronizationResults[0]
			expectedSyncResult := &expectedResult.SynchronizationResults[0]
			if expectedSyncResult.SynchronizationError != "" {
				// http errors contain a different random path for each test execution
				g.Expect(actualSyncResult.SynchronizationError).To(MatchRegexp(expectedSyncResult.SynchronizationError))
			}
			g.Expect(actualSyncResult.Dash0Origin).To(Equal(expectedSyncResult.Dash0Origin))

			// The HTTP status code for a failed synchronization is only verified when the expectation provides it
			// (most tests do not care about the exact code); otherwise it is ignored below.
			if expectedSyncResult.HttpStatusCode != 0 {
				g.Expect(actualSyncResult.HttpStatusCode).To(Equal(expectedSyncResult.HttpStatusCode))
			}

			// we do not verify the exact timestamp
			expectedResult.SynchronizedAt = result.SynchronizedAt
			// errors have been verified using regex
			actualSyncResult.SynchronizationError = ""
			expectedSyncResult.SynchronizationError = ""
			// status code has been verified above (if provided)
			actualSyncResult.HttpStatusCode = 0
			expectedSyncResult.HttpStatusCode = 0

			g.Expect(result).To(Equal(expectedResult))
		},
	).Should(Succeed())
}

func verifyNoPersesDashboardSynchronizationResultHasBeenWrittenToMonitoringResourceStatus(
	ctx context.Context,
	k8sClient client.Client,
) {
	Consistently(
		func(g Gomega) {
			monRes := LoadMonitoringResourceOrFail(ctx, k8sClient, g)
			results := monRes.Status.PersesDashboardSynchronizationResults
			g.Expect(results).To(BeNil())
		}, 200*time.Millisecond, 50*time.Millisecond,
	).Should(Succeed())
}
