// SPDX-FileCopyrightText: Copyright 2024 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package controller

import (
	"context"
	"encoding/json"
	"errors"
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

	dash0apiclient "github.com/dash0hq/dash0-api-client-go"
	prometheusv1 "github.com/prometheus-operator/prometheus-operator/pkg/apis/monitoring/v1"
	otelmetric "go.opentelemetry.io/otel/metric"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/client-go/util/workqueue"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	"sigs.k8s.io/yaml"

	dash0common "github.com/dash0hq/dash0-operator/api/operator/common"
	dash0v1beta1 "github.com/dash0hq/dash0-operator/api/operator/v1beta1"
	"github.com/dash0hq/dash0-operator/internal/selfmonitoringapiaccess"
	"github.com/dash0hq/dash0-operator/internal/util"
	"github.com/dash0hq/dash0-operator/internal/util/logd"
)

type PrometheusRuleCrdReconciler struct {
	client.Client
	apiReader                client.Reader
	queue                    *ThirdPartyResourceSyncQueue
	leaderElectionAware      util.LeaderElectionAware
	mgr                      ctrl.Manager
	apiClientPool            *ApiClientPool
	skipNameValidation       bool
	prometheusRuleReconciler *PrometheusRuleReconciler
	prometheusRuleCrdExists  atomic.Bool
}

type PrometheusRuleReconciler struct {
	client.Client
	apiReader                  client.Reader
	pseudoClusterUid           types.UID
	queue                      *ThirdPartyResourceSyncQueue
	apiClientPool              *ApiClientPool
	defaultApiConfigs          selfmonitoringapiaccess.SynchronizedSlice[ApiConfig]
	namespacedApiConfigs       selfmonitoringapiaccess.SynchronizedMapSlice[ApiConfig]
	namespacedSyncEnabled      sync.Map
	controllerStopFunctionLock sync.Mutex
	controllerStopFunction     *context.CancelFunc
}

