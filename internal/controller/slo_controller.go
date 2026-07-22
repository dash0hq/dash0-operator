// SPDX-FileCopyrightText: Copyright 2026 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package controller

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"reflect"
	"slices"
	"sync"
	"sync/atomic"
	"time"

	dash0 "github.com/dash0hq/dash0-api-client-go"
	otelmetric "go.opentelemetry.io/otel/metric"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	openslov1 "github.com/dash0hq/dash0-operator/api/openslo/v1"
	dash0common "github.com/dash0hq/dash0-operator/api/operator/common"
	dash0v1beta1 "github.com/dash0hq/dash0-operator/api/operator/v1beta1"
	"github.com/dash0hq/dash0-operator/internal/selfmonitoringapiaccess"
	"github.com/dash0hq/dash0-operator/internal/util"
	"github.com/dash0hq/dash0-operator/internal/util/logd"
)

const (
	// sloCrdQualifiedName is the name of the CustomResourceDefinition that backs the SLO kind. The Kubernetes CRD
	// group is domain-qualified ("openslo.com") because Kubernetes rejects CRD groups without a dot (see
	// api/openslo/v1/groupversion_info.go); the Dash0 API body still uses the bare "openslo/v1" document version.
	sloCrdQualifiedName = "slos.openslo.com"

	// sloApiVersion and sloKind are the OpenSLO document envelope values for the Dash0 API body. They are the bare
	// upstream OpenSLO v1 values (matching the Dash0 SLO API), independent of the Kubernetes CRD group. The controller
	// only ensures they are present on the outbound body (client-go strips TypeMeta from typed objects on read).
	sloApiVersion = "openslo/v1"
	sloKind       = "SLO"
)

type SLOReconciler struct {
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

	// conflictingCrdDetected is set to true when an incompatible slos.openslo CustomResourceDefinition (one that is
	// not managed by the Dash0 operator, e.g. installed by a future upstream OpenSLO operator) is present in the
	// cluster. When set, the controller degrades gracefully: it does not establish a watch and does not synchronize
	// any SLO resources, avoiding clobbering a CRD that Dash0 does not own.
	conflictingCrdDetected atomic.Bool
}

var (
	sloReconcileRequestMetric otelmetric.Int64Counter
)

func NewSLOReconciler(
	k8sClient client.Client,
	pseudoClusterUid types.UID,
	leaderElectionAware util.LeaderElectionAware,
	httpClient *http.Client,
) *SLOReconciler {
	return &SLOReconciler{
		Client:               k8sClient,
		pseudoClusterUid:     pseudoClusterUid,
		leaderElectionAware:  leaderElectionAware,
		httpClient:           httpClient,
		defaultApiConfigs:    *selfmonitoringapiaccess.NewSynchronizedSlice[ApiConfig](),
		namespacedApiConfigs: *selfmonitoringapiaccess.NewSynchronizedMapSlice[ApiConfig](),
		namespacedSyncMutex:  *selfmonitoringapiaccess.NewNamespaceMutex(),
	}
}

