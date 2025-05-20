// SPDX-FileCopyrightText: Copyright 2024 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package otelcolresources

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"slices"
	"strings"
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
	Scheme                    *runtime.Scheme
	OperatorManagerDeployment *appsv1.Deployment
	// OTelCollectorNamePrefix is used as a prefix for OTel collector Kubernetes resources created by the operator, set
	// to value of the environment variable OTEL_COLLECTOR_NAME_PREFIX, which is set to the Helm release name by the
	// operator Helm chart.
	OTelCollectorNamePrefix          string
	ExtraConfig                      *util.ExtraConfig
	SendBatchMaxSize                 *uint32
	NodeIp                           string
	NodeName                         string
	PseudoClusterUID                 string
	IsIPv6Cluster                    bool
	IsDocker                         bool
	OffsetStorageVolume              *corev1.Volume
	DevelopmentMode                  bool
	DebugVerbosityDetailed           bool
	obsoleteResourcesHaveBeenDeleted atomic.Bool
	kubeletStatsReceiverConfig       atomic.Pointer[KubeletStatsReceiverConfig]
}

const (
	bogusDeploymentPatch = "{\"spec\":{\"strategy\":{\"$retainKeys\":[\"type\"],\"rollingUpdate\":null}}}"

	kubeletStatsNodeNameEndpoint         = "https://${env:K8S_NODE_NAME}:10250"
	kubeletStatsNodeIpEndpoint           = "https://${env:K8S_NODE_IP}:10250"
	kubetletStatsReadyOnlyEndpoint       = "http://${env:K8S_NODE_IP}:10255"
	kubeletStatsAuthTypeServiceAccount   = "serviceAccount"
	kubeletStatsAuthTypeNone             = "none"
	kubeletStatsSummaryPath              = "/stats/summary"
	usingKubeletStatsReceiverEndpointMsg = "Using %s as kubeletstats receiver endpoint."
)

var (
	knownIrrelevantPatches = []string{bogusDeploymentPatch}

	dummyImagesForDeletion = util.Images{
		OperatorImage:              "ghcr.io/dash0hq/operator-controller:latest",
		InitContainerImage:         "ghcr.io/dash0hq/instrumentation:latest",
		CollectorImage:             "ghcr.io/dash0hq/collector:latest",
		ConfigurationReloaderImage: "ghcr.io/dash0hq/configuration-reloader:latest",
		FilelogOffsetSyncImage:     "ghcr.io/dash0hq/filelog-offset-sync:latest",
	}

	httpClient     = &http.Client{}
	insecureClient = &http.Client{Transport: &http.Transport{
		TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
	}}
)

func NewOTelColResourceManager(
	k8sClient client.Client,
	scheme *runtime.Scheme,
	operatorManagerDeployment *appsv1.Deployment,
	oTelCollectorNamePrefix string,
	extraConfig *util.ExtraConfig,
	sendBatchMaxSize *uint32,
	nodeIp string,
	nodeName string,
	pseudoClusterUID string,
	isIPv6Cluster bool,
	isDocker bool,
	offsetStorageVolume *corev1.Volume,
	developmentMode bool,
	debugVerbosityDetailed bool,
) *OTelColResourceManager {
	return &OTelColResourceManager{
		Client:                    k8sClient,
		Scheme:                    scheme,
		OperatorManagerDeployment: operatorManagerDeployment,
		OTelCollectorNamePrefix:   oTelCollectorNamePrefix,
		ExtraConfig:               extraConfig,
		SendBatchMaxSize:          sendBatchMaxSize,
		NodeIp:                    nodeIp,
		NodeName:                  nodeName,
		PseudoClusterUID:          pseudoClusterUID,
		IsIPv6Cluster:             isIPv6Cluster,
		IsDocker:                  isDocker,
		OffsetStorageVolume:       offsetStorageVolume,
		DevelopmentMode:           developmentMode,
		DebugVerbosityDetailed:    debugVerbosityDetailed,
	}
}

