// SPDX-FileCopyrightText: Copyright 2025 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package taresources

import (
	"context"
	"errors"
	"fmt"

	"github.com/cisco-open/k8s-objectmatcher/patch"
	"github.com/go-logr/logr"
	appsv1 "k8s.io/api/apps/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/dash0hq/dash0-operator/internal/util"
	"github.com/dash0hq/dash0-operator/internal/util/resources"
)

type TargetAllocatorResourceManager struct {
	client.Client
	scheme                    *runtime.Scheme
	operatorManagerDeployment *appsv1.Deployment
	targetAllocatorConfig     util.TargetAllocatorConfig
}

func NewTargetAllocatorResourceManager(
	k8sClient client.Client,
	scheme *runtime.Scheme,
	operatorManagerDeployment *appsv1.Deployment,
	targetAllocatorConfig util.TargetAllocatorConfig,
) *TargetAllocatorResourceManager {
	return &TargetAllocatorResourceManager{
		Client:                    k8sClient,
		scheme:                    scheme,
		operatorManagerDeployment: operatorManagerDeployment,
		targetAllocatorConfig:     targetAllocatorConfig,
	}
}

func (m *TargetAllocatorResourceManager) CreateOrUpdateTargetAllocatorResources(
	ctx context.Context,
	extraConfig util.ExtraConfig,
	namespacesWithPrometheusScraping []string,
	logger *logr.Logger,
) (bool, bool, error) {
	config := &targetAllocatorConfig{
		OperatorNamespace:  m.targetAllocatorConfig.OperatorNamespace,
		NamePrefix:         m.targetAllocatorConfig.TargetAllocatorNamePrefix,
		CollectorComponent: m.targetAllocatorConfig.CollectorComponent,
		Images:             m.targetAllocatorConfig.Images,
		IsGkeAutopilot:     m.targetAllocatorConfig.IsGkeAutopilot,
	}

	desiredState, err := assembleDesiredStateForUpsert(config, namespacesWithPrometheusScraping, extraConfig)
	if err != nil {
		return false, false, err
	}

	resourcesHaveBeenCreated := false
	resourcesHaveBeenUpdated := false
	for _, wrapper := range desiredState {
		desiredResource := wrapper.object
		isNew, isChanged, err := m.createOrUpdateResource(
			ctx,
			desiredResource,
			logger,
		)
		if err != nil {
			logger.Error(err, "error while creating/updating allocator")
			return resourcesHaveBeenCreated, resourcesHaveBeenUpdated, err
		} else if isNew {
			resourcesHaveBeenCreated = true
		} else if isChanged {
			resourcesHaveBeenUpdated = true
		}
	}

	return resourcesHaveBeenCreated, resourcesHaveBeenUpdated, nil
}

func (m *TargetAllocatorResourceManager) createOrUpdateResource(
	ctx context.Context,
	desiredResource client.Object,
	logger *logr.Logger,
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
	} else {
		// object might need to be updated
		hasChanged, err := m.updateResource(ctx, existingResource, desiredResource, logger)
		if err != nil {
			return false, false, err
		}
		return false, hasChanged, err
	}
}

func (m *TargetAllocatorResourceManager) createResource(
	ctx context.Context,
	desiredResource client.Object,
	logger *logr.Logger,
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

func (m *TargetAllocatorResourceManager) updateResource(
	ctx context.Context,
	existingResource client.Object,
	desiredResource client.Object,
	logger *logr.Logger,
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

	if m.targetAllocatorConfig.DevelopmentMode {
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

func (m *TargetAllocatorResourceManager) DeleteResources(
	ctx context.Context,
	extraConfig util.ExtraConfig,
	logger *logr.Logger,
) (bool, error) {
	logger.Info(
		fmt.Sprintf(
			"Deleting the OpenTelemetry target-allocator Kubernetes resources in the Dash0 operator namespace %s (if existing).",
			m.targetAllocatorConfig.OperatorNamespace,
		))
	config := &targetAllocatorConfig{
		OperatorNamespace: m.targetAllocatorConfig.OperatorNamespace,
		NamePrefix:        m.targetAllocatorConfig.TargetAllocatorNamePrefix,
		IsGkeAutopilot:    m.targetAllocatorConfig.IsGkeAutopilot,
	}
	desiredResources, err := assembleDesiredStateForDelete(config, extraConfig)
	if err != nil {
		return false, err
	}
	var allErrors []error
	resourcesHaveBeenDeleted := false
	for _, wrapper := range desiredResources {
		desiredResource := wrapper.object
		err = m.Delete(ctx, desiredResource)
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
