// SPDX-FileCopyrightText: Copyright 2026 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package startup

import (
	"context"
	"encoding/json"
	"fmt"
	"sync/atomic"
	"time"

	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/wait"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/dash0hq/dash0-operator/api/operator/v1alpha1"
	"github.com/dash0hq/dash0-operator/internal/util"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	. "github.com/dash0hq/dash0-operator/test/util"
)

// unreliableClient wraps a client and lets all create and update calls fail as long as failWrites is set.
type unreliableClient struct {
	client.Client
	failWrites    atomic.Bool
	writeAttempts atomic.Int32
	listCalls     atomic.Int32
}

func (c *unreliableClient) List(ctx context.Context, list client.ObjectList, opts ...client.ListOption) error {
	c.listCalls.Add(1)
	return c.Client.List(ctx, list, opts...)
}

func (c *unreliableClient) Create(ctx context.Context, obj client.Object, opts ...client.CreateOption) error {
	c.writeAttempts.Add(1)
	if c.failWrites.Load() {
		return fmt.Errorf("simulated error: failed calling webhook")
	}
	return c.Client.Create(ctx, obj, opts...)
}

func (c *unreliableClient) Update(ctx context.Context, obj client.Object, opts ...client.UpdateOption) error {
	c.writeAttempts.Add(1)
	if c.failWrites.Load() {
		return fmt.Errorf("simulated error: failed calling webhook")
	}
	return c.Client.Update(ctx, obj, opts...)
}

