// SPDX-FileCopyrightText: Copyright 2024 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package backendconnection

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"sync/atomic"

	"github.com/go-logr/logr"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"

	dash0v1alpha1 "github.com/dash0hq/dash0-operator/api/dash0monitoring/v1alpha1"
	"github.com/dash0hq/dash0-operator/internal/backendconnection/otelcolresources"
	"github.com/dash0hq/dash0-operator/internal/util"
)

type BackendConnectionManager struct {
	client.Client
	Clientset *kubernetes.Clientset
	*otelcolresources.OTelColResourceManager
	updateInProgress                   atomic.Bool
	resourcesHaveBeenDeletedByOperator atomic.Bool
}

type BackendConnectionReconcileTrigger string

const (
	TriggeredByWatchEvent             BackendConnectionReconcileTrigger = "watch"
	TriggeredByDash0ResourceReconcile BackendConnectionReconcileTrigger = "resource"
)

// ReconcileOpenTelemetryCollector can be triggered by a
//  1. a reconcile request from the Dash0OperatorConfiguration resource.
//  2. a reconcile request from a Dash0Monitoring resource in the cluster.
//  3. a change event on one of the OpenTelemetry collector related resources that the operator manages (a change to one
//     of "our" config maps or similar).
//
// The parameter triggeringMonitoringResource is only != nil for case (2).
//
// Returns a boolean flag indicating whether the reconciliation has been performed (true) or has been cancelled, due
// to another reconcliation already being in progress or because the resource has been deleted by the operator.
// A return value of (nil, true) does not necessarily indicate that any collector resource has been created, updated, or
// deleted; it only indicates that the reconciliation has been performed.
func (m *BackendConnectionManager) ReconcileOpenTelemetryCollector(
	ctx context.Context,
	images util.Images,
	operatorNamespace string,
	triggeringMonitoringResource *dash0v1alpha1.Dash0Monitoring,
	trigger BackendConnectionReconcileTrigger,
) (error, bool) {
	logger := log.FromContext(ctx)
	if m.resourcesHaveBeenDeletedByOperator.Load() {
		if trigger == TriggeredByWatchEvent {
			if m.DevelopmentMode {
				logger.Info("OpenTelemetry collector resources have already been deleted, ignoring reconciliation request.")
			}
			return nil, false
		} else if trigger == TriggeredByDash0ResourceReconcile {
			if m.DevelopmentMode {
				logger.Info("resetting resourcesHaveBeenDeletedByOperator")
			}
			m.resourcesHaveBeenDeletedByOperator.Store(false)
		}
	}
	if m.updateInProgress.Load() {
		if m.DevelopmentMode {
			logger.Info("creation/update of the OpenTelemetry collector resources is already in progress, skipping " +
				"additional reconciliation request.")
		}
		return nil, false
	}

	m.updateInProgress.Store(true)
	defer func() {
		m.updateInProgress.Store(false)
	}()

	operatorConfigurationResource, err := m.findOperatorConfigurationResource(ctx, &logger)
	if err != nil {
		return err, false
	}
	allMonitoringResources, err := m.findAllMonitoringResources(ctx, &logger)
	if err != nil {
		return err, false
	}
	var export *dash0v1alpha1.Export
	if operatorConfigurationResource != nil && operatorConfigurationResource.Spec.Export != nil {
		export = operatorConfigurationResource.Spec.Export
	}
	if export == nil && triggeringMonitoringResource != nil &&
		triggeringMonitoringResource.IsAvailable() &&
		triggeringMonitoringResource.Spec.Export != nil {
		export = triggeringMonitoringResource.Spec.Export
	}
	if export == nil {
		// Using the export setting of an arbitrary monitoring resource is a bandaid as long as we do not allow
		// exporting, telemetry to different backends per namespace.
		// Additional note: When using the export from an arbitrary monitoring resource, we need to be aware that the
		// result of we findAllMonitoringResources is not guaranteed to be sorted in the same way for each invocation.
		// Thus, we need to sort the monitoring resources before we arbitrarily pick the first resource, otherwise we
		// could get configmap flapping (i.e. the collector configmaps get re-rendered again and again because we
		// accidentally pick a different monitoring resource each time).
		slices.SortFunc(
			allMonitoringResources,
			func(mr1 dash0v1alpha1.Dash0Monitoring, mr2 dash0v1alpha1.Dash0Monitoring) int {
				return strings.Compare(mr1.Namespace, mr2.Namespace)
			},
		)
		for _, monitoringResource := range allMonitoringResources {
			if monitoringResource.Spec.Export != nil {
				export = monitoringResource.Spec.Export
				break
			}
		}
	}

	if export != nil {
		err = m.createOrUpdateOpenTelemetryCollector(
			ctx,
			operatorNamespace,
			images,
			operatorConfigurationResource,
			allMonitoringResources,
			export,
			&logger,
		)
		return err, err == nil
	} else {
		if operatorConfigurationResource != nil {
			logger.Info(
				fmt.Sprintf("There is an operator configuration resource (\"%s\"), but it has no export "+
					"configuration, no Dash0 OpenTelemetry collector will be created, existing Dash0 OpenTelemetry "+
					"collectors will be removed.", operatorConfigurationResource.Name),
			)
		}
		err = m.removeOpenTelemetryCollector(ctx, operatorNamespace, &logger)
		return err, err == nil
	}
}

