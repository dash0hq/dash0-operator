// SPDX-FileCopyrightText: Copyright 2026 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package controller

import (
	"context"
	"fmt"
	"slices"
	"sync"

	"go.opentelemetry.io/otel/attribute"
	otelmetric "go.opentelemetry.io/otel/metric"

	dash0common "github.com/dash0hq/dash0-operator/api/operator/common"
	"github.com/dash0hq/dash0-operator/internal/util/logd"
)

const (
	apiSyncAttributeResourceKind      = "dash0.operator.sync.resource.kind"
	apiSyncAttributeResourceNamespace = "dash0.operator.sync.resource.namespace"
	apiSyncAttributeResourceName      = "dash0.operator.sync.resource.name"
	apiSyncAttributeDataset           = "dash0.operator.sync.dataset"
	apiSyncAttributeAction            = "dash0.operator.sync.action"
	apiSyncAttributeStatus            = "dash0.operator.sync.status"
	apiSyncAttributeHttpStatusCode    = "http.response.status_code"
	apiSyncAttributeErrorType         = "error.type"

	apiSyncSynchronizationsMetricSuffix        = "api_sync.synchronizations"
	apiSyncUnsynchronizedResourcesMetricSuffix = "api_sync.unsynchronized_resources"
	apiSyncActionUpsert                        = "upsert"
	apiSyncActionDelete                        = "delete"
)

// apiSyncPreconditionFailure is the reason why a resource that is subject to synchronization cannot be synchronized
// before any request is sent to the Dash0 API. A precondition that is not met because synchronization has not been
// configured (no Dash0 monitoring resource, no API endpoint, synchronization disabled) is not a failure.
type apiSyncPreconditionFailure string

const (
	apiSyncPreconditionFailureMonitoringResourceLookup apiSyncPreconditionFailure = "monitoring_resource_lookup_failed"
	apiSyncPreconditionFailureApiConfigUnavailable     apiSyncPreconditionFailure = "api_config_unavailable"
)

var (
	apiSyncSynchronizationsMetric otelmetric.Int64Counter
	apiSyncFailures               = newApiSyncFailureRegistry()
)

// ApiSyncMetrics provides the self-monitoring metrics that are shared by all reconcilers synchronizing Kubernetes
// resources to the Dash0 API:
//   - a counter for synchronization attempts, per resource kind, namespace, dataset, action and status, and
//   - a gauge reporting each resource whose most recent synchronization was not successful, with the resource kind,
//     namespace and name as attributes. In contrast to the counter, the gauge reflects the current state: a resource
//     stays reported until it is synchronized successfully, deleted, or no longer subject to synchronization.
type ApiSyncMetrics struct{}

// InitializeSelfMonitoringMetrics creates the API sync instruments with the given meter.
func (m *ApiSyncMetrics) InitializeSelfMonitoringMetrics(
	meter otelmetric.Meter,
	metricNamePrefix string,
	logger logd.Logger,
) {
	synchronizationsMetricName := metricNamePrefix + apiSyncSynchronizationsMetricSuffix
	var err error
	if apiSyncSynchronizationsMetric, err = meter.Int64Counter(
		synchronizationsMetricName,
		otelmetric.WithUnit("1"),
		otelmetric.WithDescription("Counter for synchronization attempts of Kubernetes resources to the Dash0 API"),
	); err != nil {
		logger.Error(err, fmt.Sprintf("Cannot initialize the metric %s.", synchronizationsMetricName))
	}

	unsynchronizedResourcesMetricName := metricNamePrefix + apiSyncUnsynchronizedResourcesMetricSuffix
	if _, err = meter.Int64ObservableGauge(
		unsynchronizedResourcesMetricName,
		otelmetric.WithUnit("1"),
		otelmetric.WithDescription(
			"Kubernetes resources whose most recent synchronization to the Dash0 API was not successful",
		),
		otelmetric.WithInt64Callback(apiSyncFailures.observe),
	); err != nil {
		logger.Error(err, fmt.Sprintf("Cannot initialize the metric %s.", unsynchronizedResourcesMetricName))
	}
}

type apiSyncResourceKey struct {
	kind      string
	namespace string
	name      string
}

func newApiSyncResourceKey(apiSyncReconciler ApiSyncReconciler, namespace string, name string) apiSyncResourceKey {
	return apiSyncResourceKey{
		kind:      apiSyncReconciler.Kind(),
		namespace: namespace,
		name:      name,
	}
}

type apiSyncFailure struct {
	dataset             string
	status              dash0common.Dash0ApiResourceSynchronizationStatus
	httpStatusCode      int
	preconditionFailure apiSyncPreconditionFailure
}

