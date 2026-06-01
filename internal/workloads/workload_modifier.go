// SPDX-FileCopyrightText: Copyright 2024 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package workloads

import (
	"fmt"
	"maps"
	"reflect"
	"slices"
	"strconv"
	"strings"

	appsv1 "k8s.io/api/apps/v1"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	dash0v1beta1 "github.com/dash0hq/dash0-operator/api/operator/v1beta1"
	"github.com/dash0hq/dash0-operator/images/pkg/common"
	"github.com/dash0hq/dash0-operator/internal/util"
	"github.com/dash0hq/dash0-operator/internal/util/logd"
	"github.com/dash0hq/dash0-operator/internal/util/pointers"
)

type containerHasServiceAttributes struct {
	serviceName      bool
	serviceNamespace bool
	serviceVersion   bool
}

const (
	initContainerName = "dash0-instrumentation"

	dash0VolumeName                                                     = "dash0-instrumentation"
	dash0DirectoryEnvVarName                                            = "DASH0_INSTRUMENTATION_FOLDER_DESTINATION"
	dash0CopyInstrumentationDebugEnvVarName                             = "DASH0_COPY_INSTRUMENTATION_DEBUG"
	otelAutoInstrumentationBaseDirectory                                = "/__otel_auto_instrumentation"
	imageVolumeSubPath                                                  = "dash0-instrumentation"
	envVarLdPreloadName                                                 = "LD_PRELOAD"
	envVarLdPreloadValue                                                = "/__otel_auto_instrumentation/injector/libotelinject.so"
	envVarOtelInjectorConfigFileName                                    = "OTEL_INJECTOR_CONFIG_FILE"
	envVarOtelInjectorConfigFileValue                                   = "/__otel_auto_instrumentation/injector/injector.conf"
	envVarOtelInjectorConfigFilePythonEnabledValue                      = "/__otel_auto_instrumentation/injector/injector-with-python.conf"
	envVarOtelExporterOtlpEndpointName                                  = "OTEL_EXPORTER_OTLP_ENDPOINT"
	envVarOtelExporterOtlpProtocolName                                  = "OTEL_EXPORTER_OTLP_PROTOCOL"
	envVarOtelLogsExporterName                                          = "OTEL_LOGS_EXPORTER"
	envVarOtelLogsExporterNoneValue                                     = "none"
	envVarDash0CollectorBaseUrlName                                     = "DASH0_OTEL_COLLECTOR_BASE_URL"
	envVarOTelInjectorNamespaceName                                     = "OTEL_INJECTOR_K8S_NAMESPACE_NAME"
	envVarOTelInjectorPodName                                           = "OTEL_INJECTOR_K8S_POD_NAME"
	envVarOTelInjectorPodUidName                                        = "OTEL_INJECTOR_K8S_POD_UID"
	envVarOTelInjectorContainerName                                     = "OTEL_INJECTOR_K8S_CONTAINER_NAME"
	envVarOTelInjectorServiceName                                       = "OTEL_INJECTOR_SERVICE_NAME"
	envVarOTelInjectorServiceNamespace                                  = "OTEL_INJECTOR_SERVICE_NAMESPACE"
	envVarOTelInjectorServiceVersionName                                = "OTEL_INJECTOR_SERVICE_VERSION"
	envVarOTelInjectorResourceAttributesName                            = "OTEL_INJECTOR_RESOURCE_ATTRIBUTES"
	otelInjectorLogLevelEnvVarName                                      = "OTEL_INJECTOR_LOG_LEVEL"
	envVarOtelInstrumentationJdbcExperimentalCaptureQueryParametersName = "OTEL_INSTRUMENTATION_JDBC_EXPERIMENTAL_CAPTURE_QUERY_PARAMETERS"
	envVarOtelDotnetExperimentalSqlClientCaptureQueryParametersName     = "OTEL_DOTNET_EXPERIMENTAL_SQLCLIENT_ENABLE_TRACE_DB_QUERY_PARAMETERS"
	envVarOtelDotnetExperimentalEfCoreCaptureQueryParametersName        = "OTEL_DOTNET_EXPERIMENTAL_EFCORE_ENABLE_TRACE_DB_QUERY_PARAMETERS"
	captureSqlQueryParametersValueTrue                                  = "true"

	serviceName      = "service.name"
	serviceNamespace = "service.namespace"
	serviceVersion   = "service.version"

	defaultOtelExporterOtlpProtocol = common.ProtocolHttpProtobuf

	resourcesOpenTelemetryIoLabelPrefix   = "resource.opentelemetry.io/"
	safeToEviceLocalVolumesAnnotationName = "cluster-autoscaler.kubernetes.io/safe-to-evict-local-volumes"

	// legacy environment variables
	legacyEnvVarNodeOptionsName       = "NODE_OPTIONS"
	legacyEnvVarNodeOptionsValue      = "--require /__dash0__/instrumentation/node.js/node_modules/@dash0hq/opentelemetry"
	legacyEnvVarDash0InjectorLogLevel = "DASH0_INJECTOR_LOG_LEVEL"

	// LD_PRELOAD value until release 0.96.0
	envVarLdPreloadLegacyValue = "/__dash0__/dash0_injector.so"
)

var (
	defaultInitContainerUser  int64 = 13020
	defaultInitContainerGroup int64 = 13020

	serviceAttributes = []string{serviceName, serviceNamespace, serviceVersion}

	// CaptureSqlQueryParametersEnvVarNames are the env vars set when captureSqlQueryParameters is enabled.
	CaptureSqlQueryParametersEnvVarNames = []string{
		envVarOtelInstrumentationJdbcExperimentalCaptureQueryParametersName,
		envVarOtelDotnetExperimentalSqlClientCaptureQueryParametersName,
		envVarOtelDotnetExperimentalEfCoreCaptureQueryParametersName,
	}

	otelExporterOtlpNoOverwriteMsg = fmt.Sprintf(
		"Dash0 will not set %s/%s since the container already has at least one of those environment "+
			"variables set, this container's telemetry will be routed according to the existing settings.",
		envVarOtelExporterOtlpEndpointName,
		envVarOtelExporterOtlpProtocolName,
	)

	// Do not instrument workloads via init container and EmptyDir instrumentation volume when one of its containers has
	// an ephemeral storage limit below this threshold. The EmptyDir adds about 400 MB of ephemeral storage; on top of
	// that, the container's ephemeral-storage budget also has to cover its own writable layer plus logs, we budget ~100M
	// for that.  Containers with a tighter limit are skipped to avoid evictions caused by adding the instrumentation
	// volume.
	ephemeralStorageLimitThresholdMB    int64 = 500
	ephemeralStorageLimitThresholdBytes       = ephemeralStorageLimitThresholdMB * 1000 * 1000
	ephemeralStorageLimitThresholdLabel       = strconv.FormatInt(ephemeralStorageLimitThresholdMB, 10) + "M"
)

var (
	NoModificationReasonUnknown NoModificationReasonMessage = func(actor util.WorkloadModifierActor) string {
		return fmt.Sprintf("No modification by the %s occurred, reason unknown.", actor)
	}
	NoModificationReasonNoChanges NoModificationReasonMessage = func(actor util.WorkloadModifierActor) string {
		return fmt.Sprintf("Dash0 instrumentation was already present on this workload, no modification by the %s is necessary.", actor)
	}
	NoModificationReasonError NoModificationReasonMessage = func(actor util.WorkloadModifierActor) string {
		return fmt.Sprintf("Dash0 instrumentation by %s has not been successful.", actor)
	}
	NoModificationReasonOwnedByHigherOrderWorkload NoModificationReasonMessage = func(actor util.WorkloadModifierActor) string {
		return fmt.Sprintf("The workload is part of a higher order workload that will be instrumented by the webhook, no modification by the %s is necessary.", actor)
	}
	NoModificationReasonImmutableWorkloadCannotBeInstrumented NoModificationReasonMessage = func(actor util.WorkloadModifierActor) string {
		return "cannot instrumentation this workload, since this type of workload is immutable"
	}
	NoModificationReasonImmutableWorkloadCannotBeReverted NoModificationReasonMessage = func(actor util.WorkloadModifierActor) string {
		return "cannot remove the instrumentation from workload, since this type of workload is immutable"
	}
	NoModificationReasonIgnoreOnceLabel NoModificationReasonMessage = func(_ util.WorkloadModifierActor) string {
		return "Ignoring this admission request due to the presence of dash0.com/webhook-ignore-once."
	}
)

type NoModificationReasonMessage func(actor util.WorkloadModifierActor) string

type ModificationResult struct {
	HasBeenModified                   bool
	RenderReasonMessage               NoModificationReasonMessage
	ContainersTotal                   int
	InstrumentationIssuesPerContainer map[string][]string
	LogAtDebugOnly                    bool
	IgnoredOnce                       bool
	ImmutableWorkload                 bool
}

func NewHasBeenModifiedResult(containersTotal int, instrumentationIssuesPerContainer map[string][]string) ModificationResult {
	return ModificationResult{
		HasBeenModified:                   true,
		ContainersTotal:                   containersTotal,
		InstrumentationIssuesPerContainer: instrumentationIssuesPerContainer,
	}
}

func NewNotModifiedReasonUnknownResult() ModificationResult {
	return newNotModifiedResult(NoModificationReasonUnknown)
}

func NewNotModifiedDueToErrorResult() ModificationResult {
	return newNotModifiedResult(NoModificationReasonError)
}

func NewNotModifiedOwnedByHigherOrderWorkloadResult() ModificationResult {
	r := newNotModifiedResult(NoModificationReasonOwnedByHigherOrderWorkload)
	r.LogAtDebugOnly = true
	return r
}

func NewNotModifiedUnsupportedOperatingSystemResult(details string) ModificationResult {
	return newNotModifiedResult(func(actor util.WorkloadModifierActor) string {
		return fmt.Sprintf(
			"The %s has not modified this workload since it seems to be targeting a non-Linux operating system, workload "+
				"modifications are only supported for Linux workloads. Details: %s", actor, details)
	})
}

func NewNotModifiedEphemeralStorageLimitTooLowResult(containerName string, limit string) ModificationResult {
	return newNotModifiedResult(func(actor util.WorkloadModifierActor) string {
		return fmt.Sprintf(
			"The %s has not modified this workload since container %q has an ephemeral-storage limit of %s, which is below "+
				"the required threshold of %s. The Dash0 init-container instrumentation requires at least %s of ephemeral "+
				"storage, raise the limit or remove it to enable instrumentation, or use image volume instrumentation "+
				"delivery.",
			actor, containerName, limit, ephemeralStorageLimitThresholdLabel, ephemeralStorageLimitThresholdLabel)
	})
}

func NewNotModifiedNoChangesResult() ModificationResult {
	return newNotModifiedResult(NoModificationReasonNoChanges)
}

func NewNotModifiedImmutableWorkloadCannotBeInstrumentedResult() ModificationResult {
	return ModificationResult{
		HasBeenModified:     false,
		RenderReasonMessage: NoModificationReasonImmutableWorkloadCannotBeInstrumented,
		ImmutableWorkload:   true,
	}
}

func NewNotModifiedImmutableWorkloadCannotBeRevertedResult() ModificationResult {
	return ModificationResult{
		HasBeenModified:     false,
		RenderReasonMessage: NoModificationReasonImmutableWorkloadCannotBeReverted,
		ImmutableWorkload:   true,
	}
}

