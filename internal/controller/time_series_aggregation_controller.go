// SPDX-FileCopyrightText: Copyright 2026 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package controller

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"slices"
	"sync"
	"sync/atomic"
	"time"

	otelmetric "go.opentelemetry.io/otel/metric"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	dash0common "github.com/dash0hq/dash0-operator/api/operator/common"
	dash0v1alpha1 "github.com/dash0hq/dash0-operator/api/operator/v1alpha1"
	dash0v1beta1 "github.com/dash0hq/dash0-operator/api/operator/v1beta1"
	"github.com/dash0hq/dash0-operator/internal/selfmonitoringapiaccess"
	"github.com/dash0hq/dash0-operator/internal/util"
	"github.com/dash0hq/dash0-operator/internal/util/logd"
)

type TimeSeriesAggregationReconciler struct {
	client.Client
	pseudoClusterUid      types.UID
	leaderElectionAware   util.LeaderElectionAware
	httpClient            *http.Client
	defaultApiConfigs     selfmonitoringapiaccess.SynchronizedSlice[ApiConfig]
	namespacedApiConfigs  selfmonitoringapiaccess.SynchronizedMapSlice[ApiConfig]
	initialSyncMutex      sync.Mutex
	initialSyncInProgress atomic.Bool
	initialSyncHasHappend atomic.Bool
	namespacedSyncMutex   selfmonitoringapiaccess.NamespaceMutex
}

var (
	timeSeriesAggregationReconcileRequestMetric otelmetric.Int64Counter
)

func NewTimeSeriesAggregationReconciler(
	k8sClient client.Client,
	pseudoClusterUid types.UID,
	leaderElectionAware util.LeaderElectionAware,
	httpClient *http.Client,
) *TimeSeriesAggregationReconciler {
	return &TimeSeriesAggregationReconciler{
		Client:               k8sClient,
		pseudoClusterUid:     pseudoClusterUid,
		leaderElectionAware:  leaderElectionAware,
		httpClient:           httpClient,
		defaultApiConfigs:    *selfmonitoringapiaccess.NewSynchronizedSlice[ApiConfig](),
		namespacedApiConfigs: *selfmonitoringapiaccess.NewSynchronizedMapSlice[ApiConfig](),
		namespacedSyncMutex:  *selfmonitoringapiaccess.NewNamespaceMutex(),
	}
}

func (r *TimeSeriesAggregationReconciler) SetupWithManager(mgr manager.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&dash0v1alpha1.Dash0TimeSeriesAggregation{}).
		// ignore changes in the status subresource, but react on changes to spec, label and annotations
		WithEventFilter(generationOrLabelChangePredicate).
		Complete(r)
}

func (r *TimeSeriesAggregationReconciler) InitializeSelfMonitoringMetrics(
	meter otelmetric.Meter,
	metricNamePrefix string,
	logger logd.Logger,
) {
	reconcileRequestMetricName := fmt.Sprintf("%s%s", metricNamePrefix, "timeseriesaggregation.reconcile_requests")
	var err error
	if timeSeriesAggregationReconcileRequestMetric, err = meter.Int64Counter(
		reconcileRequestMetricName,
		otelmetric.WithUnit("1"),
		otelmetric.WithDescription("Counter for time series aggregation reconcile requests"),
	); err != nil {
		logger.Error(err, fmt.Sprintf("Cannot initialize the metric %s.", reconcileRequestMetricName))
	}
}

func (r *TimeSeriesAggregationReconciler) KindDisplayName() string {
	return "time series aggregation"
}

func (r *TimeSeriesAggregationReconciler) ShortName() string {
	return "time-series-aggregation"
}

func (r *TimeSeriesAggregationReconciler) GetDefaultApiConfigs() []ApiConfig {
	return r.defaultApiConfigs.Get()
}

func (r *TimeSeriesAggregationReconciler) GetNamespacedApiConfigs(namespace string) ([]ApiConfig, bool) {
	return r.namespacedApiConfigs.Get(namespace)
}

