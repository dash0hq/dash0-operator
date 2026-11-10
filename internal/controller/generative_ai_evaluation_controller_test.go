// SPDX-FileCopyrightText: Copyright 2026 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package controller

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	"sigs.k8s.io/yaml"

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
	generativeAiEvaluationName            = "test-generative-ai-evaluation"
	extraNamespaceGenerativeAiEvaluations = "extra-namespace-generative-ai-evaluations"
	generativeAiEvaluationName2           = "test-generative-ai-evaluation-2"
	generativeAiEvaluationApiBasePath     = "/api/generative-ai-evaluations/"

	generativeAiEvaluationId                       = "generative-ai-evaluation-id"
	generativeAiEvaluationOriginPattern            = "dash0-operator_%s_test-dataset_test-namespace_test-generative-ai-evaluation"
	generativeAiEvaluationOriginPatternExtra       = "dash0-operator_%s_test-dataset_extra-namespace-generative-ai-evaluations_test-generative-ai-evaluation-2"
	generativeAiEvaluationOriginPatternAlternative = "dash0-operator_%s_test-dataset-alt_test-namespace_test-generative-ai-evaluation"
)

var (
	defaultExpectedPathGenerativeAiEvaluation = fmt.Sprintf(
		"%s.*%s",
		generativeAiEvaluationApiBasePath,
		"dash0-operator_.*_test-dataset_test-namespace_test-generative-ai-evaluation",
	)
	expectedPathGenerativeAiEvaluationAlternative = fmt.Sprintf(
		"%s.*%s",
		generativeAiEvaluationApiBasePath,
		"dash0-operator_.*_test-dataset-alt_test-namespace_test-generative-ai-evaluation",
	)
	defaultExpectedPathGenerativeAiEvaluation2 = fmt.Sprintf(
		"%s.*%s",
		generativeAiEvaluationApiBasePath,
		"dash0-operator_.*_test-dataset_extra-namespace-generative-ai-evaluations_test-generative-ai-evaluation-2",
	)
	generativeAiEvaluationLeaderElectionAware = NewLeaderElectionAwareMock(true)
)

