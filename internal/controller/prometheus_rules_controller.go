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
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/client-go/util/workqueue"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	"sigs.k8s.io/yaml"

	dash0common "github.com/dash0hq/dash0-operator/api/operator/common"
	dash0v1beta1 "github.com/dash0hq/dash0-operator/api/operator/v1beta1"
	"github.com/dash0hq/dash0-operator/internal/selfmonitoringapiaccess"
	"github.com/dash0hq/dash0-operator/internal/util"
)

type PrometheusRuleCrdReconciler struct {
	client.Client
	queue                    *workqueue.Typed[ThirdPartyResourceSyncJob]
	leaderElectionAware      util.LeaderElectionAware
	mgr                      ctrl.Manager
	httpClient               *http.Client
	skipNameValidation       bool
	prometheusRuleReconciler *PrometheusRuleReconciler
	prometheusRuleCrdExists  atomic.Bool
}

type PrometheusRuleReconciler struct {
	client.Client
	pseudoClusterUid           types.UID
	queue                      *workqueue.Typed[ThirdPartyResourceSyncJob]
	httpClient                 *http.Client
	defaultApiConfigs          selfmonitoringapiaccess.SynchronizedSlice[ApiConfig]
	namespacedApiConfigs       selfmonitoringapiaccess.SynchronizedMapSlice[ApiConfig]
	httpRetryDelay             time.Duration
	controllerStopFunctionLock sync.Mutex
	controllerStopFunction     *context.CancelFunc
}

type CheckRule struct {
	Name          string            `json:"name"`
	Expression    string            `json:"expression"`
	For           string            `json:"for,omitempty"`
	Interval      string            `json:"interval,omitempty"`
	KeepFiringFor string            `json:"keepFiringFor,omitempty"`
	Annotations   map[string]string `json:"annotations"`
	Labels        map[string]string `json:"labels"`
}

const (
	thresholdReference                             = "$__threshold"
	thresholdDegradedAnnotation                    = "dash0-threshold-degraded"
	thresholdDegradedAnnotationLegacy              = "threshold-degraded"
	thresholdCriticalAnnotation                    = "dash0-threshold-critical"
	thresholdCriticalAnnotationLegacy              = "threshold-critical"
	thresholdAnnotationsMissingMessagePattern      = "the rule uses the token %s in its expression, but has neither the %s nor the %s annotation."
	thresholdAnnotationsNonNumericalMessagePattern = "the rule uses the token %s in its expression, but its threshold-%s annotation is not numerical: %s."
)

var (
	prometheusRuleCrdReconcileRequestMetric otelmetric.Int64Counter
	prometheusRuleReconcileRequestMetric    otelmetric.Int64Counter
)

func NewPrometheusRuleCrdReconciler(
	k8sClient client.Client,
	queue *workqueue.Typed[ThirdPartyResourceSyncJob],
	leaderElectionAware util.LeaderElectionAware,
	httpClient *http.Client,
) *PrometheusRuleCrdReconciler {
	return &PrometheusRuleCrdReconciler{
		Client:              k8sClient,
		queue:               queue,
		leaderElectionAware: leaderElectionAware,
		httpClient:          httpClient,
	}
}

