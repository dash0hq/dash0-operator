// SPDX-FileCopyrightText: Copyright 2024 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package controller

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/go-logr/logr"
	otelmetric "go.opentelemetry.io/otel/metric"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/kubernetes"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/log"

	dash0common "github.com/dash0hq/dash0-operator/api/operator/common"
	dash0v1beta1 "github.com/dash0hq/dash0-operator/api/operator/v1beta1"
	"github.com/dash0hq/dash0-operator/internal/collectors"
	"github.com/dash0hq/dash0-operator/internal/instrumentation"
	"github.com/dash0hq/dash0-operator/internal/resources"
	"github.com/dash0hq/dash0-operator/internal/selfmonitoringapiaccess"
	"github.com/dash0hq/dash0-operator/internal/targetallocator"
	"github.com/dash0hq/dash0-operator/internal/util"
)

type MonitoringReconciler struct {
	client.Client
	clientset                  *kubernetes.Clientset
	namespacedApiClients       []NamespacedApiClient
	namespacedAuthTokenClients []selfmonitoringapiaccess.NamespacedAuthTokenClient
	instrumenter               *instrumentation.Instrumenter
	collectorManager           *collectors.CollectorManager
	targetAllocatorManager     *targetallocator.TargetAllocatorManager
	danglingEventsTimeouts     *util.DanglingEventsTimeouts
	operatorNamespace          string
}

type statusUpdateInfo struct {
	previousInstrumentWorkloadsMode          dash0common.InstrumentWorkloadsMode
	currentInstrumentWorkloadsMode           dash0common.InstrumentWorkloadsMode
	previousInstrumentWorkloadsLabelSelector string
	currentInstrumentWorkloadsLabelSelector  string
	previousTraceContextPropagators          *string
	currentTraceContextPropagators           *string
}

const (
	updateStatusFailedMessageMonitoring = "Failed to update Dash0 monitoring status conditions, requeuing reconcile request."
)

var (
	defaultDanglingEventsTimeouts = &util.DanglingEventsTimeouts{
		InitialTimeout: 30 * time.Second,
		Backoff: wait.Backoff{
			Steps:    3,
			Duration: 30 * time.Second,
			Factor:   1.5,
			Jitter:   0.3,
		},
	}

	monitoringReconcileRequestMetric otelmetric.Int64Counter
)

func NewMonitoringReconciler(
	k8sClient client.Client,
	clientset *kubernetes.Clientset,
	namespacedApiClients []NamespacedApiClient,
	instrumenter *instrumentation.Instrumenter,
	collectorManager *collectors.CollectorManager,
	targetAllocatorManager *targetallocator.TargetAllocatorManager,
	danglingEventsTimeouts *util.DanglingEventsTimeouts,
	operatorNamespace string,
) *MonitoringReconciler {
	return &MonitoringReconciler{
		Client:                 k8sClient,
		clientset:              clientset,
		namespacedApiClients:   namespacedApiClients,
		instrumenter:           instrumenter,
		collectorManager:       collectorManager,
		targetAllocatorManager: targetAllocatorManager,
		danglingEventsTimeouts: danglingEventsTimeouts,
		operatorNamespace:      operatorNamespace,
	}
}

func (r *MonitoringReconciler) SetNamespacedAuthTokenClients(namespacedAuthTokenClients []selfmonitoringapiaccess.NamespacedAuthTokenClient) {
	r.namespacedAuthTokenClients = namespacedAuthTokenClients
}

func (r *MonitoringReconciler) SetupWithManager(mgr ctrl.Manager) error {
	if r.danglingEventsTimeouts == nil {
		r.danglingEventsTimeouts = defaultDanglingEventsTimeouts
	}
	return ctrl.NewControllerManagedBy(mgr).
		For(&dash0v1beta1.Dash0Monitoring{}).
		Complete(r)
}

