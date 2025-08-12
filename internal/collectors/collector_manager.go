// SPDX-FileCopyrightText: Copyright 2024 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package collectors

import (
	"context"
	"fmt"
	"reflect"
	"sync/atomic"

	"github.com/go-logr/logr"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"

	dash0common "github.com/dash0hq/dash0-operator/api/operator/common"
	dash0v1alpha1 "github.com/dash0hq/dash0-operator/api/operator/v1alpha1"
	dash0v1beta1 "github.com/dash0hq/dash0-operator/api/operator/v1beta1"
	"github.com/dash0hq/dash0-operator/internal/collectors/otelcolresources"
	"github.com/dash0hq/dash0-operator/internal/resources"
	"github.com/dash0hq/dash0-operator/internal/util"
)

type CollectorManager struct {
	client.Client
	clientset                          *kubernetes.Clientset
	oTelColResourceManager             *otelcolresources.OTelColResourceManager
	extraConfig                        atomic.Pointer[util.ExtraConfig]
	developmentMode                    bool
	updateInProgress                   atomic.Bool
	resourcesHaveBeenDeletedByOperator atomic.Bool
}

type CollectorReconcileTrigger string

const (
	TriggeredByWatchEvent             CollectorReconcileTrigger = "watch"
	TriggeredByDash0ResourceReconcile CollectorReconcileTrigger = "resource"
)

func NewCollectorManager(
	k8sClient client.Client,
	clientset *kubernetes.Clientset,
	extraConfig util.ExtraConfig,
	developmentMode bool,
	oTelColResourceManager *otelcolresources.OTelColResourceManager,
) *CollectorManager {
	m := &CollectorManager{
		Client:                 k8sClient,
		clientset:              clientset,
		developmentMode:        developmentMode,
		oTelColResourceManager: oTelColResourceManager,
	}
	m.extraConfig.Store(&extraConfig)
	return m
}

func (m *CollectorManager) UpdateExtraConfig(ctx context.Context, newConfig util.ExtraConfig, logger *logr.Logger) {
	previousConfig := m.extraConfig.Swap(&newConfig)
	if previousConfig == nil || !reflect.DeepEqual(*previousConfig, newConfig) {
		hasBeenReconciled, err := m.ReconcileOpenTelemetryCollector(ctx, nil, TriggeredByWatchEvent)
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
// The parameter triggeringMonitoringResource is only != nil for case (2).
//
// Returns a boolean flag indicating whether the reconciliation has been performed (true) or has been cancelled, due
// to another reconcliation already being in progress or because the resource has been deleted by the operator.
// A return value of (nil, true) does not necessarily indicate that any collector resource has been created, updated, or
// deleted; it only indicates that the reconciliation has been performed.
func (m *CollectorManager) ReconcileOpenTelemetryCollector(
	ctx context.Context,
	triggeringMonitoringResource *dash0v1beta1.Dash0Monitoring,
	trigger CollectorReconcileTrigger,
) (bool, error) {
	logger := log.FromContext(ctx)
	if m.resourcesHaveBeenDeletedByOperator.Load() {
		if trigger == TriggeredByWatchEvent {
			if m.developmentMode {
				logger.Info("OpenTelemetry collector resources have already been deleted, ignoring reconciliation request.")
			}
			return false, nil
		} else if trigger == TriggeredByDash0ResourceReconcile {
			if m.developmentMode {
				logger.Info("resetting resourcesHaveBeenDeletedByOperator")
			}
			m.resourcesHaveBeenDeletedByOperator.Store(false)
		}
	}
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

	operatorConfigurationResource, err := m.findOperatorConfigurationResource(ctx, &logger)
	if err != nil {
		return false, err
	}
	allMonitoringResources, err := m.findAllMonitoringResources(ctx, &logger)
	if err != nil {
		return false, err
	}
	hasAtLeastOneExport := false
	if operatorConfigurationResource != nil && operatorConfigurationResource.Spec.Export != nil {
		hasAtLeastOneExport = true
	}
	if hasAtLeastOneExport == false {
		for _, monitoringResource := range allMonitoringResources {
			if monitoringResource.Spec.Export != nil {
				hasAtLeastOneExport = true
			}
		}
	}

	extraConfig := m.extraConfig.Load()
	if extraConfig == nil {
		return false, fmt.Errorf("extra config is nil in CollectorManager#ReconcileOpenTelemetryCollector")
	}
	if operatorConfigurationResource != nil && !util.ReadBoolPointerWithDefault(operatorConfigurationResource.Spec.TelemetryCollection.Enabled, true) {
		logger.Info(
			fmt.Sprintf("Telemetry collection has been disabled explicitly via the operator configuration "+
				"resource (\"%s\"), property telemetryCollection.enabled=false, no Dash0 OpenTelemetry collector "+
				"will be created, existing Dash0 OpenTelemetry collectors (if any) will be removed.",
				operatorConfigurationResource.Name),
		)
		err = m.removeOpenTelemetryCollector(ctx, *extraConfig, &logger)
		return err == nil, err
	} else if hasAtLeastOneExport {
		err = m.createOrUpdateOpenTelemetryCollector(
			ctx,
			operatorConfigurationResource,
			allMonitoringResources,
			*extraConfig,
			&logger,
		)
		return err == nil, err
	} else {
		// This should actually never happen, as the operator configuration has a kubebuilder validation comment that
		// makes the export a required field.
		if operatorConfigurationResource != nil {
			logger.Info(
				fmt.Sprintf("There is an operator configuration resource (\"%s\"), but it has no export "+
					"configuration, no Dash0 OpenTelemetry collector will be created, existing Dash0 OpenTelemetry "+
					"collectors (if any) will be removed.", operatorConfigurationResource.Name),
			)
		}
		err = m.removeOpenTelemetryCollector(ctx, *extraConfig, &logger)
		return err == nil, err
	}
}

func (m *CollectorManager) createOrUpdateOpenTelemetryCollector(
	ctx context.Context,
	operatorConfigurationResource *dash0v1alpha1.Dash0OperatorConfiguration,
	allMonitoringResources []dash0v1beta1.Dash0Monitoring,
	extraConfig util.ExtraConfig,
	logger *logr.Logger,
) error {
	resourcesHaveBeenCreated, resourcesHaveBeenUpdated, err :=
		m.oTelColResourceManager.CreateOrUpdateOpenTelemetryCollectorResources(
			ctx,
			extraConfig,
			operatorConfigurationResource,
			allMonitoringResources,
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
	}
	return nil
}

func (m *CollectorManager) removeOpenTelemetryCollector(
	ctx context.Context,
	extraConfig util.ExtraConfig,
	logger *logr.Logger,
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
	}
	return nil
}

func (m *CollectorManager) findOperatorConfigurationResource(
	ctx context.Context,
	logger *logr.Logger,
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

func (m *CollectorManager) findAllMonitoringResources(
	ctx context.Context,
	logger *logr.Logger,
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
	return monitoringResources, nil
}
