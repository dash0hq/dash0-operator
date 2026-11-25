// SPDX-FileCopyrightText: Copyright 2026 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

// Package debounce coalesces bursts of change notifications, so that expensive work like regenerating the scrape
// configs or recomputing the assignment runs at most once per interval.
package debounce

import (
	"context"
	"time"
)

// Trigger coalesces notifications. Any number of notifications within one interval result in a single call.
type Trigger struct {
	interval time.Duration
	pending  chan struct{}
}

// New creates a Trigger that calls its function at most once per interval.
func New(interval time.Duration) *Trigger {
	return &Trigger{
		interval: interval,
		pending:  make(chan struct{}, 1),
	}
}

// Notify records that the function needs to be called. It never blocks and can be called from any goroutine.
func (t *Trigger) Notify() {
	select {
	case t.pending <- struct{}{}:
	default:
	}
}

// Run calls fn at the end of every interval in which at least one notification has arrived, until ctx is cancelled.
// All calls of fn happen in the goroutine that called Run.
func (t *Trigger) Run(ctx context.Context, fn func()) {
	ticker := time.NewTicker(t.interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			select {
			case <-t.pending:
				fn()
			default:
			}
		}
	}
}