func (r *MonitoringReconciler) InitializeSelfMonitoringMetrics(
	meter otelmetric.Meter,
	metricNamePrefix string,
	logger *logr.Logger,
) {
	reconcileRequestMetricName := fmt.Sprintf("%s%s", metricNamePrefix, "monitoring.reconcile_requests")
	var err error
	if monitoringReconcileRequestMetric, err = meter.Int64Counter(
		reconcileRequestMetricName,
		otelmetric.WithUnit("1"),
		otelmetric.WithDescription("Counter for monitoring resource reconcile requests"),
	); err != nil {
		logger.Error(err, fmt.Sprintf("Cannot initialize the metric %s.", reconcileRequestMetricName))
	}
}

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
// It is essential for the controller's reconciliation loop to be idempotent. By following the Operator
// pattern you will create Controllers which provide a reconcile function
// responsible for synchronizing resources until the desired state is reached on the cluster.
// Breaking this recommendation goes against the design principles of controller-runtime.
// and may lead to unforeseen consequences such as resources becoming stuck and requiring manual intervention.
// For further info:
// - About Operator Pattern: https://kubernetes.io/docs/concepts/extend-kubernetes/operator/
// - About Controllers: https://kubernetes.io/docs/concepts/architecture/controller/
// - https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.16.3/pkg/reconcile
func (r *MonitoringReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	if monitoringReconcileRequestMetric != nil {
		monitoringReconcileRequestMetric.Add(ctx, 1)
	}

	logger := log.FromContext(ctx)
	logger.Info("processing reconcile request for a monitoring resource")

	namespaceStillExists, err := resources.CheckIfNamespaceExists(ctx, r.clientset, req.Namespace, &logger)
	if err != nil {
		// The error has already been logged in checkIfNamespaceExists.
		return ctrl.Result{}, err
	} else if !namespaceStillExists {
		logger.Info("The namespace seems to have been deleted after this reconcile request has been scheduled. " +
			"Ignoring the reconcile request.")
		return ctrl.Result{}, nil
	}

	checkResourceResult, err := resources.VerifyThatUniqueNonDegradedResourceExists(
		ctx,
		r.Client,
		req,
		&dash0v1beta1.Dash0Monitoring{},
		updateStatusFailedMessageMonitoring,
		&logger,
	)
	if err != nil {
		// For all errors, we assume it is a temporary error and requeue the request.
		return ctrl.Result{}, err
	} else if checkResourceResult.ResourceDoesNotExist {
		// If the resource is not found, the checkResourceResult, we do not want to requeue the request.
		// Make sure we update the otel collector config (for example, remove this namespace from the allow list for
		// prometheus scraping) after removing the monitoring resource from this namespace. Or, to be more precise,
		// when r.reconcileOpenTelemetryCollector runs, the monitoring resource in this namespace is still present, but
		// it is no longer marked as available.
		if err = r.reconcileOpenTelemetryCollector(ctx, &logger); err != nil {
			return ctrl.Result{}, err
		}
		if err = r.reconcileOpenTelemetryTargetAllocator(ctx, &logger); err != nil {
			return ctrl.Result{}, err
		}

		return ctrl.Result{}, nil
	} else if checkResourceResult.StopReconcile {
		return ctrl.Result{}, nil
	}

	monitoringResource := checkResourceResult.Resource.(*dash0v1beta1.Dash0Monitoring)

	isFirstReconcile, err := resources.InitStatusConditions(
		ctx,
		r.Client,
		monitoringResource,
		monitoringResource.Status.Conditions,
		&logger,
	)
	if err != nil {
		// The error has already been logged in initStatusConditions
		return ctrl.Result{}, err
	}

	isMarkedForDeletion, runCleanupActions, err := resources.CheckImminentDeletionAndHandleFinalizers(
		ctx,
		r.Client,
		monitoringResource,
		dash0common.MonitoringFinalizerId,
		&logger,
	)
	if err != nil {
		// The error has already been logged in checkImminentDeletionAndHandleFinalizers
		return ctrl.Result{}, err
	} else if runCleanupActions {
		// r.runCleanupActions will uninstrument workloads and then remove the finalizer.
		err = r.runCleanupActions(ctx, monitoringResource, &logger)
		if err != nil {
			// error has already been logged in runCleanupActions
			return ctrl.Result{}, err
		}

		// The Dash0 monitoring resource was requested for deletion, we have just now ran cleanup actions (reverting
		// instrumented resources and removing the finalizer), no further reconciliation is necessary for this reconcile
		// request.
		return ctrl.Result{}, nil
	} else if isMarkedForDeletion {
		// For this particular controller, this branch should actually never run. When we remove the finalizer in the
		// runCleanupActions branch, the monitoring resource will be deleted immediately. A new reconcile cycle will be
		// requested for the deletion, which we stop early/immediately after the
		// resources.VerifyThatUniqueNonDegradedResourceExists check, due to checkResourceResult.ResourceDoesNotExist
		// being true. We still want to handle this case correctly for good measure (reconcile the otel collector, then
		// stop the reconcile of the monitoring resource).
		if r.reconcileOpenTelemetryCollector(ctx, &logger) != nil {
			return ctrl.Result{}, err
		}
		if r.reconcileOpenTelemetryTargetAllocator(ctx, &logger) != nil {
			return ctrl.Result{}, err
		}
		// The Dash0 monitoring resource is slated for deletion, the finalizer has already been removed in the last
		// reconcile cycle (which also means all cleanup actions have been processed last time), no further
		// reconciliation is necessary.
		return ctrl.Result{}, nil
	}

	err = r.handleDash0Authorization(ctx, monitoringResource, &logger)
	if err != nil {
		logger.Error(err, "error when handling the Dash0 authorization information in the monitoring resource")
		return ctrl.Result{}, err
	}

	var requiredAction util.ModificationMode
	monitoringResource, requiredAction, statusUpdate :=
		r.manageInstrumentWorkloadsChanges(monitoringResource, isFirstReconcile, &logger)

	if isFirstReconcile || requiredAction == util.ModificationModeInstrumentation {
		if err = r.instrumenter.CheckSettingsAndInstrumentExistingWorkloads(ctx, monitoringResource, &logger); err != nil {
			// The error has already been logged in checkSettingsAndInstrumentExistingWorkloads
			logger.Info("Requeuing reconcile request.")
			return ctrl.Result{}, err
		}
	} else if requiredAction == util.ModificationModeUninstrumentation {
		if err = r.instrumenter.UninstrumentWorkloadsIfAvailable(ctx, monitoringResource, &logger); err != nil {
			logger.Error(err, "Failed to uninstrument workloads, requeuing reconcile request.")
			return ctrl.Result{}, err
		}
	}

	r.scheduleAttachDanglingEvents(ctx, monitoringResource, &logger)

	if err = r.updateStatusAfterReconcile(ctx, monitoringResource, statusUpdate, &logger); err != nil {
		// The error has already been logged in updateStatusAfterReconcile
		return ctrl.Result{}, err
	}

	r.applyApiAccessSettings(ctx, monitoringResource, logger)

	if r.reconcileOpenTelemetryCollector(ctx, &logger) != nil {
		return ctrl.Result{}, err
	}

	if r.reconcileOpenTelemetryTargetAllocator(ctx, &logger) != nil {
		return ctrl.Result{}, err
	}

	return ctrl.Result{}, nil
}

