// SPDX-FileCopyrightText: Copyright 2026 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package kubectl

import (
	"context"
	"testing"

	"go.opentelemetry.io/otel/attribute"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"

	pb "github.com/dash0hq/dash0-operator/images/agent0-connector/proto"
	"github.com/dash0hq/dash0-operator/images/agent0-connector/selfmonitoring"
)

func TestCommandErrorType(t *testing.T) {
	tests := []struct {
		name              string
		exitCode          int32
		timedOut          bool
		expectedErrorType string
		expectedHasFailed bool
	}{
		{name: "a successful invocation is not an error", exitCode: 0, expectedErrorType: "", expectedHasFailed: false},
		{
			name:              "a timeout takes precedence over the exit code it produces",
			exitCode:          exitCodeTimedOut,
			timedOut:          true,
			expectedErrorType: selfmonitoring.ErrorTypeTimedOut,
			expectedHasFailed: true,
		},
		{
			name:              "a non-zero exit code of kubectl itself is reported as such",
			exitCode:          1,
			expectedErrorType: selfmonitoring.ErrorTypeNonZeroExitCode,
			expectedHasFailed: true,
		},
		{
			name:              "a kubectl that could not be executed is not distinguished from a non-zero exit code",
			exitCode:          exitCodeNotExecutable,
			expectedErrorType: selfmonitoring.ErrorTypeNonZeroExitCode,
			expectedHasFailed: true,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			errorType, hasFailed := commandErrorType(test.exitCode, test.timedOut)
			if errorType != test.expectedErrorType || hasFailed != test.expectedHasFailed {
				t.Errorf(
					"expected (%q, %t), got (%q, %t)",
					test.expectedErrorType,
					test.expectedHasFailed,
					errorType,
					hasFailed,
				)
			}
		})
	}
}

func TestExecuteCommandRequestRecordsMetrics(t *testing.T) {
	logger := discardLogger()

	t.Run("counts a rejected request as rejected and records no duration for it", func(t *testing.T) {
		reader := installMetricReader(t)

		ExecuteCommandRequest(context.Background(), logger, "/tmp", &pb.CommandRequest{
			RequestId: "req-rejected",
			Command:   "helm",
			Arguments: []string{"list"},
		})

		collected := collect(t, reader)
		verifySingleRecording(t, collected, commandRequestsMetricName, commandAttribute, selfmonitoring.CommandUnknown)
		verifySingleRecording(t, collected, commandErrorsMetricName, errorTypeAttribute, selfmonitoring.ErrorTypeRejected)
		if findMetric(collected, commandDurationMetricName) != nil {
			t.Error("expected no duration to be recorded for a request that was never executed")
		}
	})

	t.Run("counts an executed request once, records its duration and no error", func(t *testing.T) {
		fakeKubectlOnPath(t, "#!/bin/sh\necho stdout-line\nexit 0\n")
		reader := installMetricReader(t)

		ExecuteCommandRequest(context.Background(), logger, "/tmp", &pb.CommandRequest{
			RequestId: "req-ok",
			Command:   "kubectl",
			Arguments: []string{"get", "pods"},
		})

		collected := collect(t, reader)
		verifySingleRecording(t, collected, commandRequestsMetricName, commandAttribute, "get")
		verifySingleRecording(t, collected, commandDurationMetricName, commandAttribute, "get")
		if findMetric(collected, commandErrorsMetricName) != nil {
			t.Error("expected no error to be recorded for a successful command")
		}
	})

	t.Run("records a non-zero exit code of kubectl as an error alongside the duration", func(t *testing.T) {
		fakeKubectlOnPath(t, "#!/bin/sh\necho boom >&2\nexit 3\n")
		reader := installMetricReader(t)

		ExecuteCommandRequest(context.Background(), logger, "/tmp", &pb.CommandRequest{
			RequestId: "req-failed",
			Command:   "kubectl",
			Arguments: []string{"get", "pods"},
		})

		collected := collect(t, reader)
		verifySingleRecording(t, collected, commandRequestsMetricName, commandAttribute, "get")
		verifySingleRecording(
			t,
			collected,
			commandErrorsMetricName,
			errorTypeAttribute,
			selfmonitoring.ErrorTypeNonZeroExitCode,
		)
		verifySingleRecording(t, collected, commandDurationMetricName, commandAttribute, "get")
	})
}

