// SPDX-FileCopyrightText: Copyright 2024 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package controller

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
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
	// persesDashboardCrdReconcileRequestMetric otelmetric.Int64Counter
	persesDashboardReconcileRequestMetric otelmetric.Int64Counter
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
	// Note: The persesDashboardCrdReconcileRequestMetric is unused until we actually implement watching the
	// PersesDashboard _CRD_, see comment above in SetupWithManager.

	// reconcileRequestMetricName := fmt.Sprintf("%s%s", metricNamePrefix, "persesdashboardcrd.reconcile_requests")
	// var err error
	// if persesDashboardCrdReconcileRequestMetric, err = meter.Int64Counter(
	// 	reconcileRequestMetricName,
	// 	otelmetric.WithUnit("1"),
	// 	otelmetric.WithDescription("Counter for persesdashboard CRD reconcile requests"),
	// ); err != nil {
	// 	logger.Error(err, "Cannot initialize the metric %s.")
	// }

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

func (r *PersesDashboardReconciler) IsWatching() *atomic.Bool {
	return &r.isWatching
}

func (r *PersesDashboardReconciler) SetIsWatching(isWatching bool) {
	r.isWatching.Store(isWatching)
}

func (r *PersesDashboardReconciler) GetApiConfig() *atomic.Pointer[ApiConfig] {
	return &r.apiConfig
}

