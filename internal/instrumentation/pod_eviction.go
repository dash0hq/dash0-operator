// SPDX-FileCopyrightText: Copyright 2026 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package instrumentation

import (
	"context"
	"fmt"
	"slices"
	"time"

	corev1 "k8s.io/api/core/v1"
	policyv1 "k8s.io/api/policy/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/tools/pager"
	"sigs.k8s.io/controller-runtime/pkg/client"

	dash0v1beta1 "github.com/dash0hq/dash0-operator/api/operator/v1beta1"
	"github.com/dash0hq/dash0-operator/internal/util"
	"github.com/dash0hq/dash0-operator/internal/util/logd"
	"github.com/dash0hq/dash0-operator/internal/workloads"
)

var (
	evictablePodOwnerTypes = []metav1.TypeMeta{
		util.K8sTypeMetaDaemonSet,
		util.K8sTypeMetaReplicaSet,
		util.K8sTypeMetaStatefulSet,
	}

	waitForMonitoringResourceInCachePollInterval = 100 * time.Millisecond
	waitForMonitoringResourceInCacheTimeout      = 10 * time.Second
)

/*
evictUninstrumentedPods is the pod-level counterpart to instrumentAllWorkloads. Instead of modifying the pod templates
of workloads, it evicts running pods that are not instrumented or whose instrumentation is outdated. Their owner
recreates them, and the instrumentation webhook instruments the new pods when they are created.

The webhook only instruments pods while the monitoring resource is available, and it reads the monitoring resource from
the operator manager's cache. Pods evicted before that cache has caught up with the status update would be recreated
without instrumentation, hence this function waits for the cache first.

Only pods controlled by a daemon set, replica set or stateful set are evicted. Ownerless pods would not be recreated,
pods owned by jobs would count towards the job's backoff limit, and for other owners it is not known whether they
recreate an evicted pod. Pods in the operator namespace are never evicted, so the operator does not evict its own pods.
Evictions go through the eviction API, so they respect pod disruption budgets.
*/
func (i *Instrumenter) evictUninstrumentedPods(
	ctx context.Context,
	dash0MonitoringResource *dash0v1beta1.Dash0Monitoring,
	logger logd.Logger,
) error {
	namespace := dash0MonitoringResource.Namespace
	if namespace == i.ClusterInstrumentationConfig.OperatorNamespace {
		logger.Info("Not evicting any pods in the operator namespace.")
		return nil
	}
	if !i.waitForMonitoringResourceToBecomeAvailableInCache(ctx, dash0MonitoringResource, logger) {
		return nil
	}

	namespaceInstrumentationConfig := dash0MonitoringResource.GetNamespaceInstrumentationConfig()
	dryRunModifier := newWorkloadModifier(i.ClusterInstrumentationConfig, namespaceInstrumentationConfig, logd.Discard())
	pgr := pager.New(pager.SimplePageFunc(
		func(opts metav1.ListOptions) (runtime.Object, error) {
			return i.Clientset.CoreV1().
				Pods(namespace).
				List(ctx, opts)
		},
	))
	pgr.PageSize = apiListRequestPageSize
	return pgr.EachListItem(ctx, metav1.ListOptions{},
		func(resource runtime.Object) error {
			pod := resource.(*corev1.Pod)
			podLogger := logger.WithValues(workloadNamespaceLabel, pod.Namespace, "pod name", pod.Name)
			if i.podNeedsEviction(pod, namespaceInstrumentationConfig, dryRunModifier, podLogger) &&
				i.evictPod(ctx, pod, podLogger) {
				i.pauseAfterEachWorkload()
			}
			return nil
		},
	)
}

