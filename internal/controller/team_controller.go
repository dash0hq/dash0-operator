// SPDX-FileCopyrightText: Copyright 2026 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package controller

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"reflect"
	"slices"
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

	dash0dashv1alpha1 "github.com/dash0hq/dash0-operator/api/dash0/v1alpha1"
	dash0common "github.com/dash0hq/dash0-operator/api/operator/common"
	dash0v1beta1 "github.com/dash0hq/dash0-operator/api/operator/v1beta1"
	"github.com/dash0hq/dash0-operator/internal/selfmonitoringapiaccess"
	"github.com/dash0hq/dash0-operator/internal/util"
	"github.com/dash0hq/dash0-operator/internal/util/logd"
)

type TeamReconciler struct {
	client.Client
	pseudoClusterUid      types.UID
	leaderElectionAware   util.LeaderElectionAware
	httpClient            *http.Client
	defaultApiConfigs     selfmonitoringapiaccess.SynchronizedSlice[ApiConfig]
	namespacedApiConfigs  selfmonitoringapiaccess.SynchronizedMapSlice[ApiConfig]
	initialSyncMutex      sync.Mutex
	initialSyncInProgress atomic.Bool
	initialSyncHasHappend atomic.Bool
	namespacedSyncMutex   selfmonitoringapiaccess.NamespaceMutex
}

var (
	teamReconcileRequestMetric otelmetric.Int64Counter
)

func NewTeamReconciler(
	k8sClient client.Client,
	pseudoClusterUid types.UID,
	leaderElectionAware util.LeaderElectionAware,
	httpClient *http.Client,
) *TeamReconciler {
	return &TeamReconciler{
		Client:               k8sClient,
		pseudoClusterUid:     pseudoClusterUid,
		leaderElectionAware:  leaderElectionAware,
		httpClient:           httpClient,
		defaultApiConfigs:    *selfmonitoringapiaccess.NewSynchronizedSlice[ApiConfig](),
		namespacedApiConfigs: *selfmonitoringapiaccess.NewSynchronizedMapSlice[ApiConfig](),
		namespacedSyncMutex:  *selfmonitoringapiaccess.NewNamespaceMutex(),
	}
}

func (r *TeamReconciler) SetupWithManager(mgr manager.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&dash0dashv1alpha1.Dash0Team{}).
		// ignore changes in the status subresource, but react on changes to spec, label and annotations
		WithEventFilter(teamPredicate{}).
		Complete(r)
}

func (r *TeamReconciler) InitializeSelfMonitoringMetrics(
	meter otelmetric.Meter,
	metricNamePrefix string,
	logger logd.Logger,
) {
	reconcileRequestMetricName := fmt.Sprintf("%s%s", metricNamePrefix, "team.reconcile_requests")
	var err error
	if teamReconcileRequestMetric, err = meter.Int64Counter(
		reconcileRequestMetricName,
		otelmetric.WithUnit("1"),
		otelmetric.WithDescription("Counter for team reconcile requests"),
	); err != nil {
		logger.Error(err, fmt.Sprintf("Cannot initialize the metric %s.", reconcileRequestMetricName))
	}
}

func (r *TeamReconciler) KindDisplayName() string {
	return "team"
}

func (r *TeamReconciler) ShortName() string {
	return "team"
}

func (r *TeamReconciler) GetDefaultApiConfigs() []ApiConfig {
	return r.defaultApiConfigs.Get()
}

func (r *TeamReconciler) GetNamespacedApiConfigs(namespace string) ([]ApiConfig, bool) {
	return r.namespacedApiConfigs.Get(namespace)
}

func (r *TeamReconciler) ControllerName() string {
	return "dash0_team_controller"
}

func (r *TeamReconciler) K8sClient() client.Client {
	return r.Client
}

func (r *TeamReconciler) HttpClient() *http.Client {
	return r.httpClient
}

func (r *TeamReconciler) SetDefaultApiConfigs(
	ctx context.Context,
	apiConfigs []ApiConfig,
	logger logd.Logger,
) {
	r.defaultApiConfigs.Set(apiConfigs)
	r.maybeDoInitialSynchronizationOfAllResources(ctx, logger)
}

func (r *TeamReconciler) RemoveDefaultApiConfigs(_ context.Context, _ logd.Logger) {
	r.defaultApiConfigs.Clear()
}

