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

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	dash0common "github.com/dash0hq/dash0-operator/api/operator/common"
	dash0v1beta1 "github.com/dash0hq/dash0-operator/api/operator/v1beta1"
	"github.com/dash0hq/dash0-operator/internal/util"
	"github.com/dash0hq/dash0-operator/internal/util/logd"

	"github.com/h2non/gock"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	. "github.com/dash0hq/dash0-operator/test/util"
)

const (
	notificationChannelName            = "test-notification-channel"
	extraNamespaceNotificationChannels = "extra-namespace-nc"
	notificationChannelName2           = "test-notification-channel-2"
	notificationChannelApiBasePath     = "/api/notification-channels/"

	notificationChannelId                 = "nc-id"
	notificationChannelOriginPattern      = "dash0-operator_%s_test-namespace_test-notification-channel"
	notificationChannelOriginPatternExtra = "dash0-operator_%s_extra-namespace-nc_test-notification-channel-2"
	testK8sName                           = "test-nc"
	testDisplayName                       = "Test Channel"
)

var (
	defaultExpectedPathNotificationChannel = fmt.Sprintf(
		"%s.*%s",
		notificationChannelApiBasePath,
		"dash0-operator_.*_test-namespace_test-notification-channel",
	)
	defaultExpectedPathNotificationChannel2 = fmt.Sprintf(
		"%s.*%s",
		notificationChannelApiBasePath,
		"dash0-operator_.*_extra-namespace-nc_test-notification-channel-2",
	)
	notificationChannelLeaderElectionAware = NewLeaderElectionAwareMock(true)
)

