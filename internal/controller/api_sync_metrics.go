// SPDX-FileCopyrightText: Copyright 2026 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package controller

import (
	"context"
	"fmt"
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

	apiSyncSynchronizationsMetricSuffix        = "api_sync.synchronizations"
	apiSyncUnsynchronizedResourcesMetricSuffix = "api_sync.unsynchronized_resources"
	apiSyncActionUpsert                        = "upsert"
	apiSyncActionDelete                        = "delete"
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

type apiSyncFailure struct {
	dataset        string
	status         dash0common.Dash0ApiResourceSynchronizationStatus
	httpStatusCode int
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

func (r *apiSyncFailureRegistry) deleteMatching(matches func(apiSyncResourceKey) bool) {
	r.mutex.Lock()
	defer r.mutex.Unlock()
	for key := range r.failures {
		if matches(key) {
			delete(r.failures, key)
		}
	}
}

func (r *apiSyncFailureRegistry) deleteNamespace(namespace string) {
	r.deleteMatching(func(key apiSyncResourceKey) bool {
		return key.namespace == namespace
	})
}

func (r *apiSyncFailureRegistry) deleteKind(kind string) {
	r.deleteMatching(func(key apiSyncResourceKey) bool {
		return key.kind == kind
	})
}

func (r *apiSyncFailureRegistry) deleteKindInNamespace(kind string, namespace string) {
	r.deleteMatching(func(key apiSyncResourceKey) bool {
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
				attribute.String(apiSyncAttributeDataset, failure.dataset),
				attribute.String(apiSyncAttributeStatus, string(failure.status)),
			}
			if failure.httpStatusCode != 0 {
				attributes = append(attributes, attribute.Int(apiSyncAttributeHttpStatusCode, failure.httpStatusCode))
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
	if apiSyncSynchronizationsMetric != nil {
		apiSyncSynchronizationsMetric.Add(
			ctx,
			1,
			otelmetric.WithAttributes(
				attribute.String(apiSyncAttributeResourceKind, key.kind),
				attribute.String(apiSyncAttributeResourceNamespace, key.namespace),
				attribute.String(apiSyncAttributeDataset, apiConfig.Dataset),
				attribute.String(apiSyncAttributeAction, apiSyncActionLabel(action)),
				attribute.String(apiSyncAttributeStatus, string(status)),
			),
		)
	}
	if status == dash0common.Dash0ApiResourceSynchronizationStatusSuccessful {
		return nil
	}
	return &apiSyncFailure{
		dataset:        apiConfig.Dataset,
		status:         status,
		httpStatusCode: highestHttpStatusCode(result),
	}
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
