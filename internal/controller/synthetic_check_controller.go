// SPDX-FileCopyrightText: Copyright 2025 Dash0 Inc.
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

	"github.com/go-logr/logr"
	otelmetric "go.opentelemetry.io/otel/metric"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	dash0common "github.com/dash0hq/dash0-operator/api/operator/common"
	dash0v1alpha1 "github.com/dash0hq/dash0-operator/api/operator/v1alpha1"
	"github.com/dash0hq/dash0-operator/internal/selfmonitoringapiaccess"
	"github.com/dash0hq/dash0-operator/internal/util"
)

type SyntheticCheckReconciler struct {
	client.Client
	pseudoClusterUid      types.UID
	leaderElectionAware   util.LeaderElectionAware
	httpClient            *http.Client
	defaultApiConfigs     selfmonitoringapiaccess.SynchronizedSlice[ApiConfig]
	namespacedApiConfigs  selfmonitoringapiaccess.SynchronizedMapSlice[ApiConfig]
	initialSyncMutex      sync.Mutex
	initialSyncHasHappend atomic.Bool
	namespacedSyncMutex   selfmonitoringapiaccess.NamespaceMutex
	httpRetryDelay        time.Duration
}

var (
	syntheticCheckReconcileRequestMetric otelmetric.Int64Counter
)

func NewSyntheticCheckReconciler(
	k8sClient client.Client,
	pseudoClusterUid types.UID,
	leaderElectionAware util.LeaderElectionAware,
	httpClient *http.Client,
) *SyntheticCheckReconciler {
	return &SyntheticCheckReconciler{
		Client:               k8sClient,
		pseudoClusterUid:     pseudoClusterUid,
		leaderElectionAware:  leaderElectionAware,
		httpClient:           httpClient,
		defaultApiConfigs:    *selfmonitoringapiaccess.NewSynchronizedSlice[ApiConfig](),
		namespacedApiConfigs: *selfmonitoringapiaccess.NewSynchronizedMapSlice[ApiConfig](),
		httpRetryDelay:       1 * time.Second,
		namespacedSyncMutex:  *selfmonitoringapiaccess.NewNamespaceMutex(),
	}
}

func (r *SyntheticCheckReconciler) SetupWithManager(mgr manager.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&dash0v1alpha1.Dash0SyntheticCheck{}).
		// ignore changes in the status subresource, but react on changes to spec, label and annotations
		WithEventFilter(syntheticCheckPredicate{}).
		Complete(r)
}

func (r *SyntheticCheckReconciler) InitializeSelfMonitoringMetrics(
	meter otelmetric.Meter,
	metricNamePrefix string,
	logger logr.Logger,
) {
	reconcileRequestMetricName := fmt.Sprintf("%s%s", metricNamePrefix, "syntheticcheck.reconcile_requests")
	var err error
	if syntheticCheckReconcileRequestMetric, err = meter.Int64Counter(
		reconcileRequestMetricName,
		otelmetric.WithUnit("1"),
		otelmetric.WithDescription("Counter for synthetic check reconcile requests"),
	); err != nil {
		logger.Error(err, fmt.Sprintf("Cannot initialize the metric %s.", reconcileRequestMetricName))
	}
}

func (r *SyntheticCheckReconciler) KindDisplayName() string {
	return "synthetic check"
}

func (r *SyntheticCheckReconciler) ShortName() string {
	return "check"
}

func (r *SyntheticCheckReconciler) GetDefaultApiConfigs() []ApiConfig {
	return r.defaultApiConfigs.Get()
}

func (r *SyntheticCheckReconciler) GetNamespacedApiConfigs(namespace string) ([]ApiConfig, bool) {
	return r.namespacedApiConfigs.Get(namespace)
}

func (r *SyntheticCheckReconciler) ControllerName() string {
	return "dash0_synthetic_check_controller"
}

func (r *SyntheticCheckReconciler) K8sClient() client.Client {
	return r.Client
}

func (r *SyntheticCheckReconciler) HttpClient() *http.Client {
	return r.httpClient
}

func (r *SyntheticCheckReconciler) GetHttpRetryDelay() time.Duration {
	return r.httpRetryDelay
}

//nolint:unused
func (r *SyntheticCheckReconciler) overrideHttpRetryDelay(delay time.Duration) {
	r.httpRetryDelay = delay
}

func (r *SyntheticCheckReconciler) SetDefaultApiConfigs(
	ctx context.Context,
	apiConfigs []ApiConfig,
	logger logr.Logger,
) {
	r.defaultApiConfigs.Set(apiConfigs)
	r.maybeDoInitialSynchronizationOfAllResources(ctx, logger)
}

