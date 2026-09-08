// SPDX-FileCopyrightText: Copyright 2026 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package util

import (
	"errors"
	"sync"
	"sync/atomic"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("The reconcile guard", func() {
	It("executes the reconciliation and returns its result", func() {
		guard := &ReconcileGuard{}

		hasBeenReconciled, err := guard.Run(func() (bool, error) { return true, nil }, nil)

		Expect(err).ToNot(HaveOccurred())
		Expect(hasBeenReconciled).To(BeTrue())
	})

	It("passes on the error of the reconciliation", func() {
		guard := &ReconcileGuard{}
		expectedErr := errors.New("reconciliation failed")

		_, err := guard.Run(func() (bool, error) { return false, expectedErr }, nil)

		Expect(err).To(MatchError(expectedErr))
	})

	It("can be used again after a reconciliation has finished", func() {
		guard := &ReconcileGuard{}
		executions := 0
		reconcile := func() (bool, error) {
			executions++
			return true, nil
		}

		for range 3 {
			_, err := guard.Run(reconcile, nil)
			Expect(err).ToNot(HaveOccurred())
		}

		Expect(executions).To(Equal(3))
	})

	It("does not execute a reconciliation while another one is in progress", func() {
		guard := &ReconcileGuard{}
		executions := 0
		var nested struct {
			hasBeenReconciled bool
			err               error
			skipped           bool
		}

		_, err := guard.Run(func() (bool, error) {
			executions++
			if executions == 1 {
				nested.hasBeenReconciled, nested.err = guard.Run(
					func() (bool, error) {
						Fail("the nested reconciliation must not be executed")
						return false, nil
					},
					func() { nested.skipped = true },
				)
			}
			return true, nil
		}, nil)

		Expect(err).ToNot(HaveOccurred())
		Expect(nested.skipped).To(BeTrue())
		Expect(nested.err).ToNot(HaveOccurred())
		Expect(nested.hasBeenReconciled).To(BeFalse())
	})

	It("repeats the reconciliation once for a trigger which arrived while it was running", func() {
		guard := &ReconcileGuard{}
		executions := 0

		_, err := guard.Run(func() (bool, error) {
			executions++
			if executions == 1 {
				// A trigger arrives while the first execution is still running. It must not be lost.
				_, _ = guard.Run(func() (bool, error) { return true, nil }, nil)
			}
			return true, nil
		}, nil)

		Expect(err).ToNot(HaveOccurred())
		// Once for the original trigger, once for the one that arrived while it was running, and no more: the second
		// execution saw no further trigger.
		Expect(executions).To(Equal(2))
	})

	It("coalesces several triggers which arrive while a reconciliation is running into one repetition", func() {
		guard := &ReconcileGuard{}
		executions := 0

		_, err := guard.Run(func() (bool, error) {
			executions++
			if executions == 1 {
				for range 5 {
					_, _ = guard.Run(func() (bool, error) { return true, nil }, nil)
				}
			}
			return true, nil
		}, nil)

		Expect(err).ToNot(HaveOccurred())
		Expect(executions).To(Equal(2))
	})

	It("does not repeat a reconciliation which failed", func() {
		guard := &ReconcileGuard{}
		executions := 0
		expectedErr := errors.New("reconciliation failed")

		_, err := guard.Run(func() (bool, error) {
			executions++
			_, _ = guard.Run(func() (bool, error) { return true, nil }, nil)
			// Repeating immediately would busy-loop on a permanent failure, the caller requeues instead.
			return false, expectedErr
		}, nil)

		Expect(err).To(MatchError(expectedErr))
		Expect(executions).To(Equal(1))
	})

	It("executes exactly one reconciliation at a time under concurrent triggers", func() {
		guard := &ReconcileGuard{}
		var concurrent atomic.Int32
		var executions atomic.Int32
		var waitGroup sync.WaitGroup

		for range 50 {
			waitGroup.Add(1)
			go func() {
				defer waitGroup.Done()
				_, _ = guard.Run(func() (bool, error) {
					Expect(concurrent.Add(1)).To(BeEquivalentTo(1))
					executions.Add(1)
					concurrent.Add(-1)
					return true, nil
				}, nil)
			}()
		}
		waitGroup.Wait()

		// Every trigger either ran or was coalesced into a repetition, and none of them ran concurrently.
		Expect(executions.Load()).To(BeNumerically(">=", 1))
		Expect(concurrent.Load()).To(BeZero())
	})
})
