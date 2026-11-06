// SPDX-FileCopyrightText: Copyright 2026 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package controller

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"slices"
	"sync"

	dash0apiclient "github.com/dash0hq/dash0-api-client-go"
	otelmetric "go.opentelemetry.io/otel/metric"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
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

const telemetryTransformationRuleFinalizer = "operator.dash0.com/telemetry-transformation-rule"

// TelemetryTransformationRuleReconciler synchronizes transformation rules with their configured Dash0 datasets.
type TelemetryTransformationRuleReconciler struct {
	client.Client
	pseudoClusterUid       types.UID
	leaderElectionAware    util.LeaderElectionAware
	apiClientPool          *ApiClientPool
	defaultApiConfigs      selfmonitoringapiaccess.SynchronizedSlice[ApiConfig]
	namespacedApiConfigs   selfmonitoringapiaccess.SynchronizedMapSlice[ApiConfig]
	initialSyncMutex       sync.Mutex
	initialSyncStarted     bool
	namespacedSyncMutex    selfmonitoringapiaccess.NamespaceMutex
	reconcileMutex         selfmonitoringapiaccess.NamespaceMutex
	reconcileRequestMetric otelmetric.Int64Counter
}

// NewTelemetryTransformationRuleReconciler creates a transformation rule reconciler using the shared API transport.
func NewTelemetryTransformationRuleReconciler(
	k8sClient client.Client,
	pseudoClusterUid types.UID,
	leaderElectionAware util.LeaderElectionAware,
	apiClientPool *ApiClientPool,
) *TelemetryTransformationRuleReconciler {
	return &TelemetryTransformationRuleReconciler{
		Client:               k8sClient,
		pseudoClusterUid:     pseudoClusterUid,
		leaderElectionAware:  leaderElectionAware,
		apiClientPool:        apiClientPool,
		defaultApiConfigs:    *selfmonitoringapiaccess.NewSynchronizedSlice[ApiConfig](),
		namespacedApiConfigs: *selfmonitoringapiaccess.NewSynchronizedMapSlice[ApiConfig](),
	}
}

// SetupWithManager registers the transformation rule watch.
func (r *TelemetryTransformationRuleReconciler) SetupWithManager(mgr manager.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&dash0v1alpha1.Dash0TelemetryTransformationRule{}).
		WithEventFilter(predicate.Or(generationLabelOrDash0AnnotationChangePredicate, predicate.Funcs{
			UpdateFunc: func(e event.UpdateEvent) bool {
				return !e.ObjectOld.GetDeletionTimestamp().Equal(e.ObjectNew.GetDeletionTimestamp())
			},
		})).
		Complete(r)
}

// InitializeSelfMonitoringMetrics registers the reconcile request counter.
func (r *TelemetryTransformationRuleReconciler) InitializeSelfMonitoringMetrics(
	meter otelmetric.Meter,
	metricNamePrefix string,
	logger logd.Logger,
) {
	name := metricNamePrefix + "telemetrytransformationrule.reconcile_requests"
	var err error
	r.reconcileRequestMetric, err = meter.Int64Counter(name,
		otelmetric.WithUnit("1"),
		otelmetric.WithDescription("Counter for telemetry transformation rule reconcile requests"))
	if err != nil {
		logger.Error(err, fmt.Sprintf("Cannot initialize the metric %s.", name))
	}
}

// KindDisplayName returns the resource name for synchronization messages.
func (r *TelemetryTransformationRuleReconciler) KindDisplayName() string {
	return "telemetry transformation rule"
}

// ShortName returns the resource name used in API messages.
func (r *TelemetryTransformationRuleReconciler) ShortName() string {
	return "telemetry-transformation-rule"
}

// ControllerName returns the controller's log identity.
func (r *TelemetryTransformationRuleReconciler) ControllerName() string {
	return "dash0_telemetry_transformation_rule_controller"
}

// K8sClient returns the Kubernetes client used for synchronization.
func (r *TelemetryTransformationRuleReconciler) K8sClient() client.Client { return r.Client }

// GetDefaultApiConfigs returns the operator-wide API configuration.
func (r *TelemetryTransformationRuleReconciler) GetDefaultApiConfigs() []ApiConfig {
	return r.defaultApiConfigs.Get()
}

