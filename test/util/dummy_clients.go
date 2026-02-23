// SPDX-FileCopyrightText: Copyright 2025 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package util

import (
	"context"

	"github.com/go-logr/logr"
	otelmetric "go.opentelemetry.io/otel/metric"
)

type DummyAuthTokenClient struct {
	SetAuthTokenCalls    int
	RemoveAuthTokenCalls int
	AuthToken            string
}

func (c *DummyAuthTokenClient) SetDefaultAuthToken(_ context.Context, authToken string, _ logr.Logger) {
	c.SetAuthTokenCalls++
	c.AuthToken = authToken
}

func (c *DummyAuthTokenClient) RemoveDefaultAuthToken(_ context.Context, _ logr.Logger) {
	c.RemoveAuthTokenCalls++
	c.AuthToken = ""
}

func (c *DummyAuthTokenClient) Reset() {
	c.AuthToken = ""
	c.ResetCallCounts()
}

func (c *DummyAuthTokenClient) ResetCallCounts() {
	c.SetAuthTokenCalls = 0
	c.RemoveAuthTokenCalls = 0
}

type DummySelfMonitoringMetricsClient struct {
	InitializeSelfMonitoringMetricsCalls int
}

func (c *DummySelfMonitoringMetricsClient) InitializeSelfMonitoringMetrics(
	_ otelmetric.Meter,
	_ string,
	_ logr.Logger,
) {
	c.InitializeSelfMonitoringMetricsCalls++
}

func (c *DummySelfMonitoringMetricsClient) Reset() {
	c.ResetCallCounts()
}

func (c *DummySelfMonitoringMetricsClient) ResetCallCounts() {
	c.InitializeSelfMonitoringMetricsCalls = 0
}
