// SPDX-FileCopyrightText: Copyright 2026 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package controller

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"slices"
	"sync"
	"sync/atomic"
	"time"

	dash0apiclient "github.com/dash0hq/dash0-api-client-go"
	otelmetric "go.opentelemetry.io/otel/metric"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	dash0dashv1alpha1 "github.com/dash0hq/dash0-operator/api/dash0/v1alpha1"
	dash0common "github.com/dash0hq/dash0-operator/api/operator/common"
	dash0v1beta1 "github.com/dash0hq/dash0-operator/api/operator/v1beta1"
	"github.com/dash0hq/dash0-operator/internal/selfmonitoringapiaccess"
	"github.com/dash0hq/dash0-operator/internal/util"
	"github.com/dash0hq/dash0-operator/internal/util/logd"
)

type DatasetReconciler struct {
	client.Client
	pseudoClusterUid      types.UID
	leaderElectionAware   util.LeaderElectionAware
	apiClientPool         *ApiClientPool
	defaultApiConfigs     selfmonitoringapiaccess.SynchronizedSlice[ApiConfig]
	namespacedApiConfigs  selfmonitoringapiaccess.SynchronizedMapSlice[ApiConfig]
	initialSyncMutex      sync.Mutex
	initialSyncInProgress atomic.Bool
	initialSyncHasHappend atomic.Bool
	namespacedSyncMutex   selfmonitoringapiaccess.NamespaceMutex
}

var (
	datasetReconcileRequestMetric otelmetric.Int64Counter
)

// NewDatasetReconciler creates the reconciler for Dash0Dataset resources. The pool's transport should retry 409
// responses, which the Dash0 API returns when concurrent writes to one dataset race.
func NewDatasetReconciler(
	k8sClient client.Client,
	pseudoClusterUid types.UID,
	leaderElectionAware util.LeaderElectionAware,
	apiClientPool *ApiClientPool,
) *DatasetReconciler {
	return &DatasetReconciler{
		Client:               k8sClient,
		pseudoClusterUid:     pseudoClusterUid,
		leaderElectionAware:  leaderElectionAware,
		apiClientPool:        apiClientPool,
		defaultApiConfigs:    *selfmonitoringapiaccess.NewSynchronizedSlice[ApiConfig](),
		namespacedApiConfigs: *selfmonitoringapiaccess.NewSynchronizedMapSlice[ApiConfig](),
		namespacedSyncMutex:  *selfmonitoringapiaccess.NewNamespaceMutex(),
	}
}

func (r *DatasetReconciler) SetupWithManager(mgr manager.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&dash0dashv1alpha1.Dash0Dataset{}).
		// ignore changes in the status subresource, but react on changes to spec, labels and dash0.com/ annotations
		WithEventFilter(generationLabelOrDash0AnnotationChangePredicate).
		Complete(r)
}

func (r *DatasetReconciler) InitializeSelfMonitoringMetrics(
	meter otelmetric.Meter,
	metricNamePrefix string,
	logger logd.Logger,
) {
	reconcileRequestMetricName := fmt.Sprintf("%s%s", metricNamePrefix, "dataset.reconcile_requests")
	var err error
	if datasetReconcileRequestMetric, err = meter.Int64Counter(
		reconcileRequestMetricName,
		otelmetric.WithUnit("1"),
		otelmetric.WithDescription("Counter for dataset reconcile requests"),
	); err != nil {
		logger.Error(err, fmt.Sprintf("Cannot initialize the metric %s.", reconcileRequestMetricName))
	}
}

func (r *DatasetReconciler) KindDisplayName() string {
	return "dataset"
}

func (r *DatasetReconciler) ShortName() string {
	return "dataset"
}

func (r *DatasetReconciler) GetDefaultApiConfigs() []ApiConfig {
	return r.defaultApiConfigs.Get()
}

func (r *DatasetReconciler) GetNamespacedApiConfigs(namespace string) ([]ApiConfig, bool) {
	return r.namespacedApiConfigs.Get(namespace)
}

func (r *DatasetReconciler) ControllerName() string {
	return "dash0_dataset_controller"
}

func (r *DatasetReconciler) K8sClient() client.Client {
	return r.Client
}

func (r *DatasetReconciler) SetDefaultApiConfigs(
	ctx context.Context,
	apiConfigs []ApiConfig,
	logger logd.Logger,
) {
	r.defaultApiConfigs.Set(apiConfigs)
	r.maybeDoInitialSynchronizationOfAllResources(ctx, logger)
}

