// SPDX-FileCopyrightText: Copyright 2024 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package otelcolresources

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"sync/atomic"

	"github.com/cisco-open/k8s-objectmatcher/patch"
	"github.com/go-logr/logr"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	dash0v1alpha1 "github.com/dash0hq/dash0-operator/api/dash0monitoring/v1alpha1"
	"github.com/dash0hq/dash0-operator/internal/selfmonitoringapiaccess"
	"github.com/dash0hq/dash0-operator/internal/util"
)

type OTelColResourceManager struct {
	client.Client
	Scheme                           *runtime.Scheme
	DeploymentSelfReference          *appsv1.Deployment
	OTelCollectorNamePrefix          string
	OTelColResourceSpecs             *OTelColResourceSpecs
	IsIPv6Cluster                    bool
	IsDocker                         bool
	DevelopmentMode                  bool
	obsoleteResourcesHaveBeenDeleted atomic.Bool
}

const (
	bogusDeploymentPatch = "{\"spec\":{\"strategy\":{\"$retainKeys\":[\"type\"],\"rollingUpdate\":null}}}"
)

var (
	knownIrrelevantPatches = []string{bogusDeploymentPatch}

	dummyImagesForDeletion = util.Images{
		OperatorImage:              "ghcr.io/dash0hq/operator-controller:latest",
		InitContainerImage:         "ghcr.io/dash0hq/instrumentation:latest",
		CollectorImage:             "ghcr.io/dash0hq/collector:latest",
		ConfigurationReloaderImage: "ghcr.io/dash0hq/configuration-reloader:latest",
		FilelogOffsetSynchImage:    "ghcr.io/dash0hq/filelog-offset-synch:latest",
	}
)

func (m *OTelColResourceManager) CreateOrUpdateOpenTelemetryCollectorResources(
	ctx context.Context,
	namespace string,
	images util.Images,
	operatorConfigurationResource *dash0v1alpha1.Dash0OperatorConfiguration,
	allMonitoringResources []dash0v1alpha1.Dash0Monitoring,
	export *dash0v1alpha1.Export,
	logger *logr.Logger,
) (bool, bool, error) {
	if export == nil {
		return false, false, fmt.Errorf("cannot create or update Dash0 OpenTelemetry collectors without export settings")
	}

	selfMonitoringConfiguration, err :=
		selfmonitoringapiaccess.ConvertOperatorConfigurationResourceToSelfMonitoringConfiguration(
			operatorConfigurationResource,
			logger,
		)
	if err != nil {
		selfMonitoringConfiguration = selfmonitoringapiaccess.SelfMonitoringAndApiAccessConfiguration{
			SelfMonitoringEnabled: false,
		}
	}

	kubernetesInfrastructureMetricsCollectionEnabled := true
	clusterName := ""
	if operatorConfigurationResource != nil {
		kubernetesInfrastructureMetricsCollectionEnabled =
			util.ReadBoolPointerWithDefault(operatorConfigurationResource.Spec.KubernetesInfrastructureMetricsCollectionEnabled, true)
		clusterName = operatorConfigurationResource.Spec.ClusterName
	}

	config := &oTelColConfig{
		Namespace:                               namespace,
		NamePrefix:                              m.OTelCollectorNamePrefix,
		Export:                                  *export,
		SelfMonitoringAndApiAccessConfiguration: selfMonitoringConfiguration,
		KubernetesInfrastructureMetricsCollectionEnabled: kubernetesInfrastructureMetricsCollectionEnabled,
		// The hostmetrics receiver requires mapping the root file system as a volume mount, see
		// https://github.com/open-telemetry/opentelemetry-collector-contrib/tree/main/receiver/hostmetricsreceiver#collecting-host-metrics-from-inside-a-container-linux-only
		// This is apparently not supported in Docker, at least not in Docker Desktop. See
		// https://github.com/prometheus/node_exporter/issues/2002#issuecomment-801763211 and similar.
		// For this reason, we do not allow enabling the hostmetrics receiver when the node runtime is Docker.
		UseHostMetricsReceiver: kubernetesInfrastructureMetricsCollectionEnabled && !m.IsDocker,
		ClusterName:            clusterName,
		Images:                 images,
		IsIPv6Cluster:          m.IsIPv6Cluster,
		DevelopmentMode:        m.DevelopmentMode,
	}
	desiredState, err := assembleDesiredStateForUpsert(
		config,
		allMonitoringResources,
		m.OTelColResourceSpecs,
	)
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
			return resourcesHaveBeenCreated, resourcesHaveBeenUpdated, err
		} else if isNew {
			resourcesHaveBeenCreated = true
		} else if isChanged {
			resourcesHaveBeenUpdated = true
		}
	}

	if err = m.deleteResourcesThatAreNoLongerDesired(ctx, *config, desiredState, logger); err != nil {
		return resourcesHaveBeenCreated, resourcesHaveBeenUpdated, err
	}
	if err = m.deleteObsoleteResourcesFromPreviousOperatorVersions(ctx, namespace, logger); err != nil {
		return resourcesHaveBeenCreated, resourcesHaveBeenUpdated, err
	}

	return resourcesHaveBeenCreated, resourcesHaveBeenUpdated, nil
}

