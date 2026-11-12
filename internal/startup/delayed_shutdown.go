// SPDX-FileCopyrightText: Copyright 2026 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package startup

import (
	"context"
	"fmt"
	"net/http"
	"sync/atomic"
	"time"

	k8swebhook "sigs.k8s.io/controller-runtime/pkg/webhook"

	"github.com/dash0hq/dash0-operator/internal/util/logd"
)

// shutdownDelay needs to fit into the terminationGracePeriodSeconds of the operator manager deployment, together with
// the time the manager needs to shut down after the delay.
const shutdownDelay = 5 * time.Second

// delayCancellation returns a context that carries the values of the parent context and that is cancelled the given
// delay after the parent context has been cancelled. onTerminationSignal is called as soon as the parent context is
// cancelled.
//
// When the operator manager pod is terminated, it stays an endpoint of the webhook service until kube-proxy has
// processed the endpoint removal. If the webhook server closed its listener right away, the API server would get
// "connection refused" for webhook calls routed to this pod during that time. Delaying the cancellation of the context
// that is handed to the manager keeps the webhook server listening while the endpoint removal propagates. A second
// termination signal still exits the process immediately, see ctrl.SetupSignalHandler.
func delayCancellation(
	parent context.Context,
	delay time.Duration,
	onTerminationSignal func(),
	logger logd.Logger,
) context.Context {
	ctx, cancel := context.WithCancel(context.WithoutCancel(parent))
	context.AfterFunc(parent, func() {
		logger.Info(
			fmt.Sprintf(
				"received a termination signal, the operator manager will shut down in %s, to keep serving webhook "+
					"requests until this pod has been removed from the webhook service endpoints",
				delay,
			),
		)
		onTerminationSignal()
		time.AfterFunc(delay, cancel)
	})
	return ctx
}

// drainingWebhookServer asks clients to close their connection after each response once draining has started. The API
// server keeps sending webhook requests over its existing keep-alive connections to a terminating pod, regardless of
// the endpoint removal. Without closing them, those connections are cut when the webhook server shuts down, and
// requests in flight on them fail with EOF or broken pipe. New connections go through the webhook service and reach a
// pod that is not terminating.
type drainingWebhookServer struct {
	k8swebhook.Server
	draining atomic.Bool
}

func newDrainingWebhookServer(server k8swebhook.Server) *drainingWebhookServer {
	return &drainingWebhookServer{Server: server}
}

func (s *drainingWebhookServer) Register(path string, hook http.Handler) {
	s.Server.Register(path, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if s.draining.Load() {
			w.Header().Set("Connection", "close")
		}
		hook.ServeHTTP(w, r)
	}))
}

func (s *drainingWebhookServer) startDraining() {
	s.draining.Store(true)
}
