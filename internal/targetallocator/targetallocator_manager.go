// SPDX-FileCopyrightText: Copyright 2025 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package targetallocator

import (
	"context"
	"fmt"
	"reflect"
	"sync/atomic"

	"k8s.io/client-go/kubernetes"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/dash0hq/dash0-operator/internal/targetallocator/taresources"
	"github.com/dash0hq/dash0-operator/internal/util"
	"github.com/dash0hq/dash0-operator/internal/util/logd"
	"github.com/dash0hq/dash0-operator/internal/util/pointers"
	"github.com/dash0hq/dash0-operator/internal/util/resources"
)

type TargetAllocatorManager struct {
	client.Client
	clientset                      *kubernetes.Clientset
	targetAllocatorResourceManager *taresources.TargetAllocatorResourceManager
	extraConfig                    atomic.Pointer[util.ExtraConfig]
	developmentMode                bool
	reconcileGuard                 util.ReconcileGuard
}

type TargetAllocatorReconcileTrigger string

const (
	TriggeredByWatchEvent             TargetAllocatorReconcileTrigger = "watch"
	TriggeredByDash0ResourceReconcile TargetAllocatorReconcileTrigger = "resource"
)

func NewTargetAllocatorManager(
	k8sClient client.Client,
	clientset *kubernetes.Clientset,
	extraConfig util.ExtraConfig,
	developmentMode bool,
	targetAllocatorResourceManager *taresources.TargetAllocatorResourceManager,
) *TargetAllocatorManager {
	m := &TargetAllocatorManager{
		Client:                         k8sClient,
		clientset:                      clientset,
		developmentMode:                developmentMode,
		targetAllocatorResourceManager: targetAllocatorResourceManager,
	}
	m.extraConfig.Store(&extraConfig)
	return m
}

func (m *TargetAllocatorManager) UpdateExtraConfig(ctx context.Context, newConfig util.ExtraConfig, logger logd.Logger) {
	previousConfig := m.extraConfig.Swap(&newConfig)
	if previousConfig == nil || !reflect.DeepEqual(*previousConfig, newConfig) {
		hasBeenReconciled, err := m.ReconcileTargetAllocator(ctx, TriggeredByWatchEvent)
		if err != nil {
			logger.ErrorTelemetryCollectionIssue(err, "Failed to create/update target-allocator resources after extra config map update.")
		}
		if hasBeenReconciled {
			logger.Info("successfully reconciled target-allocator resources after extra config map update")
		}
	} else {
		logger.Info("ignoring extra config map update, both the new and the old extra config map have the same content")
	}
}

// ReconcileTargetAllocator can be triggered by a
//  1. a reconcile request from the Dash0OperatorConfiguration resource.
//  2. a reconcile request from a Dash0Monitoring resource in the cluster.
//  3. a change event on one of the target-allocator related resources that the operator manages
//
// Returns a boolean flag indicating whether the reconciliation has been performed (true) or has been cancelled, due
// to another reconciliation already being in progress or because the resource has been deleted by the operator.
// A return value of (true, nil) does not necessarily indicate that any target-allocator resource has been created,
// updated, or deleted; it only indicates that the reconciliation has been performed.
//
// A request that arrives while a reconciliation is in progress is not executed. The reconciliation which is in progress
// repeats itself once it is done, see util.ReconcileGuard. Without that, an operator configuration or extra config
// change arriving mid-reconciliation would be stored but never applied, since none of the controllers requeues
// periodically.
func (m *TargetAllocatorManager) ReconcileTargetAllocator(
	ctx context.Context,
	trigger TargetAllocatorReconcileTrigger,
) (bool, error) {
	logger := logd.FromContext(ctx)
	logger.Info("ReconcileTargetAllocator", "trigger", trigger)

	return m.reconcileGuard.Run(
		func() (bool, error) {
			return m.reconcileTargetAllocator(ctx, logger)
		},
		func() {
			if m.developmentMode {
				logger.Info("creation/update of the OpenTelemetry target-allocator resources is already in progress, " +
					"the additional reconciliation request will be served by the reconciliation which is in progress.")
			}
		},
		func() {
			logger.Warn("the reconciliation of the OpenTelemetry target-allocator resources kept being triggered while it was " +
				"running, stopped repeating it, the pending reconciliation request is dropped.")
		},
	)
}

