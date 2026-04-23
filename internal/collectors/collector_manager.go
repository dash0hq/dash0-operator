// SPDX-FileCopyrightText: Copyright 2024 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package collectors

import (
	"context"
	"fmt"
	"reflect"
	"slices"
	"strings"
	"sync/atomic"

	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"sigs.k8s.io/controller-runtime/pkg/client"

	dash0common "github.com/dash0hq/dash0-operator/api/operator/common"
	dash0v1alpha1 "github.com/dash0hq/dash0-operator/api/operator/v1alpha1"
	dash0v1beta1 "github.com/dash0hq/dash0-operator/api/operator/v1beta1"
	"github.com/dash0hq/dash0-operator/internal/collectors/otelcolresources"
	"github.com/dash0hq/dash0-operator/internal/resources"
	"github.com/dash0hq/dash0-operator/internal/util"
	"github.com/dash0hq/dash0-operator/internal/util/logd"
)

type CollectorManager struct {
	client.Client
	clientset                     *kubernetes.Clientset
	oTelColResourceManager        *otelcolresources.OTelColResourceManager
	extraConfig                   atomic.Pointer[util.ExtraConfig]
	developmentMode               bool
	intelligentEdgeFeatureEnabled bool
	updateInProgress              atomic.Bool
}

type CollectorReconcileTrigger string

const (
	logMsgOperatorConfigMissing string = "No operator configuration resource exists. No Dash0 OpenTelemetry collector " +
		"will be created, existing Dash0 OpenTelemetry collectors (if any) will be removed."
	logMsgDefaultExportMissing string = "There is an operator configuration resource (\"%s\"), but it has no export " +
		"configuration, no Dash0 OpenTelemetry collector will be created, existing Dash0 OpenTelemetry " +
		"collectors (if any) will be removed."
	logMsgTelemetryDisabled string = "Telemetry collection has been disabled explicitly via the operator configuration " +
		"resource (\"%s\"), property telemetryCollection.enabled=false, no Dash0 OpenTelemetry collector " +
		"will be created, existing Dash0 OpenTelemetry collectors (if any) will be removed."
)

func NewCollectorManager(
	k8sClient client.Client,
	clientset *kubernetes.Clientset,
	extraConfig util.ExtraConfig,
	developmentMode bool,
	intelligentEdgeFeatureEnabled bool,
	oTelColResourceManager *otelcolresources.OTelColResourceManager,
) *CollectorManager {
	m := &CollectorManager{
		Client:                        k8sClient,
		clientset:                     clientset,
		developmentMode:               developmentMode,
		intelligentEdgeFeatureEnabled: intelligentEdgeFeatureEnabled,
		oTelColResourceManager:        oTelColResourceManager,
	}
	m.extraConfig.Store(&extraConfig)
	return m
}