func (r *TimeSeriesAggregationReconciler) ControllerName() string {
	return "dash0_time_series_aggregation_controller"
}

func (r *TimeSeriesAggregationReconciler) K8sClient() client.Client {
	return r.Client
}

func (r *TimeSeriesAggregationReconciler) HttpClient() *http.Client {
	return r.httpClient
}

func (r *TimeSeriesAggregationReconciler) SetDefaultApiConfigs(
	ctx context.Context,
	apiConfigs []ApiConfig,
	logger logd.Logger,
) {
	r.defaultApiConfigs.Set(apiConfigs)
	r.maybeDoInitialSynchronizationOfAllResources(ctx, logger)
}

func (r *TimeSeriesAggregationReconciler) RemoveDefaultApiConfigs(_ context.Context, _ logd.Logger) {
	r.defaultApiConfigs.Clear()
}

func (r *TimeSeriesAggregationReconciler) SetNamespacedApiConfigs(
	ctx context.Context,
	namespace string,
	updatedApiConfigs []ApiConfig,
	logger logd.Logger,
) {
	if updatedApiConfigs != nil {
		previousApiConfigs, _ := r.namespacedApiConfigs.Get(namespace)

		r.namespacedApiConfigs.Set(namespace, updatedApiConfigs)

		if !slices.Equal(previousApiConfigs, updatedApiConfigs) {
			r.synchronizeNamespacedResources(ctx, namespace, logger)
		}
	}
}

func (r *TimeSeriesAggregationReconciler) RemoveNamespacedApiConfigs(
	ctx context.Context,
	namespace string,
	logger logd.Logger,
) {
	if _, exists := r.namespacedApiConfigs.Get(namespace); exists {
		r.namespacedApiConfigs.Delete(namespace)
		r.synchronizeNamespacedResources(ctx, namespace, logger)
	}
}

func (r *TimeSeriesAggregationReconciler) SetSynchronizationEnabled(
	_ context.Context,
	_ string,
	_ *dash0v1beta1.Dash0Monitoring,
	_ logd.Logger,
) {
	// no-op: time series aggregations do not have a per-namespace sync toggle
}

func (r *TimeSeriesAggregationReconciler) RemoveSynchronizationEnabled(_ string) {
	// no-op: time series aggregations do not have a per-namespace sync toggle
}

func (r *TimeSeriesAggregationReconciler) NotifyOperatorManagerJustBecameLeader(ctx context.Context, logger logd.Logger) {
	r.maybeDoInitialSynchronizationOfAllResources(ctx, logger)
}

func (r *TimeSeriesAggregationReconciler) maybeDoInitialSynchronizationOfAllResources(
	ctx context.Context,
	logger logd.Logger,
) {
	r.initialSyncMutex.Lock()
	defer r.initialSyncMutex.Unlock()

	if r.initialSyncHasHappend.Load() || r.initialSyncInProgress.Load() {
		return
	}

	if !r.leaderElectionAware.IsLeader() {
		logger.Info(
			fmt.Sprintf(
				"Waiting for this operator manager replica to become leader before running initial " +
					"synchronization of time series aggregations.",
			),
		)
		return
	}
	if len(filterValidApiConfigs(r.defaultApiConfigs.Get(), logger, "default operator configuration")) == 0 {
		logger.Info(
			"Waiting for the Dash0 API config before running initial synchronization of time series aggregations. " +
				"Either no Dash0 API config has been provided via the operator configuration resource, or the " +
				"operator configuration resource has not been reconciled yet. If there is an operator configuration " +
				"resource with an API endpoint and a Dash0 auth token or a secret ref present in the cluster, it will " +
				"be reconciled in a few seconds and this message can be safely ignored.",
		)
		return
	}

	logger.Info("Running initial synchronization of time series aggregations now.")
	r.initialSyncInProgress.Store(true)

	go func() {
		defer r.initialSyncInProgress.Store(false)

		allResources := dash0v1alpha1.Dash0TimeSeriesAggregationList{}
		if err := r.List(
			ctx,
			&allResources,
			&client.ListOptions{},
		); err != nil {
			logger.Error(err, "Failed to list all Dash0 time series aggregation resources.")
			return
		}

		for _, resource := range allResources.Items {
			pseudoReconcileRequest := ctrl.Request{
				NamespacedName: client.ObjectKey{
					Namespace: resource.Namespace,
					Name:      resource.Name,
				},
			}
			_, _ = r.Reconcile(ctx, pseudoReconcileRequest)
			// stagger API requests a bit
			time.Sleep(50 * time.Millisecond)
		}
		logger.Info("Initial synchronization of time series aggregations has finished.")
		r.initialSyncHasHappend.Store(true)
	}()
}

