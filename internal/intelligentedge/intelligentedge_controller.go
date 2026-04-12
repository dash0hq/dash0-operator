// SPDX-FileCopyrightText: Copyright 2026 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package intelligentedge

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
	updateStatusFailedMessageIntelligentEdge = "Failed to update Dash0 intelligent edge status conditions, requeuing reconcile request."
)

type IntelligentEdgeReconciler struct {
	client.Client
	intelligentEdgeManager *IntelligentEdgeManager
	collectorManager       *collectors.CollectorManager
}

func NewIntelligentEdgeReconciler(
	k8sClient client.Client,
	intelligentEdgeManager *IntelligentEdgeManager,
	collectorManager *collectors.CollectorManager,
) *IntelligentEdgeReconciler {
	return &IntelligentEdgeReconciler{
		Client:                 k8sClient,
		intelligentEdgeManager: intelligentEdgeManager,
		collectorManager:       collectorManager,
	}
}

func (r *IntelligentEdgeReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		Named("intelligentedgecontroller").
		For(&dash0v1alpha1.Dash0IntelligentEdge{}).
		WithEventFilter(predicate.GenerationChangedPredicate{}).
		Complete(r)
}

func (r *IntelligentEdgeReconciler) Reconcile(
	ctx context.Context,
	req reconcile.Request,
) (reconcile.Result, error) {
	logger := logd.FromContext(ctx)
	logger.Info("reconciling intelligent edge resource triggered by watch event", "request", req)

	checkResourceResult, err := resources.VerifyThatUniqueNonDegradedResourceExists(
		ctx,
		r.Client,
		req,
		&dash0v1alpha1.Dash0IntelligentEdge{},
		updateStatusFailedMessageIntelligentEdge,
		logger,
	)
	if err != nil {
		// temporary error, requeue
		return ctrl.Result{}, err
	} else if checkResourceResult.ResourceDoesNotExist {
		// IE resource has been deleted: remove barker & reconcile the collector.
		hasBeenReconciled, reconcileErr := r.intelligentEdgeManager.ReconcileIntelligentEdge(
			ctx, nil, TriggeredByWatchEvent,
		)
		if reconcileErr != nil {
			logger.Error(reconcileErr, "failed to reconcile intelligent edge deletion")
			return reconcile.Result{}, reconcileErr
		}
		if hasBeenReconciled {
			logger.Info("successfully reconciled intelligent edge resource deletion", "request", req)
		}
		if _, reconcileErr = r.collectorManager.ReconcileOpenTelemetryCollector(ctx); reconcileErr != nil {
			logger.Error(reconcileErr, "failed to reconcile collector resources after intelligent edge deletion")
			return reconcile.Result{}, reconcileErr
		}
		return reconcile.Result{}, nil
	} else if checkResourceResult.StopReconcile {
		// This resource has been marked as degraded (not the most recent one), skip reconciliation for it.
		return reconcile.Result{}, nil
	}

	intelligentEdgeResource := checkResourceResult.Resource.(*dash0v1alpha1.Dash0IntelligentEdge)

	if _, err := resources.InitStatusConditions(
		ctx,
		r.Client,
		intelligentEdgeResource,
		intelligentEdgeResource.Status.Conditions,
		logger,
	); err != nil {
		return ctrl.Result{}, err
	}

	hasBeenReconciled, err := r.intelligentEdgeManager.ReconcileIntelligentEdge(
		ctx, intelligentEdgeResource, TriggeredByWatchEvent,
	)
	if err != nil {
		intelligentEdgeResource.EnsureResourceIsMarkedAsDegraded(
			"ReconcileFailed",
			"Failed to reconcile intelligent edge resource.",
		)
		if statusErr := r.Status().Update(ctx, intelligentEdgeResource); statusErr != nil {
			logger.Error(statusErr, updateStatusFailedMessageIntelligentEdge)
		}
		logger.Error(err, "failed to reconcile intelligent edge resource")
		return reconcile.Result{}, err
	}
	if hasBeenReconciled {
		logger.Info("successfully reconciled intelligent edge resource", "request", req)
	}

	if _, err := r.collectorManager.ReconcileOpenTelemetryCollector(ctx); err != nil {
		intelligentEdgeResource.EnsureResourceIsMarkedAsDegraded(
			"CollectorReconcileFailed",
			"Failed to reconcile collector resources after intelligent edge change.",
		)
		if statusErr := r.Status().Update(ctx, intelligentEdgeResource); statusErr != nil {
			logger.Error(statusErr, updateStatusFailedMessageIntelligentEdge)
		}
		logger.Error(err, "failed to reconcile collector resources after intelligent edge change")
		return reconcile.Result{}, err
	}

	intelligentEdgeResource.EnsureResourceIsMarkedAsAvailable()
	if err := r.Status().Update(ctx, intelligentEdgeResource); err != nil {
		logger.Error(err, updateStatusFailedMessageIntelligentEdge)
		return reconcile.Result{}, err
	}

	return reconcile.Result{}, nil
}