func (i *Instrumenter) waitForMonitoringResourceToBecomeAvailableInCache(
	ctx context.Context,
	dash0MonitoringResource *dash0v1beta1.Dash0Monitoring,
	logger logd.Logger,
) bool {
	if err := wait.PollUntilContextTimeout(
		ctx,
		waitForMonitoringResourceInCachePollInterval,
		waitForMonitoringResourceInCacheTimeout,
		true,
		func(ctx context.Context) (bool, error) {
			cachedResource := &dash0v1beta1.Dash0Monitoring{}
			if err := i.Get(ctx, client.ObjectKeyFromObject(dash0MonitoringResource), cachedResource); err != nil {
				return false, nil
			}
			return cachedResource.IsAvailable() && !cachedResource.IsMarkedForDeletion(), nil
		},
	); err != nil {
		logger.Warn(
			fmt.Sprintf(
				"The Dash0 monitoring resource has not become available within %s, uninstrumented pods will not be "+
					"evicted. They will be instrumented when they are recreated.",
				waitForMonitoringResourceInCacheTimeout,
			))
		return false
	}
	return true
}

func (i *Instrumenter) podNeedsEviction(
	pod *corev1.Pod,
	namespaceInstrumentationConfig dash0v1beta1.NamespaceInstrumentationConfig,
	dryRunModifier *workloads.ResourceModifier,
	logger logd.Logger,
) bool {
	owner := metav1.GetControllerOf(pod)
	if owner == nil || !slices.ContainsFunc(evictablePodOwnerTypes, func(ownerType metav1.TypeMeta) bool {
		return owner.APIVersion == ownerType.APIVersion && owner.Kind == ownerType.Kind
	}) {
		return false
	}
	if pod.DeletionTimestamp != nil || pod.Status.Phase == corev1.PodSucceeded || pod.Status.Phase == corev1.PodFailed {
		return false
	}
	if util.HasOptedOutOfInstrumentation(&pod.ObjectMeta, namespaceInstrumentationConfig.InstrumentationLabelSelector) {
		return false
	}
	if workloads.InstrumentationIsUpToDate(
		&pod.ObjectMeta,
		pod.Spec.Containers,
		i.ClusterInstrumentationConfig,
		namespaceInstrumentationConfig,
	) {
		return false
	}
	if modificationResult := dryRunModifier.ModifyPod(pod.DeepCopy()); !modificationResult.HasBeenModified {
		if modificationResult.RenderReasonMessage != nil {
			logger.Debug(
				"not evicting the pod, its recreated replacement would not be modified either",
				"reason",
				modificationResult.RenderReasonMessage(actor),
			)
		}
		return false
	}
	return true
}

func (i *Instrumenter) evictPod(ctx context.Context, pod *corev1.Pod, logger logd.Logger) bool {
	err := i.Clientset.CoreV1().RESTClient().
		Post().
		Namespace(pod.Namespace).
		Resource("pods").
		Name(pod.Name).
		SubResource("eviction").
		// Pods(...).EvictV1 would honor the Retry-After header of a 429 response and retry up to ten times when a pod
		// disruption budget does not allow the eviction, blocking the sweep for minutes.
		MaxRetries(0).
		Body(&policyv1.Eviction{
			ObjectMeta: metav1.ObjectMeta{
				Name:      pod.Name,
				Namespace: pod.Namespace,
			},
			DeleteOptions: &metav1.DeleteOptions{
				Preconditions: &metav1.Preconditions{UID: &pod.UID},
			},
		}).
		Do(ctx).
		Error()
	switch {
	case err == nil:
		owner := metav1.GetControllerOf(pod)
		logger.Info(
			fmt.Sprintf(
				"Evicted the pod owned by %s %s, its replacement will be instrumented when it is created.",
				owner.Kind,
				owner.Name,
			))
		return true
	case apierrors.IsNotFound(err) || apierrors.IsConflict(err):
		return false
	case apierrors.IsTooManyRequests(err):
		logger.Info(
			"Not evicting the pod for instrumentation now, a pod disruption budget does not allow it. It will be " +
				"instrumented when it is recreated.")
		return false
	default:
		logger.ErrorAsWarnTelemetryCollectionIssue(err, "Failed to evict the pod for instrumentation.")
		return false
	}
}