var _ = Describe(
	"The GenerativeAiEvaluation controller", Ordered, func() {
		var (
			extraMonitoringResourceNames []types.NamespacedName
			testStartedAt                time.Time
			clusterId                    string
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
				extraMonitoringResourceNames = make([]types.NamespacedName, 0)
			},
		)

		AfterEach(
			func() {
				DeleteMonitoringResource(ctx, k8sClient)
				for _, name := range extraMonitoringResourceNames {
					DeleteMonitoringResourceByName(ctx, k8sClient, name, true)
				}
				extraMonitoringResourceNames = make([]types.NamespacedName, 0)
			},
		)

		Describe(
			"the generative AI evaluation reconciler", func() {
				var generativeAiEvaluationReconciler *GenerativeAiEvaluationReconciler

				BeforeEach(
					func() {
						generativeAiEvaluationReconciler = createGenerativeAiEvaluationReconciler(clusterId)

						// Set default API configs directly (not via SetDefaultApiConfigs) to avoid
						// triggering maybeDoInitialSynchronizationOfAllResources, which would set
						// initialSyncHasHappend to true and prevent the DescribeTable tests from
						// verifying initial sync behavior.
						generativeAiEvaluationReconciler.defaultApiConfigs.Set(
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
						DeleteMonitoringResourceIfItExists(ctx, k8sClient)
						deleteGenerativeAiEvaluationResourceIfItExists(
							ctx, k8sClient, TestNamespaceName, generativeAiEvaluationName)
						deleteGenerativeAiEvaluationResourceIfItExists(
							ctx, k8sClient, extraNamespaceGenerativeAiEvaluations, generativeAiEvaluationName2)
					},
				)

				It(
					"it ignores generative AI evaluation resource changes if no Dash0 monitoring resource exists in the namespace", func() {
						expectGenerativeAiEvaluationPutRequest(clusterId, defaultExpectedPathGenerativeAiEvaluation)
						defer gock.Off()

						generativeAiEvaluationResource :=
							createGenerativeAiEvaluationResource(TestNamespaceName, generativeAiEvaluationName)
						Expect(k8sClient.Create(ctx, generativeAiEvaluationResource)).To(Succeed())

						Expect(gock.IsPending()).To(BeTrue())
						verifyGenerativeAiEvaluationHasNoSynchronizationStatus(
							ctx,
							k8sClient,
							TestNamespaceName,
							generativeAiEvaluationName,
						)
					},
				)

				It(
					"it ignores generative AI evaluation resource changes if the API endpoint is not configured", func() {
						EnsureMonitoringResourceWithoutExportExistsAndIsAvailable(ctx, k8sClient)

						expectGenerativeAiEvaluationPutRequest(clusterId, defaultExpectedPathGenerativeAiEvaluation)
						defer gock.Off()

						generativeAiEvaluationReconciler.RemoveDefaultApiConfigs(ctx, logger)

						generativeAiEvaluationResource :=
							createGenerativeAiEvaluationResource(TestNamespaceName, generativeAiEvaluationName)
						Expect(k8sClient.Create(ctx, generativeAiEvaluationResource)).To(Succeed())
						result, err := generativeAiEvaluationReconciler.Reconcile(
							ctx, reconcile.Request{
								NamespacedName: types.NamespacedName{
									Namespace: TestNamespaceName,
									Name:      generativeAiEvaluationName,
								},
							},
						)
						Expect(err).NotTo(HaveOccurred())
						Expect(result).To(Equal(reconcile.Result{}))

						Expect(gock.IsPending()).To(BeTrue())
						verifyGenerativeAiEvaluationHasNoSynchronizationStatus(
							ctx,
							k8sClient,
							TestNamespaceName,
							generativeAiEvaluationName,
						)
					},
				)

				It(
					"creates a generative AI evaluation", func() {
						EnsureMonitoringResourceWithoutExportExistsAndIsAvailable(ctx, k8sClient)

						expectGenerativeAiEvaluationPutRequest(clusterId, defaultExpectedPathGenerativeAiEvaluation)
						defer gock.Off()

						generativeAiEvaluationResource :=
							createGenerativeAiEvaluationResource(TestNamespaceName, generativeAiEvaluationName)
						Expect(k8sClient.Create(ctx, generativeAiEvaluationResource)).To(Succeed())

						result, err := generativeAiEvaluationReconciler.Reconcile(
							ctx, reconcile.Request{
								NamespacedName: types.NamespacedName{
									Namespace: TestNamespaceName,
									Name:      generativeAiEvaluationName,
								},
							},
						)
						Expect(err).NotTo(HaveOccurred())
						Expect(result).To(Equal(reconcile.Result{}))

						verifyGenerativeAiEvaluationSynchronizationStatus(
							ctx,
							k8sClient,
							TestNamespaceName,
							generativeAiEvaluationName,
							dash0common.Dash0ApiResourceSynchronizationStatusSuccessful,
							testStartedAt,
							generativeAiEvaluationId,
							fmt.Sprintf(generativeAiEvaluationOriginPattern, clusterId),
							ApiEndpointStandardizedTest,
							DatasetCustomTest,
							"",
						)
						Expect(gock.IsDone()).To(BeTrue())
					},
				)

				It(
					"creates a generative AI evaluation with namespaced config from the monitoring resource", func() {
						monitoringResource := DefaultMonitoringResourceWithCustomApiConfigAndToken(
							MonitoringResourceQualifiedName,
							ApiEndpointTestAlternative,
							DatasetCustomTestAlternative,
							AuthorizationTokenTestAlternative,
						)
						EnsureMonitoringResourceWithSpecExistsAndIsAvailable(ctx, k8sClient, monitoringResource.Spec)

						expectGenerativeAiEvaluationPutRequestCustom(
							clusterId,
							expectedPathGenerativeAiEvaluationAlternative,
							ApiEndpointTestAlternative,
							DatasetCustomTestAlternative,
							AuthorizationTokenTestAlternative,
							generativeAiEvaluationOriginPatternAlternative,
							1,
						)
						defer gock.Off()

						generativeAiEvaluationResource :=
							createGenerativeAiEvaluationResource(TestNamespaceName, generativeAiEvaluationName)
						Expect(k8sClient.Create(ctx, generativeAiEvaluationResource)).To(Succeed())

						generativeAiEvaluationReconciler.SetNamespacedApiConfigs(
							ctx, TestNamespaceName, []ApiConfig{
								{
									Endpoint: ApiEndpointTestAlternative,
									Dataset:  DatasetCustomTestAlternative,
									Token:    AuthorizationTokenTestAlternative,
								},
							}, logger,
						)

						// note: we don't trigger reconcile here because setting the API config and token already triggers a reconciliation

						verifyGenerativeAiEvaluationSynchronizationStatus(
							ctx,
							k8sClient,
							TestNamespaceName,
							generativeAiEvaluationName,
							dash0common.Dash0ApiResourceSynchronizationStatusSuccessful,
							testStartedAt,
							generativeAiEvaluationId,
							fmt.Sprintf(generativeAiEvaluationOriginPatternAlternative, clusterId),
							ApiEndpointStandardizedTestAlternative,
							DatasetCustomTestAlternative,
							"",
						)
						Expect(gock.IsDone()).To(BeTrue())
					},
				)

				It(
					"updates a generative AI evaluation", func() {
						EnsureMonitoringResourceWithoutExportExistsAndIsAvailable(ctx, k8sClient)

						expectGenerativeAiEvaluationPutRequest(clusterId, defaultExpectedPathGenerativeAiEvaluation)
						defer gock.Off()

						generativeAiEvaluationResource :=
							createGenerativeAiEvaluationResource(TestNamespaceName, generativeAiEvaluationName)
						Expect(k8sClient.Create(ctx, generativeAiEvaluationResource)).To(Succeed())

						// Modify the resource
						generativeAiEvaluationResource.Spec.Sample.Rate = 0.5
						Expect(k8sClient.Update(ctx, generativeAiEvaluationResource)).To(Succeed())

						result, err := generativeAiEvaluationReconciler.Reconcile(
							ctx, reconcile.Request{
								NamespacedName: types.NamespacedName{
									Namespace: TestNamespaceName,
									Name:      generativeAiEvaluationName,
								},
							},
						)
						Expect(err).NotTo(HaveOccurred())
						Expect(result).To(Equal(reconcile.Result{}))

						verifyGenerativeAiEvaluationSynchronizationStatus(
							ctx,
							k8sClient,
							TestNamespaceName,
							generativeAiEvaluationName,
							dash0common.Dash0ApiResourceSynchronizationStatusSuccessful,
							testStartedAt,
							generativeAiEvaluationId,
							fmt.Sprintf(generativeAiEvaluationOriginPattern, clusterId),
							ApiEndpointStandardizedTest,
							DatasetCustomTest,
							"",
						)
						Expect(gock.IsDone()).To(BeTrue())
					},
				)

				It(
					"deletes a generative AI evaluation", func() {
						EnsureMonitoringResourceWithoutExportExistsAndIsAvailable(ctx, k8sClient)

						expectGenerativeAiEvaluationDeleteRequest(defaultExpectedPathGenerativeAiEvaluation)
						defer gock.Off()

						generativeAiEvaluationResource :=
							createGenerativeAiEvaluationResource(TestNamespaceName, generativeAiEvaluationName)
						Expect(k8sClient.Create(ctx, generativeAiEvaluationResource)).To(Succeed())
						Expect(k8sClient.Delete(ctx, generativeAiEvaluationResource)).To(Succeed())

						result, err := generativeAiEvaluationReconciler.Reconcile(
							ctx, reconcile.Request{
								NamespacedName: types.NamespacedName{
									Namespace: TestNamespaceName,
									Name:      generativeAiEvaluationName,
								},
							},
						)
						Expect(err).NotTo(HaveOccurred())
						Expect(result).To(Equal(reconcile.Result{}))

						// We do not call verifyGenerativeAiEvaluationSynchronizationStatus in this test case since the
						// entire generative AI evaluation resource is deleted, hence there is nothing to write the status to.
						Expect(gock.IsDone()).To(BeTrue())
					},
				)

				It(
					"deletes a generative AI evaluation if labelled with dash0.com/enable=false", func() {
						EnsureMonitoringResourceWithoutExportExistsAndIsAvailable(ctx, k8sClient)

						expectGenerativeAiEvaluationDeleteRequestWithHttpStatus(
							defaultExpectedPathGenerativeAiEvaluation, http.StatusNotFound)
						defer gock.Off()

						generativeAiEvaluationResource := createGenerativeAiEvaluationResourceWithEnableLabel(
							TestNamespaceName, generativeAiEvaluationName, "false")
						Expect(k8sClient.Create(ctx, generativeAiEvaluationResource)).To(Succeed())

						result, err := generativeAiEvaluationReconciler.Reconcile(
							ctx, reconcile.Request{
								NamespacedName: types.NamespacedName{
									Namespace: TestNamespaceName,
									Name:      generativeAiEvaluationName,
								},
							},
						)
						Expect(err).NotTo(HaveOccurred())
						Expect(result).To(Equal(reconcile.Result{}))

						verifyGenerativeAiEvaluationSynchronizationStatus(
							ctx,
							k8sClient,
							TestNamespaceName,
							generativeAiEvaluationName,
							dash0common.Dash0ApiResourceSynchronizationStatusSuccessful,
							testStartedAt,
							"", // when deleting an object, we do not get an HTTP response body with an ID
							fmt.Sprintf(generativeAiEvaluationOriginPattern, clusterId),
							ApiEndpointStandardizedTest,
							DatasetCustomTest,
							"",
						)
						Expect(gock.IsDone()).To(BeTrue())
					},
				)

				It(
					"reports http errors when synchronizing a generative AI evaluation", func() {
						EnsureMonitoringResourceWithoutExportExistsAndIsAvailable(ctx, k8sClient)

						gock.New(ApiEndpointTest).
							Put(defaultExpectedPathGenerativeAiEvaluation).
							MatchParam("dataset", DatasetCustomTest).
							Times(3).
							Reply(503).
							JSON(map[string]string{})
						defer gock.Off()

						generativeAiEvaluationResource :=
							createGenerativeAiEvaluationResource(TestNamespaceName, generativeAiEvaluationName)
						Expect(k8sClient.Create(ctx, generativeAiEvaluationResource)).To(Succeed())

						result, err := generativeAiEvaluationReconciler.Reconcile(
							ctx, reconcile.Request{
								NamespacedName: types.NamespacedName{
									Namespace: TestNamespaceName,
									Name:      generativeAiEvaluationName,
								},
							},
						)
						Expect(err).NotTo(HaveOccurred())
						Expect(result).To(Equal(reconcile.Result{}))

						verifyGenerativeAiEvaluationSynchronizationStatus(
							ctx,
							k8sClient,
							TestNamespaceName,
							generativeAiEvaluationName,
							dash0common.Dash0ApiResourceSynchronizationStatusFailed,
							testStartedAt,
							"",
							"",
							ApiEndpointStandardizedTest,
							DatasetCustomTest,
							"unexpected status code 503 when trying to synchronize the generative-ai-evaluation "+
								"\"test-generative-ai-evaluation\": "+
								"PUT https://api.dash0.com/api/generative-ai-evaluations/"+
								"dash0-operator_"+clusterId+
								"_test-dataset_test-namespace_test-generative-ai-evaluation?dataset=test-dataset, "+
								"response body is {}",
						)
						Expect(gock.IsDone()).To(BeTrue())
					},
				)

				type maybeDoInitialSynchronizationOfAllResourcesTest struct {
					disableSync func()
					enabledSync func()
				}

				DescribeTable(
					"synchronizes all existing generative AI evaluation resources when the auth token or api endpoint become available",
					func(testConfig maybeDoInitialSynchronizationOfAllResourcesTest) {
						EnsureMonitoringResourceWithoutExportExistsAndIsAvailable(ctx, k8sClient)

						// Disable synchronization by removing the auth token or api endpoint.
						testConfig.disableSync()

						EnsureNamespaceExists(ctx, k8sClient, extraNamespaceGenerativeAiEvaluations)
						secondMonitoringResource := EnsureMonitoringResourceWithSpecExistsInNamespaceAndIsAvailable(
							ctx,
							k8sClient,
							MonitoringResourceDefaultSpecWithoutExport,
							types.NamespacedName{Namespace: extraNamespaceGenerativeAiEvaluations, Name: MonitoringResourceName},
						)
						extraMonitoringResourceNames = append(
							extraMonitoringResourceNames, types.NamespacedName{
								Namespace: secondMonitoringResource.Namespace,
								Name:      secondMonitoringResource.Name,
							},
						)

						expectGenerativeAiEvaluationPutRequest(clusterId, defaultExpectedPathGenerativeAiEvaluation)
						expectGenerativeAiEvaluationPutRequest(clusterId, defaultExpectedPathGenerativeAiEvaluation2)
						defer gock.Off()

						generativeAiEvaluationResource1 :=
							createGenerativeAiEvaluationResource(TestNamespaceName, generativeAiEvaluationName)
						Expect(k8sClient.Create(ctx, generativeAiEvaluationResource1)).To(Succeed())
						generativeAiEvaluationResource2 := createGenerativeAiEvaluationResource(
							extraNamespaceGenerativeAiEvaluations, generativeAiEvaluationName2)
						Expect(k8sClient.Create(ctx, generativeAiEvaluationResource2)).To(Succeed())

						// verify that the generative AI evaluations have not been synchronized yet
						Expect(gock.IsPending()).To(BeTrue())
						verifyGenerativeAiEvaluationHasNoSynchronizationStatus(
							ctx, k8sClient, TestNamespaceName, generativeAiEvaluationName)
						verifyGenerativeAiEvaluationHasNoSynchronizationStatus(
							ctx, k8sClient, extraNamespaceGenerativeAiEvaluations, generativeAiEvaluationName2)

						// Now provide the auth token or API endpoint, which was unset before. This should trigger initial
						// synchronization of all resources.
						testConfig.enabledSync()

						// Verify both generative AI evaluation resources have been synchronized
						verifyGenerativeAiEvaluationSynchronizationStatus(
							ctx,
							k8sClient,
							TestNamespaceName,
							generativeAiEvaluationName,
							dash0common.Dash0ApiResourceSynchronizationStatusSuccessful,
							testStartedAt,
							generativeAiEvaluationId,
							fmt.Sprintf(generativeAiEvaluationOriginPattern, clusterId),
							ApiEndpointStandardizedTest,
							DatasetCustomTest,
							"",
						)
						verifyGenerativeAiEvaluationSynchronizationStatus(
							ctx,
							k8sClient,
							extraNamespaceGenerativeAiEvaluations,
							generativeAiEvaluationName2,
							dash0common.Dash0ApiResourceSynchronizationStatusSuccessful,
							testStartedAt,
							generativeAiEvaluationId,
							fmt.Sprintf(generativeAiEvaluationOriginPatternExtra, clusterId),
							ApiEndpointStandardizedTest,
							DatasetCustomTest,
							"",
						)
						Expect(gock.IsDone()).To(BeTrue())
					},
					Entry(
						"when the auth token becomes available", maybeDoInitialSynchronizationOfAllResourcesTest{
							disableSync: func() {
								generativeAiEvaluationReconciler.RemoveDefaultApiConfigs(ctx, logger)
							},
							enabledSync: func() {
								generativeAiEvaluationReconciler.SetDefaultApiConfigs(
									ctx, []ApiConfig{
										{
											Endpoint: ApiEndpointTest,
											Dataset:  DatasetCustomTest,
											Token:    AuthorizationTokenTest,
										},
									}, logger,
								)
							},
						},
					),
					Entry(
						"when the operator manager becomes leader", maybeDoInitialSynchronizationOfAllResourcesTest{
							disableSync: func() {
								generativeAiEvaluationLeaderElectionAware.SetLeader(false)
							},
							enabledSync: func() {
								generativeAiEvaluationLeaderElectionAware.SetLeader(true)
								generativeAiEvaluationReconciler.NotifyOperatorManagerJustBecameLeader(ctx, logger)
							},
						},
					),
				)
			},
		)

		Describe(
			"mapping generative AI evaluation resources to http requests", func() {

				type generativeAiEvaluationToRequestTestConfig struct {
					generativeAiEvaluation string
					expectedAnnotations    map[string]string
				}

				var generativeAiEvaluationReconciler *GenerativeAiEvaluationReconciler

				BeforeEach(
					func() {
						generativeAiEvaluationReconciler = &GenerativeAiEvaluationReconciler{}
					},
				)

				DescribeTable(
					"maps generative AI evaluations", func(testConfig generativeAiEvaluationToRequestTestConfig) {
						generativeAiEvaluation := map[string]any{}
						Expect(yaml.Unmarshal([]byte(testConfig.generativeAiEvaluation), &generativeAiEvaluation)).To(Succeed())
						apiConfig := ApiConfig{
							Endpoint: ApiEndpointTest,
							Dataset:  DatasetCustomTest,
							Token:    AuthorizationTokenTest,
						}
						preconditionValidationResult := &preconditionValidationResult{
							k8sName:      "dash0-generative-ai-evaluation",
							k8sNamespace: TestNamespaceName,
							resource:     generativeAiEvaluation,
							validatedApiConfigs: []ValidatedApiConfigAndToken{
								*NewValidatedApiConfigAndToken(apiConfig.Endpoint, apiConfig.Dataset, apiConfig.Token),
							},
						}
						resourceToRequestsResult :=
							generativeAiEvaluationReconciler.MapResourceToApiCalls(
								preconditionValidationResult, apiConfig, upsertAction, logger)
						Expect(resourceToRequestsResult.OriginsInResource).To(BeNil())
						Expect(resourceToRequestsResult.ValidationIssues).To(BeNil())
						Expect(resourceToRequestsResult.SynchronizationErrors).To(BeNil())

						Expect(resourceToRequestsResult.ApiRequests).To(HaveLen(1))
						apiRequest := resourceToRequestsResult.ApiRequests[0]
						Expect(apiRequest.ItemName).To(Equal("dash0-generative-ai-evaluation"))
						Expect(apiRequest.ApiClientCall).ToNot(BeNil())
						Expect(apiRequest.ApiClientCall.Method).To(Equal(http.MethodPut))
						generativeAiEvaluationDefinition, err := mapToGenerativeAiEvaluationDefinition(generativeAiEvaluation)
						Expect(err).ToNot(HaveOccurred())
						body, err := json.Marshal(generativeAiEvaluationDefinition)
						Expect(err).ToNot(HaveOccurred())
						resultingGenerativeAiEvaluationInRequest := map[string]any{}
						Expect(json.Unmarshal(body, &resultingGenerativeAiEvaluationInRequest)).To(Succeed())
						Expect(resultingGenerativeAiEvaluationInRequest["spec"]).ToNot(BeNil())

						Expect(resultingGenerativeAiEvaluationInRequest["metadata"]).ToNot(BeNil())
						Expect(ReadFromMap(resultingGenerativeAiEvaluationInRequest, []string{"metadata", "name"})).
							To(Equal("dash0-generative-ai-evaluation"))

						if testConfig.expectedAnnotations != nil {
							annotationsRaw := ReadFromMap(
								resultingGenerativeAiEvaluationInRequest, []string{"metadata", "annotations"})
							Expect(annotationsRaw).ToNot(BeNil())
							annotations := annotationsRaw.(map[string]any)
							Expect(annotations).To(HaveLen(len(testConfig.expectedAnnotations)))
							for expectedKey, expectedValue := range testConfig.expectedAnnotations {
								value, ok := annotations[expectedKey]
								Expect(ok).To(BeTrue())
								Expect(value).To(Equal(expectedValue))
							}
						} else {
							Expect(ReadFromMap(
								resultingGenerativeAiEvaluationInRequest, []string{"metadata", "annotations"})).To(BeNil())
						}
					},
					Entry(
						"should map a generative AI evaluation with a numeric output", generativeAiEvaluationToRequestTestConfig{
							generativeAiEvaluation: `
apiVersion: operator.dash0.com/v1alpha1
kind: Dash0GenerativeAiEvaluation
metadata:
  name: dash0-generative-ai-evaluation
spec:
  enabled: true
  display:
    name: Answer relevance
  match:
    filters:
      - key: gen_ai.operation.name
        operator: is_set
  sample:
    rate: 0.1
  judge:
    model: light
    systemPrompt: You review the answers of an AI assistant.
    userPrompt: "Answer: {{ gen_ai.output.messages }}"
    output:
      kind: numeric
      spec:
        threshold: 0.7
`,
						},
					),
					Entry(
						"should map a generative AI evaluation with a boolean output", generativeAiEvaluationToRequestTestConfig{
							generativeAiEvaluation: `
apiVersion: operator.dash0.com/v1alpha1
kind: Dash0GenerativeAiEvaluation
metadata:
  name: dash0-generative-ai-evaluation
spec:
  enabled: true
  display:
    name: Answer relevance
  match:
    filters:
      - key: gen_ai.operation.name
        operator: is_set
  sample:
    rate: 0.1
  judge:
    model: light
    systemPrompt: You review the answers of an AI assistant.
    userPrompt: "Answer: {{ gen_ai.output.messages }}"
    allowPartialPrompt: true
    output:
      kind: boolean
`,
						},
					),
					Entry(
						"should map a generative AI evaluation with a categorical output", generativeAiEvaluationToRequestTestConfig{
							generativeAiEvaluation: `
apiVersion: operator.dash0.com/v1alpha1
kind: Dash0GenerativeAiEvaluation
metadata:
  name: dash0-generative-ai-evaluation
spec:
  enabled: true
  display:
    name: Answer relevance
  match:
    filters:
      - key: gen_ai.operation.name
        operator: is_set
  sample:
    rate: 0.1
  judge:
    model: light
    systemPrompt: You review the answers of an AI assistant.
    userPrompt: "Answer: {{ gen_ai.output.messages }}"
    output:
      kind: categorical
      spec:
        options:
          - label: relevant
            intent: positive
          - label: off-topic
            intent: negative
          - label: unclear
`,
						},
					),
					Entry(
						"should keep only the annotations of the Dash0 API", generativeAiEvaluationToRequestTestConfig{
							generativeAiEvaluation: `
apiVersion: operator.dash0.com/v1alpha1
kind: Dash0GenerativeAiEvaluation
metadata:
  name: dash0-generative-ai-evaluation
  annotations:
    dash0.com/folder-path: /checkout
    dash0.com/sharing: team:team_01abc
    example.com/not-part-of-the-api: dropped
spec:
  enabled: true
  display:
    name: Answer relevance
  match:
    filters:
      - key: gen_ai.operation.name
        operator: is_set
  sample:
    rate: 0.1
  judge:
    model: light
    systemPrompt: You review the answers of an AI assistant.
    userPrompt: "Answer: {{ gen_ai.output.messages }}"
    output:
      kind: boolean
`,
							expectedAnnotations: map[string]string{
								"dash0.com/folder-path": "/checkout",
								"dash0.com/sharing":     "team:team_01abc",
							},
						},
					),
				)

				It(
					"reports a non-retryable synchronization error for a generative AI evaluation that cannot be converted",
					func() {
						generativeAiEvaluation := map[string]any{}
						Expect(yaml.Unmarshal([]byte(`
apiVersion: operator.dash0.com/v1alpha1
kind: Dash0GenerativeAiEvaluation
metadata:
  name: dash0-generative-ai-evaluation
spec:
  enabled: true
  display:
    name: Answer relevance
  match:
    filters:
      - key: gen_ai.operation.name
        operator: is_set
  sample:
    rate: high
  judge:
    model: light
    systemPrompt: You review the answers of an AI assistant.
    userPrompt: "Answer: {{ gen_ai.output.messages }}"
    output:
      kind: boolean
`), &generativeAiEvaluation)).To(Succeed())
						apiConfig := ApiConfig{Endpoint: ApiEndpointTest, Dataset: DatasetCustomTest, Token: AuthorizationTokenTest}
						resourceToRequestsResult :=
							generativeAiEvaluationReconciler.MapResourceToApiCalls(
								&preconditionValidationResult{
									k8sName:      "dash0-generative-ai-evaluation",
									k8sNamespace: TestNamespaceName,
									resource:     generativeAiEvaluation,
								},
								apiConfig, upsertAction, logger)
						Expect(resourceToRequestsResult.ApiRequests).To(BeEmpty())
						synchronizationError, httpStatusCode := firstSynchronizationErrorAndStatusCode(resourceToRequestsResult)
						Expect(synchronizationError).To(ContainSubstring("unable to convert the generative AI evaluation"))
						Expect(httpStatusCode).To(Equal(http.StatusBadRequest))
						Expect(isRetryableSynchronizationError(synchronizationError, httpStatusCode)).To(BeFalse())
					},
				)
			},
		)

		Describe(
			"the CRD schema", func() {
				AfterEach(
					func() {
						deleteGenerativeAiEvaluationResourceIfItExists(
							ctx, k8sClient, TestNamespaceName, generativeAiEvaluationName)
					},
				)

				DescribeTable(
					"validates the judge output",
					func(output dash0v1alpha1.Dash0GenerativeAiEvaluationJudgeOutput, expectedError string) {
						resource := createGenerativeAiEvaluationResource(TestNamespaceName, generativeAiEvaluationName)
						resource.Spec.Judge.Output = output
						err := k8sClient.Create(ctx, resource)
						if expectedError == "" {
							Expect(err).ToNot(HaveOccurred())
						} else {
							Expect(err).To(MatchError(ContainSubstring(expectedError)))
						}
					},
					Entry(
						"accepts a numeric output without a threshold",
						dash0v1alpha1.Dash0GenerativeAiEvaluationJudgeOutput{Kind: "numeric"},
						"",
					),
					Entry(
						"accepts a boolean output",
						dash0v1alpha1.Dash0GenerativeAiEvaluationJudgeOutput{Kind: "boolean"},
						"",
					),
					Entry(
						"accepts a categorical output with options",
						dash0v1alpha1.Dash0GenerativeAiEvaluationJudgeOutput{
							Kind: "categorical",
							Spec: &dash0v1alpha1.Dash0GenerativeAiEvaluationJudgeOutputSpec{
								Options: []dash0v1alpha1.Dash0GenerativeAiEvaluationJudgeOutputCategoricalOption{
									{Label: "relevant", Intent: ptr.To("positive")},
									{Label: "unclear"},
								},
							},
						},
						"",
					),
					Entry(
						"rejects an unknown kind",
						dash0v1alpha1.Dash0GenerativeAiEvaluationJudgeOutput{Kind: "score"},
						"spec.judge.output.kind: Unsupported value",
					),
					Entry(
						"rejects a spec on a boolean output",
						dash0v1alpha1.Dash0GenerativeAiEvaluationJudgeOutput{
							Kind: "boolean",
							Spec: &dash0v1alpha1.Dash0GenerativeAiEvaluationJudgeOutputSpec{Threshold: ptr.To(0.5)},
						},
						"spec must not be set when kind is 'boolean'",
					),
					Entry(
						"rejects options on a numeric output",
						dash0v1alpha1.Dash0GenerativeAiEvaluationJudgeOutput{
							Kind: "numeric",
							Spec: &dash0v1alpha1.Dash0GenerativeAiEvaluationJudgeOutputSpec{
								Options: []dash0v1alpha1.Dash0GenerativeAiEvaluationJudgeOutputCategoricalOption{
									{Label: "relevant"},
								},
							},
						},
						"spec.options must not be set when kind is 'numeric'",
					),
					Entry(
						"rejects a categorical output without options",
						dash0v1alpha1.Dash0GenerativeAiEvaluationJudgeOutput{Kind: "categorical"},
						"spec.options must be set and spec.threshold must not be set when kind is 'categorical'",
					),
					Entry(
						"rejects a threshold on a categorical output",
						dash0v1alpha1.Dash0GenerativeAiEvaluationJudgeOutput{
							Kind: "categorical",
							Spec: &dash0v1alpha1.Dash0GenerativeAiEvaluationJudgeOutputSpec{
								Threshold: ptr.To(0.5),
								Options: []dash0v1alpha1.Dash0GenerativeAiEvaluationJudgeOutputCategoricalOption{
									{Label: "relevant"},
								},
							},
						},
						"spec.options must be set and spec.threshold must not be set when kind is 'categorical'",
					),
					Entry(
						"rejects a duplicate label",
						dash0v1alpha1.Dash0GenerativeAiEvaluationJudgeOutput{
							Kind: "categorical",
							Spec: &dash0v1alpha1.Dash0GenerativeAiEvaluationJudgeOutputSpec{
								Options: []dash0v1alpha1.Dash0GenerativeAiEvaluationJudgeOutputCategoricalOption{
									{Label: "relevant"},
									{Label: "relevant", Intent: ptr.To("negative")},
								},
							},
						},
						"Duplicate value",
					),
					Entry(
						"rejects a threshold above 1",
						dash0v1alpha1.Dash0GenerativeAiEvaluationJudgeOutput{
							Kind: "numeric",
							Spec: &dash0v1alpha1.Dash0GenerativeAiEvaluationJudgeOutputSpec{Threshold: ptr.To(1.5)},
						},
						"spec.judge.output.spec.threshold: Invalid value",
					),
				)

				It("rejects a change of the output kind, but accepts other changes", func() {
					resource := createGenerativeAiEvaluationResource(TestNamespaceName, generativeAiEvaluationName)
					Expect(k8sClient.Create(ctx, resource)).To(Succeed())

					resource.Spec.Judge.Output.Spec.Threshold = ptr.To(0.9)
					resource.Spec.Sample.Rate = 0.5
					Expect(k8sClient.Update(ctx, resource)).To(Succeed())

					resource.Spec.Judge.Output = dash0v1alpha1.Dash0GenerativeAiEvaluationJudgeOutput{Kind: "boolean"}
					Expect(k8sClient.Update(ctx, resource)).To(
						MatchError(ContainSubstring("kind cannot change after the eval has been created")))
				})
			},
		)
	},
)

