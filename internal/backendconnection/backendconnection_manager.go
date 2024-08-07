// SPDX-FileCopyrightText: Copyright 2024 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package backendconnection

import (
	"context"
	"fmt"

	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"

	dash0v1alpha1 "github.com/dash0hq/dash0-operator/api/dash0monitoring/v1alpha1"
	"github.com/dash0hq/dash0-operator/internal/backendconnection/otelcolresources"
	"github.com/dash0hq/dash0-operator/internal/dash0/util"
)

type BackendConnectionManager struct {
	client.Client
	Clientset *kubernetes.Clientset
	Scheme    *runtime.Scheme
	*otelcolresources.OTelColResourceManager
}

const (
	failedToCreateMsg = "failed to create the OpenTelemetry collector instance, no telemetry will be reported to Dash0"
)

func (m *BackendConnectionManager) EnsureOpenTelemetryCollectorIsDeployedInDash0OperatorNamespace(
	ctx context.Context,
	images util.Images,
	operatorNamespace string,
	dash0MonitoringResource *dash0v1alpha1.Dash0Monitoring,
) error {
	logger := log.FromContext(ctx)

	if dash0MonitoringResource.Spec.IngressEndpoint == "" {
		err := fmt.Errorf("no ingress endpoint provided, unable to create the OpenTelemetry collector")
		logger.Error(err, failedToCreateMsg)
		return err
	}
	if dash0MonitoringResource.Spec.AuthorizationToken == "" && dash0MonitoringResource.Spec.SecretRef == "" {
		err := fmt.Errorf("neither an authorization token nor a reference to a Kubernetes secret has been provided, " +
			"unable to create the OpenTelemetry collector")
		logger.Error(err, failedToCreateMsg)
		return err
	}

	resourcesHaveBeenCreated, resourcesHaveBeenUpdated, err :=
		m.OTelColResourceManager.CreateOrUpdateOpenTelemetryCollectorResources(
			ctx,
			operatorNamespace,
			images,
			dash0MonitoringResource.Spec.IngressEndpoint,
			dash0MonitoringResource.Spec.AuthorizationToken,
			dash0MonitoringResource.Spec.SecretRef,
			&logger,
		)

	if err != nil {
		logger.Error(err, failedToCreateMsg)
		return err
	}

	if resourcesHaveBeenCreated {
		logger.Info("OpenTelemetry collector Kubernetes resources have been created.")
	} else if resourcesHaveBeenUpdated {
		logger.Info("OpenTelemetry collector Kubernetes resources have been updated.")
	}
	return nil
}

func (m *BackendConnectionManager) RemoveOpenTelemetryCollectorIfNoDash0MonitoringResourceIsLeft(
	ctx context.Context,
	images util.Images,
	operatorNamespace string,
	dash0MonitoringResourceToBeDeleted *dash0v1alpha1.Dash0Monitoring,
) error {
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
	logger.Info(fmt.Sprintf("Deleting the OpenTelemetry collector resources in the Dash0 operator namespace %s.", operatorNamespace))

	if err := m.OTelColResourceManager.DeleteResources(
		ctx,
		operatorNamespace,
		images,
		dash0MonitoringResourceToBeDeleted.Spec.IngressEndpoint,
		dash0MonitoringResourceToBeDeleted.Spec.AuthorizationToken,
		dash0MonitoringResourceToBeDeleted.Spec.SecretRef,
		&logger,
	); err != nil {
		logger.Error(err, "Failed to delete the OpenTelemetry collector resources, requeuing reconcile request.")
		return err
	}
	return nil
}
