// SPDX-FileCopyrightText: Copyright 2025 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package common

import (
	"encoding/json"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// This file contains common types used in multiple versions of the Dash0Monitoring resource definition.

const (
	MonitoringFinalizerId = "operator.dash0.com/dash0-monitoring-finalizer"
)

// InstrumentWorkloadsMode describes when exactly workloads will be instrumented.  Only one of the following modes
// may be specified. If none of the following policies is specified, the default one is All. See
// Dash0MonitoringSpec.InstrumentWorkloads.Mode for more details.
//
// +kubebuilder:validation:Enum=all;created-and-updated;none
type InstrumentWorkloadsMode string

const (
	// InstrumentWorkloadsModeAll allows instrumenting existing as well as new and updated workloads.
	InstrumentWorkloadsModeAll InstrumentWorkloadsMode = "all"

	// InstrumentWorkloadsModeCreatedAndUpdated disables instrumenting existing workloads, but new and updated workloads
	// will be instrumented.
	InstrumentWorkloadsModeCreatedAndUpdated InstrumentWorkloadsMode = "created-and-updated"

	// InstrumentWorkloadsModeNone will disable instrumentation of workloads entirely for a namespace.
	InstrumentWorkloadsModeNone InstrumentWorkloadsMode = "none"
)

var AllInstrumentWorkloadsMode = []InstrumentWorkloadsMode{
	InstrumentWorkloadsModeAll,
	InstrumentWorkloadsModeCreatedAndUpdated,
	InstrumentWorkloadsModeNone,
}

type LogCollection struct {
	// Opt-out for log collection for the target namespace. If set to `false`, the operator will not collect pod logs
	// in the target namespace and send the resulting log records to Dash0.
	//
	// This setting is optional, it defaults to `true`, that is, if this setting is omitted, the value `true` is assumed
	// and the operator will collect pod logs in the target namespace; unless there is an operator configuration
	// resource with `telemetryCollection.enabled=false`, then log collection is off by default. It is a validation error
	// to set `telemetryCollection.enabled=false` in the operator configuration resource and
	// `logCollection.enabled=true` in any monitoring resource at the same time.
	//
	// +kubebuilder:validation:Optional
	Enabled *bool `json:"enabled"`
}

type PrometheusScraping struct {
	// If enabled, the operator will configure its OpenTelemetry collector to scrape metrics from pods in the namespace
	// of this Dash0Monitoring resource according to their prometheus.io/scrape annotations via the OpenTelemetry
	// Prometheus receiver. This setting is optional, it defaults to `true`; unless there is an operator configuration
	// resource with `telemetryCollection.enabled=false`, then Prometheus scraping is off by default. It is a validation
	// error to set `telemetryCollection.enabled=false` in the operator configuration resource and
	// `prometheusScraping.enabled=true` in any monitoring resource at the same time.
	//
	// +kubebuilder:validation:Optional
	Enabled *bool `json:"enabled"`
}

// FilterTransformErrorMode determine how the filter or the transform processor reacts to errors that occur while
// processing a condition.
//
// +kubebuilder:validation:Enum=ignore;silent;propagate
type FilterTransformErrorMode string

const (
	// FilterTransformErrorModeIgnore ignores errors returned by conditions, logs them, and continues with the next
	// condition or statement.
	FilterTransformErrorModeIgnore FilterTransformErrorMode = "ignore"

	// FilterTransformErrorModeSilent ignores errors returned by conditions, does not log them, and continues with the
	// next condition or statement.
	FilterTransformErrorModeSilent FilterTransformErrorMode = "silent"

	// FilterTransformErrorModePropagate return the error up the processing pipeline. This will result in the payload
	// being dropped.
	FilterTransformErrorModePropagate FilterTransformErrorMode = "propagate"
)

type Filter struct {
	// An optional field which will determine how the filter processor reacts to errors that occur while processing a
	// condition. Possible values:
	// - ignore: ignore errors returned by conditions, log them, continue with to the next condition.
	//           This is the recommended mode and also the default mode if this property is omitted.
	// - silent: ignore errors returned by conditions, do not log them, continue with to the next condition.
	// - propagate: return the error up the processing pipeline. This will result in the payload being dropped from the
	//           collector. Not recommended.
	//
	// This is optional, default to "ignore", there is usually no reason to change this.
	//
	// Note that although this can be specified per namespace, the filter conditions will be aggregated into one
	// single filter processor in the resulting OpenTelemetry collector configuration; if different error modes are
	// specified in different namespaces, the "most severe" error mode will be used (propagate > ignore > silent).
	//
	// +kubebuilder:default=ignore
	ErrorMode FilterTransformErrorMode `json:"error_mode,omitempty"`

	// Filters for the traces signal.
	// This can be used to drop _spans_ or _span events_.
	//
	// +kubebuilder:validation:Optional
	Traces *TraceFilter `json:"traces,omitempty"`

	// Filters for the metrics signal.
	// This can be used to drop entire _metrics_, or individual _data points_.
	//
	// +kubebuilder:validation:Optional
	Metrics *MetricFilter `json:"metrics,omitempty"`

	// Filters for the logs signal.
	// This can be used to drop _log records_.
	//
	// +kubebuilder:validation:Optional
	Logs *LogFilter `json:"logs,omitempty"`
}

func (f *Filter) HasAnyFilters() bool {
	if f.Traces != nil && f.Traces.HasAnyFilters() {
		return true
	}
	if f.Metrics != nil && f.Metrics.HasAnyFilters() {
		return true
	}
	if f.Logs != nil && f.Logs.HasAnyFilters() {
		return true
	}
	return false
}

type TraceFilter struct {
	// A list of conditions for filtering spans.
	// This is a list of OTTL conditions.
	// All spans where at least one condition evaluates to true will be dropped.
	// (That is, the conditions are implicitly connected by a logical OR.)
	// Example:
	// - 'attributes["http.route"] == "/ready"'
	// - 'attributes["http.route"] == "/metrics"'
	//
	// +kubebuilder:validation:Optional
	SpanFilter []string `json:"span,omitempty"`

	// A list of conditions for filtering span events.
	// This is a list of OTTL conditions.
	// All span events where at least one condition evaluates to true will be dropped.
	// (That is, the conditions are implicitly connected by a logical OR.)
	// If all span events for a span are dropped, the span will be left intact.
	//
	// +kubebuilder:validation:Optional
	SpanEventFilter []string `json:"spanevent,omitempty"`
}

func (f *TraceFilter) HasAnyFilters() bool {
	return len(f.SpanFilter) > 0 || len(f.SpanEventFilter) > 0
}

type MetricFilter struct {
	// A list of conditions for filtering metrics.
	// This is a list of OTTL conditions.
	// All metrics where at least one condition evaluates to true will be dropped.
	// (That is, the conditions are implicitly connected by a logical OR.)
	// Example:
	// - 'name == "k8s.replicaset.available"'
	// - 'name == "k8s.replicaset.desired"'
	// - 'type == METRIC_DATA_TYPE_HISTOGRAM'
	//
	// +kubebuilder:validation:Optional
	MetricFilter []string `json:"metric,omitempty"`

	// A list of conditions for filtering metrics data points.
	// This is a list of OTTL conditions.
	// All data points where at least one condition evaluates to true will be dropped.
	// (That is, the conditions are implicitly connected by a logical OR.)
	// Note: If all datapoints for a metric are dropped, the metric will also be dropped.
	// Example:
	// - 'metric.name == "a.noisy.metric.with.many.datapoints" and value_int == 0' # filter metrics by value
	// - 'resource.attributes["service.name"] == "my_service_name"' # filter data points by resource attributes
	//
	// +kubebuilder:validation:Optional
	DataPointFilter []string `json:"datapoint,omitempty"`
}

func (f *MetricFilter) HasAnyFilters() bool {
	return len(f.MetricFilter) > 0 || len(f.DataPointFilter) > 0
}

type LogFilter struct {
	// A list of conditions for filtering log records.
	// This is a list of OTTL conditions.
	// All log records where at least one condition evaluates to true will be dropped.
	// (That is, the conditions are implicitly connected by a logical OR.)
	// Example:
	// - 'IsMatch(body, ".*password.*")'
	// - 'severity_number < SEVERITY_NUMBER_WARN'
	//
	// +kubebuilder:validation:Optional
	LogRecordFilter []string `json:"log_records,omitempty"`
}

func (f *LogFilter) HasAnyFilters() bool {
	return len(f.LogRecordFilter) > 0
}

type Transform struct {
	// An optional field which will determine how the transform processor reacts to errors that occur while processing a
	// statement. Possible values:
	// - ignore: ignore errors returned by statements, log them, continue with to the next statement.
	// - silent: ignore errors returned by statements, do not log them, continue with to the next statement.
	// - propagate: return the error up the processing pipeline. This will result in the payload being dropped from the
	//   collector.
	//
	// This is optional, default to "ignore".
	//
	// Note that although this can be specified per namespace, the transform statements will be aggregated into one
	// single transform processor in the resulting OpenTelemetry collector configuration; if different error modes are
	// specified in different namespaces, the "most severe" error mode will be used (propagate > ignore > silent).
	//
	// +kubebuilder:default=ignore
	ErrorMode *FilterTransformErrorMode `json:"error_mode,omitempty"`

	// Transform statements (or groups) for the trace signal type.
	//
	// +kubebuilder:pruning:PreserveUnknownFields
	// +kubebuilder:validation:Schemaless
	Traces []json.RawMessage `json:"trace_statements,omitempty"`

	// Transform statements (or groups) for the metric signal type.
	//
	// +kubebuilder:pruning:PreserveUnknownFields
	// +kubebuilder:validation:Schemaless
	Metrics []json.RawMessage `json:"metric_statements,omitempty"`

	// Transform statements (or groups) for the log signal type.
	//
	// +kubebuilder:pruning:PreserveUnknownFields
	// +kubebuilder:validation:Schemaless
	Logs []json.RawMessage `json:"log_statements,omitempty"`
}

type NormalizedTransformSpec struct {
	ErrorMode *FilterTransformErrorMode  `json:"error_mode,omitempty"`
	Traces    []NormalizedTransformGroup `json:"trace_statements,omitempty"`
	Metrics   []NormalizedTransformGroup `json:"metric_statements,omitempty"`
	Logs      []NormalizedTransformGroup `json:"log_statements,omitempty"`
}

func (t *NormalizedTransformSpec) HasAnyStatements() bool {
	if len(t.Traces) > 0 {
		return true
	}
	if len(t.Metrics) > 0 {
		return true
	}
	if len(t.Logs) > 0 {
		return true
	}
	return false
}

type NormalizedTransformGroup struct {
	Context    *string                   `json:"context,omitempty"`
	ErrorMode  *FilterTransformErrorMode `json:"error_mode,omitempty"`
	Conditions []string                  `json:"conditions,omitempty"`
	Statements []string                  `json:"statements,omitempty"`
}

// Dash0ApiResourceSynchronizationStatus describes the result of synchronizing a (non-third-party) Kubernetes resource
// (e.g. synthetic checks) to the Dash0 API.
//
// +kubebuilder:validation:Enum=successful;failed
type Dash0ApiResourceSynchronizationStatus string

const (
	// Dash0ApiResourceSynchronizationStatusSuccessful means the last synchronization attempt has been successsful.
	Dash0ApiResourceSynchronizationStatusSuccessful Dash0ApiResourceSynchronizationStatus = "successful"

	// Dash0ApiResourceSynchronizationStatusFailed means the last synchronization attempt has failed.
	Dash0ApiResourceSynchronizationStatusFailed Dash0ApiResourceSynchronizationStatus = "failed"
)

// ThirdPartySynchronizationStatus describes the result of synchronizing a third-party Kubernetes resource (Perses
// dashboard, Prometheus rule) to the Dash0 API.
//
// +kubebuilder:validation:Enum=successful;partially-successful;failed
type ThirdPartySynchronizationStatus string

const (
	// ThirdPartySynchronizationStatusSuccessful means all items have been synchronized.
	ThirdPartySynchronizationStatusSuccessful ThirdPartySynchronizationStatus = "successful"

	// ThirdPartySynchronizationStatusPartiallySuccessful means some items have been synchronized and for some the synchronization has failed.
	ThirdPartySynchronizationStatusPartiallySuccessful ThirdPartySynchronizationStatus = "partially-successful"

	// ThirdPartySynchronizationStatusFailed means synchronization has failed for all items.
	ThirdPartySynchronizationStatusFailed ThirdPartySynchronizationStatus = "failed"
)

type PersesDashboardSynchronizationResults struct {
	SynchronizationStatus ThirdPartySynchronizationStatus `json:"synchronizationStatus"`
	SynchronizedAt        metav1.Time                     `json:"synchronizedAt"`
	SynchronizationError  string                          `json:"synchronizationError,omitempty"`
	ValidationIssues      []string                        `json:"validationIssues,omitempty"`
}

type PrometheusRuleSynchronizationResult struct {
	SynchronizationStatus      ThirdPartySynchronizationStatus `json:"synchronizationStatus"`
	SynchronizedAt             metav1.Time                     `json:"synchronizedAt"`
	AlertingRulesTotal         int                             `json:"alertingRulesTotal"`
	SynchronizedRulesTotal     int                             `json:"synchronizedRulesTotal"`
	SynchronizedRules          []string                        `json:"synchronizedRules,omitempty"`
	SynchronizationErrorsTotal int                             `json:"synchronizationErrorsTotal"`
	SynchronizationErrors      map[string]string               `json:"synchronizationErrors,omitempty"`
	InvalidRulesTotal          int                             `json:"invalidRulesTotal"`
	InvalidRules               map[string][]string             `json:"invalidRules,omitempty"`
}