var _ = Describe(
	"The NotificationChannel controller", Ordered, func() {
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
				clusterId = string(util.ReadPseudoClusterUid(ctx, k8sClient, logger))
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
				VerifyNoUnmatchedGockRequests()

				DeleteMonitoringResource(ctx, k8sClient)
				for _, name := range extraMonitoringResourceNames {
					DeleteMonitoringResourceByName(ctx, k8sClient, name, true)
				}
				extraMonitoringResourceNames = make([]types.NamespacedName, 0)
			},
		)

		Describe(
			"the notification channel reconciler", func() {
				var ncReconciler *NotificationChannelReconciler

				BeforeEach(
					func() {
						ncReconciler = createNotificationChannelReconciler(clusterId)

						// Set default API configs directly (not via SetDefaultApiConfigs) to avoid
						// triggering maybeDoInitialSynchronizationOfAllResources, which would set
						// initialSyncHasHappend to true and prevent the DescribeTable tests from
						// verifying initial sync behavior.
						ncReconciler.defaultApiConfigs.Set(
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
						deleteNotificationChannelResourceIfItExists(ctx, k8sClient, TestNamespaceName, notificationChannelName)
						deleteNotificationChannelResourceIfItExists(ctx, k8sClient, extraNamespaceNotificationChannels, notificationChannelName2)
					},
				)

				It(
					"ignores notification channel resource changes if no Dash0 monitoring resource exists in the namespace",
					func() {
						expectNotificationChannelPutRequest(clusterId, defaultExpectedPathNotificationChannel)
						defer gock.Off()

						ncResource := createNotificationChannelResource(TestNamespaceName, notificationChannelName)
						Expect(k8sClient.Create(ctx, ncResource)).To(Succeed())

						Expect(gock.IsPending()).To(BeTrue())
						verifyNotificationChannelHasNoSynchronizationStatus(
							ctx,
							k8sClient,
							TestNamespaceName,
							notificationChannelName,
						)
					},
				)

				It(
					"creates a notification channel", func() {
						EnsureMonitoringResourceWithoutExportExistsAndIsAvailable(ctx, k8sClient)

						expectNotificationChannelPutRequest(clusterId, defaultExpectedPathNotificationChannel)
						defer gock.Off()

						ncResource := createNotificationChannelResource(TestNamespaceName, notificationChannelName)
						Expect(k8sClient.Create(ctx, ncResource)).To(Succeed())

						result, err := ncReconciler.Reconcile(
							ctx, reconcile.Request{
								NamespacedName: types.NamespacedName{
									Namespace: TestNamespaceName,
									Name:      notificationChannelName,
								},
							},
						)
						Expect(err).NotTo(HaveOccurred())
						Expect(result).To(Equal(reconcile.Result{}))

						verifyNotificationChannelSynchronizationStatus(
							ctx,
							k8sClient,
							TestNamespaceName,
							notificationChannelName,
							dash0common.Dash0ApiResourceSynchronizationStatusSuccessful,
							testStartedAt,
							notificationChannelId,
							fmt.Sprintf(notificationChannelOriginPattern, clusterId),
							ApiEndpointStandardizedTest,
							"",
						)
						Expect(gock.IsDone()).To(BeTrue())
					},
				)

				It(
					"updates a notification channel", func() {
						EnsureMonitoringResourceWithoutExportExistsAndIsAvailable(ctx, k8sClient)

						expectNotificationChannelPutRequest(clusterId, defaultExpectedPathNotificationChannel)
						defer gock.Off()

						ncResource := createNotificationChannelResource(TestNamespaceName, notificationChannelName)
						Expect(k8sClient.Create(ctx, ncResource)).To(Succeed())

						// Modify the resource
						ncResource.Spec.Display.Name = "Updated Slack Alerts"
						ncResource.Spec.SlackConfig.WebhookURL = "https://hooks.slack.com/services/T00/B00/yyyy"
						ncResource.Spec.Frequency = "5m"
						Expect(k8sClient.Update(ctx, ncResource)).To(Succeed())

						result, err := ncReconciler.Reconcile(
							ctx, reconcile.Request{
								NamespacedName: types.NamespacedName{
									Namespace: TestNamespaceName,
									Name:      notificationChannelName,
								},
							},
						)
						Expect(err).NotTo(HaveOccurred())
						Expect(result).To(Equal(reconcile.Result{}))

						verifyNotificationChannelSynchronizationStatus(
							ctx,
							k8sClient,
							TestNamespaceName,
							notificationChannelName,
							dash0common.Dash0ApiResourceSynchronizationStatusSuccessful,
							testStartedAt,
							notificationChannelId,
							fmt.Sprintf(notificationChannelOriginPattern, clusterId),
							ApiEndpointStandardizedTest,
							"",
						)
						Expect(gock.IsDone()).To(BeTrue())
					},
				)

				It(
					"deletes a notification channel", func() {
						EnsureMonitoringResourceWithoutExportExistsAndIsAvailable(ctx, k8sClient)

						expectNotificationChannelDeleteRequest(defaultExpectedPathNotificationChannel)
						defer gock.Off()

						ncResource := createNotificationChannelResource(TestNamespaceName, notificationChannelName)
						Expect(k8sClient.Create(ctx, ncResource)).To(Succeed())
						Expect(k8sClient.Delete(ctx, ncResource)).To(Succeed())

						result, err := ncReconciler.Reconcile(
							ctx, reconcile.Request{
								NamespacedName: types.NamespacedName{
									Namespace: TestNamespaceName,
									Name:      notificationChannelName,
								},
							},
						)
						Expect(err).NotTo(HaveOccurred())
						Expect(result).To(Equal(reconcile.Result{}))

						// We do not call verifyNotificationChannelSynchronizationStatus in this test case since the entire
						// resource is deleted, hence there is nothing to write the status to.
						Expect(gock.IsDone()).To(BeTrue())
					},
				)

				It(
					"deletes a notification channel if labelled with dash0.com/enable=false", func() {
						EnsureMonitoringResourceWithoutExportExistsAndIsAvailable(ctx, k8sClient)

						expectNotificationChannelDeleteRequestWithHttpStatus(defaultExpectedPathNotificationChannel, http.StatusNotFound)
						defer gock.Off()

						ncResource := createNotificationChannelResourceWithEnableLabel(TestNamespaceName, notificationChannelName, "false")
						Expect(k8sClient.Create(ctx, ncResource)).To(Succeed())

						result, err := ncReconciler.Reconcile(
							ctx, reconcile.Request{
								NamespacedName: types.NamespacedName{
									Namespace: TestNamespaceName,
									Name:      notificationChannelName,
								},
							},
						)
						Expect(err).NotTo(HaveOccurred())
						Expect(result).To(Equal(reconcile.Result{}))

						verifyNotificationChannelSynchronizationStatus(
							ctx,
							k8sClient,
							TestNamespaceName,
							notificationChannelName,
							dash0common.Dash0ApiResourceSynchronizationStatusSuccessful,
							testStartedAt,
							"", // when deleting an object, we do not get an HTTP response body with an ID
							fmt.Sprintf(notificationChannelOriginPattern, clusterId),
							ApiEndpointStandardizedTest,
							"",
						)
						Expect(gock.IsDone()).To(BeTrue())
					},
				)

				It(
					"reports http errors when synchronizing a notification channel", func() {
						EnsureMonitoringResourceWithoutExportExistsAndIsAvailable(ctx, k8sClient)

						gock.New(ApiEndpointTest).
							Put(defaultExpectedPathNotificationChannel).
							Times(3).
							Reply(503).
							JSON(map[string]string{})
						defer gock.Off()

						ncResource := createNotificationChannelResource(TestNamespaceName, notificationChannelName)
						Expect(k8sClient.Create(ctx, ncResource)).To(Succeed())

						result, err := ncReconciler.Reconcile(
							ctx, reconcile.Request{
								NamespacedName: types.NamespacedName{
									Namespace: TestNamespaceName,
									Name:      notificationChannelName,
								},
							},
						)
						Expect(err).NotTo(HaveOccurred())
						Expect(result).To(Equal(reconcile.Result{}))

						verifyNotificationChannelSynchronizationStatus(
							ctx,
							k8sClient,
							TestNamespaceName,
							notificationChannelName,
							dash0common.Dash0ApiResourceSynchronizationStatusFailed,
							testStartedAt,
							"",
							"",
							ApiEndpointStandardizedTest,
							"unexpected status code 503",
						)
						Expect(gock.IsDone()).To(BeTrue())
					},
				)

				type maybeDoInitialSynchronizationOfAllResourcesTest struct {
					disableSync func()
					enabledSync func()
				}

				DescribeTable(
					"synchronizes all existing notification channel resources when the auth token or api endpoint become available",
					func(testConfig maybeDoInitialSynchronizationOfAllResourcesTest) {
						EnsureMonitoringResourceWithoutExportExistsAndIsAvailable(ctx, k8sClient)

						// Disable synchronization by removing the auth token or api endpoint.
						testConfig.disableSync()

						EnsureNamespaceExists(ctx, k8sClient, extraNamespaceNotificationChannels)
						secondMonitoringResource := EnsureMonitoringResourceWithSpecExistsInNamespaceAndIsAvailable(
							ctx,
							k8sClient,
							MonitoringResourceDefaultSpecWithoutExport,
							types.NamespacedName{Namespace: extraNamespaceNotificationChannels, Name: MonitoringResourceName},
						)
						extraMonitoringResourceNames = append(
							extraMonitoringResourceNames, types.NamespacedName{
								Namespace: secondMonitoringResource.Namespace,
								Name:      secondMonitoringResource.Name,
							},
						)

						expectNotificationChannelPutRequest(clusterId, defaultExpectedPathNotificationChannel)
						expectNotificationChannelPutRequest(clusterId, defaultExpectedPathNotificationChannel2)
						defer gock.Off()

						ncResource1 := createNotificationChannelResource(TestNamespaceName, notificationChannelName)
						Expect(k8sClient.Create(ctx, ncResource1)).To(Succeed())
						ncResource2 := createNotificationChannelResource(extraNamespaceNotificationChannels, notificationChannelName2)
						Expect(k8sClient.Create(ctx, ncResource2)).To(Succeed())

						// verify that the notification channels have not been synchronized yet
						Expect(gock.IsPending()).To(BeTrue())
						verifyNotificationChannelHasNoSynchronizationStatus(ctx, k8sClient, TestNamespaceName, notificationChannelName)
						verifyNotificationChannelHasNoSynchronizationStatus(ctx, k8sClient, extraNamespaceNotificationChannels, notificationChannelName2)

						// Now provide the auth token or API endpoint, which was unset before.
						testConfig.enabledSync()

						// Verify both resources have been synchronized
						verifyNotificationChannelSynchronizationStatus(
							ctx,
							k8sClient,
							TestNamespaceName,
							notificationChannelName,
							dash0common.Dash0ApiResourceSynchronizationStatusSuccessful,
							testStartedAt,
							notificationChannelId,
							fmt.Sprintf(notificationChannelOriginPattern, clusterId),
							ApiEndpointStandardizedTest,
							"",
						)
						verifyNotificationChannelSynchronizationStatus(
							ctx,
							k8sClient,
							extraNamespaceNotificationChannels,
							notificationChannelName2,
							dash0common.Dash0ApiResourceSynchronizationStatusSuccessful,
							testStartedAt,
							notificationChannelId,
							fmt.Sprintf(notificationChannelOriginPatternExtra, clusterId),
							ApiEndpointStandardizedTest,
							"",
						)
						Expect(gock.IsDone()).To(BeTrue())
					},
					Entry(
						"when the auth token becomes available", maybeDoInitialSynchronizationOfAllResourcesTest{
							disableSync: func() {
								ncReconciler.RemoveDefaultApiConfigs(ctx, logger)
							},
							enabledSync: func() {
								ncReconciler.SetDefaultApiConfigs(
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
								notificationChannelLeaderElectionAware.SetLeader(false)
							},
							enabledSync: func() {
								notificationChannelLeaderElectionAware.SetLeader(true)
								ncReconciler.NotifyOperatorManagerJustBecameLeader(ctx, logger)
							},
						},
					),
				)
			},
		)

		Describe(
			"mapping notification channel resources to http requests", func() {

				var ncReconciler *NotificationChannelReconciler

				BeforeEach(
					func() {
						ncReconciler = &NotificationChannelReconciler{}
					},
				)

				type ncMappingTestConfig struct {
					channelType     string
					configFieldName string
					configMap       map[string]any
					expectedKeys    []string
				}

				DescribeTable(
					"maps notification channel types to API payloads",
					func(tc ncMappingTestConfig) {
						resource := map[string]any{
							"metadata": map[string]any{
								"name":      testK8sName,
								"namespace": TestNamespaceName,
							},
							"spec": map[string]any{
								"display": map[string]any{
									"name": testDisplayName,
								},
								"type":             tc.channelType,
								tc.configFieldName: tc.configMap,
							},
						}
						apiConfig := ApiConfig{
							Endpoint: ApiEndpointStandardizedTest,
							Dataset:  DatasetCustomTest,
							Token:    AuthorizationTokenTest,
						}
						result := ncReconciler.MapResourceToHttpRequests(
							&preconditionValidationResult{
								k8sName:      testK8sName,
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

						// Verify display name was moved to metadata.name and removed from spec.
						metadata := payload["metadata"].(map[string]any)
						Expect(metadata["name"]).To(Equal(testDisplayName))
						spec := payload["spec"].(map[string]any)
						Expect(spec).ToNot(HaveKey("display"))

						// Verify the type-specific config field was assembled into spec.config.
						Expect(spec).To(HaveKey("config"))
						Expect(spec).ToNot(HaveKey(tc.configFieldName))
						config := spec["config"].(map[string]any)
						for _, key := range tc.expectedKeys {
							Expect(config).To(HaveKey(key))
						}

						// Verify URL format (org-level, no dataset query parameter).
						Expect(req.URL.String()).NotTo(ContainSubstring("dataset="))
						Expect(req.URL.Path).To(ContainSubstring("/api/notification-channels/"))
					},

					Entry("slack", ncMappingTestConfig{
						channelType:     "slack",
						configFieldName: "slackConfig",
						configMap: map[string]any{
							"webhookURL": "https://hooks.slack.com/services/T00/B00/xxxx",
							"channel":    "#alerts",
						},
						expectedKeys: []string{"webhookURL", "channel"},
					}),

					Entry("slack_bot", ncMappingTestConfig{
						channelType:     "slack_bot",
						configFieldName: "slackBotConfig",
						configMap: map[string]any{
							"teamId":  "T12345",
							"channel": "#alerts",
						},
						expectedKeys: []string{"teamId", "channel"},
					}),

					Entry("email_v2", ncMappingTestConfig{
						channelType:     "email_v2",
						configFieldName: "emailV2Config",
						configMap: map[string]any{
							"recipients": []any{"alice@example.com", "bob@example.com"},
						},
						expectedKeys: []string{"recipients"},
					}),

					Entry("webhook", ncMappingTestConfig{
						channelType:     "webhook",
						configFieldName: "webhookConfig",
						configMap: map[string]any{
							"url":     "https://example.com/webhook",
							"headers": map[string]any{"X-Custom": "value"},
						},
						expectedKeys: []string{"url", "headers"},
					}),

					Entry("incidentio", ncMappingTestConfig{
						channelType:     "incidentio",
						configFieldName: "incidentioConfig",
						configMap: map[string]any{
							"url":     "https://api.incident.io/v2/alert_events/http/source",
							"headers": "Bearer tok_123",
						},
						expectedKeys: []string{"url", "headers"},
					}),

					Entry("opsgenie", ncMappingTestConfig{
						channelType:     "opsgenie",
						configFieldName: "opsgenieConfig",
						configMap: map[string]any{
							"instance": "eu",
							"apiKey":   "key-123",
						},
						expectedKeys: []string{"instance", "apiKey"},
					}),

					Entry("pagerduty", ncMappingTestConfig{
						channelType:     "pagerduty",
						configFieldName: "pagerdutyConfig",
						configMap: map[string]any{
							"key": "integration-key-123",
							"url": "https://events.pagerduty.com/v2/enqueue",
						},
						expectedKeys: []string{"key", "url"},
					}),

					Entry("teams_webhook", ncMappingTestConfig{
						channelType:     "teams_webhook",
						configFieldName: "teamsWebhookConfig",
						configMap: map[string]any{
							"url": "https://outlook.office.com/webhook/xxx",
						},
						expectedKeys: []string{"url"},
					}),

					Entry("discord_webhook", ncMappingTestConfig{
						channelType:     "discord_webhook",
						configFieldName: "discordWebhookConfig",
						configMap: map[string]any{
							"url": "https://discord.com/api/webhooks/xxx/yyy",
						},
						expectedKeys: []string{"url"},
					}),

					Entry("google_chat_webhook", ncMappingTestConfig{
						channelType:     "google_chat_webhook",
						configFieldName: "googleChatWebhookConfig",
						configMap: map[string]any{
							"url": "https://chat.googleapis.com/v1/spaces/xxx/messages?key=yyy",
						},
						expectedKeys: []string{"url"},
					}),

					Entry("ilert", ncMappingTestConfig{
						channelType:     "ilert",
						configFieldName: "ilertConfig",
						configMap: map[string]any{
							"url": "https://api.ilert.com/api/events/xxx",
						},
						expectedKeys: []string{"url"},
					}),

					Entry("all_quiet", ncMappingTestConfig{
						channelType:     "all_quiet",
						configFieldName: "allQuietConfig",
						configMap: map[string]any{
							"url": "https://allquiet.app/api/webhook/xxx",
						},
						expectedKeys: []string{"url"},
					}),
				)
			},
		)
	},
)

func createNotificationChannelReconciler(clusterId string) *NotificationChannelReconciler {
	ncReconciler := NewNotificationChannelReconciler(
		k8sClient,
		types.UID(clusterId),
		notificationChannelLeaderElectionAware,
		TestHTTPClient(),
	)
	return ncReconciler
}

func expectNotificationChannelPutRequest(clusterId string, expectedPath string) {
	gock.New(ApiEndpointTest).
		Put(expectedPath).
		MatchHeader("Authorization", AuthorizationHeaderTest).
		Times(1).
		Reply(200).
		JSON(notificationChannelPutResponse(clusterId, notificationChannelOriginPattern))
}

func notificationChannelPutResponse(clusterId string, originPattern string) map[string]any {
	return map[string]any{
		"metadata": map[string]any{
			"labels": map[string]any{
				"dash0.com/id":     notificationChannelId,
				"dash0.com/origin": fmt.Sprintf(originPattern, clusterId),
			},
		},
	}
}

func expectNotificationChannelDeleteRequest(expectedPath string) {
	expectNotificationChannelDeleteRequestWithHttpStatus(expectedPath, http.StatusOK)
}

func expectNotificationChannelDeleteRequestWithHttpStatus(expectedPath string, status int) {
	gock.New(ApiEndpointTest).
		Delete(expectedPath).
		MatchHeader("Authorization", AuthorizationHeaderTest).
		Times(1).
		Reply(status)
}

func createNotificationChannelResource(namespace string, name string) *dash0v1beta1.Dash0NotificationChannel {
	return createNotificationChannelResourceWithEnableLabel(namespace, name, "")
}

func createNotificationChannelResourceWithEnableLabel(
	namespace string,
	name string,
	dash0EnableLabelValue string,
) *dash0v1beta1.Dash0NotificationChannel {
	objectMeta := metav1.ObjectMeta{
		Name:      name,
		Namespace: namespace,
	}
	if dash0EnableLabelValue != "" {
		objectMeta.Labels = map[string]string{
			"dash0.com/enable": dash0EnableLabelValue,
		}
	}
	return &dash0v1beta1.Dash0NotificationChannel{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "operator.dash0.com/v1beta1",
			Kind:       "Dash0NotificationChannel",
		},
		ObjectMeta: objectMeta,
		Spec: dash0v1beta1.Dash0NotificationChannelSpec{
			Display: dash0v1beta1.Dash0NotificationChannelDisplay{
				Name: "Slack Alerts",
			},
			Type: "slack",
			SlackConfig: &dash0v1beta1.SlackConfig{
				WebhookURL: "https://hooks.slack.com/services/T00/B00/xxxx",
				Channel:    "#alerts",
			},
			Frequency: "10m",
		},
	}
}

func deleteNotificationChannelResourceIfItExists(
	ctx context.Context,
	k8sClient client.Client,
	namespace string,
	name string,
) {
	nc := &dash0v1beta1.Dash0NotificationChannel{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
	}
	err := k8sClient.Delete(
		ctx, nc, &client.DeleteOptions{
			GracePeriodSeconds: new(int64),
		},
	)
	if err != nil && !apierrors.IsNotFound(err) {
		Expect(err).NotTo(HaveOccurred())
	}
}

func verifyNotificationChannelSynchronizationStatus(
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
			nc := &dash0v1beta1.Dash0NotificationChannel{}
			err := k8sClient.Get(
				ctx, types.NamespacedName{
					Namespace: namespace,
					Name:      name,
				}, nc,
			)
			g.Expect(err).NotTo(HaveOccurred())
			g.Expect(nc.Status.SynchronizationStatus).To(Equal(expectedStatus))
			g.Expect(nc.Status.SynchronizedAt.Time).To(BeTemporally(">=", testStartedAt.Add(-1*time.Second)))
			g.Expect(nc.Status.SynchronizedAt.Time).To(BeTemporally("<=", time.Now()))
			// notification channels have no operator-side validations
			g.Expect(nc.Status.ValidationIssues).To(BeNil())

			g.Expect(nc.Status.SynchronizationResults).To(HaveLen(1))
			syncResultPerEndpoint := nc.Status.SynchronizationResults[0]
			g.Expect(syncResultPerEndpoint).ToNot(BeNil())
			g.Expect(syncResultPerEndpoint.Dash0ApiEndpoint).To(Equal(expectedApiEndpoint))
			g.Expect(syncResultPerEndpoint.Dash0Id).To(Equal(expectedId))
			g.Expect(syncResultPerEndpoint.Dash0Origin).To(Equal(expectedOrigin))
			g.Expect(syncResultPerEndpoint.SynchronizationError).To(ContainSubstring(expectedError))
		},
	).Should(Succeed())
}

func verifyNotificationChannelHasNoSynchronizationStatus(
	ctx context.Context,
	k8sClient client.Client,
	namespace string,
	name string,
) {
	Eventually(
		func(g Gomega) {
			nc := &dash0v1beta1.Dash0NotificationChannel{}
			err := k8sClient.Get(
				ctx, types.NamespacedName{
					Namespace: namespace,
					Name:      name,
				}, nc,
			)
			g.Expect(err).NotTo(HaveOccurred())
			g.Expect(string(nc.Status.SynchronizationStatus)).To(Equal(""))
			g.Expect(nc.Status.ValidationIssues).To(BeNil())
			g.Expect(nc.Status.SynchronizationResults).To(HaveLen(0))
		},
	).Should(Succeed())
}