func (r *SLOReconciler) SetupWithManager(ctx context.Context, mgr manager.Manager, logger logd.Logger) error {
	// Conflict detection (see the conflictingCrdDetected field): because "openslo" is a group Dash0 does not own,
	// another actor (notably a future upstream OpenSLO operator) could install a slos.openslo CRD with a different
	// schema. Before establishing a watch, we check whether the slos.openslo CRD that is present in the cluster is the
	// one managed by the Dash0 operator. If an incompatible declaration is present, we degrade gracefully instead of
	// fighting over the CRD.
	//
	// TODO(phase3): confirm conflict policy with Michele. Open questions that are resolved provisionally here:
	//   - Do we ship the CRD in the Helm chart unconditionally (current default), or only watch it when present?
	//   - What is the exact conflict action? Current default: log a clear warning, skip the watch, do not synchronize,
	//     and do not delete/clobber the foreign CRD or its resources. Surfacing the conflict as an operator-level
	//     status/condition (rather than only via logs) is left for the finalized policy.
	if conflict := r.detectConflictingCrd(ctx, logger); conflict {
		r.conflictingCrdDetected.Store(true)
		logger.Error(
			fmt.Errorf("incompatible %s custom resource definition detected", sloCrdQualifiedName),
			fmt.Sprintf(
				"An %s custom resource definition that is not managed by the Dash0 operator is already present in "+
					"this cluster. The Dash0 operator will not watch or synchronize openslo/v1 SLO resources to avoid "+
					"clobbering a CRD it does not own. Remove the conflicting CRD (or the Dash0 operator's SLO support) "+
					"to resolve this conflict.",
				sloCrdQualifiedName,
			),
		)
		return nil
	}

	return ctrl.NewControllerManagedBy(mgr).
		For(&openslov1.SLO{}).
		// ignore changes in the status subresource, but react on changes to spec, label and annotations
		WithEventFilter(sloPredicate{}).
		Complete(r)
}

// detectConflictingCrd reports whether an slos.openslo CustomResourceDefinition that is NOT managed by the Dash0
// operator is present in the cluster. It returns false when no such CRD exists, or when the existing CRD is the one
// shipped by the Dash0 operator (identified via the app.kubernetes.io/managed-by label). It uses the same CRD-presence
// lookup mechanism as third_party_crd_common.go.
func (r *SLOReconciler) detectConflictingCrd(ctx context.Context, logger logd.Logger) bool {
	crd := &apiextensionsv1.CustomResourceDefinition{}
	if err := r.Get(ctx, client.ObjectKey{Name: sloCrdQualifiedName}, crd); err != nil {
		if apierrors.IsNotFound(err) {
			// The CRD is not present (yet). The operator's Helm chart ships the CRD, so this is not a conflict.
			return false
		}
		// On a transient lookup error we conservatively assume no conflict and let the watch proceed; a genuine
		// conflict would be surfaced on the next reconcile of the CRD.
		logger.Error(err, fmt.Sprintf("unable to look up the %s custom resource definition for conflict detection", sloCrdQualifiedName))
		return false
	}
	if crdIsManagedByDash0Operator(crd) {
		return false
	}
	return true
}

// sloCrdManagedByOperatorLabelKey / sloCrdManagedByOperatorLabelValue is the marker label that the Dash0 operator's own
// slos.openslo CustomResourceDefinition carries (declared via a +kubebuilder:metadata:labels marker on the SLO type, so
// it is present on both the generated CRD base and the shipped Helm chart CRD). Its presence identifies a CRD that the
// Dash0 operator manages; its absence on an existing slos.openslo CRD indicates a foreign (incompatible) declaration.
const (
	sloCrdManagedByOperatorLabelKey   = "dash0.com/managed-by-operator"
	sloCrdManagedByOperatorLabelValue = "true"
)

// crdIsManagedByDash0Operator reports whether the given CustomResourceDefinition was installed by the Dash0 operator,
// as indicated by the dash0.com/managed-by-operator marker label.
func crdIsManagedByDash0Operator(crd *apiextensionsv1.CustomResourceDefinition) bool {
	if crd == nil || crd.Labels == nil {
		return false
	}
	return crd.Labels[sloCrdManagedByOperatorLabelKey] == sloCrdManagedByOperatorLabelValue
}

func (r *SLOReconciler) InitializeSelfMonitoringMetrics(
	meter otelmetric.Meter,
	metricNamePrefix string,
	logger logd.Logger,
) {
	reconcileRequestMetricName := fmt.Sprintf("%s%s", metricNamePrefix, "slo.reconcile_requests")
	var err error
	if sloReconcileRequestMetric, err = meter.Int64Counter(
		reconcileRequestMetricName,
		otelmetric.WithUnit("1"),
		otelmetric.WithDescription("Counter for SLO reconcile requests"),
	); err != nil {
		logger.Error(err, fmt.Sprintf("Cannot initialize the metric %s.", reconcileRequestMetricName))
	}
}

