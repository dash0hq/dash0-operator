// SPDX-FileCopyrightText: Copyright 2024 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package controller

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/go-logr/logr"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/kubernetes"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/log"

	dash0v1alpha1 "github.com/dash0hq/dash0-operator/api/dash0monitoring/v1alpha1"
	"github.com/dash0hq/dash0-operator/internal/backendconnection"
	"github.com/dash0hq/dash0-operator/internal/dash0/instrumentation"
	"github.com/dash0hq/dash0-operator/internal/dash0/util"
)

type DanglingEventsTimeouts struct {
	InitialTimeout time.Duration
	Backoff        wait.Backoff
}

type Dash0Reconciler struct {
	client.Client
	Clientset                *kubernetes.Clientset
	Instrumenter             *instrumentation.Instrumenter
	BackendConnectionManager *backendconnection.BackendConnectionManager
	Images                   util.Images
	OperatorNamespace        string
	DanglingEventsTimeouts   *DanglingEventsTimeouts
}

const (
	updateStatusFailedMessage = "Failed to update Dash0 monitoring status conditions, requeuing reconcile request."
)

var (
	defaultDanglingEventsTimeouts = &DanglingEventsTimeouts{
		InitialTimeout: 30 * time.Second,
		Backoff: wait.Backoff{
			Steps:    3,
			Duration: 30 * time.Second,
			Factor:   1.5,
			Jitter:   0.3,
		},
	}
)

func (r *Dash0Reconciler) SetupWithManager(mgr ctrl.Manager) error {
	if r.DanglingEventsTimeouts == nil {
		r.DanglingEventsTimeouts = defaultDanglingEventsTimeouts
	}
	return ctrl.NewControllerManagedBy(mgr).
		For(&dash0v1alpha1.Dash0Monitoring{}).
		Complete(r)
}

// The following markers are used to generate the rules permissions (RBAC) on config/rbac using controller-gen
// when the command <make manifests> is executed.
// To know more about markers see: https://book.kubebuilder.io/reference/markers.html
//+kubebuilder:rbac:groups=apps,resources=daemonsets;deployments;replicasets;statefulsets,verbs=get;list;watch;update;patch
//+kubebuilder:rbac:groups=batch,resources=cronjobs;jobs,verbs=get;list;watch;update;patch
//+kubebuilder:rbac:groups=core,resources=events,verbs=create;list;patch;update
//+kubebuilder:rbac:groups=core,resources=namespaces,verbs=get
//+kubebuilder:rbac:groups=core,resources=pods,verbs=get;list;delete
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
func (r *Dash0Reconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.FromContext(ctx)
	logger.Info("processing reconcile request for Dash0 monitoring resource")

	namespaceStillExists, err := util.CheckIfNamespaceExists(ctx, r.Clientset, req.Namespace, &logger)
	if err != nil {
		// The error has already been logged in checkIfNamespaceExists.
		return ctrl.Result{}, err
	} else if !namespaceStillExists {
		logger.Info("The namespace seems to have been deleted after this reconcile request has been scheduled. " +
			"Ignoring the reconcile request.")
		return ctrl.Result{}, nil
	}

	dash0MonitoringResource, stopReconcile, err := util.VerifyUniqueDash0MonitoringResourceExists(
		ctx,
		r.Client,
		updateStatusFailedMessage,
		req,
		&logger,
	)
	if err != nil {
		return ctrl.Result{}, err
	} else if stopReconcile {
		return ctrl.Result{}, nil
	}

	isFirstReconcile, err := util.InitStatusConditions(
		ctx,
		r.Status(),
		dash0MonitoringResource,
		&logger,
	)
	if err != nil {
		// The error has already been logged in initStatusConditions
		return ctrl.Result{}, err
	}

	isMarkedForDeletion, runCleanupActions, err := util.CheckImminentDeletionAndHandleFinalizers(
		ctx,
		r.Client,
		dash0MonitoringResource,
		dash0v1alpha1.FinalizerId,
		&logger,
	)
	if err != nil {
		// The error has already been logged in checkImminentDeletionAndHandleFinalizers
		return ctrl.Result{}, err
	} else if runCleanupActions {
		err = r.runCleanupActions(ctx, dash0MonitoringResource, &logger)
		if err != nil {
			// error has already been logged in runCleanupActions
			return ctrl.Result{}, err
		}
		// The Dash0 monitoring resource is slated for deletion, all cleanup actions (like reverting instrumented
		// resources) have been processed, no further reconciliation is necessary.
		return ctrl.Result{}, nil
	} else if isMarkedForDeletion {
		// The Dash0 monitoring resource is slated for deletion, the finalizer has already been removed (which means all
		// cleanup actions have been processed), no further reconciliation is necessary.
		return ctrl.Result{}, nil
	}

	// Make sure that an OpenTelemetry collector instance has been created in the namespace of the operator, and that
	// its configuration is up-to-date.
	if err = r.BackendConnectionManager.EnsureOpenTelemetryCollectorIsDeployedInDash0OperatorNamespace(
		ctx,
		r.Images,
		r.OperatorNamespace,
		dash0MonitoringResource,
	); err != nil {
		return ctrl.Result{}, err
	}

	var requiredAction util.ModificationMode
	dash0MonitoringResource, requiredAction, err =
		r.manageInstrumentWorkloadsChanges(ctx, dash0MonitoringResource, isFirstReconcile, &logger)
	if err != nil {
		// The error has already been logged in manageInstrumentWorkloadsChanges
		return ctrl.Result{}, err
	}

	if isFirstReconcile || requiredAction == util.ModificationModeInstrumentation {
		if err = r.Instrumenter.CheckSettingsAndInstrumentExistingWorkloads(ctx, dash0MonitoringResource, &logger); err != nil {
			// The error has already been logged in checkSettingsAndInstrumentExistingWorkloads
			logger.Info("Requeuing reconcile request.")
			return ctrl.Result{}, err
		}
	} else if requiredAction == util.ModificationModeUninstrumentation {
		if err = r.Instrumenter.UninstrumentWorkloadsIfAvailable(ctx, dash0MonitoringResource, &logger); err != nil {
			logger.Error(err, "Failed to uninstrument workloads, requeuing reconcile request.")
			return ctrl.Result{}, err
		}
	}

	r.scheduleAttachDanglingEvents(ctx, dash0MonitoringResource, &logger)

	dash0MonitoringResource.EnsureResourceIsMarkedAsAvailable()
	if err = r.Status().Update(ctx, dash0MonitoringResource); err != nil {
		logger.Error(err, updateStatusFailedMessage)
		return ctrl.Result{}, err
	}

	return ctrl.Result{}, nil
}

