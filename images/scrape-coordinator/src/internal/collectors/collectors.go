// SPDX-FileCopyrightText: Copyright 2026 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

// Package collectors watches the collector pods of each pool and reports the ones that can receive targets.
package collectors

import (
	"cmp"
	"context"
	"errors"
	"log/slog"
	"slices"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/cache"

	"github.com/dash0hq/dash0-operator/images/scrape-coordinator/internal/debounce"
)

const (
	// DefaultUpdateInterval is the minimum time between two updates of the collectors.
	DefaultUpdateInterval = 5 * time.Second

	// resyncPeriod also re-evaluates pods that stay not ready, so that they lose their targets once the grace period
	// has expired.
	resyncPeriod = 30 * time.Second
)

// Pool is a named set of collector pods.
type Pool struct {
	Name     string
	Selector *metav1.LabelSelector
}

// Collector is a collector pod that can receive targets.
type Collector struct {
	Name              string
	NodeName          string
	Pool              string
	CreationTimestamp time.Time
}

// Watcher reports the collector pods of all pools that can receive targets.
type Watcher struct {
	logger         *slog.Logger
	client         kubernetes.Interface
	namespace      string
	pools          []Pool
	gracePeriod    time.Duration
	updateInterval time.Duration
	now            func() time.Time
}

// NewWatcher creates a Watcher for the pools in the namespace. A pod that matches the selectors of several pools
// belongs to the first of them. A pod that is not running or not ready for longer than the grace period does not
// receive targets.
func NewWatcher(
	logger *slog.Logger,
	client kubernetes.Interface,
	namespace string,
	pools []Pool,
	gracePeriod time.Duration,
	updateInterval time.Duration,
) *Watcher {
	return &Watcher{
		logger:         logger,
		client:         client,
		namespace:      namespace,
		pools:          pools,
		gracePeriod:    gracePeriod,
		updateInterval: updateInterval,
		now:            time.Now,
	}
}

type poolPods struct {
	pool  Pool
	store cache.Store
}

// Run watches the collector pods until ctx is cancelled. Once the pods of all pools are known, it publishes the
// collectors, also if there are none, and afterwards after every change, at most once per update interval.
func (w *Watcher) Run(ctx context.Context, publish func([]*Collector)) error {
	trigger := debounce.New(w.updateInterval)
	handler := cache.ResourceEventHandlerFuncs{
		AddFunc:    func(any) { trigger.Notify() },
		UpdateFunc: func(any, any) { trigger.Notify() },
		DeleteFunc: func(any) { trigger.Notify() },
	}

	allPoolPods := make([]poolPods, 0, len(w.pools))
	synced := make([]cache.InformerSynced, 0, len(w.pools))
	for _, pool := range w.pools {
		selector, err := metav1.LabelSelectorAsSelector(pool.Selector)
		if err != nil {
			return err
		}
		factory := informers.NewSharedInformerFactoryWithOptions(
			w.client,
			resyncPeriod,
			informers.WithNamespace(w.namespace),
			informers.WithTweakListOptions(func(options *metav1.ListOptions) {
				options.LabelSelector = selector.String()
			}),
		)
		informer := factory.Core().V1().Pods().Informer()
		if err := informer.SetTransform(keepEligibilityFields); err != nil {
			return err
		}
		if _, err := informer.AddEventHandler(handler); err != nil {
			return err
		}
		factory.Start(ctx.Done())
		allPoolPods = append(allPoolPods, poolPods{pool: pool, store: informer.GetStore()})
		synced = append(synced, informer.HasSynced)
	}

	if !cache.WaitForNamedCacheSync("collector pods", ctx.Done(), synced...) {
		if ctx.Err() != nil {
			return nil
		}
		return errors.New("the collector pod informers did not sync")
	}
	publish(w.collectors(allPoolPods))
	trigger.Run(ctx, func() { publish(w.collectors(allPoolPods)) })
	return nil
}

// collectors returns the eligible collector pods of all pools, sorted by pool and name.
func (w *Watcher) collectors(allPoolPods []poolPods) []*Collector {
	now := w.now()
	poolOfPod := map[string]string{}
	var result []*Collector
	for _, pp := range allPoolPods {
		for _, obj := range pp.store.List() {
			pod := obj.(*corev1.Pod)
			if !w.isEligible(pod, now) {
				continue
			}
			if pool, ok := poolOfPod[pod.Name]; ok {
				w.logger.Debug("the collector pod matches the selectors of several pools, it belongs to the first one",
					"pod", pod.Name, "pool", pool, "ignoredPool", pp.pool.Name)
				continue
			}
			poolOfPod[pod.Name] = pp.pool.Name
			result = append(result, &Collector{
				Name:              pod.Name,
				NodeName:          pod.Spec.NodeName,
				Pool:              pp.pool.Name,
				CreationTimestamp: pod.CreationTimestamp.Time,
			})
		}
	}
	slices.SortFunc(result, func(a, b *Collector) int {
		return cmp.Or(cmp.Compare(a.Pool, b.Pool), cmp.Compare(a.Name, b.Name))
	})
	return result
}

// isEligible returns true if the pod can receive targets. A pod that is being deleted is never eligible, even while it
// still reports ready: the kubelet of an unreachable node never confirms the deletion.
func (w *Watcher) isEligible(pod *corev1.Pod, now time.Time) bool {
	if pod.Spec.NodeName == "" || pod.DeletionTimestamp != nil {
		return false
	}
	if pod.Status.Phase != corev1.PodRunning &&
		pod.Status.StartTime != nil &&
		now.Sub(pod.Status.StartTime.Time) > w.gracePeriod {
		return false
	}
	for _, condition := range pod.Status.Conditions {
		if condition.Type == corev1.PodReady &&
			condition.Status != corev1.ConditionTrue &&
			now.Sub(condition.LastTransitionTime.Time) > w.gracePeriod {
			return false
		}
	}
	return true
}

// keepEligibilityFields reduces a pod to the fields read by isEligible and collectors.
func keepEligibilityFields(obj any) (any, error) {
	pod, ok := obj.(*corev1.Pod)
	if !ok {
		return obj, nil
	}
	return &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:              pod.Name,
			Namespace:         pod.Namespace,
			ResourceVersion:   pod.ResourceVersion,
			CreationTimestamp: pod.CreationTimestamp,
			DeletionTimestamp: pod.DeletionTimestamp,
		},
		Spec: corev1.PodSpec{NodeName: pod.Spec.NodeName},
		Status: corev1.PodStatus{
			Phase:      pod.Status.Phase,
			StartTime:  pod.Status.StartTime,
			Conditions: pod.Status.Conditions,
		},
	}, nil
}
