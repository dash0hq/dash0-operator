// SPDX-FileCopyrightText: Copyright 2026 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package controller

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"slices"
	"sync"
	"sync/atomic"
	"time"

	dash0apiclient "github.com/dash0hq/dash0-api-client-go"
	otelmetric "go.opentelemetry.io/otel/metric"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	dash0common "github.com/dash0hq/dash0-operator/api/operator/common"
	dash0v1alpha1 "github.com/dash0hq/dash0-operator/api/operator/v1alpha1"
	dash0v1beta1 "github.com/dash0hq/dash0-operator/api/operator/v1beta1"
	"github.com/dash0hq/dash0-operator/internal/selfmonitoringapiaccess"
	"github.com/dash0hq/dash0-operator/internal/util"
	"github.com/dash0hq/dash0-operator/internal/util/logd"
)

type GenerativeAiEvaluationReconciler struct {
	client.Client
	pseudoClusterUid      types.UID
	leaderElectionAware   util.LeaderElectionAware
	apiClientPool         *ApiClientPool
	defaultApiConfigs     selfmonitoringapiaccess.SynchronizedSlice[ApiConfig]
	namespacedApiConfigs  selfmonitoringapiaccess.SynchronizedMapSlice[ApiConfig]
	initialSyncMutex      sync.Mutex
	initialSyncInProgress atomic.Bool
	initialSyncHasHappend atomic.Bool
	namespacedSyncMutex   selfmonitoringapiaccess.NamespaceMutex
}

var (
	generativeAiEvaluationReconcileRequestMetric otelmetric.Int64Counter
)

func NewGenerativeAiEvaluationReconciler(
	k8sClient client.Client,
	pseudoClusterUid types.UID,
	leaderElectionAware util.LeaderElectionAware,
	apiClientPool *ApiClientPool,
) *GenerativeAiEvaluationReconciler {
	return &GenerativeAiEvaluationReconciler{
		Client:               k8sClient,
		pseudoClusterUid:     pseudoClusterUid,
		leaderElectionAware:  leaderElectionAware,
		apiClientPool:        apiClientPool,
		defaultApiConfigs:    *selfmonitoringapiaccess.NewSynchronizedSlice[ApiConfig](),
		namespacedApiConfigs: *selfmonitoringapiaccess.NewSynchronizedMapSlice[ApiConfig](),
		namespacedSyncMutex:  *selfmonitoringapiaccess.NewNamespaceMutex(),
	}
}

func (r *GenerativeAiEvaluationReconciler) SetupWithManager(mgr manager.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&dash0v1alpha1.Dash0GenerativeAiEvaluation{}).
		// ignore changes in the status subresource, but react on changes to spec, labels and dash0.com/ annotations
		WithEventFilter(generationLabelOrDash0AnnotationChangePredicate).
		Complete(r)
}

func (r *GenerativeAiEvaluationReconciler) InitializeSelfMonitoringMetrics(
	meter otelmetric.Meter,
	metricNamePrefix string,
	logger logd.Logger,
) {
	reconcileRequestMetricName := fmt.Sprintf("%s%s", metricNamePrefix, "generativeaievaluation.reconcile_requests")
	var err error
	if generativeAiEvaluationReconcileRequestMetric, err = meter.Int64Counter(
		reconcileRequestMetricName,
		otelmetric.WithUnit("1"),
		otelmetric.WithDescription("Counter for generative AI evaluation reconcile requests"),
	); err != nil {
		logger.Error(err, fmt.Sprintf("Cannot initialize the metric %s.", reconcileRequestMetricName))
	}
}

func (r *GenerativeAiEvaluationReconciler) KindDisplayName() string {
	return "generative AI evaluation"
}

func (r *GenerativeAiEvaluationReconciler) ShortName() string {
	return "generative-ai-evaluation"
}

func (r *GenerativeAiEvaluationReconciler) GetDefaultApiConfigs() []ApiConfig {
	return r.defaultApiConfigs.Get()
}

func (r *GenerativeAiEvaluationReconciler) GetNamespacedApiConfigs(namespace string) ([]ApiConfig, bool) {
	return r.namespacedApiConfigs.Get(namespace)
}

