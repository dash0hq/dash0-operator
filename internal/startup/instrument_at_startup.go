// SPDX-FileCopyrightText: Copyright 2025 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package startup

import (
	"context"

	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/manager"

	"github.com/dash0hq/dash0-operator/internal/instrumentation"
)

// InstrumentAtStartupRunnable executes an unconditional apply/update of instrumentation for all workloads in
// Dash0-enabled namespaces, according to the respective settings of the Dash0 monitoring resource in the namespace.
// See godoc comment on Instrumenter#InstrumentAtStartup.
type InstrumentAtStartupRunnable struct {
	manager      manager.Manager
	instrumenter *instrumentation.Instrumenter
}

func NewInstrumentAtStartupRunnable(
	manager manager.Manager,
	instrumenter *instrumentation.Instrumenter,
) *InstrumentAtStartupRunnable {
	return &InstrumentAtStartupRunnable{
		manager:      manager,
		instrumenter: instrumenter,
	}
}

// NeedLeaderElection implements the LeaderElectionRunnable interface, which indicates
// the webhook server doesn't need leader election.
func (r *InstrumentAtStartupRunnable) NeedLeaderElection() bool {
	return true
}

// Start runs the instrumentation procedure.
func (r *InstrumentAtStartupRunnable) Start(ctx context.Context) error {
	logger := log.FromContext(ctx)
	r.instrumenter.Client = r.manager.GetClient()
	r.instrumenter.InstrumentAtStartup(ctx, &logger)
	return nil
}