// reconcileTargetAllocator is the body of ReconcileTargetAllocator, executed under the manager's reconcile guard. It
// reads the operator configuration resource, the monitoring resources and the extra config itself, which is what
// allows the guard to repeat it for a trigger that arrived while it was running.
func (m *TargetAllocatorManager) reconcileTargetAllocator(ctx context.Context, logger logd.Logger) (bool, error) {
	operatorConfigurationResource, err := resources.FindOperatorConfigurationResource(ctx, m.Client, logger)
	if err != nil {
		return false, err
	}
	if operatorConfigurationResource != nil {
		logger.Debug("found operator configuration resource for target allocator reconciliation", "name", operatorConfigurationResource.Name)
	} else {
		logger.Debug("no operator configuration resource found for target allocator reconciliation")
	}
	allMonitoringResources, err := resources.FindAllMonitoringResources(ctx, m.Client, logger)
	if err != nil {
		return false, err
	}
	logger.Debug("found monitoring resources for target allocator reconciliation", "count", len(allMonitoringResources))

	namespacesWithPrometheusScraping := make([]string, 0, len(allMonitoringResources))
	for _, monitoringResource := range allMonitoringResources {
		namespace := monitoringResource.Namespace
		if pointers.ReadBoolPointerWithDefault(monitoringResource.Spec.PrometheusScraping.Enabled, true) {
			namespacesWithPrometheusScraping = append(namespacesWithPrometheusScraping, namespace)
		}
	}
	hasPrometheusScrapingEnabledForAtLeastOneNamespace := len(namespacesWithPrometheusScraping) > 0
	logger.Debug("determined namespaces with Prometheus scraping enabled", "namespaces", namespacesWithPrometheusScraping)

	extraConfig := m.extraConfig.Load()
	if extraConfig == nil {
		return false, fmt.Errorf("extra config is nil in TargetAllocatorManager#ReconcileTargetAllocator")
	}

	if operatorConfigurationResource == nil {
		logger.Info("The Dash0Configuration resource is missing or has been deleted, no Dash0 OpenTelemetry " +
			"target-allocator will be created, the existing Dash0 OpenTelemetry target-allocator (if present) will " +
			"be removed.",
		)
		err = m.removeTargetAllocator(ctx, *extraConfig, logger)
		return err == nil, err
	}

	if !pointers.ReadBoolPointerWithDefault(operatorConfigurationResource.Spec.TelemetryCollection.Enabled, true) {
		logger.Info(
			fmt.Sprintf("Telemetry collection has been disabled explicitly via the operator configuration "+
				"resource (\"%s\"), property telemetryCollection.enabled=false, no Dash0 OpenTelemetry target-allocator "+
				"will be created, the existing Dash0 OpenTelemetry target-allocator (if present) will be removed.",
				operatorConfigurationResource.Name),
		)
		err = m.removeTargetAllocator(ctx, *extraConfig, logger)
		return err == nil, err
	} else if !pointers.ReadBoolPointerWithDefault(operatorConfigurationResource.Spec.PrometheusCrdSupport.Enabled, false) {
		logger.Info(
			fmt.Sprintf("Support for Prometheus CRDs has been disabled explicitly via the operator configuration "+
				"resource (\"%s\"), property prometheusCrdSupport.enabled=false, no Dash0 OpenTelemetry target-allocator "+
				"will be created, the existing Dash0 OpenTelemetry target-allocator (if present) will be removed.",
				operatorConfigurationResource.Name),
		)
		err = m.removeTargetAllocator(ctx, *extraConfig, logger)
		return err == nil, err
	} else if !hasPrometheusScrapingEnabledForAtLeastOneNamespace {
		logger.Warn(
			fmt.Sprintf("Support for Prometheus CRDs has been enabled explicitly via the operator configuration "+
				"resource (\"%s\"), property prometheusCrdSupport.enabled=true, but not a single namespace has "+
				"`prometheusScraping.enabled` via the Dash0Monitoring resource. No Dash0 OpenTelemetry target-allocator "+
				"will be created, the existing Dash0 OpenTelemetry target-allocator (if present) will be removed.",
				operatorConfigurationResource.Name),
		)
		err = m.removeTargetAllocator(ctx, *extraConfig, logger)
		return err == nil, err
	} else {
		logger.Info(
			fmt.Sprintf("Telemetry collection and support for Prometheus CRDs has been enabled via the operator configuration "+
				"resource (\"%s\"), the Dash0 OpenTelemetry target-allocator will be created or updated.",
				operatorConfigurationResource.Name),
		)
		err = m.createOrUpdateTargetAllocator(
			ctx,
			namespacesWithPrometheusScraping,
			*extraConfig,
			logger,
		)
		return err == nil, err
	}
}

func (m *TargetAllocatorManager) createOrUpdateTargetAllocator(
	ctx context.Context,
	namespacesWithPrometheusScraping []string,
	extraConfig util.ExtraConfig,
	logger logd.Logger,
) error {
	resourcesHaveBeenCreated, resourcesHaveBeenUpdated, err :=
		m.targetAllocatorResourceManager.CreateOrUpdateTargetAllocatorResources(
			ctx,
			extraConfig,
			namespacesWithPrometheusScraping,
			logger)

	if err != nil {
		logger.ErrorTelemetryCollectionIssue(
			err,
			"failed to create one or more of the OpenTelemetry target-allocator resources, "+
				"support for Prometheus CRDs might not work",
		)
		return err
	}

	if resourcesHaveBeenCreated && resourcesHaveBeenUpdated {
		logger.Info("OpenTelemetry target-allocator Kubernetes resources have been created and updated.")
	} else if resourcesHaveBeenCreated {
		logger.Info("OpenTelemetry target-allocator Kubernetes resources have been created.")
	} else if resourcesHaveBeenUpdated {
		logger.Info("OpenTelemetry target-allocator Kubernetes resources have been updated.")
	} else {
		logger.Debug("OpenTelemetry target-allocator Kubernetes resources are already up to date, no changes required")
	}

	return nil
}

func (m *TargetAllocatorManager) removeTargetAllocator(
	ctx context.Context,
	extraConfig util.ExtraConfig,
	logger logd.Logger,
) error {
	resourcesHaveBeenDeleted, err := m.targetAllocatorResourceManager.DeleteResources(
		ctx,
		extraConfig,
		logger,
	)
	if err != nil {
		logger.Error(
			err,
			"Failed to delete the OpenTelemetry target-allocator Kubernetes resources, requeuing reconcile request.",
		)
		return err
	}
	if resourcesHaveBeenDeleted {
		logger.Info("OpenTelemetry target-allocator Kubernetes resources have been deleted.")
	} else {
		logger.Debug("no OpenTelemetry target-allocator Kubernetes resources to delete")
	}
	return nil
}
