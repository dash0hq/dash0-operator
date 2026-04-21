// SPDX-FileCopyrightText: Copyright 2026 Dash0 Inc.
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
	"reflect"
	"slices"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	otelmetric "go.opentelemetry.io/otel/metric"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	dash0common "github.com/dash0hq/dash0-operator/api/operator/common"
	dash0v1alpha1 "github.com/dash0hq/dash0-operator/api/operator/v1alpha1"
	"github.com/dash0hq/dash0-operator/internal/selfmonitoringapiaccess"
	"github.com/dash0hq/dash0-operator/internal/util"
	"github.com/dash0hq/dash0-operator/internal/util/logd"
)

type SamplingRuleReconciler struct {
	client.Client
	pseudoClusterUid       types.UID
	leaderElectionAware    util.LeaderElectionAware
	httpClient             *http.Client
	defaultApiConfigs      selfmonitoringapiaccess.SynchronizedSlice[ApiConfig]
	initialSyncMutex       sync.Mutex
	initialSyncHasHappened atomic.Bool
}

var (
	samplingRuleReconcileRequestMetric otelmetric.Int64Counter
)

func NewSamplingRuleReconciler(
	k8sClient client.Client,
	pseudoClusterUid types.UID,
	leaderElectionAware util.LeaderElectionAware,
	httpClient *http.Client,
) *SamplingRuleReconciler {
	return &SamplingRuleReconciler{
		Client:              k8sClient,
		pseudoClusterUid:    pseudoClusterUid,
		leaderElectionAware: leaderElectionAware,
		httpClient:          httpClient,
		defaultApiConfigs:   *selfmonitoringapiaccess.NewSynchronizedSlice[ApiConfig](),
	}
}

func (r *SamplingRuleReconciler) SetupWithManager(mgr manager.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&dash0v1alpha1.Dash0SamplingRule{}).
		WithEventFilter(samplingRulePredicate{}).
		Complete(r)
}

func (r *SamplingRuleReconciler) InitializeSelfMonitoringMetrics(
	meter otelmetric.Meter,
	metricNamePrefix string,
	logger logd.Logger,
) {
	reconcileRequestMetricName := fmt.Sprintf("%s%s", metricNamePrefix, "samplingrule.reconcile_requests")
	var err error
	if samplingRuleReconcileRequestMetric, err = meter.Int64Counter(
		reconcileRequestMetricName,
		otelmetric.WithUnit("1"),
		otelmetric.WithDescription("Counter for sampling rule reconcile requests"),
	); err != nil {
		logger.Error(err, fmt.Sprintf("Cannot initialize the metric %s.", reconcileRequestMetricName))
	}
}

func (r *SamplingRuleReconciler) KindDisplayName() string {
	return "sampling rule"
}

func (r *SamplingRuleReconciler) ShortName() string {
	return "sampling-rule"
}

func (r *SamplingRuleReconciler) GetDefaultApiConfigs() []ApiConfig {
	return r.defaultApiConfigs.Get()
}

func (r *SamplingRuleReconciler) GetNamespacedApiConfigs(_ string) ([]ApiConfig, bool) {
	return nil, false
}

func (r *SamplingRuleReconciler) ControllerName() string {
	return "dash0_sampling_rule_controller"
}

func (r *SamplingRuleReconciler) K8sClient() client.Client {
	return r.Client
}

func (r *SamplingRuleReconciler) HttpClient() *http.Client {
	return r.httpClient
}

func (r *SamplingRuleReconciler) SetDefaultApiConfigs(
	ctx context.Context,
	apiConfigs []ApiConfig,
	logger logd.Logger,
) {
	if len(apiConfigs) > 1 {
		logger.Info("Sampling rules only support a single API config, using the first one and ignoring the rest.",
			"total", len(apiConfigs))
		apiConfigs = apiConfigs[:1]
	}
	r.defaultApiConfigs.Set(apiConfigs)
	r.maybeDoInitialSynchronizationOfAllResources(ctx, logger)
}

func (r *SamplingRuleReconciler) RemoveDefaultApiConfigs(_ context.Context, _ logd.Logger) {
	r.defaultApiConfigs.Clear()
}

func (r *SamplingRuleReconciler) NotifyOperatorManagerJustBecameLeader(ctx context.Context, logger logd.Logger) {
	r.maybeDoInitialSynchronizationOfAllResources(ctx, logger)
}