func (r *SyntheticCheckReconciler) RemoveDefaultApiConfigs(_ context.Context, _ logr.Logger) {
	r.defaultApiConfigs.Clear()
}

func (r *SyntheticCheckReconciler) SetNamespacedApiConfigs(
	ctx context.Context,
	namespace string,
	updatedApiConfigs []ApiConfig,
	logger logr.Logger,
) {
	if updatedApiConfigs != nil {
		previousApiConfigs, _ := r.namespacedApiConfigs.Get(namespace)

		r.namespacedApiConfigs.Set(namespace, updatedApiConfigs)

		if !slices.Equal(previousApiConfigs, updatedApiConfigs) {
			r.synchronizeNamespacedResources(ctx, namespace, logger)
		}
	}
}

func (r *SyntheticCheckReconciler) RemoveNamespacedApiConfigs(
	ctx context.Context,
	namespace string,
	logger logr.Logger,
) {
	if _, exists := r.namespacedApiConfigs.Get(namespace); exists {
		r.namespacedApiConfigs.Delete(namespace)
		r.synchronizeNamespacedResources(ctx, namespace, logger)
	}
}

func (r *SyntheticCheckReconciler) NotifiyOperatorManagerJustBecameLeader(ctx context.Context, logger logr.Logger) {
	r.maybeDoInitialSynchronizationOfAllResources(ctx, logger)
}

func (r *SyntheticCheckReconciler) maybeDoInitialSynchronizationOfAllResources(
	ctx context.Context,
	logger logr.Logger,
) {
	r.initialSyncMutex.Lock()
	defer r.initialSyncMutex.Unlock()

	if r.initialSyncHasHappend.Load() {
		return
	}

	if !r.leaderElectionAware.IsLeader() {
		logger.Info(
			fmt.Sprintf(
				"Waiting for the this operator manager replica to become leader before running initial " +
					"synchronization of synthetic checks.",
			),
		)
		return
	}
	if len(filterValidApiConfigs(r.defaultApiConfigs.Get(), logger, "default operator configuration")) == 0 {
		logger.Info(
			"Waiting for the Dash0 API config before running initial synchronization of synthetic checks. Either " +
				"no Dash0 API config has been provided via the operator configuration resource, or the operator " +
				"configuration resource has not been reconciled yet. If there is an operator configuration resource " +
				"with an API endpoint and a Dash0 auth token or a secret ref present in the cluster, it will be " +
				"reconciled in a few seconds and this message can be safely ignored.",
		)
		return
	}

	logger.Info("Running initial synchronization of synthetic checks now.")

	go func() {
		allSyntheticCheckResourcesInCluster := dash0v1alpha1.Dash0SyntheticCheckList{}
		if err := r.List(
			ctx,
			&allSyntheticCheckResourcesInCluster,
			&client.ListOptions{},
		); err != nil {
			logger.Error(err, "Failed to list all Dash0 synthetic check resources.")
			return
		}

		for _, syntheticCheckResource := range allSyntheticCheckResourcesInCluster.Items {
			pseudoReconcileRequest := ctrl.Request{
				NamespacedName: client.ObjectKey{
					Namespace: syntheticCheckResource.Namespace,
					Name:      syntheticCheckResource.Name,
				},
			}
			_, _ = r.Reconcile(ctx, pseudoReconcileRequest)
			// stagger API requests a bit
			time.Sleep(50 * time.Millisecond)
		}
		logger.Info("Initial synchronization of synthetic checks has finished.")
		r.initialSyncHasHappend.Store(true)
	}()
}

func (r *SyntheticCheckReconciler) synchronizeNamespacedResources(
	ctx context.Context,
	namespace string,
	logger logr.Logger,
) {
	// The namespacedSyncMutex is used so we don't trigger multiple syncs in parallel in a single namespace.
	// That happens for example when the export from a monitoring resource is removed, since that updates both the API
	// config and auth token at almost the same time, triggering two resyncs.
	r.namespacedSyncMutex.Lock(namespace)

	if !r.leaderElectionAware.IsLeader() {
		logger.Info(
			fmt.Sprintf(
				"Waiting for the this operator manager replica to become leader before running " +
					"synchronization of synthetic checks.",
			),
		)
		return
	}

	logger.Info(fmt.Sprintf("Running synchronization of synthetic checks in namespace %s now.", namespace))

	go func() {
		defer r.namespacedSyncMutex.Unlock(namespace)

		allSyntheticCheckResourcesInNamespace := dash0v1alpha1.Dash0SyntheticCheckList{}
		if err := r.List(
			ctx,
			&allSyntheticCheckResourcesInNamespace,
			&client.ListOptions{
				Namespace: namespace,
			},
		); err != nil {
			logger.Error(err, fmt.Sprintf("Failed to list Dash0 synthetic checks resources in namespace %s.", namespace))
			return
		}

		for _, syntheticCheckResource := range allSyntheticCheckResourcesInNamespace.Items {
			pseudoReconcileRequest := ctrl.Request{
				NamespacedName: client.ObjectKey{
					Namespace: syntheticCheckResource.Namespace,
					Name:      syntheticCheckResource.Name,
				},
			}
			_, _ = r.Reconcile(ctx, pseudoReconcileRequest)
			// stagger API requests a bit
			time.Sleep(50 * time.Millisecond)
		}
		logger.Info(fmt.Sprintf("Synchronization of synthetic checks in namespace %s has finished.", namespace))
	}()
}

