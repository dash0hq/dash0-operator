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
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/tools/pager"
	"sigs.k8s.io/controller-runtime/pkg/client"

	dash0v1beta1 "github.com/dash0hq/dash0-operator/api/operator/v1beta1"
	"github.com/dash0hq/dash0-operator/internal/util"
	"github.com/dash0hq/dash0-operator/internal/util/logd"
	"github.com/dash0hq/dash0-operator/internal/workloads"
)

type evictionResult int

const (
	podEvicted evictionResult = iota
	podGone
	podEvictionBlocked
	podEvictionFailed
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
EnqueueWorkloadsWithUninstrumentedPods is the pod-level counterpart to instrumentAllWorkloads. Instead of modifying the
pod templates of workloads, it finds running pods that are not instrumented or whose instrumentation is outdated, and
enqueues their workloads, so that the pods are evicted one at a time, see PodEvictionQueue. Their owner recreates them,
and the instrumentation webhook instruments the new pods when they are created.

The webhook only instruments pods while the monitoring resource is available, and it reads the monitoring resource from
the operator manager's cache. Pods evicted before that cache has caught up with the status update would be recreated
without instrumentation, hence this function waits for the cache first.

Only pods controlled by a daemon set, replica set or stateful set are evicted. Ownerless pods would not be recreated,
pods owned by jobs would count towards the job's backoff limit, and for other owners it is not known whether they
recreate an evicted pod. Pods whose controller does not exist anymore are not evicted either, since nothing would
recreate them. Pods in the operator namespace are never evicted, so the operator does not evict its own pods.
*/
func (q *PodEvictionQueue) EnqueueWorkloadsWithUninstrumentedPods(
	ctx context.Context,
	dash0MonitoringResource *dash0v1beta1.Dash0Monitoring,
	logger logd.Logger,
) error {
	namespace := dash0MonitoringResource.Namespace
	if namespace == q.clusterInstrumentationConfig.OperatorNamespace {
		logger.Info("Not evicting any pods in the operator namespace.")
		return nil
	}
	if !q.waitForMonitoringResourceToBecomeAvailableInCache(ctx, dash0MonitoringResource, logger) {
		return nil
	}

	namespaceInstrumentationConfig := dash0MonitoringResource.GetNamespaceInstrumentationConfig()
	dryRunModifier := newWorkloadModifier(q.clusterInstrumentationConfig, namespaceInstrumentationConfig, logd.Discard())
	resolveOwners := q.newCachingOwnerResolver(ctx)
	var keys []workloadKey
	enqueuedKeys := sets.New[workloadKey]()
	pgr := pager.New(pager.SimplePageFunc(
		func(opts metav1.ListOptions) (runtime.Object, error) {
			return q.clientset.CoreV1().
				Pods(namespace).
				List(ctx, opts)
		},
	))
	pgr.PageSize = apiListRequestPageSize
	err := pgr.EachListItem(ctx, metav1.ListOptions{},
		func(resource runtime.Object) error {
			pod := resource.(*corev1.Pod)
			podLogger := logger.WithValues(workloadNamespaceLabel, pod.Namespace, "pod name", pod.Name)
			owners, needsEviction := q.podNeedsEviction(
				pod,
				namespaceInstrumentationConfig,
				dryRunModifier,
				resolveOwners,
				podLogger,
			)
			if needsEviction {
				if key := newWorkloadKey(pod.Namespace, owners.TopLevel()); !enqueuedKeys.Has(key) {
					enqueuedKeys.Insert(key)
					keys = append(keys, key)
				}
			}
			return nil
		},
	)
	for _, key := range keys {
		q.queue.Add(key)
	}
	if len(keys) > 0 {
		logger.Info(
			fmt.Sprintf(
				"Workloads with uninstrumented pods: %d. Their pods will be evicted one pod per workload at a time, so "+
					"that they are recreated with instrumentation.",
				len(keys),
			))
	}
	return err
}

func (q *PodEvictionQueue) waitForMonitoringResourceToBecomeAvailableInCache(
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
			if err := q.client.Get(ctx, client.ObjectKeyFromObject(dash0MonitoringResource), cachedResource); err != nil {
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

// newCachingOwnerResolver returns a function that resolves the owners of a pod, and caches them per controller UID, so
// that the owners of the pods of one workload are only read once.
func (q *PodEvictionQueue) newCachingOwnerResolver(ctx context.Context) func(*corev1.Pod) (workloads.PodOwners, error) {
	podOwnerResolver := workloads.NewPodOwnerResolver(q.client, q.apiReader)
	ownersByController := make(map[types.UID]workloads.PodOwners)
	return func(pod *corev1.Pod) (workloads.PodOwners, error) {
		controllerUid := metav1.GetControllerOfNoCopy(pod).UID
		if owners, ok := ownersByController[controllerUid]; ok {
			return owners, nil
		}
		owners, err := podOwnerResolver.Resolve(ctx, pod)
		if err != nil {
			return nil, err
		}
		ownersByController[controllerUid] = owners
		return owners, nil
	}
}

// podNeedsEviction checks whether the pod is not instrumented or has outdated instrumentation, and whether evicting it
// would lead to an instrumented replacement. If so, it returns the owners of the pod and true.
func (q *PodEvictionQueue) podNeedsEviction(
	pod *corev1.Pod,
	namespaceInstrumentationConfig dash0v1beta1.NamespaceInstrumentationConfig,
	dryRunModifier *workloads.ResourceModifier,
	resolveOwners func(*corev1.Pod) (workloads.PodOwners, error),
	logger logd.Logger,
) (workloads.PodOwners, bool) {
	owner := metav1.GetControllerOf(pod)
	if owner == nil || !slices.ContainsFunc(evictablePodOwnerTypes, func(ownerType metav1.TypeMeta) bool {
		return owner.APIVersion == ownerType.APIVersion && owner.Kind == ownerType.Kind
	}) {
		return nil, false
	}
	if !isPodActive(pod) {
		return nil, false
	}
	if workloads.InstrumentationIsUpToDate(
		&pod.ObjectMeta,
		pod.Spec.Containers,
		q.clusterInstrumentationConfig,
		namespaceInstrumentationConfig,
	) {
		return nil, false
	}
	owners, err := resolveOwners(pod)
	if err != nil {
		logger.Info(
			"Not evicting the pod, its owners could not be read to check whether they have opted out of "+
				"instrumentation.",
			"error",
			err.Error(),
		)
		return nil, false
	}
	if owners.TopLevel() == nil {
		logger.Debug("not evicting the pod, its controller does not exist anymore, so it would not be recreated")
		return nil, false
	}
	if workloads.PodHasOptedOutOfInstrumentation(
		pod,
		owners,
		namespaceInstrumentationConfig.InstrumentationLabelSelector,
	) {
		return nil, false
	}
	if modificationResult := dryRunModifier.ModifyPod(pod.DeepCopy(), owners); !modificationResult.HasBeenModified {
		if modificationResult.RenderReasonMessage != nil {
			logger.Debug(
				"not evicting the pod, its recreated replacement would not be modified either",
				"reason",
				modificationResult.RenderReasonMessage(actor),
			)
		}
		return nil, false
	}
	return owners, true
}

func (q *PodEvictionQueue) evictPod(ctx context.Context, pod *corev1.Pod, logger logd.Logger) evictionResult {
	err := q.clientset.CoreV1().RESTClient().
		Post().
		Namespace(pod.Namespace).
		Resource("pods").
		Name(pod.Name).
		SubResource("eviction").
		// Pods(...).EvictV1 would honor the Retry-After header of a 429 response and retry up to ten times when a pod
		// disruption budget does not allow the eviction, blocking the worker. The queue retries with its own backoff.
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
		return podEvicted
	case apierrors.IsNotFound(err) || apierrors.IsConflict(err):
		return podGone
	case apierrors.IsTooManyRequests(err):
		logger.Info(
			"Not evicting the pod for instrumentation now, a pod disruption budget does not allow it. The eviction " +
				"will be retried later.")
		return podEvictionBlocked
	default:
		logger.ErrorAsWarnTelemetryCollectionIssue(err, "Failed to evict the pod for instrumentation.")
		return podEvictionFailed
	}
}

// isPodActive returns true if the pod is neither terminating nor has terminated.
func isPodActive(pod *corev1.Pod) bool {
	return pod.DeletionTimestamp == nil && pod.Status.Phase != corev1.PodSucceeded && pod.Status.Phase != corev1.PodFailed
}

func isPodReady(pod *corev1.Pod) bool {
	for _, condition := range pod.Status.Conditions {
		if condition.Type == corev1.PodReady {
			return condition.Status == corev1.ConditionTrue
		}
	}
	return false
}
