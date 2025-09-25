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
	"github.com/dash0hq/dash0-operator/internal/util"
)

type ViewReconciler struct {
	client.Client
	pseudoClusterUid      types.UID
	leaderElectionAware   util.LeaderElectionAware
	httpClient            *http.Client
	apiConfig             atomic.Pointer[ApiConfig]
	authToken             atomic.Pointer[string]
	initialSyncMutex      sync.Mutex
	initialSyncHasHappend atomic.Bool
	httpRetryDelay        time.Duration
}

var (
	viewReconcileRequestMetric otelmetric.Int64Counter
)

func NewViewReconciler(
	k8sClient client.Client,
	pseudoClusterUid types.UID,
	leaderElectionAware util.LeaderElectionAware,
	httpClient *http.Client,
) *ViewReconciler {
	return &ViewReconciler{
		Client:              k8sClient,
		pseudoClusterUid:    pseudoClusterUid,
		leaderElectionAware: leaderElectionAware,
		httpClient:          httpClient,
		httpRetryDelay:      1 * time.Second,
	}
}

func (r *ViewReconciler) SetupWithManager(mgr manager.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&dash0v1alpha1.Dash0View{}).
		// ignore changes in the status subresource, but react on changes to spec, label and annotations
		WithEventFilter(viewPredicate{}).
		Complete(r)
}

func (r *ViewReconciler) InitializeSelfMonitoringMetrics(
	meter otelmetric.Meter,
	metricNamePrefix string,
	logger *logr.Logger,
) {
	reconcileRequestMetricName := fmt.Sprintf("%s%s", metricNamePrefix, "view.reconcile_requests")
	var err error
	if viewReconcileRequestMetric, err = meter.Int64Counter(
		reconcileRequestMetricName,
		otelmetric.WithUnit("1"),
		otelmetric.WithDescription("Counter for view reconcile requests"),
	); err != nil {
		logger.Error(err, fmt.Sprintf("Cannot initialize the metric %s.", reconcileRequestMetricName))
	}
}

func (r *ViewReconciler) KindDisplayName() string {
	return "view"
}

func (r *ViewReconciler) ShortName() string {
	return "view"
}

func (r *ViewReconciler) GetAuthToken() string {
	token := r.authToken.Load()
	if token == nil {
		return ""
	}
	return *token
}

func (r *ViewReconciler) GetApiConfig() *atomic.Pointer[ApiConfig] {
	return &r.apiConfig
}

func (r *ViewReconciler) ControllerName() string {
	return "dash0_view_controller"
}

func (r *ViewReconciler) K8sClient() client.Client {
	return r.Client
}

func (r *ViewReconciler) HttpClient() *http.Client {
	return r.httpClient
}

func (r *ViewReconciler) GetHttpRetryDelay() time.Duration {
	return r.httpRetryDelay
}

//nolint:unused
func (r *ViewReconciler) overrideHttpRetryDelay(delay time.Duration) {
	r.httpRetryDelay = delay
}

func (r *ViewReconciler) SetApiEndpointAndDataset(
	ctx context.Context,
	apiConfig *ApiConfig,
	logger *logr.Logger) {
	r.apiConfig.Store(apiConfig)
	r.maybeDoInitialSynchronizationOfAllResources(ctx, logger)
}

func (r *ViewReconciler) RemoveApiEndpointAndDataset(_ context.Context, _ *logr.Logger) {
	r.apiConfig.Store(nil)
}

func (r *ViewReconciler) SetAuthToken(
	ctx context.Context,
	authToken string,
	logger *logr.Logger) {
	r.authToken.Store(&authToken)
	r.maybeDoInitialSynchronizationOfAllResources(ctx, logger)
}

func (r *ViewReconciler) RemoveAuthToken(_ context.Context, _ *logr.Logger) {
	r.authToken.Store(nil)
}

func (r *ViewReconciler) NotifiyOperatorManagerJustBecameLeader(ctx context.Context, logger *logr.Logger) {
	r.maybeDoInitialSynchronizationOfAllResources(ctx, logger)
}