func (r *TimeSeriesAggregationReconciler) synchronizeNamespacedResources(
	ctx context.Context,
	namespace string,
	logger logd.Logger,
) {
	if !r.leaderElectionAware.IsLeader() {
		logger.Info(
			fmt.Sprintf(
				"Waiting for this operator manager replica to become leader before running " +
					"synchronization of time series aggregations.",
			),
		)
		return
	}

	// namespacedSyncMutex is used so we don't trigger multiple syncs in parallel in a single namespace.
	// That happens for example when the export from a monitoring resource is removed, since that updates both the API
	// config and auth token at almost the same time, triggering two resyncs.
	r.namespacedSyncMutex.Lock(namespace)

	logger.Info(fmt.Sprintf("Running synchronization of time series aggregations in namespace %s now.", namespace))

	go func() {
		defer r.namespacedSyncMutex.Unlock(namespace)

		allResources := dash0v1alpha1.Dash0TimeSeriesAggregationList{}
		if err := r.List(
			ctx,
			&allResources,
			&client.ListOptions{
				Namespace: namespace,
			},
		); err != nil {
			logger.Error(
				err,
				fmt.Sprintf("Failed to list Dash0 time series aggregation resources in namespace %s.", namespace),
			)
			return
		}

		for _, resource := range allResources.Items {
			pseudoReconcileRequest := ctrl.Request{
				NamespacedName: client.ObjectKey{
					Namespace: resource.Namespace,
					Name:      resource.Name,
				},
			}
			_, _ = r.Reconcile(ctx, pseudoReconcileRequest)
			// stagger API requests a bit
			time.Sleep(50 * time.Millisecond)
		}
		logger.Info(
			fmt.Sprintf("Synchronization of time series aggregations in namespace %s has finished.", namespace),
		)
	}()
}

func (r *TimeSeriesAggregationReconciler) Reconcile(ctx context.Context, req reconcile.Request) (reconcile.Result, error) {
	if timeSeriesAggregationReconcileRequestMetric != nil {
		timeSeriesAggregationReconcileRequestMetric.Add(ctx, 1)
	}

	qualifiedName := req.NamespacedName.String() //nolint:staticcheck
	logger := logd.FromContext(ctx)
	logger.Info("processing reconcile request for a time series aggregation resource", "name", qualifiedName)

	action := upsertAction
	timeSeriesAggregationResource := &dash0v1alpha1.Dash0TimeSeriesAggregation{}
	if err := r.Get(ctx, req.NamespacedName, timeSeriesAggregationResource); err != nil {
		if apierrors.IsNotFound(err) {
			action = deleteAction
			logger.Info("reconciling the deletion of the time series aggregation resource", "name", qualifiedName)
			timeSeriesAggregationResource = &dash0v1alpha1.Dash0TimeSeriesAggregation{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: req.Namespace,
					Name:      req.Name,
				},
			}
		} else {
			logger.Error(
				err,
				fmt.Sprintf(
					"Failed to get the time series aggregation \"%s\", requeuing reconcile request.",
					qualifiedName,
				),
			)
			return ctrl.Result{}, err
		}
	}

	unstructuredResource, err := structToMap(timeSeriesAggregationResource)
	if err != nil {
		msg := "cannot serialize the time series aggregation resource"
		logger.Error(err, msg)
		if action != deleteAction {
			r.WriteSynchronizationResultToSynchronizedResource(
				ctx,
				timeSeriesAggregationResource,
				synchronizationResults{},
				logger,
			)
		}
		return reconcile.Result{}, nil
	}

	synchronizeViaApiAndUpdateStatus(
		ctx,
		r,
		unstructuredResource,
		timeSeriesAggregationResource,
		action,
		logger,
	)

	return reconcile.Result{}, nil
}

