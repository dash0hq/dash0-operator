// SPDX-FileCopyrightText: Copyright 2024 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package backendconnection

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

	dash0v1alpha1 "github.com/dash0hq/dash0-operator/api/dash0monitoring/v1alpha1"
	"github.com/dash0hq/dash0-operator/internal/backendconnection/otelcolresources"
	"github.com/dash0hq/dash0-operator/internal/dash0/selfmonitoring"
	"github.com/dash0hq/dash0-operator/internal/dash0/util"
)

type BackendConnectionReconciler struct {
	client.Client
	BackendConnectionManager *BackendConnectionManager
	Images                   util.Images
	OperatorNamespace        string
	OTelCollectorNamePrefix  string
}

func (r *BackendConnectionReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		Named("dash0backendconnectioncontroller").
		Watches(
			&corev1.ConfigMap{},
			&handler.EnqueueRequestForObject{},
			r.withNamePredicate([]string{
				otelcolresources.DaemonSetCollectorConfigConfigMapName(r.OTelCollectorNamePrefix),
				otelcolresources.DeploymentCollectorConfigConfigMapName(r.OTelCollectorNamePrefix),
				// Note: We are deliberately not watching the filelog receiver offsets ConfigMap, since it is updated
				// frequently by the filelog offset synch container and does not require reconciliation.
			})).
		Watches(
			&rbacv1.ClusterRole{},
			&handler.EnqueueRequestForObject{},
			r.withNamePredicate([]string{
				otelcolresources.DaemonSetClusterRoleName(r.OTelCollectorNamePrefix),
				otelcolresources.DeploymentClusterRoleName(r.OTelCollectorNamePrefix),
			})).
		Watches(
			&rbacv1.ClusterRoleBinding{},
			&handler.EnqueueRequestForObject{},
			r.withNamePredicate([]string{
				otelcolresources.DeploymentClusterRoleBindingName(r.OTelCollectorNamePrefix),
				otelcolresources.DeploymentClusterRoleName(r.OTelCollectorNamePrefix),
			})).
		Watches(
			&corev1.Service{},
			&handler.EnqueueRequestForObject{},
			r.withNamePredicate([]string{
				otelcolresources.ServiceName(r.OTelCollectorNamePrefix),
			})).
		Watches(
			&appsv1.DaemonSet{},
			&handler.EnqueueRequestForObject{},
			r.withNamePredicate([]string{
				otelcolresources.DaemonSetName(r.OTelCollectorNamePrefix),
			})).
		Watches(
			&appsv1.Deployment{},
			&handler.EnqueueRequestForObject{},
			r.withNamePredicate([]string{
				otelcolresources.DeploymentName(r.OTelCollectorNamePrefix),
			})).
		Complete(r)
}

func (r *BackendConnectionReconciler) withNamePredicate(resourceNames []string) builder.Predicates {
	return builder.WithPredicates(r.createFilterPredicate(resourceNames))
}

func (r *BackendConnectionReconciler) createFilterPredicate(resourceNames []string) predicate.Funcs {
	resourceNamespace := r.OperatorNamespace
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

func (r *BackendConnectionReconciler) Reconcile(
	ctx context.Context,
	request reconcile.Request,
) (reconcile.Result, error) {
	logger := log.FromContext(ctx)
	logger.Info("reconciling backend connection resources", "request", request)

	allDash0MonitoringResouresInCluster := &dash0v1alpha1.Dash0MonitoringList{}
	if err := r.List(
		ctx,
		allDash0MonitoringResouresInCluster,
		&client.ListOptions{},
	); err != nil {
		logger.Error(err, "Failed to list all Dash0 monitoring resources when reconciling backend connection resources.")
		return reconcile.Result{}, err
	}

	if len(allDash0MonitoringResouresInCluster.Items) == 0 {
		logger.Info("No Dash0 monitoring resources in cluster, aborting the backend connection resources reconciliation.")
		return reconcile.Result{}, nil
	}

	// TODO this needs to be fixed when we start to support sending telemetry to different backends per namespace.
	// Ultimately we need to derive one consistent configuration including multiple pipelines and routing across all
	// monitored namespaces.
	arbitraryMonitoringResource := allDash0MonitoringResouresInCluster.Items[0]

	err := r.BackendConnectionManager.EnsureOpenTelemetryCollectorIsDeployedInOperatorNamespace(
		ctx,
		r.Images,
		r.OperatorNamespace,
		&arbitraryMonitoringResource,
		selfmonitoring.ReadSelfMonitoringConfigurationFromOperatorConfigurationResource(ctx, r.Client, &logger),
		TriggeredByWatchEvent,
	)
	if err != nil {
		logger.Error(err, "Failed to create/update backend connection resources.")
		return reconcile.Result{}, err
	}

	logger.Info(
		"successfully reconciled backend connection resources",
		"request",
		request,
	)

	return reconcile.Result{}, nil
}