func createGenerativeAiEvaluationReconciler(clusterId string) *GenerativeAiEvaluationReconciler {
	generativeAiEvaluationReconciler := NewGenerativeAiEvaluationReconciler(
		k8sClient,
		types.UID(clusterId),
		generativeAiEvaluationLeaderElectionAware,
		testApiClientPool(),
	)
	return generativeAiEvaluationReconciler
}

func expectGenerativeAiEvaluationPutRequestCustom(
	clusterId string,
	expectedPath string,
	endpoint string,
	dataset string,
	token string,
	originPattern string,
	times int,
) {
	gock.New(endpoint).
		Put(expectedPath).
		MatchHeader("Authorization", token).
		MatchParam("dataset", dataset).
		Times(times).
		Reply(200).
		JSON(generativeAiEvaluationPutResponse(clusterId, originPattern, dataset))
}

func expectGenerativeAiEvaluationPutRequest(clusterId string, expectedPath string) {
	expectGenerativeAiEvaluationPutRequestCustom(
		clusterId,
		expectedPath,
		ApiEndpointTest,
		DatasetCustomTest,
		AuthorizationHeaderTest,
		generativeAiEvaluationOriginPattern,
		1,
	)
}

func generativeAiEvaluationPutResponse(clusterId string, originPattern string, dataset string) map[string]any {
	return map[string]any{
		"metadata": map[string]any{
			"labels": map[string]any{
				"dash0.com/id":      generativeAiEvaluationId,
				"dash0.com/origin":  fmt.Sprintf(originPattern, clusterId),
				"dash0.com/dataset": dataset,
			},
		},
	}
}

