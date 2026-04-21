// SPDX-FileCopyrightText: Copyright 2026 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package ieresources

import (
	"context"
	"errors"

	"github.com/cisco-open/k8s-objectmatcher/patch"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	dash0v1alpha1 "github.com/dash0hq/dash0-operator/api/operator/v1alpha1"
	"github.com/dash0hq/dash0-operator/internal/util/logd"
	"github.com/dash0hq/dash0-operator/internal/util/resources"
)

type IntelligentEdgeResourceManager struct {
	client.Client
	scheme                    *runtime.Scheme
	operatorManagerDeployment *appsv1.Deployment
	operatorNamespace         string
	namePrefix                string
	barkerImage               string
	barkerImagePullPolicy     corev1.PullPolicy
}

func NewIntelligentEdgeResourceManager(
	k8sClient client.Client,
	scheme *runtime.Scheme,
	operatorManagerDeployment *appsv1.Deployment,
	operatorNamespace string,
	namePrefix string,
	barkerImage string,
	barkerImagePullPolicy corev1.PullPolicy,
) *IntelligentEdgeResourceManager {
	return &IntelligentEdgeResourceManager{
		Client:                    k8sClient,
		scheme:                    scheme,
		operatorManagerDeployment: operatorManagerDeployment,
		operatorNamespace:         operatorNamespace,
		namePrefix:                namePrefix,
		barkerImage:               barkerImage,
		barkerImagePullPolicy:     barkerImagePullPolicy,
	}
}

func (m *IntelligentEdgeResourceManager) CreateOrUpdateResources(
	ctx context.Context,
	intelligentEdgeResource *dash0v1alpha1.Dash0IntelligentEdge,
	operatorConfig *dash0v1alpha1.Dash0OperatorConfiguration,
	logger logd.Logger,
) (bool, bool, error) {
	desiredState := assembleDesiredState(m.operatorNamespace, m.namePrefix, intelligentEdgeResource, operatorConfig, m.barkerImage, m.barkerImagePullPolicy, false, logger)

	resourcesHaveBeenCreated := false
	resourcesHaveBeenUpdated := false
	for _, wrapper := range desiredState {
		desiredResource := wrapper.object
		isNew, isChanged, err := m.createOrUpdateResource(ctx, desiredResource, logger)
		if err != nil {
			return resourcesHaveBeenCreated, resourcesHaveBeenUpdated, err
		} else if isNew {
			resourcesHaveBeenCreated = true
		} else if isChanged {
			resourcesHaveBeenUpdated = true
		}
	}

	if err := m.deleteResourcesThatAreNoLongerDesired(ctx, desiredState, logger); err != nil {
		return resourcesHaveBeenCreated, resourcesHaveBeenUpdated, err
	}

	return resourcesHaveBeenCreated, resourcesHaveBeenUpdated, nil
}

func (m *IntelligentEdgeResourceManager) createOrUpdateResource(
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
		if err = m.createResource(ctx, desiredResource, logger); err != nil {
			return false, false, err
		}
		return true, false, nil
	}
	hasChanged, err := m.updateResource(ctx, existingResource, desiredResource, logger)
	if err != nil {
		return false, false, err
	}
	return false, hasChanged, nil
}

func (m *IntelligentEdgeResourceManager) createResource(
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
	if err := m.Create(ctx, desiredResource); err != nil {
		return err
	}
	logger.Info("Created resource.", "namespace", desiredResource.GetNamespace(), "name", desiredResource.GetName())
	return nil
}

func (m *IntelligentEdgeResourceManager) updateResource(
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
	if patchResult.IsEmpty() {
		return false, nil
	}
	if err = patch.DefaultAnnotator.SetLastAppliedAnnotation(desiredResource); err != nil {
		return false, err
	}
	if err = m.Update(ctx, desiredResource); err != nil {
		return false, err
	}
	logger.Info("Updated resource.", "namespace", desiredResource.GetNamespace(), "name", desiredResource.GetName())
	return true, nil
}

func (m *IntelligentEdgeResourceManager) deleteResourcesThatAreNoLongerDesired(
	ctx context.Context,
	desiredState []clientObject,
	logger logd.Logger,
) error {
	allPossibleResources := assembleDesiredStateForDelete(m.operatorNamespace, m.namePrefix, logger)
	var allErrors []error
	for _, possibleResource := range allPossibleResources {
		isDesired := false
		for _, desiredResource := range desiredState {
			if desiredResource.object.GetName() == possibleResource.object.GetName() {
				isDesired = true
				break
			}
		}
		if !isDesired {
			if err := m.Delete(ctx, possibleResource.object); err != nil {
				if !apierrors.IsNotFound(err) {
					allErrors = append(allErrors, err)
				}
			} else {
				logger.Info("Deleted resource.", "namespace", possibleResource.object.GetNamespace(), "name", possibleResource.object.GetName())
			}
		}
	}
	if len(allErrors) > 0 {
		return errors.Join(allErrors...)
	}
	return nil
}

func (m *IntelligentEdgeResourceManager) DeleteResources(
	ctx context.Context,
	logger logd.Logger,
) (bool, error) {
	logger.Info("Deleting intelligent edge resources (if existing).", "namespace", m.operatorNamespace)
	desiredResources := assembleDesiredStateForDelete(m.operatorNamespace, m.namePrefix, logger)
	var allErrors []error
	resourcesHaveBeenDeleted := false
	for _, wrapper := range desiredResources {
		desiredResource := wrapper.object
		if err := m.Delete(ctx, desiredResource); err != nil {
			if !apierrors.IsNotFound(err) {
				allErrors = append(allErrors, err)
			}
		} else {
			resourcesHaveBeenDeleted = true
			logger.Info("Deleted resource.", "namespace", desiredResource.GetNamespace(), "name", desiredResource.GetName())
		}
	}
	if len(allErrors) > 0 {
		return resourcesHaveBeenDeleted, errors.Join(allErrors...)
	}
	return resourcesHaveBeenDeleted, nil
}