func (m *OTelColResourceManager) createOrUpdateResource(
	ctx context.Context,
	desiredResource client.Object,
	logger *logr.Logger,
) (bool, bool, error) {
	existingResource, err := m.createEmptyReceiverFor(desiredResource)
	if err != nil {
		return false, false, err
	}
	err = m.Client.Get(ctx, client.ObjectKeyFromObject(desiredResource), existingResource)
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

func (m *OTelColResourceManager) createEmptyReceiverFor(desiredResource client.Object) (client.Object, error) {
	objectKind := desiredResource.GetObjectKind()
	gvk := schema.GroupVersionKind{
		Group:   objectKind.GroupVersionKind().Group,
		Version: objectKind.GroupVersionKind().Version,
		Kind:    objectKind.GroupVersionKind().Kind,
	}
	runtimeObject, err := scheme.Scheme.New(gvk)
	if err != nil {
		return nil, err
	}
	return runtimeObject.(client.Object), nil
}

func (m *OTelColResourceManager) createResource(
	ctx context.Context,
	desiredResource client.Object,
	logger *logr.Logger,
) error {
	if err := m.setOwnerReference(desiredResource, logger); err != nil {
		return err
	}
	if err := patch.DefaultAnnotator.SetLastAppliedAnnotation(desiredResource); err != nil {
		return err
	}
	err := m.Client.Create(ctx, desiredResource)
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

func (m *OTelColResourceManager) updateResource(
	ctx context.Context,
	existingResource client.Object,
	desiredResource client.Object,
	logger *logr.Logger,
) (bool, error) {
	if err := m.setOwnerReference(desiredResource, logger); err != nil {
		return false, err
	}
	// This will change the collector daemonset and collector deployment one more time after it has been created
	// initially, by setting their own UID as an environment variable in all containers. Obviously, this cannot be done
	// when creating the daemonset/deployment. The next reconcile cycle after creating the resources will set the UID
	// environment variable, and modifying the containers will automatically restart them.
	m.amendDeploymentAndDaemonSetWithSelfReferenceUIDs(existingResource, desiredResource)

	patchResult, err := patch.DefaultPatchMaker.Calculate(
		existingResource,
		desiredResource,
		patch.IgnoreField("kind"),
		patch.IgnoreField("apiVersion"),
	)
	if err != nil {
		return false, err
	}
	hasChanged := !patchResult.IsEmpty() && !isKnownIrrelevantPatch(patchResult)
	if !hasChanged {
		return false, nil
	}

	if err = patch.DefaultAnnotator.SetLastAppliedAnnotation(desiredResource); err != nil {
		return false, err
	}

	err = m.Client.Update(ctx, desiredResource)
	if err != nil {
		return false, err
	}

	if m.DevelopmentMode {
		logger.Info(fmt.Sprintf(
			"resource %s/%s was out of sync and has been reconciled",
			desiredResource.GetNamespace(),
			desiredResource.GetName(),
		))
	}

	return hasChanged, nil
}

func isKnownIrrelevantPatch(patchResult *patch.PatchResult) bool {
	patch := string(patchResult.Patch)
	return slices.Contains(knownIrrelevantPatches, patch)
}

func (m *OTelColResourceManager) setOwnerReference(
	object client.Object,
	logger *logr.Logger,
) error {
	if object.GetNamespace() == "" {
		// cluster scoped resources like ClusterRole and ClusterRoleBinding cannot have a namespace-scoped owner.
		return nil
	}
	if err := controllerutil.SetControllerReference(&appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: m.DeploymentSelfReference.Namespace,
			Name:      m.DeploymentSelfReference.Name,
			UID:       m.DeploymentSelfReference.UID,
		},
	}, object, m.Scheme); err != nil {
		logger.Error(err, "cannot set owner reference on object")
		return err
	}
	return nil
}

func (m *OTelColResourceManager) amendDeploymentAndDaemonSetWithSelfReferenceUIDs(existingResource client.Object, desiredResource client.Object) {
	name := desiredResource.GetName()
	uid := existingResource.GetUID()
	if name == DaemonSetName(m.OTelCollectorNamePrefix) {
		daemonset := desiredResource.(*appsv1.DaemonSet)
		addSelfReferenceUidToAllContainers(&daemonset.Spec.Template.Spec.Containers, "K8S_DAEMONSET_UID", uid)
	} else if name == DeploymentName(m.OTelCollectorNamePrefix) {
		deployment := desiredResource.(*appsv1.Deployment)
		addSelfReferenceUidToAllContainers(&deployment.Spec.Template.Spec.Containers, "K8S_DEPLOYMENT_UID", uid)
	}
}

