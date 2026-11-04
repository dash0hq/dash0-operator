// SPDX-FileCopyrightText: Copyright 2026 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package instrumentation

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"slices"
	"sync"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/events"
	"k8s.io/client-go/tools/pager"
	"k8s.io/client-go/util/workqueue"
	"sigs.k8s.io/controller-runtime/pkg/client"

	dash0common "github.com/dash0hq/dash0-operator/api/operator/common"
	dash0v1beta1 "github.com/dash0hq/dash0-operator/api/operator/v1beta1"
	"github.com/dash0hq/dash0-operator/internal/util"
	"github.com/dash0hq/dash0-operator/internal/util/logd"
	"github.com/dash0hq/dash0-operator/internal/workloads"
)

const podEvictionQueueName = "dash0-pod-eviction-queue"

var (
	podEvictionWorkers            = 10
	podEvictionPollInterval       = 5 * time.Second
	podEvictionSettleTimeout      = 10 * time.Minute
	podEvictionMinReadyDuration   = 30 * time.Second
	podEvictionBlockedBaseDelay   = 5 * time.Second
	podEvictionBlockedMaxDelay    = time.Minute
	podEvictionBlockedGiveUpAfter = 30 * time.Minute
)

// workloadKey identifies the top-level owner of evictable pods, for example the deployment of pods controlled by a
// replica set.
type workloadKey struct {
	namespace string
	kind      string
	name      string
	uid       types.UID
}

func newWorkloadKey(namespace string, topLevelOwner *metav1.PartialObjectMetadata) workloadKey {
	return workloadKey{
		namespace: namespace,
		kind:      topLevelOwner.Kind,
		name:      topLevelOwner.Name,
		uid:       topLevelOwner.UID,
	}
}

type inspectionOutcome int

const (
	workloadNotSettled inspectionOutcome = iota
	workloadSettled
	stopEvictingWorkload
)

type workloadInspection struct {
	outcome inspectionOutcome
	// notSettledReason describes why the next pod cannot be evicted yet, if the outcome is workloadNotSettled.
	notSettledReason string
	// afterLastEviction is true if the outcome is workloadNotSettled and no pod needs to be evicted anymore, that is,
	// the workload has not settled after its last pod has been evicted.
	afterLastEviction bool
	owner             client.Object
	podUids           sets.Set[types.UID]
	// nextPod is the pod to evict next if the outcome is workloadSettled, or nil if no pod needs to be evicted anymore.
	nextPod *corev1.Pod
}

/*
PodEvictionQueue evicts the uninstrumented pods of workloads for pod-level instrumentation, see
EnqueueWorkloadsWithUninstrumentedPods. It is a leader-only runnable, so evictions stop when the operator manager shuts
down or loses leadership.

Each item in the queue is a workload, that is, the top-level owner of the pods. A fixed number of workers process
workloads concurrently, which limits the number of workloads that are disrupted at the same time, as well as the load
on the API server. The pods of one workload are evicted one at a time. Before each eviction, a worker waits until the
workload has settled:
  - no pod of the workload is terminating,
  - the workload has at least as many active pods as it desires, so a replacement that is created only after the
    evicted pod is gone (daemon sets and stateful sets) exists, and
  - all pods of the workload that are not going to be evicted have been ready for at least the minReadySeconds of the
    workload, or podEvictionMinReadyDuration if that is longer, in particular the instrumented replacement of the
    previously evicted pod. Like Kubernetes does for available pods, the time since when a pod has been ready is taken
    from its ready condition, so a replacement that crashes shortly after becoming ready does not pass this check.

This check does not depend on what has happened before, so it also holds when the workload is enqueued again while its
pods are being evicted, or after the operator manager has restarted. After the last pod of the workload has been
evicted, the worker waits for the workload to settle in the same way before it moves on to the next workload.

If the workload does not settle within podEvictionSettleTimeout, or if a pod that has been created after the previous
eviction has not been instrumented, has restarted, or has a terminated container or a failed init container, the worker
stops evicting the pods of that workload and records an event for it, so that instrumentation that breaks the workload
cannot take down all of its replicas. Pods that are not evicted are instrumented the next time they are recreated.
Restarts of pods that have already existed before the previous eviction do not stop the evictions.

If a pod disruption budget does not allow an eviction, the workload is requeued with an exponential backoff, which
frees the worker for other workloads. After podEvictionBlockedGiveUpAfter, the worker gives up and records an event.

Before each step, the worker checks whether the Dash0 monitoring resource in the namespace is still available and has
instrumentWorkloads.mode=all, and stops otherwise.
*/
type PodEvictionQueue struct {
	client                       client.Client
	apiReader                    client.Reader
	clientset                    *kubernetes.Clientset
	recorder                     events.EventRecorder
	clusterInstrumentationConfig *util.ClusterInstrumentationConfig
	queue                        workqueue.TypedRateLimitingInterface[workloadKey]
	blockedSince                 map[workloadKey]time.Time
	blockedSinceLock             sync.Mutex
}

