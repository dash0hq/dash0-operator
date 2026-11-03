// SPDX-FileCopyrightText: Copyright 2026 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package profiling

import (
	"context"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/cache"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	"sigs.k8s.io/controller-runtime/pkg/source"

	dash0v1alpha1 "github.com/dash0hq/dash0-operator/api/operator/v1alpha1"
	"github.com/dash0hq/dash0-operator/internal/util/logd"
	"github.com/dash0hq/dash0-operator/internal/util/resources"
)

// Reconciler watches profiling configuration, operands and referenced authorization Secrets.
type Reconciler struct {
	client.Client
	Manager *Manager
}

// NewReconciler creates the controller for profiling resource watch events.
func NewReconciler(c client.Client, manager *Manager) *Reconciler {
	return &Reconciler{Client: c, Manager: manager}
}

// SetupWithManager registers namespaced operand watches and a cluster-wide configuration watch.
func (r *Reconciler) SetupWithManager(mgr ctrl.Manager) error {
	operandCache, err := cache.New(mgr.GetConfig(), cache.Options{
		Scheme: mgr.GetScheme(), Mapper: mgr.GetRESTMapper(),
		DefaultNamespaces: map[string]cache.Config{r.Manager.ResourceManager.Config.Namespace: {}},
	})
	if err != nil {
		return err
	}
	if err := mgr.Add(operandCache); err != nil {
		return err
	}
	r.Client, err = client.New(mgr.GetConfig(), client.Options{Scheme: mgr.GetScheme(), Mapper: mgr.GetRESTMapper()})
	if err != nil {
		return err
	}
	r.Manager.ResourceManager.Client = r.Client
	managed := predicate.NewPredicateFuncs(r.Manager.ResourceManager.Manages)
	controller := ctrl.NewControllerManagedBy(mgr).Named("ebpfprofilercontroller").
		For(&dash0v1alpha1.Dash0OperatorConfiguration{}, builder.WithPredicates(predicate.GenerationChangedPredicate{}))
	for _, object := range []client.Object{&appsv1.DaemonSet{}, &corev1.ServiceAccount{},
		&corev1.ConfigMap{}, &corev1.Service{}, &networkingv1.NetworkPolicy{}} {
		controller = controller.WatchesRawSource(source.Kind(operandCache, object, &handler.EnqueueRequestForObject{}, managed))
	}
	return controller.WatchesRawSource(source.Kind(operandCache, client.Object(&corev1.Secret{}),
		handler.EnqueueRequestsFromMapFunc(r.secretRequests))).Complete(r)
}

func (r *Reconciler) secretRequests(ctx context.Context, object client.Object) []reconcile.Request {
	if object.GetNamespace() != r.Manager.ResourceManager.Config.Namespace {
		return nil
	}
	configuration, err := resources.FindOperatorConfigurationResource(ctx, r.Client, logd.FromContext(ctx))
	if err != nil || configuration == nil {
		return nil
	}
	for _, export := range configuration.EffectiveExports() {
		if export.Dash0 != nil && export.Dash0.Authorization.SecretRef != nil &&
			export.Dash0.Authorization.SecretRef.Name == object.GetName() {
			return []reconcile.Request{{NamespacedName: client.ObjectKeyFromObject(configuration)}}
		}
	}
	return nil
}

// Reconcile delegates watch events to the profiling lifecycle manager.
func (r *Reconciler) Reconcile(ctx context.Context, _ ctrl.Request) (ctrl.Result, error) {
	return r.Manager.Reconcile(ctx)
}
