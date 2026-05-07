// SPDX-FileCopyrightText: Copyright 2026 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package controller

import (
	"context"
	"fmt"
	"net/http"
	"time"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	dash0common "github.com/dash0hq/dash0-operator/api/operator/common"
	dash0v1alpha1 "github.com/dash0hq/dash0-operator/api/operator/v1alpha1"
	"github.com/dash0hq/dash0-operator/internal/util/cluster"
	"github.com/dash0hq/dash0-operator/internal/util/logd"

	"github.com/h2non/gock"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	. "github.com/dash0hq/dash0-operator/test/util"
)

const (
	signalToMetricsName        = "test-signal-to-metrics"
	signalToMetricsApiBasePath = "/api/signal-to-metrics/"

	signalToMetricsId            = "signal-to-metrics-id"
	signalToMetricsOriginPattern = "dash0-operator_%s_test-dataset_test-namespace_test-signal-to-metrics"
)

var (
	defaultExpectedPathSignalToMetrics = fmt.Sprintf(
		"%s.*%s",
		signalToMetricsApiBasePath,
		"dash0-operator_.*_test-dataset_test-namespace_test-signal-to-metrics",
	)
)

var _ = Describe(
	"The Signal-to-Metrics controller", Ordered, func() {
		var (
			testStartedAt time.Time
			clusterId     string
		)

		ctx := context.Background()
		logger := logd.FromContext(ctx)

		BeforeAll(
			func() {
				EnsureTestNamespaceExists(ctx, k8sClient)
				EnsureOperatorNamespaceExists(ctx, k8sClient)
				clusterId = string(cluster.ReadPseudoClusterUid(ctx, k8sClient, logger))
			},
		)

		BeforeEach(
			func() {
				testStartedAt = time.Now()
			},
		)

		AfterEach(
			func() {
				DeleteMonitoringResource(ctx, k8sClient)
			},
		)

		Describe(
			"the signal-to-metrics reconciler", func() {
				var signalToMetricsReconciler *SignalToMetricsReconciler

				BeforeEach(
					func() {
						signalToMetricsReconciler = createSignalToMetricsReconciler(clusterId)
						signalToMetricsReconciler.defaultApiConfigs.Set(
							[]ApiConfig{
								{
									Endpoint: ApiEndpointTest,
									Dataset:  DatasetCustomTest,
									Token:    AuthorizationTokenTest,
								},
							},
						)
					},
				)

				AfterEach(
					func() {
						VerifyNoUnmatchedGockRequests()
						DeleteMonitoringResourceIfItExists(ctx, k8sClient)
						deleteSignalToMetricsResourceIfItExists(ctx, k8sClient, TestNamespaceName, signalToMetricsName)
					},
				)

				It(
					"ignores signal-to-metrics resource changes if no Dash0 monitoring resource exists in the namespace",
					func() {
						expectSignalToMetricsPutRequest(clusterId, defaultExpectedPathSignalToMetrics)
						defer gock.Off()

						resource := createSignalToMetricsResource(TestNamespaceName, signalToMetricsName)
						Expect(k8sClient.Create(ctx, resource)).To(Succeed())

						Expect(gock.IsPending()).To(BeTrue())
						verifySignalToMetricsHasNoSynchronizationStatus(
							ctx,
							k8sClient,
							TestNamespaceName,
							signalToMetricsName,
						)
					},
				)

				It(
					"ignores signal-to-metrics resource changes if no API config is available",
					func() {
						EnsureMonitoringResourceWithoutExportExistsAndIsAvailable(ctx, k8sClient)

						expectSignalToMetricsPutRequest(clusterId, defaultExpectedPathSignalToMetrics)
						defer gock.Off()

						signalToMetricsReconciler.RemoveDefaultApiConfigs(ctx, logger)

						resource := createSignalToMetricsResource(TestNamespaceName, signalToMetricsName)
						Expect(k8sClient.Create(ctx, resource)).To(Succeed())
						result, err := signalToMetricsReconciler.Reconcile(
							ctx, reconcile.Request{
								NamespacedName: types.NamespacedName{
									Namespace: TestNamespaceName,
									Name:      signalToMetricsName,
								},
							},
						)
						Expect(err).NotTo(HaveOccurred())
						Expect(result).To(Equal(reconcile.Result{}))

						Expect(gock.IsPending()).To(BeTrue())
						verifySignalToMetricsHasNoSynchronizationStatus(
							ctx,
							k8sClient,
							TestNamespaceName,
							signalToMetricsName,
						)
					},
				)

				It(
					"synchronizes the signal-to-metrics resource via PUT and writes status back",
					func() {
						EnsureMonitoringResourceWithoutExportExistsAndIsAvailable(ctx, k8sClient)

						expectSignalToMetricsPutRequest(clusterId, defaultExpectedPathSignalToMetrics)
						defer gock.Off()

						resource := createSignalToMetricsResource(TestNamespaceName, signalToMetricsName)
						Expect(k8sClient.Create(ctx, resource)).To(Succeed())
						_, err := signalToMetricsReconciler.Reconcile(
							ctx, reconcile.Request{
								NamespacedName: types.NamespacedName{
									Namespace: TestNamespaceName,
									Name:      signalToMetricsName,
								},
							},
						)
						Expect(err).NotTo(HaveOccurred())

						verifySignalToMetricsSynchronizationStatus(
							ctx,
							k8sClient,
							TestNamespaceName,
							signalToMetricsName,
							dash0common.Dash0ApiResourceSynchronizationStatusSuccessful,
							testStartedAt,
							signalToMetricsId,
							fmt.Sprintf(signalToMetricsOriginPattern, clusterId),
							ApiEndpointStandardizedTest,
							DatasetCustomTest,
							"",
						)
					},
				)

				It(
					"issues a DELETE when the dash0.com/enable=false label is set",
					func() {
						EnsureMonitoringResourceWithoutExportExistsAndIsAvailable(ctx, k8sClient)

						expectSignalToMetricsDeleteRequest(defaultExpectedPathSignalToMetrics)
						defer gock.Off()

						resource := createSignalToMetricsResourceWithEnableLabel(
							TestNamespaceName,
							signalToMetricsName,
							"false",
						)
						Expect(k8sClient.Create(ctx, resource)).To(Succeed())
						_, err := signalToMetricsReconciler.Reconcile(
							ctx, reconcile.Request{
								NamespacedName: types.NamespacedName{
									Namespace: TestNamespaceName,
									Name:      signalToMetricsName,
								},
							},
						)
						Expect(err).NotTo(HaveOccurred())

						Expect(gock.IsPending()).To(BeFalse())
					},
				)

				It(
					"issues a DELETE when the resource has been deleted",
					func() {
						EnsureMonitoringResourceWithoutExportExistsAndIsAvailable(ctx, k8sClient)

						expectSignalToMetricsDeleteRequest(defaultExpectedPathSignalToMetrics)
						defer gock.Off()

						resource := createSignalToMetricsResource(TestNamespaceName, signalToMetricsName)
						Expect(k8sClient.Create(ctx, resource)).To(Succeed())
						Expect(k8sClient.Delete(ctx, resource)).To(Succeed())

						result, err := signalToMetricsReconciler.Reconcile(
							ctx, reconcile.Request{
								NamespacedName: types.NamespacedName{
									Namespace: TestNamespaceName,
									Name:      signalToMetricsName,
								},
							},
						)
						Expect(err).NotTo(HaveOccurred())
						Expect(result).To(Equal(reconcile.Result{}))

						// no status verification — the resource has been deleted, so there is nothing to write status to
						Expect(gock.IsDone()).To(BeTrue())
					},
				)

				It(
					"updates a signal-to-metrics rule",
					func() {
						EnsureMonitoringResourceWithoutExportExistsAndIsAvailable(ctx, k8sClient)

						expectSignalToMetricsPutRequest(clusterId, defaultExpectedPathSignalToMetrics)
						defer gock.Off()

						resource := createSignalToMetricsResource(TestNamespaceName, signalToMetricsName)
						Expect(k8sClient.Create(ctx, resource)).To(Succeed())

						resource.Spec.Display.Name = "Updated rule name"
						Expect(k8sClient.Update(ctx, resource)).To(Succeed())

						_, err := signalToMetricsReconciler.Reconcile(
							ctx, reconcile.Request{
								NamespacedName: types.NamespacedName{
									Namespace: TestNamespaceName,
									Name:      signalToMetricsName,
								},
							},
						)
						Expect(err).NotTo(HaveOccurred())

						verifySignalToMetricsSynchronizationStatus(
							ctx,
							k8sClient,
							TestNamespaceName,
							signalToMetricsName,
							dash0common.Dash0ApiResourceSynchronizationStatusSuccessful,
							testStartedAt,
							signalToMetricsId,
							fmt.Sprintf(signalToMetricsOriginPattern, clusterId),
							ApiEndpointStandardizedTest,
							DatasetCustomTest,
							"",
						)
						Expect(gock.IsDone()).To(BeTrue())
					},
				)

				It(
					"reports HTTP errors when synchronizing fails",
					func() {
						EnsureMonitoringResourceWithoutExportExistsAndIsAvailable(ctx, k8sClient)

						gock.New(ApiEndpointTest).
							Put(defaultExpectedPathSignalToMetrics).
							MatchParam("dataset", DatasetCustomTest).
							Times(3).
							Reply(503).
							JSON(map[string]string{})
						defer gock.Off()

						resource := createSignalToMetricsResource(TestNamespaceName, signalToMetricsName)
						Expect(k8sClient.Create(ctx, resource)).To(Succeed())

						_, err := signalToMetricsReconciler.Reconcile(
							ctx, reconcile.Request{
								NamespacedName: types.NamespacedName{
									Namespace: TestNamespaceName,
									Name:      signalToMetricsName,
								},
							},
						)
						Expect(err).NotTo(HaveOccurred())

						verifySignalToMetricsSynchronizationStatus(
							ctx,
							k8sClient,
							TestNamespaceName,
							signalToMetricsName,
							dash0common.Dash0ApiResourceSynchronizationStatusFailed,
							testStartedAt,
							"",
							"",
							ApiEndpointStandardizedTest,
							DatasetCustomTest,
							"unexpected status code 503",
						)
					},
				)

				It(
					"uses the namespaced API config from the monitoring resource when set",
					func() {
						monitoringResource := DefaultMonitoringResourceWithCustomApiConfigAndToken(
							MonitoringResourceQualifiedName,
							ApiEndpointTestAlternative,
							DatasetCustomTestAlternative,
							AuthorizationTokenTestAlternative,
						)
						EnsureMonitoringResourceWithSpecExistsAndIsAvailable(ctx, k8sClient, monitoringResource.Spec)

						//nolint:lll
						expectedPathAlt := fmt.Sprintf(
							"%s.*%s",
							signalToMetricsApiBasePath,
							"dash0-operator_.*_test-dataset-alt_test-namespace_test-signal-to-metrics",
						)
						originPatternAlt := "dash0-operator_%s_test-dataset-alt_test-namespace_test-signal-to-metrics"

						gock.New(ApiEndpointTestAlternative).
							Put(expectedPathAlt).
							MatchHeader("Authorization", AuthorizationHeaderTestAlternative).
							MatchParam("dataset", DatasetCustomTestAlternative).
							Times(1).
							Reply(200).
							JSON(map[string]any{
								"metadata": map[string]any{
									"labels": map[string]any{
										"dash0.com/id":      signalToMetricsId,
										"dash0.com/origin":  fmt.Sprintf(originPatternAlt, clusterId),
										"dash0.com/dataset": DatasetCustomTestAlternative,
									},
								},
							})
						defer gock.Off()

						resource := createSignalToMetricsResource(TestNamespaceName, signalToMetricsName)
						Expect(k8sClient.Create(ctx, resource)).To(Succeed())

						signalToMetricsReconciler.SetNamespacedApiConfigs(
							ctx, TestNamespaceName, []ApiConfig{
								{
									Endpoint: ApiEndpointTestAlternative,
									Dataset:  DatasetCustomTestAlternative,
									Token:    AuthorizationTokenTestAlternative,
								},
							}, logger,
						)

						verifySignalToMetricsSynchronizationStatus(
							ctx,
							k8sClient,
							TestNamespaceName,
							signalToMetricsName,
							dash0common.Dash0ApiResourceSynchronizationStatusSuccessful,
							testStartedAt,
							signalToMetricsId,
							fmt.Sprintf(originPatternAlt, clusterId),
							ApiEndpointStandardizedTestAlternative,
							DatasetCustomTestAlternative,
							"",
						)
						Expect(gock.IsDone()).To(BeTrue())
					},
				)
			},
		)
	},
)