func (m *OTelColResourceManager) CreateOrUpdateOpenTelemetryCollectorResources(
	ctx context.Context,
	operatorNamespace string,
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
		selfMonitoringConfiguration = selfmonitoringapiaccess.SelfMonitoringConfiguration{
			SelfMonitoringEnabled: false,
		}
	}

	kubernetesInfrastructureMetricsCollectionEnabled := true
	collectPodLabelsAndAnnotationsEnabled := true
	clusterName := ""
	if operatorConfigurationResource != nil {
		kubernetesInfrastructureMetricsCollectionEnabled =
			util.IsOptOutFlagWithDeprecatedVariantEnabled(
				//nolint:staticcheck
				operatorConfigurationResource.Spec.KubernetesInfrastructureMetricsCollectionEnabled,
				operatorConfigurationResource.Spec.KubernetesInfrastructureMetricsCollection.Enabled,
			)
		collectPodLabelsAndAnnotationsEnabled =
			util.ReadBoolPointerWithDefault(
				operatorConfigurationResource.Spec.CollectPodLabelsAndAnnotations.Enabled,
				true,
			)
		clusterName = operatorConfigurationResource.Spec.ClusterName
	}
	kubeletStatsReceiverConfig :=
		m.determineKubeletstatsReceiverEndpoint(
			kubernetesInfrastructureMetricsCollectionEnabled,
			logger,
		)

	config := &oTelColConfig{
		OperatorNamespace:           operatorNamespace,
		NamePrefix:                  m.OTelCollectorNamePrefix,
		Export:                      *export,
		SendBatchMaxSize:            m.SendBatchMaxSize,
		SelfMonitoringConfiguration: selfMonitoringConfiguration,
		KubernetesInfrastructureMetricsCollectionEnabled: kubernetesInfrastructureMetricsCollectionEnabled,
		CollectPodLabelsAndAnnotationsEnabled:            collectPodLabelsAndAnnotationsEnabled,
		KubeletStatsReceiverConfig:                       kubeletStatsReceiverConfig,
		// The hostmetrics receiver requires mapping the root file system as a volume mount, see
		// https://github.com/open-telemetry/opentelemetry-collector-contrib/tree/main/receiver/hostmetricsreceiver#collecting-host-metrics-from-inside-a-container-linux-only
		// This is apparently not supported in Docker, at least not in Docker Desktop. See
		// https://github.com/prometheus/node_exporter/issues/2002#issuecomment-801763211 and similar.
		// For this reason, we do not allow enabling the hostmetrics receiver when the node runtime is Docker.
		UseHostMetricsReceiver: kubernetesInfrastructureMetricsCollectionEnabled && !m.IsDocker,
		ClusterName:            clusterName,
		PseudoClusterUID:       m.PseudoClusterUID,
		Images:                 images,
		IsIPv6Cluster:          m.IsIPv6Cluster,
		OffsetStorageVolume:    m.OffsetStorageVolume,
		DevelopmentMode:        m.DevelopmentMode,
		DebugVerbosityDetailed: m.DebugVerbosityDetailed,
	}
	desiredState, err := assembleDesiredStateForUpsert(
		config,
		allMonitoringResources,
		m.ExtraConfig,
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
	if err = m.deleteObsoleteResourcesFromPreviousOperatorVersions(ctx, operatorNamespace, logger); err != nil {
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
			Namespace: m.OperatorManagerDeployment.Namespace,
			Name:      m.OperatorManagerDeployment.Name,
			UID:       m.OperatorManagerDeployment.UID,
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
	operatorNamespace string,
	logger *logr.Logger,
) (bool, error) {
	logger.Info(
		fmt.Sprintf(
			"Deleting the OpenTelemetry collector Kubernetes resources in the Dash0 operator namespace %s (if any exist).",
			operatorNamespace,
		))
	config := &oTelColConfig{
		OperatorNamespace: operatorNamespace,
		NamePrefix:        m.OTelCollectorNamePrefix,
		// For deleting the resources, we do not need the actual export settings; we only use assembleDesiredState to
		// collect the kinds and names of all resources that need to be deleted.
		Export:                      dash0v1alpha1.Export{},
		SendBatchMaxSize:            m.SendBatchMaxSize,
		SelfMonitoringConfiguration: selfmonitoringapiaccess.SelfMonitoringConfiguration{SelfMonitoringEnabled: false},
		// KubernetesInfrastructureMetricsCollectionEnabled=false would lead to not deleting the collector-deployment-
		// related resources, we always try to delete all collector resources (daemonset & deployment), no matter
		// whether both sets have been created earlier or not.
		KubernetesInfrastructureMetricsCollectionEnabled: true,
		UseHostMetricsReceiver:                           !m.IsDocker, // irrelevant for deletion
		Images:                                           dummyImagesForDeletion,
		IsIPv6Cluster:                                    m.IsIPv6Cluster,
		OffsetStorageVolume:                              m.OffsetStorageVolume,
		DevelopmentMode:                                  m.DevelopmentMode,
		DebugVerbosityDetailed:                           m.DebugVerbosityDetailed,
	}
	desiredResources, err := assembleDesiredStateForDelete(config, m.ExtraConfig)
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
	config oTelColConfig,
	desiredState []clientObject,
	logger *logr.Logger,
) error {
	// override actual config settings with settings that will produce all possible resources
	config.KubernetesInfrastructureMetricsCollectionEnabled = true

	allPossibleResources, err := assembleDesiredStateForDelete(&config, m.ExtraConfig)
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

func (m *OTelColResourceManager) determineKubeletstatsReceiverEndpoint(
	kubernetesInfrastructureMetricsCollectionEnabled bool,
	logger *logr.Logger,
) KubeletStatsReceiverConfig {
	cachedConfig := m.kubeletStatsReceiverConfig.Load()
	if cachedConfig != nil {
		return *cachedConfig
	}
	if m.NodeName == "" && m.NodeIp == "" {
		logger.Info("No K8s_NODE_NAME and no K8S_NODE_IP available, skipping kubeletstats receiver endpoint lookup. " +
			"The kubeletstats receiver will be disabled. Some Kubernetes infrastructure metrics will be missing.")
		return m.cacheKubeletStatsReceiverConfig(KubeletStatsReceiverConfig{Enabled: false})
	}
	if !kubernetesInfrastructureMetricsCollectionEnabled {
		// We do not need to run the test if we are not going to even use the kubeletstats receiver.
		logger.Info("Kubernetes infrastructure metrics collection is disabled, skipping kubeletstats receiver endpoint lookup.")
		return m.cacheKubeletStatsReceiverConfig(KubeletStatsReceiverConfig{Enabled: false})
	}

	logger.Info(fmt.Sprintf("Attempting DNS lookup for Kubernetes node name %s.", m.NodeName))
	_, err := net.LookupIP(m.NodeName)
	if err == nil {
		logger.Info(fmt.Sprintf("DNS lookup for Kubernetes node name %s has been successful.", m.NodeName))
		endpoint := kubeletStatsNodeNameEndpoint

		nodeNameEndpointWithPath := fmt.Sprintf("https://%s:10250%s", m.NodeName, kubeletStatsSummaryPath)
		logger.Info(fmt.Sprintf("Attempting probe request to %s.", nodeNameEndpointWithPath))
		err = executeHttpRequest(httpClient, nodeNameEndpointWithPath)
		if err == nil {
			logger.Info(fmt.Sprintf("Probe request request to %s has been successful.", nodeNameEndpointWithPath))
			logger.Info(fmt.Sprintf(usingKubeletStatsReceiverEndpointMsg, endpoint))
			return m.cacheKubeletStatsReceiverConfig(KubeletStatsReceiverConfig{
				Enabled:            true,
				Endpoint:           endpoint,
				AuthType:           kubeletStatsAuthTypeServiceAccount,
				InsecureSkipVerify: false,
			})
		} else if isTlsError(err) {
			logger.Info(
				fmt.Sprintf(
					"Failed to verify certificate for %s, adding insecure_skip_verify to kubeletstats receiver "+
						"configuration. (%s)",
					nodeNameEndpointWithPath,
					err.Error(),
				))
			logger.Info(fmt.Sprintf(usingKubeletStatsReceiverEndpointMsg, endpoint))
			return m.cacheKubeletStatsReceiverConfig(KubeletStatsReceiverConfig{
				Enabled:            true,
				Endpoint:           endpoint,
				AuthType:           kubeletStatsAuthTypeServiceAccount,
				InsecureSkipVerify: true,
			})
		} else {
			logger.Info(
				fmt.Sprintf(
					"DNS lookup for Kubernetes node name %s has been successful, but the probe request to %s resulted "+
						"in an error: %s. Will try using K8S_NODE_IP next.",
					m.NodeName,
					nodeNameEndpointWithPath,
					err.Error(),
				))
		}
	} else {
		logger.Info(
			fmt.Sprintf(
				"DNS lookup for Kubernetes node name %s failed with error: %s, will try K8S_NODE_IP next.",
				m.NodeName,
				err.Error(),
			))
	}

	nodeIpEndpointResultsInTlsError := false
	nodeIpEndpointWithPath := fmt.Sprintf("https://%s:10250%s", m.NodeIp, kubeletStatsSummaryPath)
	logger.Info(fmt.Sprintf("Attempting probe request %s.", nodeIpEndpointWithPath))
	err = executeHttpRequest(httpClient, nodeIpEndpointWithPath)
	if err == nil {
		endpoint := kubeletStatsNodeIpEndpoint
		logger.Info(fmt.Sprintf("Probe request request %s has been successful.", nodeIpEndpointWithPath))
		logger.Info(fmt.Sprintf(usingKubeletStatsReceiverEndpointMsg, endpoint))
		return m.cacheKubeletStatsReceiverConfig(KubeletStatsReceiverConfig{
			Enabled:            true,
			Endpoint:           endpoint,
			AuthType:           kubeletStatsAuthTypeServiceAccount,
			InsecureSkipVerify: false,
		})
	}
	if isTlsError(err) {
		logger.Info(
			fmt.Sprintf(
				"Failed to verify certificate for %s. (%s)",
				nodeIpEndpointWithPath,
				err.Error(),
			))
		nodeIpEndpointResultsInTlsError = true
	} else {
		logger.Info(
			fmt.Sprintf(
				"The probe request to %s resulted in an error: %s. Will try the read-only endpoint next.",
				nodeIpEndpointWithPath,
				err.Error(),
			))
	}

	readOnlyNodeIpEndpointWithPath := fmt.Sprintf("http://%s:10255%s", m.NodeIp, kubeletStatsSummaryPath)
	logger.Info(fmt.Sprintf("Attempting probe request to to read-only endpoint %s.", readOnlyNodeIpEndpointWithPath))
	err = executeHttpRequest(httpClient, readOnlyNodeIpEndpointWithPath)
	if err == nil {
		endpoint := kubetletStatsReadyOnlyEndpoint
		logger.Info(fmt.Sprintf("Probe request request to read-only endpoint %s has been successful.", readOnlyNodeIpEndpointWithPath))
		logger.Info(fmt.Sprintf("Using read-only endpoint %s as kubeletstats receiver endpoint.", endpoint))
		return m.cacheKubeletStatsReceiverConfig(KubeletStatsReceiverConfig{
			Enabled:            true,
			Endpoint:           endpoint,
			AuthType:           kubeletStatsAuthTypeNone,
			InsecureSkipVerify: false,
		})
	} else {
		logger.Info(
			fmt.Sprintf(
				"The probe request to %s resulted in an error: %s.",
				readOnlyNodeIpEndpointWithPath,
				err.Error(),
			))
	}

	if !nodeIpEndpointResultsInTlsError {
		return m.cacheKubeletStatsReceiverConfig(m.logErrorAndDisableKubeletStatsReceiver(logger))
	}

	logger.Info(
		fmt.Sprintf("Attempting probe request to %s without TLS certificate verification.", nodeIpEndpointWithPath))
	err = executeHttpRequest(insecureClient, nodeIpEndpointWithPath)
	if err == nil {
		endpoint := kubeletStatsNodeIpEndpoint
		logger.Info(
			fmt.Sprintf(
				"Probe request request to %s without TLS certificate verification has been successful.",
				nodeIpEndpointWithPath,
			))
		logger.Info(fmt.Sprintf("Using %s as kubeletstats receiver endpoint with insecure_skip_verify: true.", endpoint))
		return m.cacheKubeletStatsReceiverConfig(KubeletStatsReceiverConfig{
			Enabled:            true,
			Endpoint:           endpoint,
			AuthType:           kubeletStatsAuthTypeServiceAccount,
			InsecureSkipVerify: true,
		})
	} else {
		logger.Info(
			fmt.Sprintf(
				"The probe request to %s without TLS certificate verification resulted in an error: %s.",
				readOnlyNodeIpEndpointWithPath,
				err.Error(),
			))
	}

	return m.cacheKubeletStatsReceiverConfig(m.logErrorAndDisableKubeletStatsReceiver(logger))
}

func (m *OTelColResourceManager) cacheKubeletStatsReceiverConfig(config KubeletStatsReceiverConfig) KubeletStatsReceiverConfig {
	m.kubeletStatsReceiverConfig.Store(&config)
	return config
}

func executeHttpRequest(httpClient *http.Client, endpoint string) error {
	res, err := httpClient.Get(endpoint)
	defer func() {
		if res != nil {
			_, _ = io.Copy(io.Discard, res.Body)
			_ = res.Body.Close()
		}
	}()
	return err
}

func isTlsError(err error) bool {
	return strings.Contains(err.Error(), "tls: failed to verify certificate:")
}

func (m *OTelColResourceManager) logErrorAndDisableKubeletStatsReceiver(logger *logr.Logger) KubeletStatsReceiverConfig {
	logger.Error(
		fmt.Errorf("cannot determine viable endpoint for kubeletstats receiver endpoint, see above"),
		"The operator ran out of options when trying to find a viable kubeletstats receiver endpoint. The "+
			"kubeletstats receiver will be disabled. Some Kubernetes infrastructure metrics will be missing.")
	return KubeletStatsReceiverConfig{Enabled: false}
}
