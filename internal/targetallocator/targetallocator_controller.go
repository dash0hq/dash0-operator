// SPDX-FileCopyrightText: Copyright 2025 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package targetallocator

import (
	"context"
	"slices"

	"github.com/dash0hq/dash0-operator/internal/targetallocator/taresources"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

type TargetAllocatorReconciler struct {
	client.Client
	targetAllocatorManager    *TargetAllocatorManager
	operatorNamespace         string
	targetAllocatorNamePrefix string
}

func NewTargetAllocatorReconciler(
	k8sClient client.Client,
	targetAllocatorManager *TargetAllocatorManager,
	operatorNamespace string,
	targetAllocatorNamePrefix string,
) *TargetAllocatorReconciler {
	return &TargetAllocatorReconciler{
		Client:                    k8sClient,
		targetAllocatorManager:    targetAllocatorManager,
		operatorNamespace:         operatorNamespace,
		targetAllocatorNamePrefix: targetAllocatorNamePrefix,
	}
}

func (r *TargetAllocatorReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		Named("targetallocatorcontroller").
		Watches(
			&corev1.ConfigMap{},
			&handler.EnqueueRequestForObject{},
			r.withNamePredicate([]string{
				taresources.ConfigMapName(r.targetAllocatorNamePrefix),
			}, true)).
		Watches(
			&corev1.ServiceAccount{},
			&handler.EnqueueRequestForObject{},
			r.withNamePredicate([]string{
				taresources.ServiceAccountName(r.targetAllocatorNamePrefix),
			}, true)).
		Watches(
			&rbacv1.ClusterRole{},
			&handler.EnqueueRequestForObject{},
			r.withNamePredicate([]string{
				taresources.ClusterRoleName(r.targetAllocatorNamePrefix),
			}, false)).
		Watches(
			&rbacv1.ClusterRoleBinding{},
			&handler.EnqueueRequestForObject{},
			r.withNamePredicate([]string{
				taresources.ClusterRoleBindingName(r.targetAllocatorNamePrefix),
			}, false)).
		Watches(
			&corev1.Service{},
			&handler.EnqueueRequestForObject{},
			r.withNamePredicate([]string{
				taresources.ServiceName(r.targetAllocatorNamePrefix),
			}, true)).
		Watches(
			&appsv1.Deployment{},
			&handler.EnqueueRequestForObject{},
			builder.WithPredicates(
				r.createNameFilterPredicate([]string{
					taresources.DeploymentName(r.targetAllocatorNamePrefix),
				}, true), generationOrLabelChangePredicate)).
		Complete(r)
}

var generationOrLabelChangePredicate = predicate.Or(predicate.GenerationChangedPredicate{}, predicate.LabelChangedPredicate{})

func (r *TargetAllocatorReconciler) withNamePredicate(resourceNames []string, namespaced bool) builder.Predicates {
	return builder.WithPredicates(r.createNameFilterPredicate(resourceNames, namespaced))
}

func (r *TargetAllocatorReconciler) createNameFilterPredicate(resourceNames []string, namespaced bool) predicate.Funcs {
	resourceNamespace := ""
	if namespaced {
		resourceNamespace = r.operatorNamespace
	}
	return predicate.Funcs{
		CreateFunc: func(e event.CreateEvent) bool {
			return resourceMatches(e.Object, resourceNamespace, resourceNames)
		},
		UpdateFunc: func(e event.UpdateEvent) bool {
			return resourceMatches(e.ObjectOld, resourceNamespace, resourceNames) ||
				resourceMatches(e.ObjectNew, resourceNamespace, resourceNames)
		},
		DeleteFunc: func(e event.DeleteEvent) bool {
			return resourceMatches(e.Object, resourceNamespace, resourceNames)
		},
		GenericFunc: func(e event.GenericEvent) bool {
			return resourceMatches(e.Object, resourceNamespace, resourceNames)
		},
	}
}

func resourceMatches(object client.Object, resourceNamespace string, resourceNames []string) bool {
	if object.GetNamespace() != resourceNamespace {
		return false
	}
	return slices.Contains(resourceNames, object.GetName())
}

func (r *TargetAllocatorReconciler) Reconcile(
	ctx context.Context,
	request reconcile.Request,
) (reconcile.Result, error) {
	logger := log.FromContext(ctx)
	logger.Info("reconciling target-allocator resources triggered by watch event", "request", request)

	hasBeenReconciled, err := r.targetAllocatorManager.ReconcileTargetAllocator(
		ctx,
		TriggeredByWatchEvent,
	)
	if err != nil {
		logger.Error(err, "Failed to create/update target-allocator resources.")
		return reconcile.Result{}, err
	}
	if hasBeenReconciled {
		logger.Info(
			"successfully reconciled target-allocator resources",
			"request",
			request,
		)
	}

	return reconcile.Result{}, nil
}