func (r *PrometheusRuleCrdReconciler) Manager() ctrl.Manager {
	return r.mgr
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

func (r *PrometheusRuleCrdReconciler) OperatorManagerIsLeader() bool {
	return r.leaderElectionAware.IsLeader()
}

func (r *PrometheusRuleCrdReconciler) CreateThirdPartyResourceReconciler(pseudoClusterUid types.UID) {
	r.prometheusRuleReconciler = &PrometheusRuleReconciler{
		Client:               r.Client,
		queue:                r.queue,
		pseudoClusterUid:     pseudoClusterUid,
		httpClient:           r.httpClient,
		defaultApiConfigs:    *selfmonitoringapiaccess.NewSynchronizedSlice[ApiConfig](),
		namespacedApiConfigs: *selfmonitoringapiaccess.NewSynchronizedMapSlice[ApiConfig](),
		httpRetryDelay:       1 * time.Second,
	}
}

func (r *PrometheusRuleCrdReconciler) ThirdPartyResourceReconciler() ThirdPartyResourceReconciler {
	return r.prometheusRuleReconciler
}

func (r *PrometheusRuleCrdReconciler) SetupWithManager(
	ctx context.Context,
	mgr ctrl.Manager,
	startupK8sClient client.Client,
	logger logr.Logger,
) error {
	r.mgr = mgr
	return SetupThirdPartyCrdReconcilerWithManager(
		ctx,
		startupK8sClient,
		r,
		logger,
	)
}

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
	maybeStartWatchingThirdPartyResources(r, logger)
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

	stopWatchingThirdPartyResources(ctx, r, logger)
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
	logger logr.Logger,
) {
	reconcileRequestMetricName := fmt.Sprintf("%s%s", metricNamePrefix, "prometheusrulecrd.reconcile_requests")
	var err error
	if prometheusRuleCrdReconcileRequestMetric, err = meter.Int64Counter(
		reconcileRequestMetricName,
		otelmetric.WithUnit("1"),
		otelmetric.WithDescription("Counter for PrometheusRule CRD reconcile requests"),
	); err != nil {
		logger.Error(err, fmt.Sprintf("Cannot initialize the metric %s.", reconcileRequestMetricName))
	}

	r.prometheusRuleReconciler.InitializeSelfMonitoringMetrics(
		meter,
		metricNamePrefix,
		logger,
	)
}

func (r *PrometheusRuleCrdReconciler) SetDefaultApiConfigs(
	ctx context.Context,
	apiConfig []ApiConfig,
	logger logr.Logger,
) {
	r.prometheusRuleReconciler.defaultApiConfigs.Set(apiConfig)
	if len(filterValidApiConfigs(apiConfig, logger, "default operator configuration")) > 0 {
		maybeStartWatchingThirdPartyResources(r, logger)
	} else {
		stopWatchingThirdPartyResources(ctx, r, logger)
	}
}

func (r *PrometheusRuleCrdReconciler) RemoveDefaultApiConfigs(ctx context.Context, logger logr.Logger) {
	r.prometheusRuleReconciler.defaultApiConfigs.Set(nil)
	stopWatchingThirdPartyResources(ctx, r, logger)
}

func (r *PrometheusRuleCrdReconciler) SetNamespacedApiConfigs(
	ctx context.Context,
	namespace string,
	updatedApiConfig []ApiConfig,
	logger logr.Logger,
) {
	if updatedApiConfig != nil {
		previousApiConfig, _ := r.prometheusRuleReconciler.namespacedApiConfigs.Get(namespace)

		r.prometheusRuleReconciler.namespacedApiConfigs.Set(namespace, updatedApiConfig)

		if !slices.Equal(previousApiConfig, updatedApiConfig) {
			r.prometheusRuleReconciler.synchronizeNamespacedResources(ctx, namespace, logger)
		}
	}
}

func (r *PrometheusRuleCrdReconciler) RemoveNamespacedApiConfigs(
	ctx context.Context,
	namespace string,
	logger logr.Logger,
) {
	if _, exists := r.prometheusRuleReconciler.namespacedApiConfigs.Get(namespace); exists {
		r.prometheusRuleReconciler.namespacedApiConfigs.Delete(namespace)
		r.prometheusRuleReconciler.synchronizeNamespacedResources(ctx, namespace, logger)
	}
}

