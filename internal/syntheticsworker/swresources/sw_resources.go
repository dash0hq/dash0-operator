// SPDX-FileCopyrightText: Copyright 2026 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package swresources

import (
	"context"
	"errors"
	"fmt"

	"github.com/cisco-open/k8s-objectmatcher/patch"
	appsv1 "k8s.io/api/apps/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	dash0common "github.com/dash0hq/dash0-operator/api/operator/common"
	dash0v1alpha1 "github.com/dash0hq/dash0-operator/api/operator/v1alpha1"
	"github.com/dash0hq/dash0-operator/internal/selfmonitoringapiaccess"
	"github.com/dash0hq/dash0-operator/internal/util"
	"github.com/dash0hq/dash0-operator/internal/util/logd"
	"github.com/dash0hq/dash0-operator/internal/util/resources"
)

// ErrMisconfigured is wrapped into every error that CreateOrUpdateSyntheticsWorkerResources returns for an invalid
// synthetics-worker configuration. Such an error is permanent, reconciling again with the same configuration fails
// identically. Callers must not requeue a reconcile request for it.
var ErrMisconfigured = errors.New("the synthetics-worker is misconfigured")

// ErrNoLocationID and ErrNoAuthorizationToken are the individual misconfigurations, so that a caller can report which
// one occurred. Both wrap ErrMisconfigured, hence errors.Is(err, ErrMisconfigured) matches either.
var (
	ErrNoLocationID         = fmt.Errorf("%w: no private location ID is configured", ErrMisconfigured)
	ErrNoAuthorizationToken = fmt.Errorf("%w: no Dash0 authorization token is available", ErrMisconfigured)
)

type SyntheticsWorkerResourceManager struct {
	client.Client
	scheme                    *runtime.Scheme
	operatorManagerDeployment *appsv1.Deployment
	syntheticsWorkerConfig    util.SyntheticsWorkerConfig
}

func NewSyntheticsWorkerResourceManager(
	k8sClient client.Client,
	scheme *runtime.Scheme,
	operatorManagerDeployment *appsv1.Deployment,
	syntheticsWorkerConfig util.SyntheticsWorkerConfig,
) *SyntheticsWorkerResourceManager {
	return &SyntheticsWorkerResourceManager{
		Client:                    k8sClient,
		scheme:                    scheme,
		operatorManagerDeployment: operatorManagerDeployment,
		syntheticsWorkerConfig:    syntheticsWorkerConfig,
	}
}

// CreateOrUpdateSyntheticsWorkerResources creates or updates the synthetics-worker resources based on the settings in
// spec.syntheticsWorker of the given Dash0OperatorConfiguration resource. In contrast to the agent0-connector, the
// per-cluster settings (location ID, authorization) live on that resource instead of on the operator manager's
// environment variables, since they can change at runtime without a Helm re-install.
func (m *SyntheticsWorkerResourceManager) CreateOrUpdateSyntheticsWorkerResources(
	ctx context.Context,
	operatorConfigurationResource *dash0v1alpha1.Dash0OperatorConfiguration,
	logger logd.Logger,
) (bool, bool, error) {
	if operatorConfigurationResource == nil {
		return false, false, fmt.Errorf("%w: no Dash0OperatorConfiguration resource is available", ErrMisconfigured)
	}
	spec := operatorConfigurationResource.Spec.SyntheticsWorker

	if spec.LocationID == "" {
		logger.ErrorTelemetryCollectionIssue(ErrNoLocationID, "no private location ID is configured "+
			"(spec.syntheticsWorker.locationId), not creating or updating the synthetics-worker resources")
		return false, false, ErrNoLocationID
	}

	authorization := dash0common.Authorization{}
	if spec.Authorization != nil {
		authorization = *spec.Authorization
	}
	authTokenEnvVar, err := util.CreateEnvVarForAuthorization(authorization, authTokenEnvVarName)
	if err != nil {
		logger.ErrorTelemetryCollectionIssue(err, "no Dash0 authorization token is available for the "+
			"synthetics-worker workload, not creating the synthetics-worker resources")
		return false, false, fmt.Errorf("%w: %w", ErrNoAuthorizationToken, err)
	}

	desiredState, err := assembleDesiredState(
		&m.syntheticsWorkerConfig,
		spec,
		&authTokenEnvVar,
		m.createSelfMonitoringInput(ctx, operatorConfigurationResource, logger),
	)
	if err != nil {
		logger.Error(err, "cannot assemble the desired state of the synthetics-worker resources")
		return false, false, err
	}

	resourcesHaveBeenCreated := false
	resourcesHaveBeenUpdated := false
	for _, wrapper := range desiredState {
		desiredResource := wrapper.object
		isNew, isChanged, err := m.createOrUpdateResource(ctx, desiredResource, logger)
		if err != nil {
			logger.Error(err, "error while creating/updating synthetics-worker resource")
			return resourcesHaveBeenCreated, resourcesHaveBeenUpdated, err
		} else if isNew {
			resourcesHaveBeenCreated = true
		} else if isChanged {
			resourcesHaveBeenUpdated = true
		}
	}

	return resourcesHaveBeenCreated, resourcesHaveBeenUpdated, nil
}