func (r *SyntheticCheckReconciler) Reconcile(ctx context.Context, req reconcile.Request) (reconcile.Result, error) {
	if syntheticCheckReconcileRequestMetric != nil {
		syntheticCheckReconcileRequestMetric.Add(ctx, 1)
	}

	qualifiedName := req.NamespacedName.String() //nolint:staticcheck
	logger := log.FromContext(ctx)
	logger.Info("processing reconcile request for a synthetic check resource", "name", qualifiedName)

	action := upsertAction
	syntheticCheckResource := &dash0v1alpha1.Dash0SyntheticCheck{}
	if err := r.Get(ctx, req.NamespacedName, syntheticCheckResource); err != nil {
		if apierrors.IsNotFound(err) {
			action = deleteAction
			logger.Info("reconciling the deletion of the synthetic check resource", "name", qualifiedName)
			syntheticCheckResource = &dash0v1alpha1.Dash0SyntheticCheck{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: req.Namespace,
					Name:      req.Name,
				},
			}
		} else {
			logger.Error(
				err,
				fmt.Sprintf(
					"Failed to get the synthetic check \"%s\", requeuing reconcile request.",
					qualifiedName,
				),
			)
			return ctrl.Result{}, err
		}
	}

	unstructuredResource, err := structToMap(syntheticCheckResource)
	if err != nil {
		msg := "cannot serialize the synthetic check resource"
		logger.Error(err, msg)
		if action != deleteAction {
			r.WriteSynchronizationResultToSynchronizedResource(
				ctx,
				syntheticCheckResource,
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
		syntheticCheckResource,
		action,
		logger,
	)

	return reconcile.Result{}, nil
}

func (r *SyntheticCheckReconciler) MapResourceToHttpRequests(
	preconditionChecksResult *preconditionValidationResult,
	apiConfig ApiConfig,
	action apiAction,
	logger logr.Logger,
) *ResourceToRequestsResult {
	itemName := preconditionChecksResult.k8sName
	syntheticCheckUrl, syntheticCheckOrigin := r.renderSyntheticCheckUrl(
		preconditionChecksResult,
		apiConfig.Endpoint,
		apiConfig.Dataset,
	)

	var req *http.Request
	var method string
	var err error

	switch action {
	case upsertAction:
		syntheticCheck := preconditionChecksResult.resource
		serializedSyntheticCheck, _ := json.Marshal(syntheticCheck)
		requestPayload := bytes.NewBuffer(serializedSyntheticCheck)
		method = http.MethodPut
		req, err = http.NewRequest(
			method,
			syntheticCheckUrl,
			requestPayload,
		)
	case deleteAction:
		method = http.MethodDelete
		req, err = http.NewRequest(
			method,
			syntheticCheckUrl,
			nil,
		)
	default:
		unknownActionErr := fmt.Errorf("unknown API action: %d", action)
		logger.Error(unknownActionErr, "unknown API action")
		return NewResourceToRequestsResultSingleItemError(apiConfig, itemName, unknownActionErr.Error())
	}

	if err != nil {
		httpError := fmt.Errorf(
			"unable to create a new HTTP request to synchronize the synthetic check: %s %s: %w",
			method,
			syntheticCheckUrl,
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
		syntheticCheckOrigin,
	)
}

func (r *SyntheticCheckReconciler) renderSyntheticCheckUrl(
	preconditionChecksResult *preconditionValidationResult,
	endpoint string,
	dataset string,
) (string, string) {
	datasetUrlEncoded := url.QueryEscape(dataset)
	syntheticCheckOrigin := fmt.Sprintf(
		// we deliberately use _ as the separator, since that is an illegal character in Kubernetes names. This avoids
		// any potential naming collisions (e.g. namespace="abc" & name="def-ghi" vs. namespace="abc-def" & name="ghi").
		"dash0-operator_%s_%s_%s_%s",
		r.pseudoClusterUid,
		datasetUrlEncoded,
		preconditionChecksResult.k8sNamespace,
		preconditionChecksResult.k8sName,
	)
	return fmt.Sprintf(
		"%sapi/synthetic-checks/%s?dataset=%s",
		endpoint,
		syntheticCheckOrigin,
		datasetUrlEncoded,
	), syntheticCheckOrigin
}

func (r *SyntheticCheckReconciler) ExtractIdFromResponseBody(
	responseBytes []byte,
	logger logr.Logger,
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

func (r *SyntheticCheckReconciler) WriteSynchronizationResultToSynchronizedResource(
	ctx context.Context,
	synchronizedResource client.Object,
	syncResults synchronizationResults,
	logger logr.Logger,
) {
	syntheticCheck := synchronizedResource.(*dash0v1alpha1.Dash0SyntheticCheck)

	// common result
	syntheticCheck.Status.SynchronizationStatus = syncResults.resourceSyncStatus()
	syntheticCheck.Status.SynchronizedAt = metav1.Time{Time: time.Now()}
	syntheticCheck.Status.ValidationIssues = nil // we do not validate anything for synthetic checks

	// result(s) per apiConfig
	syntheticCheckSyncResults := make([]dash0v1alpha1.Dash0SyntheticCheckSynchronizationResultPerEndpointAndDataset, 0,
		len(syncResults.resultsPerApiConfig))
	for _, res := range syncResults.resultsPerApiConfig {
		synchronizationStatus := dash0common.Dash0ApiResourceSynchronizationStatusFailed
		synchronizationError := ""
		if len(res.resourceToRequestsResult.SynchronizationErrors) > 0 {
			// for synthetic checks there can be only one sync error per endpoint/dataset
			synchronizationError = slices.Collect(maps.Values(res.resourceToRequestsResult.SynchronizationErrors))[0]
		} else {
			// clear out errors from previous synchronization attempts
			synchronizationError = ""
			synchronizationStatus = dash0common.Dash0ApiResourceSynchronizationStatusSuccessful
		}
		syncResultPerEndpointAndDataset := dash0v1alpha1.Dash0SyntheticCheckSynchronizationResultPerEndpointAndDataset{
			SynchronizationStatus: synchronizationStatus,
			Dash0ApiEndpoint:      res.apiConfig.Endpoint,
			Dash0Dataset:          res.apiConfig.Dataset,
			SynchronizationError:  synchronizationError,
		}
		if len(res.successfullySynchronized) > 0 {
			// for synthetic checks we only have at most one successful result per endpoint/dataset
			synchronized := res.successfullySynchronized[0]
			if synchronized.Labels.Id != "" {
				syncResultPerEndpointAndDataset.Dash0Id = synchronized.Labels.Id
			}
			if synchronized.Labels.Origin != "" {
				syncResultPerEndpointAndDataset.Dash0Origin = synchronized.Labels.Origin
			}
		}
		syntheticCheckSyncResults = append(syntheticCheckSyncResults, syncResultPerEndpointAndDataset)
	}
	syntheticCheck.Status.SynchronizationResults = syntheticCheckSyncResults

	if err := r.Status().Update(ctx, syntheticCheck); err != nil {
		logger.Error(err, "Failed to update Dash0 synthetic check status.")
	}
}

// An event filter that ignores changes in the status subresource but reacts on changes to spec, label and annotations.
// Ideally we would just use predicate.GenerationChangedPredicate, but it unfortunately also ignores label and
// annotation changes. This is necessary because we update the status subresource when reconciling the resource, and
// without the filter this would cause another no-op reconcile request.
type syntheticCheckPredicate struct {
	predicate.Funcs
}

func (p syntheticCheckPredicate) CreateFunc(_ event.CreateEvent) bool {
	return true
}

func (p syntheticCheckPredicate) DeleteFunc(_ event.DeleteEvent) bool {
	return true
}

func (p syntheticCheckPredicate) Update(e event.UpdateEvent) bool {
	if e.ObjectOld == nil || e.ObjectNew == nil {
		return true
	}

	oldObj, okOld := e.ObjectOld.(*dash0v1alpha1.Dash0SyntheticCheck)
	newObj, okNew := e.ObjectNew.(*dash0v1alpha1.Dash0SyntheticCheck)

	if !okOld || !okNew {
		return true
	}

	specChanged := !reflect.DeepEqual(oldObj.Spec, newObj.Spec)
	labelsChanged := !reflect.DeepEqual(oldObj.Labels, newObj.Labels)
	annotationsChanged := !reflect.DeepEqual(oldObj.Annotations, newObj.Annotations)

	return specChanged || labelsChanged || annotationsChanged
}

func (p syntheticCheckPredicate) GenericFunc(_ event.GenericEvent) bool {
	return true
}
