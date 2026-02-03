// SPDX-FileCopyrightText: Copyright 2025 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package controller

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"reflect"
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
	defaultApiConfig      atomic.Pointer[ApiConfig]
	defaultAuthToken      atomic.Pointer[string]
	namespacedApiConfig   selfmonitoringapiaccess.NamespacedStore[ApiConfig]
	namespacedAuthTokens  selfmonitoringapiaccess.NamespacedStore[string]
	initialSyncMutex      sync.Mutex
	initialSyncHasHappend atomic.Bool
	namespacedSyncMutex   selfmonitoringapiaccess.NamespacedStore[*sync.Mutex]
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
		namespacedApiConfig:  *selfmonitoringapiaccess.NewNamespacedStore[ApiConfig](),
		namespacedAuthTokens: *selfmonitoringapiaccess.NewNamespacedStore[string](),
		httpRetryDelay:       1 * time.Second,
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
	logger *logr.Logger,
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

func (r *SyntheticCheckReconciler) GetDefaultAuthToken() string {
	token := r.defaultAuthToken.Load()
	if token == nil {
		return ""
	}
	return *token
}

func (r *SyntheticCheckReconciler) GetDefaultApiConfig() *atomic.Pointer[ApiConfig] {
	return &r.defaultApiConfig
}

func (r *SyntheticCheckReconciler) GetNamespacedAuthToken(namespace string) (string, bool) {
	return r.namespacedAuthTokens.Get(namespace)
}

func (r *SyntheticCheckReconciler) GetNamespacedApiConfig(namespace string) (ApiConfig, bool) {
	return r.namespacedApiConfig.Get(namespace)
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

func (r *SyntheticCheckReconciler) SetDefaultApiEndpointAndDataset(
	ctx context.Context,
	apiConfig *ApiConfig,
	logger *logr.Logger) {
	r.defaultApiConfig.Store(apiConfig)
	r.maybeDoInitialSynchronizationOfAllResources(ctx, logger)
}

func (r *SyntheticCheckReconciler) RemoveDefaultApiEndpointAndDataset(_ context.Context, _ *logr.Logger) {
	r.defaultApiConfig.Store(nil)
}

func (r *SyntheticCheckReconciler) SetNamespacedApiEndpointAndDataset(ctx context.Context, namespace string, updatedApiConfig *ApiConfig, logger *logr.Logger) {
	if updatedApiConfig != nil {
		previousApiConfig, _ := r.namespacedApiConfig.Get(namespace)

		r.namespacedApiConfig.Set(namespace, *updatedApiConfig)

		if previousApiConfig != *updatedApiConfig {
			r.synchronizeNamespacedResources(ctx, namespace, logger)
		}
	}
}

func (r *SyntheticCheckReconciler) RemoveNamespacedApiEndpointAndDataset(ctx context.Context, namespace string, logger *logr.Logger) {
	if _, exists := r.namespacedApiConfig.Get(namespace); exists {
		r.namespacedApiConfig.Delete(namespace)
		r.synchronizeNamespacedResources(ctx, namespace, logger)
	}
}

func (r *SyntheticCheckReconciler) SetDefaultAuthToken(
	ctx context.Context,
	authToken string,
	logger *logr.Logger) {
	r.defaultAuthToken.Store(&authToken)
	r.maybeDoInitialSynchronizationOfAllResources(ctx, logger)
}

func (r *SyntheticCheckReconciler) RemoveDefaultAuthToken(_ context.Context, _ *logr.Logger) {
	r.defaultAuthToken.Store(nil)
}

func (r *SyntheticCheckReconciler) SetNamespacedAuthToken(ctx context.Context, namespace string, updatedAuthToken string, logger *logr.Logger) {
	previousAuthToken, _ := r.GetNamespacedAuthToken(namespace)
	r.namespacedAuthTokens.Set(namespace, updatedAuthToken)

	if previousAuthToken != updatedAuthToken {
		r.synchronizeNamespacedResources(ctx, namespace, logger)
	}
}

func (r *SyntheticCheckReconciler) RemoveNamespacedAuthToken(ctx context.Context, namespace string, logger *logr.Logger) {
	if _, exists := r.namespacedAuthTokens.Get(namespace); exists {
		r.namespacedAuthTokens.Delete(namespace)
		r.synchronizeNamespacedResources(ctx, namespace, logger)
	}
}

func (r *SyntheticCheckReconciler) NotifiyOperatorManagerJustBecameLeader(ctx context.Context, logger *logr.Logger) {
	r.maybeDoInitialSynchronizationOfAllResources(ctx, logger)
}

func (r *SyntheticCheckReconciler) maybeDoInitialSynchronizationOfAllResources(ctx context.Context, logger *logr.Logger) {
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
			))
		return
	}
	if !isValidApiConfig(r.defaultApiConfig.Load()) {
		logger.Info(
			"Waiting for the Dash0 API endpoint before running initial synchronization of synthetic checks. Either " +
				"no Dash0 API endpoint has been provided via the operator configuration resource, or the operator " +
				"configuration resource has not been reconciled yet. If there is an operator configuration resource " +
				"with an API endpoint and a Dash0 auth token or a secret ref present in the cluster, it will be " +
				"reconciled in a few seconds and this message can be safely ignored.",
		)
		return
	}
	authToken := r.defaultAuthToken.Load()
	if authToken == nil || *authToken == "" {
		logger.Info(
			"Waiting for the Dash0 auth token before running initial synchronization of synthetic checks. Either " +
				"the auth token has not been provided via the operator configuration resource, or the operator " +
				"configuration resource has not been reconciled yet, or it has been provided as a secret reference " +
				"which has not been resolved to a token yet. If there is an operator configuration resource with an " +
				"API endpoint and a Dash0 auth token or a secret ref present in the cluster, it will be reconciled " +
				"and the secret ref (if any) resolved to a token in a few seconds and this message can be safely " +
				"ignored.",
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

func (r *SyntheticCheckReconciler) synchronizeNamespacedResources(ctx context.Context, namespace string, logger *logr.Logger) {
	// nsSyncMutex is used so we don't trigger multiple syncs in parallel in a single namespace.
	// That happens for example when the export from a monitoring resource is removed, since that updates both the API
	// config and auth token at almost the same time, triggering two resyncs.
	nsSyncMutex, exists := r.namespacedSyncMutex.Get(namespace)
	if !exists {
		nsSyncMutex = &sync.Mutex{}
		r.namespacedSyncMutex.Set(namespace, nsSyncMutex)
	}

	nsSyncMutex.Lock()

	if !r.leaderElectionAware.IsLeader() {
		logger.Info(
			fmt.Sprintf(
				"Waiting for the this operator manager replica to become leader before running " +
					"synchronization of synthetic checks.",
			))
		return
	}

	logger.Info(fmt.Sprintf("Running synchronization of synthetic checks in namespace %s now.", namespace))

	go func() {
		defer nsSyncMutex.Unlock()

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
			logger.Error(err,
				fmt.Sprintf(
					"Failed to get the synthetic check \"%s\", requeuing reconcile request.",
					qualifiedName,
				))
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
				dash0common.Dash0ApiResourceSynchronizationStatusFailed,
				Dash0ApiObjectLabels{},
				nil,
				msg,
				&logger,
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
		&logger,
	)

	return reconcile.Result{}, nil
}

func (r *SyntheticCheckReconciler) MapResourceToHttpRequests(
	preconditionChecksResult *preconditionValidationResult,
	action apiAction,
	logger *logr.Logger,
) *ResourceToRequestsResult {
	itemName := preconditionChecksResult.k8sName
	syntheticCheckUrl, syntheticCheckOrigin := r.renderSyntheticCheckUrl(preconditionChecksResult)

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
		return NewResourceToRequestsResultSingleItemError(preconditionChecksResult.validatedApiConfig.ApiConfig, itemName, unknownActionErr.Error())
	}

	if err != nil {
		httpError := fmt.Errorf(
			"unable to create a new HTTP request to synchronize the synthetic check: %s %s: %w",
			method,
			syntheticCheckUrl,
			err,
		)
		logger.Error(httpError, "error creating http request")
		return NewResourceToRequestsResultSingleItemError(preconditionChecksResult.validatedApiConfig.ApiConfig, itemName, httpError.Error())
	}

	addAuthorizationHeader(req, preconditionChecksResult.validatedApiConfig.Token)
	if action == upsertAction {
		req.Header.Set(util.ContentTypeHeaderName, util.ApplicationJsonMediaType)
	}

	return NewResourceToRequestsResultSingleItemSuccess(
		preconditionChecksResult.validatedApiConfig.ApiConfig,
		req,
		itemName,
		syntheticCheckOrigin,
	)
}

func (r *SyntheticCheckReconciler) renderSyntheticCheckUrl(preconditionChecksResult *preconditionValidationResult) (string, string) {
	datasetUrlEncoded := url.QueryEscape(preconditionChecksResult.validatedApiConfig.Dataset)
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
		preconditionChecksResult.validatedApiConfig.Endpoint,
		syntheticCheckOrigin,
		datasetUrlEncoded,
	), syntheticCheckOrigin
}

