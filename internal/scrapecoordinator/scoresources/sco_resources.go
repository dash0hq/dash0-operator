// SPDX-FileCopyrightText: Copyright 2026 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package scoresources

import (
	"context"
	"errors"
	"fmt"

	"github.com/cisco-open/k8s-objectmatcher/patch"
	appsv1 "k8s.io/api/apps/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	dash0v1alpha1 "github.com/dash0hq/dash0-operator/api/operator/v1alpha1"
	"github.com/dash0hq/dash0-operator/internal/selfmonitoringapiaccess"
	"github.com/dash0hq/dash0-operator/internal/util"
	"github.com/dash0hq/dash0-operator/internal/util/logd"
	"github.com/dash0hq/dash0-operator/internal/util/resources"
)

// ScrapeCoordinatorResourceManager creates, updates and deletes the Kubernetes resources of the scrape coordinator.
type ScrapeCoordinatorResourceManager struct {
	client.Client
	scheme                    *runtime.Scheme
	operatorManagerDeployment *appsv1.Deployment
	scrapeCoordinatorConfig   util.ScrapeCoordinatorConfig
}

// NewScrapeCoordinatorResourceManager creates a new ScrapeCoordinatorResourceManager.
func NewScrapeCoordinatorResourceManager(
	k8sClient client.Client,
	scheme *runtime.Scheme,
	operatorManagerDeployment *appsv1.Deployment,
	scrapeCoordinatorConfig util.ScrapeCoordinatorConfig,
) *ScrapeCoordinatorResourceManager {
	return &ScrapeCoordinatorResourceManager{
		Client:                    k8sClient,
		scheme:                    scheme,
		operatorManagerDeployment: operatorManagerDeployment,
		scrapeCoordinatorConfig:   scrapeCoordinatorConfig,
	}
}

// CreateOrUpdateScrapeCoordinatorResources creates the scrape coordinator resources or updates them to the desired
// state. It returns whether at least one resource has been created, and whether at least one has been updated.
func (m *ScrapeCoordinatorResourceManager) CreateOrUpdateScrapeCoordinatorResources(
	ctx context.Context,
	extraConfig util.ExtraConfig,
	operatorConfigurationResource *dash0v1alpha1.Dash0OperatorConfiguration,
	namespacesWithPrometheusScraping []string,
	logger logd.Logger,
) (bool, bool, error) {
	desiredState, err := assembleDesiredState(
		&m.scrapeCoordinatorConfig,
		namespacesWithPrometheusScraping,
		extraConfig,
		m.createSelfMonitoringInput(ctx, operatorConfigurationResource, logger),
	)
	if err != nil {
		return false, false, err
	}

	resourcesHaveBeenCreated := false
	resourcesHaveBeenUpdated := false
	for _, wrapper := range desiredState {
		desiredResource := wrapper.object
		if err = resources.SetOwnerReference(
			m.operatorManagerDeployment, operatorConfigurationResource, m.scheme, desiredResource, logger,
		); err != nil {
			return resourcesHaveBeenCreated, resourcesHaveBeenUpdated, err
		}
		isNew, isChanged, err := m.createOrUpdateResource(ctx, desiredResource, logger)
		if err != nil {
			logger.Error(err, "error while creating/updating scrape coordinator resource")
			return resourcesHaveBeenCreated, resourcesHaveBeenUpdated, err
		} else if isNew {
			resourcesHaveBeenCreated = true
		} else if isChanged {
			resourcesHaveBeenUpdated = true
		}
	}
	return resourcesHaveBeenCreated, resourcesHaveBeenUpdated, nil
}

// createSelfMonitoringInput derives the self-monitoring settings for the scrape coordinator from the
// Dash0OperatorConfiguration resource. A configuration that cannot be derived disables self-monitoring.
func (m *ScrapeCoordinatorResourceManager) createSelfMonitoringInput(
	ctx context.Context,
	operatorConfigurationResource *dash0v1alpha1.Dash0OperatorConfiguration,
	logger logd.Logger,
) selfMonitoringInput {
	if operatorConfigurationResource == nil {
		return selfMonitoringInput{}
	}
	selfMonitoringConfiguration, err :=
		selfmonitoringapiaccess.ConvertOperatorConfigurationResourceToSelfMonitoringConfiguration(
			ctx,
			m.Client,
			m.scrapeCoordinatorConfig.OperatorNamespace,
			operatorConfigurationResource,
			logger,
		)
	if err != nil {
		logger.Error(err, "cannot generate the self-monitoring configuration for the scrape coordinator")
		selfMonitoringConfiguration = selfmonitoringapiaccess.SelfMonitoringConfiguration{
			SelfMonitoringEnabled: false,
		}
	}
	// The resolved secret literals are only required for the in-process self-monitoring of the operator manager, the
	// scrape coordinator receives the secret refs as environment variables of its container.
	selfMonitoringConfiguration.Token = nil
	selfMonitoringConfiguration.ResolvedSecretHeaderValues = nil
	return selfMonitoringInput{
		configuration: selfMonitoringConfiguration,
		clusterName:   operatorConfigurationResource.Spec.ClusterName,
	}
}