func (r *GenerativeAiEvaluationReconciler) ControllerName() string {
	return "dash0_generative_ai_evaluation_controller"
}

func (r *GenerativeAiEvaluationReconciler) K8sClient() client.Client {
	return r.Client
}

func (r *GenerativeAiEvaluationReconciler) SetDefaultApiConfigs(
	ctx context.Context,
	apiConfigs []ApiConfig,
	logger logd.Logger,
) {
	r.defaultApiConfigs.Set(apiConfigs)
	r.maybeDoInitialSynchronizationOfAllResources(ctx, logger)
}

func (r *GenerativeAiEvaluationReconciler) RemoveDefaultApiConfigs(_ context.Context, _ logd.Logger) {
	r.defaultApiConfigs.Clear()
}

func (r *GenerativeAiEvaluationReconciler) SetNamespacedApiConfigs(
	ctx context.Context,
	namespace string,
	updatedApiConfigs []ApiConfig,
	logger logd.Logger,
) {
	if updatedApiConfigs != nil {
		previousApiConfigs, _ := r.namespacedApiConfigs.Get(namespace)

		r.namespacedApiConfigs.Set(namespace, updatedApiConfigs)

		if !slices.Equal(previousApiConfigs, updatedApiConfigs) {
			r.synchronizeNamespacedResources(ctx, namespace, logger)
		}
	}
}

func (r *GenerativeAiEvaluationReconciler) RemoveNamespacedApiConfigs(
	ctx context.Context,
	namespace string,
	logger logd.Logger,
) {
	if _, exists := r.namespacedApiConfigs.Get(namespace); exists {
		r.namespacedApiConfigs.Delete(namespace)
		r.synchronizeNamespacedResources(ctx, namespace, logger)
	}
}

func (r *GenerativeAiEvaluationReconciler) SetSynchronizationEnabled(
	_ context.Context,
	_ string,
	_ *dash0v1beta1.Dash0Monitoring,
	_ logd.Logger,
) {
	// no-op: generative AI evaluations do not have a per-namespace sync toggle
}

func (r *GenerativeAiEvaluationReconciler) RemoveSynchronizationEnabled(_ string) {
	// no-op: generative AI evaluations do not have a per-namespace sync toggle
}

func (r *GenerativeAiEvaluationReconciler) NotifyOperatorManagerJustBecameLeader(ctx context.Context, logger logd.Logger) {
	r.maybeDoInitialSynchronizationOfAllResources(ctx, logger)
}

func (r *GenerativeAiEvaluationReconciler) maybeDoInitialSynchronizationOfAllResources(
	ctx context.Context,
	logger logd.Logger,
) {
	r.initialSyncMutex.Lock()
	defer r.initialSyncMutex.Unlock()

	if r.initialSyncHasHappend.Load() || r.initialSyncInProgress.Load() {
		return
	}

	if !r.leaderElectionAware.IsLeader() {
		logger.Info(
			fmt.Sprintf(
				"Waiting for this operator manager replica to become leader before running initial " +
					"synchronization of generative AI evaluations.",
			),
		)
		return
	}
	if len(filterValidApiConfigs(r.defaultApiConfigs.Get(), logger, "default operator configuration")) == 0 {
		logger.Info(
			"Waiting for the Dash0 API config before running initial synchronization of generative AI evaluations. " +
				"Either no Dash0 API config has been provided via the operator configuration resource, or the " +
				"operator configuration resource has not been reconciled yet. If there is an operator configuration " +
				"resource with an API endpoint and a Dash0 auth token or a secret ref present in the cluster, it will " +
				"be reconciled in a few seconds and this message can be safely ignored.",
		)
		return
	}

	logger.Info("Running initial synchronization of generative AI evaluations now.")
	r.initialSyncInProgress.Store(true)

	go func() {
		defer r.initialSyncInProgress.Store(false)

		allResources := dash0v1alpha1.Dash0GenerativeAiEvaluationList{}
		if err := r.List(
			ctx,
			&allResources,
			&client.ListOptions{},
		); err != nil {
			logger.Error(err, "Failed to list all Dash0 generative AI evaluation resources.")
			return
		}

		for _, resource := range allResources.Items {
			pseudoReconcileRequest := ctrl.Request{
				NamespacedName: client.ObjectKey{
					Namespace: resource.Namespace,
					Name:      resource.Name,
				},
			}
			_, _ = r.Reconcile(ctx, pseudoReconcileRequest)
			// stagger API requests a bit
			time.Sleep(50 * time.Millisecond)
		}
		logger.Info("Initial synchronization of generative AI evaluations has finished.")
		r.initialSyncHasHappend.Store(true)
	}()
}

