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

	dash0common "github.com/dash0hq/dash0-operator/api/operator/common"
	dash0v1beta1 "github.com/dash0hq/dash0-operator/api/operator/v1beta1"
	"github.com/dash0hq/dash0-operator/internal/selfmonitoringapiaccess"
	"github.com/dash0hq/dash0-operator/internal/util"
	"github.com/dash0hq/dash0-operator/internal/util/logd"
)

type NotificationChannelReconciler struct {
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
	notificationChannelReconcileRequestMetric otelmetric.Int64Counter
)

func NewNotificationChannelReconciler(
	k8sClient client.Client,
	pseudoClusterUid types.UID,
	leaderElectionAware util.LeaderElectionAware,
	httpClient *http.Client,
) *NotificationChannelReconciler {
	return &NotificationChannelReconciler{
		Client:               k8sClient,
		pseudoClusterUid:     pseudoClusterUid,
		leaderElectionAware:  leaderElectionAware,
		httpClient:           httpClient,
		defaultApiConfigs:    *selfmonitoringapiaccess.NewSynchronizedSlice[ApiConfig](),
		namespacedApiConfigs: *selfmonitoringapiaccess.NewSynchronizedMapSlice[ApiConfig](),
		namespacedSyncMutex:  *selfmonitoringapiaccess.NewNamespaceMutex(),
	}
}

func (r *NotificationChannelReconciler) SetupWithManager(mgr manager.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&dash0v1beta1.Dash0NotificationChannel{}).
		// ignore changes in the status subresource, but react on changes to spec, label and annotations
		WithEventFilter(notificationChannelPredicate{}).
		Complete(r)
}

func (r *NotificationChannelReconciler) InitializeSelfMonitoringMetrics(
	meter otelmetric.Meter,
	metricNamePrefix string,
	logger logd.Logger,
) {
	reconcileRequestMetricName := fmt.Sprintf("%s%s", metricNamePrefix, "notificationchannel.reconcile_requests")
	var err error
	if notificationChannelReconcileRequestMetric, err = meter.Int64Counter(
		reconcileRequestMetricName,
		otelmetric.WithUnit("1"),
		otelmetric.WithDescription("Counter for notification channel reconcile requests"),
	); err != nil {
		logger.Error(err, fmt.Sprintf("Cannot initialize the metric %s.", reconcileRequestMetricName))
	}
}

func (r *NotificationChannelReconciler) KindDisplayName() string {
	return "notification channel"
}

func (r *NotificationChannelReconciler) ShortName() string {
	return "notification-channel"
}

func (r *NotificationChannelReconciler) GetDefaultApiConfigs() []ApiConfig {
	return r.defaultApiConfigs.Get()
}

func (r *NotificationChannelReconciler) GetNamespacedApiConfigs(namespace string) ([]ApiConfig, bool) {
	return r.namespacedApiConfigs.Get(namespace)
}

func (r *NotificationChannelReconciler) ControllerName() string {
	return "dash0_notification_channel_controller"
}

func (r *NotificationChannelReconciler) K8sClient() client.Client {
	return r.Client
}

func (r *NotificationChannelReconciler) HttpClient() *http.Client {
	return r.httpClient
}

func (r *NotificationChannelReconciler) SetDefaultApiConfigs(
	ctx context.Context,
	apiConfigs []ApiConfig,
	logger logd.Logger,
) {
	r.defaultApiConfigs.Set(apiConfigs)
	r.maybeDoInitialSynchronizationOfAllResources(ctx, logger)
}

func (r *NotificationChannelReconciler) RemoveDefaultApiConfigs(_ context.Context, _ logd.Logger) {
	r.defaultApiConfigs.Clear()
}