func (m *ScrapeCoordinatorResourceManager) createOrUpdateResource(
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
	hasChanged, err := resources.UpdateWithRetryOnConflict(
		ctx,
		m.Client,
		desiredResource,
		func(existingResource client.Object, desiredResource client.Object) (bool, error) {
			return m.updateResource(ctx, existingResource, desiredResource, logger)
		},
	)
	if err != nil {
		return false, false, err
	}
	return false, hasChanged, nil
}

func (m *ScrapeCoordinatorResourceManager) createResource(
	ctx context.Context,
	desiredResource client.Object,
	logger logd.Logger,
) error {
	if err := patch.DefaultAnnotator.SetLastAppliedAnnotation(desiredResource); err != nil {
		return err
	}
	if err := m.Create(ctx, desiredResource); err != nil {
		return err
	}
	logger.Info(fmt.Sprintf("created resource %s/%s", desiredResource.GetNamespace(), desiredResource.GetName()))
	return nil
}

func (m *ScrapeCoordinatorResourceManager) updateResource(
	ctx context.Context,
	existingResource client.Object,
	desiredResource client.Object,
	logger logd.Logger,
) (bool, error) {
	desiredResourceForComparison := desiredResource.DeepCopyObject().(client.Object)
	resources.AdoptGkeAutopilotResourceAdjustments(existingResource, desiredResourceForComparison, logger)
	patchResult, err := patch.DefaultPatchMaker.Calculate(
		existingResource,
		desiredResourceForComparison,
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
	resources.PreserveKubectlRestartedAtAnnotation(existingResource, desiredResource, logger)
	// Carry over the live resource version, so that a concurrent modification results in a conflict and a retry.
	desiredResource.SetResourceVersion(existingResource.GetResourceVersion())
	if err = m.Update(ctx, desiredResource); err != nil {
		return false, err
	}

	if m.scrapeCoordinatorConfig.DevelopmentMode {
		logger.Info(fmt.Sprintf(
			"resource %s/%s was out of sync and has been reconciled",
			desiredResource.GetNamespace(),
			desiredResource.GetName(),
		),
			"patch",
			util.RedactSensitiveEnvVarsInPatch(patchResult.Patch),
		)
	}
	return true, nil
}

// DeleteResources deletes the scrape coordinator resources. It returns whether at least one resource has been deleted.
func (m *ScrapeCoordinatorResourceManager) DeleteResources(
	ctx context.Context,
	extraConfig util.ExtraConfig,
	logger logd.Logger,
) (bool, error) {
	logger.Info(fmt.Sprintf(
		"Deleting the scrape coordinator Kubernetes resources in the Dash0 operator namespace %s (if existing).",
		m.scrapeCoordinatorConfig.OperatorNamespace,
	))
	// Only the names and namespaces matter for deleting the resources.
	desiredResources, err := assembleDesiredState(&m.scrapeCoordinatorConfig, nil, extraConfig, selfMonitoringInput{})
	if err != nil {
		return false, err
	}
	var allErrors []error
	resourcesHaveBeenDeleted := false
	for _, wrapper := range desiredResources {
		desiredResource := wrapper.object
		if err = m.Delete(ctx, desiredResource); err != nil {
			if !apierrors.IsNotFound(err) {
				allErrors = append(allErrors, err)
			}
			continue
		}
		resourcesHaveBeenDeleted = true
		logger.Info(fmt.Sprintf("deleted resource %s/%s", desiredResource.GetNamespace(), desiredResource.GetName()))
	}
	if len(allErrors) > 0 {
		return resourcesHaveBeenDeleted, errors.Join(allErrors...)
	}
	return resourcesHaveBeenDeleted, nil
}