func addSelfReferenceUidToAllContainers(containers *[]corev1.Container, envVarName string, uid types.UID) {
	for i, container := range *containers {
		selfReferenceUidIsAlreadyPresent := slices.ContainsFunc(container.Env, func(envVar corev1.EnvVar) bool {
			return envVar.Name == envVarName
		})
		if !selfReferenceUidIsAlreadyPresent {
			container.Env = append(container.Env, corev1.EnvVar{
				Name:  envVarName,
				Value: string(uid),
			})
			(*containers)[i] = container
		}
	}
}

func (m *OTelColResourceManager) DeleteResources(
	ctx context.Context,
	namespace string,
	logger *logr.Logger,
) (bool, error) {
	logger.Info(
		fmt.Sprintf(
			"Deleting the OpenTelemetry collector Kubernetes resources in the Dash0 operator namespace %s (if any exist).",
			namespace,
		))
	config := &oTelColConfig{
		Namespace:  namespace,
		NamePrefix: m.OTelCollectorNamePrefix,
		// For deleting the resources, we do not need the actual export settings; we only use assembleDesiredState to
		// collect the kinds and names of all resources that need to be deleted.
		Export:                                  dash0v1alpha1.Export{},
		SelfMonitoringAndApiAccessConfiguration: selfmonitoringapiaccess.SelfMonitoringAndApiAccessConfiguration{SelfMonitoringEnabled: false},
		// KubernetesInfrastructureMetricsCollectionEnabled=false would lead to not deleting the collector-deployment-
		// related resources, we always try to delete all collector resources (daemonset & deployment), no matter
		// whether both sets have been created earlier or not.
		KubernetesInfrastructureMetricsCollectionEnabled: true,
		UseHostMetricsReceiver:                           !m.IsDocker, // irrelevant for deletion
		Images:                                           dummyImagesForDeletion,
		IsIPv6Cluster:                                    m.IsIPv6Cluster,
		DevelopmentMode:                                  m.DevelopmentMode,
	}
	desiredResources, err := assembleDesiredStateForDelete(config, m.OTelColResourceSpecs)
	if err != nil {
		return false, err
	}
	var allErrors []error
	resourcesHaveBeenDeleted := false
	for _, wrapper := range desiredResources {
		desiredResource := wrapper.object
		err = m.Client.Delete(ctx, desiredResource)
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

func (m *OTelColResourceManager) deleteResourcesThatAreNoLongerDesired(
	ctx context.Context,
	// deliberately accepting a value and not a pointer here, so we get a copy of the original config and do not
	// accidentally modify it.
	config oTelColConfig,
	desiredState []clientObject,
	logger *logr.Logger,
) error {
	// override actual config settings with settings that will produce all possible resources
	config.KubernetesInfrastructureMetricsCollectionEnabled = true

	allPossibleResources, err := assembleDesiredStateForDelete(&config, m.OTelColResourceSpecs)
	if err != nil {
		return err
	}
	var allErrors []error
	for _, possibleResource := range allPossibleResources {
		isDesired := false
		for _, desiredResource := range desiredState {
			// We could also compare the type meta information (group, version, kind) of desiredResource and
			// possibleResources here, to make sure we match objects correctly. Unfortunately the Client.Create call we
			// execute in createResource for some reason resets the TypeMeta of the desiredResource to zero values
			// (empty strings for group, version, and kind), so instead we match by name only. This works because we
			// use unique names for all resources.
			if desiredResource.object.GetName() == possibleResource.object.GetName() {
				isDesired = true
				break
			}
		}
		if !isDesired {
			err = m.Client.Delete(ctx, possibleResource.object)
			if err != nil {
				if apierrors.IsNotFound(err) {
					// expected, ignore silently
				} else {
					allErrors = append(allErrors, err)
				}
			} else {
				logger.Info(fmt.Sprintf(
					"deleted resource %s/%s",
					possibleResource.object.GetNamespace(),
					possibleResource.object.GetName(),
				))
			}
		}
	}

	if len(allErrors) > 0 {
		return errors.Join(allErrors...)
	}
	return nil
}

func (m *OTelColResourceManager) deleteObsoleteResourcesFromPreviousOperatorVersions(
	ctx context.Context,
	namespace string,
	logger *logr.Logger,
) error {
	if m.obsoleteResourcesHaveBeenDeleted.Load() {
		return nil
	}
	obsoleteResources := compileObsoleteResources(
		namespace,
		m.OTelCollectorNamePrefix,
	)
	var allErrors []error
	for _, obsoleteResource := range obsoleteResources {
		err := m.Client.Delete(ctx, obsoleteResource)
		if err != nil {
			if apierrors.IsNotFound(err) {
				// expected, ignore silently
			} else {
				allErrors = append(allErrors, err)
			}
		} else {
			logger.Info(fmt.Sprintf(
				"deleted obsolete resource %s/%s",
				obsoleteResource.GetNamespace(),
				obsoleteResource.GetName(),
			))
		}
	}
	if len(allErrors) > 0 {
		return errors.Join(allErrors...)
	}

	m.obsoleteResourcesHaveBeenDeleted.Store(true)
	return nil
}
