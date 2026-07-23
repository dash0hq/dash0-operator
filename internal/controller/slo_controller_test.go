// SPDX-FileCopyrightText: Copyright 2026 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package controller

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	"sigs.k8s.io/yaml"

	openslov1 "github.com/dash0hq/dash0-operator/api/openslo/v1"
	dash0common "github.com/dash0hq/dash0-operator/api/operator/common"
	"github.com/dash0hq/dash0-operator/internal/util/cluster"
	"github.com/dash0hq/dash0-operator/internal/util/logd"

	"github.com/h2non/gock"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	. "github.com/dash0hq/dash0-operator/test/util"
)

const (
	sloName        = "test-slo"
	sloApiBasePath = "/api/slos/"

	sloId             = "slo-id"
	sloOriginPattern  = "dash0-operator_%s_test-dataset_test-namespace_test-slo"
	sloManagedByLabel = "dash0.com/managed-by-operator"
)

var (
	defaultExpectedPathSLO = fmt.Sprintf(
		"%s.*%s",
		sloApiBasePath,
		"dash0-operator_.*_test-dataset_test-namespace_test-slo",
	)
	sloLeaderElectionAware = NewLeaderElectionAwareMock(true)
)

var _ = Describe(
	"The SLO controller", Ordered, func() {
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

		Describe(
			"the SLO reconciler", func() {
				var sloReconciler *SLOReconciler

				BeforeEach(
					func() {
						sloReconciler = NewSLOReconciler(
							k8sClient,
							types.UID(clusterId),
							sloLeaderElectionAware,
							TestHTTPClient(),
						)
						sloReconciler.defaultApiConfigs.Set(
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
						deleteSLOResourceIfItExists(ctx, k8sClient, TestNamespaceName, sloName)
					},
				)

				It(
					"creates an SLO", func() {
						EnsureMonitoringResourceWithoutExportExistsAndIsAvailable(ctx, k8sClient)

						expectSLOPutRequest(clusterId, defaultExpectedPathSLO)
						defer gock.Off()

						sloResource := createSLOResource()
						Expect(k8sClient.Create(ctx, sloResource)).To(Succeed())

						result, err := sloReconciler.Reconcile(
							ctx, reconcile.Request{
								NamespacedName: types.NamespacedName{
									Namespace: TestNamespaceName,
									Name:      sloName,
								},
							},
						)
						Expect(err).NotTo(HaveOccurred())
						Expect(result).To(Equal(reconcile.Result{}))

						verifySLOSynchronizationStatus(
							ctx,
							k8sClient,
							TestNamespaceName,
							sloName,
							dash0common.Dash0ApiResourceSynchronizationStatusSuccessful,
							testStartedAt,
							sloId,
							fmt.Sprintf(sloOriginPattern, clusterId),
							ApiEndpointStandardizedTest,
							DatasetCustomTest,
							"",
						)
						Expect(gock.IsDone()).To(BeTrue())
					},
				)

				It(
					"updates an SLO", func() {
						EnsureMonitoringResourceWithoutExportExistsAndIsAvailable(ctx, k8sClient)

						expectSLOPutRequest(clusterId, defaultExpectedPathSLO)
						defer gock.Off()

						sloResource := createSLOResource()
						Expect(k8sClient.Create(ctx, sloResource)).To(Succeed())

						sloResource.Spec.Objectives[0].Target = 0.995
						Expect(k8sClient.Update(ctx, sloResource)).To(Succeed())

						result, err := sloReconciler.Reconcile(
							ctx, reconcile.Request{
								NamespacedName: types.NamespacedName{
									Namespace: TestNamespaceName,
									Name:      sloName,
								},
							},
						)
						Expect(err).NotTo(HaveOccurred())
						Expect(result).To(Equal(reconcile.Result{}))

						verifySLOSynchronizationStatus(
							ctx,
							k8sClient,
							TestNamespaceName,
							sloName,
							dash0common.Dash0ApiResourceSynchronizationStatusSuccessful,
							testStartedAt,
							sloId,
							fmt.Sprintf(sloOriginPattern, clusterId),
							ApiEndpointStandardizedTest,
							DatasetCustomTest,
							"",
						)
						Expect(gock.IsDone()).To(BeTrue())
					},
				)

				It(
					"deletes an SLO", func() {
						EnsureMonitoringResourceWithoutExportExistsAndIsAvailable(ctx, k8sClient)

						expectSLODeleteRequest(defaultExpectedPathSLO)
						defer gock.Off()

						sloResource := createSLOResource()
						Expect(k8sClient.Create(ctx, sloResource)).To(Succeed())
						Expect(k8sClient.Delete(ctx, sloResource)).To(Succeed())

						result, err := sloReconciler.Reconcile(
							ctx, reconcile.Request{
								NamespacedName: types.NamespacedName{
									Namespace: TestNamespaceName,
									Name:      sloName,
								},
							},
						)
						Expect(err).NotTo(HaveOccurred())
						Expect(result).To(Equal(reconcile.Result{}))

						Expect(gock.IsDone()).To(BeTrue())
					},
				)

				It(
					"reports http errors when synchronizing an SLO", func() {
						EnsureMonitoringResourceWithoutExportExistsAndIsAvailable(ctx, k8sClient)

						gock.New(ApiEndpointTest).
							Put(defaultExpectedPathSLO).
							MatchParam("dataset", DatasetCustomTest).
							Times(3).
							Reply(503).
							JSON(map[string]string{})
						defer gock.Off()

						sloResource := createSLOResource()
						Expect(k8sClient.Create(ctx, sloResource)).To(Succeed())

						result, err := sloReconciler.Reconcile(
							ctx, reconcile.Request{
								NamespacedName: types.NamespacedName{
									Namespace: TestNamespaceName,
									Name:      sloName,
								},
							},
						)
						Expect(err).NotTo(HaveOccurred())
						Expect(result).To(Equal(reconcile.Result{}))

						verifySLOSynchronizationStatus(
							ctx,
							k8sClient,
							TestNamespaceName,
							sloName,
							dash0common.Dash0ApiResourceSynchronizationStatusFailed,
							testStartedAt,
							"",
							"",
							ApiEndpointStandardizedTest,
							DatasetCustomTest,
							"unexpected status code 503",
						)
						Expect(gock.IsDone()).To(BeTrue())
					},
				)

				It(
					"does not synchronize when a conflicting slos.openslo CRD is detected", func() {
						EnsureMonitoringResourceWithoutExportExistsAndIsAvailable(ctx, k8sClient)

						// No gock expectations: the reconciler must not issue any API request in this state.
						defer gock.Off()

						sloReconciler.conflictingCrdDetected.Store(true)

						sloResource := createSLOResource()
						Expect(k8sClient.Create(ctx, sloResource)).To(Succeed())

						result, err := sloReconciler.Reconcile(
							ctx, reconcile.Request{
								NamespacedName: types.NamespacedName{
									Namespace: TestNamespaceName,
									Name:      sloName,
								},
							},
						)
						Expect(err).NotTo(HaveOccurred())
						Expect(result).To(Equal(reconcile.Result{}))
						Expect(gock.IsPending()).To(BeFalse())
					},
				)
			},
		)

		Describe(
			"mapping SLO resources to http requests", func() {
				var sloReconciler *SLOReconciler

				BeforeEach(
					func() {
						sloReconciler = &SLOReconciler{}
					},
				)

				It(
					"builds an openslo.com/v1 SLO API body with apiVersion, kind and spec", func() {
						slo := map[string]any{}
						Expect(yaml.Unmarshal([]byte(sloYamlForMapping), &slo)).To(Succeed())
						apiConfig := ApiConfig{
							Endpoint: ApiEndpointTest,
							Dataset:  DatasetCustomTest,
							Token:    AuthorizationTokenTest,
						}
						preconditionValidationResult := &preconditionValidationResult{
							k8sName:      sloName,
							k8sNamespace: TestNamespaceName,
							resource:     slo,
							validatedApiConfigs: []ValidatedApiConfigAndToken{
								*NewValidatedApiConfigAndToken(apiConfig.Endpoint, apiConfig.Dataset, apiConfig.Token),
							},
						}
						resourceToRequestsResult := sloReconciler.MapResourceToHttpRequests(
							preconditionValidationResult,
							apiConfig,
							upsertAction,
							logger,
						)
						Expect(resourceToRequestsResult.TotalProcessed()).To(Equal(1))
						Expect(resourceToRequestsResult.ValidationIssues).To(BeNil())
						Expect(resourceToRequestsResult.SynchronizationErrors).To(BeNil())
						Expect(resourceToRequestsResult.ApiRequests).To(HaveLen(1))

						apiRequest := resourceToRequestsResult.ApiRequests[0]
						Expect(apiRequest.ItemName).To(Equal(sloName))
						req := apiRequest.Request
						defer func() {
							_ = req.Body.Close()
						}()
						body, err := io.ReadAll(req.Body)
						Expect(err).ToNot(HaveOccurred())
						resultingSLO := map[string]any{}
						Expect(json.Unmarshal(body, &resultingSLO)).To(Succeed())
						// The controller must ensure the apiVersion/kind envelope is present on the outbound body, using
						// the same openslo.com/v1 / SLO values (they are stripped from TypeMeta on read).
						Expect(resultingSLO["apiVersion"]).To(Equal("openslo.com/v1"))
						Expect(resultingSLO["kind"]).To(Equal("SLO"))
						Expect(resultingSLO["spec"]).ToNot(BeNil())
						Expect(ReadFromMap(resultingSLO, []string{"spec", "budgetingMethod"})).To(Equal("Occurrences"))
						Expect(ReadFromMap(resultingSLO, []string{"metadata", "name"})).To(Equal(sloName))
					},
				)
			},
		)

		Describe(
			"conflict detection", func() {
				It(
					"treats a slos.openslo CRD carrying the operator marker label as managed", func() {
						crd := &apiextensionsv1.CustomResourceDefinition{
							ObjectMeta: metav1.ObjectMeta{
								Name:   sloCrdQualifiedName,
								Labels: map[string]string{sloManagedByLabel: "true"},
							},
						}
						Expect(crdIsManagedByDash0Operator(crd)).To(BeTrue())
					},
				)
				It(
					"treats a slos.openslo CRD without the operator marker label as a conflict", func() {
						crd := &apiextensionsv1.CustomResourceDefinition{
							ObjectMeta: metav1.ObjectMeta{
								Name:   sloCrdQualifiedName,
								Labels: map[string]string{"app.kubernetes.io/managed-by": "some-other-operator"},
							},
						}
						Expect(crdIsManagedByDash0Operator(crd)).To(BeFalse())
					},
				)
				It(
					"treats a slos.openslo CRD with no labels as a conflict", func() {
						crd := &apiextensionsv1.CustomResourceDefinition{
							ObjectMeta: metav1.ObjectMeta{Name: sloCrdQualifiedName},
						}
						Expect(crdIsManagedByDash0Operator(crd)).To(BeFalse())
					},
				)
			},
		)
	},
)

const sloYamlForMapping = `
apiVersion: openslo.com/v1
kind: SLO
metadata:
  name: test-slo
  annotations:
    dash0.com/display-name: Checkout availability
spec:
  description: 99 percent of checkout HTTP requests succeed over a rolling 28-day window.
  service: checkout
  budgetingMethod: Occurrences
  timeWindow:
    - duration: 28d
      isRolling: true
  indicator:
    metadata:
      name: checkout-success-ratio
    spec:
      ratioMetric:
        counter: true
        good:
          metricSource:
            type: Prometheus
            spec:
              query: 'http_server_request_duration_seconds_count{service_name="checkout",http_response_status_code!~"5.."}'
        total:
          metricSource:
            type: Prometheus
            spec:
              query: 'http_server_request_duration_seconds_count{service_name="checkout"}'
  objectives:
    - displayName: 99% availability
      target: 0.99
`

func expectSLOPutRequest(clusterId string, expectedPath string) {
	gock.New(ApiEndpointTest).
		Put(expectedPath).
		MatchHeader("Authorization", AuthorizationHeaderTest).
		MatchParam("dataset", DatasetCustomTest).
		Times(1).
		Reply(200).
		JSON(sloPutResponse(clusterId, sloOriginPattern, DatasetCustomTest))
}

func sloPutResponse(clusterId string, originPattern string, dataset string) map[string]any {
	return map[string]any{
		"metadata": map[string]any{
			"labels": map[string]any{
				"dash0.com/id":      sloId,
				"dash0.com/origin":  fmt.Sprintf(originPattern, clusterId),
				"dash0.com/dataset": dataset,
			},
		},
	}
}

func expectSLODeleteRequest(expectedPath string) {
	gock.New(ApiEndpointTest).
		Delete(expectedPath).
		MatchHeader("Authorization", AuthorizationHeaderTest).
		MatchParam("dataset", DatasetCustomTest).
		Times(1).
		Reply(http.StatusOK)
}

func createSLOResource() *openslov1.SLO {
	return &openslov1.SLO{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "openslo.com/v1",
			Kind:       "SLO",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      sloName,
			Namespace: TestNamespaceName,
			Annotations: map[string]string{
				"dash0.com/display-name": "Checkout availability",
				"dash0.com/enabled":      "true",
			},
		},
		Spec: openslov1.SLOSpec{
			Description:     "99 percent of checkout HTTP requests succeed over a rolling 28-day window.",
			Service:         "checkout",
			BudgetingMethod: "Occurrences",
			TimeWindow: []openslov1.SLOTimeWindow{
				{
					Duration:  "28d",
					IsRolling: true,
				},
			},
			Indicator: openslov1.SLOIndicator{
				Metadata: &openslov1.SLOIndicatorMetadata{
					Name: "checkout-success-ratio",
				},
				Spec: openslov1.SLOIndicatorSpec{
					RatioMetric: openslov1.SLORatioMetric{
						Counter: ptr.To(true),
						Good: openslov1.SLOMetricSourceWrapper{
							MetricSource: openslov1.SLOMetricSource{
								Type: "Prometheus",
								Spec: openslov1.SLOMetricSourceSpec{
									Query: `http_server_request_duration_seconds_count{service_name="checkout",http_response_status_code!~"5.."}`,
								},
							},
						},
						Total: openslov1.SLOMetricSourceWrapper{
							MetricSource: openslov1.SLOMetricSource{
								Type: "Prometheus",
								Spec: openslov1.SLOMetricSourceSpec{
									Query: `http_server_request_duration_seconds_count{service_name="checkout"}`,
								},
							},
						},
					},
				},
			},
			Objectives: []openslov1.SLOObjective{
				{
					DisplayName: "99% availability",
					Target:      0.99,
				},
			},
		},
	}
}