func NewIgnoredOnceResult() ModificationResult {
	return ModificationResult{
		HasBeenModified:     false,
		RenderReasonMessage: NoModificationReasonIgnoreOnceLabel,
		IgnoredOnce:         true,
	}
}

func newNotModifiedResult(
	reason NoModificationReasonMessage,
) ModificationResult {
	return ModificationResult{
		HasBeenModified:     false,
		RenderReasonMessage: reason,
	}
}

type modifyPodSpecResult struct {
	hasBeenModified                   bool
	instrumentationIssuesPerContainer map[string][]string
}

func InstrumentationIsUpToDate(
	objectMeta *metav1.ObjectMeta,
	containers []corev1.Container,
	clusterInstrumentationConfig *util.ClusterInstrumentationConfig,
	namespaceInstrumentationConfig dash0v1beta1.NamespaceInstrumentationConfig,
) bool {
	if !util.HasBeenInstrumentedSuccessfullyByThisVersion(objectMeta, clusterInstrumentationConfig.Images) {
		return false
	}
	if otelExportEnvVarsWillBeUpdatedForAtLeastOneContainer(containers, clusterInstrumentationConfig) {
		return false
	}
	if otelPropagatorsEnvVarWillBeUpdatedForAtLeastOneContainer(containers, namespaceInstrumentationConfig) {
		return false
	}
	if otelInjectorConfEnvVarWillBeUpdatedForAtLeastOneContainer(containers, clusterInstrumentationConfig) {
		return false
	}
	if otelLogsExporterEnvVarWillBeUpdatedForAtLeastOneContainer(containers, namespaceInstrumentationConfig) {
		return false
	}
	if captureSqlQueryParametersEnvVarWillBeUpdatedForAtLeastOneContainer(containers, namespaceInstrumentationConfig) {
		return false
	}
	return true
}

// otelLogsExporterEnvVarWillBeUpdatedForAtLeastOneContainer returns true if the OTEL_LOGS_EXPORTER env var on any
// container would be added or removed by the workload modifier based on the current LogCollectionEnabled setting.
func otelLogsExporterEnvVarWillBeUpdatedForAtLeastOneContainer(
	containers []corev1.Container,
	namespaceInstrumentationConfig dash0v1beta1.NamespaceInstrumentationConfig,
) bool {
	for _, container := range containers {
		envVar := util.GetEnvVar(new(container), envVarOtelLogsExporterName)
		if namespaceInstrumentationConfig.LogCollectionEnabled {
			// We want to add OTEL_LOGS_EXPORTER=none, but only if the user hasn't set a value via pod spec env.
			if util.IsEnvVarUnsetOrEmpty(envVar) {
				return true
			}
		} else if namespaceInstrumentationConfig.PreviousLogCollectionEnabled {
			// We want to remove OTEL_LOGS_EXPORTER=none that we previously set.
			if envVar != nil && envVar.ValueFrom == nil && envVar.Value == envVarOtelLogsExporterNoneValue {
				return true
			}
		}
	}
	return false
}

type ResourceModifier struct {
	// configuration values relevant for instrumenting workloads which apply to the whole cluster, e.g. settings from
	// the helm chart or the operator configuration resource.
	clusterInstrumentationConfig *util.ClusterInstrumentationConfig

	// configuration values relevant for instrumenting workloads which apply to one namespace, e.g. settings from the
	// monitoring resource.
	namespaceInstrumentationConfig dash0v1beta1.NamespaceInstrumentationConfig

	// the name of the component that applies the resource modifications, this will be written to the
	// dash0.com/instrumented-by label
	actor util.WorkloadModifierActor

	// the logger to use for logging messages during the resource modification process
	logger logd.Logger
}

func NewResourceModifier(
	clusterInstrumentationConfig *util.ClusterInstrumentationConfig,
	namespaceInstrumentationConfig dash0v1beta1.NamespaceInstrumentationConfig,
	actor util.WorkloadModifierActor,
	logger logd.Logger,
) *ResourceModifier {
	return &ResourceModifier{
		clusterInstrumentationConfig:   clusterInstrumentationConfig,
		namespaceInstrumentationConfig: namespaceInstrumentationConfig,
		actor:                          actor,
		logger:                         logger,
	}
}

func (m *ResourceModifier) ModifyCronJob(cronJob *batchv1.CronJob) ModificationResult {
	return m.modifyResource(
		&cronJob.Spec.JobTemplate.Spec.Template,
		&cronJob.ObjectMeta,
		&cronJob.Spec.JobTemplate.Spec.Template.ObjectMeta,
	)
}

func (m *ResourceModifier) ModifyDaemonSet(daemonSet *appsv1.DaemonSet) ModificationResult {
	return m.modifyResource(
		&daemonSet.Spec.Template,
		&daemonSet.ObjectMeta,
		&daemonSet.Spec.Template.ObjectMeta,
	)
}

func (m *ResourceModifier) ModifyDeployment(deployment *appsv1.Deployment) ModificationResult {
	return m.modifyResource(
		&deployment.Spec.Template,
		&deployment.ObjectMeta,
		&deployment.Spec.Template.ObjectMeta,
	)
}

func (m *ResourceModifier) ModifyJob(job *batchv1.Job) ModificationResult {
	return m.modifyResource(
		&job.Spec.Template,
		&job.ObjectMeta,
		&job.Spec.Template.ObjectMeta,
	)
}

func (m *ResourceModifier) AddLabelsToImmutableJob(job *batchv1.Job) ModificationResult {
	util.AddInstrumentationLabelsAndAnnotations(&job.ObjectMeta, false, m.clusterInstrumentationConfig, m.actor)
	// adding labels always works and is a modification that requires an update
	return NewHasBeenModifiedResult(len(job.Spec.Template.Spec.Containers), nil)
}

func (m *ResourceModifier) ModifyPod(pod *corev1.Pod) ModificationResult {
	if m.hasMatchingOwnerReference(pod, []metav1.TypeMeta{
		util.K8sTypeMetaDaemonSet,
		util.K8sTypeMetaReplicaSet,
		util.K8sTypeMetaStatefulSet,
		util.K8sTypeMetaCronJob,
		util.K8sTypeMetaJob,
	}) {
		return NewNotModifiedOwnedByHigherOrderWorkloadResult()
	}
	if isIneligibleForModificationResult := m.checkEligibleForModification(&pod.Spec); isIneligibleForModificationResult != nil {
		return *isIneligibleForModificationResult
	}
	podSpecResult := m.modifyPodSpec(&pod.Spec, &pod.ObjectMeta, &pod.ObjectMeta)
	if !podSpecResult.hasBeenModified {
		return NewNotModifiedNoChangesResult()
	}
	util.AddInstrumentationLabelsAndAnnotations(&pod.ObjectMeta, true, m.clusterInstrumentationConfig, m.actor)
	return NewHasBeenModifiedResult(len(pod.Spec.Containers), podSpecResult.instrumentationIssuesPerContainer)
}

func (m *ResourceModifier) ModifyReplicaSet(replicaSet *appsv1.ReplicaSet) ModificationResult {
	if m.hasMatchingOwnerReference(replicaSet, []metav1.TypeMeta{util.K8sTypeMetaDeployment}) {
		return NewNotModifiedOwnedByHigherOrderWorkloadResult()
	}
	return m.modifyResource(&replicaSet.Spec.Template, &replicaSet.ObjectMeta, &replicaSet.Spec.Template.ObjectMeta)
}

func (m *ResourceModifier) ModifyStatefulSet(statefulSet *appsv1.StatefulSet) ModificationResult {
	return m.modifyResource(&statefulSet.Spec.Template, &statefulSet.ObjectMeta, &statefulSet.Spec.Template.ObjectMeta)
}

func (m *ResourceModifier) modifyResource(
	podTemplateSpec *corev1.PodTemplateSpec,
	workloadMeta *metav1.ObjectMeta,
	podMeta *metav1.ObjectMeta,
) ModificationResult {
	if isIneligibleForModificationResult := m.checkEligibleForModification(&podTemplateSpec.Spec); isIneligibleForModificationResult != nil {
		return *isIneligibleForModificationResult
	}
	podSpecResult := m.modifyPodSpec(&podTemplateSpec.Spec, workloadMeta, podMeta)
	if !podSpecResult.hasBeenModified {
		return NewNotModifiedNoChangesResult()
	}
	util.AddInstrumentationLabelsAndAnnotations(workloadMeta, true, m.clusterInstrumentationConfig, m.actor)
	util.AddInstrumentationLabelsAndAnnotations(&podTemplateSpec.ObjectMeta, true, m.clusterInstrumentationConfig, m.actor)
	return NewHasBeenModifiedResult(len(podTemplateSpec.Spec.Containers), podSpecResult.instrumentationIssuesPerContainer)
}

func (m *ResourceModifier) modifyPodSpec(
	podSpec *corev1.PodSpec,
	workloadMeta *metav1.ObjectMeta,
	podMeta *metav1.ObjectMeta,
) modifyPodSpecResult {
	originalSpec := podSpec.DeepCopy()
	m.addInstrumentationVolume(podSpec)
	if m.clusterInstrumentationConfig.IsInstrumentationDeliveryInitContainer() {
		// The safe-to-evict-local-volumes annotation only matters for emptyDir local volumes; image volumes are
		// sourced from container images and managed by the kubelet, so they do not prevent eviction.
		m.addSafeToEvictLocalVolumesAnnotation(podMeta)
		// Only add the init container when the legacy instrumentation approach is used.
		m.addInitContainer(podSpec)
	} else {
		// The workload may have been instrumented previously via the init-container delivery mode. Remove the  artifacts
		// that were added back then but are not reconciled by the rest of modifyPodSpec, that is, the init container and
		// the safe-to-evict-local-volumes annotation entry. (The volume and the container volume mounts are reconciled in
		// place by addInstrumentationVolume/addMount.)
		m.removeInitContainer(podSpec)
		m.removeSafeToEvictLocalVolumesAnnotation(podMeta)
	}
	instrumentationIssuesPerContainer := map[string][]string{}
	for idx := range podSpec.Containers {
		container := &podSpec.Containers[idx]
		if issuesForContainer := m.instrumentContainer(container, workloadMeta, podMeta); len(issuesForContainer) > 0 {
			instrumentationIssuesPerContainer[container.Name] = issuesForContainer
		}
	}

	return modifyPodSpecResult{
		hasBeenModified:                   !reflect.DeepEqual(originalSpec, podSpec),
		instrumentationIssuesPerContainer: instrumentationIssuesPerContainer,
	}
}

func (m *ResourceModifier) addInstrumentationVolume(podSpec *corev1.PodSpec) {
	if podSpec.Volumes == nil {
		podSpec.Volumes = make([]corev1.Volume, 0)
	}
	idx := slices.IndexFunc(podSpec.Volumes, func(c corev1.Volume) bool {
		return c.Name == dash0VolumeName
	})
	dash0Volume := &corev1.Volume{
		Name:         dash0VolumeName,
		VolumeSource: m.dash0VolumeSource(),
	}

	if idx < 0 {
		podSpec.Volumes = append(podSpec.Volumes, *dash0Volume)
	} else {
		podSpec.Volumes[idx] = *dash0Volume
	}
}

