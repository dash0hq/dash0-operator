// SPDX-FileCopyrightText: Copyright 2026 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package debounce

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestCoalescesNotifications(t *testing.T) {
	trigger := New(50 * time.Millisecond)
	ctx, cancel := context.WithCancel(t.Context())
	var calls atomic.Int32
	done := make(chan struct{})
	go func() {
		trigger.Run(ctx, func() { calls.Add(1) })
		close(done)
	}()

	for range 100 {
		trigger.Notify()
	}
	require.Eventually(t, func() bool { return calls.Load() == 1 }, time.Second, 10*time.Millisecond)
	time.Sleep(150 * time.Millisecond)
	require.Equal(t, int32(1), calls.Load(), "no call without a new notification")

	trigger.Notify()
	require.Eventually(t, func() bool { return calls.Load() == 2 }, time.Second, 10*time.Millisecond)

	cancel()
	<-done
}

func TestNotifyDoesNotBlockWithoutRun(t *testing.T) {
	trigger := New(time.Hour)
	for range 10 {
		trigger.Notify()
	}
}
