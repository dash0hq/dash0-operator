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

type PrometheusRuleCrdReconciler struct {
	AuthToken                string
	mgr                      ctrl.Manager
	skipNameValidation       bool
	prometheusRuleReconciler *PrometheusRuleReconciler
	prometheusRuleCrdExists  atomic.Bool
}

//+kubebuilder:rbac:groups=apiextensions.k8s.io,resources=customresourcedefinitions,verbs=get;list;watch

var (
	// prometheusRuleCrdReconcileRequestMetric otelmetric.Int64Counter
	prometheusRuleReconcileRequestMetric otelmetric.Int64Counter
)

func (r *PrometheusRuleCrdReconciler) Manager() ctrl.Manager {
	return r.mgr
}

func (r *PrometheusRuleCrdReconciler) GetAuthToken() string {
	return r.AuthToken
}

func (r *PrometheusRuleCrdReconciler) KindDisplayName() string {
	return "Prometheus rule"
}

func (r *PrometheusRuleCrdReconciler) Group() string {
	return "monitoring.coreos.com"
}

func (r *PrometheusRuleCrdReconciler) Kind() string {
	return "PrometheusRule"
}

func (r *PrometheusRuleCrdReconciler) Version() string {
	return "v1"
}

func (r *PrometheusRuleCrdReconciler) QualifiedKind() string {
	return "prometheusrules.monitoring.coreos.com"
}

func (r *PrometheusRuleCrdReconciler) ControllerName() string {
	return "dash0_prometheus_rule_crd_controller"
}

func (r *PrometheusRuleCrdReconciler) DoesCrdExist() *atomic.Bool {
	return &r.prometheusRuleCrdExists
}

func (r *PrometheusRuleCrdReconciler) SetCrdExists(exists bool) {
	r.prometheusRuleCrdExists.Store(exists)
}

func (r *PrometheusRuleCrdReconciler) SkipNameValidation() bool {
	return r.skipNameValidation
}

func (r *PrometheusRuleCrdReconciler) CreateResourceReconciler(
	pseudoClusterUid types.UID,
	authToken string,
	httpClient *http.Client,
) {
	r.prometheusRuleReconciler = &PrometheusRuleReconciler{
		pseudoClusterUid: pseudoClusterUid,
		authToken:        authToken,
		httpClient:       httpClient,
	}
}

func (r *PrometheusRuleCrdReconciler) ResourceReconciler() ThirdPartyResourceReconciler {
	return r.prometheusRuleReconciler
}