func expectGenerativeAiEvaluationDeleteRequest(expectedPath string) {
	expectGenerativeAiEvaluationDeleteRequestWithHttpStatus(expectedPath, http.StatusOK)
}

func expectGenerativeAiEvaluationDeleteRequestWithHttpStatus(expectedPath string, status int) {
	gock.New(ApiEndpointTest).
		Delete(expectedPath).
		MatchHeader("Authorization", AuthorizationHeaderTest).
		MatchParam("dataset", DatasetCustomTest).
		Times(1).
		Reply(status)
}

func createGenerativeAiEvaluationResource(namespace string, name string) *dash0v1alpha1.Dash0GenerativeAiEvaluation {
	return createGenerativeAiEvaluationResourceWithEnableLabel(namespace, name, "")
}

func createGenerativeAiEvaluationResourceWithEnableLabel(
	namespace string,
	name string,
	dash0EnableLabelValue string,
) *dash0v1alpha1.Dash0GenerativeAiEvaluation {
	objectMeta := metav1.ObjectMeta{
		Name:      name,
		Namespace: namespace,
	}
	if dash0EnableLabelValue != "" {
		objectMeta.Labels = map[string]string{
			"dash0.com/enable": dash0EnableLabelValue,
		}
	}
	return &dash0v1alpha1.Dash0GenerativeAiEvaluation{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "operator.dash0.com/v1alpha1",
			Kind:       "Dash0GenerativeAiEvaluation",
		},
		ObjectMeta: objectMeta,
		Spec: dash0v1alpha1.Dash0GenerativeAiEvaluationSpec{
			Enabled: true,
			Display: dash0v1alpha1.Dash0GenerativeAiEvaluationDisplay{
				Name: "Answer relevance",
			},
			Match: dash0v1alpha1.Dash0GenerativeAiEvaluationMatch{
				Filters: []dash0v1alpha1.Dash0GenerativeAiEvaluationAttributeFilter{
					{Key: "gen_ai.operation.name", Operator: "is_set"},
				},
			},
			Sample: dash0v1alpha1.Dash0GenerativeAiEvaluationSampling{
				Rate: 0.1,
			},
			Judge: dash0v1alpha1.Dash0GenerativeAiEvaluationJudge{
				Model:        "light",
				SystemPrompt: "You review the answers of an AI assistant.",
				UserPrompt:   "Answer: {{ gen_ai.output.messages }}",
				Output: dash0v1alpha1.Dash0GenerativeAiEvaluationJudgeOutput{
					Kind: "numeric",
					Spec: &dash0v1alpha1.Dash0GenerativeAiEvaluationJudgeOutputSpec{Threshold: ptr.To(0.7)},
				},
			},
		},
	}
}

