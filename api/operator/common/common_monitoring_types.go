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
// +kubebuilder:validation:Enum=all;created-and-updated;pods;none
type InstrumentWorkloadsMode string

const (
	// InstrumentWorkloadsModeAll allows instrumenting existing as well as new and updated workloads.
	InstrumentWorkloadsModeAll InstrumentWorkloadsMode = "all"

	// InstrumentWorkloadsModeCreatedAndUpdated disables instrumenting existing workloads, but new and updated workloads
	// will be instrumented.
	InstrumentWorkloadsModeCreatedAndUpdated InstrumentWorkloadsMode = "created-and-updated"

	// InstrumentWorkloadsModePods instruments pods when they are created, instead of modifying the pod templates of
	// workloads. Existing pods are neither instrumented nor uninstrumented, they keep their state until they are
	// recreated.
	InstrumentWorkloadsModePods InstrumentWorkloadsMode = "pods"

	// InstrumentWorkloadsModeNone will disable instrumentation of workloads entirely for a namespace.
	InstrumentWorkloadsModeNone InstrumentWorkloadsMode = "none"
)

var AllInstrumentWorkloadsModes = []InstrumentWorkloadsMode{
	InstrumentWorkloadsModeAll,
	InstrumentWorkloadsModeCreatedAndUpdated,
	InstrumentWorkloadsModePods,
	InstrumentWorkloadsModeNone,
}

type LogCollection struct {
	// Opt-out for collecting pod logs in the target namespace. Defaults to `true`, or to `false` if the operator
	// configuration resource has `telemetryCollection.enabled=false`, in which case setting it to `true` is a
	// validation error.
	//
	// +kubebuilder:validation:Optional
	Enabled *bool `json:"enabled,omitempty"`
}

type EventCollection struct {
	// Opt-out for collecting Kubernetes events in the target namespace. Defaults to `true`, or to `false` if the
	// operator configuration resource has `telemetryCollection.enabled=false`, in which case setting it to `true` is a
	// validation error.
	//
	// +kubebuilder:validation:Optional
	Enabled *bool `json:"enabled,omitempty"`
}

type KubernetesObjectCollection struct {
	// Opt-in for sending the manifests of pods, deployments, replica sets, stateful sets and daemon sets in the target
	// namespace to Dash0 as log records. Defaults to `false`. Values of environment variables and the annotations in
	// which deploy tools (kubectl, kapp, Rancher) store a copy of the whole manifest are removed before the manifests
	// leave the cluster.
	//
	// +kubebuilder:validation:Optional
	Enabled *bool `json:"enabled,omitempty"`
}

type PrometheusScraping struct {
	// Whether to scrape metrics from pods in the target namespace according to their prometheus.io/scrape annotations.
	// Defaults to `true`, or to `false` if the operator configuration resource has `telemetryCollection.enabled=false`,
	// in which case setting it to `true` is a validation error.
	//
	// +kubebuilder:validation:Optional
	Enabled *bool `json:"enabled,omitempty"`
}

// FilterTransformErrorMode determines how the filter or the transform processor reacts to errors.
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
	// How the filter processor reacts to errors returned by conditions:
	// - ignore (default, recommended): log the error and continue with the next condition.
	// - silent: continue with the next condition without logging the error.
	// - propagate: return the error up the pipeline, which drops the payload. Not recommended.
	// All filters of the cluster end up in a single filter processor, which uses the most severe of the configured error
	// modes (propagate > ignore > silent).
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

	// Filters for the profiles signal.
	// This can be used to drop _profiles_.
	//
	// +kubebuilder:validation:Optional
	Profiles *ProfileFilter `json:"profiles,omitempty"`
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
	if f.Profiles != nil && f.Profiles.HasAnyFilters() {
		return true
	}
	return false
}

type TraceFilter struct {
	// OTTL conditions for dropping spans. A span is dropped if at least one condition is true. Example:
	// - 'attributes["http.route"] == "/ready"'
	// - 'attributes["http.route"] == "/metrics"'
	//
	// +kubebuilder:validation:Optional
	SpanFilter []string `json:"span,omitempty"`

	// OTTL conditions for dropping span events. A span event is dropped if at least one condition is true. The span
	// itself is kept, even if all of its span events are dropped.
	//
	// +kubebuilder:validation:Optional
	SpanEventFilter []string `json:"spanevent,omitempty"`
}

