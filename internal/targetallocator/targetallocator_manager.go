// SPDX-FileCopyrightText: Copyright 2025 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package targetallocator

import (
	"context"
	"fmt"
	"reflect"
	"sync/atomic"

	"github.com/go-logr/logr"
	"k8s.io/client-go/kubernetes"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"

	taresources "github.com/dash0hq/dash0-operator/internal/targetallocator/taresources"
	"github.com/dash0hq/dash0-operator/internal/util"
	"github.com/dash0hq/dash0-operator/internal/util/resources"
)

type TargetAllocatorManager struct {
	client.Client
	clientset                      *kubernetes.Clientset
	targetAllocatorResourceManager *taresources.TargetAllocatorResourceManager
	extraConfig                    atomic.Pointer[util.ExtraConfig]
	developmentMode                bool
	updateInProgress               atomic.Bool
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

func (m *TargetAllocatorManager) UpdateExtraConfig(ctx context.Context, newConfig util.ExtraConfig, logger logr.Logger) {
	previousConfig := m.extraConfig.Swap(&newConfig)
	if previousConfig == nil || !reflect.DeepEqual(*previousConfig, newConfig) {
		hasBeenReconciled, err := m.ReconcileTargetAllocator(ctx, TriggeredByWatchEvent)
		if err != nil {
			logger.Error(err, "Failed to create/update collector resources after extra config map update.")
		}
		if hasBeenReconciled {
			logger.Info("successfully reconciled collector resources after extra config map update")
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
// to another reconcliation already being in progress or because the resource has been deleted by the operator.
// A return value of (nil, true) does not necessarily indicate that any target-allocator resource has been created, updated, or
// deleted; it only indicates that the reconciliation has been performed.
func (m *TargetAllocatorManager) ReconcileTargetAllocator(
	ctx context.Context,
	trigger TargetAllocatorReconcileTrigger,
) (bool, error) {
	logger := log.FromContext(ctx)
	logger.Info("ReconcileTargetAllocator", "trigger", trigger)

	if m.updateInProgress.Load() {
		if m.developmentMode {
			logger.Info("creation/update of the OpenTelemetry collector resources is already in progress, skipping " +
				"additional reconciliation request.")
		}
		return false, nil
	}
	m.updateInProgress.Store(true)
	defer func() {
		m.updateInProgress.Store(false)
	}()

	operatorConfigurationResource, err := resources.FindOperatorConfigurationResource(ctx, m.Client, logger)
	if err != nil {
		return false, err
	}
	allMonitoringResources, err := resources.FindAllMonitoringResources(ctx, m.Client, logger)
	if err != nil {
		return false, err
	}

	namespacesWithPrometheusScraping := make([]string, 0, len(allMonitoringResources))
	for _, monitoringResource := range allMonitoringResources {
		namespace := monitoringResource.Namespace
		if util.ReadBoolPointerWithDefault(monitoringResource.Spec.PrometheusScraping.Enabled, true) {
			namespacesWithPrometheusScraping = append(namespacesWithPrometheusScraping, namespace)
		}
	}
	hasPrometheusScrapingEnabledForAtLeastOneNamespace := len(namespacesWithPrometheusScraping) > 0

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

	if !util.ReadBoolPointerWithDefault(operatorConfigurationResource.Spec.TelemetryCollection.Enabled, true) {
		logger.Info(
			fmt.Sprintf("Telemetry collection has been disabled explicitly via the operator configuration "+
				"resource (\"%s\"), property telemetryCollection.enabled=false, no Dash0 OpenTelemetry target-allocator "+
				"will be created, the existing Dash0 OpenTelemetry target-allocator (if present) will be removed.",
				operatorConfigurationResource.Name),
		)
		err = m.removeTargetAllocator(ctx, *extraConfig, logger)
		return err == nil, err
	} else if !util.ReadBoolPointerWithDefault(operatorConfigurationResource.Spec.PrometheusCrdSupport.Enabled, false) {
		logger.Info(
			fmt.Sprintf("Support for Prometheus CRDs has been disabled explicitly via the operator configuration "+
				"resource (\"%s\"), property prometheusCrdSupport.enabled=false, no Dash0 OpenTelemetry target-allocator "+
				"will be created, the existing Dash0 OpenTelemetry target-allocator (if present) will be removed.",
				operatorConfigurationResource.Name),
		)
		err = m.removeTargetAllocator(ctx, *extraConfig, logger)
		return err == nil, err
	} else if !hasPrometheusScrapingEnabledForAtLeastOneNamespace {
		logger.Info(
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
	logger logr.Logger,
) error {
	resourcesHaveBeenCreated, resourcesHaveBeenUpdated, err :=
		m.targetAllocatorResourceManager.CreateOrUpdateTargetAllocatorResources(
			ctx,
			extraConfig,
			namespacesWithPrometheusScraping,
			logger)

	if err != nil {
		logger.Error(
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
	}

	return nil
}

func (m *TargetAllocatorManager) removeTargetAllocator(
	ctx context.Context,
	extraConfig util.ExtraConfig,
	logger logr.Logger,
) error {
	resourcesHaveBeenDeleted, err := m.targetAllocatorResourceManager.DeleteResources(
		ctx,
		extraConfig,
		logger,
	)
	if err != nil {
		logger.Error(
			err,
			"Failed to delete the OpenTelemetry collector Kubernetes resources, requeuing reconcile request.",
		)
		return err
	}
	if resourcesHaveBeenDeleted {
		logger.Info("OpenTelemetry collector Kubernetes resources have been deleted.")
	}
	return nil
}