func (r *NotificationChannelReconciler) SetNamespacedApiConfigs(
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

func (r *NotificationChannelReconciler) RemoveNamespacedApiConfigs(
	ctx context.Context,
	namespace string,
	logger logd.Logger,
) {
	if _, exists := r.namespacedApiConfigs.Get(namespace); exists {
		r.namespacedApiConfigs.Delete(namespace)
		r.synchronizeNamespacedResources(ctx, namespace, logger)
	}
}

func (r *NotificationChannelReconciler) SetSynchronizationEnabled(
	_ context.Context,
	_ string,
	_ *dash0v1beta1.Dash0Monitoring,
	_ logd.Logger,
) {
	// no-op: notification channels do not have a per-namespace sync toggle
}

func (r *NotificationChannelReconciler) RemoveSynchronizationEnabled(_ string) {
	// no-op: notification channels do not have a per-namespace sync toggle
}

func (r *NotificationChannelReconciler) NotifyOperatorManagerJustBecameLeader(
	ctx context.Context,
	logger logd.Logger,
) {
	r.maybeDoInitialSynchronizationOfAllResources(ctx, logger)
}

func (r *NotificationChannelReconciler) maybeDoInitialSynchronizationOfAllResources(
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
				"Waiting for the this operator manager replica to become leader before running initial " +
					"synchronization of notification channels.",
			),
		)
		return
	}
	if len(filterValidApiConfigs(r.defaultApiConfigs.Get(), logger, "default operator configuration")) == 0 {
		logger.Info(
			"Waiting for the Dash0 API config before running initial synchronization of notification channels. Either " +
				"no Dash0 API config has been provided via the operator configuration resource, or the operator " +
				"configuration resource has not been reconciled yet. If there is an operator configuration resource " +
				"with an API endpoint and a Dash0 auth token or a secret ref present in the cluster, it will be " +
				"reconciled in a few seconds and this message can be safely ignored.",
		)
		return
	}

	logger.Info("Running initial synchronization of notification channels now.")
	r.initialSyncInProgress.Store(true)

	go func() {
		defer r.initialSyncInProgress.Store(false)

		allResources := dash0v1beta1.Dash0NotificationChannelList{}
		if err := r.List(
			ctx,
			&allResources,
			&client.ListOptions{},
		); err != nil {
			logger.Error(err, "Failed to list all Dash0 notification channel resources.")
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
		logger.Info("Initial synchronization of notification channels has finished.")
		r.initialSyncHasHappend.Store(true)
	}()
}

