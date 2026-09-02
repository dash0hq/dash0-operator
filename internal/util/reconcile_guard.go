// SPDX-FileCopyrightText: Copyright 2026 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package util

import "sync"

// ReconcileGuard serializes the reconciliations of one component and makes sure that a trigger which arrives while a
// reconciliation is in progress is not lost.
//
// A reconciliation reads the state it acts on when it starts, so a trigger that arrives afterwards refers to state the
// running reconciliation has not seen. Simply skipping that trigger loses the change it reports: none of the operator's
// controllers requeues periodically, so nothing reapplies it until an unrelated event happens to trigger the next
// reconciliation. Instead of running the reconciliations concurrently, the guard remembers the trigger and repeats the
// reconciliation once the running one has finished.
//
// A guard is only correct for a reconciliation which reads its input itself. One that receives its input as an
// argument must not be repeated, it would repeat with the stale argument; see
// SignalControlManager.ReconcileSignalControl.
//
// A reconciliation which triggers itself on every run keeps the guard repeating it. The operator's reconciliations are
// idempotent - an unchanged resource is not written, hence produces no watch event - so they settle, but a
// reconciliation which writes on every run would turn a dropped trigger into a busy loop.
//
// The zero value is ready to use. A ReconcileGuard must not be copied after first use.
type ReconcileGuard struct {
	mutex sync.Mutex
	// running reports whether a reconciliation is currently being executed.
	running bool
	// pending reports whether a trigger arrived while a reconciliation was being executed.
	pending bool
}

// Run executes reconcile and returns its result, unless another reconciliation is already in progress: then it
// remembers the trigger, calls onSkipped (if it is not nil) and returns (false, nil) without executing reconcile. The
// reconciliation which is in progress repeats itself once it is done, so the skipped trigger still takes effect.
//
// A reconciliation which returns an error is not repeated: its caller requeues the reconcile request for that, and
// repeating immediately would busy-loop on a permanent failure. The remembered trigger survives and is picked up by
// the next reconciliation.
func (g *ReconcileGuard) Run(reconcile func() (bool, error), onSkipped func()) (bool, error) {
	if !g.acquire() {
		if onSkipped != nil {
			onSkipped()
		}
		return false, nil
	}

	for {
		g.clearPending()

		hasBeenReconciled, err := reconcile()

		if g.releaseUnlessPending(err != nil) {
			return hasBeenReconciled, err
		}
	}
}

// acquire marks a reconciliation as in progress. It reports false when one is already in progress, in which case it
// records that another trigger arrived.
func (g *ReconcileGuard) acquire() bool {
	g.mutex.Lock()
	defer g.mutex.Unlock()
	if g.running {
		g.pending = true
		return false
	}
	g.running = true
	return true
}

func (g *ReconcileGuard) clearPending() {
	g.mutex.Lock()
	defer g.mutex.Unlock()
	g.pending = false
}

// releaseUnlessPending ends the reconciliation and reports true, unless a trigger arrived while it was running and it
// did not fail, in which case it reports false and the reconciliation has to run again. Checking the flag and ending
// the reconciliation happen under the same lock, otherwise a trigger arriving between the two would be recorded and
// then never acted upon.
func (g *ReconcileGuard) releaseUnlessPending(failed bool) bool {
	g.mutex.Lock()
	defer g.mutex.Unlock()
	if !failed && g.pending {
		return false
	}
	g.running = false
	return true
}