// GetNamespacedApiConfigs returns the API configuration selected by a namespace's monitoring resource.
func (r *TelemetryTransformationRuleReconciler) GetNamespacedApiConfigs(namespace string) ([]ApiConfig, bool) {
	return r.namespacedApiConfigs.Get(namespace)
}

// SetDefaultApiConfigs updates the default configuration and starts initial synchronization when possible.
func (r *TelemetryTransformationRuleReconciler) SetDefaultApiConfigs(
	ctx context.Context, configs []ApiConfig, logger logd.Logger,
) {
	previous := r.defaultApiConfigs.Get()
	r.defaultApiConfigs.Set(configs)
	r.initialSyncMutex.Lock()
	started := r.initialSyncStarted
	r.initialSyncMutex.Unlock()
	if started && !slices.Equal(previous, configs) {
		r.synchronizeNamespacedResources(ctx, "", logger)
	} else {
		r.maybeDoInitialSynchronizationOfAllResources(ctx, logger)
	}
}

// RemoveDefaultApiConfigs clears the operator-wide configuration.
func (r *TelemetryTransformationRuleReconciler) RemoveDefaultApiConfigs(_ context.Context, _ logd.Logger) {
	r.defaultApiConfigs.Clear()
}

// SetNamespacedApiConfigs synchronizes resources when their namespace configuration changes.
func (r *TelemetryTransformationRuleReconciler) SetNamespacedApiConfigs(
	ctx context.Context, namespace string, configs []ApiConfig, logger logd.Logger,
) {
	if configs == nil {
		return
	}
	previous, _ := r.namespacedApiConfigs.Get(namespace)
	r.namespacedApiConfigs.Set(namespace, configs)
	if !slices.Equal(previous, configs) {
		r.synchronizeNamespacedResources(ctx, namespace, logger)
	}
}

// RemoveNamespacedApiConfigs restores synchronization through the default configuration.
func (r *TelemetryTransformationRuleReconciler) RemoveNamespacedApiConfigs(
	ctx context.Context, namespace string, logger logd.Logger,
) {
	if _, exists := r.namespacedApiConfigs.Get(namespace); exists {
		r.namespacedApiConfigs.Delete(namespace)
		r.synchronizeNamespacedResources(ctx, namespace, logger)
	}
}

// SetSynchronizationEnabled satisfies NamespacedApiClient; owned resources have no namespace-wide toggle.
func (r *TelemetryTransformationRuleReconciler) SetSynchronizationEnabled(
	_ context.Context, _ string, _ *dash0v1beta1.Dash0Monitoring, _ logd.Logger,
) {
}

// RemoveSynchronizationEnabled satisfies NamespacedApiClient; owned resources have no namespace-wide toggle.
func (r *TelemetryTransformationRuleReconciler) RemoveSynchronizationEnabled(_ string) {}

// NotifyOperatorManagerJustBecameLeader starts synchronization once API configuration is available.
func (r *TelemetryTransformationRuleReconciler) NotifyOperatorManagerJustBecameLeader(
	ctx context.Context, logger logd.Logger,
) {
	r.maybeDoInitialSynchronizationOfAllResources(ctx, logger)
}

func (r *TelemetryTransformationRuleReconciler) maybeDoInitialSynchronizationOfAllResources(
	ctx context.Context, logger logd.Logger,
) {
	r.initialSyncMutex.Lock()
	defer r.initialSyncMutex.Unlock()
	if r.initialSyncStarted || !r.leaderElectionAware.IsLeader() ||
		len(filterValidApiConfigs(r.defaultApiConfigs.Get(), logger, "default operator configuration")) == 0 {
		return
	}
	r.initialSyncStarted = true
	go func() {
		if err := r.synchronizeResources(ctx, ""); err != nil {
			logger.Error(err, "Failed to synchronize telemetry transformation rules on startup.")
			r.initialSyncMutex.Lock()
			r.initialSyncStarted = false
			r.initialSyncMutex.Unlock()
		}
	}()
}