// The names and attribute keys of the connector's metrics, duplicated here deliberately: asserting on the literal
// names is what makes a rename of an emitted metric visible as a test failure.
const (
	metricNamePrefix          = "dash0.operator.agent0_connector."
	commandRequestsMetricName = metricNamePrefix + "command.requests"
	commandErrorsMetricName   = metricNamePrefix + "command.errors"
	commandDurationMetricName = metricNamePrefix + "command.duration"

	commandAttribute   = metricNamePrefix + "command"
	errorTypeAttribute = "error.type"
)

// installMetricReader points the connector's instruments at a fresh manual reader for the duration of the test, so
// each subtest sees only its own recordings.
func installMetricReader(t *testing.T) *sdkmetric.ManualReader {
	t.Helper()
	reader := sdkmetric.NewManualReader()
	meterProvider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	selfmonitoring.InitializeMetrics(meterProvider.Meter(selfmonitoring.MeterName), discardLogger())
	return reader
}

func collect(t *testing.T, reader *sdkmetric.ManualReader) metricdata.ResourceMetrics {
	t.Helper()
	var collected metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &collected); err != nil {
		t.Fatalf("cannot collect the recorded metrics: %v", err)
	}
	return collected
}

func findMetric(collected metricdata.ResourceMetrics, name string) *metricdata.Metrics {
	for _, scopeMetrics := range collected.ScopeMetrics {
		for _, metric := range scopeMetrics.Metrics {
			if metric.Name == name {
				return &metric
			}
		}
	}
	return nil
}

// verifySingleRecording asserts that the given metric holds exactly one data point, that it stems from exactly one
// recording, and that it carries the given attribute. It accepts both instrument types the connector uses, so a
// counter and the duration histogram are verified the same way.
func verifySingleRecording(
	t *testing.T,
	collected metricdata.ResourceMetrics,
	metricName string,
	attributeKey string,
	attributeValue string,
) {
	t.Helper()
	metric := findMetric(collected, metricName)
	if metric == nil {
		t.Fatalf("the metric %q has not been recorded", metricName)
	}

	var attributes attribute.Set
	switch data := metric.Data.(type) {
	case metricdata.Sum[int64]:
		dataPoint := singleDataPoint(t, metricName, data.DataPoints)
		if dataPoint.Value != 1 {
			t.Errorf("expected %q to have counted exactly once, got %d", metricName, dataPoint.Value)
		}
		attributes = dataPoint.Attributes
	case metricdata.Histogram[float64]:
		dataPoint := singleDataPoint(t, metricName, data.DataPoints)
		if dataPoint.Count != 1 {
			t.Errorf("expected %q to hold exactly one recording, got %d", metricName, dataPoint.Count)
		}
		attributes = dataPoint.Attributes
	default:
		t.Fatalf("unexpected data type %T for the metric %q", metric.Data, metricName)
	}

	actual, hasAttribute := attributes.Value(attribute.Key(attributeKey))
	if !hasAttribute || actual.AsString() != attributeValue {
		t.Errorf("expected %q to carry %s=%s, got %+v", metricName, attributeKey, attributeValue, attributes)
	}
}

func singleDataPoint[P any](t *testing.T, metricName string, dataPoints []P) P {
	t.Helper()
	if len(dataPoints) != 1 {
		t.Fatalf("expected exactly one data point for %q, got %d: %+v", metricName, len(dataPoints), dataPoints)
	}
	return dataPoints[0]
}