func (m *ResourceModifier) dash0VolumeSource() corev1.VolumeSource {
	if m.clusterInstrumentationConfig.IsInstrumentationDeliveryImageVolume() {
		imageVolume := &corev1.ImageVolumeSource{
			Reference: m.clusterInstrumentationConfig.InitContainerImage,
		}
		if m.clusterInstrumentationConfig.InitContainerImagePullPolicy != "" {
			imageVolume.PullPolicy = m.clusterInstrumentationConfig.InitContainerImagePullPolicy
		}
		return corev1.VolumeSource{Image: imageVolume}
	}

	return corev1.VolumeSource{
		EmptyDir: &corev1.EmptyDirVolumeSource{
			SizeLimit: resource.NewScaledQuantity(500, resource.Mega),
		},
	}
}

func (m *ResourceModifier) addSafeToEvictLocalVolumesAnnotation(podMeta *metav1.ObjectMeta) {
	// See
	// https://github.com/kubernetes/autoscaler/blob/master/cluster-autoscaler/FAQ.md#what-types-of-pods-can-prevent-ca-from-removing-a-node
	if podMeta.Annotations == nil {
		podMeta.Annotations = make(map[string]string)
	}

	annotationValue, annotationIsPresent := podMeta.Annotations[safeToEviceLocalVolumesAnnotationName]
	if !annotationIsPresent {
		// The annotation is not present yet, add it with the Dash0 volume name as its only element.
		podMeta.Annotations[safeToEviceLocalVolumesAnnotationName] = dash0VolumeName
		return
	}

	if !strings.Contains(annotationValue, dash0VolumeName) {
		// The annotation is present, but the Dash0 volume name is not yet listed. Add the volume name.
		volumeNames := parseAndNormalizeVolumeList(annotationValue)
		volumeNames = append(volumeNames, dash0VolumeName)
		podMeta.Annotations[safeToEviceLocalVolumesAnnotationName] = strings.Join(volumeNames, ",")
		return
	}

	// The Dash0 volume is already in the list, no change necessary.
}

func parseAndNormalizeVolumeList(annotationValue string) []string {
	volumeNames := strings.Split(annotationValue, ",")
	for i, volumeName := range volumeNames {
		// normalize " volume-1 , volume-2  " to "volume-1,volume-2"
		volumeNames[i] = strings.TrimSpace(volumeName)
	}
	volumeNames = slices.DeleteFunc(volumeNames, func(volumeName string) bool {
		// do not return ",dash0-volume" if the original annotation was an empty string for whatever reason
		return volumeName == ""
	})
	return volumeNames
}

func (m *ResourceModifier) addInitContainer(podSpec *corev1.PodSpec) {
	// The init container's file system contains the OpenTelemtry injector and all auto-instrumentation agents for the
	// supported runtimes, in the directory /dash0-instrumentation. Its main responsibility is to copy these files to the
	// Kubernetes volume created and mounted in addInstrumentationVolume (mounted at /__otel_auto_instrumentation in the
	// init container and also in the target containers).
	if podSpec.InitContainers == nil {
		podSpec.InitContainers = make([]corev1.Container, 0)
	}
	idx := slices.IndexFunc(podSpec.InitContainers, func(c corev1.Container) bool {
		return c.Name == initContainerName
	})
	initContainer := m.createInitContainer(podSpec)
	if idx < 0 {
		podSpec.InitContainers = append(podSpec.InitContainers, *initContainer)
	} else {
		podSpec.InitContainers[idx] = *initContainer
	}
}

func (m *ResourceModifier) createInitContainer(podSpec *corev1.PodSpec) *corev1.Container {
	initContainerUser := &defaultInitContainerUser
	initContainerGroup := &defaultInitContainerGroup
	extraConfig := m.clusterInstrumentationConfig.ExtraConfig.Load()
	if extraConfig == nil {
		panic("extra config is nil in createInitContainer")
	}

	securityContext := podSpec.SecurityContext
	if securityContext == nil {
		securityContext = &corev1.PodSecurityContext{}
	}
	if securityContext.FSGroup != nil {
		initContainerUser = securityContext.FSGroup
		initContainerGroup = securityContext.FSGroup
	}

	initContainerEnv := []corev1.EnvVar{
		{
			Name:  dash0DirectoryEnvVarName,
			Value: otelAutoInstrumentationBaseDirectory,
		},
	}
	if m.clusterInstrumentationConfig.InstrumentationDebug {
		initContainerEnv = append(initContainerEnv, corev1.EnvVar{
			Name:  dash0CopyInstrumentationDebugEnvVarName,
			Value: "true",
		})
	}
	initContainer := &corev1.Container{
		Name:  initContainerName,
		Image: m.clusterInstrumentationConfig.InitContainerImage,
		Env:   initContainerEnv,
		SecurityContext: &corev1.SecurityContext{
			AllowPrivilegeEscalation: new(false),
			Privileged:               new(false),
			ReadOnlyRootFilesystem:   new(true),
			RunAsNonRoot:             securityContext.RunAsNonRoot,
			RunAsUser:                initContainerUser,
			RunAsGroup:               initContainerGroup,
			Capabilities: &corev1.Capabilities{
				Drop: []corev1.Capability{"ALL"},
			},
			SeccompProfile: &corev1.SeccompProfile{
				Type: corev1.SeccompProfileTypeRuntimeDefault,
			},
		},
		Resources: (*extraConfig).InstrumentationInitContainerResources.ToResourceRequirements(),
		VolumeMounts: []corev1.VolumeMount{
			{
				Name:      dash0VolumeName,
				ReadOnly:  false,
				MountPath: otelAutoInstrumentationBaseDirectory,
			},
		},
	}

	if m.clusterInstrumentationConfig.InitContainerImagePullPolicy != "" {
		initContainer.ImagePullPolicy = m.clusterInstrumentationConfig.InitContainerImagePullPolicy
	}
	return initContainer
}

func (m *ResourceModifier) instrumentContainer(
	container *corev1.Container,
	workloadMeta *metav1.ObjectMeta,
	podMeta *metav1.ObjectMeta,
) []string {
	perContainerLogger := m.logger.WithValues("container", container.Name)
	m.addMount(container)
	return m.addEnvironmentVariables(container, workloadMeta, podMeta, perContainerLogger)
}

func (m *ResourceModifier) addMount(container *corev1.Container) {
	if container.VolumeMounts == nil {
		container.VolumeMounts = make([]corev1.VolumeMount, 0)
	}
	idx := slices.IndexFunc(container.VolumeMounts, func(c corev1.VolumeMount) bool {
		return c.Name == dash0VolumeName
	})

	volume := &corev1.VolumeMount{
		Name:      dash0VolumeName,
		MountPath: otelAutoInstrumentationBaseDirectory,
	}
	if m.clusterInstrumentationConfig.IsInstrumentationDeliveryImageVolume() {
		// Selecting the directory inside the init container image whose contents the legacy init container approach
		// would have copied into the emptyDir volume via copy-instrumentation.sh.
		volume.SubPath = imageVolumeSubPath
	}
	if idx < 0 {
		container.VolumeMounts = append(container.VolumeMounts, *volume)
	} else {
		container.VolumeMounts[idx] = *volume
	}
}

