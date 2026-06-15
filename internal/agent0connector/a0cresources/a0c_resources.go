// SPDX-FileCopyrightText: Copyright 2026 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package a0cresources

import (
	"context"
	"errors"
	"fmt"

	"github.com/cisco-open/k8s-objectmatcher/patch"
	appsv1 "k8s.io/api/apps/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/dash0hq/dash0-operator/internal/util"
	"github.com/dash0hq/dash0-operator/internal/util/logd"
	"github.com/dash0hq/dash0-operator/internal/util/resources"
)

type Agent0ConnectorResourceManager struct {
	client.Client
	scheme                    *runtime.Scheme
	operatorManagerDeployment *appsv1.Deployment
	agent0ConnectorConfig     util.Agent0ConnectorConfig
}

func NewAgent0ConnectorResourceManager(
	k8sClient client.Client,
	scheme *runtime.Scheme,
	operatorManagerDeployment *appsv1.Deployment,
	agent0ConnectorConfig util.Agent0ConnectorConfig,
) *Agent0ConnectorResourceManager {
	return &Agent0ConnectorResourceManager{
		Client:                    k8sClient,
		scheme:                    scheme,
		operatorManagerDeployment: operatorManagerDeployment,
		agent0ConnectorConfig:     agent0ConnectorConfig,
	}
}

func (m *Agent0ConnectorResourceManager) CreateOrUpdateAgent0ConnectorResources(
	ctx context.Context,
	logger logd.Logger,
) (bool, bool, error) {
	authTokenEnvVar, err := util.CreateEnvVarForAuthorization(
		m.agent0ConnectorConfig.Authorization,
		authTokenEnvVarName,
	)
	if err != nil {
		logger.Error(err, "no Dash0 authorization token is available for the agent0-connector workload, "+
			"not creating the agent0-connector resources")
		return false, false, err
	}

	desiredState := assembleDesiredState(&m.agent0ConnectorConfig, &authTokenEnvVar)

	resourcesHaveBeenCreated := false
	resourcesHaveBeenUpdated := false
	for _, wrapper := range desiredState {
		desiredResource := wrapper.object
		isNew, isChanged, err := m.createOrUpdateResource(ctx, desiredResource, logger)
		if err != nil {
			logger.Error(err, "error while creating/updating agent0-connector resource")
			return resourcesHaveBeenCreated, resourcesHaveBeenUpdated, err
		} else if isNew {
			resourcesHaveBeenCreated = true
		} else if isChanged {
			resourcesHaveBeenUpdated = true
		}
	}

	return resourcesHaveBeenCreated, resourcesHaveBeenUpdated, nil
}

func (m *Agent0ConnectorResourceManager) createOrUpdateResource(
	ctx context.Context,
	desiredResource client.Object,
	logger logd.Logger,
) (bool, bool, error) {
	existingResource, err := resources.CreateEmptyReceiverFor(desiredResource)
	if err != nil {
		return false, false, err
	}
	err = m.Get(ctx, client.ObjectKeyFromObject(desiredResource), existingResource)
	if err != nil {
		if !apierrors.IsNotFound(err) {
			return false, false, err
		}
		err = m.createResource(ctx, desiredResource, logger)
		if err != nil {
			return false, false, err
		}
		return true, false, nil
	}
	// object might need to be updated
	hasChanged, err := m.updateResource(ctx, existingResource, desiredResource, logger)
	if err != nil {
		return false, false, err
	}
	return false, hasChanged, err
}

func (m *Agent0ConnectorResourceManager) createResource(
	ctx context.Context,
	desiredResource client.Object,
	logger logd.Logger,
) error {
	if err := resources.SetOwnerReference(m.operatorManagerDeployment, m.scheme, desiredResource, logger); err != nil {
		return err
	}
	if err := patch.DefaultAnnotator.SetLastAppliedAnnotation(desiredResource); err != nil {
		return err
	}
	err := m.Create(ctx, desiredResource)
	if err != nil {
		return err
	}
	logger.Info(fmt.Sprintf(
		"created resource %s/%s",
		desiredResource.GetNamespace(),
		desiredResource.GetName(),
	))
	return nil
}

func (m *Agent0ConnectorResourceManager) updateResource(
	ctx context.Context,
	existingResource client.Object,
	desiredResource client.Object,
	logger logd.Logger,
) (bool, error) {
	if err := resources.SetOwnerReference(m.operatorManagerDeployment, m.scheme, desiredResource, logger); err != nil {
		return false, err
	}

	patchResult, err := patch.DefaultPatchMaker.Calculate(
		existingResource,
		desiredResource,
		patch.IgnoreField("kind"),
		patch.IgnoreField("apiVersion"),
	)
	if err != nil {
		return false, err
	}
	hasChanged := !patchResult.IsEmpty()
	if !hasChanged {
		return false, nil
	}

	if err = patch.DefaultAnnotator.SetLastAppliedAnnotation(desiredResource); err != nil {
		return false, err
	}

	err = m.Update(ctx, desiredResource)
	if err != nil {
		return false, err
	}

	if m.agent0ConnectorConfig.DevelopmentMode {
		logger.Info(fmt.Sprintf(
			"resource %s/%s was out of sync and has been reconciled",
			desiredResource.GetNamespace(),
			desiredResource.GetName(),
		),
			"patch",
			string(patchResult.Patch),
		)
	}

	return hasChanged, nil
}

func (m *Agent0ConnectorResourceManager) DeleteResources(
	ctx context.Context,
	logger logd.Logger,
) (bool, error) {
	logger.Info(
		fmt.Sprintf(
			"Deleting the agent0-connector Kubernetes resources in the Dash0 operator namespace %s (if existing).",
			m.agent0ConnectorConfig.OperatorNamespace,
		))
	desiredResources := assembleDesiredState(&m.agent0ConnectorConfig, nil)
	var allErrors []error
	resourcesHaveBeenDeleted := false
	for _, wrapper := range desiredResources {
		desiredResource := wrapper.object
		err := m.Delete(ctx, desiredResource)
		if err != nil {
			if apierrors.IsNotFound(err) {
				// A resource that we want to delete didn't exist in the first place, we can ignore this silently.
			} else {
				allErrors = append(allErrors, err)
			}
		} else {
			resourcesHaveBeenDeleted = true
			logger.Info(fmt.Sprintf(
				"deleted resource %s/%s",
				desiredResource.GetNamespace(),
				desiredResource.GetName(),
			))
		}
	}
	if len(allErrors) > 0 {
		return resourcesHaveBeenDeleted, errors.Join(allErrors...)
	}
	return resourcesHaveBeenDeleted, nil
}