func (r *MonitoringReconciler) handleDash0Authorization(
	ctx context.Context,
	monitoringResource *dash0v1beta1.Dash0Monitoring,
	logger *logr.Logger,
) error {
	if monitoringResource.Spec.Export != nil &&
		monitoringResource.Spec.Export.Dash0 != nil {
		if monitoringResource.Spec.Export.Dash0.Authorization.SecretRef != nil {
			if err := selfmonitoringapiaccess.ExchangeSecretRefForNamespacedToken(
				ctx,
				r.Client,
				r.namespacedAuthTokenClients,
				r.operatorNamespace,
				monitoringResource,
				logger,
			); err != nil {
				logger.Error(err, "cannot exchange secret ref for token")
				return err
			}
		} else if monitoringResource.Spec.Export.Dash0.Authorization.Token != nil &&
			*monitoringResource.Spec.Export.Dash0.Authorization.Token != "" {
			for _, nsAuthTokenClient := range r.namespacedAuthTokenClients {
				nsAuthTokenClient.SetNamespacedAuthToken(ctx, monitoringResource.Namespace, *monitoringResource.Spec.Export.Dash0.Authorization.Token, logger)
			}
		} else {
			// The monitoring resource neither has a secret ref nor a token literal, remove the auth token
			// for the resource's namespace from all clients.
			for _, nsAuthTokenClient := range r.namespacedAuthTokenClients {
				nsAuthTokenClient.RemoveNamespacedAuthToken(ctx, monitoringResource.Namespace, logger)
			}
		}
	} else {
		// The monitoring resource has no Dash0 export and hence also no auth token, remove the auth token
		// for the resource's namespace from all clients.
		for _, nsAuthTokenClient := range r.namespacedAuthTokenClients {
			nsAuthTokenClient.RemoveNamespacedAuthToken(ctx, monitoringResource.Namespace, logger)
		}
	}
	return nil
}

