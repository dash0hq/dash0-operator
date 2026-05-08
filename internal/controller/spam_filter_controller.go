// SPDX-FileCopyrightText: Copyright 2026 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package controller

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"maps"
	"net/http"
	"net/url"
	"reflect"
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
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	dash0common "github.com/dash0hq/dash0-operator/api/operator/common"
	dash0v1alpha1 "github.com/dash0hq/dash0-operator/api/operator/v1alpha1"
	dash0v1beta1 "github.com/dash0hq/dash0-operator/api/operator/v1beta1"
	"github.com/dash0hq/dash0-operator/internal/selfmonitoringapiaccess"
	"github.com/dash0hq/dash0-operator/internal/util"
	"github.com/dash0hq/dash0-operator/internal/util/logd"
)

type SpamFilterReconciler struct {
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
	spamFilterReconcileRequestMetric otelmetric.Int64Counter
)

func NewSpamFilterReconciler(
	k8sClient client.Client,
	pseudoClusterUid types.UID,
	leaderElectionAware util.LeaderElectionAware,
	httpClient *http.Client,
) *SpamFilterReconciler {
	return &SpamFilterReconciler{
		Client:               k8sClient,
		pseudoClusterUid:     pseudoClusterUid,
		leaderElectionAware:  leaderElectionAware,
		httpClient:           httpClient,
		defaultApiConfigs:    *selfmonitoringapiaccess.NewSynchronizedSlice[ApiConfig](),
		namespacedApiConfigs: *selfmonitoringapiaccess.NewSynchronizedMapSlice[ApiConfig](),
		namespacedSyncMutex:  *selfmonitoringapiaccess.NewNamespaceMutex(),
	}
}

func (r *SpamFilterReconciler) SetupWithManager(mgr manager.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&dash0v1alpha1.Dash0SpamFilter{}).
		// ignore changes in the status subresource, but react on changes to spec, label and annotations
		WithEventFilter(spamFilterPredicate{}).
		Complete(r)
}

func (r *SpamFilterReconciler) InitializeSelfMonitoringMetrics(
	meter otelmetric.Meter,
	metricNamePrefix string,
	logger logd.Logger,
) {
	reconcileRequestMetricName := fmt.Sprintf("%s%s", metricNamePrefix, "spamfilter.reconcile_requests")
	var err error
	if spamFilterReconcileRequestMetric, err = meter.Int64Counter(
		reconcileRequestMetricName,
		otelmetric.WithUnit("1"),
		otelmetric.WithDescription("Counter for spam filter reconcile requests"),
	); err != nil {
		logger.Error(err, fmt.Sprintf("Cannot initialize the metric %s.", reconcileRequestMetricName))
	}
}

func (r *SpamFilterReconciler) KindDisplayName() string {
	return "spam filter"
}

func (r *SpamFilterReconciler) ShortName() string {
	return "spam-filter"
}

func (r *SpamFilterReconciler) GetDefaultApiConfigs() []ApiConfig {
	return r.defaultApiConfigs.Get()
}

func (r *SpamFilterReconciler) GetNamespacedApiConfigs(namespace string) ([]ApiConfig, bool) {
	return r.namespacedApiConfigs.Get(namespace)
}

func (r *SpamFilterReconciler) ControllerName() string {
	return "dash0_spam_filter_controller"
}

func (r *SpamFilterReconciler) K8sClient() client.Client {
	return r.Client
}

func (r *SpamFilterReconciler) HttpClient() *http.Client {
	return r.httpClient
}

func (r *SpamFilterReconciler) SetDefaultApiConfigs(ctx context.Context, apiConfigs []ApiConfig, logger logd.Logger) {
	r.defaultApiConfigs.Set(apiConfigs)
	r.maybeDoInitialSynchronizationOfAllResources(ctx, logger)
}

func (r *SpamFilterReconciler) RemoveDefaultApiConfigs(_ context.Context, _ logd.Logger) {
	r.defaultApiConfigs.Clear()
}