func (r *PrometheusRuleCrdReconciler) SetupWithManager(
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

//+kubebuilder:rbac:groups=monitoring.coreos.com,resources=prometheusrules,verbs=get;list;watch

func (r *PrometheusRuleCrdReconciler) Create(
	ctx context.Context,
	_ event.TypedCreateEvent[client.Object],
	_ workqueue.TypedRateLimitingInterface[reconcile.Request],
) {
	logger := log.FromContext(ctx)
	r.prometheusRuleCrdExists.Store(true)
	maybeStartWatchingThirdPartyResources(r, false, &logger)
}

func (r *PrometheusRuleCrdReconciler) Update(
	context.Context,
	event.TypedUpdateEvent[client.Object],
	workqueue.TypedRateLimitingInterface[reconcile.Request],
) {
	// should not be called, we are not interested in updates
	// note: update is called twice prior to delete, it is also called twice after an actual create
}

func (r *PrometheusRuleCrdReconciler) Delete(
	ctx context.Context,
	_ event.TypedDeleteEvent[client.Object],
	_ workqueue.TypedRateLimitingInterface[reconcile.Request],
) {
	logger := log.FromContext(ctx)
	logger.Info("The PrometheusRule custom resource definition has been deleted.")
	r.prometheusRuleCrdExists.Store(false)

	// Known issue: We would need to stop the watch for the Prometheus rule resources here, but the controller-runtime
	// does not provide any API to stop a watch.
	// An error will be logged every ten seconds until the controller process is restarted.
}

func (r *PrometheusRuleCrdReconciler) Generic(
	context.Context,
	event.TypedGenericEvent[client.Object],
	workqueue.TypedRateLimitingInterface[reconcile.Request],
) {
	// Should not be called, we are not interested in generic events.
}

func (r *PrometheusRuleCrdReconciler) Reconcile(
	_ context.Context,
	_ reconcile.Request,
) (reconcile.Result, error) {
	// Reconcile should not be called for the PrometheusRuleCrdReconciler CRD, as we are using the
	// TypedEventHandler interface directly when setting up the watch. We still need to implement the method, as the
	// controller builder's Complete method requires implementing the Reconciler interface.
	return reconcile.Result{}, nil
}

func (r *PrometheusRuleCrdReconciler) InitializeSelfMonitoringMetrics(
	meter otelmetric.Meter,
	metricNamePrefix string,
	logger *logr.Logger,
) {
	// Note: The prometheusRuleCrdReconcileRequestMetric is unused until we actually implement watching the
	// PrometheusRule _CRD_, see comment above in SetupWithManager.

	// reconcileRequestMetricName := fmt.Sprintf("%s%s", metricNamePrefix, "prometheusrulecrd.reconcile_requests")
	// var err error
	// if prometheusRuleCrdReconcileRequestMetric, err = meter.Int64Counter(
	// 	reconcileRequestMetricName,
	// 	otelmetric.WithUnit("1"),
	// 	otelmetric.WithDescription("Counter for prometheusrule CRD reconcile requests"),
	// ); err != nil {
	// 	logger.Error(err, "Cannot initialize the metric %s.")
	// }

	r.prometheusRuleReconciler.InitializeSelfMonitoringMetrics(
		meter,
		metricNamePrefix,
		logger,
	)
}

func (r *PrometheusRuleCrdReconciler) SetApiEndpointAndDataset(
	apiConfig *ApiConfig,
	logger *logr.Logger) {
	if r.prometheusRuleReconciler == nil {
		// If no auth token has been set via environment variable, we do not even create the prometheusRuleReconciler,
		// hence this nil check is necessary.
		return
	}
	r.prometheusRuleReconciler.apiConfig.Store(apiConfig)
	maybeStartWatchingThirdPartyResources(r, false, logger)
}

func (r *PrometheusRuleCrdReconciler) RemoveApiEndpointAndDataset() {
	if r.prometheusRuleReconciler == nil {
		// If no auth token has been set via environment variable, we do not even create the prometheusRuleReconciler,
		// hence this nil check is necessary.
		return
	}
	r.prometheusRuleReconciler.apiConfig.Store(nil)
}

type PrometheusRuleReconciler struct {
	isWatching       atomic.Bool
	pseudoClusterUid types.UID
	httpClient       *http.Client
	apiConfig        atomic.Pointer[ApiConfig]
	authToken        string
}

func (r *PrometheusRuleReconciler) InitializeSelfMonitoringMetrics(
	meter otelmetric.Meter,
	metricNamePrefix string,
	logger *logr.Logger,
) {
	reconcileRequestMetricName := fmt.Sprintf("%s%s", metricNamePrefix, "prometheusrule.reconcile_requests")
	var err error
	if prometheusRuleReconcileRequestMetric, err = meter.Int64Counter(
		reconcileRequestMetricName,
		otelmetric.WithUnit("1"),
		otelmetric.WithDescription("Counter for prometheus rule reconcile requests"),
	); err != nil {
		logger.Error(err, "Cannot initialize the metric %s.")
	}
}

func (r *PrometheusRuleReconciler) IsWatching() *atomic.Bool {
	return &r.isWatching
}

func (r *PrometheusRuleReconciler) SetIsWatching(isWatching bool) {
	r.isWatching.Store(isWatching)
}

func (r *PrometheusRuleReconciler) GetApiConfig() *atomic.Pointer[ApiConfig] {
	return &r.apiConfig
}

func (r *PrometheusRuleReconciler) ControllerName() string {
	return "dash0_prometheus_rule_controller"
}

func (r *PrometheusRuleReconciler) Create(
	ctx context.Context,
	e event.TypedCreateEvent[client.Object],
	_ workqueue.TypedRateLimitingInterface[reconcile.Request],
) {
	if prometheusRuleReconcileRequestMetric != nil {
		prometheusRuleReconcileRequestMetric.Add(ctx, 1)
	}

	logger := log.FromContext(ctx)
	logger.Info(
		"Detected a new Prometheus rule resource",
		"namespace",
		e.Object.GetNamespace(),
		"name",
		e.Object.GetName(),
	)
	if err := r.UpsertRule(e.Object.(*unstructured.Unstructured), &logger); err != nil {
		logger.Error(err, "unable to upsert the rule")
	}
}

func (r *PrometheusRuleReconciler) Update(
	ctx context.Context,
	e event.TypedUpdateEvent[client.Object],
	_ workqueue.TypedRateLimitingInterface[reconcile.Request],
) {
	if prometheusRuleReconcileRequestMetric != nil {
		prometheusRuleReconcileRequestMetric.Add(ctx, 1)
	}

	logger := log.FromContext(ctx)
	logger.Info(
		"Detected a change for a Prometheus rule resource",
		"namespace",
		e.ObjectNew.GetNamespace(),
		"name",
		e.ObjectNew.GetName(),
	)

	_ = util.RetryWithCustomBackoff(
		"upsert rule",
		func() error {
			return r.UpsertRule(e.ObjectNew.(*unstructured.Unstructured), &logger)
		},
		retrySettings,
		true,
		&logger,
	)
}

func (r *PrometheusRuleReconciler) Delete(
	ctx context.Context,
	e event.TypedDeleteEvent[client.Object],
	_ workqueue.TypedRateLimitingInterface[reconcile.Request],
) {
	if prometheusRuleReconcileRequestMetric != nil {
		prometheusRuleReconcileRequestMetric.Add(ctx, 1)
	}

	logger := log.FromContext(ctx)
	logger.Info(
		"Detected the deletion of a Prometheus rule resource",
		"namespace",
		e.Object.GetNamespace(),
		"name",
		e.Object.GetName(),
	)

	_ = util.RetryWithCustomBackoff(
		"delete rule",
		func() error {
			return r.DeleteRule(e.Object.(*unstructured.Unstructured), &logger)
		},
		retrySettings,
		true,
		&logger,
	)
}

func (r *PrometheusRuleReconciler) Generic(
	_ context.Context,
	_ event.TypedGenericEvent[client.Object],
	_ workqueue.TypedRateLimitingInterface[reconcile.Request],
) {
	// ignoring generic events
}

func (r *PrometheusRuleReconciler) Reconcile(
	context.Context,
	reconcile.Request,
) (reconcile.Result, error) {
	// Reconcile should not be called on the PrometheusRuleReconciler, as we are using the TypedEventHandler interface
	// directly when setting up the watch. We still need to implement the method, as the controller builder's Complete
	// method requires implementing the Reconciler interface.
	return reconcile.Result{}, nil
}

func (r *PrometheusRuleReconciler) UpsertRule(
	prometheusRule *unstructured.Unstructured,
	logger *logr.Logger,
) error {
	apiConfig := r.apiConfig.Load()
	valResult, executeRequest := r.validateConfigAndRenderUrl(
		prometheusRule,
		apiConfig,
		logger,
	)
	if !executeRequest {
		return nil
	}

	specRaw := prometheusRule.Object["spec"]
	if specRaw == nil {
		logger.Info("Prometheus rule has no spec, the rule will not be updated in Dash0.")
		return nil
	}
	spec, ok := specRaw.(map[string]interface{})
	if !ok {
		logger.Info("Prometheus rule spec is not a map, the rule will not be updated in Dash0.")
		return nil
	}
	displayRaw := spec["display"]
	if displayRaw == nil {
		spec["display"] = map[string]interface{}{}
		displayRaw = spec["display"]
	}
	display, ok := displayRaw.(map[string]interface{})
	if !ok {
		logger.Info("Prometheus rule spec.display is not a map, the rule will not be updated in Dash0.")
		return nil
	}

	displayName, ok := display["name"]
	if !ok || displayName == "" {
		// Let the rule name default to the prometheus rule resource's namespace + name, if unset.
		display["name"] = fmt.Sprintf("%s/%s", valResult.namespace, valResult.name)
	}

	// Remove all unnecessary metadata (labels & annotations), we basically only need the rule spec.
	serializedRule, _ := json.Marshal(
		map[string]interface{}{
			"kind": "PrometheusRule",
			"spec": spec,
		})
	requestPayload := bytes.NewBuffer(serializedRule)

	req, err := http.NewRequest(
		http.MethodPut,
		valResult.url,
		requestPayload,
	)
	if err != nil {
		logger.Error(err, "unable to create a new HTTP request to upsert the rule")
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", valResult.authToken))
	logger.Info(fmt.Sprintf("Updating/creating rule %s in Dash0", valResult.origin))
	res, err := r.httpClient.Do(req)
	if err != nil {
		logger.Error(err, fmt.Sprintf("unable to execute the HTTP request to update the rule %s", valResult.origin))
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

func (r *PrometheusRuleReconciler) DeleteRule(
	prometheusRule *unstructured.Unstructured,
	logger *logr.Logger,
) error {
	apiConfig := r.apiConfig.Load()
	valResult, executeRequest := r.validateConfigAndRenderUrl(
		prometheusRule,
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
		logger.Error(err, "unable to create a new HTTP request to delete the rule")
		return err
	}
	req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", valResult.authToken))
	logger.Info(fmt.Sprintf("Deleting rule %s in Dash0", valResult.origin))
	res, err := r.httpClient.Do(req)
	if err != nil {
		logger.Error(err, fmt.Sprintf("unable to execute the HTTP request to delete the rule %s", valResult.origin))
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

func (r *PrometheusRuleReconciler) validateConfigAndRenderUrl(
	prometheusRule *unstructured.Unstructured,
	apiConfig *ApiConfig,
	logger *logr.Logger,
) (*validationResult, bool) {
	if !isValidApiConfig(apiConfig) {
		logger.Info("No Dash0 API endpoint has been provided via the operator configuration resource, the rule " +
			"will not be updated in Dash0.")
		return nil, false
	}
	if r.authToken == "" {
		logger.Info("No auth token is set on the controller deployment, the rule will not be updated " +
			"in Dash0.")
		return nil, false
	}

	dataset := apiConfig.Dataset
	if dataset == "" {
		dataset = util.DatasetDefault
	}

	namespace, name, ok := readNamespaceAndName(prometheusRule, "Prometheus rule", logger)
	if !ok {
		return nil, false
	}

	ruleUrl, ruleOrigin := r.renderRuleUrl(
		apiConfig.Endpoint,
		namespace,
		name,
		dataset,
	)
	return &validationResult{
		namespace: namespace,
		name:      name,
		url:       ruleUrl,
		origin:    ruleOrigin,
		authToken: r.authToken,
	}, true
}

func (r *PrometheusRuleReconciler) renderRuleUrl(
	dash0ApiEndpoint string,
	namespace string,
	name string,
	dataset string,
) (string, string) {

	ruleOrigin := fmt.Sprintf(
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
		"%sapi/alerting/check-rules/%s?dataset=%s",
		dash0ApiEndpoint,
		ruleOrigin,
		dataset,
	), ruleOrigin
}

func (r *PrometheusRuleReconciler) handleNon2xxStatusCode(
	res *http.Response,
	ruleOrigin string,
	logger *logr.Logger,
) error {
	defer func() {
		_ = res.Body.Close()
	}()
	responseBody, readErr := io.ReadAll(res.Body)
	if readErr != nil {
		readBodyErr := fmt.Errorf("unable to read the API response payload after receiving status code %d when "+
			"trying to udpate/create/delete the rule %s", res.StatusCode, ruleOrigin)
		logger.Error(readBodyErr, "unable to read the API response payload")
		return readBodyErr
	}

	statusCodeErr := fmt.Errorf(
		"unexpected status code %d when updating/creating/deleting the rule %s, response body is %s",
		res.StatusCode,
		ruleOrigin,
		string(responseBody),
	)
	logger.Error(statusCodeErr, "unexpected status code")
	return statusCodeErr
}
