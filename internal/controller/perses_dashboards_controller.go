// SPDX-FileCopyrightText: Copyright 2024 Dash0 Inc.
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
	"slices"
	"sync"
	"sync/atomic"
	"time"

	"github.com/go-logr/logr"
	otelmetric "go.opentelemetry.io/otel/metric"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/util/workqueue"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	dash0v1alpha1 "github.com/dash0hq/dash0-operator/api/dash0monitoring/v1alpha1"
	"github.com/dash0hq/dash0-operator/internal/startup"
	"github.com/dash0hq/dash0-operator/internal/util"
)

type PersesDashboardCrdReconciler struct {
	client.Client
	queue                     *workqueue.Typed[ThirdPartyResourceSyncJob]
	leaderElectionAware       startup.LeaderElectionAware
	mgr                       ctrl.Manager
	skipNameValidation        bool
	persesDashboardReconciler *PersesDashboardReconciler
	persesDashboardCrdExists  atomic.Bool
}

type PersesDashboardReconciler struct {
	client.Client
	pseudoClusterUID           types.UID
	queue                      *workqueue.Typed[ThirdPartyResourceSyncJob]
	httpClient                 *http.Client
	apiConfig                  atomic.Pointer[ApiConfig]
	authToken                  atomic.Pointer[string]
	httpRetryDelay             time.Duration
	controllerStopFunctionLock sync.Mutex
	controllerStopFunction     *context.CancelFunc
}

var (
	persesDashboardCrdReconcileRequestMetric otelmetric.Int64Counter
	persesDashboardReconcileRequestMetric    otelmetric.Int64Counter
)

func NewPersesDashboardCrdReconciler(
	k8sClient client.Client,
	queue *workqueue.Typed[ThirdPartyResourceSyncJob],
	leaderElectionAware startup.LeaderElectionAware,
) *PersesDashboardCrdReconciler {
	return &PersesDashboardCrdReconciler{
		Client:              k8sClient,
		queue:               queue,
		leaderElectionAware: leaderElectionAware,
	}
}

func (r *PersesDashboardCrdReconciler) Manager() ctrl.Manager {
	return r.mgr
}

func (r *PersesDashboardCrdReconciler) KindDisplayName() string {
	return "Perses dashboard"
}

func (r *PersesDashboardCrdReconciler) Group() string {
	return "perses.dev"
}

func (r *PersesDashboardCrdReconciler) Kind() string {
	return "PersesDashboard"
}

func (r *PersesDashboardCrdReconciler) Version() string {
	return "v1alpha1"
}

func (r *PersesDashboardCrdReconciler) QualifiedKind() string {
	return "persesdashboards.perses.dev"
}

func (r *PersesDashboardCrdReconciler) ControllerName() string {
	return "dash0_perses_dashboard_crd_controller"
}

func (r *PersesDashboardCrdReconciler) DoesCrdExist() *atomic.Bool {
	return &r.persesDashboardCrdExists
}

func (r *PersesDashboardCrdReconciler) SetCrdExists(exists bool) {
	r.persesDashboardCrdExists.Store(exists)
}

func (r *PersesDashboardCrdReconciler) SkipNameValidation() bool {
	return r.skipNameValidation
}

func (r *PersesDashboardCrdReconciler) OperatorManagerIsLeader() bool {
	return r.leaderElectionAware.IsLeader()
}

func (r *PersesDashboardCrdReconciler) CreateResourceReconciler(
	pseudoClusterUid types.UID,
	httpClient *http.Client,
) {
	r.persesDashboardReconciler = &PersesDashboardReconciler{
		Client:           r.Client,
		queue:            r.queue,
		pseudoClusterUID: pseudoClusterUid,
		httpClient:       httpClient,
		httpRetryDelay:   1 * time.Second,
	}
}

func (r *PersesDashboardCrdReconciler) ResourceReconciler() ThirdPartyResourceReconciler {
	return r.persesDashboardReconciler
}

