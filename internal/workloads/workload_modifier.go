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
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/dash0hq/dash0-operator/images/pkg/common"
	"github.com/dash0hq/dash0-operator/internal/collectors/otelcolresources"
	"github.com/dash0hq/dash0-operator/internal/util"
)

const (
	initContainerName = "dash0-instrumentation"

	dash0VolumeName                    = "dash0-instrumentation"
	dash0DirectoryEnvVarName           = "DASH0_INSTRUMENTATION_FOLDER_DESTINATION"
	dash0InstrumentationBaseDirectory  = "/__dash0__"
	dash0InstrumentationDirectory      = "/__dash0__/instrumentation"
	envVarLdPreloadName                = "LD_PRELOAD"
	envVarLdPreloadValue               = "/__dash0__/dash0_injector.so"
	envVarOtelExporterOtlpEndpointName = "OTEL_EXPORTER_OTLP_ENDPOINT"
	envVarOtelExporterOtlpProtocolName = "OTEL_EXPORTER_OTLP_PROTOCOL"
	envVarDash0CollectorBaseUrlName    = "DASH0_OTEL_COLLECTOR_BASE_URL"
	envVarDash0NodeIp                  = "DASH0_NODE_IP"
	envVarDash0NamespaceName           = "DASH0_NAMESPACE_NAME"
	envVarDash0PodName                 = "DASH0_POD_NAME"
	envVarDash0PodUid                  = "DASH0_POD_UID"
	envVarDash0ContainerName           = "DASH0_CONTAINER_NAME"
	envVarDash0ServiceName             = "DASH0_SERVICE_NAME"
	envVarDash0ServiceNamespace        = "DASH0_SERVICE_NAMESPACE"
	envVarDash0ServiceVersion          = "DASH0_SERVICE_VERSION"
	envVarDash0ResourceAttributes      = "DASH0_RESOURCE_ATTRIBUTES"

	safeToEviceLocalVolumesAnnotationName = "cluster-autoscaler.kubernetes.io/safe-to-evict-local-volumes"
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

type ResourceModifier struct {
	instrumentationMetadata util.InstrumentationMetadata
	logger                  *logr.Logger
}

func NewResourceModifier(
	instrumentationMetadata util.InstrumentationMetadata,
	logger *logr.Logger,
) *ResourceModifier {
	return &ResourceModifier{
		instrumentationMetadata: instrumentationMetadata,
		logger:                  logger,
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
	util.AddInstrumentationLabels(&job.ObjectMeta, false, m.instrumentationMetadata)
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
	util.AddInstrumentationLabels(&pod.ObjectMeta, true, m.instrumentationMetadata)
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
	util.AddInstrumentationLabels(workloadMeta, true, m.instrumentationMetadata)
	util.AddInstrumentationLabels(&podTemplateSpec.ObjectMeta, true, m.instrumentationMetadata)
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

	initContainer := &corev1.Container{
		Name:  initContainerName,
		Image: m.instrumentationMetadata.InitContainerImage,
		Env: []corev1.EnvVar{
			{
				Name:  dash0DirectoryEnvVarName,
				Value: dash0InstrumentationBaseDirectory,
			},
		},
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
		Resources: corev1.ResourceRequirements{
			Requests: corev1.ResourceList{
				corev1.ResourceCPU:    resource.MustParse("250m"),
				corev1.ResourceMemory: resource.MustParse("150Mi"),
			},
		},
		VolumeMounts: []corev1.VolumeMount{
			{
				Name:      dash0VolumeName,
				ReadOnly:  false,
				MountPath: dash0InstrumentationBaseDirectory,
			},
		},
	}

	if m.instrumentationMetadata.InitContainerImagePullPolicy != "" {
		initContainer.ImagePullPolicy = m.instrumentationMetadata.InitContainerImagePullPolicy
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
	m.handleLdPreloadEnvVar(container, perContainerLogger)

	m.addOrReplaceEnvironmentVariable(
		container,
		corev1.EnvVar{
			Name: envVarDash0NodeIp,
			ValueFrom: &corev1.EnvVarSource{
				FieldRef: &corev1.ObjectFieldSelector{
					FieldPath: "status.hostIP",
				},
			},
		},
	)

	collectorBaseUrlPattern := "http://$(%s):%d"

	// This should actually work - use the node's IPv6 for the collector base URL.
	// But apparently the Node.js OpenTelemetry SDK tries to resolve that as a hostname, resulting in
	// Error: getaddrinfo ENOTFOUND [2a05:d014:1bc2:3702:fc43:fec6:1d88:ace5]\n    at GetAddrInfoReqWrap.onlookup
	// all [as oncomplete] (node:dns:120:26)
	// Instead, we fall back to the service URL of the collector.
	// if m.instrumentationMetadata.IsIPv6Cluster {
	//	 collectorBaseUrlPattern = "http://[$(%s)]:%d"
	// }
	// Would be worth to give this another try after implementing
	// https://linear.app/dash0/issue/ENG-2132.
	// If successful, we can then also eliminate the setting OTelCollectorBaseUrl in all components.

	collectorBaseUrl := fmt.Sprintf(collectorBaseUrlPattern, envVarDash0NodeIp, otelcolresources.OtlpHttpHostPort)
	if m.instrumentationMetadata.IsIPv6Cluster {
		collectorBaseUrl = m.instrumentationMetadata.OTelCollectorBaseUrl
	}

	m.addOrReplaceEnvironmentVariable(
		container,
		corev1.EnvVar{
			Name:  envVarDash0CollectorBaseUrlName,
			Value: collectorBaseUrl,
		},
	)

	m.addOrReplaceEnvironmentVariable(
		container,
		corev1.EnvVar{
			Name:  envVarOtelExporterOtlpEndpointName,
			Value: collectorBaseUrl,
		},
	)

	m.addOrReplaceEnvironmentVariable(
		container,
		corev1.EnvVar{
			Name:  envVarOtelExporterOtlpProtocolName,
			Value: common.ProtocolHttpProtobuf,
		},
	)

	m.addOrReplaceEnvironmentVariable(
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

	m.addOrReplaceEnvironmentVariable(
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

	m.addOrReplaceEnvironmentVariable(
		container,
		corev1.EnvVar{
			Name: envVarDash0PodUid,
			ValueFrom: &corev1.EnvVarSource{
				FieldRef: &corev1.ObjectFieldSelector{
					FieldPath: "metadata.uid",
				},
			},
		},
	)

	m.addOrReplaceEnvironmentVariable(
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
	if podMetaHasName {
		m.addEnvVarFromLabelFieldSelector(container, envVarDash0ServiceName, util.AppKubernetesIoNameLabel)
		m.conditionallyAddEnvVarFromLabelFieldSelector(
			container,
			podMeta,
			envVarDash0ServiceNamespace,
			util.AppKubernetesIoPartOfLabel,
		)
		m.conditionallyAddEnvVarFromLabelFieldSelector(
			container,
			podMeta,
			envVarDash0ServiceVersion,
			util.AppKubernetesIoVersionLabel,
		)
	} else if workloadMetaHasName {
		m.addOrReplaceEnvironmentVariable(
			container,
			corev1.EnvVar{
				Name:  envVarDash0ServiceName,
				Value: nameFromWorkloadMeta,
			},
		)
		if partOfFromWorkloadMeta, workloadMetaHasPartOf := workloadMeta.Labels[util.AppKubernetesIoPartOfLabel]; workloadMetaHasPartOf {
			m.addOrReplaceEnvironmentVariable(
				container,
				corev1.EnvVar{
					Name:  envVarDash0ServiceNamespace,
					Value: partOfFromWorkloadMeta,
				},
			)
		}
		if versionFromWorkloadMeta, workloadMetaHasVersion := workloadMeta.Labels[util.AppKubernetesIoVersionLabel]; workloadMetaHasVersion {
			m.addOrReplaceEnvironmentVariable(
				container,
				corev1.EnvVar{
					Name:  envVarDash0ServiceVersion,
					Value: versionFromWorkloadMeta,
				},
			)
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
		m.addOrReplaceEnvironmentVariable(
			container,
			corev1.EnvVar{
				Name:  envVarDash0ResourceAttributes,
				Value: strings.Join(resourceAttributeList, ","),
			})
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

func (m *ResourceModifier) addOrReplaceEnvironmentVariable(container *corev1.Container, envVar corev1.EnvVar) {
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
	m.addOrReplaceEnvironmentVariable(
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
	m.removeLdPreload(container)
	m.removeEnvironmentVariable(container, envVarDash0NodeIp)
	m.removeEnvironmentVariable(container, envVarDash0CollectorBaseUrlName)
	m.removeEnvironmentVariable(container, envVarOtelExporterOtlpEndpointName)
	m.removeEnvironmentVariable(container, envVarOtelExporterOtlpProtocolName)
	m.removeEnvironmentVariable(container, envVarDash0NamespaceName)
	m.removeEnvironmentVariable(container, envVarDash0PodName)
	m.removeEnvironmentVariable(container, envVarDash0PodUid)
	m.removeEnvironmentVariable(container, envVarDash0ContainerName)
	m.removeEnvironmentVariable(container, envVarDash0ServiceNamespace)
	m.removeEnvironmentVariable(container, envVarDash0ServiceName)
	m.removeEnvironmentVariable(container, envVarDash0ServiceVersion)
	m.removeEnvironmentVariable(container, envVarDash0ResourceAttributes)
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
	} else {
		envVar := container.Env[idx]
		previousValue := envVar.Value
		if previousValue == "" && envVar.ValueFrom != nil {
			// Specified via ValueFrom, this has not been done by us, so we assume there is no Dash0-specific
			// LD_PRELOAD part.
			return
		} else if strings.TrimSpace(previousValue) == envVarLdPreloadValue {
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
}

func (m *ResourceModifier) removeEnvironmentVariable(container *corev1.Container, name string) {
	if container.Env == nil {
		return
	}
	container.Env = slices.DeleteFunc(container.Env, func(c corev1.EnvVar) bool {
		return c.Name == name
	})
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