func (r *PersesDashboardReconciler) ControllerName() string {
	return "dash0_perses_dashboard_controller"
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
	if err := r.UpsertDashboard(e.Object.(*unstructured.Unstructured), &logger); err != nil {
		logger.Error(err, "unable to upsert the dashboard")
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

	_ = util.RetryWithCustomBackoff(
		"upsert dashboard",
		func() error {
			return r.UpsertDashboard(e.ObjectNew.(*unstructured.Unstructured), &logger)
		},
		retrySettings,
		true,
		&logger,
	)
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

	_ = util.RetryWithCustomBackoff(
		"delete dashboard",
		func() error {
			return r.DeleteDashboard(e.Object.(*unstructured.Unstructured), &logger)
		},
		retrySettings,
		true,
		&logger,
	)
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

func (r *PersesDashboardReconciler) UpsertDashboard(
	persesDashboard *unstructured.Unstructured,
	logger *logr.Logger,
) error {
	apiConfig := r.apiConfig.Load()
	valResult, executeRequest := r.validateConfigAndRenderUrl(
		persesDashboard,
		apiConfig,
		logger,
	)
	if !executeRequest {
		return nil
	}

	specRaw := persesDashboard.Object["spec"]
	if specRaw == nil {
		logger.Info("Perses dashboard has no spec, the dashboard will not be updated in Dash0.")
		return nil
	}
	spec, ok := specRaw.(map[string]interface{})
	if !ok {
		logger.Info("Perses dashboard spec is not a map, the dashboard will not be updated in Dash0.")
		return nil
	}
	displayRaw := spec["display"]
	if displayRaw == nil {
		spec["display"] = map[string]interface{}{}
		displayRaw = spec["display"]
	}
	display, ok := displayRaw.(map[string]interface{})
	if !ok {
		logger.Info("Perses dashboard spec.display is not a map, the dashboard will not be updated in Dash0.")
		return nil
	}

	displayName, ok := display["name"]
	if !ok || displayName == "" {
		// Let the dashboard name default to the perses dashboard resource's namespace + name, if unset.
		display["name"] = fmt.Sprintf("%s/%s", valResult.namespace, valResult.name)
	}

	// Remove all unnecessary metadata (labels & annotations), we basically only need the dashboard spec.
	serializedDashboard, _ := json.Marshal(
		map[string]interface{}{
			"kind": "PersesDashboard",
			"spec": spec,
		})
	requestPayload := bytes.NewBuffer(serializedDashboard)

	req, err := http.NewRequest(
		http.MethodPut,
		valResult.url,
		requestPayload,
	)
	if err != nil {
		logger.Error(err, "unable to create a new HTTP request to upsert the dashboard")
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", valResult.authToken))
	logger.Info(fmt.Sprintf("Updating/creating dashboard %s in Dash0", valResult.origin))
	res, err := r.httpClient.Do(req)
	if err != nil {
		logger.Error(err, fmt.Sprintf("unable to execute the HTTP request to update the dashboard %s", valResult.origin))
		return err
	}

	if res.StatusCode < http.StatusOK || res.StatusCode >= http.StatusMultipleChoices {
		return r.handleNon2xxStatusCode(res, valResult.origin, logger)
	}

	// http status code was 2xx, discard the response body and close it
	defer func() {
		_, _ = io.Copy(io.Discard, res.Body)
		_ = res.Body.Close()
	}()

	return nil
}

func (r *PersesDashboardReconciler) DeleteDashboard(
	persesDashboard *unstructured.Unstructured,
	logger *logr.Logger,
) error {
	apiConfig := r.apiConfig.Load()
	valResult, executeRequest := r.validateConfigAndRenderUrl(
		persesDashboard,
		apiConfig,
		logger,
	)
	if !executeRequest {
		return nil
	}

	req, err := http.NewRequest(
		http.MethodDelete,
		valResult.url,
		nil,
	)
	if err != nil {
		logger.Error(err, "unable to create a new HTTP request to delete the dashboard")
		return err
	}
	req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", valResult.authToken))
	logger.Info(fmt.Sprintf("Deleting dashboard %s in Dash0", valResult.origin))
	res, err := r.httpClient.Do(req)
	if err != nil {
		logger.Error(err, fmt.Sprintf("unable to execute the HTTP request to delete the dashboard %s", valResult.origin))
		return err
	}

	if res.StatusCode < http.StatusOK || res.StatusCode >= http.StatusMultipleChoices {
		return r.handleNon2xxStatusCode(res, valResult.origin, logger)
	}

	// http status code was 2xx, discard the response body and close it
	defer func() {
		_, _ = io.Copy(io.Discard, res.Body)
		_ = res.Body.Close()
	}()

	return nil
}

func (r *PersesDashboardReconciler) validateConfigAndRenderUrl(
	persesDashboard *unstructured.Unstructured,
	apiConfig *ApiConfig,
	logger *logr.Logger,
) (*validationResult, bool) {
	if !isValidApiConfig(apiConfig) {
		logger.Info("No Dash0 API endpoint has been provided via the operator configuration resource, the dashboard " +
			"will not be updated in Dash0.")
		return nil, false
	}
	if r.authToken == "" {
		logger.Info("No auth token is set on the controller deployment, the dashboard will not be updated " +
			"in Dash0.")
		return nil, false
	}

	dataset := apiConfig.Dataset
	if dataset == "" {
		dataset = util.DatasetDefault
	}

	namespace, name, ok := readNamespaceAndName(persesDashboard, "Perses dashboard", logger)
	if !ok {
		return nil, false
	}

	dashboardUrl, dashboardOrigin := r.renderDashboardUrl(
		apiConfig.Endpoint,
		namespace,
		name,
		dataset,
	)
	return &validationResult{
		namespace: namespace,
		name:      name,
		url:       dashboardUrl,
		origin:    dashboardOrigin,
		authToken: r.authToken,
	}, true
}

func (r *PersesDashboardReconciler) renderDashboardUrl(
	dash0ApiEndpoint string,
	namespace string,
	name string,
	dataset string,
) (string, string) {

	dashboardOrigin := fmt.Sprintf(
		// we deliberately use _ as the separator, since that is an illegal character in Kubernetes names. This avoids
		// any potential naming collisions (e.g. namespace="abc" & name="def-ghi" vs. namespace="abc-def" & name="ghi").
		"dash0-operator_%s_%s_%s_%s",
		r.pseudoClusterUid,
		dataset,
		namespace,
		name,
	)
	if !strings.HasSuffix(dash0ApiEndpoint, "/") {
		dash0ApiEndpoint += "/"
	}
	return fmt.Sprintf(
		"%sapi/dashboards/%s?dataset=%s",
		dash0ApiEndpoint,
		dashboardOrigin,
		dataset,
	), dashboardOrigin
}

func (r *PersesDashboardReconciler) handleNon2xxStatusCode(
	res *http.Response,
	dashboardOrigin string,
	logger *logr.Logger,
) error {
	defer func() {
		_ = res.Body.Close()
	}()
	responseBody, readErr := io.ReadAll(res.Body)
	if readErr != nil {
		readBodyErr := fmt.Errorf("unable to read the API response payload after receiving status code %d when "+
			"trying to udpate/create/delete the dashboard %s", res.StatusCode, dashboardOrigin)
		logger.Error(readBodyErr, "unable to read the API response payload")
		return readBodyErr
	}

	statusCodeErr := fmt.Errorf(
		"unexpected status code %d when updating/creating/deleting the dashboard %s, response body is %s",
		res.StatusCode,
		dashboardOrigin,
		string(responseBody),
	)
	logger.Error(statusCodeErr, "unexpected status code")
	return statusCodeErr
}