func (r *PrometheusRuleReconciler) InitializeSelfMonitoringMetrics(
	meter otelmetric.Meter,
	metricNamePrefix string,
	logger logr.Logger,
) {
	reconcileRequestMetricName := fmt.Sprintf("%s%s", metricNamePrefix, "prometheusrule.reconcile_requests")
	var err error
	if prometheusRuleReconcileRequestMetric, err = meter.Int64Counter(
		reconcileRequestMetricName,
		otelmetric.WithUnit("1"),
		otelmetric.WithDescription("Counter for prometheus rule reconcile requests"),
	); err != nil {
		logger.Error(err, fmt.Sprintf("Cannot initialize the metric %s.", reconcileRequestMetricName))
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

func (r *PrometheusRuleReconciler) GetDefaultApiConfigs() []ApiConfig {
	return r.defaultApiConfigs.Get()
}

func (r *PrometheusRuleReconciler) GetNamespacedApiConfigs(namespace string) ([]ApiConfig, bool) {
	return r.namespacedApiConfigs.Get(namespace)
}

func (r *PrometheusRuleReconciler) ControllerName() string {
	return "dash0_prometheus_rule_controller"
}

func (r *PrometheusRuleReconciler) K8sClient() client.Client {
	return r.Client
}

func (r *PrometheusRuleReconciler) Queue() *workqueue.Typed[ThirdPartyResourceSyncJob] {
	return r.queue
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

func (r *PrometheusRuleReconciler) IsSynchronizationEnabled(monitoringResource *dash0v1beta1.Dash0Monitoring) bool {
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

	upsertViaApi(r, e.Object)
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

	upsertViaApi(r, e.ObjectNew)
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

	deleteViaApi(r, e.Object)
}

func (r *PrometheusRuleReconciler) Generic(
	ctx context.Context,
	e event.TypedGenericEvent[*unstructured.Unstructured],
	_ workqueue.TypedRateLimitingInterface[reconcile.Request],
) {
	if prometheusRuleReconcileRequestMetric != nil {
		prometheusRuleReconcileRequestMetric.Add(ctx, 1)
	}

	logger := log.FromContext(ctx)
	logger.Info(
		"Reconciling check rule triggered by config event (updated API config or authorization).",
		"namespace",
		e.Object.GetNamespace(),
		"name",
		e.Object.GetName(),
	)

	upsertViaApi(r, e.Object)
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

func (r *PrometheusRuleReconciler) FetchExistingResourceOriginsRequest(
	preconditionValidationResult *preconditionValidationResult,
	apiConfig ApiConfig,
) (*http.Request, error) {
	checkRulesUrl := r.renderCheckRuleListUrl(preconditionValidationResult, apiConfig.Endpoint, apiConfig.Dataset)
	if req, err := http.NewRequest(http.MethodGet, checkRulesUrl, nil); err != nil {
		return nil, err
	} else {
		addAuthorizationHeader(req, apiConfig.Token)
		req.Header.Set(util.AcceptHeaderName, util.ApplicationJsonMediaType)
		return req, nil
	}
}

func (r *PrometheusRuleReconciler) MapResourceToHttpRequests(
	preconditionChecksResult *preconditionValidationResult,
	apiConfig ApiConfig,
	action apiAction,
	logger logr.Logger,
) *ResourceToRequestsResult {
	specRaw := preconditionChecksResult.resource["spec"]
	// convert the untyped spec to a value of type prometheusv1.PrometheusRuleSpec by marshalling and then unmarshalling
	//it
	specAsYaml, err := yaml.Marshal(specRaw)
	if err != nil {
		logger.Error(err, "unable to marshal the Prometheus rule spec")
		return NewResourceToRequestsResultPreconditionError(apiConfig, err.Error())
	}
	ruleSpec := prometheusv1.PrometheusRuleSpec{}
	if err = yaml.Unmarshal(specAsYaml, &ruleSpec); err != nil {
		logger.Error(err, "unable to unmarshal the Prometheus rule spec")
		return NewResourceToRequestsResultPreconditionError(apiConfig, err.Error())
	}

	var originsInResource []string
	var requests []WrappedApiRequest
	allValidationIssues := make(map[string][]string)
	allSynchronizationErrors := make(map[string]string)

	for _, group := range ruleSpec.Groups {
		duplicateOrigins := make(map[string]int, len(group.Rules))
		for ruleIdx, rule := range group.Rules {
			itemNameSuffix := rule.Alert
			if itemNameSuffix == "" {
				itemNameSuffix = strconv.Itoa(ruleIdx)
			}
			checkRuleName := fmt.Sprintf("%s - %s", group.Name, itemNameSuffix)
			checkRuleOriginNotUrlEncoded, checkRuleOriginUrlEncoded :=
				r.renderCheckRuleOrigin(
					preconditionChecksResult,
					apiConfig.Dataset,
					duplicateOrigins,
					group.Name,
					rule.Alert,
				)
			checkRuleUrl := r.renderCheckRuleUrl(checkRuleOriginUrlEncoded, apiConfig.Endpoint, apiConfig.Dataset)
			// note: syncError does not actually refer to a failed sync attempt but indicates that the request could not be created
			request, validationIssues, syncError, ok := convertRuleToRequest(
				checkRuleUrl,
				action,
				apiConfig,
				rule,
				group.Name,
				group.Interval,
				readTopLevelAnnotations(preconditionChecksResult),
				logger,
			)
			if len(validationIssues) > 0 {
				allValidationIssues[checkRuleName] = validationIssues
				// If a rule becomes temporarily invalid due to a bad edit, we do not want to delete it in Dash0
				// (assuming it has been valid at some point before and has been synchronized). Instead, we keep the
				// most recent valid state. To do that, we add its id to the list of ids we have seen (the list will
				// later be used to determine which of the checks in Dash0 need to be removed because they are no longer
				// in the K8s resource.
				originsInResource = append(originsInResource, checkRuleOriginNotUrlEncoded)
				continue
			}
			if syncError != nil {
				// If a rule cannot be synchronized temporarily, we do not want to delete it in Dash0 immediately
				// (assuming the rule has been synchronized before at some point). Instead, we keep the
				// most recent state. To do that, we add its id to the list of ids we have seen (the list will later
				// be used to determine which of the checks in Dash0 need to be removed because they are no longer
				// in the K8s resource.
				allSynchronizationErrors[checkRuleName] = syncError.Error()
				continue
			}
			if ok {
				requests = append(
					requests, WrappedApiRequest{
						Request:  request,
						ItemName: checkRuleName,
						Origin:   checkRuleOriginNotUrlEncoded,
					},
				)
				originsInResource = append(originsInResource, checkRuleOriginNotUrlEncoded)
			}
		}
	}

	return NewResourceToRequestsResult(
		apiConfig,
		len(requests)+len(allValidationIssues)+len(allSynchronizationErrors),
		requests,
		originsInResource,
		allValidationIssues,
		allSynchronizationErrors,
	)
}

// renderCheckRuleListUrl renders the URL to fetch the list of existing check rule IDs from the Dash0 API.
func (r *PrometheusRuleReconciler) renderCheckRuleListUrl(
	preconditionChecksResult *preconditionValidationResult,
	endpoint string,
	dataset string,
) string {
	// 08.09.2025: Start sending the prefix already as originPrefix, the Dash0 API will rename the parameter
	// soon-ish. We can remove the (misnamed) idPrefix query parameter as soon as the Dash0 API has rolled out the
	// rename. Then, ~1 year later, the Dash0 API can remove support for the legacy query parameter idPrefix.
	originPrefix :=
		r.renderCheckRuleOriginPrefix(preconditionChecksResult, url.QueryEscape(dataset))
	return fmt.Sprintf(
		"%sapi/alerting/check-rules?dataset=%s&idPrefix=%s&originPrefix=%s",
		endpoint,
		url.QueryEscape(dataset),
		originPrefix,
		originPrefix,
	)
}

// renderCheckRuleUrl renders the URL for a single Dash0 check rule.
func (r *PrometheusRuleReconciler) renderCheckRuleUrl(
	checkRuleOrigin string,
	endpoint string,
	dataset string,
) string {
	return fmt.Sprintf(
		"%sapi/alerting/check-rules/%s?dataset=%s",
		endpoint,
		checkRuleOrigin,
		url.QueryEscape(dataset),
	)
}

// renderCheckRuleOrigin renders the origin of a single Dash0 check rule.
// The origin has the pattern
// "dash0-operator_${clusterUid}_${dataset}_${namespace}_${prometheusrule_resource_name}_${group.name}_${alert}".
func (r *PrometheusRuleReconciler) renderCheckRuleOrigin(
	preconditionChecksResult *preconditionValidationResult,
	dataset string,
	duplicateOrigins map[string]int,
	groupName string,
	alertName string,
) (string, string) {
	// For now the Dash0 backend treats %2F the same as "/", so we need to replace forward slashes with
	// something other than %2F.
	// See
	// https://stackoverflow.com/questions/71581828/gin-problem-accessing-url-encoded-path-param-containing-forward-slash
	// The character "%" causes a similar problem, so we also replace it.
	// See https://github.com/oapi-codegen/runtime/issues/35
	groupNameNotUrlEncoded := strings.ReplaceAll(groupName, "/", "|")
	groupNameNotUrlEncoded = strings.ReplaceAll(groupNameNotUrlEncoded, "%", "|")
	groupNameUrlEncoded := url.PathEscape(groupNameNotUrlEncoded)
	if alertName == "" {
		// Rules without an alert name will not actually be sent to the Dash0 API, since they are deemed invalid by
		// convertRuleToCheckRule ("rule has neither the alert nor the record attribute").
		alertName = "unnamed rule"
	}
	alertNameNotUrlEncoded := strings.ReplaceAll(alertName, "/", "|")
	alertNameNotUrlEncoded = strings.ReplaceAll(alertNameNotUrlEncoded, "%", "|")
	alertNameUrlEncoded := url.PathEscape(alertNameNotUrlEncoded)

	checkOriginNotUrlEncoded := fmt.Sprintf(
		"%s%s_%s",
		r.renderCheckRuleOriginPrefix(preconditionChecksResult, dataset),
		groupNameNotUrlEncoded,
		alertNameNotUrlEncoded,
	)
	checkOriginUrlEncoded := fmt.Sprintf(
		"%s%s_%s",
		// dataset can only contain letters, numbers, underscores, and hyphens, so we do not need to replace "/" -> "|".
		r.renderCheckRuleOriginPrefix(preconditionChecksResult, url.PathEscape(dataset)),
		groupNameUrlEncoded,
		alertNameUrlEncoded,
	)

	// About duplicate origins: Origins must be unique within a dataset, and they need to be stable against reordering
	// of rules within a group or deletion of individual rules from a group. We derive the origin from the group name
	// and the name of the individual alert rule. The group name is guaranteed to be unique within a PrometheusRule
	// resource, due to a "x-kubernetes-list-type: map" plus "x-kubernetes-list-map-keys: [name]" annotation in the CRD.
	// The rule.alert field has no such guarantee, duplicate alert names within a group are possible.
	// We keep track of all produced origins within a group in the duplicateOrigins map[string]int. Once an origin
	// string has been produced for the first time, we set the value for this origin to 1. The counter is increased
	// each time the origin is produced again. Starting with the second time we see the same origin (i.e. the first
	// duplicate), the counter is also appended to the origin to ensure unique origins.
	// (One downside of this approach is that _within a set of rules with identical group & alert name_, reordering
	// rules or deleting one of the rules can lead to one rule taking over the origin of another rule, which we
	// generally avoid for rules without duplicate alert names. Duplicate alert names should be fairly uncommon in
	// practice, they can be considered a configuration error, so this downside is acceptable.)
	if duplicationIndexForThisOrigin, isDuplicate := duplicateOrigins[checkOriginNotUrlEncoded]; isDuplicate {
		duplicateOrigins[checkOriginNotUrlEncoded]++
		checkOriginNotUrlEncoded = checkOriginNotUrlEncoded + "_" + strconv.Itoa(duplicationIndexForThisOrigin)
		checkOriginUrlEncoded = checkOriginNotUrlEncoded + "_" + strconv.Itoa(duplicationIndexForThisOrigin)
	} else {
		duplicateOrigins[checkOriginNotUrlEncoded] = 1
	}
	return checkOriginNotUrlEncoded, checkOriginUrlEncoded
}

// renderCheckRuleOriginPrefix renders the common origin prefix for all Dash0 check rules that are created from one
// Kubernetes PrometheusRule resource.
func (r *PrometheusRuleReconciler) renderCheckRuleOriginPrefix(
	preconditionChecksResult *preconditionValidationResult,
	dataset string,
) string {
	return fmt.Sprintf(
		// we deliberately use _ as the separator, since that is an illegal character in Kubernetes names. This avoids
		// any potential naming collisions (e.g. namespace="abc" & name="def-ghi" vs. namespace="abc-def" & name="ghi").
		"dash0-operator_%s_%s_%s_%s_",
		r.pseudoClusterUid,
		dataset,
		preconditionChecksResult.k8sNamespace,
		preconditionChecksResult.k8sName,
	)
}

func readTopLevelAnnotations(preconditionChecksResult *preconditionValidationResult) map[string]string {
	var metadataAnnotations map[string]string
	if metadataRaw, ok := preconditionChecksResult.resource["metadata"]; ok {
		if metadata, ok := metadataRaw.(map[string]any); ok {
			if annotationsRaw, ok := metadata["annotations"]; ok {
				if annotations, ok := annotationsRaw.(map[string]any); ok {
					metadataAnnotations = make(map[string]string, len(annotations))
					for key, value := range annotations {
						if strValue, ok := value.(string); ok {
							metadataAnnotations[key] = strValue
						}
					}
				}
			}
		}
	}
	return metadataAnnotations
}

// convertRuleToRequest converts a Prometheus rule to an HTTP request that can be sent to the Dash0 API. It returns the
// request object if the conversion is successful and there are no validation issues and no synchronization errors.
// Otherwise, a list of validation issues or a single synchronization errror is returned. There are also rules which we
// want to silently skip, in which case no rule, no validation issues and no error is returned, but the final boolean
// return value is false.
func convertRuleToRequest(
	checkRuleUrl string,
	action apiAction,
	apiConfig ApiConfig,
	rule prometheusv1.Rule,
	groupName string,
	interval *prometheusv1.Duration,
	metadataAnnotations map[string]string,
	logger logr.Logger,
) (*http.Request, []string, error, bool) {
	checkRule, validationIssues, ok := convertRuleToCheckRule(
		rule,
		action,
		groupName,
		interval,
		metadataAnnotations,
		logger,
	)
	if len(validationIssues) > 0 {
		return nil, validationIssues, nil, ok
	}
	if !ok {
		// This is for rules that are neither invalid nor erroneuos but that we simply ignore/skip, in particular
		// recording rules.
		return nil, nil, nil, false
	}

	var req *http.Request
	var method string
	var err error

	//nolint:ineffassign
	switch action {
	case upsertAction:
		serializedCheckRule, _ := json.Marshal(checkRule)
		requestPayload := bytes.NewBuffer(serializedCheckRule)
		method = http.MethodPut
		req, err = http.NewRequest(
			method,
			checkRuleUrl,
			requestPayload,
		)
	case deleteAction:
		method = http.MethodDelete
		req, err = http.NewRequest(
			method,
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
			"unable to create a new HTTP request to synchronize the rule: %s %s: %w",
			method,
			checkRuleUrl,
			err,
		)
		logger.Error(httpError, "error creating http request")
		return nil, nil, httpError, false
	}

	addAuthorizationHeader(req, apiConfig.Token)
	if action == upsertAction {
		req.Header.Set(util.ContentTypeHeaderName, util.ApplicationJsonMediaType)
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
	metadataAnnotations map[string]string,
	logger logr.Logger,
) (*CheckRule, []string, bool) {
	if rule.Record != "" {
		logger.Info("Skipping rule with record attribute", "record", rule.Record)
		return nil, nil, false
	}
	if rule.Alert == "" {
		logger.Info(
			fmt.Sprintf(
				"Found invalid rule in group %s which has neither a record nor an alert attribute.", groupName,
			),
		)
		return nil, []string{"rule has neither the alert nor the record attribute"}, false
	}

	if action == deleteAction {
		// When deleting a rule, we do not need an actual payload, but do need to skip rules with rule.Record or without
		// rule.Alert (that is why we still call convertRuleToCheckRule for deletions).
		return &CheckRule{}, nil, true
	}

	validationIssues := make([]string, 0)

	expression := convertIntOrString(rule.Expr)
	validationIssues = validateExpression(validationIssues, expression)

	// Merge annotations before validation so that threshold annotations from metadata are considered.
	mergedAnnotations := mergeAnnotations(metadataAnnotations, rule.Annotations)
	validationIssues = validateThreshold(validationIssues, expression, mergedAnnotations)

	if len(validationIssues) > 0 {
		return nil, validationIssues, false
	}

	// If action is not delete, it is upsert, and for that we need to create an actual payload, hence we need to convert
	// the rule to a CheckRule.
	dash0CheckRuleName := fmt.Sprintf("%s - %s", groupName, rule.Alert)
	checkRule := &CheckRule{
		Name:          dash0CheckRuleName,
		Interval:      convertDuration(interval),
		Annotations:   mergedAnnotations,
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
	degradedThresholdValue, hasThresholdDegradedAnnotation := annotations[thresholdDegradedAnnotation]
	if !hasThresholdDegradedAnnotation {
		// In January 2025, the annotation names were changed to start with "dash0-", that is, threshold-degraded became
		// dash0-threshold-degraded. For a grace period, we allow both names. At some point we can remove the check for
		// the legacy name.
		degradedThresholdValue, hasThresholdDegradedAnnotation = annotations[thresholdDegradedAnnotationLegacy]
	}
	criticalThresholdValue, hasThresholdCriticalAnnotation := annotations[thresholdCriticalAnnotation]
	if !hasThresholdCriticalAnnotation {
		criticalThresholdValue, hasThresholdCriticalAnnotation = annotations[thresholdCriticalAnnotationLegacy]
	}

	if hasThresholdInExpression && !hasThresholdDegradedAnnotation && !hasThresholdCriticalAnnotation {
		return append(
			validationIssues, fmt.Sprintf(
				thresholdAnnotationsMissingMessagePattern,
				thresholdReference,
				thresholdDegradedAnnotation,
				thresholdCriticalAnnotation,
			),
		)
	}

	if !hasThresholdDegradedAnnotation && !hasThresholdCriticalAnnotation {
		return validationIssues
	}

	if hasThresholdDegradedAnnotation {
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
	if hasThresholdCriticalAnnotation {
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

// mergeAnnotations merges metadata annotations with rule annotations. In case of conflicts (same key appears in both),
// the rule annotation takes priority over the metadata annotation.
func mergeAnnotations(metadataAnnotations map[string]string, ruleAnnotations map[string]string) map[string]string {
	if len(metadataAnnotations) == 0 {
		if ruleAnnotations == nil {
			return make(map[string]string)
		}
		return ruleAnnotations
	}

	merged := make(map[string]string, len(metadataAnnotations)+len(ruleAnnotations))
	maps.Copy(merged, metadataAnnotations)

	// override metadata annotations with individual rule annotations, i.e. annotations from individual rules take
	// priority
	maps.Copy(merged, ruleAnnotations)

	return merged
}

func (r *PrometheusRuleReconciler) CreateDeleteRequests(
	apiConfig ApiConfig,
	existingOriginsFromApi []string,
	originsInResource []string,
	logger logr.Logger,
) ([]WrappedApiRequest, map[string]string) {
	var deleteRequests []WrappedApiRequest
	allSynchronizationErrors := make(map[string]string)
	for _, existingOrigin := range existingOriginsFromApi {
		if !slices.Contains(originsInResource, existingOrigin) {
			// This means that the rule has been deleted in the resource, so we need to delete it via the API.
			deleteUrl := r.renderCheckRuleUrl(existingOrigin, apiConfig.Endpoint, apiConfig.Dataset)
			if req, err := http.NewRequest(
				http.MethodDelete,
				deleteUrl,
				nil,
			); err != nil {
				httpError := fmt.Errorf(
					"unable to create a new HTTP request to delete the rule at %s: %w",
					deleteUrl,
					err,
				)
				logger.Error(httpError, "error creating http request to delete rule")
				allSynchronizationErrors[existingOrigin] = httpError.Error()
			} else {
				addAuthorizationHeader(req, apiConfig.Token)
				deleteRequests = append(
					deleteRequests, WrappedApiRequest{
						Request:  req,
						ItemName: existingOrigin + " (deleted)",
						Origin:   existingOrigin,
					},
				)
			}
		}
	}
	return deleteRequests, allSynchronizationErrors
}

func (r *PrometheusRuleReconciler) ExtractIdFromResponseBody(
	responseBytes []byte,
	logger logr.Logger,
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

func (*PrometheusRuleReconciler) UpdateSynchronizationResultsInDash0MonitoringStatus(
	monitoringResource *dash0v1beta1.Dash0Monitoring,
	qualifiedName string,
	status dash0common.ThirdPartySynchronizationStatus,
	syncResults synchronizationResults,
) any {
	previousResults := monitoringResource.Status.PrometheusRuleSynchronizationResults
	if previousResults == nil {
		previousResults = make(map[string]dash0common.PrometheusRuleSynchronizationResult)
		monitoringResource.Status.PrometheusRuleSynchronizationResults = previousResults
	}

	rulesSyncResults := make([]dash0common.PrometheusRuleSynchronizationResultPerEndpointAndDataset, 0,
		len(syncResults.resultsPerApiConfig))
	for _, resultPerApiConfig := range syncResults.resultsPerApiConfig {
		synchronizedRuleAttributes := make(
			map[string]dash0common.PrometheusRuleSynchronizedRuleAttributes,
			len(resultPerApiConfig.successfullySynchronized),
		)
		for _, syncSuccess := range resultPerApiConfig.successfullySynchronized {
			promRuleSyncAttributes := dash0common.PrometheusRuleSynchronizedRuleAttributes{}
			apiObjectLabels := syncSuccess.Labels
			promRuleSyncAttributes.Dash0Origin = apiObjectLabels.Origin
			synchronizedRuleAttributes[syncSuccess.ItemName] = promRuleSyncAttributes
		}

		rulesSyncRes := dash0common.PrometheusRuleSynchronizationResultPerEndpointAndDataset{
			Dash0ApiEndpoint:            resultPerApiConfig.resourceToRequestsResult.ApiConfig.Endpoint,
			Dash0Dataset:                resultPerApiConfig.resourceToRequestsResult.ApiConfig.Dataset,
			SynchronizedRulesTotal:      len(resultPerApiConfig.successfullySynchronized),
			SynchronizedRulesAttributes: synchronizedRuleAttributes,
			SynchronizationErrorsTotal:  len(resultPerApiConfig.resourceToRequestsResult.SynchronizationErrors),
			SynchronizationErrors:       resultPerApiConfig.resourceToRequestsResult.SynchronizationErrors,
		}

		rulesSyncResults = append(rulesSyncResults, rulesSyncRes)
	}

	result := dash0common.PrometheusRuleSynchronizationResult{
		SynchronizationStatus:  status,
		SynchronizedAt:         metav1.Time{Time: time.Now()},
		AlertingRulesTotal:     syncResults.itemsTotal,
		InvalidRulesTotal:      len(syncResults.validationIssues),
		InvalidRules:           syncResults.validationIssues,
		SynchronizationResults: rulesSyncResults,
	}
	previousResults[qualifiedName] = result
	return result
}

// synchronizeNamespacedResources explicitly triggers a resync of check rules in a given namespace in response to an
// updated API endpoint, dataset or auth token.
func (r *PrometheusRuleReconciler) synchronizeNamespacedResources(
	ctx context.Context,
	namespace string,
	logger logr.Logger,
) {
	// do nothing if we are not currently watching the CRDs
	if !r.IsWatching() {
		return
	}

	logger.Info(fmt.Sprintf("Running synchronization of check rules in namespace %s now.", namespace))

	go func() {
		allRulesResourcesInNamespace := &unstructured.UnstructuredList{}
		allRulesResourcesInNamespace.SetGroupVersionKind(
			schema.GroupVersionKind{
				Group:   "monitoring.coreos.com",
				Version: "v1",
				Kind:    "PrometheusRuleList",
			},
		)
		if err := r.List(
			ctx,
			allRulesResourcesInNamespace,
			&client.ListOptions{
				Namespace: namespace,
			},
		); err != nil {
			logger.Error(err, fmt.Sprintf("Failed to list check rules resources in namespace %s.", namespace))
			return
		}

		for i := range allRulesResourcesInNamespace.Items {
			ruleResource := &allRulesResourcesInNamespace.Items[i]
			evt := event.TypedGenericEvent[*unstructured.Unstructured]{
				Object: ruleResource,
			}
			r.Generic(ctx, evt, nil)

			// stagger API requests a bit
			time.Sleep(50 * time.Millisecond)
		}
		logger.Info(fmt.Sprintf("Triggering synchronization of check rules in namespace %s has finished.", namespace))
	}()
}