func (r *TimeSeriesAggregationReconciler) MapResourceToHttpRequests(
	preconditionChecksResult *preconditionValidationResult,
	apiConfig ApiConfig,
	action apiAction,
	logger logd.Logger,
) *ResourceToRequestsResult {
	itemName := preconditionChecksResult.k8sName

	timeSeriesAggregationUrl, timeSeriesAggregationOrigin :=
		r.renderTimeSeriesAggregationUrl(preconditionChecksResult, apiConfig.Endpoint, apiConfig.Dataset)

	var req *http.Request
	var method string
	var err error

	switch action {
	case upsertAction:
		resource := preconditionChecksResult.resource
		serializedResource, _ := json.Marshal(resource)
		requestPayload := bytes.NewBuffer(serializedResource)
		method = http.MethodPut
		req, err = http.NewRequest(
			method,
			timeSeriesAggregationUrl,
			requestPayload,
		)
	case deleteAction:
		method = http.MethodDelete
		req, err = http.NewRequest(
			method,
			timeSeriesAggregationUrl,
			nil,
		)
	default:
		unknownActionErr := fmt.Errorf("unknown API action: %d", action)
		logger.Error(unknownActionErr, "unknown API action")
		return NewResourceToRequestsResultSingleItemError(apiConfig, itemName, unknownActionErr.Error())
	}

	if err != nil {
		httpError := fmt.Errorf(
			"unable to create a new HTTP request to synchronize the time series aggregation: %s %s: %w",
			method,
			timeSeriesAggregationUrl,
			err,
		)
		logger.Error(httpError, "error creating http request")
		return NewResourceToRequestsResultSingleItemError(apiConfig, itemName, httpError.Error())
	}

	addAuthorizationHeader(req, apiConfig.Token)
	if action == upsertAction {
		req.Header.Set(util.ContentTypeHeaderName, util.ApplicationJsonMediaType)
	}

	return NewResourceToRequestsResultSingleItemSuccess(
		apiConfig,
		req,
		itemName,
		timeSeriesAggregationOrigin,
	)
}

func (r *TimeSeriesAggregationReconciler) renderTimeSeriesAggregationUrl(
	preconditionChecksResult *preconditionValidationResult,
	endpoint string,
	dataset string,
) (string, string) {
	datasetUrlEncoded := url.QueryEscape(dataset)
	timeSeriesAggregationOrigin := fmt.Sprintf(
		// we deliberately use _ as the separator, since that is an illegal character in Kubernetes names. This avoids
		// any potential naming collisions (e.g. namespace="abc" & name="def-ghi" vs. namespace="abc-def" & name="ghi").
		"dash0-operator_%s_%s_%s_%s",
		r.pseudoClusterUid,
		datasetUrlEncoded,
		preconditionChecksResult.k8sNamespace,
		preconditionChecksResult.k8sName,
	)
	return fmt.Sprintf(
		"%sapi/time-series-aggregations/%s?dataset=%s",
		endpoint,
		timeSeriesAggregationOrigin,
		datasetUrlEncoded,
	), timeSeriesAggregationOrigin
}

func (r *TimeSeriesAggregationReconciler) ExtractIdFromResponseBody(
	responseBytes []byte,
	logger logd.Logger,
) (id string, err error) {
	objectWithMetadata := Dash0ApiObjectWithMetadata{}
	if err := json.Unmarshal(responseBytes, &objectWithMetadata); err != nil {
		logger.Error(
			err,
			"cannot parse response, will not extract the synchronized object's ID",
			"response",
			string(responseBytes),
		)
		return "", err
	}
	return objectWithMetadata.Metadata.Labels.Id, nil
}

