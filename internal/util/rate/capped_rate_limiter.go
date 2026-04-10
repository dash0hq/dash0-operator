// SPDX-FileCopyrightText: Copyright 2026 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package rate

import (
	"context"
	"fmt"
	"sync/atomic"

	"golang.org/x/time/rate"

	"github.com/dash0hq/dash0-operator/internal/util/logd"
)

const (
	// Allow at most apiSyncMaxItem per apiSyncTimeWindoSeconds, by processing them at a frequency of apiSyncRateLimit
	// requests per second. Naturally, this does not fully guarantee not running into backend-side API rate limits, since
	// other sources might also make requests (operators in other clusters, terraform, dash0-cli etc.)
	apiSyncMaxItem          = 500.0
	apiSyncTimeWindoSeconds = 300.0

	// apiSyncRateLimit per second API sync requests will be processed
	apiSyncRateLimit = apiSyncMaxItem / apiSyncTimeWindoSeconds

	// maxPendingApiSyncItems is the hard limit for pending API sync requests for dashboards, check rules etc. This is
	// an additional limit on top of the rate-limiting that we apply client-side. Under normal circumstances, it should
	// never be reached.
	maxPendingApiSyncItems = 20_000
)

// waiter blocks until a rate-limiting token is available or the context is canceled.
type waiter interface {
	Wait(ctx context.Context) error
}

// CappedRateLimiter combines a rate limiter with an upper limit on the number of pending operatrions. It is used to
// spread out API sync requests to avoid running into the backend's rate limit, while also protecting against an
// ever-growing backlog of API sync operations.
type CappedRateLimiter struct {
	limiter                waiter
	pendingOperationsCount atomic.Int64
	maxPendingOperations   int64
}

func NewDefaultCappedRateLimiter() *CappedRateLimiter {
	return NewCappedRateLimiter(apiSyncRateLimit, maxPendingApiSyncItems)
}

func NewNoOpCappedRateLimiter() *CappedRateLimiter {
	return NewCappedRateLimiter(rate.Inf, 0)
}

// NewCappedRateLimiter creates a new rate limiter that will let operations pass with a frequency of rateLimit, and drop
// operations once the hard limit of maxPendingOperations has been reached. Use golang.org/x/time#Inf to disable waiting
// entirely. Use maxPendingOperations=0 disable limiting the number of pending operations.
func NewCappedRateLimiter(rateLimit rate.Limit, maxPendingOperations int64) *CappedRateLimiter {
	return &CappedRateLimiter{
		limiter:              rate.NewLimiter(rateLimit, 1),
		maxPendingOperations: maxPendingOperations,
	}
}

// Wait blocks requests according to the rateLimit frequency set at creation time. After the wait time, it returns true
// to signal that the operation can proceed.
// It also checks whether the limit for pending operations has been reached. It return false immediately if that is the
// case and the operation should be dropped. It also returns false if the operation should be dropped for other reason,
// for example if the context has been canceled.
func (c *CappedRateLimiter) Wait(
	ctx context.Context,
	kindDisplayName string,
	resourceName string,
	logger logd.Logger,
) bool {
	if c.maxPendingOperations > 0 {
		newCount := c.pendingOperationsCount.Add(1)
		defer func() {
			c.pendingOperationsCount.Add(-1)
		}()
		if c.maxPendingOperations > 0 && newCount > c.maxPendingOperations {
			logger.Error(
				fmt.Errorf("the limit for pending API synchronization operations has been breached, dropping request"),
				fmt.Sprintf(
					"Dropping API synchronization request for %s %s: too many pending items (%d/%d)",
					kindDisplayName,
					resourceName,
					newCount-1,
					c.maxPendingOperations,
				),
			)
			return false
		}
	}

	logger.Debug(
		"Waiting on rate limiter to proceed with the API synchronization operation for %s %s.",
		kindDisplayName,
		resourceName,
	)
	if err := c.limiter.Wait(ctx); err != nil {
		// context canceled (e.g. during shutdown), no need to log
		logger.Debug("Context canceled while waiting on rate limiter for %s %s.", kindDisplayName, resourceName)
		return false
	}
	logger.Debug(
		"Rate limiter has allowed the API synchronization operation for %s %s to proceed.",
		kindDisplayName,
		resourceName,
	)
	return true
}