func (r *DatasetReconciler) RemoveDefaultApiConfigs(_ context.Context, _ logd.Logger) {
	r.defaultApiConfigs.Clear()
}

func (r *DatasetReconciler) SetNamespacedApiConfigs(
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

func (r *DatasetReconciler) RemoveNamespacedApiConfigs(
	ctx context.Context,
	namespace string,
	logger logd.Logger,
) {
	if _, exists := r.namespacedApiConfigs.Get(namespace); exists {
		r.namespacedApiConfigs.Delete(namespace)
		r.synchronizeNamespacedResources(ctx, namespace, logger)
	}
}

func (r *DatasetReconciler) SetSynchronizationEnabled(
	_ context.Context,
	_ string,
	_ *dash0v1beta1.Dash0Monitoring,
	_ logd.Logger,
) {
	// no-op: datasets do not have a per-namespace sync toggle
}

func (r *DatasetReconciler) RemoveSynchronizationEnabled(_ string) {
	// no-op: datasets do not have a per-namespace sync toggle
}

func (r *DatasetReconciler) NotifyOperatorManagerJustBecameLeader(
	ctx context.Context,
	logger logd.Logger,
) {
	r.maybeDoInitialSynchronizationOfAllResources(ctx, logger)
}

func (r *DatasetReconciler) maybeDoInitialSynchronizationOfAllResources(
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
			"Waiting for this operator manager replica to become leader before running initial " +
				"synchronization of datasets.",
		)
		return
	}
	if len(filterValidApiConfigs(r.defaultApiConfigs.Get(), logger, "default operator configuration")) == 0 {
		logger.Info(
			"Waiting for the Dash0 API config before running initial synchronization of datasets. Either " +
				"no Dash0 API config has been provided via the operator configuration resource, or the operator " +
				"configuration resource has not been reconciled yet. If there is an operator configuration resource " +
				"with an API endpoint and a Dash0 auth token or a secret ref present in the cluster, it will be " +
				"reconciled in a few seconds and this message can be safely ignored.",
		)
		return
	}

	logger.Info("Running initial synchronization of datasets now.")
	r.initialSyncInProgress.Store(true)

	go func() {
		defer r.initialSyncInProgress.Store(false)

		allResources := dash0dashv1alpha1.Dash0DatasetList{}
		if err := r.List(
			ctx,
			&allResources,
			&client.ListOptions{},
		); err != nil {
			logger.Error(err, "Failed to list all Dash0 dataset resources.")
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
		logger.Info("Initial synchronization of datasets has finished.")
		r.initialSyncHasHappend.Store(true)
	}()
}

