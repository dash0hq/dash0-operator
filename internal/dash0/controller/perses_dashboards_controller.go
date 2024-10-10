// SPDX-FileCopyrightText: Copyright 2024 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package controller

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"sync/atomic"

	"github.com/go-logr/logr"
	otelmetric "go.opentelemetry.io/otel/metric"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/util/workqueue"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	"github.com/dash0hq/dash0-operator/internal/dash0/util"
)

type PersesDashboardCrdReconciler struct {
	AuthToken                 string
	mgr                       ctrl.Manager
	skipNameValidation        bool
	persesDashboardReconciler *PersesDashboardReconciler
	persesDashboardCrdExists  atomic.Bool
}

//+kubebuilder:rbac:groups=apiextensions.k8s.io,resources=customresourcedefinitions,verbs=get;list;watch

var (
	persesDashboardCrdReconcileRequestMetric otelmetric.Int64Counter
	persesDashboardReconcileRequestMetric    otelmetric.Int64Counter
)

func (r *PersesDashboardCrdReconciler) Manager() ctrl.Manager {
	return r.mgr
}

func (r *PersesDashboardCrdReconciler) GetAuthToken() string {
	return r.AuthToken
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

func (r *PersesDashboardCrdReconciler) CreateResourceReconciler(
	pseudoClusterUid types.UID,
	authToken string,
	httpClient *http.Client,
) {
	r.persesDashboardReconciler = &PersesDashboardReconciler{
		pseudoClusterUid: pseudoClusterUid,
		authToken:        authToken,
		httpClient:       httpClient,
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
	maybeStartWatchingThirdPartyResources(r, false, &logger)
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

	// Known issue: We would need to stop the watch for the Perses dashboard resources here, but the controller-runtime
	// does not provide any API to stop a watch.
	// An error will be logged every ten seconds until the controller process is restarted.
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
		logger.Error(err, "Cannot initialize the metric %s.")
	}

	r.persesDashboardReconciler.InitializeSelfMonitoringMetrics(
		meter,
		metricNamePrefix,
		logger,
	)
}

func (r *PersesDashboardCrdReconciler) SetApiEndpointAndDataset(
	apiConfig *ApiConfig,
	logger *logr.Logger) {
	if r.persesDashboardReconciler == nil {
		// If no auth token has been set via environment variable, we do not even create the persesDashboardReconciler,
		// hence this nil check is necessary.
		return
	}
	r.persesDashboardReconciler.apiConfig.Store(apiConfig)
	maybeStartWatchingThirdPartyResources(r, false, logger)
}

func (r *PersesDashboardCrdReconciler) RemoveApiEndpointAndDataset() {
	if r.persesDashboardReconciler == nil {
		// If no auth token has been set via environment variable, we do not even create the persesDashboardReconciler,
		// hence this nil check is necessary.
		return
	}
	r.persesDashboardReconciler.apiConfig.Store(nil)
}

type PersesDashboardReconciler struct {
	isWatching       atomic.Bool
	pseudoClusterUid types.UID
	httpClient       *http.Client
	apiConfig        atomic.Pointer[ApiConfig]
	authToken        string
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
		logger.Error(err, "Cannot initialize the metric %s.")
	}
}

func (r *PersesDashboardReconciler) KindDisplayName() string {
	return "Perses dashboard"
}

func (r *PersesDashboardReconciler) ShortName() string {
	return "dashboard"
}

func (r *PersesDashboardReconciler) IsWatching() *atomic.Bool {
	return &r.isWatching
}

func (r *PersesDashboardReconciler) SetIsWatching(isWatching bool) {
	r.isWatching.Store(isWatching)
}

func (r *PersesDashboardReconciler) GetAuthToken() string {
	return r.authToken
}

func (r *PersesDashboardReconciler) GetApiConfig() *atomic.Pointer[ApiConfig] {
	return &r.apiConfig
}

func (r *PersesDashboardReconciler) ControllerName() string {
	return "dash0_perses_dashboard_controller"
}

func (r *PersesDashboardReconciler) HttpClient() *http.Client {
	return r.httpClient
}