// NewPodEvictionQueue creates a PodEvictionQueue. The client is used to read the Dash0 monitoring resources and the
// owners of pods from the cache, the apiReader to read the top-level owners of pods without the cache.
func NewPodEvictionQueue(
	client client.Client,
	apiReader client.Reader,
	clientset *kubernetes.Clientset,
	recorder events.EventRecorder,
	clusterInstrumentationConfig *util.ClusterInstrumentationConfig,
) *PodEvictionQueue {
	return &PodEvictionQueue{
		client:                       client,
		apiReader:                    apiReader,
		clientset:                    clientset,
		recorder:                     recorder,
		clusterInstrumentationConfig: clusterInstrumentationConfig,
		queue: workqueue.NewTypedRateLimitingQueueWithConfig(
			workqueue.NewTypedItemExponentialFailureRateLimiter[workloadKey](
				podEvictionBlockedBaseDelay,
				podEvictionBlockedMaxDelay,
			),
			workqueue.TypedRateLimitingQueueConfig[workloadKey]{Name: podEvictionQueueName},
		),
		blockedSince: make(map[workloadKey]time.Time),
	}
}

// NeedLeaderElection implements the LeaderElectionRunnable interface, pods are only evicted by the leader.
func (q *PodEvictionQueue) NeedLeaderElection() bool {
	return true
}

// Start starts the workers and blocks until the context is cancelled. Workloads that have been enqueued before Start is
// called are processed once the workers are running.
func (q *PodEvictionQueue) Start(ctx context.Context) error {
	logger := logd.FromContext(ctx)
	var workers sync.WaitGroup
	for range podEvictionWorkers {
		workers.Go(func() {
			for {
				if !q.processNextWorkload(ctx, logger) {
					return
				}
			}
		})
	}
	<-ctx.Done()
	q.queue.ShutDown()
	workers.Wait()
	return nil
}

func (q *PodEvictionQueue) processNextWorkload(ctx context.Context, logger logd.Logger) bool {
	key, shutdown := q.queue.Get()
	if shutdown {
		return false
	}
	defer q.queue.Done(key)
	workloadLogger := logger.WithValues(
		workloadNamespaceLabel, key.namespace,
		workkloadTypeLabel, key.kind,
		workloadNameLabel, key.name,
	)
	if q.evictPodsOfWorkload(ctx, key, workloadLogger) {
		q.queue.AddRateLimited(key)
	} else {
		q.queue.Forget(key)
		q.clearBlockedSince(key)
	}
	return true
}

// evictPodsOfWorkload evicts the pods of the workload that need to be evicted, one at a time. It returns true if the
// workload needs to be requeued, since a pod disruption budget does not allow the next eviction.
func (q *PodEvictionQueue) evictPodsOfWorkload(ctx context.Context, key workloadKey, logger logd.Logger) bool {
	var podsBeforeEviction sets.Set[types.UID]
	for {
		inspection, ok := q.waitUntilNextPodCanBeEvicted(ctx, key, podsBeforeEviction, logger)
		if !ok || inspection.nextPod == nil {
			return false
		}
		podLogger := logger.WithValues("pod name", inspection.nextPod.Name)
		switch q.evictPod(ctx, inspection.nextPod, podLogger) {
		case podEvicted:
			q.queue.Forget(key)
			q.clearBlockedSince(key)
			podsBeforeEviction = inspection.podUids
			q.pauseAfterEachEviction(ctx)
		case podGone:
			continue
		case podEvictionBlocked:
			return q.retryBlockedEvictionLater(key, inspection.owner, inspection.nextPod, podLogger)
		default:
			return false
		}
	}
}

