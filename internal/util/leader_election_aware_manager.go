// SPDX-FileCopyrightText: Copyright 2025 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package util

import (
	"context"
	"sync/atomic"

	"github.com/go-logr/logr"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

type LeaderElectionClient interface {
	NotifiyOperatorManagerJustBecameLeader(context.Context, *logr.Logger)
}

type LeaderElectionAware interface {
	IsLeader() bool
}

// LeaderElectionAwareRunnable serves the purpose of making the operator manager aware whether it is the current leader
// or not.
type LeaderElectionAwareRunnable struct {
	isLeader atomic.Bool
	clients  []LeaderElectionClient
}

func NewLeaderElectionAwareRunnable() *LeaderElectionAwareRunnable {
	return &LeaderElectionAwareRunnable{}
}

// NeedLeaderElection implements the LeaderElectionRunnable interface, which indicates
// that the LeaderElectionAwareRunnable requires leader election.
func (r *LeaderElectionAwareRunnable) NeedLeaderElection() bool {
	return true
}

func (r *LeaderElectionAwareRunnable) AddLeaderElectionClient(client LeaderElectionClient) {
	r.clients = append(r.clients, client)
}

// Start is the signal from controller-runtime for the runnable that this replica has become leader.
func (r *LeaderElectionAwareRunnable) Start(ctx context.Context) error {
	logger := log.FromContext(ctx)
	logger.Info("This operator manager replica has just become leader.")
	r.isLeader.Store(true)
	for _, client := range r.clients {
		client.NotifiyOperatorManagerJustBecameLeader(ctx, &logger)
	}
	return nil
}

func (r *LeaderElectionAwareRunnable) IsLeader() bool {
	return r.isLeader.Load()
}