func createSignalToMetricsReconciler(clusterId string) *SignalToMetricsReconciler {
	return NewSignalToMetricsReconciler(
		k8sClient,
		types.UID(clusterId),
		leaderElectionAware,
		TestHTTPClient(),
	)
}

func expectSignalToMetricsPutRequest(clusterId string, expectedPath string) {
	gock.New(ApiEndpointTest).
		Put(expectedPath).
		MatchHeader("Authorization", AuthorizationHeaderTest).
		MatchParam("dataset", DatasetCustomTest).
		Times(1).
		Reply(200).
		JSON(map[string]any{
			"metadata": map[string]any{
				"labels": map[string]any{
					"dash0.com/id":      signalToMetricsId,
					"dash0.com/origin":  fmt.Sprintf(signalToMetricsOriginPattern, clusterId),
					"dash0.com/dataset": DatasetCustomTest,
				},
			},
		})
}

func expectSignalToMetricsDeleteRequest(expectedPath string) {
	gock.New(ApiEndpointTest).
		Delete(expectedPath).
		MatchHeader("Authorization", AuthorizationHeaderTest).
		MatchParam("dataset", DatasetCustomTest).
		Times(1).
		Reply(http.StatusOK)
}

//nolint:unparam // signature kept symmetric with sibling reconciler helpers; multi-namespace cases may use it later
func createSignalToMetricsResource(namespace string, name string) *dash0v1alpha1.Dash0SignalToMetrics {
	return createSignalToMetricsResourceWithEnableLabel(namespace, name, "")
}