// createSelfMonitoringInput derives the self-monitoring settings for the synthetics-worker workload from the
// Dash0OperatorConfiguration resource. A configuration that cannot be derived disables self-monitoring.
func (m *SyntheticsWorkerResourceManager) createSelfMonitoringInput(
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
			m.syntheticsWorkerConfig.OperatorNamespace,
			operatorConfigurationResource,
			logger,
		)
	if err != nil {
		logger.Error(err, "cannot generate the self-monitoring configuration for the synthetics-worker workload")
		selfMonitoringConfiguration = selfmonitoringapiaccess.SelfMonitoringConfiguration{
			SelfMonitoringEnabled: false,
		}
	}
	// ConvertOperatorConfigurationResourceToSelfMonitoringConfiguration might have resolved Kubernetes secrets to
	// literal values, which is required for the in-process self-monitoring setup in the operator manager. The
	// synthetics-worker workload does not need access to resolved secret literals, it receives the secret refs as
	// environment variables of its container.
	selfMonitoringConfiguration.Token = nil
	selfMonitoringConfiguration.ResolvedSecretHeaderValues = nil

	return selfMonitoringInput{
		configuration: selfMonitoringConfiguration,
		clusterName:   operatorConfigurationResource.Spec.ClusterName,
	}
}

func (m *SyntheticsWorkerResourceManager) createOrUpdateResource(
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

func (m *SyntheticsWorkerResourceManager) createResource(
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

func (m *SyntheticsWorkerResourceManager) updateResource(
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

	if m.syntheticsWorkerConfig.DevelopmentMode {
		logger.Info(fmt.Sprintf(
			"resource %s/%s was out of sync and has been reconciled",
			desiredResource.GetNamespace(),
			desiredResource.GetName(),
		),
			"patch",
			util.RedactSensitiveEnvVarsInPatch(patchResult.Patch),
		)
	}

	return hasChanged, nil
}

func (m *SyntheticsWorkerResourceManager) DeleteResources(
	ctx context.Context,
	logger logd.Logger,
) (bool, error) {
	logger.Info(
		fmt.Sprintf(
			"Deleting the synthetics-worker Kubernetes resources in the Dash0 operator namespace %s (if existing).",
			m.syntheticsWorkerConfig.OperatorNamespace,
		))
	// A missing location ID/authorization or disabled self-monitoring are irrelevant for deleting the
	// synthetics-worker resources, only their names and namespaces matter.
	desiredResources, err := assembleDesiredState(
		&m.syntheticsWorkerConfig,
		dash0v1alpha1.SyntheticsWorker{},
		nil,
		selfMonitoringInput{},
	)
	if err != nil {
		return false, err
	}
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
