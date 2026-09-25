// SPDX-FileCopyrightText: Copyright 2026 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package swresources

import (
	"context"
	"errors"
	"fmt"

	"github.com/cisco-open/k8s-objectmatcher/patch"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"

	dash0common "github.com/dash0hq/dash0-operator/api/operator/common"
	dash0v1alpha1 "github.com/dash0hq/dash0-operator/api/operator/v1alpha1"
	commonotel "github.com/dash0hq/dash0-operator/images/pkg/common"
	"github.com/dash0hq/dash0-operator/internal/selfmonitoringapiaccess"
	"github.com/dash0hq/dash0-operator/internal/util"
	"github.com/dash0hq/dash0-operator/internal/util/logd"
	"github.com/dash0hq/dash0-operator/internal/util/resources"
)

// ErrMisconfigured is wrapped into every error that CreateOrUpdateSyntheticsWorkerResources returns for an invalid
// synthetics-worker configuration. Such an error is permanent, reconciling again with the same configuration fails
// identically. Callers must not requeue a reconcile request for it.
var ErrMisconfigured = errors.New("the synthetics-worker is misconfigured")

// ErrNoAuthorizationToken is the one misconfiguration that can still occur per instance at reconcile time (LocationID
// itself is required and validated by the CRD schema). It wraps ErrMisconfigured, hence
// errors.Is(err, ErrMisconfigured) matches it.
var ErrNoAuthorizationToken = fmt.Errorf("%w: no Dash0 authorization token is available", ErrMisconfigured)

// InstanceResult reports the outcome of creating or updating one synthetics-worker instance's resources. Err is set
// when this particular instance is misconfigured or failed to reconcile; it does not prevent other instances from
// being reconciled.
type InstanceResult struct {
	LocationID string
	Created    bool
	Updated    bool
	Err        error

	// ReadyReplicas and DesiredReplicas are read back from the instance's Deployment after it has been created or
	// updated; both are zero when Err is set.
	ReadyReplicas   int32
	DesiredReplicas int32
}

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

// CreateOrUpdateSyntheticsWorkerResources creates or updates the resources for every instance configured in
// spec.syntheticsWorker.instances of the given Dash0OperatorConfiguration resource, and removes the resources of any
// instance that is no longer in that list. In contrast to the agent0-connector, the per-cluster settings (location
// ID, authorization) live on that resource instead of on the operator manager's environment variables, since they can
// change at runtime without a Helm re-install.
//
// One instance's misconfiguration (reported via InstanceResult.Err) does not prevent the others from being
// reconciled. The returned error is feature-wide (e.g. the orphan cleanup failed), not tied to any single instance.
func (m *SyntheticsWorkerResourceManager) CreateOrUpdateSyntheticsWorkerResources(
	ctx context.Context,
	operatorConfigurationResource *dash0v1alpha1.Dash0OperatorConfiguration,
	logger logd.Logger,
) ([]InstanceResult, error) {
	if operatorConfigurationResource == nil {
		return nil, fmt.Errorf("%w: no Dash0OperatorConfiguration resource is available", ErrMisconfigured)
	}
	instances := operatorConfigurationResource.Spec.SyntheticsWorker.Instances
	selfMonitoring := m.createSelfMonitoringInput(ctx, operatorConfigurationResource, logger)

	results := make([]InstanceResult, 0, len(instances))
	for _, instance := range instances {
		results = append(results, m.createOrUpdateInstance(ctx, instance, selfMonitoring, logger))
	}

	if err := m.deleteOrphanedResources(ctx, instances, logger); err != nil {
		return results, err
	}

	return results, nil
}