func (r *MonitoringReconciler) applyApiAccessSettings(
	ctx context.Context,
	monitoringResource *dash0v1beta1.Dash0Monitoring,
	logger logr.Logger,
) {
	if monitoringResource.HasDash0ApiAccessConfigured() {
		dataset := monitoringResource.Spec.Export.Dash0.Dataset
		if dataset == "" {
			dataset = util.DatasetDefault
		}
		for _, apiClient := range r.namespacedApiClients {
			apiClient.SetNamespacedApiEndpointAndDataset(
				ctx,
				monitoringResource.Namespace,
				&ApiConfig{
					Endpoint: monitoringResource.Spec.Export.Dash0.ApiEndpoint,
					Dataset:  dataset,
				}, &logger)

		}
	} else {
		logger.Info(fmt.Sprintf("The API endpoint setting required for managing dashboards, check rules, synthetic checks and "+
			"views via the operator is not set or has been removed from the monitoring resource in namespace %s. API sync will use "+
			"defaults from the operator configuration.", monitoringResource.Namespace))
		for _, apiClient := range r.namespacedApiClients {
			apiClient.RemoveNamespacedApiEndpointAndDataset(ctx, monitoringResource.Namespace, &logger)
		}
	}
}

func (r *MonitoringReconciler) manageInstrumentWorkloadsChanges(
	monitoringResource *dash0v1beta1.Dash0Monitoring,
	isFirstReconcile bool,
	logger *logr.Logger,
) (*dash0v1beta1.Dash0Monitoring, util.ModificationMode, statusUpdateInfo) {
	previousInstrumentWorkloadsMode := monitoringResource.Status.PreviousInstrumentWorkloads.Mode
	currentInstrumentWorkloadsMode := monitoringResource.ReadInstrumentWorkloadsMode()

	previousLabelSelector := monitoringResource.Status.PreviousInstrumentWorkloads.LabelSelector
	currentLabelSelector := monitoringResource.Spec.InstrumentWorkloads.LabelSelector

	previousTraceContextPropagators := monitoringResource.Status.PreviousInstrumentWorkloads.TraceContext.Propagators
	currentTraceContextPropagators := monitoringResource.Spec.InstrumentWorkloads.TraceContext.Propagators

	var requiredAction util.ModificationMode
	if !isFirstReconcile {
		if previousInstrumentWorkloadsMode != dash0common.InstrumentWorkloadsModeAll && previousInstrumentWorkloadsMode != "" && currentInstrumentWorkloadsMode == dash0common.InstrumentWorkloadsModeAll {
			logger.Info(fmt.Sprintf(
				"The instrumentWorkloads mode has changed from \"%s\" to \"%s\" (or it is absent, in which case it"+
					"defaults to \"all\"). Workloads in this namespace will now be instrumented so they send "+
					"telemetry to Dash0.", previousInstrumentWorkloadsMode, currentInstrumentWorkloadsMode))
			requiredAction = util.ModificationModeInstrumentation
		} else if previousInstrumentWorkloadsMode != dash0common.InstrumentWorkloadsModeNone && currentInstrumentWorkloadsMode == dash0common.InstrumentWorkloadsModeNone {
			logger.Info(fmt.Sprintf(
				"The instrumentWorkloads mode has changed from \"%s\" to \"%s\". Instrumented workloads in this "+
					"namespace will now be uninstrumented, they will no longer send telemetry to Dash0.",
				previousInstrumentWorkloadsMode,
				currentInstrumentWorkloadsMode))
			requiredAction = util.ModificationModeUninstrumentation
		}

		// If the mode switched to "none" and we need to uninstrument, changes in individual settings (like label
		// selector, trace context propagators) are irrelevant. If the mode switched to "all" and we need to instrument
		// because of that, changes in individual settings are also irrelevant. We only need to compare individual
		// settings if the required action is "" so far, for example if the instrumentation mode has not changed, or
		// has changed to created-and-updated.
		if requiredAction == "" {
			if strings.TrimSpace(previousLabelSelector) != strings.TrimSpace(currentLabelSelector) {
				requiredAction = util.ModificationModeInstrumentation
			}
			if util.IsStringPointerValueDifferent(previousTraceContextPropagators, currentTraceContextPropagators) {
				requiredAction = util.ModificationModeInstrumentation
			}
		}
	}

	return monitoringResource, requiredAction, statusUpdateInfo{
		previousInstrumentWorkloadsMode:          previousInstrumentWorkloadsMode,
		currentInstrumentWorkloadsMode:           currentInstrumentWorkloadsMode,
		previousInstrumentWorkloadsLabelSelector: previousLabelSelector,
		currentInstrumentWorkloadsLabelSelector:  currentLabelSelector,
		previousTraceContextPropagators:          previousTraceContextPropagators,
		currentTraceContextPropagators:           currentTraceContextPropagators,
	}
}

