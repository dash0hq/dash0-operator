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
	"strconv"
	"strings"
	"sync/atomic"

	"github.com/go-logr/logr"
	prometheusv1 "github.com/prometheus-operator/prometheus-operator/pkg/apis/monitoring/v1"
	otelmetric "go.opentelemetry.io/otel/metric"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/client-go/util/workqueue"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	"sigs.k8s.io/yaml"

	"github.com/dash0hq/dash0-operator/internal/util"
)

type PrometheusRuleCrdReconciler struct {
	AuthToken                string
	mgr                      ctrl.Manager
	skipNameValidation       bool
	prometheusRuleReconciler *PrometheusRuleReconciler
	prometheusRuleCrdExists  atomic.Bool
}

//+kubebuilder:rbac:groups=apiextensions.k8s.io,resources=customresourcedefinitions,verbs=get;list;watch

type CheckRule struct {
	Name          string            `json:"name"`
	Expression    string            `json:"expression"`
	For           string            `json:"for,omitempty"`
	Interval      string            `json:"interval,omitempty"`
	KeepFiringFor string            `json:"keepFiringFor,omitempty"`
	Annotations   map[string]string `json:"annotations"` // √
	Labels        map[string]string `json:"labels"`
}

var (
	prometheusRuleCrdReconcileRequestMetric otelmetric.Int64Counter
	prometheusRuleReconcileRequestMetric    otelmetric.Int64Counter
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
	if prometheusRuleCrdReconcileRequestMetric != nil {
		prometheusRuleCrdReconcileRequestMetric.Add(ctx, 1)
	}
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
	if prometheusRuleCrdReconcileRequestMetric != nil {
		prometheusRuleCrdReconcileRequestMetric.Add(ctx, 1)
	}
	logger := log.FromContext(ctx)
	logger.Info("The PrometheusRule custom resource definition has been deleted.")
	r.prometheusRuleCrdExists.Store(false)

	// Known issue: We would need to stop the watch for the Prometheus rule resources here, but the controller-runtime
	// does not provide any API to stop a watch.
	// An error will be logged every ten seconds until the controller process is restarted.
	// See https://github.com/kubernetes-sigs/controller-runtime/issues/2983.
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
	reconcileRequestMetricName := fmt.Sprintf("%s%s", metricNamePrefix, "prometheusrulecrd.reconcile_requests")
	var err error
	if prometheusRuleCrdReconcileRequestMetric, err = meter.Int64Counter(
		reconcileRequestMetricName,
		otelmetric.WithUnit("1"),
		otelmetric.WithDescription("Counter for PrometheusRule CRD reconcile requests"),
	); err != nil {
		logger.Error(err, "Cannot initialize the metric %s.")
	}

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

func (r *PrometheusRuleReconciler) KindDisplayName() string {
	return "Prometheus rule"
}

func (r *PrometheusRuleReconciler) ShortName() string {
	return "rule"
}

func (r *PrometheusRuleReconciler) IsWatching() *atomic.Bool {
	return &r.isWatching
}

func (r *PrometheusRuleReconciler) SetIsWatching(isWatching bool) {
	r.isWatching.Store(isWatching)
}

func (r *PrometheusRuleReconciler) GetAuthToken() string {
	return r.authToken
}

func (r *PrometheusRuleReconciler) GetApiConfig() *atomic.Pointer[ApiConfig] {
	return &r.apiConfig
}

func (r *PrometheusRuleReconciler) ControllerName() string {
	return "dash0_prometheus_rule_controller"
}

func (r *PrometheusRuleReconciler) HttpClient() *http.Client {
	return r.httpClient
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

	if err := util.RetryWithCustomBackoff(
		"create rule(s)",
		func() error {
			return upsertViaApi(r, e.Object.(*unstructured.Unstructured), &logger)
		},
		retrySettings,
		true,
		&logger,
	); err != nil {
		logger.Error(err, "failed to create the rule(s)")
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

	if err := util.RetryWithCustomBackoff(
		"update rule(s)",
		func() error {
			return upsertViaApi(r, e.ObjectNew.(*unstructured.Unstructured), &logger)
		},
		retrySettings,
		true,
		&logger,
	); err != nil {
		logger.Error(err, "failed to update the rule(s)")
	}
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

	if err := util.RetryWithCustomBackoff(
		"delete rule(s)",
		func() error {
			return deleteViaApi(r, e.Object.(*unstructured.Unstructured), &logger)
		},
		retrySettings,
		true,
		&logger,
	); err != nil {
		logger.Error(err, "failed to delete the rule(s)")
	}
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

func (r *PrometheusRuleReconciler) MapResourceToHttpRequests(
	preconditionChecksResult *preconditionValidationResult,
	action apiAction,
	logger *logr.Logger,
) ([]*http.Request, error) {
	specRaw := preconditionChecksResult.spec

	specAsYaml, err := yaml.Marshal(specRaw)
	if err != nil {
		logger.Error(err, "unable to marshal the Prometheus rule spec")
		return nil, err
	}
	ruleSpec := prometheusv1.PrometheusRuleSpec{}
	if err = yaml.Unmarshal(specAsYaml, &ruleSpec); err != nil {
		logger.Error(err, "unable to unmarshal the Prometheus rule spec")
		return nil, err
	}

	urlPrefix := r.renderUrlPrefix(preconditionChecksResult)
	requests := make([]*http.Request, 0)
	for _, group := range ruleSpec.Groups {
		for ruleIdx, rule := range group.Rules {
			checkRuleUrl := fmt.Sprintf(
				"%s_%s_%d?dataset=%s",
				urlPrefix,
				urlEncodePathSegment(group.Name),
				ruleIdx,
				url.QueryEscape(preconditionChecksResult.dataset),
			)
			if request, ok := convertRuleToRequest(
				checkRuleUrl,
				action,
				rule,
				preconditionChecksResult,
				group.Name,
				group.Interval,
				logger,
			); ok {
				requests = append(requests, request)
			}
		}
	}

	return requests, nil
}

func (r *PrometheusRuleReconciler) renderUrlPrefix(preconditionCheckResult *preconditionValidationResult) string {

	ruleOriginPrefix := fmt.Sprintf(
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
		"%sapi/alerting/check-rules/%s",
		preconditionCheckResult.apiEndpoint,
		ruleOriginPrefix,
	)
}

func convertRuleToRequest(
	checkRuleUrl string,
	action apiAction,
	rule prometheusv1.Rule,
	preconditionCheckResult *preconditionValidationResult,
	groupName string,
	interval *prometheusv1.Duration,
	logger *logr.Logger,
) (*http.Request, bool) {
	checkRule, ok := convertRuleToCheckRule(rule, action, groupName, interval, logger)
	if !ok {
		return nil, false
	}

	var req *http.Request
	var err error

	//nolint:ineffassign
	actionLabel := "?"
	switch action {
	case upsert:
		actionLabel = "upsert"
		serializedCheckRule, _ := json.Marshal(checkRule)
		requestPayload := bytes.NewBuffer(serializedCheckRule)
		req, err = http.NewRequest(
			http.MethodPut,
			checkRuleUrl,
			requestPayload,
		)
	case delete:
		actionLabel = "delete"
		req, err = http.NewRequest(
			http.MethodDelete,
			checkRuleUrl,
			nil,
		)
	default:
		logger.Error(fmt.Errorf("unknown API action: %d", action), "unknown API action")
		return nil, false
	}

	if err != nil {
		logger.Error(
			err,
			fmt.Sprintf(
				"unable to create a new HTTP request to %s the rule at %s",
				actionLabel,
				checkRuleUrl,
			))
		return nil, false
	}

	req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", preconditionCheckResult.authToken))
	if action == upsert {
		req.Header.Set("Content-Type", "application/json")
	}

	return req, true
}

func convertRuleToCheckRule(
	rule prometheusv1.Rule,
	action apiAction,
	groupName string,
	interval *prometheusv1.Duration,
	logger *logr.Logger,
) (*CheckRule, bool) {
	if rule.Record != "" {
		logger.Info("Skipping rule with record attribute", "record", rule.Record)
		return nil, false
	}
	if rule.Alert == "" {
		logger.Info("Skipping rule without alert attribute", "record", rule.Record)
		return nil, false
	}

	if action == delete {
		// When deleting a rule, we do not need an actual payload, but do need to skip rules with rule.Record or without
		// rule.Alert (that is why we still call convertRuleToCheckRule for deletions).
		return &CheckRule{}, true
	}

	// If action is not delete, it is upsert, and for that we need to create an actual payload, hence we need to convert
	// the rule to a CheckRule.
	checkRule := &CheckRule{
		Name:          fmt.Sprintf("%s - %s", groupName, rule.Alert),
		Interval:      convertDuration(interval),
		Annotations:   rule.Annotations,
		Labels:        rule.Labels,
		Expression:    convertIntOrString(rule.Expr),
		For:           convertDuration(rule.For),
		KeepFiringFor: convertNonEmptyDuration(rule.KeepFiringFor),
	}

	return checkRule, true
}

func convertDuration(duration *prometheusv1.Duration) string {
	if duration == nil {
		return ""
	}
	return string(*duration)
}

func convertNonEmptyDuration(duration *prometheusv1.NonEmptyDuration) string {
	if duration == nil {
		return ""
	}
	return string(*duration)
}

func convertIntOrString(intOrString intstr.IntOrString) string {
	switch intOrString.Type {
	case intstr.String:
		return intOrString.StrVal
	case intstr.Int:
		return strconv.FormatInt(int64(intOrString.IntVal), 10)
	}
	return ""
}
