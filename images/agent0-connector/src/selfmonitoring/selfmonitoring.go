// SPDX-FileCopyrightText: Copyright 2026 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

// Package selfmonitoring defines the OpenTelemetry metrics that agent0-connector reports. As long as the instruments
// have not been created, the record function are no-ops.
package selfmonitoring

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"go.opentelemetry.io/otel/attribute"
	otelmetric "go.opentelemetry.io/otel/metric"
)

const (
	// MeterName is the name of the connector's meter, and the prefix of the name of every metric it reports.
	MeterName = "dash0.operator.agent0_connector"

	// CommandUnknown is the value of the command attribute for a command request that has been rejected before its
	// kubectl command was established. The content of a command request is generated upstream, so putting it into a
	// metric attribute verbatim would put an unbounded number of values into the metric.
	CommandUnknown = "unknown"

	// The values of the error.type attribute of the command errors metric. There is at most one of them per command
	// request.
	ErrorTypeRejected        = "Rejected"
	ErrorTypeTimedOut        = "TimedOut"
	ErrorTypeNonZeroExitCode = "NonZeroExitCode"
	ErrorTypeWithheld        = "Withheld"

	errorTypeAttributeKey = "error.type"

	metricNameLabel = "metric.name"
	errorLabel      = "error"
)

var (
	metricNamePrefix = fmt.Sprintf("%s.", MeterName)

	commandAttributeKey = attribute.Key(metricNamePrefix + "command")

	commandRequestsMetricName = metricNamePrefix + "command.requests"
	commandRequestsMetric     otelmetric.Int64Counter

	commandErrorsMetricName = metricNamePrefix + "command.errors"
	commandErrorsMetric     otelmetric.Int64Counter

	commandDurationMetricName = metricNamePrefix + "command.duration"
	commandDurationMetric     otelmetric.Float64Histogram
)

// InitializeMetrics creates the connector's instruments from the given meter. An instrument that cannot be created is
// reported as an error log message and left unset, which turns the corresponding record function into a no-op.
func InitializeMetrics(meter otelmetric.Meter, logger *slog.Logger) {
	// The meter returns a usable instrument together with the error, so each instrument is only made accessible via its
	// package-level variable once its creation succeeded.
	if requestsMetric, err := meter.Int64Counter(
		commandRequestsMetricName,
		otelmetric.WithUnit("1"),
		otelmetric.WithDescription("Counter for the command requests received from the Dash0 backend"),
	); err != nil {
		logger.Error("cannot initialize the metric", metricNameLabel, commandRequestsMetricName, errorLabel, err)
	} else {
		commandRequestsMetric = requestsMetric
	}

	if errorsMetric, err := meter.Int64Counter(
		commandErrorsMetricName,
		otelmetric.WithUnit("1"),
		otelmetric.WithDescription("Error counter for command requests that did not yield a successful response"),
	); err != nil {
		logger.Error("cannot initialize the metric", metricNameLabel, commandErrorsMetricName, errorLabel, err)
	} else {
		commandErrorsMetric = errorsMetric
	}

	if durationMetric, err := meter.Float64Histogram(
		commandDurationMetricName,
		otelmetric.WithUnit("s"),
		otelmetric.WithDescription(
			"Histogram of how long handling a command request takes, that is, running kubectl and redacting its output",
		),
	); err != nil {
		logger.Error("cannot initialize the metric", metricNameLabel, commandDurationMetricName, errorLabel, err)
	} else {
		commandDurationMetric = durationMetric
	}
}

// RecordCommandRequest counts a command request that has been received from the Dash0 backend. kubectlCommand is the
// kubectl command of the request, or CommandUnknown if the request has been rejected before it was established.
func RecordCommandRequest(ctx context.Context, kubectlCommand string) {
	if commandRequestsMetric == nil {
		return
	}
	commandRequestsMetric.Add(ctx, 1, otelmetric.WithAttributes(commandAttribute(kubectlCommand)))
}

// RecordCommandError counts a command request that did not yield a successful response, where errorType is one of the
// ErrorType* constants.
func RecordCommandError(ctx context.Context, kubectlCommand string, errorType string) {
	if commandErrorsMetric == nil {
		return
	}
	commandErrorsMetric.Add(ctx, 1, otelmetric.WithAttributes(
		commandAttribute(kubectlCommand),
		attribute.String(errorTypeAttributeKey, errorType),
	))
}

// RecordCommandDuration records how long handling a command request took. It is only recorded for a request that has
// actually been executed, not for one that has been rejected.
func RecordCommandDuration(ctx context.Context, kubectlCommand string, duration time.Duration) {
	if commandDurationMetric == nil {
		return
	}
	commandDurationMetric.Record(ctx, duration.Seconds(), otelmetric.WithAttributes(commandAttribute(kubectlCommand)))
}

func commandAttribute(kubectlCommand string) attribute.KeyValue {
	if kubectlCommand == "" {
		return commandAttributeKey.String(CommandUnknown)
	}
	return commandAttributeKey.String(kubectlCommand)
}