func (r *SyntheticCheckReconciler) ExtractIdFromResponseBody(
	responseBytes []byte,
	logger *logr.Logger,
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
	status dash0common.Dash0ApiResourceSynchronizationStatus,
	apiObjectLabels Dash0ApiObjectLabels,
	validationIssues []string,
	synchronizationError string,
	logger *logr.Logger,
) {
	syntheticCheck := synchronizedResource.(*dash0v1alpha1.Dash0SyntheticCheck)

	// common result
	syntheticCheck.Status.SynchronizationStatus = status
	syntheticCheck.Status.SynchronizedAt = metav1.Time{Time: time.Now()}
	syntheticCheck.Status.ValidationIssues = validationIssues

	// result(s) per endpoint/dataset combination
	// note: currently there is only one result, but there will be potentially multiple results once we support multi-cast
	syncResultPerEndpointAndDataset := dash0v1alpha1.Dash0SyntheticCheckSynchronizationResultPerEndpointAndDataset{
		SynchronizationStatus: status,
		Dash0ApiEndpoint:      apiObjectLabels.ApiEndpoint,
		Dash0Dataset:          apiObjectLabels.Dataset,
		SynchronizationError:  synchronizationError,
	}
	if apiObjectLabels.Id != "" {
		syncResultPerEndpointAndDataset.Dash0Id = apiObjectLabels.Id
	}
	if apiObjectLabels.Origin != "" {
		syncResultPerEndpointAndDataset.Dash0Origin = apiObjectLabels.Origin
	}
	syntheticCheck.Status.SynchronizationResults = []dash0v1alpha1.Dash0SyntheticCheckSynchronizationResultPerEndpointAndDataset{
		syncResultPerEndpointAndDataset,
	}

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
