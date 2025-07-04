// SPDX-FileCopyrightText: Copyright 2024 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package controller

import (
	"context"
	"errors"
	"fmt"
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

	dash0v1alpha1 "github.com/dash0hq/dash0-operator/api/dash0monitoring/v1alpha1"
	"github.com/dash0hq/dash0-operator/internal/collectors"
	"github.com/dash0hq/dash0-operator/internal/instrumentation"
	"github.com/dash0hq/dash0-operator/internal/resources"
	"github.com/dash0hq/dash0-operator/internal/util"
)

type MonitoringReconciler struct {
	client.Client
	Clientset              *kubernetes.Clientset
	Instrumenter           *instrumentation.Instrumenter
	CollectorManager       *collectors.CollectorManager
	Images                 util.Images
	OperatorNamespace      string
	DanglingEventsTimeouts *util.DanglingEventsTimeouts
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

func (r *MonitoringReconciler) SetupWithManager(mgr ctrl.Manager) error {
	if r.DanglingEventsTimeouts == nil {
		r.DanglingEventsTimeouts = defaultDanglingEventsTimeouts
	}
	return ctrl.NewControllerManagedBy(mgr).
		For(&dash0v1alpha1.Dash0Monitoring{}).
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

// The following markers are used to generate the rules permissions (RBAC) on config/rbac using controller-gen
// when the command <make manifests> is executed.
// To know more about markers see: https://book.kubebuilder.io/reference/markers.html
//+kubebuilder:rbac:groups=apps,resources=daemonsets;deployments;replicasets;statefulsets,verbs=get;list;watch;update;patch
//+kubebuilder:rbac:groups=batch,resources=cronjobs;jobs,verbs=get;list;watch;update;patch
//+kubebuilder:rbac:groups=core,resources=events,verbs=create;list;patch;update
//+kubebuilder:rbac:groups=core,resources=namespaces,verbs=get
//+kubebuilder:rbac:groups=core,resources=pods,verbs=get;list;delete
//+kubebuilder:rbac:groups=core,resources=endpoints,verbs=get
//+kubebuilder:rbac:groups=operator.dash0.com,resources=dash0monitorings,verbs=get;list;watch;create;update;patch;delete;deletecollection
//+kubebuilder:rbac:groups=operator.dash0.com,resources=dash0monitorings/finalizers,verbs=update
//+kubebuilder:rbac:groups=operator.dash0.com,resources=dash0monitorings/status,verbs=get;update;patch

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

	namespaceStillExists, err := resources.CheckIfNamespaceExists(ctx, r.Clientset, req.Namespace, &logger)
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
		&dash0v1alpha1.Dash0Monitoring{},
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
		if err = r.reconcileOpenTelemetryCollector(ctx, nil, &logger); err != nil {
			return ctrl.Result{}, err
		}

		return ctrl.Result{}, nil
	} else if checkResourceResult.StopReconcile {
		return ctrl.Result{}, nil
	}

	monitoringResource := checkResourceResult.Resource.(*dash0v1alpha1.Dash0Monitoring)

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
		dash0v1alpha1.MonitoringFinalizerId,
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
		if r.reconcileOpenTelemetryCollector(ctx, monitoringResource, &logger) != nil {
			return ctrl.Result{}, err
		}
		// The Dash0 monitoring resource is slated for deletion, the finalizer has already been removed in the last
		// reconcile cycle (which also means all cleanup actions have been processed last time), no further
		// reconciliation is necessary.
		return ctrl.Result{}, nil
	}

	var requiredAction util.ModificationMode
	monitoringResource, requiredAction, err =
		r.manageInstrumentWorkloadsModeChanges(ctx, monitoringResource, isFirstReconcile, &logger)
	if err != nil {
		// The error has already been logged in manageInstrumentWorkloadsModeChanges
		return ctrl.Result{}, err
	}

	if isFirstReconcile || requiredAction == util.ModificationModeInstrumentation {
		if err = r.Instrumenter.CheckSettingsAndInstrumentExistingWorkloads(ctx, monitoringResource, &logger); err != nil {
			// The error has already been logged in checkSettingsAndInstrumentExistingWorkloads
			logger.Info("Requeuing reconcile request.")
			return ctrl.Result{}, err
		}
	} else if requiredAction == util.ModificationModeUninstrumentation {
		if err = r.Instrumenter.UninstrumentWorkloadsIfAvailable(ctx, monitoringResource, &logger); err != nil {
			logger.Error(err, "Failed to uninstrument workloads, requeuing reconcile request.")
			return ctrl.Result{}, err
		}
	}

	r.scheduleAttachDanglingEvents(ctx, monitoringResource, &logger)

	monitoringResource.EnsureResourceIsMarkedAsAvailable()
	if err = r.Status().Update(ctx, monitoringResource); err != nil {
		logger.Error(err, updateStatusFailedMessageMonitoring)
		return ctrl.Result{}, err
	}

	if r.reconcileOpenTelemetryCollector(ctx, monitoringResource, &logger) != nil {
		return ctrl.Result{}, err
	}

	return ctrl.Result{}, nil
}

