// SPDX-FileCopyrightText: Copyright 2026 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package rate

import (
	"context"
	"sync"
	"time"

	"github.com/dash0hq/dash0-operator/internal/util/logd"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

// mockWaiter is a controllable waiter for tests. When blocking is true, Wait blocks until either the context is
// canceled or unblock() is called. When blocking is false, Wait returns nil immediately.
type mockWaiter struct {
	blocking  bool
	unblockCh chan struct{}
	once      sync.Once
}

func newImmediateWaiter() *mockWaiter {
	return &mockWaiter{
		blocking:  false,
		unblockCh: make(chan struct{}),
	}
}

func newBlockingWaiter() *mockWaiter {
	return &mockWaiter{
		blocking:  true,
		unblockCh: make(chan struct{}),
	}
}

func (m *mockWaiter) Wait(ctx context.Context) error {
	if !m.blocking {
		return nil
	}
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-m.unblockCh:
		return nil
	}
}

// unblock releases all goroutines currently blocked in Wait and allows future calls to return immediately.
func (m *mockWaiter) unblock() {
	m.once.Do(func() { close(m.unblockCh) })
}

var _ = Describe("CappedRateLimiter", func() {

	ctx := context.Background()
	var logger = logd.Discard()

	Describe("NewDefaultCappedRateLimiter", func() {
		It("passes the first call immediately using the initial burst token", func() {
			crl := NewDefaultCappedRateLimiter()
			ctx, cancel := context.WithTimeout(ctx, 1*time.Second)
			defer cancel()
			Expect(crl.Wait(ctx, "kind", "name", logger)).To(BeTrue())
		})

		It("blocks until the rate limiter grants a token", func() {
			w := newBlockingWaiter()
			crl := &CappedRateLimiter{limiter: w, maxPendingOperations: 0}

			result := make(chan bool, 1)
			go func() {
				defer GinkgoRecover()
				result <- crl.Wait(ctx, "kind", "name", logger)
			}()

			// Nothing arrives while the waiter is blocking.
			Consistently(result, "100ms", "10ms").ShouldNot(Receive())

			// Once the rate limiter grants the token, the operation completes successfully.
			w.unblock()
			Eventually(result).Should(Receive(BeTrue()))
		})
	})

	Describe("with maxPendingOperations limit", func() {
		It("lets operations through when below the maxPendingOperations limit", func() {
			crl := &CappedRateLimiter{
				limiter:              newImmediateWaiter(),
				maxPendingOperations: 5,
			}
			for range 5 {
				Expect(crl.Wait(ctx, "kind", "name", logger)).To(BeTrue())
			}
		})

		It("drops an operation when the maxPendingOperations limit is exceeded", func() {
			w := newBlockingWaiter()
			crl := &CappedRateLimiter{
				limiter:              w,
				maxPendingOperations: 2,
			}

			// Start 2 goroutines that will block at the waiter.
			results := make(chan bool, 2)
			for range 2 {
				go func() {
					defer GinkgoRecover()
					results <- crl.Wait(ctx, "kind", "name", logger)
				}()
			}

			// Wait until both goroutines have incremented the counter.
			Eventually(func() int64 {
				return crl.pendingOperationsCount.Load()
			}).Should(Equal(int64(2)))

			// The 3rd operation exceeds the maxPendingOperations limit and must be dropped.
			Expect(crl.Wait(ctx, "kind", "name", logger)).To(BeFalse())

			// Unblock the two waiting goroutines.
			w.unblock()
			Expect(<-results).To(BeTrue())
			Expect(<-results).To(BeTrue())
		})

		It("permits new operations once pending operations have completed", func() {
			w := newBlockingWaiter()
			crl := &CappedRateLimiter{
				limiter:              w,
				maxPendingOperations: 1,
			}

			firstResult := make(chan bool, 1)
			go func() {
				defer GinkgoRecover()
				firstResult <- crl.Wait(ctx, "kind", "name", logger)
			}()

			// Wait until the first operation is pending.
			Eventually(func() int64 {
				return crl.pendingOperationsCount.Load()
			}).Should(Equal(int64(1)))

			// While the first is pending, a second is dropped (maxPendingOperations=1, pending count would become 2).
			Expect(crl.Wait(ctx, "kind", "name", logger)).To(BeFalse())

			// Unblock the first and verify it succeeded.
			w.unblock()
			Expect(<-firstResult).To(BeTrue())

			// Counter must return to zero.
			Eventually(func() int64 {
				return crl.pendingOperationsCount.Load()
			}).Should(Equal(int64(0)))

			// A new operation must now succeed. The closed unblockCh immediately satisfies the select in mockWaiter.
			Expect(crl.Wait(ctx, "kind", "name", logger)).To(BeTrue())
		})
	})

	Describe("context cancellation", func() {
		It("returns false when the context is canceled while waiting at the rate limiter", func() {
			w := newBlockingWaiter()
			crl := &CappedRateLimiter{
				limiter:              w,
				maxPendingOperations: 5,
			}

			ctx, cancel := context.WithCancel(ctx)
			result := make(chan bool, 1)
			go func() {
				defer GinkgoRecover()
				result <- crl.Wait(ctx, "kind", "name", logger)
			}()

			// Wait until the goroutine is pending, then cancel.
			Eventually(func() int64 {
				return crl.pendingOperationsCount.Load()
			}).Should(Equal(int64(1)))
			cancel()

			Expect(<-result).To(BeFalse())
		})

		It("decrements the pending counter after context cancellation", func() {
			w := newBlockingWaiter()
			crl := &CappedRateLimiter{
				limiter:              w,
				maxPendingOperations: 5,
			}

			ctx, cancel := context.WithCancel(ctx)
			done := make(chan struct{})
			go func() {
				defer GinkgoRecover()
				defer close(done)
				crl.Wait(ctx, "kind", "name", logger)
			}()

			Eventually(func() int64 {
				return crl.pendingOperationsCount.Load()
			}).Should(Equal(int64(1)))

			cancel()
			<-done

			Expect(crl.pendingOperationsCount.Load()).To(Equal(int64(0)))
		})
	})

	Describe("pending operations counter", func() {
		It("accurately reflects the number of concurrently pending operations", func() {
			const numOps = 5
			w := newBlockingWaiter()
			crl := &CappedRateLimiter{
				limiter:              w,
				maxPendingOperations: numOps,
			}

			for range numOps {
				go func() {
					defer GinkgoRecover()
					crl.Wait(ctx, "kind", "name", logger)
				}()
			}

			// Counter must reach numOps.
			Eventually(func() int64 {
				return crl.pendingOperationsCount.Load()
			}).Should(Equal(int64(numOps)))

			// Unblock all goroutines.
			w.unblock()

			// Counter must return to zero once all operations complete.
			Eventually(func() int64 {
				return crl.pendingOperationsCount.Load()
			}).Should(Equal(int64(0)))
		})

		It("decrements the counter even when the maxPendingOperations limit is exceeded", func() {
			w := newBlockingWaiter()
			crl := &CappedRateLimiter{
				limiter:              w,
				maxPendingOperations: 1,
			}

			firstDone := make(chan struct{})
			go func() {
				defer GinkgoRecover()
				defer close(firstDone)
				crl.Wait(ctx, "kind", "name", logger)
			}()

			Eventually(func() int64 {
				return crl.pendingOperationsCount.Load()
			}).Should(Equal(int64(1)))

			// The second operation is dropped; its counter increment must be rolled back immediately.
			Expect(crl.Wait(ctx, "kind", "name", logger)).To(BeFalse())
			Expect(crl.pendingOperationsCount.Load()).To(Equal(int64(1)))

			w.unblock()
			<-firstDone
			Expect(crl.pendingOperationsCount.Load()).To(Equal(int64(0)))
		})
	})
})
