// SPDX-FileCopyrightText: Copyright 2026 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package controller

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"time"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	dash0dashv1alpha1 "github.com/dash0hq/dash0-operator/api/dash0/v1alpha1"
	dash0common "github.com/dash0hq/dash0-operator/api/operator/common"
	"github.com/dash0hq/dash0-operator/internal/util/cluster"
	"github.com/dash0hq/dash0-operator/internal/util/logd"

	"github.com/h2non/gock"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	. "github.com/dash0hq/dash0-operator/test/util"
)

const (
	teamName            = "backend-team"
	teamApiBasePath     = "/api/teams/"
	teamId              = "00000000-0000-0000-0000-000000000001"
	teamOriginPattern   = "dash0-operator_%s_test-namespace_backend-team"
	teamDisplayName     = "Backend Team"
	teamDisplayDesc     = "Owns backend services and the data platform."
	teamColorFrom       = "#6366F1"
	teamColorTo         = "#8B5CF6"
	teamMemberAlice     = "alice@example.com"
	teamMemberBob       = "bob@example.com"
	unresolvableEmail   = "ghost@example.com"
	unresolvableMessage = "the following emails could not be resolved to any organization member: ghost@example.com"
)

var (
	defaultExpectedPathTeam = fmt.Sprintf(
		"%s.*%s",
		teamApiBasePath,
		"dash0-operator_.*_test-namespace_backend-team",
	)
	teamLeaderElectionAware = NewLeaderElectionAwareMock(true)
)