func (r *TeamReconciler) SetNamespacedApiConfigs(
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

func (r *TeamReconciler) RemoveNamespacedApiConfigs(
	ctx context.Context,
	namespace string,
	logger logd.Logger,
) {
	if _, exists := r.namespacedApiConfigs.Get(namespace); exists {
		r.namespacedApiConfigs.Delete(namespace)
		r.synchronizeNamespacedResources(ctx, namespace, logger)
	}
}

func (r *TeamReconciler) SetSynchronizationEnabled(
	_ context.Context,
	_ string,
	_ *dash0v1beta1.Dash0Monitoring,
	_ logd.Logger,
) {
	// no-op: teams do not have a per-namespace sync toggle
}

func (r *TeamReconciler) RemoveSynchronizationEnabled(_ string) {
	// no-op: teams do not have a per-namespace sync toggle
}

func (r *TeamReconciler) NotifyOperatorManagerJustBecameLeader(
	ctx context.Context,
	logger logd.Logger,
) {
	r.maybeDoInitialSynchronizationOfAllResources(ctx, logger)
}

func (r *TeamReconciler) maybeDoInitialSynchronizationOfAllResources(
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
			"Waiting for this operator manager replica to become leader before running initial " +
				"synchronization of teams.",
		)
		return
	}
	if len(filterValidApiConfigs(r.defaultApiConfigs.Get(), logger, "default operator configuration")) == 0 {
		logger.Info(
			"Waiting for the Dash0 API config before running initial synchronization of teams. Either " +
				"no Dash0 API config has been provided via the operator configuration resource, or the operator " +
				"configuration resource has not been reconciled yet. If there is an operator configuration resource " +
				"with an API endpoint and a Dash0 auth token or a secret ref present in the cluster, it will be " +
				"reconciled in a few seconds and this message can be safely ignored.",
		)
		return
	}

	logger.Info("Running initial synchronization of teams now.")
	r.initialSyncInProgress.Store(true)

	go func() {
		defer r.initialSyncInProgress.Store(false)

		allResources := dash0dashv1alpha1.Dash0TeamList{}
		if err := r.List(
			ctx,
			&allResources,
			&client.ListOptions{},
		); err != nil {
			logger.Error(err, "Failed to list all Dash0 team resources.")
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
		logger.Info("Initial synchronization of teams has finished.")
		r.initialSyncHasHappend.Store(true)
	}()
}