var _ = Describe(
	"Retry creating the operator configuration resource at startup", Ordered, func() {

		const (
			testTimeout     = 5 * time.Second
			pollingInterval = 50 * time.Millisecond
			// Two attempts per round, so three rounds have been started once the fifth attempt has been made.
			attemptsPerRound            = 2
			attemptsSpanningThreeRounds = 5
		)

		var (
			testCtx            context.Context
			cancelTestCtx      context.CancelFunc
			readyCheckExecuter *ReadyCheckExecuter
			unreliable         *unreliableClient
			createdObjects     []client.Object

			logger, capturingLogSink = NewCapturingLogger()
		)

		newHandler := func(
			operatorConfigurationValues OperatorConfigurationValues,
			extraConfig util.ExtraConfig,
		) *AutoOperatorConfigurationResourceHandler {
			handler := NewAutoOperatorConfigurationResourceHandler(
				unreliable,
				readyCheckExecuter,
				operatorConfigurationValues,
				extraConfig,
			)
			handler.createOrUpdateBackoff = wait.Backoff{
				Duration: 1 * time.Millisecond,
				Factor:   1,
				Steps:    attemptsPerRound,
			}
			handler.retryDelays = retryDelays{
				initial: 10 * time.Millisecond,
				factor:  1,
				max:     10 * time.Millisecond,
			}
			handler.NotifyOperatorManagerJustBecameLeader(testCtx, logger)
			return handler
		}

		loadAutoResource := func(g Gomega) *v1alpha1.Dash0OperatorConfiguration {
			operatorConfiguration := v1alpha1.Dash0OperatorConfiguration{}
			g.Expect(
				k8sClient.Get(
					testCtx,
					types.NamespacedName{Name: util.OperatorConfigurationAutoResourceName},
					&operatorConfiguration,
				),
			).To(Succeed())
			return &operatorConfiguration
		}

		verifyThatAutoResourceDoesNotExist := func(g Gomega) {
			g.Expect(
				k8sClient.Get(
					testCtx,
					types.NamespacedName{Name: util.OperatorConfigurationAutoResourceName},
					&v1alpha1.Dash0OperatorConfiguration{},
				),
			).To(MatchError(ContainSubstring("not found")))
		}

		BeforeAll(
			func() {
				EnsureOperatorNamespaceExists(context.Background(), k8sClient)
			},
		)

		BeforeEach(
			func() {
				testCtx, cancelTestCtx = context.WithCancel(context.Background())
				readyCheckExecuter = NewReadyCheckExecuter(
					k8sClient,
					OperatorNamespace,
					OperatorWebhookServiceName,
				)
				readyCheckExecuter.bypassWebhookCheck = true
				unreliable = &unreliableClient{Client: k8sClient}
				createdObjects = make([]client.Object, 0)
				capturingLogSink.Reset()
			},
		)

		AfterEach(
			func() {
				// Stop handlers that are still retrying, otherwise they might recreate the resource after the cleanup.
				cancelTestCtx()
				cleanupCtx := context.Background()
				createdObjects = DeleteAllCreatedObjects(cleanupCtx, k8sClient, createdObjects)
				DeleteAllOperatorConfigurationResources(cleanupCtx, k8sClient)
			},
		)

		It(
			"should keep retrying to create the resource until the error has gone away", func() {
				unreliable.failWrites.Store(true)
				handler := newHandler(operatorConfigurationValuesWithToken, util.ExtraConfig{})
				_, err := handler.CreateOrUpdateOperatorConfigurationResource(testCtx, logger)
				Expect(err).ToNot(HaveOccurred())

				Eventually(
					func(g Gomega) {
						g.Expect(unreliable.writeAttempts.Load()).To(BeNumerically(">=", attemptsSpanningThreeRounds))
						capturingLogSink.HasLogMessage(
							g,
							"failed to create or update the Dash0 operator configuration resource, trying again in 10ms",
						)
					}, testTimeout, pollingInterval,
				).Should(Succeed())
				verifyThatAutoResourceDoesNotExist(Default)
				Expect(handler.WaitForFirstCreateOrUpdate(testCtx, 100*time.Millisecond)).To(BeFalse())

				unreliable.failWrites.Store(false)

				Expect(handler.WaitForFirstCreateOrUpdate(testCtx, testTimeout)).To(BeTrue())
				loadAutoResource(Default)
			},
		)

		It(
			"should keep retrying to update an existing resource until the error has gone away", func() {
				CreateOperatorConfigurationResourceWithName(
					testCtx,
					k8sClient,
					util.OperatorConfigurationAutoResourceName,
					v1alpha1.Dash0OperatorConfigurationSpec{},
				)

				unreliable.failWrites.Store(true)
				handler := newHandler(
					OperatorConfigurationValues{
						Endpoint:    EndpointDash0Test,
						Token:       AuthorizationTokenTest,
						ClusterName: "updated-cluster-name",
					},
					util.ExtraConfig{},
				)
				_, err := handler.CreateOrUpdateOperatorConfigurationResource(testCtx, logger)
				Expect(err).ToNot(HaveOccurred())

				Eventually(
					func(g Gomega) {
						g.Expect(unreliable.writeAttempts.Load()).To(BeNumerically(">=", attemptsSpanningThreeRounds))
					}, testTimeout, pollingInterval,
				).Should(Succeed())
				Expect(loadAutoResource(Default).Spec.ClusterName).To(BeEmpty())

				unreliable.failWrites.Store(false)

				Eventually(
					func(g Gomega) {
						g.Expect(loadAutoResource(g).Spec.ClusterName).To(Equal("updated-cluster-name"))
					}, testTimeout, pollingInterval,
				).Should(Succeed())
			},
		)

		It(
			"should keep waiting for the webhook service endpoint after the ready check has timed out", func() {
				readyCheckExecuter.bypassWebhookCheck = false
				readyCheckExecuter.retryBackoff = wait.Backoff{
					Duration: 10 * time.Millisecond,
					Factor:   1,
					Steps:    2,
				}
				handler := newHandler(operatorConfigurationValuesWithToken, util.ExtraConfig{})
				_, err := handler.CreateOrUpdateOperatorConfigurationResource(testCtx, logger)
				Expect(err).ToNot(HaveOccurred())

				Eventually(
					func(g Gomega) {
						capturingLogSink.HasLogMessage(
							g,
							"failed to create or update the Dash0 operator configuration resource, trying again in 10ms",
						)
					}, testTimeout, pollingInterval,
				).Should(Succeed())
				verifyThatAutoResourceDoesNotExist(Default)

				webhookService := WebhookService()
				Expect(k8sClient.Create(testCtx, &webhookService)).To(Succeed())
				createdObjects = append(createdObjects, &webhookService)
				endpointSlice := WebhookServiceEndpointSlice(true, true)
				Expect(k8sClient.Create(testCtx, &endpointSlice)).To(Succeed())
				createdObjects = append(createdObjects, &endpointSlice)

				Eventually(
					func(g Gomega) {
						loadAutoResource(g)
					}, testTimeout, pollingInterval,
				).Should(Succeed())
			},
		)

		It(
			"should not retry if there is a manually created operator configuration resource", func() {
				CreateOperatorConfigurationResourceWithName(
					testCtx,
					k8sClient,
					"manually-created-resource",
					v1alpha1.Dash0OperatorConfigurationSpec{},
				)

				handler := newHandler(operatorConfigurationValuesWithToken, util.ExtraConfig{})
				_, err := handler.CreateOrUpdateOperatorConfigurationResource(testCtx, logger)
				Expect(err).ToNot(HaveOccurred())

				Eventually(
					func(g Gomega) {
						capturingLogSink.HasLogMessage(
							g,
							"failed to create or update the Dash0 operator configuration resource, this will not be retried",
						)
					}, testTimeout, pollingInterval,
				).Should(Succeed())

				Consistently(
					func(g Gomega) {
						g.Expect(unreliable.listCalls.Load()).To(Equal(int32(1)))
						g.Expect(unreliable.writeAttempts.Load()).To(BeZero())
					}, 300*time.Millisecond, pollingInterval,
				).Should(Succeed())
			},
		)

		It(
			"should stop retrying when the context is cancelled", func() {
				unreliable.failWrites.Store(true)
				handler := newHandler(operatorConfigurationValuesWithToken, util.ExtraConfig{})
				_, err := handler.CreateOrUpdateOperatorConfigurationResource(testCtx, logger)
				Expect(err).ToNot(HaveOccurred())

				Eventually(
					func(g Gomega) {
						g.Expect(unreliable.writeAttempts.Load()).To(BeNumerically(">=", attemptsSpanningThreeRounds))
					}, testTimeout, pollingInterval,
				).Should(Succeed())

				cancelTestCtx()

				Eventually(
					func(g Gomega) {
						capturingLogSink.HasLogMessage(
							g,
							"the attempt to create or update the Dash0 operator configuration resource has been "+
								"cancelled or superseded by a newer attempt",
						)
					}, testTimeout, pollingInterval,
				).Should(Succeed())

				writeAttemptsAfterCancel := unreliable.writeAttempts.Load()
				Consistently(
					func(g Gomega) {
						g.Expect(unreliable.writeAttempts.Load()).To(Equal(writeAttemptsAfterCancel))
					}, 300*time.Millisecond, pollingInterval,
				).Should(Succeed())
			},
		)

		It(
			"should abandon a retrying attempt when the extra config is updated", func() {
				initialFilter := json.RawMessage(`{"traces":{"span":["attributes[\"http.route\"] == \"/ready\""]}}`)
				updatedFilter := json.RawMessage(`{"traces":{"span":["attributes[\"http.route\"] == \"/metrics\""]}}`)

				unreliable.failWrites.Store(true)
				handler := newHandler(
					operatorConfigurationValuesWithTokenAndTelemetryCollection,
					util.ExtraConfig{FilterRaw: &initialFilter},
				)
				_, err := handler.CreateOrUpdateOperatorConfigurationResource(testCtx, logger)
				Expect(err).ToNot(HaveOccurred())
				Eventually(
					func(g Gomega) {
						g.Expect(unreliable.writeAttempts.Load()).To(BeNumerically(">=", attemptsPerRound))
					}, testTimeout, pollingInterval,
				).Should(Succeed())

				handler.UpdateExtraConfig(testCtx, util.ExtraConfig{FilterRaw: &updatedFilter}, logger)
				Eventually(
					func(g Gomega) {
						capturingLogSink.HasLogMessage(
							g,
							"the attempt to create or update the Dash0 operator configuration resource has been "+
								"cancelled or superseded by a newer attempt",
						)
					}, testTimeout, pollingInterval,
				).Should(Succeed())
				Expect(handler.WaitForFirstCreateOrUpdate(testCtx, 100*time.Millisecond)).To(BeFalse())

				unreliable.failWrites.Store(false)

				Expect(handler.WaitForFirstCreateOrUpdate(testCtx, testTimeout)).To(BeTrue())
				verifyUpdatedFilter := func(g Gomega) {
					filter := loadAutoResource(g).Spec.Filter
					g.Expect(filter).ToNot(BeNil())
					g.Expect(filter.Traces).ToNot(BeNil())
					g.Expect(filter.Traces.SpanFilter).To(ConsistOf(`attributes["http.route"] == "/metrics"`))
				}
				Eventually(verifyUpdatedFilter, testTimeout, pollingInterval).Should(Succeed())
				Consistently(verifyUpdatedFilter, 300*time.Millisecond, pollingInterval).Should(Succeed())
			},
		)

		DescribeTable(
			"should increase the delay between rounds up to the maximum",
			func(current time.Duration, expected time.Duration) {
				Expect(defaultRetryDelays.next(current)).To(Equal(expected))
			},
			Entry("after the initial delay", 30*time.Second, 1*time.Minute),
			Entry("after the second delay", 1*time.Minute, 2*time.Minute),
			Entry("when exceeding the maximum", 4*time.Minute, 5*time.Minute),
			Entry("when the maximum has been reached", 5*time.Minute, 5*time.Minute),
		)
	},
)
