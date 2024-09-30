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
	"github.com/dash0hq/dash0-operator/internal/dash0/selfmonitoring"
	"github.com/dash0hq/dash0-operator/internal/dash0/util"
)

type OTelColResourceManager struct {
	client.Client
	Scheme                           *runtime.Scheme
	DeploymentSelfReference          *appsv1.Deployment
	OTelCollectorNamePrefix          string
	OTelColResourceSpecs             *OTelColResourceSpecs
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
	monitoringResource *dash0v1alpha1.Dash0Monitoring,
	logger *logr.Logger,
) (bool, bool, error) {
	operatorConfigurationResource, err := m.findOperatorConfigurationResource(ctx, logger)
	if err != nil {
		return false, false, err
	}

	export := monitoringResource.Spec.Export
	if export == nil {
		if operatorConfigurationResource == nil {
			return false, false, fmt.Errorf("the provided Dash0Monitoring resource does not have an export " +
				"configuration and no Dash0OperatorConfiguration resource has been found")
		} else if operatorConfigurationResource.Spec.Export == nil {
			return false, false, fmt.Errorf("the provided Dash0Monitoring resource does not have an export " +
				"configuration and the Dash0OperatorConfiguration resource does not have one either")
		} else {
			export = operatorConfigurationResource.Spec.Export
		}
	}

	selfMonitoringConfiguration, err := selfmonitoring.ConvertOperatorConfigurationResourceToSelfMonitoringConfiguration(
		operatorConfigurationResource,
		logger,
	)
	if err != nil {
		selfMonitoringConfiguration = selfmonitoring.SelfMonitoringConfiguration{
			Enabled: false,
		}
	}

	config := &oTelColConfig{
		Namespace:                   namespace,
		NamePrefix:                  m.OTelCollectorNamePrefix,
		Export:                      *export,
		SelfMonitoringConfiguration: selfMonitoringConfiguration,
		Images:                      images,
		DevelopmentMode:             m.DevelopmentMode,
	}
	desiredState, err := assembleDesiredState(config, m.OTelColResourceSpecs, false)
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

	if err = m.deleteObsoleteResourcesFromPreviousOperatorVersions(ctx, namespace, logger); err != nil {
		return resourcesHaveBeenCreated, resourcesHaveBeenUpdated, err
	}

	return resourcesHaveBeenCreated, resourcesHaveBeenUpdated, nil
}

func (m *OTelColResourceManager) findOperatorConfigurationResource(
	ctx context.Context,
	logger *logr.Logger,
) (*dash0v1alpha1.Dash0OperatorConfiguration, error) {
	operatorConfigurationResource, err := util.FindUniqueOrMostRecentResourceInScope(
		ctx,
		m.Client,
		"", /* cluster-scope, thus no namespace */
		&dash0v1alpha1.Dash0OperatorConfiguration{},
		logger,
	)
	if err != nil {
		return nil, err
	}
	if operatorConfigurationResource == nil {
		return nil, nil
	}
	return operatorConfigurationResource.(*dash0v1alpha1.Dash0OperatorConfiguration), nil
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
	if m.DevelopmentMode {
		logger.Info(fmt.Sprintf(
			"checking whether resource %s/%s requires update",
			desiredResource.GetNamespace(),
			desiredResource.GetName(),
		))
	}

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
		if m.DevelopmentMode {
			logger.Info(fmt.Sprintf("resource %s/%s is already up to date",
				desiredResource.GetNamespace(),
				desiredResource.GetName(),
			))
		}
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
) error {
	config := &oTelColConfig{
		Namespace:  namespace,
		NamePrefix: m.OTelCollectorNamePrefix,
		// For deleting the resources, we do not need the actual export settings; we only use assembleDesiredState to
		// collect the kinds and names of all resources that need to be deleted.
		Export:                      dash0v1alpha1.Export{},
		SelfMonitoringConfiguration: selfmonitoring.SelfMonitoringConfiguration{Enabled: false},
		Images:                      dummyImagesForDeletion,
		DevelopmentMode:             m.DevelopmentMode,
	}
	desiredResources, err := assembleDesiredState(config, m.OTelColResourceSpecs, true)
	if err != nil {
		return err
	}
	var allErrors []error
	for _, wrapper := range desiredResources {
		desiredResource := wrapper.object
		err = m.Client.Delete(ctx, desiredResource)
		if err != nil {
			if apierrors.IsNotFound(err) {
				logger.Info(fmt.Sprintf(
					"wanted to delete resource %s/%s, but it did not exist",
					desiredResource.GetNamespace(),
					desiredResource.GetName(),
				))
			} else {
				allErrors = append(allErrors, err)
			}
		} else {
			logger.Info(fmt.Sprintf(
				"deleted resource %s/%s",
				desiredResource.GetNamespace(),
				desiredResource.GetName(),
			))
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