func (r *MonitoringReconciler) runCleanupActions(
	ctx context.Context,
	monitoringResource *dash0v1beta1.Dash0Monitoring,
	logger *logr.Logger,
) error {
	if err := r.instrumenter.UninstrumentWorkloadsIfAvailable(
		ctx,
		monitoringResource,
		logger,
	); err != nil {
		logger.Error(err, "Failed to uninstrument workloads, requeuing reconcile request.")
		return err
	}

	// The Dash0 monitoring resource will be deleted after this reconcile finished. We still need to update its status,
	// to make sure that when we run r.reconcileOpenTelemetryCollector in the next and final reconcile cycle triggered
	// by the actual deletion of the resource, the monitoring resource is no longer marked as available; so that this
	// namespace will be removed from the list of namespaces for prometheus scraping.
	monitoringResource.EnsureResourceIsMarkedAsAboutToBeDeleted()
	if err := r.Status().Update(ctx, monitoringResource); err != nil {
		logger.Error(err, updateStatusFailedMessageMonitoring)
		return err
	}

	if err := r.reconcileOpenTelemetryCollector(ctx, logger); err != nil {
		// err has been logged in r.reconcileOpenTelemetryCollector already
		return err
	}

	if err := r.reconcileOpenTelemetryTargetAllocator(ctx, logger); err != nil {
		// err has been logged in r.reconcileOpenTelemetryTargetAllocator already
		return err
	}

	if finalizersUpdated := controllerutil.RemoveFinalizer(monitoringResource, dash0common.MonitoringFinalizerId); finalizersUpdated {
		if err := r.Update(ctx, monitoringResource); err != nil {
			logger.Error(err, "Failed to remove the finalizer from the Dash0 monitoring resource, requeuing reconcile "+
				"request.")
			return err
		}
	}
	return nil
}

func (r *MonitoringReconciler) scheduleAttachDanglingEvents(
	ctx context.Context,
	monitoringResource *dash0v1beta1.Dash0Monitoring,
	logger *logr.Logger,
) {
	// execute the event attaching in a separate go routine to not block the main reconcile loop
	go func() {
		if r.danglingEventsTimeouts.InitialTimeout > 0 {
			// wait a bit before attempting to attach dangling events, since the K8s resources do not exist yet when
			// the webhook queues the events
			time.Sleep(r.danglingEventsTimeouts.InitialTimeout)
		}
		r.attachDanglingEvents(ctx, monitoringResource, logger)
	}()
}