func (m *SyntheticsWorkerResourceManager) createOrUpdateInstance(
	ctx context.Context,
	instance dash0v1alpha1.SyntheticsWorkerInstance,
	selfMonitoring selfMonitoringInput,
	logger logd.Logger,
) InstanceResult {
	result := InstanceResult{LocationID: instance.LocationID}

	authorization := ptr.Deref(instance.Authorization, dash0common.Authorization{})
	authTokenEnvVar, err := util.CreateEnvVarForAuthorization(authorization, authTokenEnvVarName)
	if err != nil {
		logger.ErrorTelemetryCollectionIssue(err, fmt.Sprintf("no Dash0 authorization token is available for the "+
			"synthetics-worker instance for private location %s, not creating its resources", instance.LocationID))
		result.Err = fmt.Errorf("%w: %w", ErrNoAuthorizationToken, err)
		return result
	}

	desiredState, err := assembleDesiredState(
		&m.syntheticsWorkerConfig,
		instance,
		&authTokenEnvVar,
		selfMonitoring,
	)
	if err != nil {
		logger.Error(err, "cannot assemble the desired state of the synthetics-worker resources")
		result.Err = err
		return result
	}

	for _, wrapper := range desiredState {
		isNew, isChanged, err := m.createOrUpdateResource(ctx, wrapper.object, logger)
		if err != nil {
			logger.Error(err, "error while creating/updating synthetics-worker resource")
			result.Err = err
			return result
		}
		result.Created = result.Created || isNew
		result.Updated = result.Updated || isChanged
	}

	var deployment appsv1.Deployment
	deploymentKey := client.ObjectKey{
		Namespace: m.syntheticsWorkerConfig.OperatorNamespace,
		Name:      DeploymentName(m.syntheticsWorkerConfig.NamePrefix, instance.LocationID),
	}
	if err := m.Get(ctx, deploymentKey, &deployment); err != nil {
		logger.Error(err, "cannot read back the synthetics-worker deployment to determine its readiness")
		return result
	}
	result.ReadyReplicas = deployment.Status.ReadyReplicas
	result.DesiredReplicas = ptr.Deref(deployment.Spec.Replicas, defaultReplicas)

	return result
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
	if err := m.Create(ctx, desiredResource); err != nil {
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

	if err = m.Update(ctx, desiredResource); err != nil {
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

	return true, nil
}

// deleteOrphanedResources removes any synthetics-worker Deployment/ServiceAccount that does not belong to one of the
// given instances. Instance names are user-chosen and dynamic, so unlike the other resource managers' fixed-name
// cleanup, this lists the actually existing resources by their shared feature label instead of reconstructing a fixed
// set of names.
func (m *SyntheticsWorkerResourceManager) deleteOrphanedResources(
	ctx context.Context,
	instances []dash0v1alpha1.SyntheticsWorkerInstance,
	logger logd.Logger,
) error {
	desiredNames := make(map[string]struct{}, 2*len(instances))
	for _, instance := range instances {
		desiredNames[ServiceAccountName(m.syntheticsWorkerConfig.NamePrefix, instance.LocationID)] = struct{}{}
		desiredNames[DeploymentName(m.syntheticsWorkerConfig.NamePrefix, instance.LocationID)] = struct{}{}
	}
	_, err := m.deleteOrphaned(ctx, desiredNames, logger)
	return err
}

// deleteOrphaned deletes every Deployment and ServiceAccount carrying the synthetics-worker feature label whose name
// is not in desiredNames. An empty desiredNames set deletes every synthetics-worker resource of both kinds, which is
// how DeleteResources removes the whole feature.
func (m *SyntheticsWorkerResourceManager) deleteOrphaned(
	ctx context.Context,
	desiredNames map[string]struct{},
	logger logd.Logger,
) (bool, error) {
	var deploymentList appsv1.DeploymentList
	if err := m.List(
		ctx,
		&deploymentList,
		client.InNamespace(m.syntheticsWorkerConfig.OperatorNamespace),
		client.MatchingLabels(FeatureLabelSelector()),
	); err != nil {
		return false, err
	}
	var serviceAccountList corev1.ServiceAccountList
	if err := m.List(
		ctx,
		&serviceAccountList,
		client.InNamespace(m.syntheticsWorkerConfig.OperatorNamespace),
		client.MatchingLabels(FeatureLabelSelector()),
	); err != nil {
		return false, err
	}

	items := make([]client.Object, 0, len(deploymentList.Items)+len(serviceAccountList.Items))
	for i := range deploymentList.Items {
		items = append(items, &deploymentList.Items[i])
	}
	for i := range serviceAccountList.Items {
		items = append(items, &serviceAccountList.Items[i])
	}

	deletedAny := false
	var allErrors []error
	for _, item := range items {
		if _, desired := desiredNames[item.GetName()]; desired {
			continue
		}
		if err := m.Delete(ctx, item); err != nil && !apierrors.IsNotFound(err) {
			allErrors = append(allErrors, err)
			continue
		}
		deletedAny = true
		logger.Info(fmt.Sprintf("deleted orphaned resource %s/%s", item.GetNamespace(), item.GetName()))
	}
	if len(allErrors) > 0 {
		return deletedAny, errors.Join(allErrors...)
	}
	return deletedAny, nil
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

	protocol := selfmonitoringapiaccess.ConvertExportConfigurationToEnvVarSettings(selfMonitoringConfiguration.Export).Protocol
	// The synthetics-worker only builds gRPC OTel exporters.
	if selfMonitoringConfiguration.SelfMonitoringEnabled && protocol == commonotel.ProtocolHttpProtobuf {
		selfMonitoringConfiguration.SelfMonitoringEnabled = false
		logger.Info("self-monitoring is disabled for the synthetics-worker: the resolved export protocol " +
			"(http/protobuf) is not supported, only grpc is")
	}

	return selfMonitoringInput{
		configuration: selfMonitoringConfiguration,
		clusterName:   operatorConfigurationResource.Spec.ClusterName,
	}
}

// DeleteResources removes every synthetics-worker resource (of every instance) in the operator namespace. It is used
// when the feature is disabled or the Dash0OperatorConfiguration resource is gone, so there is no instance list left
// to derive names from; it lists resources by their shared feature label instead, the same mechanism
// deleteOrphanedResources uses to remove a single dropped instance.
func (m *SyntheticsWorkerResourceManager) DeleteResources(
	ctx context.Context,
	logger logd.Logger,
) (bool, error) {
	logger.Info(
		fmt.Sprintf(
			"Deleting the synthetics-worker Kubernetes resources in the Dash0 operator namespace %s (if existing).",
			m.syntheticsWorkerConfig.OperatorNamespace,
		))
	return m.deleteOrphaned(ctx, map[string]struct{}{}, logger)
}
