// SPDX-FileCopyrightText: Copyright 2026 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package agent0connector

import (
	"context"
	"slices"

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

	"github.com/dash0hq/dash0-operator/internal/agent0connector/a0cresources"
	"github.com/dash0hq/dash0-operator/internal/util/logd"
)

type Agent0ConnectorReconciler struct {
	client.Client
	agent0ConnectorManager *Agent0ConnectorManager
	operatorNamespace      string
	namePrefix             string
}

func NewAgent0ConnectorReconciler(
	k8sClient client.Client,
	agent0ConnectorManager *Agent0ConnectorManager,
	operatorNamespace string,
	namePrefix string,
) *Agent0ConnectorReconciler {
	return &Agent0ConnectorReconciler{
		Client:                 k8sClient,
		agent0ConnectorManager: agent0ConnectorManager,
		operatorNamespace:      operatorNamespace,
		namePrefix:             namePrefix,
	}
}

func (r *Agent0ConnectorReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		Named("agent0connectorcontroller").
		Watches(
			&corev1.ServiceAccount{},
			&handler.EnqueueRequestForObject{},
			r.withNamePredicate([]string{
				a0cresources.ServiceAccountName(r.namePrefix),
			}, true)).
		Watches(
			&rbacv1.ClusterRole{},
			&handler.EnqueueRequestForObject{},
			r.withNamePredicate([]string{
				a0cresources.ClusterRoleName(r.namePrefix),
			}, false)).
		Watches(
			&rbacv1.ClusterRoleBinding{},
			&handler.EnqueueRequestForObject{},
			r.withNamePredicate([]string{
				a0cresources.ClusterRoleBindingName(r.namePrefix),
			}, false)).
		Watches(
			&appsv1.Deployment{},
			&handler.EnqueueRequestForObject{},
			builder.WithPredicates(
				r.createNameFilterPredicate([]string{
					a0cresources.DeploymentName(r.namePrefix),
				}, true), generationOrLabelChangePredicate)).
		Complete(r)
}

var generationOrLabelChangePredicate = predicate.Or(predicate.GenerationChangedPredicate{}, predicate.LabelChangedPredicate{})

func (r *Agent0ConnectorReconciler) withNamePredicate(resourceNames []string, namespaced bool) builder.Predicates {
	return builder.WithPredicates(r.createNameFilterPredicate(resourceNames, namespaced))
}

func (r *Agent0ConnectorReconciler) createNameFilterPredicate(resourceNames []string, namespaced bool) predicate.Funcs {
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

func (r *Agent0ConnectorReconciler) Reconcile(
	ctx context.Context,
	request reconcile.Request,
) (reconcile.Result, error) {
	logger := logd.FromContext(ctx)
	logger.Debug("reconciling agent0-connector resources triggered by watch event", "request", request)

	hasBeenReconciled, err := r.agent0ConnectorManager.ReconcileAgent0Connector(ctx, TriggeredByWatchEvent)
	if err != nil {
		logger.Error(err, "Failed to create/update agent0-connector resources.")
		return reconcile.Result{}, err
	}
	if hasBeenReconciled {
		logger.Debug("successfully reconciled agent0-connector resources", "request", request)
	}

	return reconcile.Result{}, nil
}