// apiSyncFailureRegistry holds the resources whose most recent synchronization was not successful, with one entry per
// API config that the synchronization failed for.
type apiSyncFailureRegistry struct {
	mutex    sync.Mutex
	failures map[apiSyncResourceKey][]apiSyncFailure
}

func newApiSyncFailureRegistry() *apiSyncFailureRegistry {
	return &apiSyncFailureRegistry{
		failures: make(map[apiSyncResourceKey][]apiSyncFailure),
	}
}

func (r *apiSyncFailureRegistry) set(key apiSyncResourceKey, failures []apiSyncFailure) {
	r.mutex.Lock()
	defer r.mutex.Unlock()
	if len(failures) == 0 {
		delete(r.failures, key)
		return
	}
	r.failures[key] = failures
}

func (r *apiSyncFailureRegistry) delete(key apiSyncResourceKey) {
	r.mutex.Lock()
	defer r.mutex.Unlock()
	delete(r.failures, key)
}

func (r *apiSyncFailureRegistry) deleteMatching(matches func(apiSyncResourceKey, []apiSyncFailure) bool) {
	r.mutex.Lock()
	defer r.mutex.Unlock()
	for key, failures := range r.failures {
		if matches(key, failures) {
			delete(r.failures, key)
		}
	}
}

func (r *apiSyncFailureRegistry) deleteNamespace(namespace string) {
	r.deleteMatching(func(key apiSyncResourceKey, _ []apiSyncFailure) bool {
		return key.namespace == namespace
	})
}

// deletePreconditionFailuresInNamespace removes the resources in the given namespace that are reported because of the
// given precondition failure.
func (r *apiSyncFailureRegistry) deletePreconditionFailuresInNamespace(
	namespace string,
	preconditionFailure apiSyncPreconditionFailure,
) {
	r.deleteMatching(func(key apiSyncResourceKey, failures []apiSyncFailure) bool {
		return key.namespace == namespace && slices.ContainsFunc(failures, func(failure apiSyncFailure) bool {
			return failure.preconditionFailure == preconditionFailure
		})
	})
}

func (r *apiSyncFailureRegistry) deleteKind(kind string) {
	r.deleteMatching(func(key apiSyncResourceKey, _ []apiSyncFailure) bool {
		return key.kind == kind
	})
}

func (r *apiSyncFailureRegistry) deleteKindInNamespace(kind string, namespace string) {
	r.deleteMatching(func(key apiSyncResourceKey, _ []apiSyncFailure) bool {
		return key.kind == kind && key.namespace == namespace
	})
}

func (r *apiSyncFailureRegistry) clear() {
	r.mutex.Lock()
	defer r.mutex.Unlock()
	clear(r.failures)
}

func (r *apiSyncFailureRegistry) observe(_ context.Context, observer otelmetric.Int64Observer) error {
	r.mutex.Lock()
	defer r.mutex.Unlock()
	for key, failures := range r.failures {
		for _, failure := range failures {
			attributes := []attribute.KeyValue{
				attribute.String(apiSyncAttributeResourceKind, key.kind),
				attribute.String(apiSyncAttributeResourceNamespace, key.namespace),
				attribute.String(apiSyncAttributeResourceName, key.name),
				attribute.String(apiSyncAttributeStatus, string(failure.status)),
			}
			if failure.dataset != "" {
				attributes = append(attributes, attribute.String(apiSyncAttributeDataset, failure.dataset))
			}
			if failure.httpStatusCode != 0 {
				attributes = append(attributes, attribute.Int(apiSyncAttributeHttpStatusCode, failure.httpStatusCode))
			}
			if failure.preconditionFailure != "" {
				attributes = append(
					attributes, attribute.String(apiSyncAttributeErrorType, string(failure.preconditionFailure)),
				)
			}
			observer.Observe(1, otelmetric.WithAttributes(attributes...))
		}
	}
	return nil
}

// apiSyncStatusForResult determines the synchronization status for the result of synchronizing one resource with one
// API config.
func apiSyncStatusForResult(
	result *ResourceToRequestsResult,
	successfullySynchronized int,
) dash0common.Dash0ApiResourceSynchronizationStatus {
	if result.HasNoErrorsAndNoIssues() {
		return dash0common.Dash0ApiResourceSynchronizationStatusSuccessful
	}
	if successfullySynchronized > 0 {
		return dash0common.Dash0ApiResourceSynchronizationStatusPartiallySuccessful
	}
	return dash0common.Dash0ApiResourceSynchronizationStatusFailed
}