func (r *PersesDashboardCrdReconciler) SetupWithManager(
	ctx context.Context,
	mgr ctrl.Manager,
	startupK8sClient client.Client,
	logger *logr.Logger,
) error {
	r.mgr = mgr
	return SetupThirdPartyCrdReconcilerWithManager(
		ctx,
		startupK8sClient,
		r,
		logger,
	)
}

//+kubebuilder:rbac:groups=perses.dev,resources=persesdashboards,verbs=get;list;watch

func (r *PersesDashboardCrdReconciler) Create(
	ctx context.Context,
	_ event.TypedCreateEvent[client.Object],
	_ workqueue.TypedRateLimitingInterface[reconcile.Request],
) {
	if persesDashboardCrdReconcileRequestMetric != nil {
		persesDashboardCrdReconcileRequestMetric.Add(ctx, 1)
	}
	logger := log.FromContext(ctx)
	r.persesDashboardCrdExists.Store(true)
	maybeStartWatchingThirdPartyResources(r, &logger)
}

func (r *PersesDashboardCrdReconciler) Update(
	context.Context,
	event.TypedUpdateEvent[client.Object],
	workqueue.TypedRateLimitingInterface[reconcile.Request],
) {
	// should not be called, we are not interested in updates
	// note: update is called twice prior to delete, it is also called twice after an actual create
}

func (r *PersesDashboardCrdReconciler) Delete(
	ctx context.Context,
	_ event.TypedDeleteEvent[client.Object],
	_ workqueue.TypedRateLimitingInterface[reconcile.Request],
) {
	if persesDashboardCrdReconcileRequestMetric != nil {
		persesDashboardCrdReconcileRequestMetric.Add(ctx, 1)
	}
	logger := log.FromContext(ctx)
	logger.Info("The PersesDashboard custom resource definition has been deleted.")
	r.persesDashboardCrdExists.Store(false)

	stopWatchingThirdPartyResources(ctx, r, &logger)
}

func (r *PersesDashboardCrdReconciler) Generic(
	context.Context,
	event.TypedGenericEvent[client.Object],
	workqueue.TypedRateLimitingInterface[reconcile.Request],
) {
	// Should not be called, we are not interested in generic events.
}

func (r *PersesDashboardCrdReconciler) Reconcile(
	_ context.Context,
	_ reconcile.Request,
) (reconcile.Result, error) {
	// Reconcile should not be called for the PersesDashboardCrdReconciler CRD, as we are using the
	// TypedEventHandler interface directly when setting up the watch. We still need to implement the method, as the
	// controller builder's Complete method requires implementing the Reconciler interface.
	return reconcile.Result{}, nil
}

func (r *PersesDashboardCrdReconciler) InitializeSelfMonitoringMetrics(
	meter otelmetric.Meter,
	metricNamePrefix string,
	logger *logr.Logger,
) {
	reconcileRequestMetricName := fmt.Sprintf("%s%s", metricNamePrefix, "persesdashboardcrd.reconcile_requests")
	var err error
	if persesDashboardCrdReconcileRequestMetric, err = meter.Int64Counter(
		reconcileRequestMetricName,
		otelmetric.WithUnit("1"),
		otelmetric.WithDescription("Counter for persesdashboard CRD reconcile requests"),
	); err != nil {
		logger.Error(err, fmt.Sprintf("Cannot initialize the metric %s.", reconcileRequestMetricName))
	}

	r.persesDashboardReconciler.InitializeSelfMonitoringMetrics(
		meter,
		metricNamePrefix,
		logger,
	)
}

func (r *PersesDashboardCrdReconciler) SetApiEndpointAndDataset(
	ctx context.Context,
	apiConfig *ApiConfig,
	logger *logr.Logger) {
	r.persesDashboardReconciler.apiConfig.Store(apiConfig)
	if isValidApiConfig(apiConfig) {
		maybeStartWatchingThirdPartyResources(r, logger)
	} else {
		stopWatchingThirdPartyResources(ctx, r, logger)
	}
}

func (r *PersesDashboardCrdReconciler) RemoveApiEndpointAndDataset(ctx context.Context, logger *logr.Logger) {
	r.persesDashboardReconciler.apiConfig.Store(nil)
	stopWatchingThirdPartyResources(ctx, r, logger)
}