func (r *TelemetryTransformationRuleReconciler) synchronizeNamespacedResources(
	ctx context.Context, namespace string, logger logd.Logger,
) {
	if !r.leaderElectionAware.IsLeader() {
		return
	}
	r.namespacedSyncMutex.Lock(namespace)
	go func() {
		defer r.namespacedSyncMutex.Unlock(namespace)
		if err := r.synchronizeResources(ctx, namespace); err != nil {
			logger.Error(err, "Failed to synchronize telemetry transformation rules.", "namespace", namespace)
		}
	}()
}

func (r *TelemetryTransformationRuleReconciler) synchronizeResources(ctx context.Context, namespace string) error {
	resources := &dash0v1alpha1.Dash0TelemetryTransformationRuleList{}
	if err := r.List(ctx, resources, client.InNamespace(namespace)); err != nil {
		return err
	}
	var syncErrors []error
	for _, resource := range resources.Items {
		if _, err := r.Reconcile(ctx, reconcile.Request{NamespacedName: client.ObjectKeyFromObject(&resource)}); err != nil {
			syncErrors = append(syncErrors, err)
		}
	}
	return errors.Join(syncErrors...)
}

// Reconcile applies the Kubernetes rule and retains its finalizer until remote deletion succeeds.
func (r *TelemetryTransformationRuleReconciler) Reconcile(
	ctx context.Context, req reconcile.Request,
) (reconcile.Result, error) {
	r.reconcileMutex.Lock(req.String())
	defer r.reconcileMutex.Unlock(req.String())
	if r.reconcileRequestMetric != nil {
		r.reconcileRequestMetric.Add(ctx, 1)
	}
	resource := &dash0v1alpha1.Dash0TelemetryTransformationRule{}
	if err := r.Get(ctx, req.NamespacedName, resource); err != nil {
		return reconcile.Result{}, client.IgnoreNotFound(err)
	}
	logger := logd.FromContext(ctx)
	unstructuredResource, err := telemetryTransformationRuleToMap(resource)
	if err != nil {
		return reconcile.Result{}, err
	}
	if !resource.DeletionTimestamp.IsZero() {
		return reconcile.Result{}, r.finalize(ctx, resource, unstructuredResource, logger)
	}
	preconditions := validatePreconditionsAndPreprocess(ctx, r, unstructuredResource, logger)
	if !preconditions.synchronizeResource {
		return reconcile.Result{}, nil
	}
	if controllerutil.AddFinalizer(resource, telemetryTransformationRuleFinalizer) {
		if err := r.Update(ctx, resource); err != nil {
			return reconcile.Result{}, err
		}
	}
	synchronizeViaApiAndUpdateStatus(ctx, r, unstructuredResource, resource, upsertAction, logger)
	return reconcile.Result{}, nil
}

func (r *TelemetryTransformationRuleReconciler) finalize(
	ctx context.Context,
	resource *dash0v1alpha1.Dash0TelemetryTransformationRule,
	unstructuredResource *unstructured.Unstructured,
	logger logd.Logger,
) error {
	if !controllerutil.ContainsFinalizer(resource, telemetryTransformationRuleFinalizer) {
		return nil
	}
	preconditions := validatePreconditionsAndPreprocess(ctx, r, unstructuredResource, logger)
	if !preconditions.synchronizeResource {
		return errors.New("cannot delete remote telemetry transformation rule without monitoring and API configuration")
	}
	results := synchronizationResults{isDeletion: true}
	var deletionErrors []error
	for _, config := range preconditions.validatedApiConfigs {
		requests := r.MapResourceToApiCalls(preconditions, config.ApiConfig, deleteAction, logger)
		successes, syncErrors, statusCodes := executeAllApiCalls(ctx, r, requests.ApiRequests, logger)
		requests.SynchronizationErrors = syncErrors
		requests.SynchronizationErrorStatusCodes = statusCodes
		results.resultsPerApiConfig = append(results.resultsPerApiConfig, synchronizationResultPerApiConfig{
			apiConfig: config.ApiConfig, successfullySynchronized: successes, resourceToRequestsResult: requests,
		})
		for _, message := range syncErrors {
			deletionErrors = append(deletionErrors, errors.New(message))
		}
	}
	if len(deletionErrors) > 0 {
		r.WriteSynchronizationResultToSynchronizedResource(ctx, resource, results, logger)
		return errors.Join(deletionErrors...)
	}
	controllerutil.RemoveFinalizer(resource, telemetryTransformationRuleFinalizer)
	return r.Update(ctx, resource)
}

