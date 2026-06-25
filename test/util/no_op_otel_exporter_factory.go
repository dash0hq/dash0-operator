// SPDX-FileCopyrightText: Copyright 2026 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package util

import (
	"context"

	sdklog "go.opentelemetry.io/otel/sdk/log"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"

	"github.com/dash0hq/dash0-operator/images/pkg/common"
)

// NoopExporterFactory is a common.ExporterFactory that creates no-op metric and log exporters. It is intended for unit
// tests, where the real OTLP gRPC exporters would otherwise attempt to connect to (and flush telemetry to) an
// unreachable endpoint. In particular, the real exporters block for up to one second on shutdown while their flush
// attempts time out against the unreachable endpoint; the no-op exporters return immediately instead.
type NoopExporterFactory struct{}

func NewNoopExporterFactory() common.ExporterFactory {
	return NoopExporterFactory{}
}

func (NoopExporterFactory) CreateMetricExporter(_ context.Context, _ *common.OTelSdkConfig) sdkmetric.Exporter {
	return noopMetricExporter{}
}

func (NoopExporterFactory) CreateLogExporter(_ context.Context, _ *common.OTelSdkConfig) sdklog.Exporter {
	return noopLogExporter{}
}

type noopMetricExporter struct{}

func (noopMetricExporter) Temporality(kind sdkmetric.InstrumentKind) metricdata.Temporality {
	return sdkmetric.DefaultTemporalitySelector(kind)
}

func (noopMetricExporter) Aggregation(kind sdkmetric.InstrumentKind) sdkmetric.Aggregation {
	return sdkmetric.DefaultAggregationSelector(kind)
}

func (noopMetricExporter) Export(_ context.Context, _ *metricdata.ResourceMetrics) error { return nil }

func (noopMetricExporter) ForceFlush(_ context.Context) error { return nil }

func (noopMetricExporter) Shutdown(_ context.Context) error { return nil }

type noopLogExporter struct{}

func (noopLogExporter) Export(_ context.Context, _ []sdklog.Record) error { return nil }

func (noopLogExporter) ForceFlush(_ context.Context) error { return nil }

func (noopLogExporter) Shutdown(_ context.Context) error { return nil }
