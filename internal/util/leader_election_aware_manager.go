// SPDX-FileCopyrightText: Copyright 2025 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package util

import (
	"context"
	"sync/atomic"

	"sigs.k8s.io/controller-runtime/pkg/log"
)

type LeaderElectionAware interface {
	IsLeader() bool
}

// LeaderElectionAwareRunnable serves the purpose of making the operator manager aware whether it is the current leader
// or not.
type LeaderElectionAwareRunnable struct {
	isLeader atomic.Bool
}

func NewLeaderElectionAwareRunnable() *LeaderElectionAwareRunnable {
	return &LeaderElectionAwareRunnable{}
}

// NeedLeaderElection implements the LeaderElectionRunnable interface, which indicates
// that the LeaderElectionAwareRunnable requires leader election.
func (r *LeaderElectionAwareRunnable) NeedLeaderElection() bool {
	return true
}

// Start is the signal from controller-runtime for the runnable that this replica has become leader.
func (r *LeaderElectionAwareRunnable) Start(ctx context.Context) error {
	log.FromContext(ctx).Info("This operator manager replica has just become leader.")
	r.isLeader.Store(true)
	return nil
}

func (r *LeaderElectionAwareRunnable) IsLeader() bool {
	return r.isLeader.Load()
}
