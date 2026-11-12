// SPDX-FileCopyrightText: Copyright 2026 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package startup

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"time"

	k8swebhook "sigs.k8s.io/controller-runtime/pkg/webhook"

	"github.com/dash0hq/dash0-operator/internal/util/logd"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	. "github.com/dash0hq/dash0-operator/test/util"
)

type delayedShutdownTestContextKey struct{}

var _ = Describe("delayed cancellation of the manager context", func() {
	const delay = 300 * time.Millisecond

	It("is not cancelled while the parent context is active", func() {
		parent, cancelParent := context.WithCancel(context.Background())
		defer cancelParent()
		logger, capturingLogSink := NewCapturingLogger()
		var terminationSignalReceived atomic.Bool

		ctx := delayCancellation(parent, delay, func() { terminationSignalReceived.Store(true) }, logger)

		Consistently(ctx.Done(), delay).ShouldNot(BeClosed())
		Expect(terminationSignalReceived.Load()).To(BeFalse())
		capturingLogSink.HasNoLogMessages(Default)
	})

	It("is cancelled after the delay once the parent context has been cancelled", func() {
		parent, cancelParent := context.WithCancel(context.Background())
		logger, capturingLogSink := NewCapturingLogger()
		var terminationSignalReceived atomic.Bool

		ctx := delayCancellation(parent, delay, func() { terminationSignalReceived.Store(true) }, logger)
		cancelParent()

		Eventually(terminationSignalReceived.Load, delay/3).Should(BeTrue())
		Consistently(ctx.Done(), delay/3).ShouldNot(BeClosed())
		capturingLogSink.HasLogMessage(
			Default,
			"received a termination signal, the operator manager will shut down in 300ms, to keep serving webhook "+
				"requests until this pod has been removed from the webhook service endpoints",
		)
		Eventually(ctx.Done(), 2*delay).Should(BeClosed())
		Expect(ctx.Err()).To(MatchError(context.Canceled))
	})

	It("carries the values of the parent context", func() {
		parent := context.WithValue(context.Background(), delayedShutdownTestContextKey{}, "value")

		ctx := delayCancellation(parent, delay, func() {}, logd.Discard())

		Expect(ctx.Value(delayedShutdownTestContextKey{})).To(Equal("value"))
	})
})

var _ = Describe("the draining webhook server", func() {
	const path = "/test-webhook"

	var (
		webhookServer *drainingWebhookServer
		httpServer    *httptest.Server
	)

	BeforeEach(func() {
		webhookServer = newDrainingWebhookServer(k8swebhook.NewServer(k8swebhook.Options{}))
		webhookServer.Register(path, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusOK)
		}))
		httpServer = httptest.NewServer(webhookServer.WebhookMux())
	})

	AfterEach(func() {
		httpServer.Close()
	})

	get := func() *http.Response {
		response, err := httpServer.Client().Get(httpServer.URL + path)
		Expect(err).ToNot(HaveOccurred())
		Expect(response.Body.Close()).To(Succeed())
		Expect(response.StatusCode).To(Equal(http.StatusOK))
		return response
	}

	It("keeps connections alive before draining has started", func() {
		response := get()
		Expect(response.Close).To(BeFalse())
	})

	It("asks the client to close the connection once draining has started", func() {
		get()
		webhookServer.startDraining()
		response := get()
		Expect(response.Close).To(BeTrue())
	})
})