func createSignalToMetricsResourceWithEnableLabel(
	namespace string,
	name string,
	dash0EnableLabelValue string,
) *dash0v1alpha1.Dash0SignalToMetrics {
	objectMeta := metav1.ObjectMeta{
		Name:      name,
		Namespace: namespace,
	}
	if dash0EnableLabelValue != "" {
		objectMeta.Labels = map[string]string{
			"dash0.com/enable": dash0EnableLabelValue,
		}
	}
	return &dash0v1alpha1.Dash0SignalToMetrics{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "operator.dash0.com/v1alpha1",
			Kind:       "Dash0SignalToMetrics",
		},
		ObjectMeta: objectMeta,
		Spec: dash0v1alpha1.Dash0SignalToMetricsSpec{
			Enabled: true,
			Display: dash0v1alpha1.Dash0SignalToMetricsDisplay{
				Name: "Test Signal-to-Metrics Rule",
			},
			Match: dash0v1alpha1.Dash0SignalToMetricsMatch{
				Signal: dash0v1alpha1.Dash0SignalToMetricsSignalTypeSpans,
				Filters: []dash0v1alpha1.Dash0SignalToMetricsAttributeFilter{
					{
						Key:      "service.name",
						Operator: "is",
						Value:    stringPtr("checkout-service"),
					},
				},
			},
			Output: dash0v1alpha1.Dash0SignalToMetricsOutput{
				Name:     "checkout.request.duration",
				Interval: "60s",
			},
		},
	}
}

