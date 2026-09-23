// SPDX-FileCopyrightText: Copyright 2026 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package syntheticsworker

import (
	"context"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	"github.com/dash0hq/dash0-operator/internal/syntheticsworker/swresources"
	"github.com/dash0hq/dash0-operator/internal/util/logd"
)

type SyntheticsWorkerReconciler struct {
	client.Client
	syntheticsWorkerManager *SyntheticsWorkerManager
	operatorNamespace       string
	namePrefix              string
}

func NewSyntheticsWorkerReconciler(
	k8sClient client.Client,
	syntheticsWorkerManager *SyntheticsWorkerManager,
	operatorNamespace string,
	namePrefix string,
) *SyntheticsWorkerReconciler {
	return &SyntheticsWorkerReconciler{
		Client:                  k8sClient,
		syntheticsWorkerManager: syntheticsWorkerManager,
		operatorNamespace:       operatorNamespace,
		namePrefix:              namePrefix,
	}
}

func (r *SyntheticsWorkerReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		Named("syntheticsworkercontroller").
		Watches(
			&corev1.ServiceAccount{},
			&handler.EnqueueRequestForObject{},
			builder.WithPredicates(r.createFeatureFilterPredicate())).
		Watches(
			&appsv1.Deployment{},
			&handler.EnqueueRequestForObject{},
			builder.WithPredicates(r.createFeatureFilterPredicate(), generationOrLabelChangePredicate)).
		Complete(r)
}

var generationOrLabelChangePredicate = predicate.Or(predicate.GenerationChangedPredicate{}, predicate.LabelChangedPredicate{})

// createFeatureFilterPredicate restricts the watch to synthetics-worker resources (identified by label, since the set
// of instance names is dynamic) in the operator namespace. Every resource the synthetics-worker controller watches
// (ServiceAccount, Deployment) is namespaced, unlike the agent0-connector's cluster-scoped RBAC resources, so there is
// no need for a namespaced/cluster-scoped switch here.
func (r *SyntheticsWorkerReconciler) createFeatureFilterPredicate() predicate.Funcs {
	return predicate.Funcs{
		CreateFunc: func(e event.CreateEvent) bool {
			return resourceMatches(e.Object, r.operatorNamespace)
		},
		UpdateFunc: func(e event.UpdateEvent) bool {
			return resourceMatches(e.ObjectOld, r.operatorNamespace) ||
				resourceMatches(e.ObjectNew, r.operatorNamespace)
		},
		DeleteFunc: func(e event.DeleteEvent) bool {
			return resourceMatches(e.Object, r.operatorNamespace)
		},
		GenericFunc: func(e event.GenericEvent) bool {
			return resourceMatches(e.Object, r.operatorNamespace)
		},
	}
}

func resourceMatches(object client.Object, resourceNamespace string) bool {
	if object.GetNamespace() != resourceNamespace {
		return false
	}
	return swresources.IsSyntheticsWorkerResource(object)
}

func (r *SyntheticsWorkerReconciler) Reconcile(
	ctx context.Context,
	request reconcile.Request,
) (reconcile.Result, error) {
	logger := logd.FromContext(ctx)
	logger.Debug("reconciling synthetics-worker resources triggered by watch event", "request", request)

	hasBeenReconciled, err := r.syntheticsWorkerManager.ReconcileSyntheticsWorker(ctx, TriggeredByWatchEvent)
	if err != nil {
		logger.Error(err, "Failed to create/update synthetics-worker resources.")
		return reconcile.Result{}, err
	}
	if hasBeenReconciled {
		logger.Debug("successfully reconciled synthetics-worker resources", "request", request)
	}

	return reconcile.Result{}, nil
}