func (f *TraceFilter) HasAnyFilters() bool {
	return len(f.SpanFilter) > 0 || len(f.SpanEventFilter) > 0
}

type MetricFilter struct {
	// OTTL conditions for dropping metrics. A metric is dropped if at least one condition is true. Example:
	// - 'name == "k8s.replicaset.available"'
	// - 'type == METRIC_DATA_TYPE_HISTOGRAM'
	//
	// +kubebuilder:validation:Optional
	MetricFilter []string `json:"metric,omitempty"`

	// OTTL conditions for dropping metric data points. A data point is dropped if at least one condition is true. A
	// metric whose data points are all dropped is dropped as well. Example:
	// - 'metric.name == "a.noisy.metric" and value_int == 0'
	// - 'resource.attributes["service.name"] == "my_service_name"'
	//
	// +kubebuilder:validation:Optional
	DataPointFilter []string `json:"datapoint,omitempty"`
}

func (f *MetricFilter) HasAnyFilters() bool {
	return len(f.MetricFilter) > 0 || len(f.DataPointFilter) > 0
}

type LogFilter struct {
	// OTTL conditions for dropping log records. A log record is dropped if at least one condition is true. Example:
	// - 'IsMatch(body, ".*password.*")'
	// - 'severity_number < SEVERITY_NUMBER_WARN'
	//
	// +kubebuilder:validation:Optional
	LogRecordFilter []string `json:"log_records,omitempty"`
}

func (f *LogFilter) HasAnyFilters() bool {
	return len(f.LogRecordFilter) > 0
}

type ProfileFilter struct {
	// OTTL conditions for dropping profiles. A profile is dropped if at least one condition is true.
	//
	// +kubebuilder:validation:Optional
	ProfileFilter []string `json:"profile,omitempty"`
}

func (f *ProfileFilter) HasAnyFilters() bool {
	return len(f.ProfileFilter) > 0
}

type Transform struct {
	// How the transform processor reacts to errors returned by statements:
	// - ignore (default): log the error and continue with the next statement.
	// - silent: continue with the next statement without logging the error.
	// - propagate: return the error up the pipeline, which drops the payload.
	// All transforms of the cluster end up in a single transform processor, which uses the most severe of the
	// configured error modes (propagate > ignore > silent).
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

	// Transform statements (or groups) for the profile signal type.
	//
	// +kubebuilder:pruning:PreserveUnknownFields
	// +kubebuilder:validation:Schemaless
	Profiles []json.RawMessage `json:"profile_statements,omitempty"`
}

