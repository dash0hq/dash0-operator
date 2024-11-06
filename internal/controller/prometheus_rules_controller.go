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
	"sync"
	"sync/atomic"
	"time"

	"github.com/go-logr/logr"
	prometheusv1 "github.com/prometheus-operator/prometheus-operator/pkg/apis/monitoring/v1"
	otelmetric "go.opentelemetry.io/otel/metric"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
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

	dash0v1alpha1 "github.com/dash0hq/dash0-operator/api/dash0monitoring/v1alpha1"
)

type PrometheusRuleCrdReconciler struct {
	Client                   client.Client
	AuthToken                string
	mgr                      ctrl.Manager
	skipNameValidation       bool
	prometheusRuleReconciler *PrometheusRuleReconciler
	prometheusRuleCrdExists  atomic.Bool
}

type PrometheusRuleReconciler struct {
	client.Client
	pseudoClusterUid           types.UID
	httpClient                 *http.Client
	apiConfig                  atomic.Pointer[ApiConfig]
	authToken                  string
	httpRetryDelay             time.Duration
	controllerStopFunctionLock sync.Mutex
	controllerStopFunction     *context.CancelFunc
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

const (
	thresholdReference                             = "$__threshold"
	thresholdDegradedAnnotation                    = "threshold-degraded"
	thresholdCriticalAnnotation                    = "threshold-critical"
	thresholdAnnotationsMissingMessagePattern      = "the rule uses the token %s in its expression, but has neither the %s nor the %s annotation."
	thresholdAnnotationsNonNumericalMessagePattern = "the rule uses the token %s in its expression, but its threshold-%s annotation is not numerical: %s."
)

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
		Client:           r.Client,
		pseudoClusterUid: pseudoClusterUid,
		authToken:        authToken,
		httpClient:       httpClient,
		httpRetryDelay:   1 * time.Second,
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

	stopWatchingThirdPartyResources(ctx, r, &logger)
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

func (r *PrometheusRuleReconciler) ControllerStopFunctionLock() *sync.Mutex {
	return &r.controllerStopFunctionLock
}

func (r *PrometheusRuleReconciler) GetControllerStopFunction() *context.CancelFunc {
	return r.controllerStopFunction
}

func (r *PrometheusRuleReconciler) SetControllerStopFunction(controllerStopFunction *context.CancelFunc) {
	r.controllerStopFunction = controllerStopFunction
}

func (r *PrometheusRuleReconciler) IsWatching() bool {
	return r.controllerStopFunction != nil
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

func (r *PrometheusRuleReconciler) K8sClient() client.Client {
	return r.Client
}

func (r *PrometheusRuleReconciler) HttpClient() *http.Client {
	return r.httpClient
}

func (r *PrometheusRuleReconciler) GetHttpRetryDelay() time.Duration {
	return r.httpRetryDelay
}

func (r *PrometheusRuleReconciler) overrideHttpRetryDelay(delay time.Duration) {
	r.httpRetryDelay = delay
}

func (r *PrometheusRuleReconciler) IsSynchronizationEnabled(monitoringResource *dash0v1alpha1.Dash0Monitoring) bool {
	if monitoringResource == nil {
		return false
	}
	boolPtr := monitoringResource.Spec.SynchronizePrometheusRules
	if boolPtr == nil {
		return true
	}
	return *boolPtr
}

func (r *PrometheusRuleReconciler) Create(
	ctx context.Context,
	e event.TypedCreateEvent[*unstructured.Unstructured],
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

	upsertViaApi(ctx, r, e.Object, &logger)
}

func (r *PrometheusRuleReconciler) Update(
	ctx context.Context,
	e event.TypedUpdateEvent[*unstructured.Unstructured],
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

	upsertViaApi(ctx, r, e.ObjectNew, &logger)
}

func (r *PrometheusRuleReconciler) Delete(
	ctx context.Context,
	e event.TypedDeleteEvent[*unstructured.Unstructured],
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

	deleteViaApi(ctx, r, e.Object, &logger)
}

func (r *PrometheusRuleReconciler) Generic(
	_ context.Context,
	_ event.TypedGenericEvent[*unstructured.Unstructured],
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
) (int, []HttpRequestWithItemName, map[string][]string, map[string]string) {
	urlPrefix := r.renderUrlPrefix(preconditionChecksResult)
	requests := make([]HttpRequestWithItemName, 0)
	allValidationIssues := make(map[string][]string)
	allSynchronizationErrors := make(map[string]string)

	specRaw := preconditionChecksResult.thirdPartyResourceSpec
	specAsYaml, err := yaml.Marshal(specRaw)
	if err != nil {
		logger.Error(err, "unable to marshal the Prometheus rule spec")
		return 0, nil, nil, map[string]string{"*": err.Error()}
	}
	ruleSpec := prometheusv1.PrometheusRuleSpec{}
	if err = yaml.Unmarshal(specAsYaml, &ruleSpec); err != nil {
		logger.Error(err, "unable to unmarshal the Prometheus rule spec")
		return 0, nil, nil, map[string]string{"*": err.Error()}
	}

	for _, group := range ruleSpec.Groups {
		for ruleIdx, rule := range group.Rules {
			itemNameSuffix := rule.Alert
			if itemNameSuffix == "" {
				itemNameSuffix = strconv.Itoa(ruleIdx)
			}
			itemName := fmt.Sprintf("%s - %s", group.Name, itemNameSuffix)

			checkRuleUrl := fmt.Sprintf(
				"%s_%s_%d?dataset=%s",
				urlPrefix,
				urlEncodePathSegment(group.Name),
				ruleIdx,
				url.QueryEscape(preconditionChecksResult.dataset),
			)
			request, validationIssues, syncError, ok := convertRuleToRequest(
				checkRuleUrl,
				action,
				rule,
				preconditionChecksResult,
				group.Name,
				group.Interval,
				logger,
			)
			if len(validationIssues) > 0 {
				allValidationIssues[itemName] = validationIssues
				continue
			}
			if syncError != nil {
				allSynchronizationErrors[itemName] = syncError.Error()
				continue
			}
			if ok {
				requests = append(requests, HttpRequestWithItemName{
					ItemName: itemName,
					Request:  request,
				})
			}
		}
	}

	return len(requests) + len(allValidationIssues) + len(allSynchronizationErrors),
		requests,
		allValidationIssues,
		allSynchronizationErrors
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

// convertRuleToRequest converts a Prometheus rule to an HTTP request that can be sent to the Dash0 API. It returns the
// request object if the conversion is successful and there are no validation issues and no synchronization errors.
// Otherwise, a list of validation issues or a single synchronization errror is returned. There are also rules which we
// want to silently skip, in which case no rule, no validation issues and no error is returned, but the final boolean
// return value is false.
func convertRuleToRequest(
	checkRuleUrl string,
	action apiAction,
	rule prometheusv1.Rule,
	preconditionCheckResult *preconditionValidationResult,
	groupName string,
	interval *prometheusv1.Duration,
	logger *logr.Logger,
) (*http.Request, []string, error, bool) {
	checkRule, validationIssues, ok := convertRuleToCheckRule(rule, action, groupName, interval, logger)
	if len(validationIssues) > 0 {
		return nil, validationIssues, nil, ok
	}
	if !ok {
		// This is for rules that are neither invalid nor erroneuos but that we simply ignore/skip, in particular
		// recording rules.
		return nil, nil, nil, false
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
		unknownActionErr := fmt.Errorf("unknown API action: %d", action)
		logger.Error(unknownActionErr, "unknown API action")
		return nil, nil, unknownActionErr, false
	}

	if err != nil {
		httpError := fmt.Errorf(
			"unable to create a new HTTP request to %s the rule at %s: %w",
			actionLabel,
			checkRuleUrl,
			err,
		)
		logger.Error(httpError, "error creating http request")
		return nil, nil, httpError, false
	}

	req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", preconditionCheckResult.authToken))
	if action == upsert {
		req.Header.Set("Content-Type", "application/json")
	}

	return req, nil, nil, true
}

// convertRuleToCheckRule converts a Prometheus rule to a CheckRule. It returns the converted CheckRule if the
// conversion is successful and there are no validation issues. Otherwise, a list of validation issues is returned.
// There are also rules which we want to silently skip, in which case no rule and no validation issues are returned,
// but the boolean return value is false.
func convertRuleToCheckRule(
	rule prometheusv1.Rule,
	action apiAction,
	groupName string,
	interval *prometheusv1.Duration,
	logger *logr.Logger,
) (*CheckRule, []string, bool) {
	if rule.Record != "" {
		logger.Info("Skipping rule with record attribute", "record", rule.Record)
		return nil, nil, false
	}
	if rule.Alert == "" {
		logger.Info(
			fmt.Sprintf(
				"Found invalid rule in group %s which has neither a record nor an alert attribute.", groupName))
		return nil, []string{"rule has neither the alert nor the record attribute"}, false
	}

	if action == delete {
		// When deleting a rule, we do not need an actual payload, but do need to skip rules with rule.Record or without
		// rule.Alert (that is why we still call convertRuleToCheckRule for deletions).
		return &CheckRule{}, nil, true
	}

	validationIssues := make([]string, 0)

	expression := convertIntOrString(rule.Expr)
	validationIssues = validateExpression(validationIssues, expression)
	validationIssues = validateThreshold(validationIssues, expression, rule.Annotations)

	if len(validationIssues) > 0 {
		return nil, validationIssues, false
	}

	// If action is not delete, it is upsert, and for that we need to create an actual payload, hence we need to convert
	// the rule to a CheckRule.
	dash0CheckRuleName := fmt.Sprintf("%s - %s", groupName, rule.Alert)
	checkRule := &CheckRule{
		Name:          dash0CheckRuleName,
		Interval:      convertDuration(interval),
		Annotations:   rule.Annotations,
		Labels:        rule.Labels,
		Expression:    expression,
		For:           convertDuration(rule.For),
		KeepFiringFor: convertNonEmptyDuration(rule.KeepFiringFor),
	}

	return checkRule, nil, true
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

func validateExpression(validationIssues []string, expression string) []string {
	if expression == "" {
		return append(validationIssues, "the rule has no expression attribute (or the expression attribute is empty)")
	}
	return validationIssues
}

func validateThreshold(
	validationIssues []string,
	expression string,
	annotations map[string]string,
) []string {
	hasThresholdInExpression := strings.Contains(expression, thresholdReference)
	degradedThresholdValue, hasThresholdDegradedInAnnotation := annotations[thresholdDegradedAnnotation]
	criticalThresholdValue, hasThresholdCriticalInAnnotation := annotations[thresholdCriticalAnnotation]

	if hasThresholdInExpression && !hasThresholdDegradedInAnnotation && !hasThresholdCriticalInAnnotation {
		return append(validationIssues, fmt.Sprintf(
			thresholdAnnotationsMissingMessagePattern,
			thresholdReference,
			thresholdDegradedAnnotation,
			thresholdCriticalAnnotation,
		))
	}

	if !hasThresholdDegradedInAnnotation && !hasThresholdCriticalInAnnotation {
		return validationIssues
	}

	if hasThresholdDegradedInAnnotation {
		_, err := strconv.ParseFloat(degradedThresholdValue, 32)
		if err != nil {
			validationIssues = append(
				validationIssues,
				fmt.Sprintf(
					thresholdAnnotationsNonNumericalMessagePattern,
					thresholdReference,
					"degraded",
					degradedThresholdValue,
				),
			)
		}
	}
	if hasThresholdCriticalInAnnotation {
		_, err := strconv.ParseFloat(criticalThresholdValue, 32)
		if err != nil {
			validationIssues = append(
				validationIssues,
				fmt.Sprintf(
					thresholdAnnotationsNonNumericalMessagePattern,
					thresholdReference,
					"critical",
					criticalThresholdValue,
				),
			)
		}
	}

	return validationIssues
}

func (r *PrometheusRuleReconciler) UpdateSynchronizationResultsInStatus(
	monitoringResource *dash0v1alpha1.Dash0Monitoring,
	qualifiedName string,
	status dash0v1alpha1.SynchronizationStatus,
	itemsTotal int,
	succesfullySynchronized []string,
	synchronizationErrorsPerItem map[string]string,
	validationIssuesPerItem map[string][]string,
) interface{} {
	previousResults := monitoringResource.Status.PrometheusRuleSynchronizationResults
	if previousResults == nil {
		previousResults = make(map[string]dash0v1alpha1.PrometheusRuleSynchronizationResult)
		monitoringResource.Status.PrometheusRuleSynchronizationResults = previousResults
	}
	result := dash0v1alpha1.PrometheusRuleSynchronizationResult{
		SynchronizationStatus:      status,
		SynchronizedAt:             metav1.Time{Time: time.Now()},
		AlertingRulesTotal:         itemsTotal,
		SynchronizedRulesTotal:     len(succesfullySynchronized),
		SynchronizedRules:          succesfullySynchronized,
		SynchronizationErrorsTotal: len(synchronizationErrorsPerItem),
		SynchronizationErrors:      synchronizationErrorsPerItem,
		InvalidRulesTotal:          len(validationIssuesPerItem),
		InvalidRules:               validationIssuesPerItem,
	}
	previousResults[qualifiedName] = result
	return result
}
