// SPDX-FileCopyrightText: Copyright 2024 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package controller

import (
	"context"
	"sync/atomic"

	"github.com/go-logr/logr"
	prometheusoperator "github.com/prometheus-operator/prometheus-operator/pkg/apis/monitoring/v1alpha1"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/util/workqueue"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	"sigs.k8s.io/controller-runtime/pkg/source"

	"github.com/dash0hq/dash0-operator/internal/backendconnection"
)

type PrometheusScrapeConfigCrdReconciler struct {
	client.Client
	*kubernetes.Clientset
	*runtime.Scheme
	*backendconnection.BackendConnectionReconciler
	mgr                     ctrl.Manager
	cancelControllerContext atomic.Pointer[context.CancelFunc]
	scrapeConfigReconciler  *PrometheusScrapeConfigReconciler
}

//+kubebuilder:rbac:groups=apiextensions.k8s.io,resources=customresourcedefinitions,verbs=get;list;watch

func (r *PrometheusScrapeConfigCrdReconciler) SetupWithManager(
	ctx context.Context,
	mgr ctrl.Manager,
	startupK8sClient client.Client,
	logger *logr.Logger,
) error {
	r.mgr = mgr
	r.scrapeConfigReconciler = &PrometheusScrapeConfigReconciler{
		BackendConnectionReconciler: r.BackendConnectionReconciler,
	}

	if err := startupK8sClient.Get(ctx, client.ObjectKey{
		Name: "scrapeconfigs.monitoring.coreos.com",
	}, &apiextensionsv1.CustomResourceDefinition{}); err != nil {
		if apierrors.IsNotFound(err) {
			logger.Info("The scrapeconfigs.monitoring.coreos.com custom resource definition does not exist in this " +
				"cluster, the operator will not watch for ScrapeConfig resources.")
		} else {
			logger.Error(err, "unable to call get the scrapeconfigs.monitoring.coreos.com custom resource definition")
			return err
		}
	} else {
		logger.Info("The scrapeconfigs.monitoring.coreos.com custom resource definition is present in this " +
			"cluster, the operator will watch for ScrapeConfig resources.")
		r.startWatchingScrapeConfigs(ctx, logger)
	}

	if err := ctrl.NewControllerManagedBy(mgr).
		Named("dash0_scrapeconfig_crd_controller").
		Watches(
			&apiextensionsv1.CustomResourceDefinition{},
			// Deliberately not using a convenience mechanism like &handler.EnqueueRequestForObject{} (which would
			// feed all events into the Reconcile method) here, since using the lower-level TypedEventHandler interface
			// directly allows us to distinguish between create and delete events more easily.
			r,
			builder.WithPredicates(makeFilterPredicate())).
		Complete(r); err != nil {
		logger.Error(err, "unable to create a controller for the PrometheusScrapeConfigReconciler")
		return err
	}

	return nil
}

func makeFilterPredicate() predicate.Funcs {
	return predicate.Funcs{
		CreateFunc: func(e event.CreateEvent) bool {
			return isScrapeConfigCrd(e.Object)
		},
		UpdateFunc: func(e event.UpdateEvent) bool {
			// We are not interested in updates, but we still need to define a filter predicate for it, otherwise _all_
			// update events for CRDs would be passed to our event handler. We always return false to ignore update
			// events entirely. Same for generic events.
			return false
		},
		DeleteFunc: func(e event.DeleteEvent) bool {
			return isScrapeConfigCrd(e.Object)
		},
		GenericFunc: func(e event.GenericEvent) bool {
			return false
		},
	}
}

func isScrapeConfigCrd(crd_ client.Object) bool {
	if crd, ok := crd_.(*apiextensionsv1.CustomResourceDefinition); ok {
		return crd.Spec.Group == "monitoring.coreos.com" &&
			crd.Spec.Names.Kind == "ScrapeConfig"
	} else {
		return false
	}
}

func (r *PrometheusScrapeConfigCrdReconciler) Create(
	ctx context.Context,
	_ event.TypedCreateEvent[client.Object],
	_ workqueue.TypedRateLimitingInterface[reconcile.Request],
) {
	logger := log.FromContext(ctx)
	r.startWatchingScrapeConfigs(ctx, &logger)
}

func (r *PrometheusScrapeConfigCrdReconciler) Update(
	context.Context,
	event.TypedUpdateEvent[client.Object],
	workqueue.TypedRateLimitingInterface[reconcile.Request],
) {
	// should not be called, we are not interested in updates
	// note: update is called twice prior to delete, it is also called twice after an actual create
}