type NormalizedTransformSpec struct {
	ErrorMode *FilterTransformErrorMode  `json:"error_mode,omitempty"`
	Traces    []NormalizedTransformGroup `json:"trace_statements,omitempty"`
	Metrics   []NormalizedTransformGroup `json:"metric_statements,omitempty"`
	Logs      []NormalizedTransformGroup `json:"log_statements,omitempty"`
	Profiles  []NormalizedTransformGroup `json:"profile_statements,omitempty"`
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
	if len(t.Profiles) > 0 {
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
// +kubebuilder:validation:Enum=successful;partially-successful;failed
type Dash0ApiResourceSynchronizationStatus string

const (
	// Dash0ApiResourceSynchronizationStatusSuccessful means the resource was synced successfully to all provided endpoints/datasets.
	Dash0ApiResourceSynchronizationStatusSuccessful Dash0ApiResourceSynchronizationStatus = "successful"

	// Dash0ApiResourceSynchronizationStatusPartiallySuccessful means the sync has succeeded for some endpoints/datasets and failed for others.
	Dash0ApiResourceSynchronizationStatusPartiallySuccessful Dash0ApiResourceSynchronizationStatus = "partially-successful"

	// Dash0ApiResourceSynchronizationStatusFailed means the resource could not be synced to any endpoints/datasets.
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
	// +kubebuilder:validation:Optional
	SynchronizationStatus ThirdPartySynchronizationStatus `json:"synchronizationStatus"`
	// +kubebuilder:validation:Optional
	SynchronizedAt metav1.Time `json:"synchronizedAt"`
	// +kubebuilder:validation:Optional
	ValidationIssues []string `json:"validationIssues,omitempty"`
	// +kubebuilder:validation:Optional`
	SynchronizationResults []PersesDashboardSynchronizationResultPerEndpointAndDataset `json:"synchronizationResults,omitempty"`
}

type PersesDashboardSynchronizationResultPerEndpointAndDataset struct {
	Dash0ApiEndpoint string `json:"dash0ApiEndpoint"`
	// +kubebuilder:validation:Optional
	Dash0Dataset string `json:"dash0Dataset,omitempty"`
	// +kubebuilder:validation:Optional
	Dash0Origin string `json:"dash0Origin,omitempty"`
	// +kubebuilder:validation:Optional
	SynchronizationError string `json:"synchronizationError,omitempty"`
	// HttpStatusCode is the HTTP status code that the Dash0 API returned for the failed synchronization attempt, if the
	// failure was caused by an unexpected HTTP response. It is 0 (absent) for successful synchronizations and for
	// transport-level errors (network errors, timeouts) where no HTTP response was received. It is used to decide
	// whether a failed synchronization should be retried.
	// +kubebuilder:validation:Optional
	HttpStatusCode int `json:"httpStatusCode,omitempty"`
}

type PrometheusRuleSynchronizationResult struct {
	// +kubebuilder:validation:Optional
	SynchronizationStatus ThirdPartySynchronizationStatus `json:"synchronizationStatus"`
	// +kubebuilder:validation:Optional
	SynchronizedAt metav1.Time `json:"synchronizedAt"`
	// +kubebuilder:validation:Optional
	AlertingRulesTotal int `json:"alertingRulesTotal"`
	// +kubebuilder:validation:Optional
	RecordingRulesTotal int `json:"recordingRulesTotal"`
	// +kubebuilder:validation:Optional
	InvalidRulesTotal int `json:"invalidRulesTotal"`
	// +kubebuilder:validation:Optional
	InvalidRules map[string][]string `json:"invalidRules,omitempty"`
	// +kubebuilder:validation:Optional
	SynchronizationResults []PrometheusRuleSynchronizationResultPerEndpointAndDataset `json:"synchronizationResults,omitempty"`
}

type PrometheusRuleSynchronizationResultPerEndpointAndDataset struct {
	Dash0ApiEndpoint string `json:"dash0ApiEndpoint"`
	// +kubebuilder:validation:Optional
	Dash0Dataset string `json:"dash0Dataset,omitempty"`
	// +kubebuilder:validation:Optional
	SynchronizedRulesTotal int `json:"synchronizedRulesTotal"`
	// +kubebuilder:validation:Optional
	SynchronizedRulesAttributes map[string]PrometheusRuleSynchronizedRuleAttributes `json:"synchronizedRulesAttributes,omitempty"`
	// +kubebuilder:validation:Optional
	SynchronizationErrorsTotal int `json:"synchronizationErrorsTotal"`
	// +kubebuilder:validation:Optional
	SynchronizationErrors map[string]string `json:"synchronizationErrors,omitempty"`
	// SynchronizationErrorHttpStatusCodes maps the name of a rule that failed to synchronize to the HTTP status code
	// that the Dash0 API returned for that rule, if the failure was caused by an unexpected HTTP response. The status
	// code is 0 (absent) for transport-level errors (network errors, timeouts) where no HTTP response was received. It
	// is used to decide whether a failed synchronization should be retried. The keys match the keys of
	// SynchronizationErrors.
	// +kubebuilder:validation:Optional
	SynchronizationErrorHttpStatusCodes map[string]int `json:"synchronizationErrorHttpStatusCodes,omitempty"`
}

type PrometheusRuleSynchronizedRuleAttributes struct {
	// +kubebuilder:validation:Optional
	Dash0Origin string `json:"dash0Origin,omitempty"`
}