// attachDanglingEvents attaches events that have been created by the webhook to the involved object of the event.
// When the webhook creates the respective events, the workload might not exist yet, so the UID of the workload is not
// set in the event. The list of events for that workload will not contain this event. We go the extra mile here and
// retroactively fix the reference of all those events.
//
// Without doing this, the event will not show up in the event section of "kubectl describe" for the workload, because
// kubectl always searches for related events with the object's uid.
//
// This code still uses the legacy event API, as the regarding.UID field is immutable in the new k8s.io/api/events/v1
// API.
func (r *MonitoringReconciler) attachDanglingEvents(
	ctx context.Context,
	monitoringResource *dash0v1beta1.Dash0Monitoring,
	logger *logr.Logger,
) {
	namespace := monitoringResource.Namespace
	legacyEventApi := r.clientset.CoreV1().Events(namespace)
	backoff := r.danglingEventsTimeouts.Backoff
	for _, eventType := range util.AllEvents {
		retryErr := util.RetryWithCustomBackoff(
			"attaching dangling event to involved object",
			func() error {
				danglingEvents, listErr := legacyEventApi.List(ctx, metav1.ListOptions{
					FieldSelector: fmt.Sprintf("involvedObject.uid=,reason=%s", eventType),
				})
				if listErr != nil {
					return listErr
				}

				if len(danglingEvents.Items) > 0 {
					logger.Info(fmt.Sprintf("Attempting to attach %d dangling event(s).", len(danglingEvents.Items)))
				}

				var allAttachErrors []error
				for _, event := range danglingEvents.Items {
					attachErr := util.AttachEventToInvolvedObject(ctx, r.Client, legacyEventApi, &event)
					if attachErr != nil {
						allAttachErrors = append(allAttachErrors, attachErr)
					} else {
						involvedObject := event.InvolvedObject
						logger.Info(fmt.Sprintf("Attached '%s' event (uid: %s) to its associated object (%s %s/%s).",
							eventType, event.UID, involvedObject.Kind, involvedObject.Namespace, involvedObject.Name))
					}
				}
				if len(allAttachErrors) > 0 {
					return errors.Join(allAttachErrors...)
				}

				if len(danglingEvents.Items) > 0 {
					logger.Info(
						fmt.Sprintf(
							"%d dangling event(s) have been successfully attached to its/their associated object(s).",
							len(danglingEvents.Items)),
					)
				}

				return nil
			},
			backoff,
			false,
			false,
			logger,
		)

		if retryErr != nil {
			logger.Error(retryErr, fmt.Sprintf(
				"Could not attach all dangling events after %d retries.", backoff.Steps))
		}
	}
}

func (r *MonitoringReconciler) reconcileOpenTelemetryCollector(
	ctx context.Context,
	logger *logr.Logger,
) error {
	// This will look up the operator configuration resource and all monitoring resources in the cluster (including
	// the one that has just been reconciled, hence we must only do this _after_ this resource has been updated (e.g.
	// marked as available). Otherwise, the reconciliation of the collectors would work with an outdated state.
	if _, err := r.collectorManager.ReconcileOpenTelemetryCollector(
		ctx,
	); err != nil {
		logger.Error(err, "Failed to reconcile the OpenTelemetry collector, requeuing reconcile request.")
		return err
	}
	return nil
}

func (r *MonitoringReconciler) reconcileOpenTelemetryTargetAllocator(
	ctx context.Context,
	logger *logr.Logger,
) error {
	// This will look up the operator configuration resource and all monitoring resources in the cluster (including
	// the one that has just been reconciled, hence we must only do this _after_ this resource has been updated (e.g.
	// marked as available). Otherwise, the reconciliation of the collectors would work with an outdated state.
	if _, err := r.targetAllocatorManager.ReconcileTargetAllocator(
		ctx,
		targetallocator.TriggeredByDash0ResourceReconcile,
	); err != nil {
		logger.Error(err, "Failed to reconcile the OpenTelemetry target-allocator, requeuing reconcile request.")
		return err
	}
	return nil
}

func (r *MonitoringReconciler) updateStatusAfterReconcile(
	ctx context.Context,
	monitoringResource *dash0v1beta1.Dash0Monitoring,
	statusUpdate statusUpdateInfo,
	logger *logr.Logger,
) error {
	monitoringResource.EnsureResourceIsMarkedAsAvailable()
	if statusUpdate.previousInstrumentWorkloadsMode != statusUpdate.currentInstrumentWorkloadsMode {
		monitoringResource.Status.PreviousInstrumentWorkloads.Mode = statusUpdate.currentInstrumentWorkloadsMode
	}
	if strings.TrimSpace(statusUpdate.previousInstrumentWorkloadsLabelSelector) != strings.TrimSpace(statusUpdate.currentInstrumentWorkloadsLabelSelector) {
		monitoringResource.Status.PreviousInstrumentWorkloads.LabelSelector = statusUpdate.currentInstrumentWorkloadsLabelSelector
	}
	if util.IsStringPointerValueDifferent(statusUpdate.previousTraceContextPropagators, statusUpdate.currentTraceContextPropagators) {
		monitoringResource.Status.PreviousInstrumentWorkloads.TraceContext.Propagators = statusUpdate.currentTraceContextPropagators
	}
	if err := r.Status().Update(ctx, monitoringResource); err != nil {
		logger.Error(err, updateStatusFailedMessageMonitoring)
		return err
	}
	return nil
}