func (r *PersesDashboardCrdReconciler) SetAuthToken(
	ctx context.Context,
	authToken string,
	logger *logr.Logger) {
	r.persesDashboardReconciler.authToken.Store(&authToken)
	if authToken != "" {
		maybeStartWatchingThirdPartyResources(r, logger)
	} else {
		stopWatchingThirdPartyResources(ctx, r, logger)
	}
}

func (r *PersesDashboardCrdReconciler) RemoveAuthToken(ctx context.Context, logger *logr.Logger) {
	r.persesDashboardReconciler.authToken.Store(nil)
	stopWatchingThirdPartyResources(ctx, r, logger)
}

func (r *PersesDashboardReconciler) InitializeSelfMonitoringMetrics(
	meter otelmetric.Meter,
	metricNamePrefix string,
	logger *logr.Logger,
) {
	reconcileRequestMetricName := fmt.Sprintf("%s%s", metricNamePrefix, "persesdashboard.reconcile_requests")
	var err error
	if persesDashboardReconcileRequestMetric, err = meter.Int64Counter(
		reconcileRequestMetricName,
		otelmetric.WithUnit("1"),
		otelmetric.WithDescription("Counter for perses dashboard reconcile requests"),
	); err != nil {
		logger.Error(err, fmt.Sprintf("Cannot initialize the metric %s.", reconcileRequestMetricName))
	}
}

func (r *PersesDashboardReconciler) KindDisplayName() string {
	return "Perses dashboard"
}

func (r *PersesDashboardReconciler) ShortName() string {
	return "dashboard"
}

func (r *PersesDashboardReconciler) ControllerStopFunctionLock() *sync.Mutex {
	return &r.controllerStopFunctionLock
}

func (r *PersesDashboardReconciler) GetControllerStopFunction() *context.CancelFunc {
	return r.controllerStopFunction
}

func (r *PersesDashboardReconciler) SetControllerStopFunction(controllerStopFunction *context.CancelFunc) {
	r.controllerStopFunction = controllerStopFunction
}

func (r *PersesDashboardReconciler) IsWatching() bool {
	return r.controllerStopFunction != nil
}

func (r *PersesDashboardReconciler) GetAuthToken() string {
	token := r.authToken.Load()
	if token == nil {
		return ""
	}
	return *token
}

func (r *PersesDashboardReconciler) GetApiConfig() *atomic.Pointer[ApiConfig] {
	return &r.apiConfig
}

func (r *PersesDashboardReconciler) ControllerName() string {
	return "dash0_perses_dashboard_controller"
}

func (r *PersesDashboardReconciler) K8sClient() client.Client {
	return r.Client
}

func (r *PersesDashboardReconciler) Queue() *workqueue.Typed[ThirdPartyResourceSyncJob] {
	return r.queue
}

func (r *PersesDashboardReconciler) HttpClient() *http.Client {
	return r.httpClient
}

func (r *PersesDashboardReconciler) GetHttpRetryDelay() time.Duration {
	return r.httpRetryDelay
}

func (r *PersesDashboardReconciler) overrideHttpRetryDelay(delay time.Duration) {
	r.httpRetryDelay = delay
}

func (r *PersesDashboardReconciler) IsSynchronizationEnabled(monitoringResource *dash0v1alpha1.Dash0Monitoring) bool {
	if monitoringResource == nil {
		return false
	}
	boolPtr := monitoringResource.Spec.SynchronizePersesDashboards
	if boolPtr == nil {
		return true
	}
	return *boolPtr
}

func (r *PersesDashboardReconciler) Create(
	ctx context.Context,
	e event.TypedCreateEvent[*unstructured.Unstructured],
	_ workqueue.TypedRateLimitingInterface[reconcile.Request],
) {
	if persesDashboardReconcileRequestMetric != nil {
		persesDashboardReconcileRequestMetric.Add(ctx, 1)
	}

	logger := log.FromContext(ctx)
	logger.Info(
		"Detected a new Perses dashboard resource",
		"namespace",
		e.Object.GetNamespace(),
		"name",
		e.Object.GetName(),
	)

	upsertViaApi(r, e.Object)
}

