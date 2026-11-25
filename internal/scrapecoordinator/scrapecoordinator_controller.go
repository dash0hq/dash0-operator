// SPDX-FileCopyrightText: Copyright 2026 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package scrapecoordinator

import (
	"context"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	"github.com/dash0hq/dash0-operator/internal/scrapecoordinator/scoresources"
	"github.com/dash0hq/dash0-operator/internal/util/logd"
)

// ScrapeCoordinatorReconciler reconciles the scrape coordinator resources when one of them is changed or deleted.
type ScrapeCoordinatorReconciler struct {
	client.Client
	manager           *ScrapeCoordinatorManager
	operatorNamespace string
	namePrefix        string
}

// NewScrapeCoordinatorReconciler creates a new ScrapeCoordinatorReconciler.
func NewScrapeCoordinatorReconciler(
	k8sClient client.Client,
	manager *ScrapeCoordinatorManager,
	operatorNamespace string,
	namePrefix string,
) *ScrapeCoordinatorReconciler {
	return &ScrapeCoordinatorReconciler{
		Client:            k8sClient,
		manager:           manager,
		operatorNamespace: operatorNamespace,
		namePrefix:        namePrefix,
	}
}

// SetupWithManager registers the watches for the scrape coordinator resources.
func (r *ScrapeCoordinatorReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		Named("scrapecoordinatorcontroller").
		Watches(
			&corev1.ConfigMap{},
			&handler.EnqueueRequestForObject{},
			r.withNamePredicate(scoresources.ConfigMapName(r.namePrefix), true)).
		Watches(
			&corev1.ServiceAccount{},
			&handler.EnqueueRequestForObject{},
			r.withNamePredicate(scoresources.ServiceAccountName(r.namePrefix), true)).
		Watches(
			&rbacv1.ClusterRole{},
			&handler.EnqueueRequestForObject{},
			r.withNamePredicate(scoresources.ClusterRoleName(r.namePrefix), false)).
		Watches(
			&rbacv1.ClusterRoleBinding{},
			&handler.EnqueueRequestForObject{},
			r.withNamePredicate(scoresources.ClusterRoleBindingName(r.namePrefix), false)).
		Watches(
			&corev1.Service{},
			&handler.EnqueueRequestForObject{},
			r.withNamePredicate(scoresources.ServiceName(r.namePrefix), true)).
		Watches(
			&appsv1.Deployment{},
			&handler.EnqueueRequestForObject{},
			builder.WithPredicates(
				r.createNameFilterPredicate(scoresources.DeploymentName(r.namePrefix), true),
				generationOrLabelChangePredicate,
			)).
		Complete(r)
}

var generationOrLabelChangePredicate = predicate.Or(
	predicate.GenerationChangedPredicate{},
	predicate.LabelChangedPredicate{},
)

func (r *ScrapeCoordinatorReconciler) withNamePredicate(resourceName string, namespaced bool) builder.Predicates {
	return builder.WithPredicates(r.createNameFilterPredicate(resourceName, namespaced))
}

func (r *ScrapeCoordinatorReconciler) createNameFilterPredicate(resourceName string, namespaced bool) predicate.Funcs {
	resourceNamespace := ""
	if namespaced {
		resourceNamespace = r.operatorNamespace
	}
	matches := func(object client.Object) bool {
		return object.GetNamespace() == resourceNamespace && object.GetName() == resourceName
	}
	return predicate.Funcs{
		CreateFunc: func(e event.CreateEvent) bool {
			return matches(e.Object)
		},
		UpdateFunc: func(e event.UpdateEvent) bool {
			return matches(e.ObjectOld) || matches(e.ObjectNew)
		},
		DeleteFunc: func(e event.DeleteEvent) bool {
			return matches(e.Object)
		},
		GenericFunc: func(e event.GenericEvent) bool {
			return matches(e.Object)
		},
	}
}

// Reconcile reconciles all scrape coordinator resources, regardless of which one triggered the request.
func (r *ScrapeCoordinatorReconciler) Reconcile(ctx context.Context, request reconcile.Request) (reconcile.Result, error) {
	logger := logd.FromContext(ctx)
	logger.Info("reconciling scrape coordinator resources triggered by watch event", "request", request)

	hasBeenReconciled, err := r.manager.ReconcileScrapeCoordinator(ctx, TriggeredByWatchEvent)
	if err != nil {
		logger.ErrorTelemetryCollectionIssue(err, "Failed to create/update scrape coordinator resources.")
		return reconcile.Result{}, err
	}
	if hasBeenReconciled {
		logger.Info("successfully reconciled scrape coordinator resources", "request", request)
	}
	return reconcile.Result{}, nil
}
