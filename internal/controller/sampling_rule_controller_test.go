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

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	"sigs.k8s.io/yaml"

	dash0common "github.com/dash0hq/dash0-operator/api/operator/common"
	dash0v1alpha1 "github.com/dash0hq/dash0-operator/api/operator/v1alpha1"
	"github.com/dash0hq/dash0-operator/internal/util"
	"github.com/dash0hq/dash0-operator/internal/util/logd"

	"github.com/h2non/gock"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	. "github.com/dash0hq/dash0-operator/test/util"
)

const (
	samplingRuleName        = "test-sampling-rule"
	samplingRuleName2       = "test-sampling-rule-2"
	samplingRuleApiBasePath = "/api/sampling-rules/"

	samplingRuleId             = "sampling-rule-id"
	samplingRuleOriginPattern  = "dash0-operator_%s_test-dataset_test-sampling-rule"
	samplingRuleOriginPattern2 = "dash0-operator_%s_test-dataset_test-sampling-rule-2"
)

var (
	defaultExpectedPathSamplingRule = fmt.Sprintf(
		"%s.*%s",
		samplingRuleApiBasePath,
		"dash0-operator_.*_test-dataset_test-sampling-rule",
	)
	defaultExpectedPathSamplingRule2 = fmt.Sprintf(
		"%s.*%s",
		samplingRuleApiBasePath,
		"dash0-operator_.*_test-dataset_test-sampling-rule-2",
	)
)