func (r *DatasetReconciler) synchronizeNamespacedResources(
	ctx context.Context,
	namespace string,
	logger logd.Logger,
) {
	if !r.leaderElectionAware.IsLeader() {
		logger.Info(
			"Waiting for this operator manager replica to become leader before running " +
				"synchronization of datasets.",
		)
		return
	}

	// The namespacedSyncMutex is used so we don't trigger multiple syncs in parallel in a single namespace.
	// That happens for example when the export from a monitoring resource is removed, since that updates both the API
	// config and auth token at almost the same time, triggering two resyncs.
	r.namespacedSyncMutex.Lock(namespace)

	logger.Info(fmt.Sprintf("Running synchronization of datasets in namespace %s now.", namespace))

	go func() {
		defer r.namespacedSyncMutex.Unlock(namespace)

		allResources := dash0dashv1alpha1.Dash0DatasetList{}
		if err := r.List(
			ctx,
			&allResources,
			&client.ListOptions{
				Namespace: namespace,
			},
		); err != nil {
			logger.Error(err, fmt.Sprintf("Failed to list Dash0 dataset resources in namespace %s.", namespace))
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
		logger.Info(fmt.Sprintf("Synchronization of datasets in namespace %s has finished.", namespace))
	}()
}

func (r *DatasetReconciler) Reconcile(
	ctx context.Context,
	req reconcile.Request,
) (reconcile.Result, error) {
	if datasetReconcileRequestMetric != nil {
		datasetReconcileRequestMetric.Add(ctx, 1)
	}

	qualifiedName := req.NamespacedName.String() //nolint:staticcheck
	logger := logd.FromContext(ctx)
	logger.Info("processing reconcile request for a dataset resource", "name", qualifiedName)

	action := upsertAction
	datasetResource := &dash0dashv1alpha1.Dash0Dataset{}
	if err := r.Get(ctx, req.NamespacedName, datasetResource); err != nil {
		if apierrors.IsNotFound(err) {
			action = deleteAction
			logger.Info("reconciling the deletion of the dataset resource", "name", qualifiedName)
			datasetResource = &dash0dashv1alpha1.Dash0Dataset{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: req.Namespace,
					Name:      req.Name,
				},
			}
		} else {
			logger.Error(
				err,
				fmt.Sprintf(
					"Failed to get the dataset \"%s\", requeuing reconcile request.",
					qualifiedName,
				),
			)
			return ctrl.Result{}, err
		}
	}

	unstructuredResource, err := structToMap(datasetResource)
	if err != nil {
		msg := "cannot serialize the dataset resource"
		logger.Error(err, msg)
		if action != deleteAction {
			r.WriteSynchronizationResultToSynchronizedResource(
				ctx,
				datasetResource,
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
		datasetResource,
		action,
		logger,
	)

	return reconcile.Result{}, nil
}

func (r *DatasetReconciler) MapResourceToApiCalls(
	preconditionChecksResult *preconditionValidationResult,
	apiConfig ApiConfig,
	action apiAction,
	logger logd.Logger,
) *ResourceToRequestsResult {
	itemName := preconditionChecksResult.k8sName
	datasetOrigin := r.datasetOrigin(preconditionChecksResult)

	var apiClientCall *ApiClientCall
	switch action {
	case upsertAction:
		datasetDefinition, err := mapToDatasetDefinition(preconditionChecksResult.resource, datasetOrigin)
		if err != nil {
			logger.Error(err, "error converting dataset")
			return NewResourceToRequestsResultSingleItemConversionError(apiConfig, itemName, err.Error())
		}
		slug := datasetDefinition.Spec.Slug
		apiClientCall = &ApiClientCall{
			Method: http.MethodPut,
			Url:    renderDatasetUrl(apiConfig.Endpoint, slug),
			Execute: func(ctx context.Context) (string, error) {
				apiClient, err := r.apiClientPool.Get(apiConfig.Endpoint, apiConfig.Token)
				if err != nil {
					return "", err
				}
				return upsertDataset(ctx, apiClient, slug, datasetDefinition)
			},
		}
	case deleteAction:
		// The resource is gone and its slug with it, so the dataset is addressed by the origin the operator set.
		apiClientCall = &ApiClientCall{
			Method: http.MethodDelete,
			Url:    renderDatasetUrl(apiConfig.Endpoint, datasetOrigin),
			Execute: func(ctx context.Context) (string, error) {
				apiClient, err := r.apiClientPool.Get(apiConfig.Endpoint, apiConfig.Token)
				if err != nil {
					return "", err
				}
				return "", apiClient.DeleteDataset(ctx, datasetOrigin)
			},
		}
	default:
		unknownActionErr := fmt.Errorf("unknown API action: %d", action)
		logger.Error(unknownActionErr, "unknown API action")
		return NewResourceToRequestsResultSingleItemError(apiConfig, itemName, unknownActionErr.Error())
	}

	return NewResourceToRequestsResultSingleItemApiClientCall(
		apiConfig,
		apiClientCall,
		itemName,
		datasetOrigin,
	)
}

// upsertDataset updates the dataset with the given slug, or creates it when it does not exist. A soft-deleted dataset
// also answers 404, and the create restores it.
func upsertDataset(
	ctx context.Context,
	apiClient dash0apiclient.Client,
	slug string,
	datasetDefinition *dash0apiclient.DatasetDefinition,
) (string, error) {
	var result *dash0apiclient.DatasetDefinition
	_, err := apiClient.GetDataset(ctx, slug)
	switch {
	case err == nil:
		result, err = apiClient.UpdateDataset(ctx, slug, datasetDefinition)
	case dash0apiclient.IsNotFound(err):
		result, err = apiClient.CreateDataset(ctx, datasetDefinition)
	}
	if err != nil {
		return "", err
	}
	if result == nil {
		return "", errors.New("unexpected nil/empty response body")
	}
	return dash0apiclient.GetDatasetID(result), nil
}

// mapToDatasetDefinition converts the dataset resource to the dataset definition of the Dash0 API client. The display
// name becomes metadata.name, and the origin label marks the dataset as managed by this resource.
func mapToDatasetDefinition(resource map[string]any, origin string) (*dash0apiclient.DatasetDefinition, error) {
	spec := &dash0dashv1alpha1.Dash0DatasetSpec{}
	serializedSpec, err := json.Marshal(resource["spec"])
	if err != nil {
		return nil, err
	}
	if err = json.Unmarshal(serializedSpec, spec); err != nil {
		return nil, fmt.Errorf("unable to convert the dataset to the Dash0 API format: %w", err)
	}
	if spec.Slug == "" || spec.Display.Name == "" {
		return nil, errors.New("unable to convert the dataset to the Dash0 API format: spec.slug and spec.display.name are required")
	}
	serializedSpec, err = json.Marshal(spec)
	if err != nil {
		return nil, err
	}
	datasetDefinition := &dash0apiclient.DatasetDefinition{
		ApiVersion: "dash0.com/v1alpha1",
		Kind:       dash0apiclient.Dash0Dataset,
		Metadata: dash0apiclient.DatasetMetadata{
			Name:   spec.Display.Name,
			Labels: &dash0apiclient.DatasetLabels{Dash0Comorigin: &origin},
		},
	}
	if err = json.Unmarshal(serializedSpec, &datasetDefinition.Spec); err != nil {
		return nil, fmt.Errorf("unable to convert the dataset to the Dash0 API format: %w", err)
	}
	dash0apiclient.SetDatasetID(datasetDefinition, spec.Slug)
	return datasetDefinition, nil
}

func (r *DatasetReconciler) datasetOrigin(preconditionChecksResult *preconditionValidationResult) string {
	return fmt.Sprintf(
		// _ is an illegal character in Kubernetes names, which avoids collisions between namespace and name parts.
		"dash0-operator_%s_%s_%s",
		r.pseudoClusterUid,
		preconditionChecksResult.k8sNamespace,
		preconditionChecksResult.k8sName,
	)
}

func renderDatasetUrl(endpoint string, originOrSlug string) string {
	return fmt.Sprintf("%sapi/datasets/%s", endpoint, url.PathEscape(originOrSlug))
}

func (r *DatasetReconciler) WriteSynchronizationResultToSynchronizedResource(
	ctx context.Context,
	synchronizedResource client.Object,
	syncResults synchronizationResults,
	logger logd.Logger,
) {
	dataset := synchronizedResource.(*dash0dashv1alpha1.Dash0Dataset)

	// common result
	dataset.Status.SynchronizationStatus = syncResults.resourceSyncStatus()
	dataset.Status.SynchronizedAt = metav1.Time{Time: time.Now()}
	dataset.Status.ValidationIssues = nil // we do not validate anything for datasets operator-side

	// result(s) per apiConfig
	datasetSyncResults := make(
		[]dash0dashv1alpha1.Dash0DatasetSynchronizationResultPerEndpoint,
		0,
		len(syncResults.resultsPerApiConfig),
	)
	for _, res := range syncResults.resultsPerApiConfig {
		synchronizationStatus := dash0common.Dash0ApiResourceSynchronizationStatusFailed
		// for datasets there can be only one sync error per endpoint
		synchronizationError, httpStatusCode := firstSynchronizationErrorAndStatusCode(res.resourceToRequestsResult)
		if synchronizationError == "" {
			// no error: mark this endpoint as successful (this also clears errors from previous attempts)
			synchronizationStatus = dash0common.Dash0ApiResourceSynchronizationStatusSuccessful
		}
		syncResultPerEndpoint := dash0dashv1alpha1.Dash0DatasetSynchronizationResultPerEndpoint{
			SynchronizationStatus: synchronizationStatus,
			Dash0ApiEndpoint:      res.apiConfig.Endpoint,
			SynchronizationError:  synchronizationError,
			HttpStatusCode:        httpStatusCode,
		}
		if len(res.successfullySynchronized) > 0 {
			// for datasets we only have at most one successful result per endpoint
			synchronized := res.successfullySynchronized[0]
			if synchronized.Labels.Id != "" {
				syncResultPerEndpoint.Dash0Id = synchronized.Labels.Id
			}
			if synchronized.Labels.Origin != "" {
				syncResultPerEndpoint.Dash0Origin = synchronized.Labels.Origin
			}
		}
		datasetSyncResults = append(datasetSyncResults, syncResultPerEndpoint)
	}
	dataset.Status.SynchronizationResults = datasetSyncResults

	if err := r.Status().Update(ctx, dataset); err != nil {
		logger.Error(err, "Failed to update Dash0 dataset status.")
	}
}

func (r *DatasetReconciler) CreateReconcileRequestsForRetryableSyncErrors(
	ctx context.Context,
) ([]reconcile.Request, error) {
	allResources := &dash0dashv1alpha1.Dash0DatasetList{}
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