func (m *CollectorManager) UpdateExtraConfig(ctx context.Context, newConfig util.ExtraConfig, logger logd.Logger) {
	previousConfig := m.extraConfig.Swap(&newConfig)
	if previousConfig == nil || !reflect.DeepEqual(*previousConfig, newConfig) {
		hasBeenReconciled, err := m.ReconcileOpenTelemetryCollector(ctx)
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

// ReconcileOpenTelemetryCollector can be triggered by a
//  1. a reconcile request from the Dash0OperatorConfiguration resource.
//  2. a reconcile request from a Dash0Monitoring resource in the cluster.
//  3. a change event on one of the OpenTelemetry collector related resources that the operator manages (a change to one
//     of "our" config maps or similar).
//  4. a file change event picked up by the extra config map watcher
//
// Returns a boolean flag indicating whether the reconciliation has been performed (true) or has been cancelled, due
// to another reconciliation already being in progress or because the resource has been deleted by the operator.
// A return value of (nil, true) does not necessarily indicate that any collector resource has been created, updated, or
// deleted; it only indicates that the reconciliation has been performed.
func (m *CollectorManager) ReconcileOpenTelemetryCollector(
	ctx context.Context,
) (bool, error) {
	logger := logd.FromContext(ctx)
	if m.updateInProgress.Load() {
		logger.Debug("creation/update of the OpenTelemetry collector resources is already in progress, skipping " +
			"additional reconciliation request.")
		return false, nil
	}

	m.updateInProgress.Store(true)
	defer func() {
		m.updateInProgress.Store(false)
	}()

	operatorConfigurationResource, err := m.findOperatorConfigurationResource(ctx, logger)
	if err != nil {
		return false, err
	}
	if operatorConfigurationResource != nil {
		logger.Debug("found operator configuration resource for collector reconciliation", "name", operatorConfigurationResource.Name)
	} else {
		logger.Debug("no operator configuration resource found for collector reconciliation")
	}
	allMonitoringResources, err := m.findAllMonitoringResources(ctx, logger)
	if err != nil {
		return false, err
	}
	logger.Debug("found available monitoring resources for collector reconciliation", "count", len(allMonitoringResources))
	var intelligentEdgeResource *dash0v1alpha1.Dash0IntelligentEdge
	if m.intelligentEdgeFeatureEnabled {
		intelligentEdgeResource, err = m.findIntelligentEdgeResource(ctx, logger)
		if err != nil {
			return false, err
		}
		if intelligentEdgeResource != nil {
			logger.Debug("found intelligent edge resource for collector reconciliation", "name", intelligentEdgeResource.Name)
		} else {
			logger.Debug("no intelligent edge resource found for collector reconciliation")
		}
	}

	extraConfig := m.extraConfig.Load()
	if extraConfig == nil {
		return false, fmt.Errorf("extra config is nil in CollectorManager#ReconcileOpenTelemetryCollector")
	}

	if operatorConfigurationResource == nil {
		logger.Info(logMsgOperatorConfigMissing)
		err = m.removeOpenTelemetryCollector(ctx, *extraConfig, logger)
		return err == nil, err
	} else if !util.ReadBoolPointerWithDefault(operatorConfigurationResource.Spec.TelemetryCollection.Enabled, true) {
		logger.Info(fmt.Sprintf(logMsgTelemetryDisabled, operatorConfigurationResource.Name))
		err = m.removeOpenTelemetryCollector(ctx, *extraConfig, logger)
		return err == nil, err
	} else if !operatorConfigurationResource.HasExportsConfigured() {
		logger.Info(fmt.Sprintf(logMsgDefaultExportMissing, operatorConfigurationResource.Name))
		err = m.removeOpenTelemetryCollector(ctx, *extraConfig, logger)
		return err == nil, err
	} else {
		err = m.createOrUpdateOpenTelemetryCollector(
			ctx,
			operatorConfigurationResource,
			allMonitoringResources,
			intelligentEdgeResource,
			*extraConfig,
			logger,
		)
		return err == nil, err
	}
}

func (m *CollectorManager) createOrUpdateOpenTelemetryCollector(
	ctx context.Context,
	operatorConfigurationResource *dash0v1alpha1.Dash0OperatorConfiguration,
	allMonitoringResources []dash0v1beta1.Dash0Monitoring,
	intelligentEdgeResource *dash0v1alpha1.Dash0IntelligentEdge,
	extraConfig util.ExtraConfig,
	logger logd.Logger,
) error {
	slices.SortFunc(
		allMonitoringResources,
		func(mr1 dash0v1beta1.Dash0Monitoring, mr2 dash0v1beta1.Dash0Monitoring) int {
			return strings.Compare(mr1.Namespace, mr2.Namespace)
		},
	)
	resourcesHaveBeenCreated, resourcesHaveBeenUpdated, err :=
		m.oTelColResourceManager.CreateOrUpdateOpenTelemetryCollectorResources(
			ctx,
			extraConfig,
			operatorConfigurationResource,
			allMonitoringResources,
			intelligentEdgeResource,
			logger,
		)
	if err != nil {
		logger.Error(
			err,
			"failed to create one or more of the OpenTelemetry collector DaemonSet/Deployment resources, some or "+
				"all telemetry will be missing",
		)
		return err
	}
	if resourcesHaveBeenCreated && resourcesHaveBeenUpdated {
		logger.Info("OpenTelemetry collector Kubernetes resources have been created and updated.")
	} else if resourcesHaveBeenCreated {
		logger.Info("OpenTelemetry collector Kubernetes resources have been created.")
	} else if resourcesHaveBeenUpdated {
		logger.Info("OpenTelemetry collector Kubernetes resources have been updated.")
	} else {
		logger.Debug("OpenTelemetry collector Kubernetes resources are already up to date, no changes required")
	}
	return nil
}

func (m *CollectorManager) removeOpenTelemetryCollector(
	ctx context.Context,
	extraConfig util.ExtraConfig,
	logger logd.Logger,
) error {
	resourcesHaveBeenDeleted, err := m.oTelColResourceManager.DeleteResources(
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
	} else {
		logger.Debug("no OpenTelemetry collector Kubernetes resources to delete")
	}
	return nil
}

func (m *CollectorManager) findOperatorConfigurationResource(
	ctx context.Context,
	logger logd.Logger,
) (*dash0v1alpha1.Dash0OperatorConfiguration, error) {
	operatorConfigurationResource, err := resources.FindUniqueOrMostRecentResourceInScope(
		ctx,
		m.Client,
		"", /* cluster-scope, thus no namespace */
		&dash0v1alpha1.Dash0OperatorConfiguration{},
		logger,
	)
	if err != nil {
		return nil, err
	}
	if operatorConfigurationResource == nil {
		return nil, nil
	}
	return operatorConfigurationResource.(*dash0v1alpha1.Dash0OperatorConfiguration), nil
}

func (m *CollectorManager) findIntelligentEdgeResource(
	ctx context.Context,
	logger logd.Logger,
) (*dash0v1alpha1.Dash0IntelligentEdge, error) {
	intelligentEdgeResource, err := resources.FindUniqueOrMostRecentResourceInScope(
		ctx,
		m.Client,
		"",
		&dash0v1alpha1.Dash0IntelligentEdge{},
		logger,
	)
	if err != nil {
		return nil, err
	}
	if intelligentEdgeResource == nil {
		return nil, nil
	}
	return intelligentEdgeResource.(*dash0v1alpha1.Dash0IntelligentEdge), nil
}

func (m *CollectorManager) findAllMonitoringResources(
	ctx context.Context,
	logger logd.Logger,
) ([]dash0v1beta1.Dash0Monitoring, error) {
	monitoringResourceList := dash0v1beta1.Dash0MonitoringList{}
	if err := m.List(
		ctx,
		&monitoringResourceList,
		&client.ListOptions{},
	); err != nil {
		logger.Error(err, "Failed to list all Dash0 monitoring resources, requeuing reconcile request.")
		return nil, err
	}

	// filter monitoring resources that are not in state available
	monitoringResources := make([]dash0v1beta1.Dash0Monitoring, 0, len(monitoringResourceList.Items))
	for _, mr := range monitoringResourceList.Items {
		availableCondition := meta.FindStatusCondition(
			mr.Status.Conditions,
			string(dash0common.ConditionTypeAvailable),
		)
		if availableCondition == nil || availableCondition.Status != metav1.ConditionTrue {
			continue
		}
		monitoringResources = append(monitoringResources, mr)
	}
	logger.Debug(
		"filtered monitoring resources by availability",
		"total", len(monitoringResourceList.Items),
		"available", len(monitoringResources),
	)
	return monitoringResources, nil
}