func (r *SamplingRuleReconciler) maybeDoInitialSynchronizationOfAllResources(
	ctx context.Context,
	logger logd.Logger,
) {
	r.initialSyncMutex.Lock()
	defer r.initialSyncMutex.Unlock()

	if r.initialSyncHasHappened.Load() {
		return
	}

	if !r.leaderElectionAware.IsLeader() {
		logger.Info(
			"Waiting for this operator manager replica to become leader before running initial " +
				"synchronization of sampling rules.",
		)
		return
	}
	if len(filterValidApiConfigs(r.defaultApiConfigs.Get(), logger, "default operator configuration")) == 0 {
		logger.Info(
			"Waiting for the Dash0 API config before running initial synchronization of sampling rules. Either " +
				"no Dash0 API config has been provided via the operator configuration resource, or the operator " +
				"configuration resource has not been reconciled yet. If there is an operator configuration resource " +
				"with an API endpoint and a Dash0 auth token or a secret ref present in the cluster, it will be " +
				"reconciled in a few seconds and this message can be safely ignored.",
		)
		return
	}

	logger.Info("Running initial synchronization of sampling rules now.")

	go func() {
		allSamplingRuleResourcesInCluster := dash0v1alpha1.Dash0SamplingRuleList{}
		if err := r.List(
			ctx,
			&allSamplingRuleResourcesInCluster,
			&client.ListOptions{},
		); err != nil {
			logger.Error(err, "Failed to list all Dash0 sampling rule resources.")
			return
		}

		for _, samplingRuleResource := range allSamplingRuleResourcesInCluster.Items {
			pseudoReconcileRequest := ctrl.Request{
				NamespacedName: client.ObjectKey{
					Name: samplingRuleResource.Name,
				},
			}
			_, _ = r.Reconcile(ctx, pseudoReconcileRequest)
			time.Sleep(50 * time.Millisecond)
		}
		logger.Info("Initial synchronization of sampling rules has finished.")
		r.initialSyncHasHappened.Store(true)
	}()
}

func (r *SamplingRuleReconciler) Reconcile(ctx context.Context, req reconcile.Request) (reconcile.Result, error) {
	if samplingRuleReconcileRequestMetric != nil {
		samplingRuleReconcileRequestMetric.Add(ctx, 1)
	}

	qualifiedName := req.NamespacedName.String() //nolint:staticcheck
	logger := logd.FromContext(ctx)
	logger.Info("processing reconcile request for a sampling rule resource", "name", qualifiedName)

	action := upsertAction
	samplingRuleResource := &dash0v1alpha1.Dash0SamplingRule{}
	if err := r.Get(ctx, req.NamespacedName, samplingRuleResource); err != nil {
		if apierrors.IsNotFound(err) {
			action = deleteAction
			logger.Info("reconciling the deletion of the sampling rule resource", "name", qualifiedName)
			samplingRuleResource = &dash0v1alpha1.Dash0SamplingRule{
				ObjectMeta: metav1.ObjectMeta{
					Name: req.Name,
				},
			}
		} else {
			logger.Error(
				err,
				fmt.Sprintf(
					"Failed to get the sampling rule \"%s\", requeuing reconcile request.",
					qualifiedName,
				),
			)
			return ctrl.Result{}, err
		}
	}

	unstructuredResource, err := structToMap(samplingRuleResource)
	if err != nil {
		msg := "cannot serialize the sampling rule resource"
		logger.Error(err, msg)
		if action != deleteAction {
			r.WriteSynchronizationResultToSynchronizedResource(
				ctx,
				samplingRuleResource,
				synchronizationResults{},
				logger,
			)
		}
		return reconcile.Result{}, nil
	}

	synchronizeViaApiAndUpdateStatus(
		ctx,
		r,
		unstructuredResource,
		samplingRuleResource,
		action,
		logger,
	)

	return reconcile.Result{}, nil
}