func (m *ResourceModifier) addEnvironmentVariables(
	container *corev1.Container,
	workloadMeta *metav1.ObjectMeta,
	podMeta *metav1.ObjectMeta,
	perContainerLogger logd.Logger,
) []string {
	var instrumentationIssues []string
	if container.Env == nil {
		container.Env = make([]corev1.EnvVar, 0)
	}

	m.removeLegacyEnvironmentVariables(container)

	m.prependDash0NodeIp(container)

	collectorBaseUrl := m.clusterInstrumentationConfig.OTelCollectorBaseUrl
	addOrReplaceEnvironmentVariable(
		container,
		corev1.EnvVar{
			Name:  envVarDash0CollectorBaseUrlName,
			Value: collectorBaseUrl,
		},
	)

	// We currently attempt all environment variable modifications that can potentially fail (OTEL_EXPORTER_OTLP_*,
	// LD_PRELOAD) sequentially. Another reasonable approach might be to check whether all modifications are possible
	// ahead of time, and only apply them if all are possible. In practice, the current approach has a few advantages:
	// - There is a use case for having the operator set OTEL_INJECTOR_K8S_POD_NAME etc. even if we cannot set
	//   OTEL_EXPORTER_OTLP_*, for workloads that require a GRPC export (which can be set manually), like nginx, but
	//   still want to benefit from the provided OTEL_INJECTOR_* variables to derive resource attributes, so it makes
	//   sense to set them, independent of our ability to set OTEL_EXPORTER_OTLP_*.
	// - The same goes for setting LD_PRELOAD if OTEL_EXPORTER_OTLP_* cannot be set; this allows routing the telemetry
	//   to a different local collector, but still have the injector take care of attaching the OTel SDK.
	// - Finally, setting OTEL_EXPORTER_OTLP_* if LD_PRELOAD cannot be modified is probably not very useful, but it also
	//   should not have any detrimental effects. Plus, the only reason for not being able to modfiy LD_PRELOAD would be
	//   if it is set via valueFrom, which probably never happens in practice anyway.
	instrumentationIssues = m.addOrUpdateOtelExporterOtlpEnvVars(
		container,
		collectorBaseUrl,
		instrumentationIssues,
		perContainerLogger,
	)

	m.addOtelLogsExporterEnvVar(container)

	instrumentationIssues = m.addOrAppendToLdPreloadEnvVar(container, instrumentationIssues, perContainerLogger)
	otelInjectorConfigFile := envVarOtelInjectorConfigFileValue
	if m.clusterInstrumentationConfig.EnablePythonAutoInstrumentation {
		otelInjectorConfigFile = envVarOtelInjectorConfigFilePythonEnabledValue
	}
	addOrReplaceEnvironmentVariable(
		container,
		corev1.EnvVar{
			Name:  envVarOtelInjectorConfigFileName,
			Value: otelInjectorConfigFile,
		},
	)

	m.addOTelPropagatorsEnvVar(container)

	m.addCaptureSqlQueryParametersEnvVar(container)

	addOrReplaceEnvironmentVariable(
		container,
		corev1.EnvVar{
			Name: envVarOTelInjectorNamespaceName,
			ValueFrom: &corev1.EnvVarSource{
				FieldRef: &corev1.ObjectFieldSelector{
					FieldPath: "metadata.namespace",
				},
			},
		},
	)

	addOrReplaceEnvironmentVariable(
		container,
		corev1.EnvVar{
			Name: envVarOTelInjectorPodName,
			ValueFrom: &corev1.EnvVarSource{
				FieldRef: &corev1.ObjectFieldSelector{
					FieldPath: "metadata.name",
				},
			},
		},
	)

	addOrReplaceEnvironmentVariable(
		container,
		corev1.EnvVar{
			Name: envVarOTelInjectorPodUidName,
			ValueFrom: &corev1.EnvVarSource{
				FieldRef: &corev1.ObjectFieldSelector{
					FieldPath: "metadata.uid",
				},
			},
		},
	)

	addOrReplaceEnvironmentVariable(
		container,
		corev1.EnvVar{
			Name:  envVarOTelInjectorContainerName,
			Value: container.Name,
		},
	)

	// Add values from app.kubernetes.io/* labels as environment variables. Those will be picked up by the OpenTelemetry
	// injector and turned into resource attributes. We will look for the labels in the pod metadata first, and if the
	// pod does not have them, we will also check the workload labels. Labels will only be used from one of those two
	// levels in a consistent way, that is, we do not combine app.kubernetes.io/name from the pod with
	// app.kubernetes.io/part-of from the workload etc.
	//
	// `app.kubernetes.io/name` becomes `service.name`
	// `app.kubernetes.io/version` becomes `service.version`
	// `app.kubernetes.io/part-of` becomes `service.namespace`
	_, podMetaHasName := podMeta.Labels[util.AppKubernetesIoNameLabel]
	nameFromWorkloadMeta, workloadMetaHasName := workloadMeta.Labels[util.AppKubernetesIoNameLabel]
	hasServiceAttributes := m.checkContainerForServiceAttributes(container)
	if podMetaHasName {
		if !hasServiceAttributes.serviceName {
			m.addEnvVarFromLabelFieldSelector(container, envVarOTelInjectorServiceName, util.AppKubernetesIoNameLabel)
		}
		_, podMetaHasPartOfLabel := podMeta.Labels[util.AppKubernetesIoPartOfLabel]
		if !hasServiceAttributes.serviceNamespace && podMetaHasPartOfLabel {
			m.addEnvVarFromLabelFieldSelector(
				container,
				envVarOTelInjectorServiceNamespace,
				util.AppKubernetesIoPartOfLabel,
			)
		}
		_, podMetaVersionLabel := podMeta.Labels[util.AppKubernetesIoVersionLabel]
		if !hasServiceAttributes.serviceVersion && podMetaVersionLabel {
			m.addEnvVarFromLabelFieldSelector(
				container,
				envVarOTelInjectorServiceVersionName,
				util.AppKubernetesIoVersionLabel,
			)
		}
	} else if workloadMetaHasName {
		if !hasServiceAttributes.serviceName {
			addOrReplaceEnvironmentVariable(
				container,
				corev1.EnvVar{
					Name:  envVarOTelInjectorServiceName,
					Value: nameFromWorkloadMeta,
				},
			)
		}
		if !hasServiceAttributes.serviceNamespace {
			if partOfFromWorkloadMeta, workloadMetaHasPartOf := workloadMeta.Labels[util.AppKubernetesIoPartOfLabel]; workloadMetaHasPartOf {
				addOrReplaceEnvironmentVariable(
					container,
					corev1.EnvVar{
						Name:  envVarOTelInjectorServiceNamespace,
						Value: partOfFromWorkloadMeta,
					},
				)
			}
		}
		if !hasServiceAttributes.serviceVersion {
			if versionFromWorkloadMeta, workloadMetaHasVersion := workloadMeta.Labels[util.AppKubernetesIoVersionLabel]; workloadMetaHasVersion {
				addOrReplaceEnvironmentVariable(
					container,
					corev1.EnvVar{
						Name:  envVarOTelInjectorServiceVersionName,
						Value: versionFromWorkloadMeta,
					},
				)
			}
		}
	}

	// Last but not least, look for annotations that match the pattern "resource.opentelemetry.io/*" and turn those into
	// resource attributes. If there are "resource.opentelemetry.io/*" annotations for service.name, service.namespace or
	// service.version, they take priority over attributes derived from app.kubernetes.io/name etc.

	resourceAttributes := map[string]string{}
	serviceAttributesFromResourcesOpenTelemetryIoAnnotation := make(map[string]string, 3)

	// Map any workload annotation `resource.opentelemetry.io/some.key: "value"` to a corresponding resource attribute
	// some.key=value.
	m.deriveResourceAttributesFromAnnotations(
		workloadMeta.Annotations,
		resourceAttributes,
		serviceAttributesFromResourcesOpenTelemetryIoAnnotation,
	)

	// Same thing as above, this time for pod annotations. That is, map any pod annotation
	// `resource.opentelemetry.io/some.key: "value"` to a corresponding resource attribute some.key=value.
	// By iterating over the pod annotations _after_ the workload annotations, we ensure that the pod annotations take
	// precedence over the workload annotations.
	m.deriveResourceAttributesFromAnnotations(
		podMeta.Annotations,
		resourceAttributes,
		serviceAttributesFromResourcesOpenTelemetryIoAnnotation,
	)

	if len(resourceAttributes) > 0 {
		//nolint:prealloc
		var resourceAttributeList []string
		for _, resourceAttributeKey := range slices.Sorted(maps.Keys(resourceAttributes)) {
			resourceAttributeValue := resourceAttributes[resourceAttributeKey]
			resourceAttributeList = append(
				resourceAttributeList,
				fmt.Sprintf("%s=%s", resourceAttributeKey, resourceAttributeValue))
		}
		addOrReplaceEnvironmentVariable(
			container,
			corev1.EnvVar{
				Name:  envVarOTelInjectorResourceAttributesName,
				Value: strings.Join(resourceAttributeList, ","),
			})
	}

	if value, ok := serviceAttributesFromResourcesOpenTelemetryIoAnnotation[serviceName]; ok && !hasServiceAttributes.serviceName {
		addOrReplaceEnvironmentVariable(
			container,
			corev1.EnvVar{
				Name:  envVarOTelInjectorServiceName,
				Value: value,
			},
		)
	}
	if value, ok := serviceAttributesFromResourcesOpenTelemetryIoAnnotation[serviceNamespace]; ok && !hasServiceAttributes.serviceNamespace {
		addOrReplaceEnvironmentVariable(
			container,
			corev1.EnvVar{
				Name:  envVarOTelInjectorServiceNamespace,
				Value: value,
			},
		)
	}
	if value, ok := serviceAttributesFromResourcesOpenTelemetryIoAnnotation[serviceVersion]; ok && !hasServiceAttributes.serviceVersion {
		addOrReplaceEnvironmentVariable(
			container,
			corev1.EnvVar{
				Name:  envVarOTelInjectorServiceVersionName,
				Value: value,
			},
		)
	}

	if m.clusterInstrumentationConfig.InstrumentationDebug {
		addOrReplaceEnvironmentVariable(
			container,
			corev1.EnvVar{
				Name:  otelInjectorLogLevelEnvVarName,
				Value: "debug",
			},
		)
	}
	m.migrateLegacyInjectorLogLevel(container)

	return instrumentationIssues
}

func (m *ResourceModifier) deriveResourceAttributesFromAnnotations(
	annotations map[string]string,
	resourceAttributes map[string]string,
	serviceAttributesFromResourcesOpenTelemetryIoAnnotation map[string]string,
) {
	for annotationName, annotationValue := range annotations {
		if after, ok := strings.CutPrefix(annotationName, resourcesOpenTelemetryIoLabelPrefix); ok {
			resourceAttributeKey := after
			resourceAttributes[resourceAttributeKey] = annotationValue

			// Check if this is one of resource.opentelemetry.io/service.name, resource.opentelemetry.io/service.namespace or
			// resource.opentelemetry.io/service.version.
			for _, k := range serviceAttributes {
				if resourceAttributeKey == k {
					serviceAttributesFromResourcesOpenTelemetryIoAnnotation[k] = annotationValue
				}
			}
		}
	}
}

func (m *ResourceModifier) prependDash0NodeIp(container *corev1.Container) {
	// The DASH0_NODE_IP environment variable is required to resolve the collector base URL, in case it uses the
	// node-local/host port address. The collectorBaseUrl will be "http://$(DASH0_NODE_IP):40318" in this setup.
	// We also enforce DASH0_NODE_IP to be listed first in the container's env array, since env vars that are referenced
	// by other env vars need to come before the env vars referencing them.
	removeEnvironmentVariable(container, util.EnvVarDash0NodeIp)
	container.Env = slices.Insert(
		container.Env,
		0,
		corev1.EnvVar{
			Name: util.EnvVarDash0NodeIp,
			ValueFrom: &corev1.EnvVarSource{
				FieldRef: &corev1.ObjectFieldSelector{
					FieldPath: "status.hostIP",
				},
			},
		},
	)
}

// addOtelLogsExporterEnvVar sets OTEL_LOGS_EXPORTER=none when log collection is enabled for the namespace, so the
// workload's own OTel SDK does not also export logs via OTLP (the collector's file_log receiver already tails stdout,
// which would result in duplicate log records). If the user has already set OTEL_LOGS_EXPORTER explicitly, we preserve
// their value.
//
// Conversely, when log collection has just been disabled for the namespace (i.e. LogCollectionEnabled=false and
// PreviousLogCollectionEnabled=true), this function also removes the OTEL_LOGS_EXPORTER=none entry that we previously
// added, so re-instrumenting a workload via the normal instrumentation path also cleans up the env var. A user-set
// value is preserved.
func (m *ResourceModifier) addOtelLogsExporterEnvVar(container *corev1.Container) {
	if !m.namespaceInstrumentationConfig.LogCollectionEnabled {
		m.removeOtelLogsExporterEnvVar(container)
		return
	}
	if isSet, _ := envVarIsSetAndNotEmpty(container, envVarOtelLogsExporterName); isSet {
		return
	}
	addOrReplaceEnvironmentVariable(
		container,
		corev1.EnvVar{
			Name:  envVarOtelLogsExporterName,
			Value: envVarOtelLogsExporterNoneValue,
		},
	)
}

// removeOtelLogsExporterEnvVar removes the OTEL_LOGS_EXPORTER env var, but only if the value matches what we would
// have set (i.e. "none") and log collection was enabled at the previous reconcile. Any user-set value is preserved.
func (m *ResourceModifier) removeOtelLogsExporterEnvVar(container *corev1.Container) {
	if !m.namespaceInstrumentationConfig.PreviousLogCollectionEnabled {
		return
	}
	idx := findEnvVarIdx(container, envVarOtelLogsExporterName)
	if idx < 0 {
		return
	}
	if !envVarHasValue(container, idx, envVarOtelLogsExporterNoneValue) {
		return
	}
	removeEnvironmentVariable(container, envVarOtelLogsExporterName)
}

func (m *ResourceModifier) addOrUpdateOtelExporterOtlpEnvVars(
	container *corev1.Container,
	otelExporterOtlpEndpointValue string,
	instrumentationIssues []string,
	perContainerLogger logd.Logger,
) []string {
	if otelExportEnvVarsCanBeUpdatedForContainer(container, m.clusterInstrumentationConfig) {
		// It is safe to set/update the OTEL_EXPORTER_OTLP_* env vars, and at least one of them needs to be changed.
		addOrReplaceEnvironmentVariable(
			container,
			corev1.EnvVar{
				Name:  envVarOtelExporterOtlpEndpointName,
				Value: otelExporterOtlpEndpointValue,
			},
		)
		addOrReplaceEnvironmentVariable(
			container,
			corev1.EnvVar{
				Name:  envVarOtelExporterOtlpProtocolName,
				Value: defaultOtelExporterOtlpProtocol,
			},
		)
		return instrumentationIssues
	}

	if otelExporterOtlpEnvVarsAlreadyHaveDesiredValues(container, otelExporterOtlpEndpointValue) {
		// Both env vars are already set to the values we would set. There is nothing to do and nothing to warn about.
		return instrumentationIssues
	}

	// At least one of the OTEL_EXPORTER_OTLP_* env vars is set to a value that we did not set (a manual configuration),
	// so it is not safe to update them. Replacing/overwriting them could lead to breaking the telemetry export of a
	// container that is configured manually. For example:
	// * If a Go workload uses otlptracegrpc.New(context.Background()), this will respect OTEL_EXPORTER_OTLP_ENDPOINT
	//   but not OTEL_EXPORTER_OTLP_PROTOCOL, and with us overriding the endpoint to an HTTP endpoint, the workload
	//   would send GRPC traffic to an endpoint expecting HTTP traffic. This can also occur in other scenarios where
	//   an OTel SDK is set up manually in the workload's code.
	// * It will also break for OTel SDKs that only support the GRPC exporter, but not HTTP (Python & nginx for
	//   example).
	perContainerLogger.WarnTelemetryCollectionIssue(otelExporterOtlpNoOverwriteMsg)
	instrumentationIssues = append(instrumentationIssues, otelExporterOtlpNoOverwriteMsg)
	return instrumentationIssues
}