func (r *ViewReconciler) maybeDoInitialSynchronizationOfAllResources(ctx context.Context, logger *logr.Logger) {
	r.initialSyncMutex.Lock()
	defer r.initialSyncMutex.Unlock()

	if r.initialSyncHasHappend.Load() {
		return
	}

	if !r.leaderElectionAware.IsLeader() {
		logger.Info(
			fmt.Sprintf(
				"Waiting for the this operator manager replica to become leader before running initial " +
					"synchronization of views.",
			))
		return
	}
	if !isValidApiConfig(r.apiConfig.Load()) {
		logger.Info(
			"Waiting for the Dash0 API endpoint before running initial synchronization of views. Either " +
				"no Dash0 API endpoint has been provided via the operator configuration resource, or the operator " +
				"configuration resource has not been reconciled yet. If there is an operator configuration resource " +
				"with an API endpoint and a Dash0 auth token or a secret ref present in the cluster, it will be " +
				"reconciled in a few seconds and this message can be safely ignored.",
		)
		return
	}
	authToken := r.authToken.Load()
	if authToken == nil || *authToken == "" {
		logger.Info(
			"Waiting for the Dash0 auth token before running initial synchronization of views. Either " +
				"the auth token has not been provided via the operator configuration resource, or the operator " +
				"configuration resource has not been reconciled yet, or it has been provided as a secret reference " +
				"which has not been resolved to a token yet. If there is an operator configuration resource with an " +
				"API endpoint and a Dash0 auth token or a secret ref present in the cluster, it will be reconciled " +
				"and the secret ref (if any) resolved to a token in a few seconds and this message can be safely " +
				"ignored.",
		)
		return
	}

	logger.Info("Running initial synchronization of views now.")

	go func() {
		allViewResourcesInCluster := dash0v1alpha1.Dash0ViewList{}
		if err := r.List(
			ctx,
			&allViewResourcesInCluster,
			&client.ListOptions{},
		); err != nil {
			logger.Error(err, "Failed to list all Dash0 view resources.")
			return
		}

		for _, viewResource := range allViewResourcesInCluster.Items {
			pseudoReconcileRequest := ctrl.Request{
				NamespacedName: client.ObjectKey{
					Namespace: viewResource.Namespace,
					Name:      viewResource.Name,
				},
			}
			_, _ = r.Reconcile(ctx, pseudoReconcileRequest)
			// stagger API requests a bit
			time.Sleep(50 * time.Millisecond)
		}
		logger.Info("Initial synchronization of views has finished.")
		r.initialSyncHasHappend.Store(true)
	}()
}

