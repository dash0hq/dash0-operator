// SPDX-FileCopyrightText: Copyright 2025 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package startup

import (
	"context"

	"sigs.k8s.io/controller-runtime/pkg/manager"

	"github.com/dash0hq/dash0-operator/internal/instrumentation"
	"github.com/dash0hq/dash0-operator/internal/util/cluster"
	"github.com/dash0hq/dash0-operator/internal/util/logd"
)

// InstrumentAtStartupRunnable executes an unconditional apply/update of instrumentation for all workloads in
// Dash0-enabled namespaces, according to the respective settings of the Dash0 monitoring resource in the namespace.
// See godoc comment on Instrumenter#InstrumentAtStartup.
type InstrumentAtStartupRunnable struct {
	manager            manager.Manager
	instrumenter       *instrumentation.Instrumenter
	readyCheckExecuter *ReadyCheckExecuter
}

func NewInstrumentAtStartupRunnable(
	manager manager.Manager,
	instrumenter *instrumentation.Instrumenter,
	readyCheckExecuter *ReadyCheckExecuter,
) *InstrumentAtStartupRunnable {
	return &InstrumentAtStartupRunnable{
		manager:            manager,
		instrumenter:       instrumenter,
		readyCheckExecuter: readyCheckExecuter,
	}
}

// NeedLeaderElection implements the LeaderElectionRunnable interface, which indicates
// that the InstrumentAtStartupRunnable requires leader election.
func (r *InstrumentAtStartupRunnable) NeedLeaderElection() bool {
	return true
}

// Start runs the instrumentation procedure.
func (r *InstrumentAtStartupRunnable) Start(ctx context.Context) error {
	logger := logd.FromContext(ctx)
	r.instrumenter.Client = r.manager.GetClient()
	logger.Info("waiting for instrumentation delivery resolution before instrumenting workloads at startup")
	// The minimum kubelet version detection runs concurrently with the operator manager startup, so it can still be in
	// progress here. Instrumenting workloads now would pin them to the init container fallback, since they are not
	// re-instrumented when the resolved delivery mechanism changes later.
	resolvedInstrumentationDelivery := r.instrumenter.ClusterInstrumentationConfig.
		WaitForInstrumentationDeliveryAutoToBeResolved(ctx, cluster.InstrumentAtStartUpDeliverySettleTimeout)
	if r.instrumenter.ClusterInstrumentationConfig.InstrumentPods && !r.waitForWebhookService(ctx, logger) {
		return nil
	}
	logger.Info(
		"instrumenting existing workloads at startup",
		"resolved instrumentation delivery", resolvedInstrumentationDelivery,
	)
	r.instrumenter.InstrumentAtStartup(ctx, logger)
	return nil
}

// waitForWebhookService blocks until the webhook service endpoint is ready. Pods evicted at startup are only instrumented
// if the webhook is reachable when they are recreated, and without a lease holder this replica leads before that.
func (r *InstrumentAtStartupRunnable) waitForWebhookService(ctx context.Context, logger logd.Logger) bool {
	logger.Info("waiting for the webhook service to become available before evicting uninstrumented pods at startup")
	webhookServiceIsAvailable, err := r.readyCheckExecuter.waitForWebhookServiceEndpointToBecomeReady(ctx, logger)
	if err != nil || !webhookServiceIsAvailable {
		logger.Warn(
			"the webhook service did not become available, not evicting uninstrumented pods at startup",
			"error", err,
		)
		return false
	}
	return true
}