func stringPtr(s string) *string {
	return &s
}

func deleteSignalToMetricsResourceIfItExists(
	ctx context.Context,
	k8sClient client.Client,
	namespace string,
	name string,
) {
	resource := &dash0v1alpha1.Dash0SignalToMetrics{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
	}
	err := k8sClient.Delete(
		ctx, resource, &client.DeleteOptions{
			GracePeriodSeconds: new(int64),
		},
	)
	if err != nil && !apierrors.IsNotFound(err) {
		Expect(err).NotTo(HaveOccurred())
	}
}

//nolint:unparam // signature kept symmetric with sibling reconciler helpers; multi-namespace cases may use it later
func verifySignalToMetricsSynchronizationStatus(
	ctx context.Context,
	k8sClient client.Client,
	namespace string,
	name string,
	expectedStatus dash0common.Dash0ApiResourceSynchronizationStatus,
	testStartedAt time.Time,
	expectedId string,
	expectedOrigin string,
	expectedApiEndpoint string,
	expectedDataset string,
	expectedError string,
) {
	Eventually(
		func(g Gomega) {
			resource := &dash0v1alpha1.Dash0SignalToMetrics{}
			err := k8sClient.Get(
				ctx, types.NamespacedName{
					Namespace: namespace,
					Name:      name,
				}, resource,
			)
			g.Expect(err).NotTo(HaveOccurred())
			g.Expect(resource.Status.SynchronizationStatus).To(Equal(expectedStatus))
			g.Expect(resource.Status.SynchronizedAt.Time).To(BeTemporally(">=", testStartedAt.Add(-1*time.Second)))
			g.Expect(resource.Status.SynchronizedAt.Time).To(BeTemporally("<=", time.Now()))
			g.Expect(resource.Status.ValidationIssues).To(BeNil())

			g.Expect(resource.Status.SynchronizationResults).To(HaveLen(1))
			syncResultPerEndpointAndDataset := resource.Status.SynchronizationResults[0]
			g.Expect(syncResultPerEndpointAndDataset).ToNot(BeNil())
			g.Expect(syncResultPerEndpointAndDataset.Dash0ApiEndpoint).To(Equal(expectedApiEndpoint))
			g.Expect(syncResultPerEndpointAndDataset.Dash0Dataset).To(Equal(expectedDataset))
			g.Expect(syncResultPerEndpointAndDataset.Dash0Id).To(Equal(expectedId))
			g.Expect(syncResultPerEndpointAndDataset.Dash0Origin).To(Equal(expectedOrigin))
			if expectedError == "" {
				g.Expect(syncResultPerEndpointAndDataset.SynchronizationError).To(BeEmpty())
			} else {
				g.Expect(syncResultPerEndpointAndDataset.SynchronizationError).To(ContainSubstring(expectedError))
			}
		},
	).Should(Succeed())
}

func verifySignalToMetricsHasNoSynchronizationStatus(
	ctx context.Context,
	k8sClient client.Client,
	namespace string,
	name string,
) {
	Eventually(
		func(g Gomega) {
			resource := &dash0v1alpha1.Dash0SignalToMetrics{}
			err := k8sClient.Get(
				ctx, types.NamespacedName{
					Namespace: namespace,
					Name:      name,
				}, resource,
			)
			g.Expect(err).NotTo(HaveOccurred())
			g.Expect(string(resource.Status.SynchronizationStatus)).To(Equal(""))
			g.Expect(resource.Status.ValidationIssues).To(BeNil())
			g.Expect(resource.Status.SynchronizationResults).To(HaveLen(0))
		},
	).Should(Succeed())
}