func deleteGenerativeAiEvaluationResourceIfItExists(
	ctx context.Context,
	k8sClient client.Client,
	namespace string,
	name string,
) {
	generativeAiEvaluation := &dash0v1alpha1.Dash0GenerativeAiEvaluation{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
	}
	err := k8sClient.Delete(
		ctx, generativeAiEvaluation, &client.DeleteOptions{
			GracePeriodSeconds: new(int64),
		},
	)
	if err != nil && !apierrors.IsNotFound(err) {
		Expect(err).NotTo(HaveOccurred())
	}
}

func verifyGenerativeAiEvaluationSynchronizationStatus(
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
			generativeAiEvaluation := &dash0v1alpha1.Dash0GenerativeAiEvaluation{}
			err := k8sClient.Get(
				ctx, types.NamespacedName{
					Namespace: namespace,
					Name:      name,
				}, generativeAiEvaluation,
			)
			g.Expect(err).NotTo(HaveOccurred())
			g.Expect(generativeAiEvaluation.Status.SynchronizationStatus).To(Equal(expectedStatus))
			g.Expect(generativeAiEvaluation.Status.SynchronizedAt.Time).
				To(BeTemporally(">=", testStartedAt.Add(-1*time.Second)))
			g.Expect(generativeAiEvaluation.Status.SynchronizedAt.Time).To(BeTemporally("<=", time.Now()))
			// generative AI evaluations have no operator-side validations, all local validations are encoded in the CRD already
			g.Expect(generativeAiEvaluation.Status.ValidationIssues).To(BeNil())

			g.Expect(generativeAiEvaluation.Status.SynchronizationResults).To(HaveLen(1))
			syncResultPerEndpointAndDataset := generativeAiEvaluation.Status.SynchronizationResults[0]
			g.Expect(syncResultPerEndpointAndDataset).ToNot(BeNil())
			g.Expect(syncResultPerEndpointAndDataset.Dash0ApiEndpoint).To(Equal(expectedApiEndpoint))
			g.Expect(syncResultPerEndpointAndDataset.Dash0Dataset).To(Equal(expectedDataset))
			g.Expect(syncResultPerEndpointAndDataset.Dash0Id).To(Equal(expectedId))
			g.Expect(syncResultPerEndpointAndDataset.Dash0Origin).To(Equal(expectedOrigin))
			g.Expect(syncResultPerEndpointAndDataset.SynchronizationError).To(ContainSubstring(expectedError))
		},
	).Should(Succeed())
}

func verifyGenerativeAiEvaluationHasNoSynchronizationStatus(
	ctx context.Context,
	k8sClient client.Client,
	namespace string,
	name string,
) {
	Eventually(
		func(g Gomega) {
			generativeAiEvaluation := &dash0v1alpha1.Dash0GenerativeAiEvaluation{}
			err := k8sClient.Get(
				ctx, types.NamespacedName{
					Namespace: namespace,
					Name:      name,
				}, generativeAiEvaluation,
			)
			g.Expect(err).NotTo(HaveOccurred())
			g.Expect(string(generativeAiEvaluation.Status.SynchronizationStatus)).To(Equal(""))
			g.Expect(generativeAiEvaluation.Status.ValidationIssues).To(BeNil())
			g.Expect(generativeAiEvaluation.Status.SynchronizationResults).To(HaveLen(0))
		},
	).Should(Succeed())
}