func (r *NotificationChannelReconciler) synchronizeNamespacedResources(
	ctx context.Context,
	namespace string,
	logger logd.Logger,
) {
	// The namespacedSyncMutex is used so we don't trigger multiple syncs in parallel in a single namespace.
	// That happens for example when the export from a monitoring resource is removed, since that updates both the API
	// config and auth token at almost the same time, triggering two resyncs.
	r.namespacedSyncMutex.Lock(namespace)

	if !r.leaderElectionAware.IsLeader() {
		logger.Info(
			fmt.Sprintf(
				"Waiting for the this operator manager replica to become leader before running " +
					"synchronization of notification channels.",
			),
		)
		return
	}

	logger.Info(fmt.Sprintf("Running synchronization of notification channels in namespace %s now.", namespace))

	go func() {
		defer r.namespacedSyncMutex.Unlock(namespace)

		allResources := dash0v1beta1.Dash0NotificationChannelList{}
		if err := r.List(
			ctx,
			&allResources,
			&client.ListOptions{
				Namespace: namespace,
			},
		); err != nil {
			logger.Error(err, fmt.Sprintf("Failed to list Dash0 notification channel resources in namespace %s.", namespace))
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
		logger.Info(fmt.Sprintf("Synchronization of notification channels in namespace %s has finished.", namespace))
	}()
}

func (r *NotificationChannelReconciler) Reconcile(
	ctx context.Context,
	req reconcile.Request,
) (reconcile.Result, error) {
	if notificationChannelReconcileRequestMetric != nil {
		notificationChannelReconcileRequestMetric.Add(ctx, 1)
	}

	qualifiedName := req.NamespacedName.String() //nolint:staticcheck
	logger := logd.FromContext(ctx)
	logger.Info("processing reconcile request for a notification channel resource", "name", qualifiedName)

	action := upsertAction
	notificationChannelResource := &dash0v1beta1.Dash0NotificationChannel{}
	if err := r.Get(ctx, req.NamespacedName, notificationChannelResource); err != nil {
		if apierrors.IsNotFound(err) {
			action = deleteAction
			logger.Info("reconciling the deletion of the notification channel resource", "name", qualifiedName)
			notificationChannelResource = &dash0v1beta1.Dash0NotificationChannel{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: req.Namespace,
					Name:      req.Name,
				},
			}
		} else {
			logger.Error(
				err,
				fmt.Sprintf(
					"Failed to get the notification channel \"%s\", requeuing reconcile request.",
					qualifiedName,
				),
			)
			return ctrl.Result{}, err
		}
	}

	unstructuredResource, err := structToMap(notificationChannelResource)
	if err != nil {
		msg := "cannot serialize the notification channel resource"
		logger.Error(err, msg)
		if action != deleteAction {
			r.WriteSynchronizationResultToSynchronizedResource(
				ctx,
				notificationChannelResource,
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
		notificationChannelResource,
		action,
		logger,
	)

	return reconcile.Result{}, nil
}

func (r *NotificationChannelReconciler) MapResourceToHttpRequests(
	preconditionChecksResult *preconditionValidationResult,
	apiConfig ApiConfig,
	action apiAction,
	logger logd.Logger,
) *ResourceToRequestsResult {
	itemName := preconditionChecksResult.k8sName

	notificationChannelUrl, notificationChannelOrigin := r.renderNotificationChannelUrl(
		preconditionChecksResult,
		apiConfig.Endpoint,
	)

	var req *http.Request
	var method string
	var err error

	switch action {
	case upsertAction:
		// Transform the CRD resource into the API payload format:
		// - move spec.display.name → metadata.name (API expects display name there)
		// - assemble the type-specific config (e.g. spec.slackConfig) into spec.config
		resource := preconditionChecksResult.resource
		prepareNotificationChannelApiPayload(resource)
		serializedResource, _ := json.Marshal(resource)
		requestPayload := bytes.NewBuffer(serializedResource)
		method = http.MethodPut
		req, err = http.NewRequest(
			method,
			notificationChannelUrl,
			requestPayload,
		)
	case deleteAction:
		method = http.MethodDelete
		req, err = http.NewRequest(
			method,
			notificationChannelUrl,
			nil,
		)
	default:
		unknownActionErr := fmt.Errorf("unknown API action: %d", action)
		logger.Error(unknownActionErr, "unknown API action")
		return NewResourceToRequestsResultSingleItemError(apiConfig, itemName, unknownActionErr.Error())
	}

	if err != nil {
		httpError := fmt.Errorf(
			"unable to create a new HTTP request to synchronize the notification channel: %s %s: %w",
			method,
			notificationChannelUrl,
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
		notificationChannelOrigin,
	)
}

// prepareNotificationChannelApiPayload transforms the Kubernetes CRD resource map into the format expected by the
// Dash0 notification channel API:
//  1. Moves spec.display.name → metadata.name (the API expects the display name there, but Kubernetes metadata.name
//     is restricted to DNS-compatible strings).
//  2. Assembles the type-specific config field (e.g. spec.slackConfig) into spec.config and removes the CRD-specific
//     fields that the API does not understand.
func prepareNotificationChannelApiPayload(resource map[string]any) {
	specRaw, ok := resource["spec"]
	if !ok {
		return
	}
	spec, ok := specRaw.(map[string]any)
	if !ok {
		return
	}

	// 1. Move display name into metadata.name
	if displayRaw, ok := spec["display"]; ok {
		if display, ok := displayRaw.(map[string]any); ok {
			if displayName, ok := display["name"].(string); ok && displayName != "" {
				if metadataRaw, ok := resource["metadata"]; ok {
					if metadata, ok := metadataRaw.(map[string]any); ok {
						metadata["name"] = displayName
					}
				}
			}
		}
		delete(spec, "display")
	}

	// 2. Find the type-specific config and set it as spec.config.
	configFieldNames := []string{
		"slackConfig",
		"slackBotConfig",
		"emailV2Config",
		"webhookConfig",
		"incidentioConfig",
		"opsgenieConfig",
		"pagerdutyConfig",
		"teamsWebhookConfig",
		"discordWebhookConfig",
		"googleChatWebhookConfig",
		"ilertConfig",
		"allQuietConfig",
	}
	for _, fieldName := range configFieldNames {
		if configValue, exists := spec[fieldName]; exists && configValue != nil {
			spec["config"] = configValue
			delete(spec, fieldName)
			break
		}
	}
}

// renderNotificationChannelUrl builds the URL and origin for notification channel API requests.
// Notification channels are organization-level, so the origin omits the dataset segment and the URL has no dataset
// query parameter.
func (r *NotificationChannelReconciler) renderNotificationChannelUrl(
	preconditionChecksResult *preconditionValidationResult,
	endpoint string,
) (string, string) {
	notificationChannelOrigin := fmt.Sprintf(
		// we deliberately use _ as the separator, since that is an illegal character in Kubernetes names. This avoids
		// any potential naming collisions (e.g. namespace="abc" & name="def-ghi" vs. namespace="abc-def" & name="ghi").
		"dash0-operator_%s_%s_%s",
		r.pseudoClusterUid,
		preconditionChecksResult.k8sNamespace,
		preconditionChecksResult.k8sName,
	)
	return fmt.Sprintf(
		"%sapi/notification-channels/%s",
		endpoint,
		notificationChannelOrigin,
	), notificationChannelOrigin
}

func (r *NotificationChannelReconciler) ExtractIdFromResponseBody(
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

func (r *NotificationChannelReconciler) WriteSynchronizationResultToSynchronizedResource(
	ctx context.Context,
	synchronizedResource client.Object,
	syncResults synchronizationResults,
	logger logd.Logger,
) {
	notificationChannel := synchronizedResource.(*dash0v1beta1.Dash0NotificationChannel)

	// common result
	notificationChannel.Status.SynchronizationStatus = syncResults.resourceSyncStatus()
	notificationChannel.Status.SynchronizedAt = metav1.Time{Time: time.Now()}
	notificationChannel.Status.ValidationIssues = nil // we do not validate anything for notification channels

	// result(s) per apiConfig
	ncSyncResults := make(
		[]dash0v1beta1.Dash0NotificationChannelSynchronizationResultPerEndpointAndDataset,
		0,
		len(syncResults.resultsPerApiConfig),
	)
	for _, res := range syncResults.resultsPerApiConfig {
		synchronizationStatus := dash0common.Dash0ApiResourceSynchronizationStatusFailed
		synchronizationError := ""
		if len(res.resourceToRequestsResult.SynchronizationErrors) > 0 {
			// for notification channels there can be only one sync error per endpoint
			synchronizationError = slices.Collect(maps.Values(res.resourceToRequestsResult.SynchronizationErrors))[0]
		} else {
			// clear out errors from previous synchronization attempts
			synchronizationError = ""
			synchronizationStatus = dash0common.Dash0ApiResourceSynchronizationStatusSuccessful
		}
		syncResultPerEndpoint := dash0v1beta1.Dash0NotificationChannelSynchronizationResultPerEndpointAndDataset{
			SynchronizationStatus: synchronizationStatus,
			Dash0ApiEndpoint:      res.apiConfig.Endpoint,
			SynchronizationError:  synchronizationError,
		}
		if len(res.successfullySynchronized) > 0 {
			// for notification channels we only have at most one successful result per endpoint
			synchronized := res.successfullySynchronized[0]
			if synchronized.Labels.Id != "" {
				syncResultPerEndpoint.Dash0Id = synchronized.Labels.Id
			}
			if synchronized.Labels.Origin != "" {
				syncResultPerEndpoint.Dash0Origin = synchronized.Labels.Origin
			}
		}
		ncSyncResults = append(ncSyncResults, syncResultPerEndpoint)
	}
	notificationChannel.Status.SynchronizationResults = ncSyncResults

	if err := r.Status().Update(ctx, notificationChannel); err != nil {
		logger.Error(err, "Failed to update Dash0 notification channel status.")
	}
}

// An event filter that ignores changes in the status subresource but reacts on changes to spec, label and annotations.
// Ideally we would just use predicate.GenerationChangedPredicate, but it unfortunately also ignores label and
// annotation changes. This is necessary because we update the status subresource when reconciling the resource, and
// without the filter this would cause another no-op reconcile request.
type notificationChannelPredicate struct {
	predicate.Funcs
}

func (p notificationChannelPredicate) Update(e event.UpdateEvent) bool {
	if e.ObjectOld == nil || e.ObjectNew == nil {
		return true
	}

	oldObj, okOld := e.ObjectOld.(*dash0v1beta1.Dash0NotificationChannel)
	newObj, okNew := e.ObjectNew.(*dash0v1beta1.Dash0NotificationChannel)

	if !okOld || !okNew {
		return true
	}

	specChanged := !reflect.DeepEqual(oldObj.Spec, newObj.Spec)
	labelsChanged := !reflect.DeepEqual(oldObj.Labels, newObj.Labels)
	annotationsChanged := !reflect.DeepEqual(oldObj.Annotations, newObj.Annotations)

	return specChanged || labelsChanged || annotationsChanged
}