func (r *MonitoringReconciler) manageInstrumentWorkloadsModeChanges(
	ctx context.Context,
	monitoringResource *dash0v1alpha1.Dash0Monitoring,
	isFirstReconcile bool,
	logger *logr.Logger,
) (*dash0v1alpha1.Dash0Monitoring, util.ModificationMode, error) {
	previous := monitoringResource.Status.PreviousInstrumentWorkloadsMode
	current := monitoringResource.ReadInstrumentWorkloadsMode()

	var requiredAction util.ModificationMode
	if !isFirstReconcile {
		if previous != dash0v1alpha1.All && previous != "" && current == dash0v1alpha1.All {
			logger.Info(fmt.Sprintf(
				"The instrumentWorkloads mode has changed from \"%s\" to \"%s\" (or it is absent, in which case it"+
					"defaults to \"all\"). Workloads in this namespace will now be instrumented so they send "+
					"telemetry to Dash0.", previous, current))
			requiredAction = util.ModificationModeInstrumentation
		} else if previous != dash0v1alpha1.None && current == dash0v1alpha1.None {
			logger.Info(fmt.Sprintf(
				"The instrumentWorkloads mode has changed from \"%s\" to \"%s\". Instrumented workloads in this "+
					"namespace will now be uninstrumented, they will no longer send telemetry to Dash0.",
				previous,
				current))
			requiredAction = util.ModificationModeUninstrumentation
		}
	}

	if previous != current {
		monitoringResource.Status.PreviousInstrumentWorkloadsMode = current
		if err := r.Status().Update(ctx, monitoringResource); err != nil {
			logger.Error(err, "Failed to update the previous instrumentWorkloads mode status on the Dash0 monitoring "+
				"resource, requeuing reconcile request.")
			return monitoringResource, "", err
		}
	}
	return monitoringResource, requiredAction, nil
}

func (r *MonitoringReconciler) runCleanupActions(
	ctx context.Context,
	monitoringResource *dash0v1alpha1.Dash0Monitoring,
	logger *logr.Logger,
) error {
	if err := r.Instrumenter.UninstrumentWorkloadsIfAvailable(
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

	if err := r.reconcileOpenTelemetryCollector(ctx, monitoringResource, logger); err != nil {
		// err has been logged in r.reconcileOpenTelemetryCollector already
		return err
	}

	if finalizersUpdated := controllerutil.RemoveFinalizer(monitoringResource, dash0v1alpha1.MonitoringFinalizerId); finalizersUpdated {
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
	monitoringResource *dash0v1alpha1.Dash0Monitoring,
	logger *logr.Logger,
) {
	// execute the attachment in a separate go routine to not block the main reconcile loop
	go func() {
		if r.DanglingEventsTimeouts.InitialTimeout > 0 {
			// wait a bit before attempting to attach dangling events, since the K8s resources do not exist yet when
			// the webhook queues the events
			time.Sleep(r.DanglingEventsTimeouts.InitialTimeout)
		}
		r.attachDanglingEvents(ctx, monitoringResource, logger)
	}()
}

/*
 * attachDanglingEvents attaches events that have been created by the webhook to the involved object of the event.
 * When the webhook creates the respective events, the workload might not exist yet, so the UID of the workload is not
 * set in the event. The list of events for that workload will not contain this event. We go the extra mile here and
 * retroactively fix the reference of all those events.
 */
func (r *MonitoringReconciler) attachDanglingEvents(
	ctx context.Context,
	monitoringResource *dash0v1alpha1.Dash0Monitoring,
	logger *logr.Logger,
) {
	namespace := monitoringResource.Namespace
	eventApi := r.Clientset.CoreV1().Events(namespace)
	backoff := r.DanglingEventsTimeouts.Backoff
	for _, eventType := range util.AllEvents {
		retryErr := util.RetryWithCustomBackoff(
			"attaching dangling event to involved object",
			func() error {
				danglingEvents, listErr := eventApi.List(ctx, metav1.ListOptions{
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
					attachErr := util.AttachEventToInvolvedObject(ctx, r.Client, eventApi, &event)
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
	monitoringResource *dash0v1alpha1.Dash0Monitoring,
	logger *logr.Logger,
) error {
	// This will look up all the operator configuration resource and all monitoring resources in the cluster (including
	// the one that has just been reconciled, hence we must only do this _after_ this resource has been updated (e.g.
	// marked as available). Otherwise, the reconciliation of the collectors would work with an outdated state.
	if err, _ := r.CollectorManager.ReconcileOpenTelemetryCollector(
		ctx,
		r.Images,
		r.OperatorNamespace,
		monitoringResource,
		collectors.TriggeredByDash0ResourceReconcile,
	); err != nil {
		logger.Error(err, "Failed to reconcile the OpenTelemetry collector, requeuing reconcile request.")
		return err
	}
	return nil
}