func (r *SamplingRuleReconciler) MapResourceToHttpRequests(
	preconditionChecksResult *preconditionValidationResult,
	apiConfig ApiConfig,
	action apiAction,
	logger logd.Logger,
) *ResourceToRequestsResult {
	itemName := preconditionChecksResult.k8sName
	samplingRuleUrl, samplingRuleOrigin := r.renderSamplingRuleUrl(
		preconditionChecksResult,
		apiConfig.Endpoint,
		apiConfig.Dataset,
	)

	var req *http.Request
	var method string
	var err error

	switch action {
	case upsertAction:
		apiPayload, transformErr := transformToSamplingApiPayload(preconditionChecksResult.resource, itemName, apiConfig.Dataset)
		if transformErr != nil {
			logger.Error(transformErr, "error transforming sampling rule to API payload")
			return NewResourceToRequestsResultSingleItemError(apiConfig, itemName, transformErr.Error())
		}
		serialized, _ := json.Marshal(apiPayload)
		method = http.MethodPut
		req, err = http.NewRequest(method, samplingRuleUrl, bytes.NewBuffer(serialized))
	case deleteAction:
		method = http.MethodDelete
		req, err = http.NewRequest(method, samplingRuleUrl, nil)
	default:
		unknownActionErr := fmt.Errorf("unknown API action: %d", action)
		logger.Error(unknownActionErr, "unknown API action")
		return NewResourceToRequestsResultSingleItemError(apiConfig, itemName, unknownActionErr.Error())
	}

	if err != nil {
		httpError := fmt.Errorf(
			"unable to create a new HTTP request to synchronize the sampling rule: %s %s: %w",
			method, samplingRuleUrl, err)
		logger.Error(httpError, "error creating http request")
		return NewResourceToRequestsResultSingleItemError(apiConfig, itemName, httpError.Error())
	}

	addAuthorizationHeader(req, apiConfig.Token)
	if action == upsertAction {
		req.Header.Set(util.ContentTypeHeaderName, util.ApplicationJsonMediaType)
	}

	return NewResourceToRequestsResultSingleItemSuccess(apiConfig, req, itemName, samplingRuleOrigin)
}

// transformToSamplingApiPayload converts the K8s resource map into the SamplingDefinition format expected by the
// Dash0 API. The API expects kind "Dash0Sampling", simplified metadata, and the probabilistic rate as a number
// rather than a string.
func transformToSamplingApiPayload(resource map[string]interface{}, name string, dataset string) (map[string]interface{}, error) {
	spec, ok := resource["spec"].(map[string]interface{})
	if !ok {
		return nil, fmt.Errorf("sampling rule %s has no spec", name)
	}

	specCopy, err := deepCopyMap(spec)
	if err != nil {
		return nil, fmt.Errorf("sampling rule %s: %w", name, err)
	}
	convertConditionRates(specCopy)

	apiPayload := map[string]interface{}{
		"kind": "Dash0Sampling",
		"metadata": map[string]interface{}{
			"name": name,
			"labels": map[string]interface{}{
				"dash0.com/dataset": dataset,
			},
		},
		"spec": specCopy,
	}
	return apiPayload, nil
}

func deepCopyMap(m map[string]interface{}) (map[string]interface{}, error) {
	raw, err := json.Marshal(m)
	if err != nil {
		return nil, fmt.Errorf("cannot marshal map for deep copy: %w", err)
	}
	var result map[string]interface{}
	if err = json.Unmarshal(raw, &result); err != nil {
		return nil, fmt.Errorf("cannot unmarshal map for deep copy: %w", err)
	}
	return result, nil
}

// convertConditionRates recursively converts probabilistic rate values from string to float in condition maps.
func convertConditionRates(spec map[string]interface{}) {
	conditions, ok := spec["conditions"]
	if !ok {
		return
	}
	condMap, ok := conditions.(map[string]interface{})
	if !ok {
		return
	}
	convertRateInCondition(condMap)
}

func convertRateInCondition(condition map[string]interface{}) {
	condSpec, ok := condition["spec"].(map[string]interface{})
	if !ok {
		return
	}
	if rateStr, ok := condSpec["rate"].(string); ok {
		if rate, err := strconv.ParseFloat(rateStr, 64); err == nil {
			condSpec["rate"] = rate
		}
	}
	if subConditions, ok := condSpec["conditions"].([]interface{}); ok {
		for _, sub := range subConditions {
			if subMap, ok := sub.(map[string]interface{}); ok {
				convertRateInCondition(subMap)
			}
		}
	}
}