func (r *Dash0Reconciler) manageInstrumentWorkloadsChanges(
	ctx context.Context,
	dash0MonitoringResource *dash0v1alpha1.Dash0Monitoring,
	isFirstReconcile bool,
	logger *logr.Logger,
) (*dash0v1alpha1.Dash0Monitoring, util.ModificationMode, error) {
	previous := dash0MonitoringResource.Status.PreviousInstrumentWorkloads
	current := dash0MonitoringResource.ReadInstrumentWorkloadsSetting()

	var requiredAction util.ModificationMode
	if !isFirstReconcile {
		if previous != dash0v1alpha1.All && previous != "" && current == dash0v1alpha1.All {
			logger.Info(fmt.Sprintf(
				"The instrumentWorkloads setting has changed from \"%s\" to \"%s\" (or it is absent, in which case it"+
					"defaults to \"all\"). Workloads in this namespace will now be instrumented so they send "+
					"telemetry to Dash0.", previous, current))
			requiredAction = util.ModificationModeInstrumentation
		} else if previous != dash0v1alpha1.None && current == dash0v1alpha1.None {
			logger.Info(fmt.Sprintf(
				"The instrumentWorkloads setting has changed from \"%s\" to \"%s\". Instrumented workloads in this "+
					"namespace will now be uninstrumented, they will no longer send telemetry to Dash0.",
				previous,
				current))
			requiredAction = util.ModificationModeUninstrumentation
		}
	}

	if previous != current {
		dash0MonitoringResource.Status.PreviousInstrumentWorkloads = current
		if err := r.Status().Update(ctx, dash0MonitoringResource); err != nil {
			logger.Error(err, "Failed to update the previous instrumentWorkloads status on the Dash0 monitoring "+
				"resource, requeuing reconcile request.")
			return dash0MonitoringResource, "", err
		}
	}
	return dash0MonitoringResource, requiredAction, nil
}

func (r *Dash0Reconciler) runCleanupActions(
	ctx context.Context,
	dash0MonitoringResource *dash0v1alpha1.Dash0Monitoring,
	logger *logr.Logger,
) error {
	if err := r.Instrumenter.UninstrumentWorkloadsIfAvailable(
		ctx,
		dash0MonitoringResource,
		logger,
	); err != nil {
		logger.Error(err, "Failed to uninstrument workloads, requeuing reconcile request.")
		return err
	}

	if err := r.BackendConnectionManager.RemoveOpenTelemetryCollectorIfNoDash0MonitoringResourceIsLeft(
		ctx,
		r.Images,
		r.OperatorNamespace,
		dash0MonitoringResource,
	); err != nil {
		logger.Error(err, "Failed to check if the OpenTelemetry collector instance needs to be removed or failed "+
			"removing it.")
		return err
	}

	// The Dash0 monitoring resource will be deleted after this reconcile finished, so updating the status is
	// probably unnecessary. But for due process we do it anyway. In particular, if deleting it should fail
	// for any reason or take a while, the resource is no longer marked as available.
	dash0MonitoringResource.EnsureResourceIsMarkedAsAboutToBeDeleted()
	if err := r.Status().Update(ctx, dash0MonitoringResource); err != nil {
		logger.Error(err, updateStatusFailedMessage)
		return err
	}

	controllerutil.RemoveFinalizer(dash0MonitoringResource, dash0v1alpha1.FinalizerId)
	if err := r.Update(ctx, dash0MonitoringResource); err != nil {
		logger.Error(err, "Failed to remove the finalizer from the Dash0 monitoring resource, requeuing reconcile "+
			"request.")
		return err
	}
	return nil
}

func (r *Dash0Reconciler) scheduleAttachDanglingEvents(
	ctx context.Context,
	dash0MonitoringResource *dash0v1alpha1.Dash0Monitoring,
	logger *logr.Logger,
) {
	// execute the attachment in a separate go routine to not block the main reconcile loop
	go func() {
		if r.DanglingEventsTimeouts.InitialTimeout > 0 {
			// wait a bit before attempting to attach dangling events, since the K8s resources do not exist yet when
			// the webhook queues the events
			time.Sleep(r.DanglingEventsTimeouts.InitialTimeout)
		}
		r.attachDanglingEvents(ctx, dash0MonitoringResource, logger)
	}()
}

/*
 * attachDanglingEvents attaches events that have been created by the webhook to the involved object of the event.
 * When the webhook creates the respective events, the workload might not exist yet, so the UID of the workload is not
 * set in the event. The list of events for that workload will not contain this event. We go the extra mile here and
 * retroactively fix the reference of all those events.
 */
func (r *Dash0Reconciler) attachDanglingEvents(
	ctx context.Context,
	dash0MonitoringResource *dash0v1alpha1.Dash0Monitoring,
	logger *logr.Logger,
) {
	namespace := dash0MonitoringResource.Namespace
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