func (r *PersesDashboardReconciler) Update(
	ctx context.Context,
	e event.TypedUpdateEvent[*unstructured.Unstructured],
	_ workqueue.TypedRateLimitingInterface[reconcile.Request],
) {
	if persesDashboardReconcileRequestMetric != nil {
		persesDashboardReconcileRequestMetric.Add(ctx, 1)
	}

	logger := log.FromContext(ctx)
	logger.Info(
		"Detected a change for a Perses dashboard resource",
		"namespace",
		e.ObjectNew.GetNamespace(),
		"name",
		e.ObjectNew.GetName(),
	)

	upsertViaApi(r, e.ObjectNew)
}

func (r *PersesDashboardReconciler) Delete(
	ctx context.Context,
	e event.TypedDeleteEvent[*unstructured.Unstructured],
	_ workqueue.TypedRateLimitingInterface[reconcile.Request],
) {
	if persesDashboardReconcileRequestMetric != nil {
		persesDashboardReconcileRequestMetric.Add(ctx, 1)
	}

	logger := log.FromContext(ctx)
	logger.Info(
		"Detected the deletion of a Perses dashboard resource",
		"namespace",
		e.Object.GetNamespace(),
		"name",
		e.Object.GetName(),
	)

	deleteViaApi(r, e.Object)
}

func (r *PersesDashboardReconciler) Generic(
	_ context.Context,
	_ event.TypedGenericEvent[*unstructured.Unstructured],
	_ workqueue.TypedRateLimitingInterface[reconcile.Request],
) {
	// ignoring generic events
}

func (r *PersesDashboardReconciler) Reconcile(
	context.Context,
	reconcile.Request,
) (reconcile.Result, error) {
	// Reconcile should not be called on the PersesDashboardReconciler, as we are using the TypedEventHandler interface
	// directly when setting up the watch. We still need to implement the method, as the controller builder's Complete
	// method requires implementing the Reconciler interface.
	return reconcile.Result{}, nil
}

func (r *PersesDashboardReconciler) FetchExistingResourceIdsRequest(
	_ *preconditionValidationResult,
) (*http.Request, error) {
	// The mechanism to delete individual dashboards when synchronizing one Kubernetes PersesDashboard resource is not
	// required, since each PersesDashboard only contains one dashboard. It is only needed when the resource type holds
	// multiple objects that are synchronized (as it is the case for PrometheusRule). Thus, this controller does not
	// need to implement this method.
	return nil, nil
}

