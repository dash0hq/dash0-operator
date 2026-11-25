// SPDX-FileCopyrightText: Copyright 2026 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package collectors

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/client-go/kubernetes/fake"
	k8stesting "k8s.io/client-go/testing"
)

const (
	namespace      = "dash0-system"
	gracePeriod    = 30 * time.Second
	updateInterval = 10 * time.Millisecond
)

var (
	discardLogger = slog.New(slog.DiscardHandler)
	now           = time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	perNodePool   = Pool{
		Name:     "per-node",
		Selector: &metav1.LabelSelector{MatchLabels: map[string]string{"component": "agent"}},
	}
	otherPool = Pool{
		Name:     "other",
		Selector: &metav1.LabelSelector{MatchLabels: map[string]string{"role": "scraper"}},
	}
)

type podOption func(*corev1.Pod)

func newPod(name string, podLabels map[string]string, options ...podOption) *corev1.Pod {
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:              name,
			Namespace:         namespace,
			Labels:            podLabels,
			CreationTimestamp: metav1.NewTime(now.Add(-time.Hour)),
		},
		Spec: corev1.PodSpec{NodeName: "node-" + name},
		Status: corev1.PodStatus{
			Phase:      corev1.PodRunning,
			StartTime:  &metav1.Time{Time: now.Add(-time.Hour)},
			Conditions: []corev1.PodCondition{{Type: corev1.PodReady, Status: corev1.ConditionTrue}},
		},
	}
	for _, option := range options {
		option(pod)
	}
	return pod
}

func notReadySince(d time.Duration) podOption {
	return func(pod *corev1.Pod) {
		pod.Status.Conditions = []corev1.PodCondition{{
			Type:               corev1.PodReady,
			Status:             corev1.ConditionFalse,
			LastTransitionTime: metav1.NewTime(now.Add(-d)),
		}}
	}
}

func pendingSince(d time.Duration) podOption {
	return func(pod *corev1.Pod) {
		pod.Status.Phase = corev1.PodPending
		pod.Status.StartTime = &metav1.Time{Time: now.Add(-d)}
		pod.Status.Conditions = nil
	}
}

var agentLabels = map[string]string{"component": "agent"}

func TestEligibility(t *testing.T) {
	deleting := metav1.NewTime(now)
	for _, tc := range []struct {
		name string
		pod  *corev1.Pod
		want bool
	}{
		{name: "running and ready", pod: newPod("a", agentLabels), want: true},
		{name: "not scheduled", pod: newPod("a", agentLabels, func(pod *corev1.Pod) { pod.Spec.NodeName = "" })},
		{
			name: "being deleted while ready",
			pod:  newPod("a", agentLabels, func(pod *corev1.Pod) { pod.DeletionTimestamp = &deleting }),
		},
		{name: "not ready within the grace period", pod: newPod("a", agentLabels, notReadySince(10*time.Second)), want: true},
		{name: "not ready beyond the grace period", pod: newPod("a", agentLabels, notReadySince(time.Minute))},
		{name: "pending within the grace period", pod: newPod("a", agentLabels, pendingSince(10*time.Second)), want: true},
		{name: "pending beyond the grace period", pod: newPod("a", agentLabels, pendingSince(time.Minute))},
	} {
		t.Run(tc.name, func(t *testing.T) {
			w := NewWatcher(discardLogger, nil, namespace, nil, gracePeriod, updateInterval)
			assert.Equal(t, tc.want, w.isEligible(tc.pod, now))
		})
	}
}

type publications struct {
	mu     sync.Mutex
	latest []*Collector
	count  int
}

func (p *publications) publish(collectors []*Collector) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.latest = collectors
	p.count++
}

func (p *publications) get() ([]*Collector, int) {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.latest, p.count
}

func (p *publications) names() []string {
	latest, _ := p.get()
	result := make([]string, 0, len(latest))
	for _, c := range latest {
		result = append(result, c.Pool+"/"+c.Name)
	}
	return result
}

// newClientset returns a fake clientset whose pod watches report the events of a label-selected watch of the API
// server: a pod that starts to match the selector is added, and a pod that stops to match it is deleted.
func newClientset(objects ...runtime.Object) *fake.Clientset {
	client := fake.NewClientset(objects...)
	client.PrependWatchReactor("pods", func(action k8stesting.Action) (bool, watch.Interface, error) {
		restrictions := action.(k8stesting.WatchAction).GetWatchRestrictions()
		w, err := client.Tracker().Watch(action.GetResource(), action.GetNamespace(),
			metav1.ListOptions{ResourceVersion: restrictions.ResourceVersion})
		if err != nil {
			return false, nil, err
		}
		matching := map[string]bool{}
		return true, watch.Filter(w, func(event watch.Event) (watch.Event, bool) {
			pod := event.Object.(*corev1.Pod)
			matched := matching[pod.Name]
			matches := event.Type != watch.Deleted && restrictions.Labels.Matches(labels.Set(pod.Labels))
			matching[pod.Name] = matches
			switch {
			case matches && !matched:
				event.Type = watch.Added
			case !matches && matched:
				event.Type = watch.Deleted
			case !matches:
				return event, false
			}
			return event, true
		}), nil
	})
	return client
}

func startWatcher(t *testing.T, client *fake.Clientset, pools ...Pool) *publications {
	t.Helper()
	ctx, cancel := context.WithCancel(t.Context())
	w := NewWatcher(discardLogger, client, namespace, pools, gracePeriod, updateInterval)
	w.now = func() time.Time { return now }
	p := &publications{}
	done := make(chan error, 1)
	go func() { done <- w.Run(ctx, p.publish) }()
	t.Cleanup(func() {
		cancel()
		require.NoError(t, <-done)
	})
	return p
}