func (r *GenerativeAiEvaluationReconciler) synchronizeNamespacedResources(
	ctx context.Context,
	namespace string,
	logger logd.Logger,
) {
	if !r.leaderElectionAware.IsLeader() {
		logger.Info(
			fmt.Sprintf(
				"Waiting for this operator manager replica to become leader before running " +
					"synchronization of generative AI evaluations.",
			),
		)
		return
	}

	// namespacedSyncMutex is used so we don't trigger multiple syncs in parallel in a single namespace.
	// That happens for example when the export from a monitoring resource is removed, since that updates both the API
	// config and auth token at almost the same time, triggering two resyncs.
	r.namespacedSyncMutex.Lock(namespace)

	logger.Info(fmt.Sprintf("Running synchronization of generative AI evaluations in namespace %s now.", namespace))

	go func() {
		defer r.namespacedSyncMutex.Unlock(namespace)

		allResources := dash0v1alpha1.Dash0GenerativeAiEvaluationList{}
		if err := r.List(
			ctx,
			&allResources,
			&client.ListOptions{
				Namespace: namespace,
			},
		); err != nil {
			logger.Error(
				err,
				fmt.Sprintf("Failed to list Dash0 generative AI evaluation resources in namespace %s.", namespace),
			)
			return
		}

		for _, resource := range allResources.Items {
			pseudoReconcileRequest := ctrl.Request{
				NamespacedName: client.ObjectKey{
					Namespace: resource.Namespace,
					Name:      resource.Name,
				},
			}
			_, _ = r.Reconcile(ctx, pseudoReconcileRequest)
			// stagger API requests a bit
			time.Sleep(50 * time.Millisecond)
		}
		logger.Info(
			fmt.Sprintf("Synchronization of generative AI evaluations in namespace %s has finished.", namespace),
		)
	}()
}