// waitUntilNextPodCanBeEvicted polls the workload until it has settled, and returns the pod to evict next. It returns
// false if the pods of the workload should not be evicted any further.
func (q *PodEvictionQueue) waitUntilNextPodCanBeEvicted(
	ctx context.Context,
	key workloadKey,
	podsBeforeEviction sets.Set[types.UID],
	logger logd.Logger,
) (*workloadInspection, bool) {
	var inspection *workloadInspection
	var owner client.Object
	err := wait.PollUntilContextTimeout(
		ctx,
		podEvictionPollInterval,
		podEvictionSettleTimeout,
		true,
		// The inspection uses ctx instead of the poll's context, so that the timeout does not cut requests short and
		// replace the reason why the workload has not settled with a deadline error.
		func(context.Context) (bool, error) {
			inspection = q.inspectWorkload(ctx, key, podsBeforeEviction, logger)
			if inspection.owner != nil {
				owner = inspection.owner
			}
			return inspection.outcome != workloadNotSettled, nil
		},
	)
	if err != nil {
		if ctx.Err() != nil || inspection == nil {
			return nil, false
		}
		reason := fmt.Sprintf(
			"the workload has not become ready for evicting the next pod within %s (%s)",
			podEvictionSettleTimeout,
			inspection.notSettledReason,
		)
		if inspection.afterLastEviction {
			reason = fmt.Sprintf(
				"the workload has not become ready within %s after evicting its last pod (%s)",
				podEvictionSettleTimeout,
				inspection.notSettledReason,
			)
		}
		if owner != nil {
			q.stopEvictingWorkload(owner, reason, logger)
		} else {
			logger.Warn(fmt.Sprintf("Not evicting further pods of the workload, %s.", reason))
		}
		return nil, false
	}
	return inspection, inspection.outcome == workloadSettled
}