func (r *SLOReconciler) KindDisplayName() string {
	return "SLO"
}

func (r *SLOReconciler) ShortName() string {
	return "SLO"
}

func (r *SLOReconciler) GetDefaultApiConfigs() []ApiConfig {
	return r.defaultApiConfigs.Get()
}

func (r *SLOReconciler) GetNamespacedApiConfigs(namespace string) ([]ApiConfig, bool) {
	return r.namespacedApiConfigs.Get(namespace)
}

func (r *SLOReconciler) ControllerName() string {
	return "dash0_slo_controller"
}

func (r *SLOReconciler) K8sClient() client.Client {
	return r.Client
}

func (r *SLOReconciler) HttpClient() *http.Client {
	return r.httpClient
}

func (r *SLOReconciler) SetDefaultApiConfigs(
	ctx context.Context,
	apiConfigs []ApiConfig,
	logger logd.Logger,
) {
	r.defaultApiConfigs.Set(apiConfigs)
	r.maybeDoInitialSynchronizationOfAllResources(ctx, logger)
}

func (r *SLOReconciler) RemoveDefaultApiConfigs(_ context.Context, _ logd.Logger) {
	r.defaultApiConfigs.Clear()
}

func (r *SLOReconciler) SetNamespacedApiConfigs(
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

func (r *SLOReconciler) RemoveNamespacedApiConfigs(
	ctx context.Context,
	namespace string,
	logger logd.Logger,
) {
	if _, exists := r.namespacedApiConfigs.Get(namespace); exists {
		r.namespacedApiConfigs.Delete(namespace)
		r.synchronizeNamespacedResources(ctx, namespace, logger)
	}
}

func (r *SLOReconciler) SetSynchronizationEnabled(
	_ context.Context,
	_ string,
	_ *dash0v1beta1.Dash0Monitoring,
	_ logd.Logger,
) {
	// no-op: SLOs do not have a per-namespace sync toggle
}

func (r *SLOReconciler) RemoveSynchronizationEnabled(_ string) {
	// no-op: SLOs do not have a per-namespace sync toggle
}

func (r *SLOReconciler) NotifyOperatorManagerJustBecameLeader(ctx context.Context, logger logd.Logger) {
	r.maybeDoInitialSynchronizationOfAllResources(ctx, logger)
}

func (r *SLOReconciler) maybeDoInitialSynchronizationOfAllResources(
	ctx context.Context,
	logger logd.Logger,
) {
	if r.conflictingCrdDetected.Load() {
		return
	}

	r.initialSyncMutex.Lock()
	defer r.initialSyncMutex.Unlock()

	if r.initialSyncHasHappend.Load() || r.initialSyncInProgress.Load() {
		return
	}

	if !r.leaderElectionAware.IsLeader() {
		logger.Info(
			"Waiting for this operator manager replica to become leader before running initial " +
				"synchronization of SLOs.",
		)
		return
	}
	if len(filterValidApiConfigs(r.defaultApiConfigs.Get(), logger, "default operator configuration")) == 0 {
		logger.Info(
			"Waiting for the Dash0 API config before running initial synchronization of SLOs. Either " +
				"no Dash0 API config has been provided via the operator configuration resource, or the operator " +
				"configuration resource has not been reconciled yet. If there is an operator configuration resource " +
				"with an API endpoint and a Dash0 auth token or a secret ref present in the cluster, it will be " +
				"reconciled in a few seconds and this message can be safely ignored.",
		)
		return
	}

	logger.Info("Running initial synchronization of SLOs now.")
	r.initialSyncInProgress.Store(true)

	go func() {
		defer r.initialSyncInProgress.Store(false)

		allSLOResourcesInCluster := openslov1.SLOList{}
		if err := r.List(
			ctx,
			&allSLOResourcesInCluster,
			&client.ListOptions{},
		); err != nil {
			logger.Error(err, "Failed to list all SLO resources.")
			return
		}

		for _, sloResource := range allSLOResourcesInCluster.Items {
			pseudoReconcileRequest := ctrl.Request{
				NamespacedName: client.ObjectKey{
					Namespace: sloResource.Namespace,
					Name:      sloResource.Name,
				},
			}
			_, _ = r.Reconcile(ctx, pseudoReconcileRequest)
			// stagger API requests a bit
			time.Sleep(50 * time.Millisecond)
		}
		logger.Info("Initial synchronization of SLOs has finished.")
		r.initialSyncHasHappend.Store(true)
	}()
}

func (r *SLOReconciler) synchronizeNamespacedResources(
	ctx context.Context,
	namespace string,
	logger logd.Logger,
) {
	if r.conflictingCrdDetected.Load() {
		return
	}

	if !r.leaderElectionAware.IsLeader() {
		logger.Info(
			"Waiting for this operator manager replica to become leader before running " +
				"synchronization of SLOs.",
		)
		return
	}

	// The namespacedSyncMutex is used so we don't trigger multiple syncs in parallel in a single namespace.
	r.namespacedSyncMutex.Lock(namespace)

	logger.Info(fmt.Sprintf("Running synchronization of SLOs in namespace %s now.", namespace))

	go func() {
		defer r.namespacedSyncMutex.Unlock(namespace)

		allSLOResourcesInNamespace := openslov1.SLOList{}
		if err := r.List(
			ctx,
			&allSLOResourcesInNamespace,
			&client.ListOptions{
				Namespace: namespace,
			},
		); err != nil {
			logger.Error(err, fmt.Sprintf("Failed to list SLO resources in namespace %s.", namespace))
			return
		}

		for _, sloResource := range allSLOResourcesInNamespace.Items {
			pseudoReconcileRequest := ctrl.Request{
				NamespacedName: client.ObjectKey{
					Namespace: sloResource.Namespace,
					Name:      sloResource.Name,
				},
			}
			_, _ = r.Reconcile(ctx, pseudoReconcileRequest)
			// stagger API requests a bit
			time.Sleep(50 * time.Millisecond)
		}
		logger.Info(fmt.Sprintf("Synchronization of SLOs in namespace %s has finished.", namespace))
	}()
}

func (r *SLOReconciler) Reconcile(ctx context.Context, req reconcile.Request) (reconcile.Result, error) {
	if sloReconcileRequestMetric != nil {
		sloReconcileRequestMetric.Add(ctx, 1)
	}

	if r.conflictingCrdDetected.Load() {
		// A conflicting slos.openslo CRD is present; the operator does not synchronize SLOs in this state.
		return reconcile.Result{}, nil
	}

	qualifiedName := req.NamespacedName.String() //nolint:staticcheck
	logger := logd.FromContext(ctx)
	logger.Info("processing reconcile request for an SLO resource", "name", qualifiedName)

	action := upsertAction
	sloResource := &openslov1.SLO{}
	if err := r.Get(ctx, req.NamespacedName, sloResource); err != nil {
		if apierrors.IsNotFound(err) {
			action = deleteAction
			logger.Info("reconciling the deletion of the SLO resource", "name", qualifiedName)
			sloResource = &openslov1.SLO{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: req.Namespace,
					Name:      req.Name,
				},
			}
		} else {
			logger.Error(
				err,
				fmt.Sprintf(
					"Failed to get the SLO \"%s\", requeuing reconcile request.",
					qualifiedName,
				),
			)
			return ctrl.Result{}, err
		}
	}

	unstructuredResource, err := structToMap(sloResource)
	if err != nil {
		msg := "cannot serialize the SLO resource"
		logger.Error(err, msg)
		if action != deleteAction {
			r.WriteSynchronizationResultToSynchronizedResource(
				ctx,
				sloResource,
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
		sloResource,
		action,
		logger,
	)

	return reconcile.Result{}, nil
}

func (r *SLOReconciler) MapResourceToHttpRequests(
	preconditionChecksResult *preconditionValidationResult,
	apiConfig ApiConfig,
	action apiAction,
	logger logd.Logger,
) *ResourceToRequestsResult {
	itemName := preconditionChecksResult.k8sName
	sloUrl, sloOrigin := r.renderSLOUrl(
		preconditionChecksResult,
		apiConfig.Endpoint,
		apiConfig.Dataset,
	)

	var req *http.Request
	var method string
	var err error

	switch action {
	case upsertAction:
		serializedSLO, buildErr := buildSLOApiBody(preconditionChecksResult.resource)
		if buildErr != nil {
			buildError := fmt.Errorf("unable to build the SLO API body: %w", buildErr)
			logger.Error(buildError, "error building SLO API body")
			return NewResourceToRequestsResultSingleItemError(apiConfig, itemName, buildError.Error())
		}
		requestPayload := bytes.NewBuffer(serializedSLO)
		method = http.MethodPut
		req, err = http.NewRequest(
			method,
			sloUrl,
			requestPayload,
		)
	case deleteAction:
		method = http.MethodDelete
		req, err = http.NewRequest(
			method,
			sloUrl,
			nil,
		)
	default:
		unknownActionErr := fmt.Errorf("unknown API action: %d", action)
		logger.Error(unknownActionErr, "unknown API action")
		return NewResourceToRequestsResultSingleItemError(apiConfig, itemName, unknownActionErr.Error())
	}

	if err != nil {
		httpError := fmt.Errorf(
			"unable to create a new HTTP request to synchronize the SLO: %s %s: %w",
			method,
			sloUrl,
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
		sloOrigin,
	)
}

// buildSLOApiBody builds the OpenSLO document to send to the Dash0 API from the (preprocessed) Kubernetes resource
// map. It uses the api client's dash0.SloDefinition wire type: the CR's spec and metadata mirror the API's SloSpec /
// SloMetadata JSON shape field-for-field, so a JSON round-trip carries them across without any field-level conversion.
// The apiVersion and kind envelope is set explicitly because client-go strips TypeMeta from typed objects on read
// (both the CR and the API body use the identical openslo/v1 / SLO values, so there is no version rewrite). Any
// server-managed labels/annotations and the ID are stripped defensively.
func buildSLOApiBody(resource map[string]any) ([]byte, error) {
	resourceBytes, err := json.Marshal(resource)
	if err != nil {
		return nil, err
	}
	sloDefinition := &dash0.SloDefinition{}
	if err := json.Unmarshal(resourceBytes, sloDefinition); err != nil {
		return nil, err
	}
	sloDefinition.ApiVersion = dash0.SloDefinitionApiVersion(sloApiVersion)
	sloDefinition.Kind = dash0.SloDefinitionKind(sloKind)
	dash0.StripSLOServerFields(sloDefinition)
	dash0.ClearSLOID(sloDefinition)
	return json.Marshal(sloDefinition)
}

func (r *SLOReconciler) renderSLOUrl(
	preconditionChecksResult *preconditionValidationResult,
	endpoint string,
	dataset string,
) (string, string) {
	datasetUrlEncoded := url.QueryEscape(dataset)
	sloOrigin := fmt.Sprintf(
		// we deliberately use _ as the separator, since that is an illegal character in Kubernetes names. This avoids
		// any potential naming collisions (e.g. namespace="abc" & name="def-ghi" vs. namespace="abc-def" & name="ghi").
		"dash0-operator_%s_%s_%s_%s",
		r.pseudoClusterUid,
		datasetUrlEncoded,
		preconditionChecksResult.k8sNamespace,
		preconditionChecksResult.k8sName,
	)
	return fmt.Sprintf(
		"%sapi/slos/%s?dataset=%s",
		endpoint,
		sloOrigin,
		datasetUrlEncoded,
	), sloOrigin
}

func (r *SLOReconciler) ExtractIdFromResponseBody(
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

func (r *SLOReconciler) WriteSynchronizationResultToSynchronizedResource(
	ctx context.Context,
	synchronizedResource client.Object,
	syncResults synchronizationResults,
	logger logd.Logger,
) {
	slo := synchronizedResource.(*openslov1.SLO)

	// common result
	slo.Status.SynchronizationStatus = syncResults.resourceSyncStatus()
	slo.Status.SynchronizedAt = metav1.Time{Time: time.Now()}
	slo.Status.ValidationIssues = nil // we do not validate anything for SLOs beyond the CRD schema

	// result(s) per apiConfig
	sloSyncResults := make([]openslov1.SLOSynchronizationResultPerEndpointAndDataset, 0,
		len(syncResults.resultsPerApiConfig))
	for _, res := range syncResults.resultsPerApiConfig {
		synchronizationStatus := dash0common.Dash0ApiResourceSynchronizationStatusFailed
		// for SLOs there can be only one sync error per endpoint/dataset
		synchronizationError, httpStatusCode := firstSynchronizationErrorAndStatusCode(res.resourceToRequestsResult)
		if synchronizationError == "" {
			// no error: mark this endpoint/dataset as successful (this also clears errors from previous attempts)
			synchronizationStatus = dash0common.Dash0ApiResourceSynchronizationStatusSuccessful
		}
		syncResultPerEndpointAndDataset := openslov1.SLOSynchronizationResultPerEndpointAndDataset{
			SynchronizationStatus: synchronizationStatus,
			Dash0ApiEndpoint:      res.apiConfig.Endpoint,
			Dash0Dataset:          res.apiConfig.Dataset,
			SynchronizationError:  synchronizationError,
			HttpStatusCode:        httpStatusCode,
		}
		if len(res.successfullySynchronized) > 0 {
			// for SLOs we only have at most one successful result per endpoint/dataset
			synchronized := res.successfullySynchronized[0]
			if synchronized.Labels.Id != "" {
				syncResultPerEndpointAndDataset.Dash0Id = synchronized.Labels.Id
			}
			if synchronized.Labels.Origin != "" {
				syncResultPerEndpointAndDataset.Dash0Origin = synchronized.Labels.Origin
			}
		}
		sloSyncResults = append(sloSyncResults, syncResultPerEndpointAndDataset)
	}
	slo.Status.SynchronizationResults = sloSyncResults

	if err := r.Status().Update(ctx, slo); err != nil {
		logger.Error(err, "Failed to update SLO status.")
	}
}

func (r *SLOReconciler) CreateReconcileRequestsForRetryableSyncErrors(
	ctx context.Context,
) ([]reconcile.Request, error) {
	if r.conflictingCrdDetected.Load() {
		return nil, nil
	}
	allResources := &openslov1.SLOList{}
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
type sloPredicate struct {
	predicate.Funcs
}

func (p sloPredicate) Update(e event.UpdateEvent) bool {
	if e.ObjectOld == nil || e.ObjectNew == nil {
		return true
	}

	oldObj, okOld := e.ObjectOld.(*openslov1.SLO)
	newObj, okNew := e.ObjectNew.(*openslov1.SLO)

	if !okOld || !okNew {
		return true
	}

	specChanged := !reflect.DeepEqual(oldObj.Spec, newObj.Spec)
	labelsChanged := !reflect.DeepEqual(oldObj.Labels, newObj.Labels)
	annotationsChanged := !reflect.DeepEqual(oldObj.Annotations, newObj.Annotations)

	return specChanged || labelsChanged || annotationsChanged
}
