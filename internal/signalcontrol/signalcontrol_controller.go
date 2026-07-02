// SPDX-FileCopyrightText: Copyright 2026 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package signalcontrol

import (
	"context"

	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	dash0v1alpha1 "github.com/dash0hq/dash0-operator/api/operator/v1alpha1"
	"github.com/dash0hq/dash0-operator/internal/collectors"
	"github.com/dash0hq/dash0-operator/internal/resources"
	"github.com/dash0hq/dash0-operator/internal/util/logd"
)

const (
	updateStatusFailedMessageSignalControl = "Failed to update Dash0 Signal Control status conditions, requeuing reconcile request."
)

type SignalControlReconciler struct {
	client.Client
	signalControlManager *SignalControlManager
	collectorManager     *collectors.CollectorManager
}

func NewSignalControlReconciler(
	k8sClient client.Client,
	signalControlManager *SignalControlManager,
	collectorManager *collectors.CollectorManager,
) *SignalControlReconciler {
	return &SignalControlReconciler{
		Client:               k8sClient,
		signalControlManager: signalControlManager,
		collectorManager:     collectorManager,
	}
}

func (r *SignalControlReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		Named("signalcontrolcontroller").
		For(&dash0v1alpha1.Dash0SignalControl{}).
		WithEventFilter(predicate.GenerationChangedPredicate{}).
		Complete(r)
}

func (r *SignalControlReconciler) Reconcile(
	ctx context.Context,
	req reconcile.Request,
) (reconcile.Result, error) {
	logger := logd.FromContext(ctx)
	logger.Info("reconciling Signal Control resource triggered by watch event", "request", req)

	checkResourceResult, err := resources.VerifyThatUniqueNonDegradedResourceExists(
		ctx,
		r.Client,
		req,
		&dash0v1alpha1.Dash0SignalControl{},
		updateStatusFailedMessageSignalControl,
		logger,
	)
	if err != nil {
		// temporary error, requeue
		return ctrl.Result{}, err
	} else if checkResourceResult.ResourceDoesNotExist {
		// Signal Control resource has been deleted: remove the Edge Proxy & reconcile the collector.
		hasBeenReconciled, reconcileErr := r.signalControlManager.ReconcileSignalControl(ctx, nil)
		if reconcileErr != nil {
			logger.Error(reconcileErr, "failed to reconcile Signal Control deletion")
			return reconcile.Result{}, reconcileErr
		}
		if hasBeenReconciled {
			logger.Info("successfully reconciled Signal Control resource deletion", "request", req)
		}
		if _, reconcileErr = r.collectorManager.ReconcileOpenTelemetryCollector(ctx); reconcileErr != nil {
			logger.Error(reconcileErr, "failed to reconcile collector resources after Signal Control deletion")
			return reconcile.Result{}, reconcileErr
		}
		return reconcile.Result{}, nil
	} else if checkResourceResult.StopReconcile {
		// This resource has been marked as degraded (not the most recent one), skip reconciliation for it.
		return reconcile.Result{}, nil
	}

	signalControlResource := checkResourceResult.Resource.(*dash0v1alpha1.Dash0SignalControl)

	if _, err := resources.InitStatusConditions(
		ctx,
		r.Client,
		signalControlResource,
		signalControlResource.Status.Conditions,
		logger,
	); err != nil {
		return ctrl.Result{}, err
	}

	hasBeenReconciled, err := r.signalControlManager.ReconcileSignalControl(ctx, signalControlResource)
	if err != nil {
		signalControlResource.EnsureResourceIsMarkedAsDegraded(
			"ReconcileFailed",
			"Failed to reconcile Signal Control resource.",
		)
		if statusErr := r.Status().Update(ctx, signalControlResource); statusErr != nil {
			logger.Error(statusErr, updateStatusFailedMessageSignalControl)
		}
		logger.Error(err, "failed to reconcile Signal Control resource")
		return reconcile.Result{}, err
	}
	if hasBeenReconciled {
		logger.Info("successfully reconciled Signal Control resource", "request", req)
	}

	if _, err := r.collectorManager.ReconcileOpenTelemetryCollector(ctx); err != nil {
		signalControlResource.EnsureResourceIsMarkedAsDegraded(
			"CollectorReconcileFailed",
			"Failed to reconcile collector resources after Signal Control change.",
		)
		if statusErr := r.Status().Update(ctx, signalControlResource); statusErr != nil {
			logger.Error(statusErr, updateStatusFailedMessageSignalControl)
		}
		logger.Error(err, "failed to reconcile collector resources after Signal Control change")
		return reconcile.Result{}, err
	}

	signalControlResource.EnsureResourceIsMarkedAsAvailable()
	if err := r.Status().Update(ctx, signalControlResource); err != nil {
		logger.Error(err, updateStatusFailedMessageSignalControl)
		return reconcile.Result{}, err
	}

	return reconcile.Result{}, nil
}
