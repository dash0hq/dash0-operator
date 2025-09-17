// SPDX-FileCopyrightText: Copyright 2025 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package startup

import (
	"context"
	"time"

	"k8s.io/apimachinery/pkg/util/wait"
	"sigs.k8s.io/controller-runtime/pkg/client"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	. "github.com/dash0hq/dash0-operator/test/util"
)

var _ = Describe("operator manager pod ready check", Ordered, func() {
	ctx := context.Background()

	const (
		testTimeout     = 1 * time.Second
		pollingInterval = 100 * time.Millisecond
	)

	var (
		readyCheckExecuter   *ReadyCheckExecuter
		retryBackoffForTests = wait.Backoff{
			Duration: 10 * time.Millisecond,
			Factor:   1,
			Steps:    2,
		}

		logger, capturingLogSink = NewCapturingLogger()

		createdObjects []client.Object
	)

	BeforeAll(func() {
		EnsureOperatorNamespaceExists(ctx, k8sClient)

		readyCheckExecuter = NewReadyCheckExecuter(
			k8sClient,
			OperatorNamespace,
			OperatorWebhookServiceName,
		)
		readyCheckExecuter.retryBackoff = retryBackoffForTests
	})

	BeforeEach(func() {
		createdObjects = make([]client.Object, 0)
		capturingLogSink.Reset()
	})

	AfterEach(func() {
		createdObjects = DeleteAllCreatedObjects(ctx, k8sClient, createdObjects)
	})

	Describe("start", func() {
		It("should stop polling and give up at some point if the endpoint does not exist", func() {
			readyCheckExecuter.start(ctx, &logger)

			Eventually(func(g Gomega) {
				capturingLogSink.HasLogMessage(g, "failed to poll the webhook service endpoint, the pod will not be marked as ready")
			}, testTimeout, pollingInterval).Should(Succeed())

			Expect(readyCheckExecuter.webhookEndpointHasPortAssigned.Load()).To(BeFalse())
		})

		It("should stop polling and give up at some point if the endpoint has no port", func() {
			webhookService := WebhookService()
			Expect(k8sClient.Create(ctx, &webhookService)).To(Succeed())
			createdObjects = append(createdObjects, &webhookService)

			endpointSlice := WebhookServiceEndpointSlice(false, false)
			Expect(k8sClient.Create(ctx, &endpointSlice)).To(Succeed())
			createdObjects = append(createdObjects, &endpointSlice)

			readyCheckExecuter.start(ctx, &logger)

			Eventually(func(g Gomega) {
				capturingLogSink.HasLogMessage(g, "failed to poll the webhook service endpoint, the pod will not be marked as ready")
			}, testTimeout, pollingInterval).Should(Succeed())

			Expect(readyCheckExecuter.webhookEndpointHasPortAssigned.Load()).To(BeFalse())
		})

		It("should mark pod as ready when webservice endpoint gets a port assigned", func() {
			webhookService := WebhookService()
			Expect(k8sClient.Create(ctx, &webhookService)).To(Succeed())
			createdObjects = append(createdObjects, &webhookService)

			endpointSlice := WebhookServiceEndpointSlice(true, false)
			Expect(k8sClient.Create(ctx, &endpointSlice)).To(Succeed())
			createdObjects = append(createdObjects, &endpointSlice)

			readyCheckExecuter.start(ctx, &logger)

			Eventually(func(g Gomega) {
				capturingLogSink.HasLogMessage(g, "the webhook service endpoint is ready (it has a port assigned)")
			}, testTimeout, pollingInterval).Should(Succeed())

			Expect(readyCheckExecuter.webhookEndpointHasPortAssigned.Load()).To(BeTrue())
		})
	})

	Describe("waitForWebhookServiceEndpointToBecomeReady", func() {
		It("should stop polling and give up at some point if the endpoint does not exist", func() {
			startTime := time.Now()
			var elapsedTimeNanoseconds int64

			go func() {
				defer GinkgoRecover()
				ready, err := readyCheckExecuter.waitForWebhookServiceEndpointToBecomeReady(ctx, &logger)
				Expect(ready).To(BeFalse())
				Expect(err).To(MatchError(
					"waiting for the webhook service endpoint has timed out (no more retries left): the webhook service endpoint is not ready yet",
				))
				elapsedTimeNanoseconds = time.Since(startTime).Nanoseconds()
			}()

			Eventually(func(g Gomega) {
				g.Expect(elapsedTimeNanoseconds).ToNot(BeZero())
				g.Expect(elapsedTimeNanoseconds).To(
					BeNumerically(
						"<=",
						100_000_000,
					))
			}, testTimeout, pollingInterval).Should(Succeed())
		})

		It("should stop polling and give up at some point if the endpoint is not marked as ready", func() {
			webhookService := WebhookService()
			Expect(k8sClient.Create(ctx, &webhookService)).To(Succeed())
			createdObjects = append(createdObjects, &webhookService)

			endpointSlice := WebhookServiceEndpointSlice(true, false)
			Expect(k8sClient.Create(ctx, &endpointSlice)).To(Succeed())
			createdObjects = append(createdObjects, &endpointSlice)

			startTime := time.Now()
			var elapsedTimeNanoseconds int64

			go func() {
				defer GinkgoRecover()
				ready, err := readyCheckExecuter.waitForWebhookServiceEndpointToBecomeReady(ctx, &logger)
				Expect(ready).To(BeFalse())
				Expect(err).To(MatchError(
					"waiting for the webhook service endpoint has timed out (no more retries left): the webhook service endpoint is not ready yet",
				))
				elapsedTimeNanoseconds = time.Since(startTime).Nanoseconds()
			}()

			Eventually(func(g Gomega) {
				g.Expect(elapsedTimeNanoseconds).ToNot(BeZero())
				g.Expect(elapsedTimeNanoseconds).To(
					BeNumerically(
						"<=",
						100_000_000,
					))
			}, testTimeout, pollingInterval).Should(Succeed())
		})

		It("should succeed if the endpoint is marked as ready", func() {
			webhookService := WebhookService()
			Expect(k8sClient.Create(ctx, &webhookService)).To(Succeed())
			createdObjects = append(createdObjects, &webhookService)

			endpointSlice := WebhookServiceEndpointSlice(true, true)
			Expect(k8sClient.Create(ctx, &endpointSlice)).To(Succeed())
			createdObjects = append(createdObjects, &endpointSlice)

			ready, err := readyCheckExecuter.waitForWebhookServiceEndpointToBecomeReady(ctx, &logger)
			Expect(ready).To(BeTrue())
			Expect(err).To(Succeed())
		})
	})
})