var _ = Describe(
	"The Team controller", Ordered, func() {
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
				VerifyNoUnmatchedGockRequests()
				DeleteMonitoringResource(ctx, k8sClient)
			},
		)

		Describe(
			"the team reconciler", func() {
				var teamReconciler *TeamReconciler

				BeforeEach(
					func() {
						teamReconciler = createTeamReconciler(clusterId)

						// Set default API configs directly (not via SetDefaultApiConfigs) to avoid
						// triggering maybeDoInitialSynchronizationOfAllResources.
						teamReconciler.defaultApiConfigs.Set(
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
						deleteTeamResourceIfItExists(ctx, k8sClient, TestNamespaceName, teamName)
					},
				)

				It(
					"creates a team", func() {
						EnsureMonitoringResourceWithoutExportExistsAndIsAvailable(ctx, k8sClient)

						expectTeamPutRequest(clusterId, defaultExpectedPathTeam)
						defer gock.Off()

						teamResource := createTeamResource(TestNamespaceName, teamName)
						Expect(k8sClient.Create(ctx, teamResource)).To(Succeed())

						result, err := teamReconciler.Reconcile(
							ctx, reconcile.Request{
								NamespacedName: types.NamespacedName{
									Namespace: TestNamespaceName,
									Name:      teamName,
								},
							},
						)
						Expect(err).NotTo(HaveOccurred())
						Expect(result).To(Equal(reconcile.Result{}))

						verifyTeamSynchronizationStatus(
							ctx,
							k8sClient,
							TestNamespaceName,
							teamName,
							dash0common.Dash0ApiResourceSynchronizationStatusSuccessful,
							testStartedAt,
							teamId,
							fmt.Sprintf(teamOriginPattern, clusterId),
							ApiEndpointStandardizedTest,
							"",
						)
						Expect(gock.IsDone()).To(BeTrue())
					},
				)

				It(
					"updates a team", func() {
						EnsureMonitoringResourceWithoutExportExistsAndIsAvailable(ctx, k8sClient)

						expectTeamPutRequest(clusterId, defaultExpectedPathTeam)
						defer gock.Off()

						teamResource := createTeamResource(TestNamespaceName, teamName)
						Expect(k8sClient.Create(ctx, teamResource)).To(Succeed())

						// Modify the resource (mirrors operator-cr-updated.yaml: description tweak + membership change).
						teamResource.Spec.Display.Description = "Owns backend services, the data platform, and the on-call rotation."
						teamResource.Spec.Members = []string{teamMemberAlice, "carol@example.com"}
						Expect(k8sClient.Update(ctx, teamResource)).To(Succeed())

						result, err := teamReconciler.Reconcile(
							ctx, reconcile.Request{
								NamespacedName: types.NamespacedName{
									Namespace: TestNamespaceName,
									Name:      teamName,
								},
							},
						)
						Expect(err).NotTo(HaveOccurred())
						Expect(result).To(Equal(reconcile.Result{}))

						verifyTeamSynchronizationStatus(
							ctx,
							k8sClient,
							TestNamespaceName,
							teamName,
							dash0common.Dash0ApiResourceSynchronizationStatusSuccessful,
							testStartedAt,
							teamId,
							fmt.Sprintf(teamOriginPattern, clusterId),
							ApiEndpointStandardizedTest,
							"",
						)
						Expect(gock.IsDone()).To(BeTrue())
					},
				)

				It(
					"deletes a team", func() {
						EnsureMonitoringResourceWithoutExportExistsAndIsAvailable(ctx, k8sClient)

						expectTeamDeleteRequest(defaultExpectedPathTeam)
						defer gock.Off()

						teamResource := createTeamResource(TestNamespaceName, teamName)
						Expect(k8sClient.Create(ctx, teamResource)).To(Succeed())
						Expect(k8sClient.Delete(ctx, teamResource)).To(Succeed())

						result, err := teamReconciler.Reconcile(
							ctx, reconcile.Request{
								NamespacedName: types.NamespacedName{
									Namespace: TestNamespaceName,
									Name:      teamName,
								},
							},
						)
						Expect(err).NotTo(HaveOccurred())
						Expect(result).To(Equal(reconcile.Result{}))

						// The resource is gone, so there is no status to inspect.
						Expect(gock.IsDone()).To(BeTrue())
					},
				)

				It(
					"surfaces the server's 400 for unresolvable emails on the resource status",
					func() {
						EnsureMonitoringResourceWithoutExportExistsAndIsAvailable(ctx, k8sClient)

						gock.New(ApiEndpointTest).
							Put(defaultExpectedPathTeam).
							MatchHeader("Authorization", AuthorizationHeaderTest).
							Times(1).
							Reply(400).
							JSON(map[string]any{"message": unresolvableMessage})
						defer gock.Off()

						teamResource := createTeamResource(TestNamespaceName, teamName)
						teamResource.Spec.Members = []string{teamMemberAlice, unresolvableEmail}
						Expect(k8sClient.Create(ctx, teamResource)).To(Succeed())

						result, err := teamReconciler.Reconcile(
							ctx, reconcile.Request{
								NamespacedName: types.NamespacedName{
									Namespace: TestNamespaceName,
									Name:      teamName,
								},
							},
						)
						Expect(err).NotTo(HaveOccurred())
						Expect(result).To(Equal(reconcile.Result{}))

						verifyTeamSynchronizationStatus(
							ctx,
							k8sClient,
							TestNamespaceName,
							teamName,
							dash0common.Dash0ApiResourceSynchronizationStatusFailed,
							testStartedAt,
							"",
							"",
							ApiEndpointStandardizedTest,
							unresolvableMessage,
						)
						Expect(gock.IsDone()).To(BeTrue())
					},
				)
			},
		)

		Describe(
			"mapping team resources to http requests", func() {

				var teamReconciler *TeamReconciler

				BeforeEach(
					func() {
						teamReconciler = &TeamReconciler{}
					},
				)

				It(
					"builds an org-scoped PUT payload that preserves metadata.name and passes members through",
					func() {
						resource := map[string]any{
							"metadata": map[string]any{
								"name":      teamName,
								"namespace": TestNamespaceName,
							},
							"spec": map[string]any{
								"display": map[string]any{
									"name":        teamDisplayName,
									"description": teamDisplayDesc,
									"color": map[string]any{
										"from": teamColorFrom,
										"to":   teamColorTo,
									},
								},
								"members": []any{teamMemberAlice, teamMemberBob},
							},
							"status": map[string]any{
								"synchronizationStatus": "failed",
							},
						}
						apiConfig := ApiConfig{
							Endpoint: ApiEndpointStandardizedTest,
							Dataset:  DatasetCustomTest,
							Token:    AuthorizationTokenTest,
						}
						result := teamReconciler.MapResourceToHttpRequests(
							&preconditionValidationResult{
								k8sName:      teamName,
								k8sNamespace: TestNamespaceName,
								resource:     resource,
								validatedApiConfigs: []ValidatedApiConfigAndToken{
									*NewValidatedApiConfigAndToken(apiConfig.Endpoint, apiConfig.Dataset, apiConfig.Token),
								},
							},
							apiConfig,
							upsertAction,
							logger,
						)

						Expect(result.TotalProcessed()).To(Equal(1))
						Expect(result.SynchronizationErrors).To(BeNil())
						Expect(result.ApiRequests).To(HaveLen(1))

						req := result.ApiRequests[0].Request
						defer func() { _ = req.Body.Close() }()
						body, err := io.ReadAll(req.Body)
						Expect(err).ToNot(HaveOccurred())

						var payload map[string]any
						Expect(json.Unmarshal(body, &payload)).To(Succeed())

						// Envelope fields on the wire.
						Expect(payload["kind"]).To(Equal("Dash0Team"))
						Expect(payload["apiVersion"]).To(Equal("dash0.com/v1alpha1"))

						// Kubernetes bookkeeping is filtered out; metadata.name is preserved.
						metadata := payload["metadata"].(map[string]any)
						Expect(metadata["name"]).To(Equal(teamName))
						Expect(metadata).ToNot(HaveKey("namespace"))
						Expect(payload).ToNot(HaveKey("status"))

						// Display and members are passed through unchanged.
						spec := payload["spec"].(map[string]any)
						display := spec["display"].(map[string]any)
						Expect(display["name"]).To(Equal(teamDisplayName))
						Expect(display["description"]).To(Equal(teamDisplayDesc))
						color := display["color"].(map[string]any)
						Expect(color["from"]).To(Equal(teamColorFrom))
						Expect(color["to"]).To(Equal(teamColorTo))
						members := spec["members"].([]any)
						Expect(members).To(HaveLen(2))
						Expect(members[0]).To(Equal(teamMemberAlice))
						Expect(members[1]).To(Equal(teamMemberBob))

						// Verify URL format (org-level, no dataset query parameter).
						Expect(req.URL.String()).NotTo(ContainSubstring("dataset="))
						Expect(req.URL.Path).To(ContainSubstring("/api/teams/"))
					},
				)
			},
		)
	},
)