// highestHttpStatusCode returns the highest HTTP status code of all synchronization errors in the result, or 0 if none
// of the errors carries an HTTP status code.
func highestHttpStatusCode(result *ResourceToRequestsResult) int {
	highest := 0
	for _, statusCode := range result.SynchronizationErrorStatusCodes {
		highest = max(highest, statusCode)
	}
	return highest
}

func apiSyncActionLabel(action apiAction) string {
	if action == deleteAction {
		return apiSyncActionDelete
	}
	return apiSyncActionUpsert
}

// recordApiSyncResult records the result of synchronizing one resource with one API config in the synchronizations
// counter, and returns the failure to report in the unsynchronized resources gauge, or nil if the synchronization was
// successful.
func recordApiSyncResult(
	ctx context.Context,
	key apiSyncResourceKey,
	apiConfig ApiConfig,
	action apiAction,
	result *ResourceToRequestsResult,
	successfullySynchronized int,
) *apiSyncFailure {
	if result.IsNoOp() {
		return nil
	}
	status := apiSyncStatusForResult(result, successfullySynchronized)
	countApiSyncSynchronization(ctx, key, apiConfig.Dataset, action, status, "")
	if status == dash0common.Dash0ApiResourceSynchronizationStatusSuccessful {
		return nil
	}
	return &apiSyncFailure{
		dataset:        apiConfig.Dataset,
		status:         status,
		httpStatusCode: highestHttpStatusCode(result),
	}
}

// recordApiSyncPreconditionFailure records a synchronization that failed before any request was sent to the Dash0 API
// in the synchronizations counter, and reports the resource in the unsynchronized resources gauge, replacing the
// failures of its previous synchronization.
func recordApiSyncPreconditionFailure(
	ctx context.Context,
	key apiSyncResourceKey,
	action apiAction,
	preconditionFailure apiSyncPreconditionFailure,
) {
	status := dash0common.Dash0ApiResourceSynchronizationStatusFailed
	countApiSyncSynchronization(ctx, key, "", action, status, preconditionFailure)
	apiSyncFailures.set(key, []apiSyncFailure{{status: status, preconditionFailure: preconditionFailure}})
}

func countApiSyncSynchronization(
	ctx context.Context,
	key apiSyncResourceKey,
	dataset string,
	action apiAction,
	status dash0common.Dash0ApiResourceSynchronizationStatus,
	preconditionFailure apiSyncPreconditionFailure,
) {
	if apiSyncSynchronizationsMetric == nil {
		return
	}
	attributes := []attribute.KeyValue{
		attribute.String(apiSyncAttributeResourceKind, key.kind),
		attribute.String(apiSyncAttributeResourceNamespace, key.namespace),
		attribute.String(apiSyncAttributeAction, apiSyncActionLabel(action)),
		attribute.String(apiSyncAttributeStatus, string(status)),
	}
	if dataset != "" {
		attributes = append(attributes, attribute.String(apiSyncAttributeDataset, dataset))
	}
	if preconditionFailure != "" {
		attributes = append(attributes, attribute.String(apiSyncAttributeErrorType, string(preconditionFailure)))
	}
	apiSyncSynchronizationsMetric.Add(ctx, 1, otelmetric.WithAttributes(attributes...))
}

// apiSyncLogAttributes returns structured log attributes describing the result of synchronizing one resource with one
// API config.
func apiSyncLogAttributes(
	key apiSyncResourceKey,
	apiConfig ApiConfig,
	action apiAction,
	result *ResourceToRequestsResult,
	successfullySynchronized int,
) []any {
	attributes := []any{
		apiSyncAttributeResourceKind, key.kind,
		apiSyncAttributeResourceNamespace, key.namespace,
		apiSyncAttributeResourceName, key.name,
		apiSyncAttributeDataset, apiConfig.Dataset,
		apiSyncAttributeAction, apiSyncActionLabel(action),
		apiSyncAttributeStatus, string(apiSyncStatusForResult(result, successfullySynchronized)),
	}
	if statusCode := highestHttpStatusCode(result); statusCode != 0 {
		attributes = append(attributes, apiSyncAttributeHttpStatusCode, statusCode)
	}
	return attributes
}

// apiSyncPreconditionFailureLogAttributes returns structured log attributes describing a synchronization that failed
// before any request was sent to the Dash0 API.
func apiSyncPreconditionFailureLogAttributes(
	key apiSyncResourceKey,
	preconditionFailure apiSyncPreconditionFailure,
) []any {
	return []any{
		apiSyncAttributeResourceKind, key.kind,
		apiSyncAttributeResourceNamespace, key.namespace,
		apiSyncAttributeResourceName, key.name,
		apiSyncAttributeStatus, string(dash0common.Dash0ApiResourceSynchronizationStatusFailed),
		apiSyncAttributeErrorType, string(preconditionFailure),
	}
}
