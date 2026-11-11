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
	generativeAiEvaluationName          = "test-generative-ai-evaluation"
	generativeAiEvaluationId            = "generative-ai-evaluation-id"
	generativeAiEvaluationOriginPattern = "dash0-operator_%s_test-dataset_test-namespace_test-generative-ai-evaluation"
)

var (
	generativeAiEvaluationLeaderElectionAware = NewLeaderElectionAwareMock(true)
)

var _ = Describe(
	"The GenerativeAiEvaluation controller", Ordered, func() {
		var clusterId string

		ctx := context.Background()
		logger := logd.FromContext(ctx)

		BeforeAll(
			func() {
				EnsureTestNamespaceExists(ctx, k8sClient)
				EnsureOperatorNamespaceExists(ctx, k8sClient)
				clusterId = string(cluster.ReadPseudoClusterUid(ctx, k8sClient, logger))
			},
		)

		AfterEach(
			func() {
				DeleteMonitoringResourceIfItExists(ctx, k8sClient)
				deleteGenerativeAiEvaluationResourceIfItExists(ctx, TestNamespaceName, generativeAiEvaluationName)
			},
		)

		It(
			"creates a generative AI evaluation", func() {
				testStartedAt := time.Now()
				EnsureMonitoringResourceWithoutExportExistsAndIsAvailable(ctx, k8sClient)

				generativeAiEvaluationReconciler := NewGenerativeAiEvaluationReconciler(
					k8sClient,
					types.UID(clusterId),
					generativeAiEvaluationLeaderElectionAware,
					testApiClientPool(),
				)
				generativeAiEvaluationReconciler.defaultApiConfigs.Set(
					[]ApiConfig{{Endpoint: ApiEndpointTest, Dataset: DatasetCustomTest, Token: AuthorizationTokenTest}},
				)

				gock.New(ApiEndpointTest).
					Put("/api/generative-ai-evaluations/dash0-operator_.*_test-dataset_test-namespace_test-generative-ai-evaluation").
					MatchHeader("Authorization", AuthorizationHeaderTest).
					MatchParam("dataset", DatasetCustomTest).
					Times(1).
					Reply(200).
					JSON(map[string]any{
						"metadata": map[string]any{
							"labels": map[string]any{
								"dash0.com/id":      generativeAiEvaluationId,
								"dash0.com/origin":  fmt.Sprintf(generativeAiEvaluationOriginPattern, clusterId),
								"dash0.com/dataset": DatasetCustomTest,
							},
						},
					})
				defer gock.Off()

				Expect(k8sClient.Create(
					ctx, createGenerativeAiEvaluationResource(TestNamespaceName, generativeAiEvaluationName),
				)).To(Succeed())

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

				Eventually(
					func(g Gomega) {
						generativeAiEvaluation := &dash0v1alpha1.Dash0GenerativeAiEvaluation{}
						g.Expect(k8sClient.Get(
							ctx,
							types.NamespacedName{Namespace: TestNamespaceName, Name: generativeAiEvaluationName},
							generativeAiEvaluation,
						)).To(Succeed())
						status := generativeAiEvaluation.Status
						g.Expect(status.SynchronizationStatus).To(
							Equal(dash0common.Dash0ApiResourceSynchronizationStatusSuccessful))
						g.Expect(status.SynchronizedAt.Time).To(BeTemporally(">=", testStartedAt.Add(-1*time.Second)))
						g.Expect(status.ValidationIssues).To(BeNil())
						g.Expect(status.SynchronizationResults).To(HaveLen(1))
						syncResult := status.SynchronizationResults[0]
						g.Expect(syncResult.Dash0ApiEndpoint).To(Equal(ApiEndpointStandardizedTest))
						g.Expect(syncResult.Dash0Dataset).To(Equal(DatasetCustomTest))
						g.Expect(syncResult.Dash0Id).To(Equal(generativeAiEvaluationId))
						g.Expect(syncResult.Dash0Origin).To(
							Equal(fmt.Sprintf(generativeAiEvaluationOriginPattern, clusterId)))
						g.Expect(syncResult.SynchronizationError).To(BeEmpty())
					},
				).Should(Succeed())
				Expect(gock.IsDone()).To(BeTrue())
			},
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
					(&GenerativeAiEvaluationReconciler{}).MapResourceToApiCalls(
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

		Describe(
			"the CRD schema", func() {
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

func createGenerativeAiEvaluationResource(namespace string, name string) *dash0v1alpha1.Dash0GenerativeAiEvaluation {
	return &dash0v1alpha1.Dash0GenerativeAiEvaluation{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "operator.dash0.com/v1alpha1",
			Kind:       "Dash0GenerativeAiEvaluation",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
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

func deleteGenerativeAiEvaluationResourceIfItExists(ctx context.Context, namespace string, name string) {
	err := k8sClient.Delete(
		ctx,
		&dash0v1alpha1.Dash0GenerativeAiEvaluation{ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: namespace}},
		&client.DeleteOptions{GracePeriodSeconds: new(int64)},
	)
	if err != nil && !apierrors.IsNotFound(err) {
		Expect(err).NotTo(HaveOccurred())
	}
}
