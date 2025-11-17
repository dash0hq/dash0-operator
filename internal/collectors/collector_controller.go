// SPDX-FileCopyrightText: Copyright 2024 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package collectors

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
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	"github.com/dash0hq/dash0-operator/internal/collectors/otelcolresources"
)

type CollectorReconciler struct {
	client.Client
	collectorManager        *CollectorManager
	operatorNamespace       string
	oTelCollectorNamePrefix string
}

func NewCollectorReconciler(
	k8sClient client.Client,
	collectorManager *CollectorManager,
	operatorNamespace string,
	oTelCollectorNamePrefix string,
) *CollectorReconciler {
	return &CollectorReconciler{
		Client:                  k8sClient,
		collectorManager:        collectorManager,
		operatorNamespace:       operatorNamespace,
		oTelCollectorNamePrefix: oTelCollectorNamePrefix,
	}
}

func (r *CollectorReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		Named("collectorcontroller").
		Watches(
			&corev1.ConfigMap{},
			&handler.EnqueueRequestForObject{},
			r.withNamePredicate([]string{
				otelcolresources.DaemonSetCollectorConfigConfigMapName(r.oTelCollectorNamePrefix),
				otelcolresources.DeploymentCollectorConfigConfigMapName(r.oTelCollectorNamePrefix),
				// Note: We are deliberately not watching the filelog receiver offsets ConfigMap, since it is updated
				// frequently by the filelog offset sync container and does not require reconciliation.
			}, true)).
		Watches(
			&rbacv1.ClusterRole{},
			&handler.EnqueueRequestForObject{},
			r.withNamePredicate([]string{
				otelcolresources.DaemonSetClusterRoleName(r.oTelCollectorNamePrefix),
				otelcolresources.DeploymentClusterRoleName(r.oTelCollectorNamePrefix),
			}, false)).
		Watches(
			&rbacv1.ClusterRoleBinding{},
			&handler.EnqueueRequestForObject{},
			r.withNamePredicate([]string{
				otelcolresources.DeploymentClusterRoleBindingName(r.oTelCollectorNamePrefix),
				otelcolresources.DeploymentClusterRoleName(r.oTelCollectorNamePrefix),
			}, false)).
		Watches(
			&corev1.Service{},
			&handler.EnqueueRequestForObject{},
			r.withNamePredicate([]string{
				otelcolresources.ServiceName(r.oTelCollectorNamePrefix),
			}, true)).
		Watches(
			&appsv1.DaemonSet{},
			&handler.EnqueueRequestForObject{},
			builder.WithPredicates(
				r.createNameFilterPredicate([]string{
					otelcolresources.DaemonSetName(r.oTelCollectorNamePrefix),
				}, true), generationOrLabelChangePredicate)).
		Watches(
			&appsv1.Deployment{},
			&handler.EnqueueRequestForObject{},
			builder.WithPredicates(
				r.createNameFilterPredicate([]string{
					otelcolresources.DeploymentName(r.oTelCollectorNamePrefix),
				}, true), generationOrLabelChangePredicate)).
		Complete(r)
}

var generationOrLabelChangePredicate = predicate.Or(predicate.GenerationChangedPredicate{}, predicate.LabelChangedPredicate{})

func (r *CollectorReconciler) withNamePredicate(resourceNames []string, namespaced bool) builder.Predicates {
	return builder.WithPredicates(r.createNameFilterPredicate(resourceNames, namespaced))
}

func (r *CollectorReconciler) createNameFilterPredicate(resourceNames []string, namespaced bool) predicate.Funcs {
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

func (r *CollectorReconciler) Reconcile(
	ctx context.Context,
	request reconcile.Request,
) (reconcile.Result, error) {
	logger := log.FromContext(ctx)
	logger.Info("reconciling collector resources", "request", request)

	hasBeenReconciled, err := r.collectorManager.ReconcileOpenTelemetryCollector(
		ctx,
		nil,
		TriggeredByWatchEvent,
	)
	if err != nil {
		logger.Error(err, "Failed to create/update collector resources.")
		return reconcile.Result{}, err
	}
	if hasBeenReconciled {
		logger.Info(
			"successfully reconciled collector resources",
			"request",
			request,
		)
	}

	return reconcile.Result{}, nil
}
