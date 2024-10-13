// SPDX-FileCopyrightText: Copyright 2024 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package backendconnection

import (
	"context"
	"fmt"
	"sync/atomic"

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
	TriggeredByWatchEvent    BackendConnectionReconcileTrigger = "watch"
	TriggeredByDash0Resource BackendConnectionReconcileTrigger = "resource"
)

func (m *BackendConnectionManager) EnsureOpenTelemetryCollectorIsDeployedInOperatorNamespace(
	ctx context.Context,
	images util.Images,
	operatorNamespace string,
	monitoringResource *dash0v1alpha1.Dash0Monitoring,
	trigger BackendConnectionReconcileTrigger,
) error {
	logger := log.FromContext(ctx)
	if m.resourcesHaveBeenDeletedByOperator.Load() {
		if trigger == TriggeredByWatchEvent {
			if m.DevelopmentMode {
				logger.Info("OpenTelemetry collector resources have already been deleted, ignoring reconciliation request.")
			}
			return nil
		} else if trigger == TriggeredByDash0Resource {
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
		return nil
	}

	m.updateInProgress.Store(true)
	defer func() {
		m.updateInProgress.Store(false)
	}()

	resourcesHaveBeenCreated, resourcesHaveBeenUpdated, err :=
		m.OTelColResourceManager.CreateOrUpdateOpenTelemetryCollectorResources(
			ctx,
			operatorNamespace,
			images,
			monitoringResource,
			&logger,
		)

	if err != nil {
		logger.Error(
			err,
			"failed to create one or more of the OpenTelemetry collector DaemonSet/Deployment resources, some or "+
				"all telemetry will be missing",
		)
		return err
	}

	if resourcesHaveBeenCreated {
		logger.Info("OpenTelemetry collector Kubernetes resources have been created.")
	} else if resourcesHaveBeenUpdated {
		logger.Info("OpenTelemetry collector Kubernetes resources have been updated.")
	}
	return nil
}

func (m *BackendConnectionManager) RemoveOpenTelemetryCollectorIfNoMonitoringResourceIsLeft(
	ctx context.Context,
	operatorNamespace string,
	dash0MonitoringResourceToBeDeleted *dash0v1alpha1.Dash0Monitoring,
) error {
	m.resourcesHaveBeenDeletedByOperator.Store(true)
	m.updateInProgress.Store(true)
	defer func() {
		m.updateInProgress.Store(false)
	}()

	logger := log.FromContext(ctx)
	list := &dash0v1alpha1.Dash0MonitoringList{}
	err := m.Client.List(
		ctx,
		list,
	)

	if err != nil {
		logger.Error(err, "Error when checking whether there are any Dash0 monitoring resources left in the cluster.")
		return err
	}
	if len(list.Items) > 1 {
		// There is still more than one Dash0 monitoring resource in the namespace, do not remove the backend connection.
		return nil
	}

	if len(list.Items) == 1 && list.Items[0].UID != dash0MonitoringResourceToBeDeleted.UID {
		// There is only one Dash0 monitoring resource left, but it is *not* the one that is about to be deleted.
		// Do not remove the backend connection.
		logger.Info(
			"There is only one Dash0 monitoring resource left, but it is not the one being deleted.",
			"to be deleted/UID",
			dash0MonitoringResourceToBeDeleted.UID,
			"to be deleted/namespace",
			dash0MonitoringResourceToBeDeleted.Namespace,
			"to be deleted/name",
			dash0MonitoringResourceToBeDeleted.Name,
			"existing resource/UID",
			list.Items[0].UID,
			"existing resource/namespace",
			list.Items[0].Namespace,
			"existing resource/name",
			list.Items[0].Name,
		)
		return nil
	}

	// Either there is no Dash0 monitoring resource left, or only one and that one is about to be deleted. Delete the
	// backend connection.
	logger.Info(
		fmt.Sprintf(
			"Deleting the OpenTelemetry collector Kuberenetes resources in the Dash0 operator namespace %s.",
			operatorNamespace,
		))

	if err = m.OTelColResourceManager.DeleteResources(
		ctx,
		operatorNamespace,
		&logger,
	); err != nil {
		logger.Error(
			err,
			"Failed to delete the OpenTelemetry collector Kuberenetes resources, requeuing reconcile request.",
		)
		return err
	}
	return nil
}