func (r *PrometheusScrapeConfigCrdReconciler) Delete(
	ctx context.Context,
	_ event.TypedDeleteEvent[client.Object],
	_ workqueue.TypedRateLimitingInterface[reconcile.Request],
) {
	logger := log.FromContext(ctx)
	logger.Info("XXXXXX CRD DELETE")

	cancelControllerContext := r.cancelControllerContext.Load()
	if cancelControllerContext != nil {
		logger.Info("No longer watching Prometheus ScrapeConfig custom resources, since the " +
			"scrapeconfigs.monitoring.coreos.com custom resource definition has been deleted.")
		(*cancelControllerContext)()

		// Known issue: Despite stopping the controller which owns the watch, the controller runtime is still calling
		// client.Client#List for v1alpha1.ScrapeConfig every ten seconds, which results in the following error being
		// logged every ten seconds:
		// manager W0920 11:02:18.546083 1 reflector.go:561
		// pkg/mod/k8s.io/client-go@v0.31.1/tools/cache/reflector.go:243: failed to list *v1alpha1.ScrapeConfig:
		// the server could not find the requested resource (get scrapeconfigs.monitoring.coreos.com)
	}
	r.cancelControllerContext.Store(nil)
}

func (r *PrometheusScrapeConfigCrdReconciler) Generic(
	context.Context,
	event.TypedGenericEvent[client.Object],
	workqueue.TypedRateLimitingInterface[reconcile.Request],
) {
	// Should not be called, we are not interested in generic events.
}

func (r *PrometheusScrapeConfigCrdReconciler) Reconcile(
	_ context.Context,
	_ reconcile.Request,
) (reconcile.Result, error) {
	// Reconcile should not be called for the PrometheusScrapeConfigCrdReconciler CRD, as we are using the
	// TypedEventHandler interface directly when setting up the watch. We still need to implement the method, as the
	// controller builder's Complete method requires implementing the Reconciler interface.
	return reconcile.Result{}, nil
}

//+kubebuilder:rbac:groups=monitoring.coreos.com,resources=scrapeconfigs,verbs=get;list;watch

func (r *PrometheusScrapeConfigCrdReconciler) startWatchingScrapeConfigs(
	ctx context.Context,
	logger *logr.Logger,
) {
	if r.cancelControllerContext.Load() != nil {
		// We are already watching scrape config resources.
		return
	}

	logger.Info("Setting up a watch for Prometheus ScrapeConfig custom resources now.")
	scrapeConfigWatchingController, err :=
		controller.NewTypedUnmanaged[reconcile.Request]("dash0_scrapeconfig_controller", r.mgr, controller.Options{
			Reconciler: r,
		})
	if err != nil {
		logger.Error(err, "Cannot create a new controller to watch Prometheus ScrapeConfigs")
		return
	}

	if err = scrapeConfigWatchingController.Watch(
		source.TypedKind[*prometheusoperator.ScrapeConfig, reconcile.Request](
			r.mgr.GetCache(),
			&prometheusoperator.ScrapeConfig{},
			r.scrapeConfigReconciler,
		),
	); err != nil {
		logger.Error(err, "unable to create a new controller for watching Prometheus ScrapeConfigs")
		return
	}

	childContextForController, cancelControllerContext := context.WithCancel(ctx)
	r.cancelControllerContext.Store(&cancelControllerContext)

	go func() {
		if err = scrapeConfigWatchingController.Start(childContextForController); err != nil {
			logger.Error(err, "unable to start the controller for watching Prometheus ScrapeConfigs")
			return
		}
	}()
}

type PrometheusScrapeConfigReconciler struct {
	*backendconnection.BackendConnectionReconciler
}

func (r *PrometheusScrapeConfigReconciler) Create(
	ctx context.Context,
	e event.TypedCreateEvent[*prometheusoperator.ScrapeConfig],
	_ workqueue.TypedRateLimitingInterface[reconcile.Request],
) {
	logger := log.FromContext(ctx)
	logger.Info(
		"Detected a new Prometheus ScrapeConfig resource",
		"namespace",
		e.Object.GetNamespace(),
		"name",
		e.Object.GetName(),
	)
	r.BackendConnectionReconciler.UpdatePrometheusScrapeConfigs(ctx, e.Object)
}

func (r *PrometheusScrapeConfigReconciler) Update(
	ctx context.Context,
	e event.TypedUpdateEvent[*prometheusoperator.ScrapeConfig],
	_ workqueue.TypedRateLimitingInterface[reconcile.Request],
) {
	logger := log.FromContext(ctx)
	logger.Info(
		"Detected a change for a Prometheus ScrapeConfig resource",
		"namespace",
		e.ObjectNew.GetNamespace(),
		"name",
		e.ObjectNew.GetName(),
	)
	r.BackendConnectionReconciler.UpdatePrometheusScrapeConfigs(ctx, e.ObjectNew)
}

func (r *PrometheusScrapeConfigReconciler) Delete(
	ctx context.Context,
	e event.TypedDeleteEvent[*prometheusoperator.ScrapeConfig],
	_ workqueue.TypedRateLimitingInterface[reconcile.Request],
) {
	logger := log.FromContext(ctx)
	logger.Info(
		"Detected the deletion of a Prometheus ScrapeConfig resource",
		"namespace",
		e.Object.GetNamespace(),
		"name",
		e.Object.GetName(),
	)
	r.BackendConnectionReconciler.DeletePrometheusScrapeConfigs(ctx, e.Object)
}

func (r *PrometheusScrapeConfigReconciler) Generic(
	_ context.Context,
	_ event.TypedGenericEvent[*prometheusoperator.ScrapeConfig],
	_ workqueue.TypedRateLimitingInterface[reconcile.Request],
) {
	// ignoring generic events
}