func (r *PersesDashboardReconciler) Create(
	ctx context.Context,
	e event.TypedCreateEvent[client.Object],
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

	if err := util.RetryWithCustomBackoff(
		"create dashboard",
		func() error {
			return upsertViaApi(r, e.Object.(*unstructured.Unstructured), &logger)
		},
		retrySettings,
		true,
		&logger,
	); err != nil {
		logger.Error(err, "failed to create the dashboard")
	}
}

func (r *PersesDashboardReconciler) Update(
	ctx context.Context,
	e event.TypedUpdateEvent[client.Object],
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

	if err := util.RetryWithCustomBackoff(
		"update dashboard",
		func() error {
			return upsertViaApi(r, e.ObjectNew.(*unstructured.Unstructured), &logger)
		},
		retrySettings,
		true,
		&logger,
	); err != nil {
		logger.Error(err, "failed to update the dashboard")
	}
}

func (r *PersesDashboardReconciler) Delete(
	ctx context.Context,
	e event.TypedDeleteEvent[client.Object],
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

	if err := util.RetryWithCustomBackoff(
		"delete dashboard",
		func() error {
			return deleteViaApi(r, e.Object.(*unstructured.Unstructured), &logger)
		},
		retrySettings,
		true,
		&logger,
	); err != nil {
		logger.Error(err, "failed to delete the dashboard")
	}
}

func (r *PersesDashboardReconciler) Generic(
	_ context.Context,
	_ event.TypedGenericEvent[client.Object],
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

func (r *PersesDashboardReconciler) MapResourceToHttpRequests(
	preconditionChecksResult *preconditionValidationResult,
	action apiAction,
	logger *logr.Logger,
) ([]*http.Request, error) {
	dashboardUrl := r.renderDashboardUrl(preconditionChecksResult)

	var req *http.Request
	var err error

	//nolint:ineffassign
	actionLabel := "?"
	switch action {
	case upsert:
		actionLabel = "upsert"
		spec := preconditionChecksResult.spec
		displayRaw := spec["display"]
		if displayRaw == nil {
			spec["display"] = map[string]interface{}{}
			displayRaw = spec["display"]
		}
		display, ok := displayRaw.(map[string]interface{})
		if !ok {
			logger.Info("Perses dashboard spec.display is not a map, the dashboard will not be updated in Dash0.")
			return nil, nil
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
				"spec": spec,
			})
		requestPayload := bytes.NewBuffer(serializedDashboard)

		req, err = http.NewRequest(
			http.MethodPut,
			dashboardUrl,
			requestPayload,
		)
	case delete:
		actionLabel = "delete"
		req, err = http.NewRequest(
			http.MethodDelete,
			dashboardUrl,
			nil,
		)
	default:
		logger.Error(fmt.Errorf("unknown API action: %d", action), "unknown API action")
		return nil, nil
	}

	if err != nil {
		logger.Error(err, fmt.Sprintf("unable to create a new HTTP request to %s the dashboard", actionLabel))
		return nil, err
	}

	req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", preconditionChecksResult.authToken))
	if action == upsert {
		req.Header.Set("Content-Type", "application/json")
	}

	return []*http.Request{req}, nil
}

func (r *PersesDashboardReconciler) renderDashboardUrl(preconditionCheckResult *preconditionValidationResult) string {

	dashboardOrigin := fmt.Sprintf(
		// we deliberately use _ as the separator, since that is an illegal character in Kubernetes names. This avoids
		// any potential naming collisions (e.g. namespace="abc" & name="def-ghi" vs. namespace="abc-def" & name="ghi").
		"dash0-operator_%s_%s_%s_%s",
		r.pseudoClusterUid,
		urlEncodePathSegment(preconditionCheckResult.dataset),
		preconditionCheckResult.k8sNamespace,
		preconditionCheckResult.k8sName,
	)
	if !strings.HasSuffix(preconditionCheckResult.apiEndpoint, "/") {
		preconditionCheckResult.apiEndpoint += "/"
	}
	return fmt.Sprintf(
		"%sapi/dashboards/%s?dataset=%s",
		preconditionCheckResult.apiEndpoint,
		dashboardOrigin,
		url.QueryEscape(preconditionCheckResult.dataset),
	)
}
