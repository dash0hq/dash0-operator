// SPDX-FileCopyrightText: Copyright 2026 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package startup

import (
	"context"
	"time"

	"github.com/go-logr/logr"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/tools/pager"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"

	dash0v1alpha1 "github.com/dash0hq/dash0-operator/api/operator/v1alpha1"
	dash0v1beta1 "github.com/dash0hq/dash0-operator/api/operator/v1beta1"
)

const (
	logInterval                     = 24 * time.Hour
	monitoringResourcesListPageSize = 50
)

// LogConfigurationResourcesRunnable is a repeating task that lists all Dash0OperatorConfiguration and Dash0Monitoring
// resources in the cluster and logs each one as an event via LogResourceAsEvent. It runs every 24 hours, with the first
// run 24 hours after startup. We also log them at the end of each reconcile request. This additional logging makes sure
// we have the resource content available in the self-monitoring telemetry, even if some resources have not changed
// and have not reconciled for a long time.
type LogConfigurationResourcesRunnable struct {
	client client.Client
}

func NewLogConfigurationResourcesRunnable(client client.Client) *LogConfigurationResourcesRunnable {
	return &LogConfigurationResourcesRunnable{
		client: client,
	}
}

// NeedLeaderElection implements the LeaderElectionRunnable interface, which indicates that the
// LogConfigurationResourcesRunnable requires leader election.
func (r *LogConfigurationResourcesRunnable) NeedLeaderElection() bool {
	return true
}

func (r *LogConfigurationResourcesRunnable) Start(ctx context.Context) error {
	ticker := time.NewTicker(logInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			r.logAllResources(ctx)
		case <-ctx.Done():
			log.FromContext(ctx).Info("Stopping LogConfigurationResourcesRunnable")
			return nil
		}
	}
}

func (r *LogConfigurationResourcesRunnable) logAllResources(ctx context.Context) {
	logger := log.FromContext(ctx)
	r.logOperatorConfigurationResources(ctx, logger)
	r.logMonitoringResources(ctx, logger)
}

func (r *LogConfigurationResourcesRunnable) logOperatorConfigurationResources(ctx context.Context, logger logr.Logger) {
	allOperatorConfigurationResources := &dash0v1alpha1.Dash0OperatorConfigurationList{}
	if err := r.client.List(ctx, allOperatorConfigurationResources); err != nil {
		logger.Error(err, "failed to list Dash0OperatorConfiguration resources for logging")
		return
	}
	for i := range allOperatorConfigurationResources.Items {
		allOperatorConfigurationResources.Items[i].LogResourceAsEvent(logger)
	}
}

func (r *LogConfigurationResourcesRunnable) logMonitoringResources(ctx context.Context, logger logr.Logger) {
	pgr := pager.New(pager.SimplePageFunc(
		func(opts metav1.ListOptions) (runtime.Object, error) {
			list := &dash0v1beta1.Dash0MonitoringList{}
			if err := r.client.List(ctx, list, &client.ListOptions{
				Limit:    opts.Limit,
				Continue: opts.Continue,
			}); err != nil {
				return nil, err
			}
			return list, nil
		},
	))
	pgr.PageSize = monitoringResourcesListPageSize
	if err := pgr.EachListItem(ctx, metav1.ListOptions{},
		func(resource runtime.Object) error {
			resource.(*dash0v1beta1.Dash0Monitoring).LogResourceAsEvent(logger)
			return nil
		},
	); err != nil {
		logger.Error(err, "failed to list Dash0Monitoring resources for logging")
	}
}