func (q *PodEvictionQueue) inspectWorkload(
	ctx context.Context,
	key workloadKey,
	podsBeforeEviction sets.Set[types.UID],
	logger logd.Logger,
) *workloadInspection {
	monitoringResource, err := q.readMonitoringResourceInNamespace(ctx, key.namespace)
	if err != nil {
		return notSettled(nil, fmt.Sprintf("cannot read the Dash0 monitoring resource: %v", err))
	}
	if monitoringResource == nil ||
		!monitoringResource.IsAvailable() ||
		monitoringResource.IsMarkedForDeletion() ||
		monitoringResource.ReadInstrumentWorkloadsMode() != dash0common.InstrumentWorkloadsModeAll {
		logger.Info(
			"Not evicting further pods of the workload, the Dash0 monitoring resource in the namespace has been " +
				"removed or no longer instruments existing workloads.")
		return &workloadInspection{outcome: stopEvictingWorkload}
	}

	owner, err := q.readWorkload(ctx, key)
	if apierrors.IsNotFound(err) {
		logger.Info("Not evicting further pods of the workload, it does not exist anymore.")
		return &workloadInspection{outcome: stopEvictingWorkload}
	} else if err != nil {
		return notSettled(nil, fmt.Sprintf("cannot read the workload: %v", err))
	}
	desiredPods, selector := desiredPodsAndSelector(owner)
	resolveOwners := q.newCachingOwnerResolver(ctx)
	pods, err := q.listPodsOfWorkload(ctx, key, selector, resolveOwners)
	if err != nil {
		return notSettled(owner, fmt.Sprintf("cannot list the pods of the workload: %v", err))
	}

	namespaceInstrumentationConfig := monitoringResource.GetNamespaceInstrumentationConfig()
	dryRunModifier := newWorkloadModifier(q.clusterInstrumentationConfig, namespaceInstrumentationConfig, logd.Discard())
	podUids := sets.New[types.UID]()
	var activePods, podsToEvict []*corev1.Pod
	var terminatingPod *corev1.Pod
	for _, pod := range pods {
		podUids.Insert(pod.UID)
		if pod.DeletionTimestamp != nil {
			terminatingPod = pod
		}
		if !isPodActive(pod) {
			continue
		}
		activePods = append(activePods, pod)
		if isReplacement(pod, podsBeforeEviction) {
			if failure := replacementFailure(pod); failure != "" {
				q.stopEvictingWorkload(
					owner,
					fmt.Sprintf(
						"the pod %s, which has been created after the previous eviction, %s",
						pod.Name,
						failure,
					),
					logger,
				)
				return &workloadInspection{outcome: stopEvictingWorkload}
			}
		}
		podLogger := logger.WithValues("pod name", pod.Name)
		if _, needsEviction := q.podNeedsEviction(
			pod,
			namespaceInstrumentationConfig,
			dryRunModifier,
			resolveOwners,
			podLogger,
		); needsEviction {
			if isReplacement(pod, podsBeforeEviction) {
				q.stopEvictingWorkload(
					owner,
					fmt.Sprintf(
						"the pod %s, which has been created after the previous eviction, %s",
						pod.Name,
						q.uninstrumentedReplacementDescription(pod),
					),
					logger,
				)
				return &workloadInspection{outcome: stopEvictingWorkload}
			}
			podsToEvict = append(podsToEvict, pod)
		}
	}

	if len(podsToEvict) == 0 && podsBeforeEviction == nil {
		return &workloadInspection{outcome: workloadSettled, owner: owner, podUids: podUids}
	}
	if reason := notSettledReason(
		owner,
		desiredPods,
		activePods,
		podsToEvict,
		terminatingPod,
		podsBeforeEviction,
	); reason != "" {
		inspection := notSettled(owner, reason)
		inspection.afterLastEviction = len(podsToEvict) == 0
		return inspection
	}
	if len(podsToEvict) == 0 {
		return &workloadInspection{outcome: workloadSettled, owner: owner, podUids: podUids}
	}
	slices.SortFunc(podsToEvict, func(a, b *corev1.Pod) int {
		if aReady, bReady := isPodReady(a), isPodReady(b); aReady != bReady {
			if aReady {
				return 1
			}
			return -1
		}
		return cmp.Compare(a.Name, b.Name)
	})
	return &workloadInspection{outcome: workloadSettled, owner: owner, podUids: podUids, nextPod: podsToEvict[0]}
}

func notSettled(owner client.Object, reason string) *workloadInspection {
	return &workloadInspection{outcome: workloadNotSettled, owner: owner, notSettledReason: reason}
}

// notSettledReason returns why the workload has not settled yet, or an empty string if it has settled.
func notSettledReason(
	workload client.Object,
	desiredPods int32,
	activePods []*corev1.Pod,
	podsToEvict []*corev1.Pod,
	terminatingPod *corev1.Pod,
	podsBeforeEviction sets.Set[types.UID],
) string {
	if terminatingPod != nil {
		return fmt.Sprintf("the pod %s is terminating", terminatingPod.Name)
	}
	if len(activePods) < int(desiredPods) {
		return fmt.Sprintf("%d of %d pods exist", len(activePods), desiredPods)
	}
	minReadyDuration := minReadyDurationOf(workload)
	now := time.Now()
	for _, pod := range activePods {
		if slices.Contains(podsToEvict, pod) || podIsStablyReady(pod, minReadyDuration, now) {
			continue
		}
		description := "the pod " + pod.Name
		if isReplacement(pod, podsBeforeEviction) {
			description = "the replacement " + pod.Name
		}
		if !isPodReady(pod) {
			return description + " is not ready"
		}
		return fmt.Sprintf("%s has not been ready for %s yet", description, minReadyDuration)
	}
	return ""
}