func (r *GenerativeAiEvaluationReconciler) Reconcile(ctx context.Context, req reconcile.Request) (reconcile.Result, error) {
	if generativeAiEvaluationReconcileRequestMetric != nil {
		generativeAiEvaluationReconcileRequestMetric.Add(ctx, 1)
	}

	qualifiedName := req.NamespacedName.String() //nolint:staticcheck
	logger := logd.FromContext(ctx)
	logger.Info("processing reconcile request for a generative AI evaluation resource", "name", qualifiedName)

	action := upsertAction
	generativeAiEvaluationResource := &dash0v1alpha1.Dash0GenerativeAiEvaluation{}
	if err := r.Get(ctx, req.NamespacedName, generativeAiEvaluationResource); err != nil {
		if apierrors.IsNotFound(err) {
			action = deleteAction
			logger.Info("reconciling the deletion of the generative AI evaluation resource", "name", qualifiedName)
			generativeAiEvaluationResource = &dash0v1alpha1.Dash0GenerativeAiEvaluation{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: req.Namespace,
					Name:      req.Name,
				},
			}
		} else {
			logger.Error(
				err,
				fmt.Sprintf(
					"Failed to get the generative AI evaluation \"%s\", requeuing reconcile request.",
					qualifiedName,
				),
			)
			return ctrl.Result{}, err
		}
	}

	unstructuredResource, err := structToMap(generativeAiEvaluationResource)
	if err != nil {
		msg := "cannot serialize the generative AI evaluation resource"
		logger.Error(err, msg)
		if action != deleteAction {
			r.WriteSynchronizationResultToSynchronizedResource(
				ctx,
				generativeAiEvaluationResource,
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
		generativeAiEvaluationResource,
		action,
		logger,
	)

	return reconcile.Result{}, nil
}

func (r *GenerativeAiEvaluationReconciler) MapResourceToApiCalls(
	preconditionChecksResult *preconditionValidationResult,
	apiConfig ApiConfig,
	action apiAction,
	logger logd.Logger,
) *ResourceToRequestsResult {
	itemName := preconditionChecksResult.k8sName

	generativeAiEvaluationUrl, generativeAiEvaluationOrigin :=
		r.renderGenerativeAiEvaluationUrl(preconditionChecksResult, apiConfig.Endpoint, apiConfig.Dataset)

	dataset := apiConfig.Dataset

	var apiClientCall *ApiClientCall
	switch action {
	case upsertAction:
		generativeAiEvaluationDefinition, err := mapToGenerativeAiEvaluationDefinition(preconditionChecksResult.resource)
		if err != nil {
			logger.Error(err, "error converting generative AI evaluation")
			return NewResourceToRequestsResultSingleItemConversionError(apiConfig, itemName, err.Error())
		}
		apiClientCall = &ApiClientCall{
			Method: http.MethodPut,
			Url:    generativeAiEvaluationUrl,
			Execute: func(ctx context.Context) (string, error) {
				apiClient, err := r.apiClientPool.Get(apiConfig.Endpoint, apiConfig.Token)
				if err != nil {
					return "", err
				}
				updatedGenerativeAiEvaluation, err := apiClient.UpdateGenerativeAiEvaluation(
					ctx, generativeAiEvaluationOrigin, generativeAiEvaluationDefinition, &dataset)
				if err != nil {
					return "", err
				}
				if updatedGenerativeAiEvaluation == nil {
					return "", errors.New("unexpected nil/empty response body")
				}
				return dash0apiclient.GetGenerativeAiEvaluationID(updatedGenerativeAiEvaluation), nil
			},
		}
	case deleteAction:
		apiClientCall = &ApiClientCall{
			Method: http.MethodDelete,
			Url:    generativeAiEvaluationUrl,
			Execute: func(ctx context.Context) (string, error) {
				apiClient, err := r.apiClientPool.Get(apiConfig.Endpoint, apiConfig.Token)
				if err != nil {
					return "", err
				}
				return "", apiClient.DeleteGenerativeAiEvaluation(ctx, generativeAiEvaluationOrigin, &dataset)
			},
		}
	default:
		unknownActionErr := fmt.Errorf("unknown API action: %d", action)
		logger.Error(unknownActionErr, "unknown API action")
		return NewResourceToRequestsResultSingleItemError(apiConfig, itemName, unknownActionErr.Error())
	}

	return NewResourceToRequestsResultSingleItemApiClientCall(
		apiConfig,
		apiClientCall,
		itemName,
		generativeAiEvaluationOrigin,
	)
}

// mapToGenerativeAiEvaluationDefinition converts the generative AI evaluation resource to the typed generative AI
// evaluation definition of the Dash0 API client. Fields that the Dash0 API does not define for generative AI
// evaluations (for example metadata.namespace, status, Kubernetes labels and annotations other than the dash0.com/
// ones of the API) are dropped.
func mapToGenerativeAiEvaluationDefinition(
	resource map[string]any,
) (*dash0apiclient.GenerativeAiEvaluationDefinition, error) {
	serializedResource, err := json.Marshal(resource)
	if err != nil {
		return nil, err
	}
	generativeAiEvaluationDefinition := &dash0apiclient.GenerativeAiEvaluationDefinition{}
	if err = json.Unmarshal(serializedResource, generativeAiEvaluationDefinition); err != nil {
		return nil, fmt.Errorf("unable to convert the generative AI evaluation to the Dash0 API format: %w", err)
	}
	return generativeAiEvaluationDefinition, nil
}

func (r *GenerativeAiEvaluationReconciler) renderGenerativeAiEvaluationUrl(
	preconditionChecksResult *preconditionValidationResult,
	endpoint string,
	dataset string,
) (string, string) {
	generativeAiEvaluationOrigin := fmt.Sprintf(
		// we deliberately use _ as the separator, since that is an illegal character in Kubernetes names. This avoids
		// any potential naming collisions (e.g. namespace="abc" & name="def-ghi" vs. namespace="abc-def" & name="ghi").
		"dash0-operator_%s_%s_%s_%s",
		r.pseudoClusterUid,
		datasetInOrigin(dataset),
		preconditionChecksResult.k8sNamespace,
		preconditionChecksResult.k8sName,
	)
	return fmt.Sprintf(
		"%sapi/generative-ai-evaluations/%s?dataset=%s",
		endpoint,
		url.PathEscape(generativeAiEvaluationOrigin),
		url.QueryEscape(dataset),
	), generativeAiEvaluationOrigin
}

func (r *GenerativeAiEvaluationReconciler) WriteSynchronizationResultToSynchronizedResource(
	ctx context.Context,
	synchronizedResource client.Object,
	syncResults synchronizationResults,
	logger logd.Logger,
) {
	generativeAiEvaluation := synchronizedResource.(*dash0v1alpha1.Dash0GenerativeAiEvaluation)

	// common result
	generativeAiEvaluation.Status.SynchronizationStatus = syncResults.resourceSyncStatus()
	generativeAiEvaluation.Status.SynchronizedAt = metav1.Time{Time: time.Now()}
	generativeAiEvaluation.Status.ValidationIssues = nil // we do not validate anything for generative AI evaluations

	// result(s) per apiConfig
	generativeAiEvaluationSyncResults :=
		make([]dash0v1alpha1.Dash0GenerativeAiEvaluationSynchronizationResultPerEndpointAndDataset, 0,
			len(syncResults.resultsPerApiConfig))
	for _, res := range syncResults.resultsPerApiConfig {
		synchronizationStatus := dash0common.Dash0ApiResourceSynchronizationStatusFailed
		// for generative AI evaluations there can be only one sync error per endpoint/dataset
		synchronizationError, httpStatusCode := firstSynchronizationErrorAndStatusCode(res.resourceToRequestsResult)
		if synchronizationError == "" {
			// no error: mark this endpoint/dataset as successful (this also clears errors from previous attempts)
			synchronizationStatus = dash0common.Dash0ApiResourceSynchronizationStatusSuccessful
		}
		syncResultPerEndpointAndDataset :=
			dash0v1alpha1.Dash0GenerativeAiEvaluationSynchronizationResultPerEndpointAndDataset{
				SynchronizationStatus: synchronizationStatus,
				Dash0ApiEndpoint:      res.apiConfig.Endpoint,
				Dash0Dataset:          res.apiConfig.Dataset,
				SynchronizationError:  synchronizationError,
				HttpStatusCode:        httpStatusCode,
			}
		if len(res.successfullySynchronized) > 0 {
			// for generative AI evaluations we only have at most one successful result per endpoint/dataset
			synchronized := res.successfullySynchronized[0]
			if synchronized.Labels.Id != "" {
				syncResultPerEndpointAndDataset.Dash0Id = synchronized.Labels.Id
			}
			if synchronized.Labels.Origin != "" {
				syncResultPerEndpointAndDataset.Dash0Origin = synchronized.Labels.Origin
			}
		}
		generativeAiEvaluationSyncResults = append(generativeAiEvaluationSyncResults, syncResultPerEndpointAndDataset)
	}
	generativeAiEvaluation.Status.SynchronizationResults = generativeAiEvaluationSyncResults

	if err := r.Status().Update(ctx, generativeAiEvaluation); err != nil {
		logger.Error(err, "Failed to update Dash0 generative AI evaluation status.")
	}
}

func (r *GenerativeAiEvaluationReconciler) CreateReconcileRequestsForRetryableSyncErrors(
	ctx context.Context,
) ([]reconcile.Request, error) {
	allResources := &dash0v1alpha1.Dash0GenerativeAiEvaluationList{}
	if err := r.List(ctx, allResources); err != nil {
		return nil, err
	}
	var requests []reconcile.Request
	for i := range allResources.Items {
		resource := &allResources.Items[i]
		for _, syncResult := range resource.Status.SynchronizationResults {
			if isRetryableSynchronizationError(syncResult.SynchronizationError, syncResult.HttpStatusCode) {
				requests = append(requests, reconcile.Request{
					NamespacedName: client.ObjectKey{Namespace: resource.Namespace, Name: resource.Name},
				})
				break
			}
		}
	}
	return requests, nil
}