func TestPublishesOnceWithoutPods(t *testing.T) {
	p := startWatcher(t, newClientset(), perNodePool)
	require.Eventually(t, func() bool {
		_, count := p.get()
		return count == 1
	}, 5*time.Second, 10*time.Millisecond)
	latest, _ := p.get()
	assert.Empty(t, latest)
}

func TestCollectorsPerPool(t *testing.T) {
	client := newClientset(
		newPod("agent-1", agentLabels),
		newPod("scraper-1", map[string]string{"role": "scraper"}),
		newPod("both", map[string]string{"component": "agent", "role": "scraper"}),
		newPod("unrelated", map[string]string{"app": "other"}),
	)
	p := startWatcher(t, client, perNodePool, otherPool)
	require.Eventually(t, func() bool {
		return assert.ObjectsAreEqual([]string{"other/scraper-1", "per-node/agent-1", "per-node/both"}, p.names())
	}, 5*time.Second, 10*time.Millisecond, "a pod in both pools belongs to the first one")

	latest, _ := p.get()
	assert.Equal(t, &Collector{
		Name:              "agent-1",
		NodeName:          "node-agent-1",
		Pool:              "per-node",
		CreationTimestamp: now.Add(-time.Hour),
	}, latest[1])
}

func TestPublishesOnlyAfterAllPoolsHaveSynced(t *testing.T) {
	client := newClientset(
		newPod("agent-1", agentLabels),
		newPod("scraper-1", map[string]string{"role": "scraper"}),
	)
	var otherPoolListable atomic.Bool
	client.PrependReactor("list", "pods", func(action k8stesting.Action) (bool, runtime.Object, error) {
		restrictions := action.(k8stesting.ListAction).GetListRestrictions()
		if restrictions.Labels.String() == "role=scraper" && !otherPoolListable.Load() {
			return true, nil, errors.New("the pods of the other pool cannot be listed yet")
		}
		return false, nil, nil
	})
	p := startWatcher(t, client, perNodePool, otherPool)

	time.Sleep(20 * updateInterval)
	_, count := p.get()
	assert.Zero(t, count, "no publication while the other pool has not synced")

	otherPoolListable.Store(true)
	require.Eventually(t, func() bool {
		return assert.ObjectsAreEqual([]string{"other/scraper-1", "per-node/agent-1"}, p.names())
	}, 5*time.Second, 10*time.Millisecond)
}

func TestPodChanges(t *testing.T) {
	client := newClientset(newPod("agent-1", agentLabels))
	p := startWatcher(t, client, perNodePool, otherPool)
	require.Eventually(t, func() bool {
		return assert.ObjectsAreEqual([]string{"per-node/agent-1"}, p.names())
	}, 5*time.Second, 10*time.Millisecond)

	pods := client.CoreV1().Pods(namespace)
	_, err := pods.Create(t.Context(), newPod("agent-2", agentLabels), metav1.CreateOptions{})
	require.NoError(t, err)
	require.Eventually(t, func() bool {
		return assert.ObjectsAreEqual([]string{"per-node/agent-1", "per-node/agent-2"}, p.names())
	}, 5*time.Second, 10*time.Millisecond, "a new pod is added")

	_, err = pods.Update(t.Context(), newPod("agent-2", map[string]string{"role": "scraper"}), metav1.UpdateOptions{})
	require.NoError(t, err)
	require.Eventually(t, func() bool {
		return assert.ObjectsAreEqual([]string{"other/agent-2", "per-node/agent-1"}, p.names())
	}, 5*time.Second, 10*time.Millisecond, "a label change moves the pod to the other pool")

	_, err = pods.Update(t.Context(), newPod("agent-1", agentLabels, notReadySince(time.Minute)), metav1.UpdateOptions{})
	require.NoError(t, err)
	require.Eventually(t, func() bool {
		return assert.ObjectsAreEqual([]string{"other/agent-2"}, p.names())
	}, 5*time.Second, 10*time.Millisecond, "a pod that is not ready beyond the grace period is removed")

	require.NoError(t, pods.Delete(t.Context(), "agent-2", metav1.DeleteOptions{}))
	require.Eventually(t, func() bool {
		return assert.ObjectsAreEqual([]string{}, p.names())
	}, 5*time.Second, 10*time.Millisecond, "a deleted pod is removed")
}

func TestTransformKeepsTheFieldsOfTheEligibilityCheck(t *testing.T) {
	deleting := metav1.NewTime(now)
	pod := newPod("agent-1", agentLabels, notReadySince(time.Second), func(pod *corev1.Pod) {
		pod.DeletionTimestamp = &deleting
		pod.Annotations = map[string]string{"large": "annotation"}
		pod.Spec.Containers = []corev1.Container{{Name: "collector"}}
	})

	transformed, err := keepEligibilityFields(pod)
	require.NoError(t, err)
	stripped := transformed.(*corev1.Pod)
	assert.Empty(t, stripped.Annotations)
	assert.Empty(t, stripped.Labels)
	assert.Empty(t, stripped.Spec.Containers)
	assert.Equal(t, pod.Name, stripped.Name)
	assert.Equal(t, pod.CreationTimestamp, stripped.CreationTimestamp)
	assert.Equal(t, pod.DeletionTimestamp, stripped.DeletionTimestamp)
	assert.Equal(t, pod.Spec.NodeName, stripped.Spec.NodeName)
	assert.Equal(t, pod.Status.Phase, stripped.Status.Phase)
	assert.Equal(t, pod.Status.StartTime, stripped.Status.StartTime)
	assert.Equal(t, pod.Status.Conditions, stripped.Status.Conditions)
}
