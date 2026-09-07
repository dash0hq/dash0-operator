// SPDX-FileCopyrightText: Copyright 2026 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package selfmonitoring

import (
	"context"
	"io"
	"log/slog"
	"testing"
	"time"

	"go.opentelemetry.io/otel/attribute"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
)

func TestRecordMetrics(t *testing.T) {
	reader := sdkmetric.NewManualReader()
	meterProvider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	InitializeMetrics(meterProvider.Meter(MeterName), discardingLogger())

	ctx := context.Background()
	RecordCommandRequest(ctx, "get")
	RecordCommandRequest(ctx, CommandUnknown)
	RecordCommandError(ctx, "get", ErrorTypeTimedOut)
	RecordCommandDuration(ctx, "get", 250*time.Millisecond)

	var collected metricdata.ResourceMetrics
	if err := reader.Collect(ctx, &collected); err != nil {
		t.Fatalf("cannot collect the recorded metrics: %v", err)
	}

	requests := findMetric(t, collected, "dash0.operator.agent0_connector.command.requests")
	requestsData, ok := requests.Data.(metricdata.Sum[int64])
	if !ok {
		t.Fatalf("expected the command requests metric to be an int64 sum, got %T", requests.Data)
	}
	if len(requestsData.DataPoints) != 2 {
		t.Fatalf("expected one data point per command, got %d", len(requestsData.DataPoints))
	}
	verifyAttribute(t, requestsData.DataPoints, "dash0.operator.agent0_connector.command", "get")
	verifyAttribute(t, requestsData.DataPoints, "dash0.operator.agent0_connector.command", CommandUnknown)

	errors := findMetric(t, collected, "dash0.operator.agent0_connector.command.errors")
	errorsData, ok := errors.Data.(metricdata.Sum[int64])
	if !ok {
		t.Fatalf("expected the command errors metric to be an int64 sum, got %T", errors.Data)
	}
	verifyAttribute(t, errorsData.DataPoints, "error.type", ErrorTypeTimedOut)

	duration := findMetric(t, collected, "dash0.operator.agent0_connector.command.duration")
	durationData, ok := duration.Data.(metricdata.Histogram[float64])
	if !ok {
		t.Fatalf("expected the command duration metric to be a float64 histogram, got %T", duration.Data)
	}
	if len(durationData.DataPoints) != 1 || durationData.DataPoints[0].Sum != 0.25 {
		t.Errorf("unexpected data points for the command duration metric: %+v", durationData.DataPoints)
	}
	if duration.Unit != "s" {
		t.Errorf("expected the command duration metric to be reported in seconds, got %q", duration.Unit)
	}
}

// TestRecordWithoutInstruments verifies that recording a metric before the instruments have been created (the OTel SDK
// is not configured, or an instrument could not be created) does nothing instead of panicking.
func TestRecordWithoutInstruments(t *testing.T) {
	previousRequests, previousErrors, previousDuration := commandRequestsMetric, commandErrorsMetric, commandDurationMetric
	t.Cleanup(func() {
		commandRequestsMetric, commandErrorsMetric, commandDurationMetric =
			previousRequests, previousErrors, previousDuration
	})

	commandRequestsMetric = nil
	commandErrorsMetric = nil
	commandDurationMetric = nil

	ctx := context.Background()
	RecordCommandRequest(ctx, "get")
	RecordCommandError(ctx, "get", ErrorTypeRejected)
	RecordCommandDuration(ctx, "get", time.Second)
}

func findMetric(t *testing.T, collected metricdata.ResourceMetrics, name string) metricdata.Metrics {
	t.Helper()
	for _, scopeMetrics := range collected.ScopeMetrics {
		for _, metric := range scopeMetrics.Metrics {
			if metric.Name == name {
				return metric
			}
		}
	}
	t.Fatalf("the metric %q has not been recorded", name)
	return metricdata.Metrics{}
}

func verifyAttribute[N int64 | float64](
	t *testing.T,
	dataPoints []metricdata.DataPoint[N],
	key string,
	value string,
) {
	t.Helper()
	for _, dataPoint := range dataPoints {
		if actual, hasAttribute := dataPoint.Attributes.Value(attribute.Key(key)); hasAttribute &&
			actual.AsString() == value {
			return
		}
	}
	t.Errorf("no data point carries the attribute %s=%s: %+v", key, value, dataPoints)
}

func discardingLogger() *slog.Logger {
	return slog.New(slog.NewJSONHandler(io.Discard, nil))
}
