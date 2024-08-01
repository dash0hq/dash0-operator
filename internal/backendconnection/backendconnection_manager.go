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

	dash0v1alpha1 "github.com/dash0hq/dash0-operator/api/dash0/v1alpha1"
	"github.com/dash0hq/dash0-operator/internal/backendconnection/otelcolresources"
)

type BackendConnectionManager struct {
	client.Client
	Clientset *kubernetes.Clientset
	Scheme    *runtime.Scheme
	*otelcolresources.OTelColResourceManager
}

func (m *BackendConnectionManager) EnsureOpenTelemetryCollectorIsDeployedInDash0OperatorNamespace(
	ctx context.Context,
	operatorNamespace string,
	dash0CustomResource *dash0v1alpha1.Dash0,
) error {
	logger := log.FromContext(ctx)

	if dash0CustomResource.Spec.IngressEndpoint == "" {
		err := fmt.Errorf("no ingress endpoint provided, unable to create the OpenTelemetry collector")
		logger.Error(err, "failed to create a backend connection, no telemetry will be reported to Dash0")
		return err
	}
	if dash0CustomResource.Spec.AuthorizationToken == "" && dash0CustomResource.Spec.SecretRef == "" {
		err := fmt.Errorf("neither an authorization token nor a reference to a Kubernetes secret has been provided, " +
			"unable to create the OpenTelemetry collector")
		logger.Error(err, "failed to create a backend connection, no telemetry will be reported to Dash0")
		return err
	}

	resourcesHaveBeenCreated, resourcesHaveBeenUpdated, err :=
		m.OTelColResourceManager.CreateOrUpdateOpenTelemetryCollectorResources(
			ctx,
			operatorNamespace,
			dash0CustomResource.Spec.IngressEndpoint,
			dash0CustomResource.Spec.AuthorizationToken,
			dash0CustomResource.Spec.SecretRef,
			&logger,
		)

	if err != nil {
		logger.Error(err, "failed to create a backend connection, no telemetry will be reported to Dash0")
		return err
	}

	if resourcesHaveBeenCreated {
		logger.Info("OpenTelemetry collector Kubernetes resources have been created.")
	} else if resourcesHaveBeenUpdated {
		logger.Info("OpenTelemetry collector Kubernetes resources have been updated.")
	}
	return nil
}

func (m *BackendConnectionManager) RemoveOpenTelemetryCollectorIfNoDash0CustomResourceIsLeft(
	ctx context.Context,
	operatorNamespace string,
	dash0CustomResourceToBeDeleted *dash0v1alpha1.Dash0,
) error {
	logger := log.FromContext(ctx)
	list := &dash0v1alpha1.Dash0List{}
	err := m.Client.List(
		ctx,
		list,
	)

	if err != nil {
		logger.Error(err, "Error when checking whether there are any Dash0 custom resources left in the cluster.")
		return err
	}
	if len(list.Items) > 1 {
		// There is still more than one Dash0 custom resource in the namespace, do not remove the backend connection.
		return nil
	}

	if len(list.Items) == 1 && list.Items[0].UID != dash0CustomResourceToBeDeleted.UID {
		// There is only one Dash0 custom resource left, but it is *not* the one that is about to be deleted.
		// Do not remove the backend connection.
		logger.Info(
			"There is only one Dash0 custom resource left, but it is not the one being deleted.",
			"to be deleted/UID",
			dash0CustomResourceToBeDeleted.UID,
			"to be deleted/namespace",
			dash0CustomResourceToBeDeleted.Namespace,
			"to be deleted/name",
			dash0CustomResourceToBeDeleted.Name,
			"existing resource/UID",
			list.Items[0].UID,
			"existing resource/namespace",
			list.Items[0].Namespace,
			"existing resource/name",
			list.Items[0].Name,
		)
		return nil
	}

	// Either there is no Dash0 custom resource left, or only one and that one is about to be deleted. Delete the
	// backend connection.
	logger.Info(fmt.Sprintf("Deleting the OpenTelemetry collector resources in the Dash0 operator namespace %s.", operatorNamespace))

	if err := m.OTelColResourceManager.DeleteResources(
		ctx,
		operatorNamespace,
		dash0CustomResourceToBeDeleted.Spec.IngressEndpoint,
		dash0CustomResourceToBeDeleted.Spec.AuthorizationToken,
		dash0CustomResourceToBeDeleted.Spec.SecretRef,
		&logger,
	); err != nil {
		logger.Error(err, "Failed to delete the OpenTelemetry collector resources, requeuing reconcile request.")
		return err
	}
	return nil
}