func createTeamReconciler(clusterId string) *TeamReconciler {
	return NewTeamReconciler(
		k8sClient,
		types.UID(clusterId),
		teamLeaderElectionAware,
		TestHTTPClient(),
	)
}

func expectTeamPutRequest(clusterId string, expectedPath string) {
	gock.New(ApiEndpointTest).
		Put(expectedPath).
		MatchHeader("Authorization", AuthorizationHeaderTest).
		Times(1).
		Reply(200).
		JSON(teamPutResponse(clusterId, teamOriginPattern))
}

func teamPutResponse(clusterId string, originPattern string) map[string]any {
	return map[string]any{
		"apiVersion": "dash0.com/v1alpha1",
		"kind":       "Dash0Team",
		"metadata": map[string]any{
			"name": teamName,
			"labels": map[string]any{
				"dash0.com/id":     teamId,
				"dash0.com/origin": fmt.Sprintf(originPattern, clusterId),
				"dash0.com/source": "operator",
			},
			"annotations": map[string]any{
				"dash0.com/created-at": "2026-01-15T10:00:00Z",
				"dash0.com/updated-at": "2026-01-15T10:00:00Z",
			},
		},
		"spec": map[string]any{
			"display": map[string]any{
				"name":        teamDisplayName,
				"description": teamDisplayDesc,
				"color": map[string]any{
					"from": teamColorFrom,
					"to":   teamColorTo,
				},
			},
			"members": []any{
				"00000000-0000-0000-0000-0000000000A1",
				"00000000-0000-0000-0000-0000000000A2",
			},
		},
	}
}