func (r *ViewReconciler) Reconcile(ctx context.Context, req reconcile.Request) (reconcile.Result, error) {
	if viewReconcileRequestMetric != nil {
		viewReconcileRequestMetric.Add(ctx, 1)
	}

	qualifiedName := req.NamespacedName.String() //nolint:staticcheck
	logger := log.FromContext(ctx)
	logger.Info("processing reconcile request for a view resource", "name", qualifiedName)

	action := upsert
	viewResource := &dash0v1alpha1.Dash0View{}
	if err := r.Get(ctx, req.NamespacedName, viewResource); err != nil {
		if apierrors.IsNotFound(err) {
			action = delete
			logger.Info("reconciling the deletion of the view resource", "name", qualifiedName)
			viewResource = &dash0v1alpha1.Dash0View{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: req.Namespace,
					Name:      req.Name,
				},
			}
		} else {
			logger.Error(err,
				fmt.Sprintf(
					"Failed to get the view \"%s\", requeuing reconcile request.",
					qualifiedName,
				))
			return ctrl.Result{}, err
		}
	}

	unstructuredResource, err := structToMap(viewResource)
	if err != nil {
		msg := "cannot serialize the view resource"
		logger.Error(err, msg)
		if action != delete {
			r.WriteSynchronizationResultToSynchronizedResource(
				ctx,
				viewResource,
				dash0common.Dash0ApiResourceSynchronizationStatusFailed,
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
		viewResource,
		action,
		&logger,
	)

	return reconcile.Result{}, nil
}

func (r *ViewReconciler) MapResourceToHttpRequests(
	preconditionChecksResult *preconditionValidationResult,
	action apiAction,
	logger *logr.Logger,
) (int, []HttpRequestWithItemName, []string, map[string][]string, map[string]string) {
	itemName := preconditionChecksResult.k8sName
	viewUrl := r.renderViewUrl(preconditionChecksResult)

	var req *http.Request
	var method string
	var err error

	switch action {
	case upsert:
		spec := preconditionChecksResult.dash0ApiResourceSpec

		// Remove all unnecessary metadata (labels & annotations), we basically only need the view spec.
		serializedView, _ := json.Marshal(
			map[string]interface{}{
				"kind": "View",
				"spec": spec,
			})
		requestPayload := bytes.NewBuffer(serializedView)

		method = http.MethodPut
		req, err = http.NewRequest(
			method,
			viewUrl,
			requestPayload,
		)
	case delete:
		method = http.MethodDelete
		req, err = http.NewRequest(
			method,
			viewUrl,
			nil,
		)
	default:
		unknownActionErr := fmt.Errorf("unknown API action: %d", action)
		logger.Error(unknownActionErr, "unknown API action")
		return 1, nil, nil, nil, map[string]string{itemName: unknownActionErr.Error()}
	}

	if err != nil {
		httpError := fmt.Errorf(
			"unable to create a new HTTP request to synchronize the view: %s %s: %w",
			method,
			viewUrl,
			err,
		)
		logger.Error(httpError, "error creating http request")
		return 1, nil, nil, nil, map[string]string{itemName: httpError.Error()}
	}

	addAuthorizationHeader(req, preconditionChecksResult)
	if action == upsert {
		req.Header.Set(util.ContentTypeHeaderName, util.ApplicationJsonMediaType)
	}

	return 1, []HttpRequestWithItemName{{
		ItemName: itemName,
		Request:  req,
	}}, nil, nil, nil
}

func (r *ViewReconciler) renderViewUrl(preconditionChecksResult *preconditionValidationResult) string {
	datasetUrlEncoded := url.QueryEscape(preconditionChecksResult.dataset)
	viewOrigin := fmt.Sprintf(
		// we deliberately use _ as the separator, since that is an illegal character in Kubernetes names. This avoids
		// any potential naming collisions (e.g. namespace="abc" & name="def-ghi" vs. namespace="abc-def" & name="ghi").
		"dash0-operator_%s_%s_%s_%s",
		r.pseudoClusterUid,
		datasetUrlEncoded,
		preconditionChecksResult.k8sNamespace,
		preconditionChecksResult.k8sName,
	)
	return fmt.Sprintf(
		"%sapi/views/%s?dataset=%s",
		preconditionChecksResult.apiEndpoint,
		viewOrigin,
		datasetUrlEncoded,
	)
}

func (r *ViewReconciler) WriteSynchronizationResultToSynchronizedResource(
	ctx context.Context,
	synchronizedResource client.Object,
	status dash0common.Dash0ApiResourceSynchronizationStatus,
	validationIssues []string,
	synchronizationError string,
	logger *logr.Logger,
) {
	view := synchronizedResource.(*dash0v1alpha1.Dash0View)
	view.Status.SynchronizationStatus = status
	view.Status.SynchronizedAt = metav1.Time{Time: time.Now()}
	view.Status.SynchronizationError = synchronizationError
	view.Status.ValidationIssues = validationIssues
	if err := r.Status().Update(ctx, view); err != nil {
		logger.Error(err, "Failed to update Dash0 view status.")
	}
}

// An event filter that ignores changes in the status subresource but reacts on changes to spec, label and annotations.
// Ideally we would just use predicate.GenerationChangedPredicate, but it unfortunately also ignores label and
// annotation changes. This is necessary because we update the status subresource when reconciling the resource, and
// without the filter this would cause another no-op reconcile request.
type viewPredicate struct {
	predicate.Funcs
}

func (p viewPredicate) CreateFunc(_ event.CreateEvent) bool {
	return true
}

func (p viewPredicate) DeleteFunc(_ event.DeleteEvent) bool {
	return true
}

func (p viewPredicate) Update(e event.UpdateEvent) bool {
	if e.ObjectOld == nil || e.ObjectNew == nil {
		return true
	}

	oldObj, okOld := e.ObjectOld.(*dash0v1alpha1.Dash0View)
	newObj, okNew := e.ObjectNew.(*dash0v1alpha1.Dash0View)

	if !okOld || !okNew {
		return true
	}

	specChanged := !reflect.DeepEqual(oldObj.Spec, newObj.Spec)
	labelsChanged := !reflect.DeepEqual(oldObj.Labels, newObj.Labels)
	annotationsChanged := !reflect.DeepEqual(oldObj.Annotations, newObj.Annotations)

	return specChanged || labelsChanged || annotationsChanged
}

func (p viewPredicate) GenericFunc(_ event.GenericEvent) bool {
	return true
}
