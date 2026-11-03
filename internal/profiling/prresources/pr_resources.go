// SPDX-FileCopyrightText: Copyright 2026 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package prresources

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"

	"github.com/cisco-open/k8s-objectmatcher/patch"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/apiutil"

	dash0v1alpha1 "github.com/dash0hq/dash0-operator/api/operator/v1alpha1"
	"github.com/dash0hq/dash0-operator/internal/util/logd"
	"github.com/dash0hq/dash0-operator/internal/util/resources"
)

// Changes describes the resource operations performed during a reconciliation.
type Changes struct {
	Created         bool
	Updated         bool
	Deleted         bool
	PendingDeletion bool
}

// ResourceManager assembles and applies the profiler and debuginfo proxy resources.
type ResourceManager struct {
	client.Client
	Scheme             *runtime.Scheme
	OperatorDeployment *appsv1.Deployment
	Config             Config
}

// NewResourceManager creates a manager for the installation's profiling resources.
func NewResourceManager(c client.Client, scheme *runtime.Scheme, deployment *appsv1.Deployment, config Config) *ResourceManager {
	return &ResourceManager{Client: c, Scheme: scheme, OperatorDeployment: deployment, Config: config}
}

// Manages reports whether an object belongs to this installation's profiler or proxy.
func (m *ResourceManager) Manages(object client.Object) bool {
	return object.GetNamespace() == m.Config.Namespace && object.GetLabels()[instanceLabel] == m.Config.NamePrefix &&
		(object.GetLabels()[componentLabel] == profilerComponent || object.GetLabels()[componentLabel] == proxyComponent)
}

// CreateOrUpdateResources applies desired profiling resources and removes an unused proxy.
func (m *ResourceManager) CreateOrUpdateResources(ctx context.Context, configuration *dash0v1alpha1.Dash0OperatorConfiguration) (Changes, error) {
	changes := Changes{}
	proxyConfig := proxyConfiguration{}
	profiler := configuration.Spec.Profiling.EbpfProfiler
	var err error
	if profiler.Debuginfo == nil || ptr.Deref(profiler.Debuginfo.UploadEnabled, true) {
		proxyConfig, err = buildProxyConfiguration(ctx, configuration)
		if err != nil {
			return changes, err
		}
	}
	uploads := proxyConfig.yaml != ""
	if err := m.Config.validateImages(uploads); err != nil {
		return changes, err
	}
	var desired []client.Object
	if uploads {
		revision, err := m.proxyRevision(ctx, proxyConfig)
		if err != nil {
			return changes, err
		}
		desired = m.Config.proxy(configuration, proxyConfig, revision)
	}
	desired = append(desired, m.Config.profiler(configuration, uploads)...)
	for _, object := range desired {
		created, updated, err := m.apply(ctx, configuration, object)
		changes.Created = changes.Created || created
		changes.Updated = changes.Updated || updated
		if err != nil {
			return changes, err
		}
	}
	if !uploads {
		removed, err := m.DeleteResources(ctx, true)
		changes.Deleted, changes.PendingDeletion = removed.Deleted, removed.PendingDeletion
		return changes, err
	}
	return changes, nil
}

func (m *ResourceManager) proxyRevision(ctx context.Context, config proxyConfiguration) (string, error) {
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
		err := m.Get(ctx, client.ObjectKey{Namespace: m.Config.Namespace, Name: ref.Name}, secret)
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

func (m *ResourceManager) apply(ctx context.Context, configuration *dash0v1alpha1.Dash0OperatorConfiguration, desired client.Object) (bool, bool, error) {
	logger := logd.FromContext(ctx)
	gvk, err := apiutil.GVKForObject(desired, m.Scheme)
	if err != nil {
		return false, false, err
	}
	desired.GetObjectKind().SetGroupVersionKind(gvk)
	if err := resources.SetOwnerReference(m.OperatorDeployment, configuration, m.Scheme, desired, logger); err != nil {
		return false, false, err
	}
	existing := desired.DeepCopyObject().(client.Object)
	if err := m.Get(ctx, client.ObjectKeyFromObject(desired), existing); err != nil {
		if !apierrors.IsNotFound(err) {
			return false, false, err
		}
		if err := patch.DefaultAnnotator.SetLastAppliedAnnotation(desired); err != nil {
			return false, false, err
		}
		err = m.Create(ctx, desired)
		return err == nil, false, err
	}
	updated, err := resources.UpdateWithRetryOnConflict(ctx, m.Client, desired, func(existing, desired client.Object) (bool, error) {
		if existing.GetLabels()[instanceLabel] != m.Config.NamePrefix || existing.GetLabels()[componentLabel] != desired.GetLabels()[componentLabel] {
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
		return true, m.Update(ctx, desired)
	})
	return false, updated, err
}

// DeleteResources removes managed resources, retaining proxy dependencies until its pods have drained.
func (m *ResourceManager) DeleteResources(ctx context.Context, proxyOnly bool) (Changes, error) {
	changes := Changes{}
	objects := []client.Object{
		&appsv1.DaemonSet{ObjectMeta: m.Config.metadata(proxyComponent)},
		&corev1.Service{ObjectMeta: m.Config.metadata(proxyComponent)},
		&corev1.ConfigMap{ObjectMeta: m.Config.metadata(proxyComponent)},
		&networkingv1.NetworkPolicy{ObjectMeta: m.Config.metadata(proxyComponent)},
		&corev1.ServiceAccount{ObjectMeta: m.Config.metadata(proxyComponent)},
	}
	if !proxyOnly {
		objects = append([]client.Object{&appsv1.DaemonSet{ObjectMeta: m.Config.metadata(profilerComponent)},
			&corev1.ServiceAccount{ObjectMeta: m.Config.metadata(profilerComponent)}}, objects...)
	}
	for _, object := range objects {
		component := object.GetLabels()[componentLabel]
		_, daemonSet := object.(*appsv1.DaemonSet)
		drainProxy := daemonSet && component == proxyComponent
		err := m.Get(ctx, client.ObjectKeyFromObject(object), object)
		if err != nil && !apierrors.IsNotFound(err) {
			return changes, err
		}
		if err == nil {
			if object.GetLabels()[instanceLabel] != m.Config.NamePrefix || object.GetLabels()[componentLabel] != component {
				continue
			}
			propagation := metav1.DeletePropagationBackground
			if drainProxy {
				propagation = metav1.DeletePropagationForeground
			}
			err := m.Delete(ctx, object, client.Preconditions{UID: ptr.To(object.GetUID())}, client.PropagationPolicy(propagation))
			if err != nil && !apierrors.IsNotFound(err) {
				return changes, err
			}
			changes.Deleted = changes.Deleted || err == nil
		}
		if drainProxy {
			pods := &corev1.PodList{}
			if err := m.List(ctx, pods, client.InNamespace(m.Config.Namespace), client.MatchingLabels(m.Config.labels(component))); err != nil {
				return changes, err
			}
			if len(pods.Items) > 0 {
				changes.PendingDeletion = true
				return changes, nil
			}
			if err := m.Get(ctx, client.ObjectKeyFromObject(object), object); !apierrors.IsNotFound(err) {
				if err != nil {
					return changes, err
				}
				changes.PendingDeletion = true
				return changes, nil
			}
		}
	}
	return changes, nil
}