// Preserve OTLP integers such as timeUnixNano through the intermediate map used by synchronization.
func telemetryTransformationRuleToMap(resource *dash0v1alpha1.Dash0TelemetryTransformationRule) (*unstructured.Unstructured, error) {
	raw, err := json.Marshal(resource)
	if err != nil {
		return nil, err
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	var object map[string]any
	if err := decoder.Decode(&object); err != nil {
		return nil, err
	}
	return &unstructured.Unstructured{Object: object}, nil
}

// MapResourceToApiCalls prepares typed client calls for one rule in one dataset.
func (r *TelemetryTransformationRuleReconciler) MapResourceToApiCalls(
	preconditions *preconditionValidationResult, config ApiConfig, action apiAction, logger logd.Logger,
) *ResourceToRequestsResult {
	origin := fmt.Sprintf("dash0-operator_%s_%s_%s_%s", r.pseudoClusterUid, datasetInOrigin(config.Dataset),
		preconditions.k8sNamespace, preconditions.k8sName)
	requestURL := fmt.Sprintf("%sapi/telemetry-transformation-rules/%s?dataset=%s",
		config.Endpoint, url.PathEscape(origin), url.QueryEscape(config.Dataset))
	call := &ApiClientCall{Method: http.MethodPut, Url: requestURL}
	switch action {
	case upsertAction:
		definition, err := mapToTelemetryTransformationRuleDefinition(preconditions.resource)
		if err != nil {
			return NewResourceToRequestsResultSingleItemConversionError(config, preconditions.k8sName, err.Error())
		}
		definition.Metadata.Labels = &dash0apiclient.TelemetryTransformationRuleLabels{
			Dash0Comorigin: &origin, Dash0Comdataset: &config.Dataset,
		}
		definition.Metadata.Annotations = nil
		call.Execute = func(ctx context.Context) (string, error) {
			apiClient, err := r.apiClientPool.Get(config.Endpoint, config.Token)
			if err != nil {
				return "", err
			}
			return synchronizeTelemetryTransformationRule(ctx, apiClient, origin, definition, config.Dataset)
		}
	case deleteAction:
		call.Method = http.MethodDelete
		call.Execute = func(ctx context.Context) (string, error) {
			apiClient, err := r.apiClientPool.Get(config.Endpoint, config.Token)
			if err != nil {
				return "", err
			}
			return "", apiClient.DeleteTelemetryTransformationRule(ctx, origin, &config.Dataset)
		}
	default:
		logger.Error(fmt.Errorf("unknown API action: %d", action), "Cannot synchronize telemetry transformation rule.")
		return NewResourceToRequestsResultSingleItemError(config, preconditions.k8sName, "unknown API action")
	}
	return NewResourceToRequestsResultSingleItemApiClientCall(config, call, preconditions.k8sName, origin)
}

func mapToTelemetryTransformationRuleDefinition(resource map[string]any) (*dash0apiclient.TelemetryTransformationRuleDefinition, error) {
	raw, err := json.Marshal(resource)
	if err != nil {
		return nil, err
	}
	definition := &dash0apiclient.TelemetryTransformationRuleDefinition{}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	if err := decoder.Decode(definition); err != nil {
		return nil, fmt.Errorf("cannot convert telemetry transformation rule to the Dash0 API format: %w", err)
	}
	definition.Kind = "Dash0TelemetryTransformationRule"
	return definition, nil
}

func synchronizeTelemetryTransformationRule(
	ctx context.Context, apiClient dash0apiclient.Client, origin string,
	definition *dash0apiclient.TelemetryTransformationRuleDefinition, dataset string,
) (string, error) {
	existing, err := apiClient.GetTelemetryTransformationRule(ctx, origin, &dataset)
	if isTelemetryTransformationRuleStatus(err, http.StatusNotFound) {
		created, createErr := apiClient.CreateTelemetryTransformationRule(ctx, definition, &dataset)
		if !isTelemetryTransformationRuleStatus(createErr, http.StatusConflict) {
			return dash0apiclient.GetTelemetryTransformationRuleID(created), createErr
		}
		// GET hides tombstones; PUT revives a reserved origin only in the same dataset.
		existing, err = apiClient.GetTelemetryTransformationRule(ctx, origin, &dataset)
		if isTelemetryTransformationRuleStatus(err, http.StatusNotFound) {
			err = nil
		}
	}
	if err != nil {
		return "", err
	}
	if existing != nil {
		definition.Metadata.Annotations = existing.Metadata.Annotations
		dash0apiclient.StripTelemetryTransformationRuleServerFields(definition)
	}
	updated, err := apiClient.UpdateTelemetryTransformationRule(ctx, origin, definition, &dataset)
	return dash0apiclient.GetTelemetryTransformationRuleID(updated), err
}

func isTelemetryTransformationRuleStatus(err error, status int) bool {
	var apiError *dash0apiclient.APIError
	return errors.As(err, &apiError) && apiError.StatusCode == status
}

// WriteSynchronizationResultToSynchronizedResource records endpoint, dataset, identity, and API errors in status.
func (r *TelemetryTransformationRuleReconciler) WriteSynchronizationResultToSynchronizedResource(
	ctx context.Context, synchronizedResource client.Object, results synchronizationResults, logger logd.Logger,
) {
	resource := synchronizedResource.(*dash0v1alpha1.Dash0TelemetryTransformationRule)
	resource.Status.SynchronizationStatus = results.resourceSyncStatus()
	resource.Status.SynchronizedAt = metav1.Now()
	resource.Status.ValidationIssues = nil
	for _, issues := range results.validationIssues {
		resource.Status.ValidationIssues = append(resource.Status.ValidationIssues, issues...)
	}
	resource.Status.SynchronizationResults = make([]dash0v1alpha1.Dash0TelemetryTransformationRuleSynchronizationResultPerEndpointAndDataset,
		0, len(results.resultsPerApiConfig))
	for _, result := range results.resultsPerApiConfig {
		message, status := firstSynchronizationErrorAndStatusCode(result.resourceToRequestsResult)
		entry := dash0v1alpha1.Dash0TelemetryTransformationRuleSynchronizationResultPerEndpointAndDataset{
			SynchronizationStatus: dash0common.Dash0ApiResourceSynchronizationStatusFailed,
			Dash0ApiEndpoint:      result.apiConfig.Endpoint, Dash0Dataset: result.apiConfig.Dataset,
			SynchronizationError: message, HttpStatusCode: status,
		}
		if message == "" {
			entry.SynchronizationStatus = dash0common.Dash0ApiResourceSynchronizationStatusSuccessful
		}
		if len(result.successfullySynchronized) > 0 {
			entry.Dash0Id = result.successfullySynchronized[0].Labels.Id
			entry.Dash0Origin = result.successfullySynchronized[0].Labels.Origin
		}
		resource.Status.SynchronizationResults = append(resource.Status.SynchronizationResults, entry)
	}
	if err := r.Status().Update(ctx, resource); err != nil {
		logger.Error(err, "Failed to update telemetry transformation rule synchronization status.")
	}
}

// CreateReconcileRequestsForRetryableSyncErrors selects rules whose previous synchronization failed transiently.
func (r *TelemetryTransformationRuleReconciler) CreateReconcileRequestsForRetryableSyncErrors(
	ctx context.Context,
) ([]reconcile.Request, error) {
	resources := &dash0v1alpha1.Dash0TelemetryTransformationRuleList{}
	if err := r.List(ctx, resources); err != nil {
		return nil, err
	}
	var requests []reconcile.Request
	for _, resource := range resources.Items {
		for _, result := range resource.Status.SynchronizationResults {
			if isRetryableSynchronizationError(result.SynchronizationError, result.HttpStatusCode) {
				requests = append(requests, reconcile.Request{NamespacedName: client.ObjectKeyFromObject(&resource)})
				break
			}
		}
	}
	return requests, nil
}