func (r *PersesDashboardReconciler) MapResourceToHttpRequests(
	preconditionChecksResult *preconditionValidationResult,
	action apiAction,
	logger *logr.Logger,
) (int, []HttpRequestWithItemName, []string, map[string][]string, map[string]string) {
	itemName := preconditionChecksResult.k8sName
	dashboardUrl := r.renderDashboardUrl(preconditionChecksResult)

	var req *http.Request
	var method string
	var err error

	//nolint:ineffassign
	switch action {
	case upsert:
		specOrConfig := preconditionChecksResult.thirdPartyResourceSpec

		configRaw := specOrConfig["config"]
		if configRaw != nil {
			// See https://github.com/perses/perses-operator/pull/128, the CRD spec has been changed, a new wrapper
			// object "config" has been added around the dashboard spec.
			if config, ok := configRaw.(map[string]interface{}); ok {
				specOrConfig = config
			}
		}

		displayRaw := specOrConfig["display"]
		if displayRaw == nil {
			specOrConfig["display"] = map[string]interface{}{}
			displayRaw = specOrConfig["display"]
		}
		display, ok := displayRaw.(map[string]interface{})
		if !ok {
			logger.Info("Perses dashboard spec.display is not a map, the dashboard will not be updated in Dash0.")
			return 1,
				nil,
				nil,
				map[string][]string{
					itemName: {"spec.display is not a map"},
				},
				nil
		}
		displayName, ok := display["name"]
		if !ok || displayName == "" {
			// Let the dashboard name default to the perses dashboard resource's namespace + name, if unset.
			display["name"] = fmt.Sprintf("%s/%s", preconditionChecksResult.k8sNamespace, preconditionChecksResult.k8sName)
		}

		// Remove all unnecessary metadata (labels & annotations), we basically only need the dashboard spec.
		serializedDashboard, _ := json.Marshal(
			map[string]interface{}{
				"kind": "PersesDashboard",
				"spec": specOrConfig,
			})
		requestPayload := bytes.NewBuffer(serializedDashboard)

		method = http.MethodPut
		req, err = http.NewRequest(
			method,
			dashboardUrl,
			requestPayload,
		)
	case delete:
		method = http.MethodDelete
		req, err = http.NewRequest(
			method,
			dashboardUrl,
			nil,
		)
	default:
		unknownActionErr := fmt.Errorf("unknown API action: %d", action)
		logger.Error(unknownActionErr, "unknown API action")
		return 1, nil, nil, nil, map[string]string{itemName: unknownActionErr.Error()}
	}

	if err != nil {
		httpError := fmt.Errorf(
			"unable to create a new HTTP request to synchronize the dashboard: %s %s: %w",
			method,
			dashboardUrl,
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

func (r *PersesDashboardReconciler) renderDashboardUrl(preconditionChecksResult *preconditionValidationResult) string {
	datasetUrlEncoded := url.QueryEscape(preconditionChecksResult.dataset)
	dashboardId := fmt.Sprintf(
		// we deliberately use _ as the separator, since that is an illegal character in Kubernetes names. This avoids
		// any potential naming collisions (e.g. namespace="abc" & name="def-ghi" vs. namespace="abc-def" & name="ghi").
		"dash0-operator_%s_%s_%s_%s",
		r.pseudoClusterUID,
		datasetUrlEncoded,
		preconditionChecksResult.k8sNamespace,
		preconditionChecksResult.k8sName,
	)
	return fmt.Sprintf(
		"%sapi/dashboards/%s?dataset=%s",
		preconditionChecksResult.apiEndpoint,
		dashboardId,
		datasetUrlEncoded,
	)
}

func (r *PersesDashboardReconciler) CreateDeleteRequests(
	_ *preconditionValidationResult,
	_ []string,
	_ []string,
	_ *logr.Logger,
) ([]HttpRequestWithItemName, map[string]string) {
	// The mechanism to delete individual dashboards when synchronizing one Kubernetes PersesDashboard resource is not
	// required, since each PersesDashboard only contains one dashboard. It is only needed when the resource type holds
	// multiple objects that are synchronized (as it is the case for PrometheusRule). Thus, this controller does not
	// need to implement this method.
	return nil, nil
}

func (r *PersesDashboardReconciler) UpdateSynchronizationResultsInStatus(
	monitoringResource *dash0v1alpha1.Dash0Monitoring,
	qualifiedName string,
	status dash0v1alpha1.SynchronizationStatus,
	_ int,
	_ []string,
	synchronizationErrors map[string]string,
	validationIssuesMap map[string][]string,
) interface{} {
	previousResults := monitoringResource.Status.PersesDashboardSynchronizationResults
	if previousResults == nil {
		previousResults = make(map[string]dash0v1alpha1.PersesDashboardSynchronizationResults)
		monitoringResource.Status.PersesDashboardSynchronizationResults = previousResults
	}

	// A Perses dashboard resource can only contain one dashboard, so its SynchronizationResults struct is considerably
	// simpler than the PrometheusRuleSynchronizationResults struct.
	result := dash0v1alpha1.PersesDashboardSynchronizationResults{
		SynchronizedAt:        metav1.Time{Time: time.Now()},
		SynchronizationStatus: status,
	}
	if len(synchronizationErrors) > 0 {
		// there can only be at most one synchronization error for a Perses dashboard resource
		result.SynchronizationError = slices.Collect(maps.Values(synchronizationErrors))[0]
	}
	if len(validationIssuesMap) > 0 {
		// there can only be at most one list of validation issues for a Perses dashboard resource
		result.ValidationIssues = slices.Collect(maps.Values(validationIssuesMap))[0]
	}
	previousResults[qualifiedName] = result
	return result
}