func deleteSLOResourceIfItExists(
	ctx context.Context,
	k8sClient client.Client,
	namespace string,
	name string,
) {
	slo := &openslov1.SLO{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
	}
	err := k8sClient.Delete(
		ctx, slo, &client.DeleteOptions{
			GracePeriodSeconds: new(int64),
		},
	)
	if err != nil && !apierrors.IsNotFound(err) {
		Expect(err).NotTo(HaveOccurred())
	}
}

func verifySLOSynchronizationStatus(
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
			slo := &openslov1.SLO{}
			err := k8sClient.Get(
				ctx, types.NamespacedName{
					Namespace: namespace,
					Name:      name,
				}, slo,
			)
			g.Expect(err).NotTo(HaveOccurred())
			g.Expect(slo.Status.SynchronizationStatus).To(Equal(expectedStatus))
			g.Expect(slo.Status.SynchronizedAt.Time).To(BeTemporally(">=", testStartedAt.Add(-1*time.Second)))
			g.Expect(slo.Status.SynchronizedAt.Time).To(BeTemporally("<=", time.Now()))
			g.Expect(slo.Status.ValidationIssues).To(BeNil())

			g.Expect(slo.Status.SynchronizationResults).To(HaveLen(1))
			syncResultPerEndpointAndDataset := slo.Status.SynchronizationResults[0]
			g.Expect(syncResultPerEndpointAndDataset.Dash0ApiEndpoint).To(Equal(expectedApiEndpoint))
			g.Expect(syncResultPerEndpointAndDataset.Dash0Dataset).To(Equal(expectedDataset))
			g.Expect(syncResultPerEndpointAndDataset.Dash0Id).To(Equal(expectedId))
			g.Expect(syncResultPerEndpointAndDataset.Dash0Origin).To(Equal(expectedOrigin))
			g.Expect(syncResultPerEndpointAndDataset.SynchronizationError).To(ContainSubstring(expectedError))
		},
	).Should(Succeed())
}