var _ = Describe(
	"The Sampling Rule controller", Ordered, func() {
		ctx := context.Background()
		logger := logd.FromContext(ctx)
		var testStartedAt time.Time
		var clusterId string

		BeforeAll(
			func() {
				EnsureTestNamespaceExists(ctx, k8sClient)
				EnsureOperatorNamespaceExists(ctx, k8sClient)
				clusterId = string(util.ReadPseudoClusterUid(ctx, k8sClient, logger))
			},
		)

		BeforeEach(
			func() {
				testStartedAt = time.Now()
			},
		)

		Describe(
			"the sampling rule reconciler", func() {
				var samplingRuleReconciler *SamplingRuleReconciler

				BeforeEach(
					func() {
						samplingRuleReconciler = createSamplingRuleReconciler(clusterId)

						samplingRuleReconciler.defaultApiConfigs.Set(
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
						deleteSamplingRuleResourceIfItExists(ctx, k8sClient, samplingRuleName)
						deleteSamplingRuleResourceIfItExists(ctx, k8sClient, samplingRuleName2)
					},
				)

				It(
					"it ignores sampling rule resource changes if the API endpoint is not configured", func() {
						expectSamplingRulePutRequest(clusterId, defaultExpectedPathSamplingRule)
						defer gock.Off()

						samplingRuleReconciler.RemoveDefaultApiConfigs(ctx, logger)

						samplingRuleResource := createSamplingRuleResource(samplingRuleName)
						Expect(k8sClient.Create(ctx, samplingRuleResource)).To(Succeed())
						result, err := samplingRuleReconciler.Reconcile(
							ctx, reconcile.Request{
								NamespacedName: types.NamespacedName{
									Name: samplingRuleName,
								},
							},
						)
						Expect(err).NotTo(HaveOccurred())
						Expect(result).To(Equal(reconcile.Result{}))

						Expect(gock.IsPending()).To(BeTrue())
						verifySamplingRuleHasNoSynchronizationStatus(
							ctx,
							k8sClient,
							samplingRuleName,
						)
					},
				)

				It(
					"creates a sampling rule", func() {
						expectSamplingRulePutRequest(clusterId, defaultExpectedPathSamplingRule)
						defer gock.Off()

						samplingRuleResource := createSamplingRuleResource(samplingRuleName)
						Expect(k8sClient.Create(ctx, samplingRuleResource)).To(Succeed())

						result, err := samplingRuleReconciler.Reconcile(
							ctx, reconcile.Request{
								NamespacedName: types.NamespacedName{
									Name: samplingRuleName,
								},
							},
						)
						Expect(err).NotTo(HaveOccurred())
						Expect(result).To(Equal(reconcile.Result{}))

						verifySamplingRuleSynchronizationStatus(
							ctx,
							k8sClient,
							samplingRuleName,
							dash0common.Dash0ApiResourceSynchronizationStatusSuccessful,
							testStartedAt,
							samplingRuleId,
							fmt.Sprintf(samplingRuleOriginPattern, clusterId),
							ApiEndpointStandardizedTest,
							DatasetCustomTest,
							"",
						)
						Expect(gock.IsDone()).To(BeTrue())
					},
				)

				It(
					"updates a sampling rule", func() {
						expectSamplingRulePutRequest(clusterId, defaultExpectedPathSamplingRule)
						defer gock.Off()

						samplingRuleResource := createSamplingRuleResource(samplingRuleName)
						Expect(k8sClient.Create(ctx, samplingRuleResource)).To(Succeed())

						samplingRuleResource.Spec.Display = &dash0v1alpha1.Dash0SamplingRuleDisplay{
							Name: "Updated Sampling Rule",
						}
						Expect(k8sClient.Update(ctx, samplingRuleResource)).To(Succeed())

						result, err := samplingRuleReconciler.Reconcile(
							ctx, reconcile.Request{
								NamespacedName: types.NamespacedName{
									Name: samplingRuleName,
								},
							},
						)
						Expect(err).NotTo(HaveOccurred())
						Expect(result).To(Equal(reconcile.Result{}))

						verifySamplingRuleSynchronizationStatus(
							ctx,
							k8sClient,
							samplingRuleName,
							dash0common.Dash0ApiResourceSynchronizationStatusSuccessful,
							testStartedAt,
							samplingRuleId,
							fmt.Sprintf(samplingRuleOriginPattern, clusterId),
							ApiEndpointStandardizedTest,
							DatasetCustomTest,
							"",
						)
						Expect(gock.IsDone()).To(BeTrue())
					},
				)

				It(
					"deletes a sampling rule", func() {
						expectSamplingRuleDeleteRequest(defaultExpectedPathSamplingRule)
						defer gock.Off()

						samplingRuleResource := createSamplingRuleResource(samplingRuleName)
						Expect(k8sClient.Create(ctx, samplingRuleResource)).To(Succeed())
						Expect(k8sClient.Delete(ctx, samplingRuleResource)).To(Succeed())

						result, err := samplingRuleReconciler.Reconcile(
							ctx, reconcile.Request{
								NamespacedName: types.NamespacedName{
									Name: samplingRuleName,
								},
							},
						)
						Expect(err).NotTo(HaveOccurred())
						Expect(result).To(Equal(reconcile.Result{}))

						Expect(gock.IsDone()).To(BeTrue())
					},
				)

				It(
					"deletes a sampling rule if labelled with dash0.com/enable=false", func() {
						expectSamplingRuleDeleteRequestWithHttpStatus(defaultExpectedPathSamplingRule, http.StatusNotFound)
						defer gock.Off()

						samplingRuleResource :=
							createSamplingRuleResourceWithEnableLabel(samplingRuleName, "false")
						Expect(k8sClient.Create(ctx, samplingRuleResource)).To(Succeed())

						result, err := samplingRuleReconciler.Reconcile(
							ctx, reconcile.Request{
								NamespacedName: types.NamespacedName{
									Name: samplingRuleName,
								},
							},
						)
						Expect(err).NotTo(HaveOccurred())
						Expect(result).To(Equal(reconcile.Result{}))

						verifySamplingRuleSynchronizationStatus(
							ctx,
							k8sClient,
							samplingRuleName,
							dash0common.Dash0ApiResourceSynchronizationStatusSuccessful,
							testStartedAt,
							"",
							fmt.Sprintf(samplingRuleOriginPattern, clusterId),
							ApiEndpointStandardizedTest,
							DatasetCustomTest,
							"",
						)
						Expect(gock.IsDone()).To(BeTrue())
					},
				)

				It(
					"reports http errors when synchronizing a sampling rule", func() {
						gock.New(ApiEndpointTest).
							Put(defaultExpectedPathSamplingRule).
							MatchParam("dataset", DatasetCustomTest).
							Times(3).
							Reply(503).
							JSON(map[string]string{})
						defer gock.Off()

						samplingRuleResource := createSamplingRuleResource(samplingRuleName)
						Expect(k8sClient.Create(ctx, samplingRuleResource)).To(Succeed())

						result, err := samplingRuleReconciler.Reconcile(
							ctx, reconcile.Request{
								NamespacedName: types.NamespacedName{
									Name: samplingRuleName,
								},
							},
						)
						Expect(err).NotTo(HaveOccurred())
						Expect(result).To(Equal(reconcile.Result{}))

						verifySamplingRuleSynchronizationStatus(
							ctx,
							k8sClient,
							samplingRuleName,
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
					"retries synchronization when synchronizing a sampling rule", func() {
						gock.New(ApiEndpointTest).
							Put(defaultExpectedPathSamplingRule).
							MatchParam("dataset", DatasetCustomTest).
							Times(2).
							Reply(503).
							JSON(map[string]string{})
						expectSamplingRulePutRequest(clusterId, defaultExpectedPathSamplingRule)
						defer gock.Off()

						samplingRuleResource := createSamplingRuleResource(samplingRuleName)
						Expect(k8sClient.Create(ctx, samplingRuleResource)).To(Succeed())

						result, err := samplingRuleReconciler.Reconcile(
							ctx, reconcile.Request{
								NamespacedName: types.NamespacedName{
									Name: samplingRuleName,
								},
							},
						)
						Expect(err).NotTo(HaveOccurred())
						Expect(result).To(Equal(reconcile.Result{}))

						verifySamplingRuleSynchronizationStatus(
							ctx,
							k8sClient,
							samplingRuleName,
							dash0common.Dash0ApiResourceSynchronizationStatusSuccessful,
							testStartedAt,
							samplingRuleId,
							fmt.Sprintf(samplingRuleOriginPattern, clusterId),
							ApiEndpointStandardizedTest,
							DatasetCustomTest,
							"",
						)
						Expect(gock.IsDone()).To(BeTrue())
					},
				)

				type maybeDoInitialSynchronizationOfAllSamplingRulesTest struct {
					disableSync func()
					enabledSync func()
				}

				DescribeTable(
					"synchronizes all existing sampling rule resources when the auth token or api endpoint become available",
					func(testConfig maybeDoInitialSynchronizationOfAllSamplingRulesTest) {
						testConfig.disableSync()

						expectSamplingRulePutRequest(clusterId, defaultExpectedPathSamplingRule)
						expectSamplingRulePutRequest(clusterId, defaultExpectedPathSamplingRule2)
						defer gock.Off()

						samplingRuleResource1 := createSamplingRuleResource(samplingRuleName)
						Expect(k8sClient.Create(ctx, samplingRuleResource1)).To(Succeed())
						samplingRuleResource2 := createSamplingRuleResource(samplingRuleName2)
						Expect(k8sClient.Create(ctx, samplingRuleResource2)).To(Succeed())

						Expect(gock.IsPending()).To(BeTrue())
						verifySamplingRuleHasNoSynchronizationStatus(ctx, k8sClient, samplingRuleName)
						verifySamplingRuleHasNoSynchronizationStatus(ctx, k8sClient, samplingRuleName2)

						testConfig.enabledSync()

						verifySamplingRuleSynchronizationStatus(
							ctx,
							k8sClient,
							samplingRuleName,
							dash0common.Dash0ApiResourceSynchronizationStatusSuccessful,
							testStartedAt,
							samplingRuleId,
							fmt.Sprintf(samplingRuleOriginPattern, clusterId),
							ApiEndpointStandardizedTest,
							DatasetCustomTest,
							"",
						)
						verifySamplingRuleSynchronizationStatus(
							ctx,
							k8sClient,
							samplingRuleName2,
							dash0common.Dash0ApiResourceSynchronizationStatusSuccessful,
							testStartedAt,
							samplingRuleId,
							fmt.Sprintf(samplingRuleOriginPattern2, clusterId),
							ApiEndpointStandardizedTest,
							DatasetCustomTest,
							"",
						)
						Expect(gock.IsDone()).To(BeTrue())
					},
					Entry(
						"when the auth token becomes available", maybeDoInitialSynchronizationOfAllSamplingRulesTest{
							disableSync: func() {
								samplingRuleReconciler.RemoveDefaultApiConfigs(ctx, logger)
							},
							enabledSync: func() {
								samplingRuleReconciler.SetDefaultApiConfigs(
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
						"when the operator manager becomes leader", maybeDoInitialSynchronizationOfAllSamplingRulesTest{
							disableSync: func() {
								leaderElectionAware.SetLeader(false)
							},
							enabledSync: func() {
								leaderElectionAware.SetLeader(true)
								samplingRuleReconciler.NotifyOperatorManagerJustBecameLeader(ctx, logger)
							},
						},
					),
					Entry(
						"only sync once even if the auth token is set multiple times",
						maybeDoInitialSynchronizationOfAllSamplingRulesTest{
							disableSync: func() {
								samplingRuleReconciler.RemoveDefaultApiConfigs(ctx, logger)
							},
							enabledSync: func() {
								samplingRuleReconciler.SetDefaultApiConfigs(
									ctx, []ApiConfig{
										{
											Endpoint: ApiEndpointTest,
											Dataset:  DatasetCustomTest,
											Token:    AuthorizationTokenTest,
										},
									}, logger,
								)
								samplingRuleReconciler.SetDefaultApiConfigs(
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
				)
			},
		)

		Describe(
			"mapping sampling rule resources to http requests", func() {

				var samplingRuleReconciler *SamplingRuleReconciler

				BeforeEach(
					func() {
						samplingRuleReconciler = &SamplingRuleReconciler{}
					},
				)

				It("should transform sampling rule to Dash0 API payload format", func() {
					samplingRule := map[string]interface{}{}
					Expect(yaml.Unmarshal([]byte(`
apiVersion: operator.dash0.com/v1alpha1
kind: Dash0SamplingRule
metadata:
  name: dash0-sampling-rule
  annotations:
    dash0com/annotation1: value1
spec:
  enabled: true
  conditions:
    kind: probabilistic
    spec:
      rate: "0.5"
`), &samplingRule)).To(Succeed())
					apiConfig := ApiConfig{
						Endpoint: ApiEndpointTest,
						Dataset:  DatasetCustomTest,
						Token:    AuthorizationTokenTest,
					}
					preconditionValidationResult := &preconditionValidationResult{
						k8sName:  "dash0-sampling-rule",
						resource: samplingRule,
						validatedApiConfigs: []ValidatedApiConfigAndToken{
							*NewValidatedApiConfigAndToken(apiConfig.Endpoint, apiConfig.Dataset, apiConfig.Token),
						},
					}
					resourceToRequestsResult :=
						samplingRuleReconciler.MapResourceToHttpRequests(
							preconditionValidationResult,
							apiConfig,
							upsertAction,
							logger,
						)
					Expect(resourceToRequestsResult.TotalProcessed()).To(Equal(1))
					Expect(resourceToRequestsResult.SynchronizationErrors).To(BeNil())
					Expect(resourceToRequestsResult.ApiRequests).To(HaveLen(1))

					apiRequest := resourceToRequestsResult.ApiRequests[0]
					req := apiRequest.Request
					defer func() {
						_ = req.Body.Close()
					}()
					body, err := io.ReadAll(req.Body)
					Expect(err).ToNot(HaveOccurred())
					payload := map[string]interface{}{}
					Expect(json.Unmarshal(body, &payload)).To(Succeed())

					Expect(payload["kind"]).To(Equal("Dash0Sampling"))
					Expect(payload["apiVersion"]).To(BeNil())
					Expect(payload["status"]).To(BeNil())

					Expect(ReadFromMap(payload, []string{"metadata", "name"})).To(Equal("dash0-sampling-rule"))
					Expect(ReadFromMap(payload, []string{"metadata", "annotations"})).To(BeNil())
					Expect(ReadFromMap(payload, []string{"metadata", "namespace"})).To(BeNil())

					Expect(payload["spec"]).ToNot(BeNil())
					rate := ReadFromMap(payload, []string{"spec", "conditions", "spec", "rate"})
					Expect(rate).To(BeNumerically("==", 0.5))
				})
			},
		)
	},
)

func createSamplingRuleReconciler(clusterId string) *SamplingRuleReconciler {
	return NewSamplingRuleReconciler(
		k8sClient,
		types.UID(clusterId),
		leaderElectionAware,
		TestHTTPClient(),
	)
}

func expectSamplingRulePutRequestCustom(
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
		JSON(samplingRulePutResponse(clusterId, originPattern, dataset))
}

func expectSamplingRulePutRequest(clusterId string, expectedPath string) {
	expectSamplingRulePutRequestCustom(
		clusterId,
		expectedPath,
		ApiEndpointTest,
		DatasetCustomTest,
		AuthorizationHeaderTest,
		samplingRuleOriginPattern,
		1,
	)
}

func samplingRulePutResponse(clusterId string, originPattern string, dataset string) map[string]interface{} {
	return map[string]interface{}{
		"metadata": map[string]interface{}{
			"labels": map[string]interface{}{
				"dash0.com/id":      samplingRuleId,
				"dash0.com/origin":  fmt.Sprintf(originPattern, clusterId),
				"dash0.com/dataset": dataset,
			},
		},
	}
}

func expectSamplingRuleDeleteRequest(expectedPath string) {
	expectSamplingRuleDeleteRequestWithHttpStatus(expectedPath, http.StatusOK)
}

func expectSamplingRuleDeleteRequestWithHttpStatus(expectedPath string, status int) {
	gock.New(ApiEndpointTest).
		Delete(expectedPath).
		MatchHeader("Authorization", AuthorizationHeaderTest).
		MatchParam("dataset", DatasetCustomTest).
		Times(1).
		Reply(status)
}

func createSamplingRuleResource(name string) *dash0v1alpha1.Dash0SamplingRule {
	return createSamplingRuleResourceWithEnableLabel(name, "")
}

func createSamplingRuleResourceWithEnableLabel(
	name string,
	dash0EnableLabelValue string,
) *dash0v1alpha1.Dash0SamplingRule {
	objectMeta := metav1.ObjectMeta{
		Name: name,
	}
	if dash0EnableLabelValue != "" {
		objectMeta.Labels = map[string]string{
			"dash0.com/enable": dash0EnableLabelValue,
		}
	}
	return &dash0v1alpha1.Dash0SamplingRule{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "operator.dash0.com/v1alpha1",
			Kind:       "Dash0SamplingRule",
		},
		ObjectMeta: objectMeta,
		Spec: dash0v1alpha1.Dash0SamplingRuleSpec{
			Enabled: true,
			Display: &dash0v1alpha1.Dash0SamplingRuleDisplay{
				Name: "Test Sampling Rule",
			},
			Conditions: dash0v1alpha1.Dash0SamplingRuleCondition{
				Kind: "probabilistic",
				Spec: &dash0v1alpha1.Dash0SamplingRuleConditionSpec{
					Rate: ptr.To("0.5"),
				},
			},
		},
	}
}

func deleteSamplingRuleResourceIfItExists(
	ctx context.Context,
	k8sClient client.Client,
	name string,
) {
	samplingRule := &dash0v1alpha1.Dash0SamplingRule{
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
		},
	}
	_ = k8sClient.Delete(
		ctx, samplingRule, &client.DeleteOptions{
			GracePeriodSeconds: new(int64),
		},
	)
}

//nolint:unparam
func verifySamplingRuleSynchronizationStatus(
	ctx context.Context,
	k8sClient client.Client,
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
			samplingRule := &dash0v1alpha1.Dash0SamplingRule{}
			err := k8sClient.Get(
				ctx, types.NamespacedName{
					Name: name,
				}, samplingRule,
			)
			g.Expect(err).NotTo(HaveOccurred())
			g.Expect(samplingRule.Status.SynchronizationStatus).To(Equal(expectedStatus))
			g.Expect(samplingRule.Status.SynchronizedAt.Time).To(BeTemporally(">=", testStartedAt.Add(-1*time.Second)))
			g.Expect(samplingRule.Status.SynchronizedAt.Time).To(BeTemporally("<=", time.Now()))
			g.Expect(samplingRule.Status.ValidationIssues).To(BeNil())

			g.Expect(samplingRule.Status.SynchronizationResults).To(HaveLen(1))
			syncResultPerEndpointAndDataset := samplingRule.Status.SynchronizationResults[0]
			g.Expect(syncResultPerEndpointAndDataset).ToNot(BeNil())
			g.Expect(syncResultPerEndpointAndDataset.Dash0ApiEndpoint).To(Equal(expectedApiEndpoint))
			g.Expect(syncResultPerEndpointAndDataset.Dash0Dataset).To(Equal(expectedDataset))
			g.Expect(syncResultPerEndpointAndDataset.Dash0Id).To(Equal(expectedId))
			g.Expect(syncResultPerEndpointAndDataset.Dash0Origin).To(Equal(expectedOrigin))
			g.Expect(syncResultPerEndpointAndDataset.SynchronizationError).To(ContainSubstring(expectedError))
		},
	).Should(Succeed())
}

func verifySamplingRuleHasNoSynchronizationStatus(
	ctx context.Context,
	k8sClient client.Client,
	name string,
) {
	Eventually(
		func(g Gomega) {
			samplingRule := &dash0v1alpha1.Dash0SamplingRule{}
			err := k8sClient.Get(
				ctx, types.NamespacedName{
					Name: name,
				}, samplingRule,
			)
			g.Expect(err).NotTo(HaveOccurred())
			g.Expect(string(samplingRule.Status.SynchronizationStatus)).To(Equal(""))
			g.Expect(samplingRule.Status.ValidationIssues).To(BeNil())
			g.Expect(samplingRule.Status.SynchronizationResults).To(HaveLen(0))
		},
	).Should(Succeed())
}
