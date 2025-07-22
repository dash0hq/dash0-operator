// SPDX-FileCopyrightText: Copyright 2024 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package workloads

import (
	"fmt"
	"reflect"
	"slices"
	"strings"

	"github.com/go-logr/logr"
	appsv1 "k8s.io/api/apps/v1"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/dash0hq/dash0-operator/images/pkg/common"
	"github.com/dash0hq/dash0-operator/internal/collectors/otelcolresources"
	"github.com/dash0hq/dash0-operator/internal/util"
)

type containerHasServiceAttributes struct {
	serviceName      bool
	serviceNamespace bool
	serviceVersion   bool
}

const (
	initContainerName = "dash0-instrumentation"

	dash0VolumeName                         = "dash0-instrumentation"
	dash0DirectoryEnvVarName                = "DASH0_INSTRUMENTATION_FOLDER_DESTINATION"
	dash0CopyInstrumentationDebugEnvVarName = "DASH0_COPY_INSTRUMENTATION_DEBUG"
	dash0InstrumentationBaseDirectory       = "/__dash0__"
	dash0InstrumentationDirectory           = "/__dash0__/instrumentation"
	envVarLdPreloadName                     = "LD_PRELOAD"
	envVarLdPreloadValue                    = "/__dash0__/dash0_injector.so"
	envVarOtelExporterOtlpEndpointName      = "OTEL_EXPORTER_OTLP_ENDPOINT"
	envVarOtelExporterOtlpProtocolName      = "OTEL_EXPORTER_OTLP_PROTOCOL"
	envVarDash0CollectorBaseUrlName         = "DASH0_OTEL_COLLECTOR_BASE_URL"
	envVarDash0NamespaceName                = "DASH0_NAMESPACE_NAME"
	envVarDash0PodName                      = "DASH0_POD_NAME"
	envVarDash0PodUidName                   = "DASH0_POD_UID"
	envVarDash0ContainerName                = "DASH0_CONTAINER_NAME"
	envVarDash0ServiceName                  = "DASH0_SERVICE_NAME"
	envVarDash0ServiceNamespace             = "DASH0_SERVICE_NAMESPACE"
	envVarDash0ServiceVersionName           = "DASH0_SERVICE_VERSION"
	envVarDash0ResourceAttributesName       = "DASH0_RESOURCE_ATTRIBUTES"
	dash0InjectorDebugEnvVarName            = "DASH0_INJECTOR_DEBUG"

	safeToEviceLocalVolumesAnnotationName = "cluster-autoscaler.kubernetes.io/safe-to-evict-local-volumes"

	// legacy environment variables
	legacyEnvVarNodeOptionsName  = "NODE_OPTIONS"
	legacyEnvVarNodeOptionsValue = "--require /__dash0__/instrumentation/node.js/node_modules/@dash0hq/opentelemetry"
)

var (
	defaultInitContainerUser              int64 = 13020
	defaultInitContainerGroup             int64 = 13020
	initContainerAllowPrivilegeEscalation       = false
	initContainerPrivileged                     = false
	initContainerReadOnlyRootFilesystem         = true
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
	HasBeenModified     bool
	RenderReasonMessage NoModificationReasonMessage
	SkipLogging         bool
	IgnoredOnce         bool
	ImmutableWorkload   bool
}

func NewHasBeenModifiedResult() ModificationResult {
	return ModificationResult{
		HasBeenModified: true,
	}
}

func NewNotModifiedReasonUnknownResult() ModificationResult {
	return newNotModifiedResult(NoModificationReasonUnknown)
}

func NewNotModifiedDueToErrorResult() ModificationResult {
	return newNotModifiedResult(NoModificationReasonError)
}