// isReplacement returns true if the pod has been created after the previous eviction.
func isReplacement(pod *corev1.Pod, podsBeforeEviction sets.Set[types.UID]) bool {
	return podsBeforeEviction != nil && !podsBeforeEviction.Has(pod.UID)
}

// uninstrumentedReplacementDescription describes the instrumentation state of a pod that has been created after the
// previous eviction, but still needs to be evicted.
func (q *PodEvictionQueue) uninstrumentedReplacementDescription(pod *corev1.Pod) string {
	switch {
	case !util.HasBeenInstrumentedSuccessfully(&pod.ObjectMeta):
		return "has not been instrumented"
	case !util.HasBeenInstrumentedSuccessfullyByThisVersion(&pod.ObjectMeta, q.clusterInstrumentationConfig.Images):
		return "has been instrumented by another version of the operator"
	default:
		return "has been instrumented with outdated settings"
	}
}

// readMonitoringResourceInNamespace returns the Dash0 monitoring resource in the namespace, or nil if there is none.
// Like the instrumentation webhook, it only takes the first one into account.
func (q *PodEvictionQueue) readMonitoringResourceInNamespace(
	ctx context.Context,
	namespace string,
) (*dash0v1beta1.Dash0Monitoring, error) {
	monitoringResources := &dash0v1beta1.Dash0MonitoringList{}
	if err := q.client.List(ctx, monitoringResources, client.InNamespace(namespace)); err != nil {
		return nil, err
	}
	if len(monitoringResources.Items) == 0 {
		return nil, nil
	}
	return &monitoringResources.Items[0], nil
}

// readWorkload reads the top-level owner of the pods, and returns a not found error if it has been replaced by another
// object with the same name.
func (q *PodEvictionQueue) readWorkload(ctx context.Context, key workloadKey) (client.Object, error) {
	var workload client.Object
	switch key.kind {
	case util.K8sTypeMetaDaemonSet.Kind:
		workload = &appsv1.DaemonSet{}
	case util.K8sTypeMetaDeployment.Kind:
		workload = &appsv1.Deployment{}
	case util.K8sTypeMetaReplicaSet.Kind:
		workload = &appsv1.ReplicaSet{}
	case util.K8sTypeMetaStatefulSet.Kind:
		workload = &appsv1.StatefulSet{}
	default:
		return nil, apierrors.NewNotFound(appsv1.Resource(key.kind), key.name)
	}
	if err := q.apiReader.Get(ctx, client.ObjectKey{Namespace: key.namespace, Name: key.name}, workload); err != nil {
		return nil, err
	}
	if workload.GetUID() != key.uid {
		return nil, apierrors.NewNotFound(appsv1.Resource(key.kind), key.name)
	}
	return workload, nil
}

// desiredPodsAndSelector returns the number of pods the workload should have and its pod selector.
func desiredPodsAndSelector(workload client.Object) (int32, *metav1.LabelSelector) {
	switch w := workload.(type) {
	case *appsv1.DaemonSet:
		return w.Status.DesiredNumberScheduled, w.Spec.Selector
	case *appsv1.Deployment:
		return replicasOrDefault(w.Spec.Replicas), w.Spec.Selector
	case *appsv1.ReplicaSet:
		return replicasOrDefault(w.Spec.Replicas), w.Spec.Selector
	case *appsv1.StatefulSet:
		return replicasOrDefault(w.Spec.Replicas), w.Spec.Selector
	default:
		return 0, nil
	}
}

func replicasOrDefault(replicas *int32) int32 {
	if replicas == nil {
		return 1
	}
	return *replicas
}

// minReadyDurationOf returns how long the pods of the workload need to have been ready before the next pod is evicted,
// that is, the minReadySeconds of the workload, but at least podEvictionMinReadyDuration.
func minReadyDurationOf(workload client.Object) time.Duration {
	var minReadySeconds int32
	switch w := workload.(type) {
	case *appsv1.DaemonSet:
		minReadySeconds = w.Spec.MinReadySeconds
	case *appsv1.Deployment:
		minReadySeconds = w.Spec.MinReadySeconds
	case *appsv1.ReplicaSet:
		minReadySeconds = w.Spec.MinReadySeconds
	case *appsv1.StatefulSet:
		minReadySeconds = w.Spec.MinReadySeconds
	}
	return max(podEvictionMinReadyDuration, time.Duration(minReadySeconds)*time.Second)
}