func otelExportEnvVarsWillBeUpdatedForAtLeastOneContainer(
	containers []corev1.Container,
	clusterInstrumentationConfig *util.ClusterInstrumentationConfig,
) bool {
	for _, container := range containers {
		if otelExportEnvVarsCanBeUpdatedForContainer(new(container), clusterInstrumentationConfig) {
			return true
		}
	}
	return false
}

// otelExportEnvVarsCanBeUpdatedForContainer reports whether the operator can (and needs to) set or update the
// OTEL_EXPORTER_OTLP_ENDPOINT and OTEL_EXPORTER_OTLP_PROTOCOL env vars on the given container. It returns true only if
// touching the env vars is safe (i.e. they have not been configured manually to a value the operator would not set)
// AND at least one of them needs to be changed to match the current configuration. It returns false when the env vars
// are already set to the desired values (nothing to do) or when they have been configured manually and must not be
// overwritten.
func otelExportEnvVarsCanBeUpdatedForContainer(
	container *corev1.Container,
	clusterInstrumentationConfig *util.ClusterInstrumentationConfig,
) bool {
	otelExporterOtlpEndpointIsSet, otelExporterOtlpEndpointIdx :=
		envVarIsSetAndNotEmpty(container, envVarOtelExporterOtlpEndpointName)
	otelExporterOtlpProtocolIsSet, otelExporterOtlpProtocolIdx :=
		envVarIsSetAndNotEmpty(container, envVarOtelExporterOtlpProtocolName)

	if !otelExporterOtlpEndpointIsSet && !otelExporterOtlpProtocolIsSet {
		// Neither env var is set (or both are empty). It is safe to add both.
		return true
	}

	if !otelExporterOtlpEndpointIsSet ||
		!envVarHasAnyValueFrom(
			container,
			otelExporterOtlpEndpointIdx,
			clusterInstrumentationConfig.PossibleCollectorUrls.All(),
		) {
		// Either OTEL_EXPORTER_OTLP_ENDPOINT is set to a value we did not set (a manual configuration we must not
		// overwrite), or only OTEL_EXPORTER_OTLP_PROTOCOL is set while OTEL_EXPORTER_OTLP_ENDPOINT is missing. In the
		// latter case we do not assume it is safe to instrument the container (why would anyone set
		// OTEL_EXPORTER_OTLP_PROTOCOL without OTEL_EXPORTER_OTLP_ENDPOINT?). In both cases we must not touch the env vars.
		return false
	}

	// At this point OTEL_EXPORTER_OTLP_ENDPOINT is set to one of the collector base URLs that the operator uses.

	if otelExporterOtlpProtocolIsSet &&
		!envVarHasValue(container, otelExporterOtlpProtocolIdx, defaultOtelExporterOtlpProtocol) {
		// OTEL_EXPORTER_OTLP_PROTOCOL is set to a value that is different from what we would set. Replacing/overwriting
		// any OTEL_EXPORTER_OTLP_* setting could break the telemetry export of a container that is configured manually.
		return false
	}

	endpointMatchesCurrentConfig :=
		envVarHasValue(container, otelExporterOtlpEndpointIdx, clusterInstrumentationConfig.OTelCollectorBaseUrl)
	if endpointMatchesCurrentConfig && otelExporterOtlpProtocolIsSet {
		// Both env vars are already set to the values we would set for the current configuration, nothing to do.
		return false
	}

	// Either OTEL_EXPORTER_OTLP_ENDPOINT needs to be updated to match the current configuration (e.g. when switching
	// between the node-local URL and the service URL), or OTEL_EXPORTER_OTLP_PROTOCOL still needs to be added.
	return true
}

// otelExporterOtlpEnvVarsAlreadyHaveDesiredValues reports whether OTEL_EXPORTER_OTLP_ENDPOINT and
// OTEL_EXPORTER_OTLP_PROTOCOL are both already set to the values the operator would set for the current configuration.
func otelExporterOtlpEnvVarsAlreadyHaveDesiredValues(
	container *corev1.Container,
	otelExporterOtlpEndpointValue string,
) bool {
	endpointIsSet, endpointIdx := envVarIsSetAndNotEmpty(container, envVarOtelExporterOtlpEndpointName)
	protocolIsSet, protocolIdx := envVarIsSetAndNotEmpty(container, envVarOtelExporterOtlpProtocolName)
	return endpointIsSet &&
		envVarHasValue(container, endpointIdx, otelExporterOtlpEndpointValue) &&
		protocolIsSet &&
		envVarHasValue(container, protocolIdx, defaultOtelExporterOtlpProtocol)
}

func (m *ResourceModifier) addOrAppendToLdPreloadEnvVar(
	container *corev1.Container,
	instrumentationIssues []string,
	perContainerLogger logd.Logger,
) []string {
	idx := findEnvVarIdx(container, envVarLdPreloadName)
	if idx < 0 {
		container.Env = append(container.Env, corev1.EnvVar{
			Name:  envVarLdPreloadName,
			Value: envVarLdPreloadValue,
		})
	} else {
		// Note: This needs to be a pointer to the env var, otherwise updates would only be local to this function.
		envVar := &container.Env[idx]
		if envVar.Value == "" && envVar.ValueFrom != nil {
			msg := fmt.Sprintf(
				"Dash0 cannot prepend anything to the environment variable %s as it is specified via "+
					"ValueFrom, this container will not be instrumented to send telemetry to Dash0.",
				envVarLdPreloadName)
			perContainerLogger.WarnTelemetryCollectionIssue(msg)
			instrumentationIssues = append(instrumentationIssues, msg)
			return instrumentationIssues
		}

		if !strings.Contains(envVar.Value, envVarLdPreloadValue) {
			if strings.TrimSpace(envVar.Value) == "" {
				envVar.Value = envVarLdPreloadValue
			} else {
				envVar.Value = fmt.Sprintf("%s %s", envVarLdPreloadValue, envVar.Value)
			}
		}
	}

	return instrumentationIssues
}

func (m *ResourceModifier) addOTelPropagatorsEnvVar(container *corev1.Container) {
	if otelPropagatorsCanBeUpdatedForContainer(container, m.namespaceInstrumentationConfig) {
		if pointers.IsEmpty(m.namespaceInstrumentationConfig.TraceContextPropagators) {
			removeEnvironmentVariable(container, util.OtelPropagatorsEnvVarName)
		} else {
			addOrReplaceEnvironmentVariable(
				container,
				corev1.EnvVar{
					Name:  util.OtelPropagatorsEnvVarName,
					Value: strings.TrimSpace(*m.namespaceInstrumentationConfig.TraceContextPropagators),
				},
			)
		}
	}
}

func otelPropagatorsEnvVarWillBeUpdatedForAtLeastOneContainer(containers []corev1.Container, namespaceInstrumentationConfig dash0v1beta1.NamespaceInstrumentationConfig) bool {
	for _, container := range containers {
		if otelPropagatorsCanBeUpdatedForContainer(new(container), namespaceInstrumentationConfig) {
			return true
		}
	}
	return false
}

func otelPropagatorsCanBeUpdatedForContainer(container *corev1.Container, namespaceInstrumentationConfig dash0v1beta1.NamespaceInstrumentationConfig) bool {
	envVarOnContainer := util.GetEnvVar(container, util.OtelPropagatorsEnvVarName)

	if envVarOnContainer != nil && envVarOnContainer.ValueFrom != nil {
		// The environment variable OTEL_PROPAGATORS is set via ValueFrom, it was not set by the Dash0 operator, and
		// the operator is not supposed to change it, no matter what the monitoring resource specifies.
		return false
	}

	if pointers.IsEmpty(namespaceInstrumentationConfig.TraceContextPropagators) {
		// The monitoring resource does not have spec.instrumentWorkloads.traceContext.propagators set. We might need
		// to remove the environment variable OTEL_PROPAGATORS from the container, but only if there is such an
		// environment variable, and it has been set by the operator.

		if util.IsEnvVarUnsetOrEmpty(envVarOnContainer) {
			// The monitoring resource does not have spec.instrumentWorkloads.traceContext.propagators set, and the
			// container does not have the environment variable OTEL_PROPAGATORS set, hence nothing needs to be changed.
			return false
		} else {
			// The monitoring resource does not have spec.instrumentWorkloads.traceContext.propagators set, but the
			// container has the environment variable OTEL_PROPAGATORS set. If it has been set by the operator, we
			// need to remove it, otherwise we leave it as is. To determine whether it has been set by the operator,
			// we compare the current env var value against the previous requested setting in the monitoring resource.
			if pointers.IsEmpty(namespaceInstrumentationConfig.PreviousTraceContextPropagators) {
				// There is no previous trace context propagators setting, apparently the env var has not been set by
				// the operator, do nothing.
				return false
			}
			if envVarOnContainer != nil &&
				strings.TrimSpace(envVarOnContainer.Value) == strings.TrimSpace(*namespaceInstrumentationConfig.PreviousTraceContextPropagators) {
				// There previous trace context propagators setting matches the current env var value, apparently the
				// env var has been set by the operator, remove it.
				return true
			} else {
				// There previous trace context propagators setting exists, but it does not match the current env var
				// value, apparently the env var has not been set by the operator, so we do not remove it.
				return false
			}
		}
	} // if pointers.IsEmpty(namespaceInstrumentationConfig.TraceContextPropagators) {

	// The monitoring resource does have a spec.instrumentWorkloads.traceContext.propagators value. We might need
	// to add or update the environment variable OTEL_PROPAGATORS from the container

	if util.IsEnvVarUnsetOrEmpty(envVarOnContainer) {
		// The container currently does not have the OTEL_PROPAGATORS environment variable set. It is safe to add it,
		// as there is no risk of us overwriting an environment variable that was set by the user directly on the pod
		// spec.
		return true
	} else {
		currentEnvVarValue := (*envVarOnContainer).Value
		desiredValueFromMonitoringResource := *namespaceInstrumentationConfig.TraceContextPropagators

		if strings.TrimSpace(currentEnvVarValue) == strings.TrimSpace(desiredValueFromMonitoringResource) {
			// The environment variable is already up to date, no change is required.
			return false
		}

		// The container already has the OTEL_PROPAGATORS environment variable, and it has a different value then
		// requested in the monitoring resource. We can only change the value safely if the current value has been set
		// by the operator, which we can determine by checking the previous requested setting in the monitoring
		// resource's status.

		if pointers.IsEmpty(namespaceInstrumentationConfig.PreviousTraceContextPropagators) {
			// There is no previous trace context propagators setting, apparently the env var has not been set by
			// the operator, do nothing.
			return false
		}

		// The monitoring resource does have a spec.instrumentWorkloads.traceContext.propagators value, but the
		// container already has this environment variable set. We only overwrite the current env var if its value
		// matches the previous requested setting in the monitoring resource, which indicates the current env var has
		// been set by the operator.
		if strings.TrimSpace(envVarOnContainer.Value) == strings.TrimSpace(*namespaceInstrumentationConfig.PreviousTraceContextPropagators) {
			// The previous trace context propagators setting matches the current env var value, apparently the current
			// env var has been set by the operator, update it with the new value from the monitoring resource.
			return true
		} else {
			// The previous trace context propagators setting does not match the current env var value, hence the env
			// var has not been set by the operator, do nothing.
			return false
		}
	}
}

