// SPDX-FileCopyrightText: Copyright 2026 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package profiling

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"time"

	"github.com/cisco-open/k8s-objectmatcher/patch"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/utils/ptr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/cache"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/apiutil"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	"sigs.k8s.io/controller-runtime/pkg/source"

	dash0v1alpha1 "github.com/dash0hq/dash0-operator/api/operator/v1alpha1"
	"github.com/dash0hq/dash0-operator/internal/util/logd"
	"github.com/dash0hq/dash0-operator/internal/util/resources"
)

// Reconciler manages the privileged profiler independently of the collector and the unprivileged proxy.
type Reconciler struct {
	client.Client
	Scheme             *runtime.Scheme
	OperatorDeployment *appsv1.Deployment
	Config             Config
}

// SetupWithManager watches configuration, managed resources and changes to referenced authorization Secrets.
func (r *Reconciler) SetupWithManager(mgr ctrl.Manager) error {
	operandCache, err := cache.New(mgr.GetConfig(), cache.Options{
		Scheme: mgr.GetScheme(), Mapper: mgr.GetRESTMapper(),
		DefaultNamespaces: map[string]cache.Config{r.Config.Namespace: {}},
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
	managed := predicate.NewPredicateFuncs(func(object client.Object) bool {
		return object.GetNamespace() == r.Config.Namespace &&
			object.GetLabels()[instanceLabel] == r.Config.NamePrefix &&
			(object.GetLabels()[componentLabel] == profilerComponent || object.GetLabels()[componentLabel] == proxyComponent)
	})
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
	if object.GetNamespace() != r.Config.Namespace {
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

// Reconcile creates, updates or removes profiler resources, including when the configuration is deleted.
func (r *Reconciler) Reconcile(ctx context.Context, _ ctrl.Request) (ctrl.Result, error) {
	configuration, err := resources.FindOperatorConfigurationResource(ctx, r.Client, logd.FromContext(ctx))
	if err != nil {
		return ctrl.Result{}, err
	}
	enabled := r.Config.Enabled && r.Config.TelemetryCollectionEnabled && configuration != nil &&
		ptr.Deref(configuration.Spec.TelemetryCollection.Enabled, true) && len(configuration.EffectiveExports()) > 0 &&
		configuration.Spec.Profiling != nil && ptr.Deref(configuration.Spec.Profiling.Enabled, false) &&
		configuration.Spec.Profiling.EbpfProfiler != nil && ptr.Deref(configuration.Spec.Profiling.EbpfProfiler.Enabled, false)
	if !enabled {
		return r.remove(ctx, false)
	}
	proxyConfig := proxyConfiguration{}
	profiler := configuration.Spec.Profiling.EbpfProfiler
	if profiler.Debuginfo == nil || ptr.Deref(profiler.Debuginfo.UploadEnabled, true) {
		proxyConfig, err = buildProxyConfiguration(configuration)
		if err != nil {
			return ctrl.Result{}, err
		}
	}
	uploads := proxyConfig.yaml != ""
	if err := r.Config.validateImages(uploads); err != nil {
		return ctrl.Result{}, err
	}
	if uploads {
		revision, err := r.proxyRevision(ctx, proxyConfig)
		if err != nil {
			return ctrl.Result{}, err
		}
		for _, object := range r.Config.proxy(configuration, proxyConfig, revision) {
			if err := r.apply(ctx, configuration, object); err != nil {
				return ctrl.Result{}, err
			}
		}
	}
	for _, object := range r.Config.profiler(configuration, uploads) {
		if err := r.apply(ctx, configuration, object); err != nil {
			return ctrl.Result{}, err
		}
	}
	if !uploads {
		return r.remove(ctx, true)
	}
	return ctrl.Result{}, nil
}

func (r *Reconciler) proxyRevision(ctx context.Context, config proxyConfiguration) (string, error) {
	hash := sha256.New()
	_, _ = hash.Write([]byte(config.yaml))
	env, err := json.Marshal(config.env)
	if err != nil {
		return "", err
	}
	_, _ = hash.Write(env)
	for _, env := range config.env {
		if env.ValueFrom == nil || env.ValueFrom.SecretKeyRef == nil {
			continue
		}
		ref := env.ValueFrom.SecretKeyRef
		secret := &corev1.Secret{}
		err := r.Get(ctx, client.ObjectKey{Namespace: r.Config.Namespace, Name: ref.Name}, secret)
		if apierrors.IsNotFound(err) {
			_, _ = fmt.Fprintf(hash, "%s:missing", ref.Name)
			continue
		}
		if err != nil {
			return "", err
		}
		_, _ = fmt.Fprintf(hash, "%s:%s:%s", ref.Name, secret.UID, secret.ResourceVersion)
	}
	return fmt.Sprintf("%x", hash.Sum(nil)), nil
}

func (r *Reconciler) apply(ctx context.Context, configuration *dash0v1alpha1.Dash0OperatorConfiguration, desired client.Object) error {
	logger := logd.FromContext(ctx)
	gvk, err := apiutil.GVKForObject(desired, r.Scheme)
	if err != nil {
		return err
	}
	desired.GetObjectKind().SetGroupVersionKind(gvk)
	if err := resources.SetOwnerReference(r.OperatorDeployment, configuration, r.Scheme, desired, logger); err != nil {
		return err
	}
	existing := desired.DeepCopyObject().(client.Object)
	if err := r.Get(ctx, client.ObjectKeyFromObject(desired), existing); err != nil {
		if !apierrors.IsNotFound(err) {
			return err
		}
		if err := patch.DefaultAnnotator.SetLastAppliedAnnotation(desired); err != nil {
			return err
		}
		return r.Create(ctx, desired)
	}
	_, err = resources.UpdateWithRetryOnConflict(ctx, r.Client, desired, func(existing, desired client.Object) (bool, error) {
		if existing.GetLabels()[instanceLabel] != r.Config.NamePrefix || existing.GetLabels()[componentLabel] != desired.GetLabels()[componentLabel] {
			return false, fmt.Errorf("refusing to replace unmanaged profiling resource %s", existing.GetName())
		}
		result, err := patch.DefaultPatchMaker.Calculate(existing, desired, patch.IgnoreField("kind"), patch.IgnoreField("apiVersion"))
		if err != nil || result.IsEmpty() {
			return false, err
		}
		if err := patch.DefaultAnnotator.SetLastAppliedAnnotation(desired); err != nil {
			return false, err
		}
		resources.PreserveKubectlRestartedAtAnnotation(existing, desired, logger)
		desired.SetResourceVersion(existing.GetResourceVersion())
		if service, ok := desired.(*corev1.Service); ok {
			live := existing.(*corev1.Service)
			service.Spec.ClusterIP, service.Spec.ClusterIPs = live.Spec.ClusterIP, live.Spec.ClusterIPs
		}
		return true, r.Update(ctx, desired)
	})
	return err
}

func (r *Reconciler) remove(ctx context.Context, proxyOnly bool) (ctrl.Result, error) {
	objects := []client.Object{
		&appsv1.DaemonSet{ObjectMeta: r.Config.metadata(proxyComponent)},
		&corev1.Service{ObjectMeta: r.Config.metadata(proxyComponent)},
		&corev1.ConfigMap{ObjectMeta: r.Config.metadata(proxyComponent)},
		&networkingv1.NetworkPolicy{ObjectMeta: r.Config.metadata(proxyComponent)},
		&corev1.ServiceAccount{ObjectMeta: r.Config.metadata(proxyComponent)},
	}
	if !proxyOnly {
		objects = append([]client.Object{&appsv1.DaemonSet{ObjectMeta: r.Config.metadata(profilerComponent)},
			&corev1.ServiceAccount{ObjectMeta: r.Config.metadata(profilerComponent)}}, objects...)
	}
	for _, object := range objects {
		component := object.GetLabels()[componentLabel]
		_, daemonSet := object.(*appsv1.DaemonSet)
		err := r.Get(ctx, client.ObjectKeyFromObject(object), object)
		if err != nil && !apierrors.IsNotFound(err) {
			return ctrl.Result{}, err
		}
		if err == nil {
			if object.GetLabels()[instanceLabel] != r.Config.NamePrefix || object.GetLabels()[componentLabel] != component {
				continue
			}
			propagation := metav1.DeletePropagationBackground
			if daemonSet {
				propagation = metav1.DeletePropagationForeground
			}
			if err := r.Delete(ctx, object, client.Preconditions{UID: ptr.To(object.GetUID())}, client.PropagationPolicy(propagation)); err != nil && !apierrors.IsNotFound(err) {
				return ctrl.Result{}, err
			}
		}
		if daemonSet {
			pods := &corev1.PodList{}
			if err := r.List(ctx, pods, client.InNamespace(r.Config.Namespace), client.MatchingLabels(r.Config.labels(component))); err != nil {
				return ctrl.Result{}, err
			}
			if len(pods.Items) > 0 {
				return ctrl.Result{RequeueAfter: time.Second}, nil
			}
			if err := r.Get(ctx, client.ObjectKeyFromObject(object), object); !apierrors.IsNotFound(err) {
				if err != nil {
					return ctrl.Result{}, err
				}
				return ctrl.Result{RequeueAfter: time.Second}, nil
			}
		}
	}
	return ctrl.Result{}, nil
}