func expectTeamDeleteRequest(expectedPath string) {
	gock.New(ApiEndpointTest).
		Delete(expectedPath).
		MatchHeader("Authorization", AuthorizationHeaderTest).
		Times(1).
		Reply(200)
}

func createTeamResource(namespace string, name string) *dash0dashv1alpha1.Dash0Team { //nolint:unparam
	return &dash0dashv1alpha1.Dash0Team{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "dash0.com/v1alpha1",
			Kind:       "Dash0Team",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
		Spec: dash0dashv1alpha1.Dash0TeamSpec{
			Display: dash0dashv1alpha1.Dash0TeamDisplay{
				Name:        teamDisplayName,
				Description: teamDisplayDesc,
				Color: dash0dashv1alpha1.Dash0TeamColor{
					From: teamColorFrom,
					To:   teamColorTo,
				},
			},
			Members: []string{teamMemberAlice, teamMemberBob},
		},
	}
}

func deleteTeamResourceIfItExists(
	ctx context.Context,
	k8sClient client.Client,
	namespace string,
	name string,
) {
	t := &dash0dashv1alpha1.Dash0Team{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
	}
	err := k8sClient.Delete(
		ctx, t, &client.DeleteOptions{
			GracePeriodSeconds: new(int64),
		},
	)
	if err != nil && !apierrors.IsNotFound(err) {
		Expect(err).NotTo(HaveOccurred())
	}
}

func verifyTeamSynchronizationStatus(
	ctx context.Context,
	k8sClient client.Client,
	namespace string,
	name string,
	expectedStatus dash0common.Dash0ApiResourceSynchronizationStatus,
	testStartedAt time.Time,
	expectedId string,
	expectedOrigin string,
	expectedApiEndpoint string, //nolint:unparam
	expectedError string,
) {
	Eventually(
		func(g Gomega) {
			t := &dash0dashv1alpha1.Dash0Team{}
			err := k8sClient.Get(
				ctx, types.NamespacedName{
					Namespace: namespace,
					Name:      name,
				}, t,
			)
			g.Expect(err).NotTo(HaveOccurred())
			g.Expect(t.Status.SynchronizationStatus).To(Equal(expectedStatus))
			g.Expect(t.Status.SynchronizedAt.Time).To(BeTemporally(">=", testStartedAt.Add(-1*time.Second)))
			g.Expect(t.Status.SynchronizedAt.Time).To(BeTemporally("<=", time.Now()))
			// teams have no operator-side validations
			g.Expect(t.Status.ValidationIssues).To(BeNil())

			g.Expect(t.Status.SynchronizationResults).To(HaveLen(1))
			syncResultPerEndpoint := t.Status.SynchronizationResults[0]
			g.Expect(syncResultPerEndpoint).ToNot(BeNil())
			g.Expect(syncResultPerEndpoint.Dash0ApiEndpoint).To(Equal(expectedApiEndpoint))
			g.Expect(syncResultPerEndpoint.Dash0Id).To(Equal(expectedId))
			g.Expect(syncResultPerEndpoint.Dash0Origin).To(Equal(expectedOrigin))
			g.Expect(syncResultPerEndpoint.SynchronizationError).To(ContainSubstring(expectedError))
		},
	).Should(Succeed())
}