// addCaptureSqlQueryParametersEnvVar reconciles the SQL query parameter capture environment variables on the
// container. When the monitoring resource enables capture, missing env vars in CaptureSqlQueryParametersEnvVarNames
// are added with Value="true". When capture is disabled, env vars previously written by the operator (i.e. whose
// current Value is the literal "true" and whose PreviousCaptureSqlQueryParameters was true) are removed.
//
// Each env var is evaluated independently. User-set Values whose payload differs from "true" are preserved, as are
// values set via ValueFrom. Today the slice contains the OpenTelemetry Java agent's
// OTEL_INSTRUMENTATION_JDBC_EXPERIMENTAL_CAPTURE_QUERY_PARAMETERS and the OpenTelemetry .NET auto-instrumentation's
// OTEL_DOTNET_EXPERIMENTAL_SQLCLIENT_ENABLE_TRACE_DB_QUERY_PARAMETERS and
// OTEL_DOTNET_EXPERIMENTAL_EFCORE_ENABLE_TRACE_DB_QUERY_PARAMETERS. Note: a user-set Value of exactly "true" is
// indistinguishable from an operator-set value and will be removed on toggle-off if the previous reconcile had the
// field enabled.
func (m *ResourceModifier) addCaptureSqlQueryParametersEnvVar(container *corev1.Container) {
	desiredOn := pointers.ReadBoolPointerWithDefault(m.namespaceInstrumentationConfig.CaptureSqlQueryParameters, false)
	for _, envVarName := range CaptureSqlQueryParametersEnvVarNames {
		if !captureSqlQueryParametersCanBeUpdatedForContainer(container, envVarName, m.namespaceInstrumentationConfig) {
			continue
		}
		if desiredOn {
			addOrReplaceEnvironmentVariable(
				container,
				corev1.EnvVar{
					Name:  envVarName,
					Value: captureSqlQueryParametersValueTrue,
				},
			)
		} else {
			removeEnvironmentVariable(container, envVarName)
		}
	}
}

func captureSqlQueryParametersEnvVarWillBeUpdatedForAtLeastOneContainer(
	containers []corev1.Container,
	namespaceInstrumentationConfig dash0v1beta1.NamespaceInstrumentationConfig,
) bool {
	for _, container := range containers {
		for _, envVarName := range CaptureSqlQueryParametersEnvVarNames {
			if captureSqlQueryParametersCanBeUpdatedForContainer(new(container), envVarName, namespaceInstrumentationConfig) {
				return true
			}
		}
	}
	return false
}

func captureSqlQueryParametersCanBeUpdatedForContainer(
	container *corev1.Container,
	envVarName string,
	namespaceInstrumentationConfig dash0v1beta1.NamespaceInstrumentationConfig,
) bool {
	envVarOnContainer := util.GetEnvVar(container, envVarName)

	if envVarOnContainer != nil && envVarOnContainer.ValueFrom != nil {
		// The environment variable is set via ValueFrom, it was not set by the Dash0 operator (the operator only ever
		// writes Value), and the operator is not supposed to change it, no matter what the monitoring resource specifies.
		return false
	}

	desiredOn := pointers.ReadBoolPointerWithDefault(namespaceInstrumentationConfig.CaptureSqlQueryParameters, false)
	previousOn := pointers.ReadBoolPointerWithDefault(namespaceInstrumentationConfig.PreviousCaptureSqlQueryParameters, false)

	if !desiredOn {
		// The monitoring resource does not request capturing SQL query parameters. We might need to remove the
		// environment variable, but only if it was previously set by the operator.

		if util.IsEnvVarUnsetOrEmpty(envVarOnContainer) {
			// The env var is not set on the container, nothing to do.
			return false
		}
		if !previousOn {
			// There is no previous "true" setting, apparently the env var has not been set by the operator, do nothing.
			return false
		}
		if envVarOnContainer != nil &&
			strings.TrimSpace(envVarOnContainer.Value) == captureSqlQueryParametersValueTrue {
			// The previous setting was on and the current env var value matches what we would have set, apparently the
			// env var has been set by the operator, remove it.
			return true
		}
		// The previous setting was on but the current env var value differs from what we would have set, hence the env
		// var has been changed by the user, do not remove it.
		return false
	}

	// The monitoring resource requests capturing SQL query parameters. We might need to add the environment variable.

	if util.IsEnvVarUnsetOrEmpty(envVarOnContainer) {
		// The container currently does not have the environment variable set. It is safe to add it.
		return true
	}

	if strings.TrimSpace(envVarOnContainer.Value) == captureSqlQueryParametersValueTrue {
		// The environment variable is already up to date, no change is required.
		return false
	}

	// The container has the environment variable set with a value different from what we would set. The operator only
	// ever writes "true", so any non-"true" value must have been set by the user. Do not overwrite. (Unlike the
	// OTEL_PROPAGATORS sibling, the operator-ownership check via PreviousCaptureSqlQueryParameters is not meaningful
	// in this branch: the "previous operator-written value" is implicitly the literal "true", which is exactly what the
	// "already up to date" check above caught.)
	return false
}

func otelInjectorConfEnvVarWillBeUpdatedForAtLeastOneContainer(
	containers []corev1.Container,
	clusterInstrumentationConfig *util.ClusterInstrumentationConfig,
) bool {
	for _, container := range containers {
		envVarOnContainer := util.GetEnvVar(new(container), envVarOtelInjectorConfigFileName)
		if envVarOnContainer != nil && envVarOnContainer.ValueFrom != nil {
			// The environment variable OTEL_INJECTOR_CONFIG_FILE is set via ValueFrom for this container, which is basically
			// impossible because only the operator should ever set this variable, and it never uses ValueFrom. Anyway, we
			// will update this to use Value instead of ValueFrom in addEnvironmentVariables.
			return true
		}

		if util.IsEnvVarUnsetOrEmpty(envVarOnContainer) {
			// This container currently does not have the OTEL_INJECTOR_CONFIG_FILE environment variable set. It will be set
			// in addEnvironmentVariables.
			return true
		}

		currentEnvVarValue := (*envVarOnContainer).Value
		desiredValue := envVarOtelInjectorConfigFileValue
		if clusterInstrumentationConfig.EnablePythonAutoInstrumentation {
			desiredValue = envVarOtelInjectorConfigFilePythonEnabledValue
		}

		if strings.TrimSpace(currentEnvVarValue) != strings.TrimSpace(desiredValue) {
			// The container has the wrong value for OTEL_INJECTOR_CONFIG_FILE, this will be updated in
			// addEnvironmentVariables.
			return true
		}
	}

	// No container requires changing OTEL_INJECTOR_CONFIG_FILE.
	return false
}

// checkContainerForServiceAttributes checks whether the container sets service attributes (service.name etc.) directly,
// via OTEL_SERVICE_NAME or OTEL_RESOURCE_ATTRIBUTES. If that is the case, the respective service attribute is not
// derived from Kubernetes labels or annotations (e.g. container environment variables win over everything else).
func (m *ResourceModifier) checkContainerForServiceAttributes(container *corev1.Container) containerHasServiceAttributes {
	hasServiceAttributes := containerHasServiceAttributes{}
	otelServiceName := util.GetEnvVar(container, util.OtelServiceNameEnvVarName)
	otelResourceAttributes := util.GetEnvVar(container, util.OtelResourceAttributesEnvVarName)
	var otelResourceAttributesKeyValuePairs []string
	if otelResourceAttributes != nil &&
		otelResourceAttributes.ValueFrom == nil &&
		strings.TrimSpace(otelResourceAttributes.Value) != "" {
		otelResourceAttributesKeyValuePairsRaw := strings.Split(otelResourceAttributes.Value, ",")
		otelResourceAttributesKeyValuePairs = make([]string, len(otelResourceAttributesKeyValuePairsRaw))
		for i, keyValuePair := range otelResourceAttributesKeyValuePairsRaw {
			otelResourceAttributesKeyValuePairs[i] = strings.ReplaceAll(strings.TrimSpace(keyValuePair), " ", "")
		}
	}

	if otelServiceName != nil && (otelServiceName.ValueFrom != nil || strings.TrimSpace(otelServiceName.Value) != "") {
		hasServiceAttributes.serviceName = true
	}
	for _, keyValuePair := range otelResourceAttributesKeyValuePairs {
		if strings.HasPrefix(keyValuePair, "service.name=") {
			hasServiceAttributes.serviceName = true
		}
		if strings.HasPrefix(keyValuePair, "service.namespace=") {
			hasServiceAttributes.serviceNamespace = true
		}
		if strings.HasPrefix(keyValuePair, "service.version=") {
			hasServiceAttributes.serviceVersion = true
		}
	}
	return hasServiceAttributes
}

func (m *ResourceModifier) migrateLegacyInjectorLogLevel(container *corev1.Container) {
	// Translate the legacy injector log level environment variable DASH0_INJECTOR_LOG_LEVEL automatically to
	// OTEL_INJECTOR_LOG_LEVEL. (Obviously this will not work if DASH0_INJECTOR_LOG_LEVEL is not defined in K8s but in
	// the Dockerfile etc.)
	legacyInjectorLogLevelIsSet, legacyInjectorLogLevelIdx :=
		envVarIsSetAndNotEmpty(container, legacyEnvVarDash0InjectorLogLevel)
	otelInjectorLogLevelIsSet, _ := envVarIsSetAndNotEmpty(container, otelInjectorLogLevelEnvVarName)
	if legacyInjectorLogLevelIsSet && !otelInjectorLogLevelIsSet {
		otelInjectorLogLevelEnvVar := corev1.EnvVar{
			Name:  otelInjectorLogLevelEnvVarName,
			Value: container.Env[legacyInjectorLogLevelIdx].Value,
		}
		addOrReplaceEnvironmentVariable(
			container,
			otelInjectorLogLevelEnvVar,
		)
		removeEnvironmentVariable(container, legacyEnvVarDash0InjectorLogLevel)
	}
}

func addOrReplaceEnvironmentVariable(container *corev1.Container, envVar corev1.EnvVar) {
	idx := findEnvVarIdx(container, envVar.Name)
	if idx < 0 {
		container.Env = append(container.Env, envVar)
	} else {
		replaceExistingEnvironmentVariable(container, idx, envVar)
	}

}