// CheckRule is a validated Prometheus alerting rule in the format of the Dash0 check rules API. It is converted to the
// check rule type of the Dash0 API client via mapToPrometheusAlertRule.
type CheckRule struct {
	Name          string            `json:"name"`
	Expression    string            `json:"expression"`
	For           string            `json:"for,omitempty"`
	Interval      string            `json:"interval,omitempty"`
	KeepFiringFor string            `json:"keep_firing_for,omitempty"`
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

// The object types of check rules and recording rules, see ApiObjectOrigin.
const (
	checkRuleObjectType     = "check rule"
	recordingRuleObjectType = "recording rule"
)

var (
	prometheusRuleCrdReconcileRequestMetric otelmetric.Int64Counter
	prometheusRuleReconcileRequestMetric    otelmetric.Int64Counter
)

func NewPrometheusRuleCrdReconciler(
	k8sClient client.Client,
	apiReader client.Reader,
	queue *ThirdPartyResourceSyncQueue,
	leaderElectionAware util.LeaderElectionAware,
	apiClientPool *ApiClientPool,
) *PrometheusRuleCrdReconciler {
	return &PrometheusRuleCrdReconciler{
		Client:              k8sClient,
		apiReader:           apiReader,
		queue:               queue,
		leaderElectionAware: leaderElectionAware,
		apiClientPool:       apiClientPool,
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

func (r *PrometheusRuleCrdReconciler) WantsCrdUpdateEvents() bool {
	return false
}

// OnCrdChange is a no-op for PrometheusRule — the CRD has a single version (v1) and we have
// nothing to derive from observing the CRD object. The hook is required to satisfy the
// ThirdPartyCrdReconciler interface (see PersesDashboardCrdReconciler for a reconciler that
// uses it to choose between served versions).
func (r *PrometheusRuleCrdReconciler) OnCrdChange(_ *apiextensionsv1.CustomResourceDefinition, _ logd.Logger) {
}

func (r *PrometheusRuleCrdReconciler) OperatorManagerIsLeader() bool {
	return r.leaderElectionAware.IsLeader()
}

func (r *PrometheusRuleCrdReconciler) CreateThirdPartyResourceReconciler(pseudoClusterUid types.UID) {
	r.prometheusRuleReconciler = &PrometheusRuleReconciler{
		Client:               r.Client,
		apiReader:            r.apiReader,
		queue:                r.queue,
		pseudoClusterUid:     pseudoClusterUid,
		apiClientPool:        r.apiClientPool,
		defaultApiConfigs:    *selfmonitoringapiaccess.NewSynchronizedSlice[ApiConfig](),
		namespacedApiConfigs: *selfmonitoringapiaccess.NewSynchronizedMapSlice[ApiConfig](),
	}
}

func (r *PrometheusRuleCrdReconciler) ThirdPartyResourceReconciler() ThirdPartyResourceReconciler {
	return r.prometheusRuleReconciler
}

func (r *PrometheusRuleCrdReconciler) SetupWithManager(
	ctx context.Context,
	mgr ctrl.Manager,
	startupK8sClient client.Client,
	logger logd.Logger,
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
	e event.TypedCreateEvent[client.Object],
	_ workqueue.TypedRateLimitingInterface[reconcile.Request],
) {
	if prometheusRuleCrdReconcileRequestMetric != nil {
		prometheusRuleCrdReconcileRequestMetric.Add(ctx, 1)
	}
	logger := logd.FromContext(ctx)
	r.prometheusRuleCrdExists.Store(true)
	if crd, ok := e.Object.(*apiextensionsv1.CustomResourceDefinition); ok {
		r.OnCrdChange(crd, logger)
	}
	maybeStartWatchingThirdPartyResources(r, logger)
}

func (r *PrometheusRuleCrdReconciler) Update(
	context.Context,
	event.TypedUpdateEvent[client.Object],
	workqueue.TypedRateLimitingInterface[reconcile.Request],
) {
	// We are not interested in updates of the PrometheusRule CRD.
}

func (r *PrometheusRuleCrdReconciler) Delete(
	ctx context.Context,
	_ event.TypedDeleteEvent[client.Object],
	_ workqueue.TypedRateLimitingInterface[reconcile.Request],
) {
	if prometheusRuleCrdReconcileRequestMetric != nil {
		prometheusRuleCrdReconcileRequestMetric.Add(ctx, 1)
	}
	logger := logd.FromContext(ctx)
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
	logger logd.Logger,
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
	logger logd.Logger,
) {
	r.prometheusRuleReconciler.defaultApiConfigs.Set(apiConfig)
	if len(filterValidApiConfigs(apiConfig, logger, "default operator configuration")) > 0 {
		maybeStartWatchingThirdPartyResources(r, logger)
	} else {
		stopWatchingThirdPartyResources(ctx, r, logger)
	}
}

func (r *PrometheusRuleCrdReconciler) RemoveDefaultApiConfigs(ctx context.Context, logger logd.Logger) {
	r.prometheusRuleReconciler.defaultApiConfigs.Set(nil)
	stopWatchingThirdPartyResources(ctx, r, logger)
}

func (r *PrometheusRuleCrdReconciler) SetNamespacedApiConfigs(
	ctx context.Context,
	namespace string,
	updatedApiConfig []ApiConfig,
	logger logd.Logger,
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
	logger logd.Logger,
) {
	if _, exists := r.prometheusRuleReconciler.namespacedApiConfigs.Get(namespace); exists {
		r.prometheusRuleReconciler.namespacedApiConfigs.Delete(namespace)
		r.prometheusRuleReconciler.synchronizeNamespacedResources(ctx, namespace, logger)
	}
}

func (r *PrometheusRuleCrdReconciler) SetSynchronizationEnabled(
	ctx context.Context,
	namespace string,
	monitoringResource *dash0v1beta1.Dash0Monitoring,
	logger logd.Logger,
) {
	enabled := r.prometheusRuleReconciler.IsSynchronizationEnabled(monitoringResource)
	previous, hadPrevious := r.prometheusRuleReconciler.namespacedSyncEnabled.Swap(namespace, enabled)
	if prev, ok := previous.(bool); hadPrevious && ok && !prev && enabled {
		logger.Info(fmt.Sprintf(
			"Synchronization of check rules has been enabled for namespace %s, triggering resync.",
			namespace,
		))
		r.prometheusRuleReconciler.synchronizeNamespacedResources(ctx, namespace, logger)
	}
}

func (r *PrometheusRuleCrdReconciler) RemoveSynchronizationEnabled(namespace string) {
	r.prometheusRuleReconciler.namespacedSyncEnabled.Delete(namespace)
}

func (r *PrometheusRuleReconciler) InitializeSelfMonitoringMetrics(
	meter otelmetric.Meter,
	metricNamePrefix string,
	logger logd.Logger,
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

func (r *PrometheusRuleReconciler) Queue() *ThirdPartyResourceSyncQueue {
	return r.queue
}

func (r *PrometheusRuleReconciler) ApiReader() client.Reader {
	return r.apiReader
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

	logger := logd.FromContext(ctx)
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
	logger := logd.FromContext(ctx)
	if hasUnchangedResourceVersion(e) {
		logger.Debug(
			"Ignoring an update event for an unchanged Prometheus rule resource",
			"namespace",
			e.ObjectNew.GetNamespace(),
			"name",
			e.ObjectNew.GetName(),
			"resourceVersion",
			e.ObjectNew.GetResourceVersion(),
		)
		return
	}

	if prometheusRuleReconcileRequestMetric != nil {
		prometheusRuleReconcileRequestMetric.Add(ctx, 1)
	}

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

	logger := logd.FromContext(ctx)
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
	context.Context,
	event.TypedGenericEvent[*unstructured.Unstructured],
	workqueue.TypedRateLimitingInterface[reconcile.Request],
) {
	// Should not be called, the watch is backed by source.Kind, which never emits generic events.
}

func (r *PrometheusRuleReconciler) resyncViaApi(
	ctx context.Context,
	ruleResource *unstructured.Unstructured,
	originsInNamespace *existingOriginsInNamespace,
) {
	if prometheusRuleReconcileRequestMetric != nil {
		prometheusRuleReconcileRequestMetric.Add(ctx, 1)
	}

	logger := logd.FromContext(ctx)
	logger.Info(
		"Reconciling check rule triggered by config event (updated API config or authorization).",
		"namespace",
		ruleResource.GetNamespace(),
		"name",
		ruleResource.GetName(),
	)

	upsertViaApiWithOriginsInNamespace(r, ruleResource, originsInNamespace)
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

func (r *PrometheusRuleReconciler) MapsToMultipleApiObjects() bool {
	return true
}

func (r *PrometheusRuleReconciler) FetchExistingResourceOrigins(
	ctx context.Context,
	preconditionValidationResult *preconditionValidationResult,
	apiConfig ApiConfig,
	logger logd.Logger,
) ([]ApiObjectOrigin, error) {
	return r.fetchExistingOrigins(
		ctx,
		r.renderRuleOriginPrefix(preconditionValidationResult, apiConfig.Dataset),
		apiConfig,
		logger,
	)
}

func (r *PrometheusRuleReconciler) FetchExistingNamespaceOrigins(
	ctx context.Context,
	namespace string,
	apiConfig ApiConfig,
	logger logd.Logger,
) ([]ApiObjectOrigin, error) {
	return r.fetchExistingOrigins(
		ctx,
		r.renderRuleOriginPrefixForNamespace(namespace, apiConfig.Dataset),
		apiConfig,
		logger,
	)
}

// fetchExistingOrigins lists the origins of all existing check rules and recording rules whose origin starts with the
// given prefix, each with the type of the rule. It stops at the first list call that fails and returns that error
// without any origins, so the caller never works with a partial list.
func (r *PrometheusRuleReconciler) fetchExistingOrigins(
	ctx context.Context,
	originPrefix string,
	apiConfig ApiConfig,
	logger logd.Logger,
) ([]ApiObjectOrigin, error) {
	dataset := apiConfig.Dataset
	withOriginPrefix := dash0apiclient.WithOriginPrefix(originPrefix)
	listCalls := []struct {
		objectType  string
		url         string
		listOrigins func(dash0apiclient.Client) ([]string, error)
	}{
		{
			objectType: checkRuleObjectType,
			url:        r.renderCheckRuleListUrl(originPrefix, apiConfig.Endpoint, dataset),
			listOrigins: func(apiClient dash0apiclient.Client) ([]string, error) {
				checkRules, err := apiClient.ListCheckRules(ctx, &dataset, withOriginPrefix)
				if err != nil {
					return nil, err
				}
				var origins []string
				for _, checkRule := range checkRules {
					if checkRule != nil && checkRule.Origin != nil && *checkRule.Origin != "" {
						origins = append(origins, *checkRule.Origin)
					}
				}
				return origins, nil
			},
		},
		{
			objectType: recordingRuleObjectType,
			url:        r.renderRecordingRuleListUrl(originPrefix, apiConfig.Endpoint, dataset),
			listOrigins: func(apiClient dash0apiclient.Client) ([]string, error) {
				recordingRules, err := apiClient.ListRecordingRules(ctx, &dataset, withOriginPrefix)
				if err != nil {
					return nil, err
				}
				var origins []string
				for _, recordingRule := range recordingRules {
					if recordingRule == nil || recordingRule.Metadata.Labels == nil {
						continue
					}
					if origin := (*recordingRule.Metadata.Labels)[dash0apiclient.LabelOrigin]; origin != "" {
						origins = append(origins, origin)
					}
				}
				return origins, nil
			},
		},
	}

	var allExistingOrigins []ApiObjectOrigin
	for _, listCall := range listCalls {
		actionLabel := fmt.Sprintf("fetch existing origins: %s %s", http.MethodGet, listCall.url)
		logger.Info(fmt.Sprintf("executing HTTP request to %s", actionLabel))
		apiClient, err := r.apiClientPool.Get(apiConfig.Endpoint, apiConfig.Token)
		var origins []string
		if err == nil {
			origins, err = listCall.listOrigins(apiClient)
		}
		if err != nil {
			err = convertApiClientError(err, actionLabel, false)
			logger.Error(err, "cannot fetch existing origins")
			return nil, err
		}
		for _, origin := range origins {
			allExistingOrigins = append(allExistingOrigins, ApiObjectOrigin{ObjectType: listCall.objectType, Origin: origin})
		}
	}
	logger.Info(fmt.Sprintf("existing origins for this %s", r.ShortName()), "origins", allExistingOrigins)
	return allExistingOrigins, nil
}

func (r *PrometheusRuleReconciler) IsOriginOfResource(
	origin string,
	preconditionValidationResult *preconditionValidationResult,
	apiConfig ApiConfig,
) bool {
	return strings.HasPrefix(origin, r.renderRuleOriginPrefix(preconditionValidationResult, apiConfig.Dataset))
}

func (r *PrometheusRuleReconciler) MapResourceToApiCalls(
	preconditionChecksResult *preconditionValidationResult,
	apiConfig ApiConfig,
	action apiAction,
	logger logd.Logger,
) *ResourceToRequestsResult {
	specRaw := preconditionChecksResult.resource["spec"]
	// convert the untyped spec to a value of type prometheusv1.PrometheusRuleSpec by marshalling and then unmarshalling
	//it
	specAsYaml, err := yaml.Marshal(specRaw)
	if err != nil {
		logger.Error(err, "unable to marshal the Prometheus rule spec")
		return NewResourceToRequestsResultSingleItemConversionError(apiConfig, preconditionErrorItemName, err.Error())
	}
	ruleSpec := prometheusv1.PrometheusRuleSpec{}
	if err = yaml.Unmarshal(specAsYaml, &ruleSpec); err != nil {
		logger.Error(err, "unable to unmarshal the Prometheus rule spec")
		return NewResourceToRequestsResultSingleItemConversionError(apiConfig, preconditionErrorItemName, err.Error())
	}

	var originsInResource []ApiObjectOrigin
	var requests []WrappedApiRequest
	var alertingRulesTotal int
	var recordingRulesTotal int
	allValidationIssues := make(map[string][]string)
	allSynchronizationErrors := make(map[string]string)
	allSynchronizationErrorStatusCodes := make(map[string]int)

	ruleOrigins := r.renderRuleOrigins(preconditionChecksResult, apiConfig.Dataset, ruleSpec.Groups)
	for groupIdx, group := range ruleSpec.Groups {
		for ruleIdx, rule := range group.Rules {
			if rule.Record != "" {
				// This is a recording rule.
				recordingRulesTotal++
				ruleName := fmt.Sprintf("%s - %s", group.Name, rule.Record)
				ruleOrigin := ruleOrigins[groupIdx][ruleIdx]
				// Rules that cannot be synchronized keep their origin in originsInResource, so that the most recent
				// state that has been synchronized to Dash0 is not deleted as an orphan.
				originsInResource = append(
					originsInResource,
					ApiObjectOrigin{ObjectType: recordingRuleObjectType, Origin: ruleOrigin},
				)
				var recordingRule *dash0apiclient.RecordingRule
				if action == upsertAction {
					if recordingRule, err = mapToRecordingRule(rule, group.Name, group.Interval); err != nil {
						conversionErr :=
							fmt.Errorf("unable to convert the recording rule to the Dash0 API format: %w", err)
						logger.Error(conversionErr, "error converting recording rule")
						allSynchronizationErrors[ruleName] = conversionErr.Error()
						allSynchronizationErrorStatusCodes[ruleName] = http.StatusBadRequest
						continue
					}
				}
				apiClientCall, unknownActionErr :=
					r.createRecordingRuleApiClientCall(ruleOrigin, action, apiConfig, recordingRule)
				if unknownActionErr != nil {
					logger.Error(unknownActionErr, "unknown API action")
					allSynchronizationErrors[ruleName] = unknownActionErr.Error()
					continue
				}
				requests = append(
					requests, WrappedApiRequest{
						ApiClientCall: apiClientCall,
						ItemName:      ruleName,
						Origin:        ruleOrigin,
						ObjectType:    recordingRuleObjectType,
					},
				)
			} else {
				// This is an alerting rule.
				alertingRulesTotal++
				itemNameSuffix := rule.Alert
				if itemNameSuffix == "" {
					itemNameSuffix = strconv.Itoa(ruleIdx)
				}
				checkRuleName := fmt.Sprintf("%s - %s", group.Name, itemNameSuffix)
				checkRuleOrigin := ruleOrigins[groupIdx][ruleIdx]
				typedCheckRuleOrigin := ApiObjectOrigin{ObjectType: checkRuleObjectType, Origin: checkRuleOrigin}
				checkRule, validationIssues, ok := convertAlertingRuleToCheckRule(
					rule,
					action,
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
					// later be used to determine which of the checks in Dash0 need to be removed because they are no
					// longer in the K8s resource.
					originsInResource = append(originsInResource, typedCheckRuleOrigin)
					continue
				}
				if !ok {
					continue
				}
				// For the same reason, rules that cannot be synchronized keep their origin in originsInResource.
				originsInResource = append(originsInResource, typedCheckRuleOrigin)
				var alertRule *dash0apiclient.PrometheusAlertRule
				if action == upsertAction {
					if alertRule, err = mapToPrometheusAlertRule(checkRule); err != nil {
						conversionErr :=
							fmt.Errorf("unable to convert the alerting rule to the Dash0 API format: %w", err)
						logger.Error(conversionErr, "error converting alerting rule")
						allSynchronizationErrors[checkRuleName] = conversionErr.Error()
						allSynchronizationErrorStatusCodes[checkRuleName] = http.StatusBadRequest
						continue
					}
				}
				apiClientCall, unknownActionErr :=
					r.createCheckRuleApiClientCall(checkRuleOrigin, action, apiConfig, alertRule)
				if unknownActionErr != nil {
					logger.Error(unknownActionErr, "unknown API action")
					allSynchronizationErrors[checkRuleName] = unknownActionErr.Error()
					continue
				}
				requests = append(
					requests, WrappedApiRequest{
						ApiClientCall: apiClientCall,
						ItemName:      checkRuleName,
						Origin:        checkRuleOrigin,
						ObjectType:    checkRuleObjectType,
					},
				)
			}
		}
	}

	result := NewResourceToRequestsResult(
		apiConfig,
		requests,
		originsInResource,
		allValidationIssues,
		allSynchronizationErrors,
	)
	if len(allSynchronizationErrorStatusCodes) > 0 {
		result.SynchronizationErrorStatusCodes = allSynchronizationErrorStatusCodes
	}
	result.AlertingRulesTotal = alertingRulesTotal
	result.RecordingRulesTotal = recordingRulesTotal
	return result
}

// createCheckRuleApiClientCall creates the call to upsert or delete the check rule with the given origin via the Dash0
// API client. For upserts, alertRule is the payload; for deletes it is ignored.
func (r *PrometheusRuleReconciler) createCheckRuleApiClientCall(
	origin string,
	action apiAction,
	apiConfig ApiConfig,
	alertRule *dash0apiclient.PrometheusAlertRule,
) (*ApiClientCall, error) {
	switch action {
	case upsertAction:
		dataset := apiConfig.Dataset
		return &ApiClientCall{
			Method: http.MethodPut,
			Url:    r.renderCheckRuleUrl(origin, apiConfig.Endpoint, apiConfig.Dataset),
			Execute: func(ctx context.Context) (string, error) {
				apiClient, err := r.apiClientPool.Get(apiConfig.Endpoint, apiConfig.Token)
				if err != nil {
					return "", err
				}
				updatedCheckRule, err := apiClient.UpdateCheckRule(ctx, origin, alertRule, &dataset)
				if err != nil {
					return "", err
				}
				if updatedCheckRule == nil {
					return "", errors.New("unexpected nil/empty response body")
				}
				return dash0apiclient.GetCheckRuleID(updatedCheckRule), nil
			},
		}, nil
	case deleteAction:
		return r.createCheckRuleDeleteApiClientCall(origin, apiConfig), nil
	default:
		return nil, fmt.Errorf("unknown API action: %d", action)
	}
}

// createCheckRuleDeleteApiClientCall creates the call to delete the check rule with the given origin via the Dash0 API
// client.
func (r *PrometheusRuleReconciler) createCheckRuleDeleteApiClientCall(
	origin string,
	apiConfig ApiConfig,
) *ApiClientCall {
	dataset := apiConfig.Dataset
	return &ApiClientCall{
		Method: http.MethodDelete,
		Url:    r.renderCheckRuleUrl(origin, apiConfig.Endpoint, apiConfig.Dataset),
		Execute: func(ctx context.Context) (string, error) {
			apiClient, err := r.apiClientPool.Get(apiConfig.Endpoint, apiConfig.Token)
			if err != nil {
				return "", err
			}
			return "", apiClient.DeleteCheckRule(ctx, origin, &dataset)
		},
	}
}

// createRecordingRuleApiClientCall creates the call to upsert or delete the recording rule with the given origin via
// the Dash0 API client. For upserts, recordingRule is the payload; for deletes it is ignored.
func (r *PrometheusRuleReconciler) createRecordingRuleApiClientCall(
	origin string,
	action apiAction,
	apiConfig ApiConfig,
	recordingRule *dash0apiclient.RecordingRule,
) (*ApiClientCall, error) {
	switch action {
	case upsertAction:
		dataset := apiConfig.Dataset
		return &ApiClientCall{
			Method: http.MethodPut,
			Url:    r.renderRecordingRuleUrl(origin, apiConfig.Endpoint, apiConfig.Dataset),
			Execute: func(ctx context.Context) (string, error) {
				apiClient, err := r.apiClientPool.Get(apiConfig.Endpoint, apiConfig.Token)
				if err != nil {
					return "", err
				}
				updatedRecordingRule, err := apiClient.UpdateRecordingRule(ctx, origin, recordingRule, &dataset)
				if err != nil {
					return "", err
				}
				if updatedRecordingRule == nil {
					return "", errors.New("unexpected nil/empty response body")
				}
				return dash0apiclient.GetRecordingRuleID(updatedRecordingRule), nil
			},
		}, nil
	case deleteAction:
		return r.createRecordingRuleDeleteApiClientCall(origin, apiConfig), nil
	default:
		return nil, fmt.Errorf("unknown API action: %d", action)
	}
}

// createRecordingRuleDeleteApiClientCall creates the call to delete the recording rule with the given origin via the
// Dash0 API client.
func (r *PrometheusRuleReconciler) createRecordingRuleDeleteApiClientCall(
	origin string,
	apiConfig ApiConfig,
) *ApiClientCall {
	dataset := apiConfig.Dataset
	return &ApiClientCall{
		Method: http.MethodDelete,
		Url:    r.renderRecordingRuleUrl(origin, apiConfig.Endpoint, apiConfig.Dataset),
		Execute: func(ctx context.Context) (string, error) {
			apiClient, err := r.apiClientPool.Get(apiConfig.Endpoint, apiConfig.Token)
			if err != nil {
				return "", err
			}
			return "", apiClient.DeleteRecordingRule(ctx, origin, &dataset)
		},
	}
}

// renderCheckRuleListUrl renders the URL to fetch the list of existing check rules with the given origin prefix from
// the Dash0 API. The URL is only used for logging and error messages, the request is sent by the Dash0 API client.
func (r *PrometheusRuleReconciler) renderCheckRuleListUrl(
	originPrefix string,
	endpoint string,
	dataset string,
) string {
	return fmt.Sprintf(
		"%sapi/alerting/check-rules?dataset=%s&originPrefix=%s",
		endpoint,
		url.QueryEscape(dataset),
		url.QueryEscape(originPrefix),
	)
}

// renderCheckRuleUrl renders the URL for a single Dash0 check rule. The URL is only used for logging and error
// messages, the request is sent by the Dash0 API client.
func (r *PrometheusRuleReconciler) renderCheckRuleUrl(
	checkRuleOrigin string,
	endpoint string,
	dataset string,
) string {
	return fmt.Sprintf(
		"%sapi/alerting/check-rules/%s?dataset=%s",
		endpoint,
		url.PathEscape(checkRuleOrigin),
		url.QueryEscape(dataset),
	)
}

// renderRuleOrigins renders the unique origins of all alerting and recording rules in the given groups. The result has
// one slice per group, which holds the origin of each rule of that group, in the order of the rules.
func (r *PrometheusRuleReconciler) renderRuleOrigins(
	preconditionChecksResult *preconditionValidationResult,
	dataset string,
	groups []prometheusv1.RuleGroup,
) [][]string {
	// About duplicate origins: Origins must be unique within a dataset, and they need to be stable against reordering
	// of rules within a group or deletion of individual rules from a group. We derive the origin from the group name
	// and the name of the individual rule. The group name is guaranteed to be unique within a PrometheusRule
	// resource, due to a "x-kubernetes-list-type: map" plus "x-kubernetes-list-map-keys: [name]" annotation in the CRD.
	// The rule name field has no such guarantee, duplicate names within a group are possible. Also, since "_" can occur
	// in group and rule names, two different combinations of group and rule name can render the same origin (e.g.
	// group "a_b" with rule "c" and group "a" with rule "b_c").
	// In a first pass, each rule gets the origin derived from its group and rule name, unless an earlier rule already
	// got that origin. In a second pass, each of the remaining rules gets that origin with a counter appended, which
	// starts at 1 for each origin and skips all origins that are already taken (e.g. "..._alert_1", "..._alert_2").
	// Thus, a rule whose derived origin is unique always keeps it, even if it is literally named like the suffixed
	// origin of a duplicate (e.g. "alert_1").
	// (One downside of this approach is that _within a set of rules with colliding origins_, reordering rules or
	// deleting one of the rules can lead to one rule taking over the origin of another rule, which we generally avoid
	// for rules without duplicate names. Duplicate names should be fairly uncommon in practice, they can be considered
	// a configuration error, so this downside is acceptable.)
	type ruleIndex struct {
		groupIdx int
		ruleIdx  int
	}
	origins := make([][]string, len(groups))
	producedOrigins := make(map[string]int)
	var duplicates []ruleIndex
	for groupIdx, group := range groups {
		origins[groupIdx] = make([]string, len(group.Rules))
		for ruleIdx, rule := range group.Rules {
			ruleName := rule.Record
			if ruleName == "" {
				ruleName = rule.Alert
			}
			origin := r.renderRuleOrigin(preconditionChecksResult, dataset, group.Name, ruleName)
			origins[groupIdx][ruleIdx] = origin
			if _, isDuplicate := producedOrigins[origin]; isDuplicate {
				duplicates = append(duplicates, ruleIndex{groupIdx: groupIdx, ruleIdx: ruleIdx})
				continue
			}
			producedOrigins[origin] = 1
		}
	}
	for _, duplicate := range duplicates {
		origin := origins[duplicate.groupIdx][duplicate.ruleIdx]
		for {
			duplicationIndex := producedOrigins[origin]
			producedOrigins[origin]++
			candidate := origin + "_" + strconv.Itoa(duplicationIndex)
			if _, isDuplicate := producedOrigins[candidate]; !isDuplicate {
				producedOrigins[candidate] = 1
				origins[duplicate.groupIdx][duplicate.ruleIdx] = candidate
				break
			}
		}
	}
	return origins
}

// renderRuleOrigin renders the origin of a single alerting or recording rule, derived from its group and rule name.
// The origin has the pattern
// "dash0-operator_${clusterUid}_${dataset}_${namespace}_${prometheusrule_resource_name}_${group.name}_${ruleName}".
// The origin is not necessarily unique, see renderRuleOrigins.
func (r *PrometheusRuleReconciler) renderRuleOrigin(
	preconditionChecksResult *preconditionValidationResult,
	dataset string,
	groupName string,
	ruleName string,
) string {
	// For now the Dash0 backend treats %2F the same as "/", so we need to replace forward slashes with
	// something other than %2F.
	// See
	// https://stackoverflow.com/questions/71581828/gin-problem-accessing-url-encoded-path-param-containing-forward-slash
	// The character "%" causes a similar problem, so we also replace it.
	// See https://github.com/oapi-codegen/runtime/issues/35
	// Colons ":" also cause issues with the recording rules API (404 errors), so we replace them too. This is
	// particularly relevant for recording rules, which conventionally use colons in their names (e.g.,
	// "job:http_requests:rate5m").
	sanitizedGroupName := strings.ReplaceAll(groupName, "/", "|")
	sanitizedGroupName = strings.ReplaceAll(sanitizedGroupName, "%", "|")
	sanitizedGroupName = strings.ReplaceAll(sanitizedGroupName, ":", "|")
	if ruleName == "" {
		// Rules without a name will not actually be sent to the Dash0 API, since they are deemed invalid by
		// convertAlertingRuleToCheckRule ("rule has neither the alert nor the record attribute").
		ruleName = "unnamed rule"
	}
	sanitizedRuleName := strings.ReplaceAll(ruleName, "/", "|")
	sanitizedRuleName = strings.ReplaceAll(sanitizedRuleName, "%", "|")
	sanitizedRuleName = strings.ReplaceAll(sanitizedRuleName, ":", "|")

	return fmt.Sprintf(
		"%s%s_%s",
		r.renderRuleOriginPrefix(preconditionChecksResult, dataset),
		sanitizedGroupName,
		sanitizedRuleName,
	)
}

// renderRuleOriginPrefix renders the common origin prefix for all alerting or recording rules that are
// created from one Kubernetes PrometheusRule resource.
func (r *PrometheusRuleReconciler) renderRuleOriginPrefix(
	preconditionChecksResult *preconditionValidationResult,
	dataset string,
) string {
	return fmt.Sprintf(
		"%s%s_",
		r.renderRuleOriginPrefixForNamespace(preconditionChecksResult.k8sNamespace, dataset),
		preconditionChecksResult.k8sName,
	)
}

// renderRuleOriginPrefixForNamespace renders the common origin prefix for all alerting or recording rules that are
// created from Kubernetes PrometheusRule resources in one namespace.
func (r *PrometheusRuleReconciler) renderRuleOriginPrefixForNamespace(
	namespace string,
	dataset string,
) string {
	return fmt.Sprintf(
		// we deliberately use _ as the separator, since that is an illegal character in Kubernetes names. This avoids
		// any potential naming collisions (e.g. namespace="abc" & name="def-ghi" vs. namespace="abc-def" & name="ghi").
		"dash0-operator_%s_%s_%s_",
		r.pseudoClusterUid,
		dataset,
		namespace,
	)
}

// renderRecordingRuleListUrl renders the URL to fetch the list of existing recording rules with the given origin prefix
// from the Dash0 API. The URL is only used for logging and error messages, the request is sent by the Dash0 API client.
func (r *PrometheusRuleReconciler) renderRecordingRuleListUrl(
	originPrefix string,
	endpoint string,
	dataset string,
) string {
	return fmt.Sprintf(
		"%sapi/recording-rules?dataset=%s&originPrefix=%s",
		endpoint,
		url.QueryEscape(dataset),
		url.QueryEscape(originPrefix),
	)
}

// renderRecordingRuleUrl renders the URL for a single Dash0 recording rule. The URL is only used for logging and error
// messages, the request is sent by the Dash0 API client.
func (r *PrometheusRuleReconciler) renderRecordingRuleUrl(
	recordingRuleOrigin string,
	endpoint string,
	dataset string,
) string {
	return fmt.Sprintf(
		"%sapi/recording-rules/%s?dataset=%s",
		endpoint,
		url.PathEscape(recordingRuleOrigin),
		url.QueryEscape(dataset),
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

// mapToRecordingRule converts a Prometheus recording rule to the recording rule type of the Dash0 API client, which is
// a PrometheusRule resource with a single group containing a single rule.
func mapToRecordingRule(
	rule prometheusv1.Rule,
	groupName string,
	interval *prometheusv1.Duration,
) (*dash0apiclient.RecordingRule, error) {
	prometheusRule := map[string]any{
		"apiVersion": "monitoring.coreos.com/v1",
		"kind":       "PrometheusRule",
		"metadata": map[string]any{
			"name": fmt.Sprintf("%s - %s", groupName, rule.Record),
		},
		"spec": map[string]any{
			"groups": []map[string]any{
				{
					"name":     groupName,
					"interval": convertDuration(interval),
					"rules": []map[string]any{
						{
							"record": rule.Record,
							"expr":   convertIntOrString(rule.Expr),
							"labels": rule.Labels,
						},
					},
				},
			},
		},
	}
	serializedRecordingRule, err := json.Marshal(prometheusRule)
	if err != nil {
		return nil, err
	}
	recordingRule := &dash0apiclient.RecordingRule{}
	if err = json.Unmarshal(serializedRecordingRule, recordingRule); err != nil {
		return nil, err
	}
	return recordingRule, nil
}

// mapToPrometheusAlertRule converts a validated check rule to the check rule type of the Dash0 API client. The check
// rule is decoded directly instead of using dash0apiclient.ConvertPrometheusRuleToPrometheusAlertRule, since that would
// move the threshold and enabled annotations out of the annotations; they are sent as annotations to the Dash0 API.
func mapToPrometheusAlertRule(checkRule *CheckRule) (*dash0apiclient.PrometheusAlertRule, error) {
	serializedCheckRule, err := json.Marshal(checkRule)
	if err != nil {
		return nil, err
	}
	alertRule := &dash0apiclient.PrometheusAlertRule{}
	if err = json.Unmarshal(serializedCheckRule, alertRule); err != nil {
		return nil, err
	}
	return alertRule, nil
}

// convertAlertingRuleToCheckRule converts a Prometheus alerting rule to a CheckRule. It returns the converted CheckRule
// if the conversion is successful and there are no validation issues. Otherwise, a list of validation issues is
// returned. There are also rules which we want to silently skip, in which case no rule and no validation issues are
// returned, but the boolean return value is false.
// Note: recording rules (rule.Record != "") are handled separately in MapResourceToApiCalls and should not reach
// this function.
func convertAlertingRuleToCheckRule(
	rule prometheusv1.Rule,
	action apiAction,
	groupName string,
	interval *prometheusv1.Duration,
	metadataAnnotations map[string]string,
	logger logd.Logger,
) (*CheckRule, []string, bool) {
	if rule.Alert == "" {
		logger.Warn(
			fmt.Sprintf(
				"Found invalid rule in group %s which has neither a record nor an alert attribute.", groupName,
			),
		)
		return nil, []string{"rule has neither the alert nor the record attribute"}, false
	}

	if action == deleteAction {
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

// CreateDeleteRequests produces one DELETE request for each existing check rule or recording rule that is not in the
// K8s resource (anymore), sent to the API the rule exists in. Check rules and recording rules are compared separately,
// since a check rule and a recording rule can have the same origin, for example when an alerting rule takes over the
// origin a recording rule had before, and the recording rule moves to an origin with a counter (see renderRuleOrigins).
func (r *PrometheusRuleReconciler) CreateDeleteRequests(
	apiConfig ApiConfig,
	existingOriginsFromApi []ApiObjectOrigin,
	originsInResource []ApiObjectOrigin,
) []WrappedApiRequest {
	var deleteRequests []WrappedApiRequest
	for _, existingOrigin := range existingOriginsFromApi {
		if slices.Contains(originsInResource, existingOrigin) {
			continue
		}
		var deleteCall *ApiClientCall
		switch existingOrigin.ObjectType {
		case checkRuleObjectType:
			deleteCall = r.createCheckRuleDeleteApiClientCall(existingOrigin.Origin, apiConfig)
		case recordingRuleObjectType:
			deleteCall = r.createRecordingRuleDeleteApiClientCall(existingOrigin.Origin, apiConfig)
		default:
			continue
		}
		deleteRequests = append(
			deleteRequests, WrappedApiRequest{
				ApiClientCall: deleteCall,
				ItemName:      existingOrigin.Origin + " (deleted)",
				Origin:        existingOrigin.Origin,
				ObjectType:    existingOrigin.ObjectType,
			},
		)
	}
	return deleteRequests
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
			Dash0ApiEndpoint:                    resultPerApiConfig.resourceToRequestsResult.ApiConfig.Endpoint,
			Dash0Dataset:                        resultPerApiConfig.resourceToRequestsResult.ApiConfig.Dataset,
			SynchronizedRulesTotal:              len(resultPerApiConfig.successfullySynchronized),
			SynchronizedRulesAttributes:         synchronizedRuleAttributes,
			SynchronizationErrorsTotal:          len(resultPerApiConfig.resourceToRequestsResult.SynchronizationErrors),
			SynchronizationErrors:               resultPerApiConfig.resourceToRequestsResult.SynchronizationErrors,
			SynchronizationErrorHttpStatusCodes: resultPerApiConfig.resourceToRequestsResult.SynchronizationErrorStatusCodes,
		}

		rulesSyncResults = append(rulesSyncResults, rulesSyncRes)
	}

	result := dash0common.PrometheusRuleSynchronizationResult{
		SynchronizationStatus:  status,
		SynchronizedAt:         metav1.Time{Time: time.Now()},
		AlertingRulesTotal:     syncResults.alertingRulesTotal,
		RecordingRulesTotal:    syncResults.recordingRulesTotal,
		InvalidRulesTotal:      len(syncResults.validationIssues),
		InvalidRules:           syncResults.validationIssues,
		SynchronizationResults: rulesSyncResults,
	}
	previousResults[qualifiedName] = result
	return result
}

// synchronizeNamespacedResources explicitly triggers a resync of check rules in a given namespace in response to an
// updated API endpoint, dataset or auth token, or when synchronization has been enabled for the namespace.
func (r *PrometheusRuleReconciler) synchronizeNamespacedResources(
	ctx context.Context,
	namespace string,
	logger logd.Logger,
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

		originsInNamespace := newExistingOriginsInNamespace(namespace)
		for i := range allRulesResourcesInNamespace.Items {
			r.resyncViaApi(ctx, &allRulesResourcesInNamespace.Items[i], originsInNamespace)

			// stagger API requests a bit
			time.Sleep(50 * time.Millisecond)
		}
		logger.Info(fmt.Sprintf("Triggering synchronization of check rules in namespace %s has finished.", namespace))
	}()
}