func (r *SamplingRuleReconciler) renderSamplingRuleUrl(
	preconditionChecksResult *preconditionValidationResult,
	endpoint string,
	dataset string,
) (string, string) {
	datasetUrlEncoded := url.QueryEscape(dataset)
	samplingRuleOrigin := fmt.Sprintf(
		"dash0-operator_%s_%s_%s",
		r.pseudoClusterUid,
		datasetUrlEncoded,
		preconditionChecksResult.k8sName,
	)
	return fmt.Sprintf(
		"%sapi/sampling-rules/%s?dataset=%s",
		endpoint,
		samplingRuleOrigin,
		datasetUrlEncoded,
	), samplingRuleOrigin
}

func (r *SamplingRuleReconciler) ExtractIdFromResponseBody(
	responseBytes []byte,
	logger logd.Logger,
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

func (r *SamplingRuleReconciler) WriteSynchronizationResultToSynchronizedResource(
	ctx context.Context,
	synchronizedResource client.Object,
	syncResults synchronizationResults,
	logger logd.Logger,
) {
	samplingRule := synchronizedResource.(*dash0v1alpha1.Dash0SamplingRule)

	samplingRule.Status.SynchronizationStatus = syncResults.resourceSyncStatus()
	samplingRule.Status.SynchronizedAt = metav1.Time{Time: time.Now()}
	samplingRule.Status.ValidationIssues = nil

	samplingRuleSyncResults := make([]dash0v1alpha1.Dash0SamplingRuleSynchronizationResultPerEndpointAndDataset, 0,
		len(syncResults.resultsPerApiConfig))
	for _, res := range syncResults.resultsPerApiConfig {
		synchronizationStatus := dash0common.Dash0ApiResourceSynchronizationStatusFailed
		synchronizationError := ""
		if len(res.resourceToRequestsResult.SynchronizationErrors) > 0 {
			synchronizationError = slices.Collect(maps.Values(res.resourceToRequestsResult.SynchronizationErrors))[0]
		} else {
			synchronizationError = ""
			synchronizationStatus = dash0common.Dash0ApiResourceSynchronizationStatusSuccessful
		}
		syncResultPerEndpointAndDataset := dash0v1alpha1.Dash0SamplingRuleSynchronizationResultPerEndpointAndDataset{
			SynchronizationStatus: synchronizationStatus,
			Dash0ApiEndpoint:      res.apiConfig.Endpoint,
			Dash0Dataset:          res.apiConfig.Dataset,
			SynchronizationError:  synchronizationError,
		}
		if len(res.successfullySynchronized) > 0 {
			synchronized := res.successfullySynchronized[0]
			if synchronized.Labels.Id != "" {
				syncResultPerEndpointAndDataset.Dash0Id = synchronized.Labels.Id
			}
			if synchronized.Labels.Origin != "" {
				syncResultPerEndpointAndDataset.Dash0Origin = synchronized.Labels.Origin
			}
		}
		samplingRuleSyncResults = append(samplingRuleSyncResults, syncResultPerEndpointAndDataset)
	}
	samplingRule.Status.SynchronizationResults = samplingRuleSyncResults

	if err := r.Status().Update(ctx, samplingRule); err != nil {
		logger.Error(err, "Failed to update Dash0 sampling rule status.")
	}
}

// An event filter that ignores changes in the status subresource but reacts on changes to spec, labels and annotations.
// Using predicate.GenerationChangedPredicate would also ignore label and annotation changes, which is not desired since
// we need to react to the dash0.com/enable label.
type samplingRulePredicate struct {
	predicate.Funcs
}

func (p samplingRulePredicate) Update(e event.UpdateEvent) bool {
	if e.ObjectOld == nil || e.ObjectNew == nil {
		return true
	}

	oldObj, okOld := e.ObjectOld.(*dash0v1alpha1.Dash0SamplingRule)
	newObj, okNew := e.ObjectNew.(*dash0v1alpha1.Dash0SamplingRule)

	if !okOld || !okNew {
		return true
	}

	specChanged := !reflect.DeepEqual(oldObj.Spec, newObj.Spec)
	labelsChanged := !reflect.DeepEqual(oldObj.Labels, newObj.Labels)
	annotationsChanged := !reflect.DeepEqual(oldObj.Annotations, newObj.Annotations)

	return specChanged || labelsChanged || annotationsChanged
}