func replaceExistingEnvironmentVariable(container *corev1.Container, idx int, envVar corev1.EnvVar) {
	if envVar.Value != "" {
		container.Env[idx].ValueFrom = nil
		container.Env[idx].Value = envVar.Value
	} else {
		container.Env[idx].Value = ""
		container.Env[idx].ValueFrom = envVar.ValueFrom
	}
}

func (m *ResourceModifier) addEnvVarFromLabelFieldSelector(
	container *corev1.Container,
	envVarName string,
	labelName string,
) {
	addOrReplaceEnvironmentVariable(
		container,
		corev1.EnvVar{
			Name: envVarName,
			ValueFrom: &corev1.EnvVarSource{
				FieldRef: &corev1.ObjectFieldSelector{
					FieldPath: fmt.Sprintf("metadata.labels['%s']", labelName),
				},
			},
		},
	)
}

func findEnvVarIdx(container *corev1.Container, envVarName string) int {
	return slices.IndexFunc(container.Env, func(c corev1.EnvVar) bool {
		return c.Name == envVarName
	})
}

func envVarIsSetAndNotEmpty(container *corev1.Container, name string) (bool, int) {
	idx := findEnvVarIdx(container, name)
	if idx < 0 {
		return false, idx
	}
	return container.Env[idx].ValueFrom != nil || strings.TrimSpace(container.Env[idx].Value) != "", idx
}

func envVarHasValue(container *corev1.Container, idx int, value string) bool {
	return container.Env[idx].ValueFrom == nil && strings.TrimSpace(container.Env[idx].Value) == value
}

func envVarHasAnyValueFrom(container *corev1.Container, idx int, values []string) bool {
	if container.Env[idx].ValueFrom != nil {
		return false
	}
	currentValue := strings.TrimSpace(container.Env[idx].Value)
	return slices.Contains(values, currentValue)
}

func (m *ResourceModifier) RevertCronJob(cronJob *batchv1.CronJob) ModificationResult {
	return m.revertResource(
		&cronJob.Spec.JobTemplate.Spec.Template,
		&cronJob.ObjectMeta,
		&cronJob.Spec.JobTemplate.Spec.Template.ObjectMeta,
	)
}

func (m *ResourceModifier) RevertDaemonSet(daemonSet *appsv1.DaemonSet) ModificationResult {
	return m.revertResource(
		&daemonSet.Spec.Template,
		&daemonSet.ObjectMeta,
		&daemonSet.Spec.Template.ObjectMeta,
	)
}

func (m *ResourceModifier) RevertDeployment(deployment *appsv1.Deployment) ModificationResult {
	return m.revertResource(
		&deployment.Spec.Template,
		&deployment.ObjectMeta,
		&deployment.Spec.Template.ObjectMeta,
	)
}

func (m *ResourceModifier) RemoveLabelsFromImmutableJob(job *batchv1.Job) ModificationResult {
	util.RemoveInstrumentationLabelsAndAnnotations(&job.ObjectMeta)
	// removing labels always works and is a modification that requires an update
	return NewHasBeenModifiedResult(len(job.Spec.Template.Spec.Containers), nil)
}

func (m *ResourceModifier) RevertReplicaSet(replicaSet *appsv1.ReplicaSet) ModificationResult {
	if m.hasMatchingOwnerReference(replicaSet, []metav1.TypeMeta{util.K8sTypeMetaDeployment}) {
		return NewNotModifiedOwnedByHigherOrderWorkloadResult()
	}
	return m.revertResource(
		&replicaSet.Spec.Template,
		&replicaSet.ObjectMeta,
		&replicaSet.Spec.Template.ObjectMeta,
	)
}

func (m *ResourceModifier) RevertStatefulSet(statefulSet *appsv1.StatefulSet) ModificationResult {
	return m.revertResource(
		&statefulSet.Spec.Template,
		&statefulSet.ObjectMeta,
		&statefulSet.Spec.Template.ObjectMeta,
	)
}

func (m *ResourceModifier) revertResource(
	podTemplateSpec *corev1.PodTemplateSpec,
	workloadMeta *metav1.ObjectMeta,
	podMeta *metav1.ObjectMeta,
) ModificationResult {
	if util.InstrumentationAttemptHasFailed(workloadMeta) {
		// workload has never been instrumented successfully, only remove labels
		util.RemoveInstrumentationLabelsAndAnnotations(workloadMeta)
		util.RemoveInstrumentationLabelsAndAnnotations(&podTemplateSpec.ObjectMeta)
		return NewHasBeenModifiedResult(len(podTemplateSpec.Spec.Containers), nil)
	}
	podSpecResult := m.revertPodSpec(&podTemplateSpec.Spec, podMeta)
	if !podSpecResult.hasBeenModified {
		return NewNotModifiedNoChangesResult()
	}
	util.RemoveInstrumentationLabelsAndAnnotations(workloadMeta)
	util.RemoveInstrumentationLabelsAndAnnotations(&podTemplateSpec.ObjectMeta)
	return NewHasBeenModifiedResult(len(podTemplateSpec.Spec.Containers), podSpecResult.instrumentationIssuesPerContainer)
}

func (m *ResourceModifier) revertPodSpec(podSpec *corev1.PodSpec, podMeta *metav1.ObjectMeta) modifyPodSpecResult {
	originalSpec := podSpec.DeepCopy()
	m.removeInstrumentationVolume(podSpec)
	m.removeSafeToEvictLocalVolumesAnnotation(podMeta)
	m.removeInitContainer(podSpec)
	for idx := range podSpec.Containers {
		container := &podSpec.Containers[idx]
		m.uninstrumentContainer(container)
	}

	return modifyPodSpecResult{
		hasBeenModified: !reflect.DeepEqual(originalSpec, podSpec),
		// reverting a container can currently not create any instrumentation issues
		instrumentationIssuesPerContainer: nil,
	}
}

func (m *ResourceModifier) removeInstrumentationVolume(podSpec *corev1.PodSpec) {
	if podSpec.Volumes == nil {
		return
	}
	podSpec.Volumes = slices.DeleteFunc(podSpec.Volumes, func(c corev1.Volume) bool {
		return c.Name == dash0VolumeName
	})
}

func (m *ResourceModifier) removeSafeToEvictLocalVolumesAnnotation(podMeta *metav1.ObjectMeta) {
	if podMeta.Annotations == nil {
		// There are no annotations, nothing to remove.
		return
	}

	annotationValue, annotationIsPresent := podMeta.Annotations[safeToEviceLocalVolumesAnnotationName]
	if !annotationIsPresent {
		// The annotation is not present, nothing to remove.
		return
	}
	if annotationValue == dash0VolumeName {
		// If the dash0-instrumentation volume is the only volume in the list, remove the annotation entirely.
		delete(podMeta.Annotations, safeToEviceLocalVolumesAnnotationName)
		return
	}

	if !strings.Contains(annotationValue, dash0VolumeName) {
		// The annotation is present, but it does not contain dash0-instrumentation volume, nothing to remove.
		return
	}

	// There are multiple volumes in the list, remove only the dash0-instrumentation volume.
	volumeNames := parseAndNormalizeVolumeList(annotationValue)
	volumeNames = slices.Delete(
		volumeNames,
		slices.Index(volumeNames, dash0VolumeName),
		slices.Index(volumeNames, dash0VolumeName)+1,
	)
	if len(volumeNames) == 0 {
		delete(podMeta.Annotations, safeToEviceLocalVolumesAnnotationName)
		return
	} else {
		podMeta.Annotations[safeToEviceLocalVolumesAnnotationName] = strings.Join(volumeNames, ",")
	}
}

func (m *ResourceModifier) removeInitContainer(podSpec *corev1.PodSpec) {
	if podSpec.InitContainers == nil {
		return
	}
	podSpec.InitContainers = slices.DeleteFunc(podSpec.InitContainers, func(c corev1.Container) bool {
		return c.Name == initContainerName
	})
}

func (m *ResourceModifier) uninstrumentContainer(container *corev1.Container) {
	m.removeMount(container)
	m.removeEnvironmentVariables(container)
}

func (m *ResourceModifier) removeMount(container *corev1.Container) {
	if container.VolumeMounts == nil {
		return
	}
	container.VolumeMounts = slices.DeleteFunc(container.VolumeMounts, func(c corev1.VolumeMount) bool {
		return c.Name == dash0VolumeName
	})
}

func (m *ResourceModifier) removeEnvironmentVariables(container *corev1.Container) {
	if container.Env == nil {
		return
	}
	removeEnvironmentVariable(container, util.EnvVarDash0NodeIp)
	m.removeLegacyEnvironmentVariables(container)
	m.removeLdPreload(container)
	removeEnvironmentVariable(container, envVarOtelInjectorConfigFileName)
	removeEnvironmentVariable(container, envVarDash0CollectorBaseUrlName)

	m.removeOtelExporterOtlpEnvVarsIfSetByOperator(container)
	m.removeOtelPropagatorsIfCurrentValueMatchesConfig(container)
	m.removeCaptureSqlQueryParametersIfCurrentValueMatchesConfig(container)
	m.removeOtelLogsExporterEnvVar(container)

	removeEnvironmentVariable(container, envVarOTelInjectorNamespaceName)
	removeEnvironmentVariable(container, envVarOTelInjectorPodName)
	removeEnvironmentVariable(container, envVarOTelInjectorPodUidName)
	removeEnvironmentVariable(container, envVarOTelInjectorContainerName)
	removeEnvironmentVariable(container, envVarOTelInjectorServiceNamespace)
	removeEnvironmentVariable(container, envVarOTelInjectorServiceName)
	removeEnvironmentVariable(container, envVarOTelInjectorServiceVersionName)
	removeEnvironmentVariable(container, envVarOTelInjectorResourceAttributesName)
	removeEnvironmentVariable(container, otelInjectorLogLevelEnvVarName)
}

func (m *ResourceModifier) removeLdPreload(container *corev1.Container) {
	m.removeEntryFromLdPreload(container, envVarLdPreloadValue)
}

func (m *ResourceModifier) removeEntryFromLdPreload(container *corev1.Container, ldPreloadEntry string) {
	idx := findEnvVarIdx(container, envVarLdPreloadName)
	if idx < 0 {
		return
	}

	envVar := container.Env[idx]
	previousValue := envVar.Value
	if previousValue == "" && envVar.ValueFrom != nil {
		// Specified via ValueFrom, this has not been done by us, so we assume there is no Dash0-specific
		// LD_PRELOAD part.
		return
	}
	if strings.TrimSpace(previousValue) == ldPreloadEntry {
		container.Env = slices.Delete(container.Env, idx, idx+1)
		return
	} else if !strings.Contains(previousValue, ldPreloadEntry) {
		return
	}

	separator := " "
	if strings.Contains(previousValue, ":") {
		separator = ":"
	}
	librariesUntrimmed := strings.Split(previousValue, separator)
	libraries := make([]string, 0, len(librariesUntrimmed))
	for _, lib := range librariesUntrimmed {
		libraries = append(libraries, strings.TrimSpace(lib))
	}
	libraries = slices.DeleteFunc(libraries, func(lib string) bool {
		return strings.TrimSpace(lib) == ldPreloadEntry || lib == ""
	})
	container.Env[idx].Value = strings.Join(libraries, separator)
}