func (r *TimeSeriesAggregationReconciler) WriteSynchronizationResultToSynchronizedResource(
	ctx context.Context,
	synchronizedResource client.Object,
	syncResults synchronizationResults,
	logger logd.Logger,
) {
	timeSeriesAggregation := synchronizedResource.(*dash0v1alpha1.Dash0TimeSeriesAggregation)

	// common result
	timeSeriesAggregation.Status.SynchronizationStatus = syncResults.resourceSyncStatus()
	timeSeriesAggregation.Status.SynchronizedAt = metav1.Time{Time: time.Now()}
	timeSeriesAggregation.Status.ValidationIssues = nil // we do not validate anything for time series aggregations

	// result(s) per apiConfig
	timeSeriesAggregationSyncResults :=
		make([]dash0v1alpha1.Dash0TimeSeriesAggregationSynchronizationResultPerEndpointAndDataset, 0,
			len(syncResults.resultsPerApiConfig))
	for _, res := range syncResults.resultsPerApiConfig {
		synchronizationStatus := dash0common.Dash0ApiResourceSynchronizationStatusFailed
		// for time series aggregations there can be only one sync error per endpoint/dataset
		synchronizationError, httpStatusCode := firstSynchronizationErrorAndStatusCode(res.resourceToRequestsResult)
		if synchronizationError == "" {
			// no error: mark this endpoint/dataset as successful (this also clears errors from previous attempts)
			synchronizationStatus = dash0common.Dash0ApiResourceSynchronizationStatusSuccessful
		}
		syncResultPerEndpointAndDataset :=
			dash0v1alpha1.Dash0TimeSeriesAggregationSynchronizationResultPerEndpointAndDataset{
				SynchronizationStatus: synchronizationStatus,
				Dash0ApiEndpoint:      res.apiConfig.Endpoint,
				Dash0Dataset:          res.apiConfig.Dataset,
				SynchronizationError:  synchronizationError,
				HttpStatusCode:        httpStatusCode,
			}
		if len(res.successfullySynchronized) > 0 {
			// for time series aggregations we only have at most one successful result per endpoint/dataset
			synchronized := res.successfullySynchronized[0]
			if synchronized.Labels.Id != "" {
				syncResultPerEndpointAndDataset.Dash0Id = synchronized.Labels.Id
			}
			if synchronized.Labels.Origin != "" {
				syncResultPerEndpointAndDataset.Dash0Origin = synchronized.Labels.Origin
			}
		}
		timeSeriesAggregationSyncResults = append(timeSeriesAggregationSyncResults, syncResultPerEndpointAndDataset)
	}
	timeSeriesAggregation.Status.SynchronizationResults = timeSeriesAggregationSyncResults

	if err := r.Status().Update(ctx, timeSeriesAggregation); err != nil {
		logger.Error(err, "Failed to update Dash0 time series aggregation status.")
	}
}

func (r *TimeSeriesAggregationReconciler) CreateReconcileRequestsForRetryableSyncErrors(
	ctx context.Context,
) ([]reconcile.Request, error) {
	allResources := &dash0v1alpha1.Dash0TimeSeriesAggregationList{}
	if err := r.List(ctx, allResources); err != nil {
		return nil, err
	}
	var requests []reconcile.Request
	for i := range allResources.Items {
		resource := &allResources.Items[i]
		for _, syncResult := range resource.Status.SynchronizationResults {
			if isRetryableSynchronizationError(syncResult.SynchronizationError, syncResult.HttpStatusCode) {
				requests = append(requests, reconcile.Request{
					NamespacedName: client.ObjectKey{Namespace: resource.Namespace, Name: resource.Name},
				})
				break
			}
		}
	}
	return requests, nil
}