func (r *SpamFilterReconciler) SetNamespacedApiConfigs(
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

func (r *SpamFilterReconciler) RemoveNamespacedApiConfigs(ctx context.Context, namespace string, logger logd.Logger) {
	if _, exists := r.namespacedApiConfigs.Get(namespace); exists {
		r.namespacedApiConfigs.Delete(namespace)
		r.synchronizeNamespacedResources(ctx, namespace, logger)
	}
}

func (r *SpamFilterReconciler) SetSynchronizationEnabled(
	_ context.Context,
	_ string,
	_ *dash0v1beta1.Dash0Monitoring,
	_ logd.Logger,
) {
	// no-op: spam filters do not have a per-namespace sync toggle
}

func (r *SpamFilterReconciler) RemoveSynchronizationEnabled(_ string) {
	// no-op: spam filters do not have a per-namespace sync toggle
}

func (r *SpamFilterReconciler) NotifyOperatorManagerJustBecameLeader(ctx context.Context, logger logd.Logger) {
	r.maybeDoInitialSynchronizationOfAllResources(ctx, logger)
}

func (r *SpamFilterReconciler) maybeDoInitialSynchronizationOfAllResources(ctx context.Context, logger logd.Logger) {
	r.initialSyncMutex.Lock()
	defer r.initialSyncMutex.Unlock()

	if r.initialSyncHasHappend.Load() || r.initialSyncInProgress.Load() {
		return
	}

	if !r.leaderElectionAware.IsLeader() {
		logger.Info(
			fmt.Sprintf(
				"Waiting for the this operator manager replica to become leader before running initial " +
					"synchronization of spam filters.",
			),
		)
		return
	}
	if len(filterValidApiConfigs(r.defaultApiConfigs.Get(), logger, "default operator configuration")) == 0 {
		logger.Info(
			"Waiting for the Dash0 API config before running initial synchronization of spam filters. Either " +
				"no Dash0 API config has been provided via the operator configuration resource, or the operator " +
				"configuration resource has not been reconciled yet. If there is an operator configuration resource " +
				"with an API endpoint and a Dash0 auth token or a secret ref present in the cluster, it will be " +
				"reconciled in a few seconds and this message can be safely ignored.",
		)
		return
	}

	logger.Info("Running initial synchronization of spam filters now.")
	r.initialSyncInProgress.Store(true)

	go func() {
		defer r.initialSyncInProgress.Store(false)

		allResources := dash0v1alpha1.Dash0SpamFilterList{}
		if err := r.List(
			ctx,
			&allResources,
			&client.ListOptions{},
		); err != nil {
			logger.Error(err, "Failed to list all Dash0 spam filter resources.")
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
		logger.Info("Initial synchronization of spam filters has finished.")
		r.initialSyncHasHappend.Store(true)
	}()
}

func (r *SpamFilterReconciler) synchronizeNamespacedResources(ctx context.Context, namespace string, logger logd.Logger) {
	// namespacedSyncMutex is used so we don't trigger multiple syncs in parallel in a single namespace.
	// That happens for example when the export from a monitoring resource is removed, since that updates both the API
	// config and auth token at almost the same time, triggering two resyncs.
	r.namespacedSyncMutex.Lock(namespace)

	if !r.leaderElectionAware.IsLeader() {
		logger.Info(
			fmt.Sprintf(
				"Waiting for the this operator manager replica to become leader before running " +
					"synchronization of spam filters.",
			),
		)
		return
	}

	logger.Info(fmt.Sprintf("Running synchronization of spam filters in namespace %s now.", namespace))

	go func() {
		defer r.namespacedSyncMutex.Unlock(namespace)

		allResources := dash0v1alpha1.Dash0SpamFilterList{}
		if err := r.List(
			ctx,
			&allResources,
			&client.ListOptions{
				Namespace: namespace,
			},
		); err != nil {
			logger.Error(err, fmt.Sprintf("Failed to list Dash0 spam filter resources in namespace %s.", namespace))
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
		logger.Info(fmt.Sprintf("Synchronization of spam filters in namespace %s has finished.", namespace))
	}()
}

func (r *SpamFilterReconciler) Reconcile(ctx context.Context, req reconcile.Request) (reconcile.Result, error) {
	if spamFilterReconcileRequestMetric != nil {
		spamFilterReconcileRequestMetric.Add(ctx, 1)
	}

	qualifiedName := req.NamespacedName.String() //nolint:staticcheck
	logger := logd.FromContext(ctx)
	logger.Info("processing reconcile request for a spam filter resource", "name", qualifiedName)

	action := upsertAction
	spamFilterResource := &dash0v1alpha1.Dash0SpamFilter{}
	if err := r.Get(ctx, req.NamespacedName, spamFilterResource); err != nil {
		if apierrors.IsNotFound(err) {
			action = deleteAction
			logger.Info("reconciling the deletion of the spam filter resource", "name", qualifiedName)
			spamFilterResource = &dash0v1alpha1.Dash0SpamFilter{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: req.Namespace,
					Name:      req.Name,
				},
			}
		} else {
			logger.Error(
				err,
				fmt.Sprintf(
					"Failed to get the spam filter \"%s\", requeuing reconcile request.",
					qualifiedName,
				),
			)
			return ctrl.Result{}, err
		}
	}

	unstructuredResource, err := structToMap(spamFilterResource)
	if err != nil {
		msg := "cannot serialize the spam filter resource"
		logger.Error(err, msg)
		if action != deleteAction {
			r.WriteSynchronizationResultToSynchronizedResource(ctx, spamFilterResource, synchronizationResults{}, logger)
		}
		return reconcile.Result{}, nil
	}

	synchronizeViaApiAndUpdateStatus(
		ctx,
		r,
		unstructuredResource,
		spamFilterResource,
		action,
		logger,
	)

	return reconcile.Result{}, nil
}

func (r *SpamFilterReconciler) MapResourceToHttpRequests(
	preconditionChecksResult *preconditionValidationResult,
	apiConfig ApiConfig,
	action apiAction,
	logger logd.Logger,
) *ResourceToRequestsResult {
	itemName := preconditionChecksResult.k8sName

	spamFilterUrl, spamFilterOrigin := r.renderSpamFilterUrl(preconditionChecksResult, apiConfig.Endpoint, apiConfig.Dataset)

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
			spamFilterUrl,
			requestPayload,
		)
	case deleteAction:
		method = http.MethodDelete
		req, err = http.NewRequest(
			method,
			spamFilterUrl,
			nil,
		)
	default:
		unknownActionErr := fmt.Errorf("unknown API action: %d", action)
		logger.Error(unknownActionErr, "unknown API action")
		return NewResourceToRequestsResultSingleItemError(apiConfig, itemName, unknownActionErr.Error())
	}

	if err != nil {
		httpError := fmt.Errorf(
			"unable to create a new HTTP request to synchronize the spam filter: %s %s: %w",
			method,
			spamFilterUrl,
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
		spamFilterOrigin,
	)
}

func (r *SpamFilterReconciler) renderSpamFilterUrl(
	preconditionChecksResult *preconditionValidationResult,
	endpoint string,
	dataset string,
) (string, string) {
	datasetUrlEncoded := url.QueryEscape(dataset)
	spamFilterOrigin := fmt.Sprintf(
		// we deliberately use _ as the separator, since that is an illegal character in Kubernetes names. This avoids
		// any potential naming collisions (e.g. namespace="abc" & name="def-ghi" vs. namespace="abc-def" & name="ghi").
		"dash0-operator_%s_%s_%s_%s",
		r.pseudoClusterUid,
		datasetUrlEncoded,
		preconditionChecksResult.k8sNamespace,
		preconditionChecksResult.k8sName,
	)
	return fmt.Sprintf(
		"%sapi/spam-filters/%s?dataset=%s",
		endpoint,
		spamFilterOrigin,
		datasetUrlEncoded,
	), spamFilterOrigin
}

func (r *SpamFilterReconciler) ExtractIdFromResponseBody(
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

func (r *SpamFilterReconciler) WriteSynchronizationResultToSynchronizedResource(
	ctx context.Context,
	synchronizedResource client.Object,
	syncResults synchronizationResults,
	logger logd.Logger,
) {
	spamFilter := synchronizedResource.(*dash0v1alpha1.Dash0SpamFilter)

	// common result
	spamFilter.Status.SynchronizationStatus = syncResults.resourceSyncStatus()
	spamFilter.Status.SynchronizedAt = metav1.Time{Time: time.Now()}
	spamFilter.Status.ValidationIssues = nil // we do not validate anything for spam filters

	// result(s) per apiConfig
	spamFilterSyncResults := make([]dash0v1alpha1.Dash0SpamFilterSynchronizationResultPerEndpointAndDataset, 0,
		len(syncResults.resultsPerApiConfig))
	for _, res := range syncResults.resultsPerApiConfig {
		synchronizationStatus := dash0common.Dash0ApiResourceSynchronizationStatusFailed
		synchronizationError := ""
		if len(res.resourceToRequestsResult.SynchronizationErrors) > 0 {
			// for spam filters there can be only one sync error per endpoint/dataset
			synchronizationError = slices.Collect(maps.Values(res.resourceToRequestsResult.SynchronizationErrors))[0]
		} else {
			// clear out errors from previous synchronization attempts
			synchronizationError = ""
			synchronizationStatus = dash0common.Dash0ApiResourceSynchronizationStatusSuccessful
		}
		syncResultPerEndpointAndDataset := dash0v1alpha1.Dash0SpamFilterSynchronizationResultPerEndpointAndDataset{
			SynchronizationStatus: synchronizationStatus,
			Dash0ApiEndpoint:      res.apiConfig.Endpoint,
			Dash0Dataset:          res.apiConfig.Dataset,
			SynchronizationError:  synchronizationError,
		}
		if len(res.successfullySynchronized) > 0 {
			// for spam filters we only have at most one successful result per endpoint/dataset
			synchronized := res.successfullySynchronized[0]
			if synchronized.Labels.Id != "" {
				syncResultPerEndpointAndDataset.Dash0Id = synchronized.Labels.Id
			}
			if synchronized.Labels.Origin != "" {
				syncResultPerEndpointAndDataset.Dash0Origin = synchronized.Labels.Origin
			}
		}
		spamFilterSyncResults = append(spamFilterSyncResults, syncResultPerEndpointAndDataset)
	}
	spamFilter.Status.SynchronizationResults = spamFilterSyncResults

	if err := r.Status().Update(ctx, spamFilter); err != nil {
		logger.Error(err, "Failed to update Dash0 spam filter status.")
	}
}

// An event filter that ignores changes in the status subresource but reacts on changes to spec, label and annotations.
// Ideally we would just use predicate.GenerationChangedPredicate, but it unfortunately also ignores label and
// annotation changes. This is necessary because we update the status subresource when reconciling the resource, and
// without the filter this would cause another no-op reconcile request.
type spamFilterPredicate struct {
	predicate.Funcs
}

func (p spamFilterPredicate) Update(e event.UpdateEvent) bool {
	if e.ObjectOld == nil || e.ObjectNew == nil {
		return true
	}

	oldObj, okOld := e.ObjectOld.(*dash0v1alpha1.Dash0SpamFilter)
	newObj, okNew := e.ObjectNew.(*dash0v1alpha1.Dash0SpamFilter)

	if !okOld || !okNew {
		return true
	}

	specChanged := !reflect.DeepEqual(oldObj.Spec, newObj.Spec)
	labelsChanged := !reflect.DeepEqual(oldObj.Labels, newObj.Labels)
	annotationsChanged := !reflect.DeepEqual(oldObj.Annotations, newObj.Annotations)

	return specChanged || labelsChanged || annotationsChanged
}