func (m *ResourceModifier) removeOtelExporterOtlpEnvVarsIfSetByOperator(container *corev1.Container) {
	otelExporterOtlpEndpointIsSet, otelExporterOtlpEndpointIdx :=
		envVarIsSetAndNotEmpty(container, envVarOtelExporterOtlpEndpointName)
	otelExporterOtlpProtocolIsSet, otelExporterOtlpProtocolIdx :=
		envVarIsSetAndNotEmpty(container, envVarOtelExporterOtlpProtocolName)
	if !otelExporterOtlpEndpointIsSet || !otelExporterOtlpProtocolIsSet {
		return
	}

	if !envVarHasAnyValueFrom(
		container,
		otelExporterOtlpEndpointIdx,
		m.clusterInstrumentationConfig.PossibleCollectorUrls.All(),
	) ||
		!envVarHasValue(container, otelExporterOtlpProtocolIdx, defaultOtelExporterOtlpProtocol) {
		return
	}

	removeEnvironmentVariable(container, envVarOtelExporterOtlpEndpointName)
	removeEnvironmentVariable(container, envVarOtelExporterOtlpProtocolName)
}

func (m *ResourceModifier) removeOtelPropagatorsIfCurrentValueMatchesConfig(container *corev1.Container) {
	if m.namespaceInstrumentationConfig.TraceContextPropagators != nil &&
		strings.TrimSpace(*m.namespaceInstrumentationConfig.TraceContextPropagators) != "" {
		idx := findEnvVarIdx(container, util.OtelPropagatorsEnvVarName)
		if idx < 0 {
			// env var is not set, nothing to do
			return
		}
		existingEnvVar := container.Env[idx]
		if existingEnvVar.ValueFrom != nil {
			// if OTEL_PROPAGATORS is set via ValueFrom, it hasn't been set by us, leave it alone
			return
		}
		if strings.TrimSpace(existingEnvVar.Value) == strings.TrimSpace(*m.namespaceInstrumentationConfig.TraceContextPropagators) {
			removeEnvironmentVariable(container, util.OtelPropagatorsEnvVarName)
		}
	}
}

func (m *ResourceModifier) removeCaptureSqlQueryParametersIfCurrentValueMatchesConfig(container *corev1.Container) {
	if !pointers.ReadBoolPointerWithDefault(m.namespaceInstrumentationConfig.CaptureSqlQueryParameters, false) {
		return
	}
	for _, envVarName := range CaptureSqlQueryParametersEnvVarNames {
		idx := findEnvVarIdx(container, envVarName)
		if idx < 0 {
			continue
		}
		existingEnvVar := container.Env[idx]
		if existingEnvVar.ValueFrom != nil {
			// set via ValueFrom, not by us, leave alone
			continue
		}
		if existingEnvVar.Value == captureSqlQueryParametersValueTrue {
			// Compare without TrimSpace: the operator only ever writes the bare literal "true", so a value of " true " or
			// similar must have been set by the user and should be left alone.
			removeEnvironmentVariable(container, envVarName)
		}
	}
}

func removeEnvironmentVariable(container *corev1.Container, name string) {
	container.Env = slices.DeleteFunc(container.Env, func(c corev1.EnvVar) bool {
		return c.Name == name
	})
}

// removeLegacyEnvironmentVariables removes environment variables that previous versions of the operator added to
// workloads, but which are no longer set. When operator versions <= x set the env var EXAMPLE_VAR, and operator version
// x + 1 stops setting it, it would never be removed without us actively cleaning up.
func (m *ResourceModifier) removeLegacyEnvironmentVariables(container *corev1.Container) {
	// removed in release 0.70.1, follow up to adding the Dash0 injector in release 0.28.0
	// (https://github.com/dash0hq/dash0-operator/pull/453, follow up to
	// https://github.com/dash0hq/dash0-operator/pull/155)
	m.removeLegacyEnvVarNodeOptions(container)

	// removed in release 0.47.1 (https://github.com/dash0hq/dash0-operator/pull/280)
	removeEnvironmentVariable(container, "DASH0_SERVICE_INSTANCE_ID")

	// removed in release 0.88.0 (https://github.com/dash0hq/dash0-operator/pull/590)
	removeEnvironmentVariable(container, "DASH0_INJECTOR_DEBUG")

	// removed in release 0.97.0 (https://github.com/dash0hq/dash0-operator/pull/706), replaced with the respective
	// OTEL_INJECTOR_* variables.
	removeEnvironmentVariable(container, "DASH0_NAMESPACE_NAME")
	removeEnvironmentVariable(container, "DASH0_POD_NAME")
	removeEnvironmentVariable(container, "DASH0_POD_UID")
	removeEnvironmentVariable(container, "DASH0_CONTAINER_NAME")
	removeEnvironmentVariable(container, "DASH0_SERVICE_NAME")
	removeEnvironmentVariable(container, "DASH0_SERVICE_NAMESPACE")
	removeEnvironmentVariable(container, "DASH0_SERVICE_VERSION")
	removeEnvironmentVariable(container, "DASH0_RESOURCE_ATTRIBUTES")

	// removed in release 0.97.1, follow up to replacing the Dash0 injector with the OpenTelemetry injector in release
	// 0.97.0 (https://github.com/dash0hq/dash0-operator/pull/721, follow-up to
	// https://github.com/dash0hq/dash0-operator/pull/706).
	m.removeEntryFromLdPreload(container, envVarLdPreloadLegacyValue)
}

func (m *ResourceModifier) removeLegacyEnvVarNodeOptions(container *corev1.Container) {
	idx := findEnvVarIdx(container, legacyEnvVarNodeOptionsName)
	if idx < 0 {
		return
	}

	envVar := container.Env[idx]
	previousValue := envVar.Value
	if previousValue == "" && envVar.ValueFrom != nil {
		// Specified via ValueFrom, this has not been done by us, so we assume there is no Dash0-specific
		// NODE_OPTIONS part.
		return
	}
	if !strings.Contains(previousValue, legacyEnvVarNodeOptionsValue) {
		// NODE_OPTIONS does not contain the Dash0 --require, nothing to do.
		return
	}

	if strings.TrimSpace(previousValue) == legacyEnvVarNodeOptionsValue {
		container.Env = slices.Delete(container.Env, idx, idx+1)
		return
	}

	// for cases where other options are listed after our --require value
	newValue := strings.ReplaceAll(previousValue, legacyEnvVarNodeOptionsValue+" ", "")
	// for cases where other options are listed before our --require value (if the previous replace command worked, this
	// one will not match)
	newValue = strings.ReplaceAll(newValue, " "+legacyEnvVarNodeOptionsValue, "")
	// this should have been handled earlier (the Dash0 --require is the only option present), but just in case
	// (if one of the previous replace commands worked, this one will not match)
	newValue = strings.ReplaceAll(newValue, legacyEnvVarNodeOptionsValue, "")
	if strings.TrimSpace(newValue) == "" {
		// if it was only our --require, surrounded by whitespace, we have an empty string now and can remove the env
		// var entirely
		container.Env = slices.Delete(container.Env, idx, idx+1)
		return
	}

	// there are other options left, so leave the env var in place and only update the value with the string where the
	// Dash0 --require has been removed.
	container.Env[idx].Value = newValue
}

// checkEligibleForModification checks whether the given PodTemplateSpec is eligible for modification.
// If it is, the function returns nil, otherwise it returns a ModificationResult describing why it is not eligible.
// In particular, this function checks whether the pod spec template is a non-Linux workload, or whether any container
// has an ephemeral-storage limit too low to accommodate the Dash0 instrumentation volume.
func (m *ResourceModifier) checkEligibleForModification(podSpec *corev1.PodSpec) *ModificationResult {
	if podSpec.OS != nil && podSpec.OS.Name != "" && podSpec.OS.Name != corev1.Linux {
		return new(NewNotModifiedUnsupportedOperatingSystemResult(fmt.Sprintf("pod.spec.os.name: \"%s\"", podSpec.OS.Name)))
	}
	for key, value := range podSpec.NodeSelector {
		if key == util.KubernetesIoOs && value != "linux" {
			return new(NewNotModifiedUnsupportedOperatingSystemResult(fmt.Sprintf("pod.spec.nodeSelector: \"%s=%s\"", key, value)))
		}
	}
	if podSpec.Affinity != nil &&
		podSpec.Affinity.NodeAffinity != nil &&
		podSpec.Affinity.NodeAffinity.RequiredDuringSchedulingIgnoredDuringExecution != nil {
		for _, nodeSelectorTerm := range podSpec.Affinity.NodeAffinity.RequiredDuringSchedulingIgnoredDuringExecution.NodeSelectorTerms {
			for _, matchExpression := range nodeSelectorTerm.MatchExpressions {
				if matchExpression.Key == util.KubernetesIoOs &&
					((matchExpression.Operator == corev1.NodeSelectorOpIn && !slices.Contains(matchExpression.Values, "linux")) ||
						(matchExpression.Operator == corev1.NodeSelectorOpNotIn && slices.Contains(matchExpression.Values, "linux"))) {
					return new(NewNotModifiedUnsupportedOperatingSystemResult(fmt.Sprintf(
						"pod.spec.affinity.nodeAffinity.requiredDuringSchedulingIgnoredDuringExecution."+
							"nodeSelectorTerms.matchExpression: key: \"%s\", operator: \"%s\", values: \"%v\"",
						matchExpression.Key,
						matchExpression.Operator,
						matchExpression.Values,
					)))
				}
			}
		}
	}
	if m.clusterInstrumentationConfig.IsInstrumentationDeliveryInitContainer() {
		if notModifiedResult := m.checkEphemeralStorageLimit(podSpec); notModifiedResult != nil {
			return notModifiedResult
		}
	}
	return nil
}

// checkEphemeralStorageLimit returns a not-modified result if any container in the pod spec declares an ephemeral
// storage limit below the threshold (to not cause pod eviction once we add the instrumentation volume). Init containers
// and ephemeral containers are not checked, since we do not instrument them. A telemetry-collection-issue warning is
// logged for the first offending container; subsequent offenders are not reported.
func (m *ResourceModifier) checkEphemeralStorageLimit(podSpec *corev1.PodSpec) *ModificationResult {
	for i := range podSpec.Containers {
		container := &podSpec.Containers[i]
		limit, hasLimit := container.Resources.Limits[corev1.ResourceEphemeralStorage]
		if !hasLimit {
			continue
		}
		if limit.Value() < ephemeralStorageLimitThresholdBytes {
			result := NewNotModifiedEphemeralStorageLimitTooLowResult(container.Name, limit.String())
			m.logger.WithValues("container", container.Name).
				WarnTelemetryCollectionIssue(result.RenderReasonMessage(m.actor))
			return &result
		}
	}
	return nil
}

func (m *ResourceModifier) hasMatchingOwnerReference(workload client.Object, possibleOwnerTypes []metav1.TypeMeta) bool {
	ownerReferences := workload.GetOwnerReferences()
	if len(ownerReferences) == 0 {
		return false
	}
	for _, actualOwnerRef := range ownerReferences {
		for _, ownerType := range possibleOwnerTypes {
			if actualOwnerRef.APIVersion == ownerType.APIVersion && actualOwnerRef.Kind == ownerType.Kind {
				return true
			}
		}
	}
	return false
}