func (m *BackendConnectionManager) createOrUpdateOpenTelemetryCollector(
	ctx context.Context,
	operatorNamespace string,
	images util.Images,
	operatorConfigurationResource *dash0v1alpha1.Dash0OperatorConfiguration,
	allMonitoringResources []dash0v1alpha1.Dash0Monitoring,
	export *dash0v1alpha1.Export,
	logger *logr.Logger,
) error {
	resourcesHaveBeenCreated, resourcesHaveBeenUpdated, err :=
		m.OTelColResourceManager.CreateOrUpdateOpenTelemetryCollectorResources(
			ctx,
			operatorNamespace,
			images,
			operatorConfigurationResource,
			allMonitoringResources,
			export,
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

func (m *BackendConnectionManager) removeOpenTelemetryCollector(
	ctx context.Context,
	operatorNamespace string,
	logger *logr.Logger,
) error {
	resourcesHaveBeenDeleted, err := m.OTelColResourceManager.DeleteResources(
		ctx,
		operatorNamespace,
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

func (m *BackendConnectionManager) findOperatorConfigurationResource(
	ctx context.Context,
	logger *logr.Logger,
) (*dash0v1alpha1.Dash0OperatorConfiguration, error) {
	operatorConfigurationResource, err := util.FindUniqueOrMostRecentResourceInScope(
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

func (m *BackendConnectionManager) findAllMonitoringResources(
	ctx context.Context,
	logger *logr.Logger,
) ([]dash0v1alpha1.Dash0Monitoring, error) {
	monitoringResourceList := dash0v1alpha1.Dash0MonitoringList{}
	if err := m.List(
		ctx,
		&monitoringResourceList,
		&client.ListOptions{},
	); err != nil {
		logger.Error(err, "Failed to list all Dash0 monitoring resources, requeuing reconcile request.")
		return nil, err
	}

	// filter monitoring resources that are not in state available
	monitoringResources := make([]dash0v1alpha1.Dash0Monitoring, 0, len(monitoringResourceList.Items))
	for _, mr := range monitoringResourceList.Items {
		availableCondition := meta.FindStatusCondition(
			mr.Status.Conditions,
			string(dash0v1alpha1.ConditionTypeAvailable),
		)
		if availableCondition == nil || availableCondition.Status != metav1.ConditionTrue {
			continue
		}
		monitoringResources = append(monitoringResources, mr)
	}
	return monitoringResources, nil
}