func NewNotModifiedOwnedByHigherOrderWorkloadResult() ModificationResult {
	return newNotModifiedSkipLoggingResult(NoModificationReasonOwnedByHigherOrderWorkload)
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

func newNotModifiedSkipLoggingResult(
	reason NoModificationReasonMessage,
) ModificationResult {
	return ModificationResult{
		HasBeenModified:     false,
		RenderReasonMessage: reason,
		SkipLogging:         true,
	}
}

func InstrumentationIsUpToDate(
	objectMeta *metav1.ObjectMeta,
	containers []corev1.Container,
	images util.Images,
	namespaceInstrumentationConfig util.NamespaceInstrumentationConfig,
) bool {
	if !util.HasBeenInstrumentedSuccessfullyByThisVersion(objectMeta, images) {
		return false
	}
	if otelPropagatorsEnvVarWillBeUpdatedForAtLeastOneContainer(containers, namespaceInstrumentationConfig) {
		return false
	}
	return true
}

type ResourceModifier struct {
	// configuration values relevant for instrumenting workloads which apply to the whole cluster, e.g. settings from
	// the helm chart or the operator configuration resource.
	clusterInstrumentationConfig util.ClusterInstrumentationConfig

	// configuration values relevant for instrumenting workloads which apply to one namespace, e.g. settings from the
	// monitoring resource.
	namespaceInstrumentationConfig util.NamespaceInstrumentationConfig

	// the name of the component that applies the resource modifications, this will be written to the
	// dash0.com/instrumented-by label
	actor util.WorkloadModifierActor

	// the logger to use for logging messages during the resource modification process
	logger *logr.Logger
}

func NewResourceModifier(
	clusterInstrumentationConfig util.ClusterInstrumentationConfig,
	namespaceInstrumentationConfig util.NamespaceInstrumentationConfig,
	actor util.WorkloadModifierActor,
	logger *logr.Logger,
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
	util.AddInstrumentationLabels(&job.ObjectMeta, false, m.clusterInstrumentationConfig, m.actor)
	// adding labels always works and is a modification that requires an update
	return NewHasBeenModifiedResult()
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
	if hasBeenModified := m.modifyPodSpec(&pod.Spec, &pod.ObjectMeta, &pod.ObjectMeta); !hasBeenModified {
		return NewNotModifiedNoChangesResult()
	}
	util.AddInstrumentationLabels(&pod.ObjectMeta, true, m.clusterInstrumentationConfig, m.actor)
	return NewHasBeenModifiedResult()
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
	if hasBeenModified := m.modifyPodSpec(&podTemplateSpec.Spec, workloadMeta, podMeta); !hasBeenModified {
		return NewNotModifiedNoChangesResult()
	}
	util.AddInstrumentationLabels(workloadMeta, true, m.clusterInstrumentationConfig, m.actor)
	util.AddInstrumentationLabels(&podTemplateSpec.ObjectMeta, true, m.clusterInstrumentationConfig, m.actor)
	return NewHasBeenModifiedResult()
}

func (m *ResourceModifier) modifyPodSpec(
	podSpec *corev1.PodSpec,
	workloadMeta *metav1.ObjectMeta,
	podMeta *metav1.ObjectMeta,
) bool {
	originalSpec := podSpec.DeepCopy()
	m.addInstrumentationVolume(podSpec)
	m.addSafeToEvictLocalVolumesAnnotation(podMeta)
	m.addInitContainer(podSpec)
	for idx := range podSpec.Containers {
		container := &podSpec.Containers[idx]
		m.instrumentContainer(container, workloadMeta, podMeta)
	}

	return !reflect.DeepEqual(originalSpec, podSpec)
}

func (m *ResourceModifier) addInstrumentationVolume(podSpec *corev1.PodSpec) {
	if podSpec.Volumes == nil {
		podSpec.Volumes = make([]corev1.Volume, 0)
	}
	idx := slices.IndexFunc(podSpec.Volumes, func(c corev1.Volume) bool {
		return c.Name == dash0VolumeName
	})
	dash0Volume := &corev1.Volume{
		Name: dash0VolumeName,
		VolumeSource: corev1.VolumeSource{
			EmptyDir: &corev1.EmptyDirVolumeSource{
				SizeLimit: resource.NewScaledQuantity(500, resource.Mega),
			},
		},
	}

	if idx < 0 {
		podSpec.Volumes = append(podSpec.Volumes, *dash0Volume)
	} else {
		podSpec.Volumes[idx] = *dash0Volume
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
	// The init container has all the instrumentation packages (e.g. the Dash0 Node.js distribution etc.), stored under
	// /dash0-init-container/instrumentation. Its main responsibility is to copy these files to the Kubernetes volume
	// created and mounted in addInstrumentationVolume (mounted at /__dash0__/instrumentation in the init container and
	// also in the target containers).

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
			Value: dash0InstrumentationBaseDirectory,
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
			AllowPrivilegeEscalation: &initContainerAllowPrivilegeEscalation,
			Privileged:               &initContainerPrivileged,
			ReadOnlyRootFilesystem:   &initContainerReadOnlyRootFilesystem,
			RunAsNonRoot:             securityContext.RunAsNonRoot,
			RunAsUser:                initContainerUser,
			RunAsGroup:               initContainerGroup,
			SeccompProfile: &corev1.SeccompProfile{
				Type: corev1.SeccompProfileTypeRuntimeDefault,
			},
		},
		Resources: m.clusterInstrumentationConfig.ExtraConfig.InstrumentationInitContainerResources.ToResourceRequirements(),
		VolumeMounts: []corev1.VolumeMount{
			{
				Name:      dash0VolumeName,
				ReadOnly:  false,
				MountPath: dash0InstrumentationBaseDirectory,
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
) {
	perContainerLogger := m.logger.WithValues("container", container.Name)
	m.addMount(container)
	m.addEnvironmentVariables(container, workloadMeta, podMeta, perContainerLogger)
}

func (m *ResourceModifier) addMount(container *corev1.Container) {
	if container.VolumeMounts == nil {
		container.VolumeMounts = make([]corev1.VolumeMount, 0)
	}
	idx := slices.IndexFunc(container.VolumeMounts, func(c corev1.VolumeMount) bool {
		return c.Name == dash0VolumeName || c.MountPath == dash0InstrumentationDirectory
	})

	volume := &corev1.VolumeMount{
		Name:      dash0VolumeName,
		MountPath: dash0InstrumentationBaseDirectory,
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
	perContainerLogger logr.Logger,
) {
	m.removeLegacyEnvironmentVariables(container)

	m.handleLdPreloadEnvVar(container, perContainerLogger)

	// The DASH0_NODE_IP environment variable is required to resolve the collector base URL, in case it uses the
	// node-local/host port address. The collectorBaseUrl will be "http://$(DASH0_NODE_IP):40318" in this setup.
	addOrReplaceEnvironmentVariable(
		container,
		corev1.EnvVar{
			Name: otelcolresources.EnvVarDash0NodeIp,
			ValueFrom: &corev1.EnvVarSource{
				FieldRef: &corev1.ObjectFieldSelector{
					FieldPath: "status.hostIP",
				},
			},
		},
	)

	collectorBaseUrl := m.clusterInstrumentationConfig.OTelCollectorBaseUrl
	addOrReplaceEnvironmentVariable(
		container,
		corev1.EnvVar{
			Name:  envVarDash0CollectorBaseUrlName,
			Value: collectorBaseUrl,
		},
	)
	addOrReplaceEnvironmentVariable(
		container,
		corev1.EnvVar{
			Name:  envVarOtelExporterOtlpEndpointName,
			Value: collectorBaseUrl,
		},
	)
	addOrReplaceEnvironmentVariable(
		container,
		corev1.EnvVar{
			Name:  envVarOtelExporterOtlpProtocolName,
			Value: common.ProtocolHttpProtobuf,
		},
	)

	m.handleOTelPropagatorsEnvVar(container)

	addOrReplaceEnvironmentVariable(
		container,
		corev1.EnvVar{
			Name: envVarDash0NamespaceName,
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
			Name: envVarDash0PodName,
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
			Name: envVarDash0PodUidName,
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
			Name:  envVarDash0ContainerName,
			Value: container.Name,
		},
	)

	// Add values from app.kubernetes.io/* labels as environment variables. Those will be picked up by the injector and
	// turned into resource attributes. We will look for the labels in the pod metadata first, and if the pod does not
	// have them, we will also check the workload labels. Labels will only be used from one of those two levels in a
	// consistent way, that is, we do not combine app.kubernetes.io/name from the pod with app.kubernetes.io/part-of
	// from the workload etc.
	//
	// `app.kubernetes.io/name` becomes `service.name`
	// `app.kubernetes.io/version` becomes `service.version`
	// `app.kubernetes.io/part-of` becomes `service.namespace`
	_, podMetaHasName := podMeta.Labels[util.AppKubernetesIoNameLabel]
	nameFromWorkloadMeta, workloadMetaHasName := workloadMeta.Labels[util.AppKubernetesIoNameLabel]
	hasServiceAttributes := m.checkContainerForServiceAttributes(container)
	if podMetaHasName {
		if !hasServiceAttributes.serviceName {
			m.addEnvVarFromLabelFieldSelector(container, envVarDash0ServiceName, util.AppKubernetesIoNameLabel)
		}
		if !hasServiceAttributes.serviceNamespace {
			m.conditionallyAddEnvVarFromLabelFieldSelector(
				container,
				podMeta,
				envVarDash0ServiceNamespace,
				util.AppKubernetesIoPartOfLabel,
			)
		}
		if !hasServiceAttributes.serviceVersion {
			m.conditionallyAddEnvVarFromLabelFieldSelector(
				container,
				podMeta,
				envVarDash0ServiceVersionName,
				util.AppKubernetesIoVersionLabel,
			)
		}
	} else if workloadMetaHasName {
		if !hasServiceAttributes.serviceName {
			addOrReplaceEnvironmentVariable(
				container,
				corev1.EnvVar{
					Name:  envVarDash0ServiceName,
					Value: nameFromWorkloadMeta,
				},
			)
		}
		if !hasServiceAttributes.serviceNamespace {
			if partOfFromWorkloadMeta, workloadMetaHasPartOf := workloadMeta.Labels[util.AppKubernetesIoPartOfLabel]; workloadMetaHasPartOf {
				addOrReplaceEnvironmentVariable(
					container,
					corev1.EnvVar{
						Name:  envVarDash0ServiceNamespace,
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
						Name:  envVarDash0ServiceVersionName,
						Value: versionFromWorkloadMeta,
					},
				)
			}
		}
	}

	// Map annotations resource.opentelemetry.io/your-key: "your-value" to resource attributes.
	resourceAttributes := map[string]string{}
	for annotationName, annotationValue := range workloadMeta.Annotations {
		if strings.HasPrefix(annotationName, "resource.opentelemetry.io/") {
			resourceAttributeKey := strings.TrimPrefix(annotationName, "resource.opentelemetry.io/")
			resourceAttributes[resourceAttributeKey] = annotationValue
		}
	}
	// By iterating over the pod annotations _after_ the workload annotations, we ensure that the pod annotations take
	// precedence over the workload annotations.
	for annotationName, annotationValue := range podMeta.Annotations {
		if strings.HasPrefix(annotationName, "resource.opentelemetry.io/") {
			resourceAttributeKey := strings.TrimPrefix(annotationName, "resource.opentelemetry.io/")
			resourceAttributes[resourceAttributeKey] = annotationValue
		}
	}
	if len(resourceAttributes) > 0 {
		var resourceAttributeList []string
		for resourceAttributeKey, resourceAttributeValue := range resourceAttributes {
			resourceAttributeList = append(
				resourceAttributeList,
				fmt.Sprintf("%s=%s", resourceAttributeKey, resourceAttributeValue))
		}
		addOrReplaceEnvironmentVariable(
			container,
			corev1.EnvVar{
				Name:  envVarDash0ResourceAttributesName,
				Value: strings.Join(resourceAttributeList, ","),
			})
	}

	if m.clusterInstrumentationConfig.InstrumentationDebug {
		addOrReplaceEnvironmentVariable(
			container,
			corev1.EnvVar{
				Name:  dash0InjectorDebugEnvVarName,
				Value: "true",
			},
		)
	}
}

func (m *ResourceModifier) handleLdPreloadEnvVar(
	container *corev1.Container,
	perContainerLogger logr.Logger,
) {
	if container.Env == nil {
		container.Env = make([]corev1.EnvVar, 0)
	}
	idx := slices.IndexFunc(container.Env, func(c corev1.EnvVar) bool {
		return c.Name == envVarLdPreloadName
	})

	if idx < 0 {
		container.Env = append(container.Env, corev1.EnvVar{
			Name:  envVarLdPreloadName,
			Value: envVarLdPreloadValue,
		})
	} else {
		// Note: This needs to be a pointer to the env var, otherwise updates would only be local to this function.
		envVar := &container.Env[idx]
		if envVar.Value == "" && envVar.ValueFrom != nil {
			perContainerLogger.Info(
				fmt.Sprintf(
					"Dash0 cannot prepend anything to the environment variable %s as it is specified via "+
						"ValueFrom. This container will not be instrumented.",
					envVarLdPreloadName))
			return
		}

		if !strings.Contains(envVar.Value, envVarLdPreloadValue) {
			if strings.TrimSpace(envVar.Value) == "" {
				envVar.Value = envVarLdPreloadValue
			} else {
				envVar.Value = fmt.Sprintf("%s %s", envVarLdPreloadValue, envVar.Value)
			}
		}
	}
}

func otelPropagatorsEnvVarWillBeUpdatedForAtLeastOneContainer(containers []corev1.Container, namespaceInstrumentationConfig util.NamespaceInstrumentationConfig) bool {
	for _, container := range containers {
		if otelPropagatorsCanBeUpdatedForContainer(ptr.To(container), namespaceInstrumentationConfig) {
			return true
		}
	}
	return false
}

func (m *ResourceModifier) handleOTelPropagatorsEnvVar(container *corev1.Container) {
	if otelPropagatorsCanBeUpdatedForContainer(container, m.namespaceInstrumentationConfig) {
		if util.IsEmpty(m.namespaceInstrumentationConfig.TraceContextPropagators) {
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

func otelPropagatorsCanBeUpdatedForContainer(container *corev1.Container, namespaceInstrumentationConfig util.NamespaceInstrumentationConfig) bool {
	envVarOnContainer := util.GetEnvVar(container, util.OtelPropagatorsEnvVarName)

	if envVarOnContainer != nil && envVarOnContainer.ValueFrom != nil {
		// The environment variable OTEL_PROPAGATORS is set via ValueFrom, it was not set by the Dash0 operator, and
		// the operator is not supposed to change it, no matter what the monitoring resource specifies.
		return false
	}

	if util.IsEmpty(namespaceInstrumentationConfig.TraceContextPropagators) {
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
			if util.IsEmpty(namespaceInstrumentationConfig.PreviousTraceContextPropagators) {
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
	} // if util.IsEmpty(namespaceInstrumentationConfig.TraceContextPropagators) {

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

		if util.IsEmpty(namespaceInstrumentationConfig.PreviousTraceContextPropagators) {
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

func addOrReplaceEnvironmentVariable(container *corev1.Container, envVar corev1.EnvVar) {
	if container.Env == nil {
		container.Env = make([]corev1.EnvVar, 0)
	}
	idx := slices.IndexFunc(container.Env, func(c corev1.EnvVar) bool {
		return c.Name == envVar.Name
	})

	if idx < 0 {
		container.Env = append(container.Env, envVar)
	} else if envVar.Value != "" {
		container.Env[idx].ValueFrom = nil
		container.Env[idx].Value = envVar.Value
	} else {
		container.Env[idx].Value = ""
		container.Env[idx].ValueFrom = envVar.ValueFrom
	}
}

func (m *ResourceModifier) conditionallyAddEnvVarFromLabelFieldSelector(
	container *corev1.Container,
	podMeta *metav1.ObjectMeta,
	envVarName string,
	labelName string,
) {
	if _, podMetaHasLabel := podMeta.Labels[labelName]; podMetaHasLabel {
		m.addEnvVarFromLabelFieldSelector(container, envVarName, labelName)
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
	util.RemoveInstrumentationLabels(&job.ObjectMeta)
	// removing labels always works and is a modification that requires an update
	return NewHasBeenModifiedResult()
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
		util.RemoveInstrumentationLabels(workloadMeta)
		util.RemoveInstrumentationLabels(&podTemplateSpec.ObjectMeta)
		return NewHasBeenModifiedResult()
	}
	if hasBeenModified := m.revertPodSpec(&podTemplateSpec.Spec, podMeta); !hasBeenModified {
		return NewNotModifiedNoChangesResult()
	}
	util.RemoveInstrumentationLabels(workloadMeta)
	util.RemoveInstrumentationLabels(&podTemplateSpec.ObjectMeta)
	return NewHasBeenModifiedResult()
}

func (m *ResourceModifier) revertPodSpec(podSpec *corev1.PodSpec, podMeta *metav1.ObjectMeta) bool {
	originalSpec := podSpec.DeepCopy()
	m.removeInstrumentationVolume(podSpec)
	m.removeSafeToEvictLocalVolumesAnnotation(podMeta)
	m.removeInitContainer(podSpec)
	for idx := range podSpec.Containers {
		container := &podSpec.Containers[idx]
		m.uninstrumentContainer(container)
	}

	return !reflect.DeepEqual(originalSpec, podSpec)
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
		return c.Name == dash0VolumeName || c.MountPath == dash0InstrumentationDirectory
	})
}

func (m *ResourceModifier) removeEnvironmentVariables(container *corev1.Container) {
	m.removeLegacyEnvironmentVariables(container)
	m.removeLdPreload(container)
	removeEnvironmentVariable(container, otelcolresources.EnvVarDash0NodeIp)
	removeEnvironmentVariable(container, envVarDash0CollectorBaseUrlName)
	removeEnvironmentVariable(container, envVarOtelExporterOtlpEndpointName)
	removeEnvironmentVariable(container, envVarOtelExporterOtlpProtocolName)
	m.removeOtelPropagatorsIfCurrentValueMatchesConfig(container)
	removeEnvironmentVariable(container, envVarDash0NamespaceName)
	removeEnvironmentVariable(container, envVarDash0PodName)
	removeEnvironmentVariable(container, envVarDash0PodUidName)
	removeEnvironmentVariable(container, envVarDash0ContainerName)
	removeEnvironmentVariable(container, envVarDash0ServiceNamespace)
	removeEnvironmentVariable(container, envVarDash0ServiceName)
	removeEnvironmentVariable(container, envVarDash0ServiceVersionName)
	removeEnvironmentVariable(container, envVarDash0ResourceAttributesName)
	removeEnvironmentVariable(container, dash0InjectorDebugEnvVarName)
}

func (m *ResourceModifier) removeLdPreload(container *corev1.Container) {
	if container.Env == nil {
		return
	}
	idx := slices.IndexFunc(container.Env, func(c corev1.EnvVar) bool {
		return c.Name == envVarLdPreloadName
	})

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
	if strings.TrimSpace(previousValue) == envVarLdPreloadValue {
		container.Env = slices.Delete(container.Env, idx, idx+1)
		return
	} else if !strings.Contains(previousValue, envVarLdPreloadValue) {
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
		return strings.TrimSpace(lib) == envVarLdPreloadValue || lib == ""
	})
	container.Env[idx].Value = strings.Join(libraries, separator)
}

func (m *ResourceModifier) removeOtelPropagatorsIfCurrentValueMatchesConfig(container *corev1.Container) {
	if m.namespaceInstrumentationConfig.TraceContextPropagators != nil &&
		strings.TrimSpace(*m.namespaceInstrumentationConfig.TraceContextPropagators) != "" {
		idx := slices.IndexFunc(container.Env, func(c corev1.EnvVar) bool {
			return c.Name == util.OtelPropagatorsEnvVarName
		})
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

func removeEnvironmentVariable(container *corev1.Container, name string) {
	if container.Env == nil {
		return
	}
	container.Env = slices.DeleteFunc(container.Env, func(c corev1.EnvVar) bool {
		return c.Name == name
	})
}

// removeLegacyEnvironmentVariables removes environment variables that previous versions of the operator added to
// workloads, but which are no longer set. When operator versions <= x set the env var EXAMPLE_VAR, and operator version
// x + 1 stops setting it, it would never be removed without us actively cleaning up.
func (m *ResourceModifier) removeLegacyEnvironmentVariables(container *corev1.Container) {
	m.removeLegacyEnvVarNodeOptions(container)
	removeEnvironmentVariable(container, "DASH0_SERVICE_INSTANCE_ID")
}

func (m *ResourceModifier) removeLegacyEnvVarNodeOptions(container *corev1.Container) {
	if container.Env == nil {
		return
	}
	idx := slices.IndexFunc(container.Env, func(c corev1.EnvVar) bool {
		return c.Name == legacyEnvVarNodeOptionsName
	})

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
	newValue := strings.Replace(previousValue, legacyEnvVarNodeOptionsValue+" ", "", -1)
	// for cases where other options are listed before our --require value (if the previous replace command worked, this
	// one will not match)
	newValue = strings.Replace(newValue, " "+legacyEnvVarNodeOptionsValue, "", -1)
	// this should have been handled earlier (the Dash0 --require is the only option present), but just in case
	// (if one of the previous replace commands worked, this one will not match)
	newValue = strings.Replace(newValue, legacyEnvVarNodeOptionsValue, "", -1)
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