func (r *TeamReconciler) synchronizeNamespacedResources(
	ctx context.Context,
	namespace string,
	logger logd.Logger,
) {
	if !r.leaderElectionAware.IsLeader() {
		logger.Info(
			"Waiting for this operator manager replica to become leader before running " +
				"synchronization of teams.",
		)
		return
	}

	// The namespacedSyncMutex is used so we don't trigger multiple syncs in parallel in a single namespace.
	// That happens for example when the export from a monitoring resource is removed, since that updates both the API
	// config and auth token at almost the same time, triggering two resyncs.
	r.namespacedSyncMutex.Lock(namespace)

	logger.Info(fmt.Sprintf("Running synchronization of teams in namespace %s now.", namespace))

	go func() {
		defer r.namespacedSyncMutex.Unlock(namespace)

		allResources := dash0dashv1alpha1.Dash0TeamList{}
		if err := r.List(
			ctx,
			&allResources,
			&client.ListOptions{
				Namespace: namespace,
			},
		); err != nil {
			logger.Error(err, fmt.Sprintf("Failed to list Dash0 team resources in namespace %s.", namespace))
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
		logger.Info(fmt.Sprintf("Synchronization of teams in namespace %s has finished.", namespace))
	}()
}

func (r *TeamReconciler) Reconcile(
	ctx context.Context,
	req reconcile.Request,
) (reconcile.Result, error) {
	if teamReconcileRequestMetric != nil {
		teamReconcileRequestMetric.Add(ctx, 1)
	}

	qualifiedName := req.NamespacedName.String() //nolint:staticcheck
	logger := logd.FromContext(ctx)
	logger.Info("processing reconcile request for a team resource", "name", qualifiedName)

	action := upsertAction
	teamResource := &dash0dashv1alpha1.Dash0Team{}
	if err := r.Get(ctx, req.NamespacedName, teamResource); err != nil {
		if apierrors.IsNotFound(err) {
			action = deleteAction
			logger.Info("reconciling the deletion of the team resource", "name", qualifiedName)
			teamResource = &dash0dashv1alpha1.Dash0Team{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: req.Namespace,
					Name:      req.Name,
				},
			}
		} else {
			logger.Error(
				err,
				fmt.Sprintf(
					"Failed to get the team \"%s\", requeuing reconcile request.",
					qualifiedName,
				),
			)
			return ctrl.Result{}, err
		}
	}

	unstructuredResource, err := structToMap(teamResource)
	if err != nil {
		msg := "cannot serialize the team resource"
		logger.Error(err, msg)
		if action != deleteAction {
			r.WriteSynchronizationResultToSynchronizedResource(
				ctx,
				teamResource,
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
		teamResource,
		action,
		logger,
	)

	return reconcile.Result{}, nil
}

func (r *TeamReconciler) MapResourceToHttpRequests(
	preconditionChecksResult *preconditionValidationResult,
	apiConfig ApiConfig,
	action apiAction,
	logger logd.Logger,
) *ResourceToRequestsResult {
	itemName := preconditionChecksResult.k8sName

	teamUrl, teamOrigin := r.renderTeamUrl(
		preconditionChecksResult,
		apiConfig.Endpoint,
	)

	var req *http.Request
	var method string
	var err error

	switch action {
	case upsertAction:
		// The API expects a TeamDefinitionV1Alpha1 envelope. The controller preserves the CR's metadata.name as the
		// technical name and passes spec.display and spec.members through unchanged. The server accepts emails in
		// spec.members and resolves them to internal IDs during reconciliation, so no client-side lookup is needed.
		resource := preconditionChecksResult.resource
		prepareTeamApiPayload(resource, preconditionChecksResult.k8sName)
		serializedResource, _ := json.Marshal(resource)
		requestPayload := bytes.NewBuffer(serializedResource)
		method = http.MethodPut
		req, err = http.NewRequest(
			method,
			teamUrl,
			requestPayload,
		)
	case deleteAction:
		method = http.MethodDelete
		req, err = http.NewRequest(
			method,
			teamUrl,
			nil,
		)
	default:
		unknownActionErr := fmt.Errorf("unknown API action: %d", action)
		logger.Error(unknownActionErr, "unknown API action")
		return NewResourceToRequestsResultSingleItemError(apiConfig, itemName, unknownActionErr.Error())
	}

	if err != nil {
		httpError := fmt.Errorf(
			"unable to create a new HTTP request to synchronize the team: %s %s: %w",
			method,
			teamUrl,
			err,
		)
		logger.Error(httpError, "error creating http request")
		return NewResourceToRequestsResultSingleItemError(apiConfig, itemName, httpError.Error())
	}

	addAuthorizationHeader(req, apiConfig.Token)
	if action == upsertAction {
		req.Header.Set(util.ContentTypeHeaderName, util.ApplicationJsonMediaType)
	}

	return NewResourceToRequestsResultSingleItemSuccess(
		apiConfig,
		req,
		itemName,
		teamOrigin,
	)
}

// prepareTeamApiPayload projects the operator's CR shape into the TeamDefinitionV1Alpha1 envelope the Dash0
// API expects. cleanUpMetadata (called earlier via the shared precondition path) has already stripped
// managedFields, dash0.com/{id,dataset,source,version} labels, and the kubectl last-applied-configuration
// annotation. This helper adds team-specific projections and delegates the remaining Kubernetes-only field
// stripping to stripKubernetesOnlyMetadataFields (safe here because the resource map for teams comes from
// structToMap, i.e. a fresh copy — not the Unstructured backing map used by third-party controllers).
func prepareTeamApiPayload(resource map[string]any, k8sName string) {
	resource["kind"] = "Dash0Team"
	resource["apiVersion"] = "dash0.com/v1alpha1"

	stripKubernetesOnlyMetadataFields(resource)

	if metadataRaw, ok := resource["metadata"]; ok {
		if metadata, ok := metadataRaw.(map[string]any); ok {
			metadata["name"] = k8sName
		}
	}
	delete(resource, "status")
}

// renderTeamUrl builds the URL and origin for team API requests. Teams are organization-scoped, so the
// origin omits the dataset segment (matching notification channels) and the URL has no dataset query
// parameter.
func (r *TeamReconciler) renderTeamUrl(
	preconditionChecksResult *preconditionValidationResult,
	endpoint string,
) (string, string) {
	teamOrigin := fmt.Sprintf(
		// we deliberately use _ as the separator, since that is an illegal character in Kubernetes names. This avoids
		// any potential naming collisions (e.g. namespace="abc" & name="def-ghi" vs. namespace="abc-def" & name="ghi").
		"dash0-operator_%s_%s_%s",
		r.pseudoClusterUid,
		preconditionChecksResult.k8sNamespace,
		preconditionChecksResult.k8sName,
	)
	return fmt.Sprintf(
		"%sapi/teams/%s",
		endpoint,
		teamOrigin,
	), teamOrigin
}

func (r *TeamReconciler) ExtractIdFromResponseBody(
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

func (r *TeamReconciler) WriteSynchronizationResultToSynchronizedResource(
	ctx context.Context,
	synchronizedResource client.Object,
	syncResults synchronizationResults,
	logger logd.Logger,
) {
	team := synchronizedResource.(*dash0dashv1alpha1.Dash0Team)

	// common result
	team.Status.SynchronizationStatus = syncResults.resourceSyncStatus()
	team.Status.SynchronizedAt = metav1.Time{Time: time.Now()}
	team.Status.ValidationIssues = nil // we do not validate anything for teams operator-side

	// result(s) per apiConfig
	teamSyncResults := make(
		[]dash0dashv1alpha1.Dash0TeamSynchronizationResultPerEndpointAndDataset,
		0,
		len(syncResults.resultsPerApiConfig),
	)
	for _, res := range syncResults.resultsPerApiConfig {
		synchronizationStatus := dash0common.Dash0ApiResourceSynchronizationStatusFailed
		// for teams there can be only one sync error per endpoint
		synchronizationError, httpStatusCode := firstSynchronizationErrorAndStatusCode(res.resourceToRequestsResult)
		if synchronizationError == "" {
			// no error: mark this endpoint as successful (this also clears errors from previous attempts)
			synchronizationStatus = dash0common.Dash0ApiResourceSynchronizationStatusSuccessful
		}
		syncResultPerEndpoint := dash0dashv1alpha1.Dash0TeamSynchronizationResultPerEndpointAndDataset{
			SynchronizationStatus: synchronizationStatus,
			Dash0ApiEndpoint:      res.apiConfig.Endpoint,
			SynchronizationError:  synchronizationError,
			HttpStatusCode:        httpStatusCode,
		}
		if len(res.successfullySynchronized) > 0 {
			// for teams we only have at most one successful result per endpoint
			synchronized := res.successfullySynchronized[0]
			if synchronized.Labels.Id != "" {
				syncResultPerEndpoint.Dash0Id = synchronized.Labels.Id
			}
			if synchronized.Labels.Origin != "" {
				syncResultPerEndpoint.Dash0Origin = synchronized.Labels.Origin
			}
		}
		teamSyncResults = append(teamSyncResults, syncResultPerEndpoint)
	}
	team.Status.SynchronizationResults = teamSyncResults

	if err := r.Status().Update(ctx, team); err != nil {
		logger.Error(err, "Failed to update Dash0 team status.")
	}
}

func (r *TeamReconciler) CreateReconcileRequestsForRetryableSyncErrors(
	ctx context.Context,
) ([]reconcile.Request, error) {
	allResources := &dash0dashv1alpha1.Dash0TeamList{}
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

// An event filter that ignores changes in the status subresource but reacts on changes to spec, label and annotations.
// Ideally we would just use predicate.GenerationChangedPredicate, but it unfortunately also ignores label and
// annotation changes. This is necessary because we update the status subresource when reconciling the resource, and
// without the filter this would cause another no-op reconcile request.
type teamPredicate struct {
	predicate.Funcs
}

func (p teamPredicate) Update(e event.UpdateEvent) bool {
	if e.ObjectOld == nil || e.ObjectNew == nil {
		return true
	}

	oldObj, okOld := e.ObjectOld.(*dash0dashv1alpha1.Dash0Team)
	newObj, okNew := e.ObjectNew.(*dash0dashv1alpha1.Dash0Team)

	if !okOld || !okNew {
		return true
	}

	specChanged := !reflect.DeepEqual(oldObj.Spec, newObj.Spec)
	labelsChanged := !reflect.DeepEqual(oldObj.Labels, newObj.Labels)
	annotationsChanged := !reflect.DeepEqual(oldObj.Annotations, newObj.Annotations)

	return specChanged || labelsChanged || annotationsChanged
}