// listPodsOfWorkload lists the pods matching the selector of the workload whose top-level owner is the workload.
func (q *PodEvictionQueue) listPodsOfWorkload(
	ctx context.Context,
	key workloadKey,
	selector *metav1.LabelSelector,
	resolveOwners func(*corev1.Pod) (workloads.PodOwners, error),
) ([]*corev1.Pod, error) {
	labelSelector, err := metav1.LabelSelectorAsSelector(selector)
	if err != nil {
		return nil, err
	}
	var pods []*corev1.Pod
	var ownerErrors []error
	pgr := pager.New(pager.SimplePageFunc(
		func(opts metav1.ListOptions) (runtime.Object, error) {
			return q.clientset.CoreV1().
				Pods(key.namespace).
				List(ctx, opts)
		},
	))
	pgr.PageSize = apiListRequestPageSize
	if err = pgr.EachListItem(ctx, metav1.ListOptions{LabelSelector: labelSelector.String()},
		func(resource runtime.Object) error {
			pod := resource.(*corev1.Pod)
			controller := metav1.GetControllerOfNoCopy(pod)
			if controller == nil {
				return nil
			}
			if controller.UID == key.uid {
				pods = append(pods, pod)
				return nil
			}
			if key.kind != util.K8sTypeMetaDeployment.Kind || controller.Kind != util.K8sTypeMetaReplicaSet.Kind {
				return nil
			}
			owners, err := resolveOwners(pod)
			if err != nil {
				ownerErrors = append(ownerErrors, err)
				return nil
			}
			if topLevel := owners.TopLevel(); topLevel != nil && topLevel.UID == key.uid {
				pods = append(pods, pod)
			}
			return nil
		},
	); err != nil {
		return nil, err
	}
	return pods, errors.Join(ownerErrors...)
}

// retryBlockedEvictionLater returns true if the eviction of the workload's pods should be retried later, and false if
// it has been blocked for too long.
func (q *PodEvictionQueue) retryBlockedEvictionLater(
	key workloadKey,
	owner client.Object,
	pod *corev1.Pod,
	logger logd.Logger,
) bool {
	q.blockedSinceLock.Lock()
	blockedSince, found := q.blockedSince[key]
	if !found {
		q.blockedSince[key] = time.Now()
	}
	q.blockedSinceLock.Unlock()
	if !found || time.Since(blockedSince) < podEvictionBlockedGiveUpAfter {
		return true
	}
	q.stopEvictingWorkload(
		owner,
		fmt.Sprintf(
			"a pod disruption budget has not allowed evicting the pod %s for %s",
			pod.Name,
			podEvictionBlockedGiveUpAfter,
		),
		logger,
	)
	return false
}

func (q *PodEvictionQueue) clearBlockedSince(key workloadKey) {
	q.blockedSinceLock.Lock()
	delete(q.blockedSince, key)
	q.blockedSinceLock.Unlock()
}

func (q *PodEvictionQueue) stopEvictingWorkload(owner client.Object, reason string, logger logd.Logger) {
	message := fmt.Sprintf(
		"Dash0 has stopped evicting the pods of this workload for instrumentation: %s. Pods that have not been "+
			"evicted will be instrumented when they are recreated.",
		reason,
	)
	logger.Warn(message)
	util.QueuePodEvictionStoppedEvent(q.recorder, owner, message)
}

func (q *PodEvictionQueue) pauseAfterEachEviction(ctx context.Context) {
	delays := q.clusterInstrumentationConfig.InstrumentationDelays
	if delays == nil || delays.AfterEachWorkloadMillis == 0 {
		return
	}
	select {
	case <-ctx.Done():
	case <-time.After(time.Duration(delays.AfterEachWorkloadMillis) * time.Millisecond):
	}
}
