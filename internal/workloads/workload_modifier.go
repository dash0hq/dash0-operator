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

	"github.com/dash0hq/dash0-operator/internal/backendconnection/otelcolresources"
	"github.com/dash0hq/dash0-operator/internal/util"
)

const (
	initContainerName = "dash0-instrumentation"

	dash0VolumeName                   = "dash0-instrumentation"
	dash0DirectoryEnvVarName          = "DASH0_INSTRUMENTATION_FOLDER_DESTINATION"
	dash0InstrumentationBaseDirectory = "/__dash0__"
	dash0InstrumentationDirectory     = "/__dash0__/instrumentation"
	envVarLdPreloadName               = "LD_PRELOAD"
	envVarLdPreloadValue              = "/__dash0__/dash0_injector.so"
	envVarDash0CollectorBaseUrlName   = "DASH0_OTEL_COLLECTOR_BASE_URL"
	envVarDash0NodeIp                 = "DASH0_NODE_IP"
	envVarDash0PodUidName             = "DASH0_POD_UID"
	envVarDash0ContainerName          = "DASH0_CONTAINER_NAME"
)

var (
	defaultInitContainerUser              int64 = 1302
	defaultInitContainerGroup             int64 = 1302
	initContainerAllowPrivilegeEscalation       = false
	initContainerPrivileged                     = false
	initContainerReadOnlyRootFilesystem         = true
)

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

func (m *ResourceModifier) ModifyCronJob(cronJob *batchv1.CronJob) bool {
	return m.modifyResource(&cronJob.Spec.JobTemplate.Spec.Template, &cronJob.ObjectMeta)
}

func (m *ResourceModifier) ModifyDaemonSet(daemonSet *appsv1.DaemonSet) bool {
	return m.modifyResource(&daemonSet.Spec.Template, &daemonSet.ObjectMeta)
}

func (m *ResourceModifier) ModifyDeployment(deployment *appsv1.Deployment) bool {
	return m.modifyResource(&deployment.Spec.Template, &deployment.ObjectMeta)
}

func (m *ResourceModifier) ModifyJob(job *batchv1.Job) bool {
	return m.modifyResource(&job.Spec.Template, &job.ObjectMeta)
}

func (m *ResourceModifier) AddLabelsToImmutableJob(job *batchv1.Job) bool {
	util.AddInstrumentationLabels(&job.ObjectMeta, false, m.instrumentationMetadata)
	// adding labels always works and is a modification that requires an update
	return true
}

func (m *ResourceModifier) ModifyPod(pod *corev1.Pod) bool {
	if m.hasOwnerReference(pod) {
		return false
	}
	hasBeenModified := m.modifyPodSpec(&pod.Spec)
	if hasBeenModified {
		util.AddInstrumentationLabels(&pod.ObjectMeta, true, m.instrumentationMetadata)
	}
	return hasBeenModified
}

func (m *ResourceModifier) ModifyReplicaSet(replicaSet *appsv1.ReplicaSet) bool {
	if m.hasOwnerReference(replicaSet) {
		return false
	}
	return m.modifyResource(&replicaSet.Spec.Template, &replicaSet.ObjectMeta)
}

func (m *ResourceModifier) ModifyStatefulSet(statefulSet *appsv1.StatefulSet) bool {
	return m.modifyResource(&statefulSet.Spec.Template, &statefulSet.ObjectMeta)
}

func (m *ResourceModifier) modifyResource(podTemplateSpec *corev1.PodTemplateSpec, meta *metav1.ObjectMeta) bool {
	hasBeenModified := m.modifyPodSpec(&podTemplateSpec.Spec)
	if hasBeenModified {
		util.AddInstrumentationLabels(meta, true, m.instrumentationMetadata)
		util.AddInstrumentationLabels(&podTemplateSpec.ObjectMeta, true, m.instrumentationMetadata)
	}
	return hasBeenModified
}

func (m *ResourceModifier) modifyPodSpec(podSpec *corev1.PodSpec) bool {
	originalSpec := podSpec.DeepCopy()
	m.addInstrumentationVolume(podSpec)
	m.addInitContainer(podSpec)
	for idx := range podSpec.Containers {
		container := &podSpec.Containers[idx]
		m.instrumentContainer(container)
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

func (m *ResourceModifier) instrumentContainer(container *corev1.Container) {
	perContainerLogger := m.logger.WithValues("container", container.Name)
	m.addMount(container)
	m.addEnvironmentVariables(container, perContainerLogger)
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

func (m *ResourceModifier) addEnvironmentVariables(container *corev1.Container, perContainerLogger logr.Logger) {
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
	// But apparently the Node.js OpenTelemetry SDK tries to resolve that as a a hostname, resulting in
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
			Name: envVarDash0PodUidName,
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
		// Note: This needs to be a point to the env var, otherwise updates would only be local to this function.
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
			if envVar.Value == "" {
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

func (m *ResourceModifier) RevertCronJob(cronJob *batchv1.CronJob) bool {
	return m.revertResource(&cronJob.Spec.JobTemplate.Spec.Template, &cronJob.ObjectMeta)
}

func (m *ResourceModifier) RevertDaemonSet(daemonSet *appsv1.DaemonSet) bool {
	return m.revertResource(&daemonSet.Spec.Template, &daemonSet.ObjectMeta)
}

func (m *ResourceModifier) RevertDeployment(deployment *appsv1.Deployment) bool {
	return m.revertResource(&deployment.Spec.Template, &deployment.ObjectMeta)
}

func (m *ResourceModifier) RemoveLabelsFromImmutableJob(job *batchv1.Job) bool {
	util.RemoveInstrumentationLabels(&job.ObjectMeta)
	// removing labels always works and is a modification that requires an update
	return true
}

func (m *ResourceModifier) RevertReplicaSet(replicaSet *appsv1.ReplicaSet) bool {
	if m.hasOwnerReference(replicaSet) {
		return false
	}
	return m.revertResource(&replicaSet.Spec.Template, &replicaSet.ObjectMeta)
}

func (m *ResourceModifier) RevertStatefulSet(statefulSet *appsv1.StatefulSet) bool {
	return m.revertResource(&statefulSet.Spec.Template, &statefulSet.ObjectMeta)
}

func (m *ResourceModifier) revertResource(podTemplateSpec *corev1.PodTemplateSpec, meta *metav1.ObjectMeta) bool {
	if util.InstrumentationAttemptHasFailed(meta) {
		// resource has never been instrumented successfully, only remove labels
		util.RemoveInstrumentationLabels(meta)
		util.RemoveInstrumentationLabels(&podTemplateSpec.ObjectMeta)
		return true
	}
	hasBeenModified := m.revertPodSpec(&podTemplateSpec.Spec)
	if hasBeenModified {
		util.RemoveInstrumentationLabels(meta)
		util.RemoveInstrumentationLabels(&podTemplateSpec.ObjectMeta)
		return true
	}
	return false
}

func (m *ResourceModifier) revertPodSpec(podSpec *corev1.PodSpec) bool {
	originalSpec := podSpec.DeepCopy()
	m.removeInstrumentationVolume(podSpec)
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
	m.removeEnvironmentVariable(container, envVarDash0PodUidName)
	m.removeEnvironmentVariable(container, envVarDash0ContainerName)
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
		} else if previousValue == envVarLdPreloadValue {
			container.Env = slices.Delete(container.Env, idx, idx+1)
			return
		}

		container.Env[idx].Value = strings.Replace(previousValue, envVarLdPreloadValue, "", -1)
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

func (m *ResourceModifier) hasOwnerReference(workload client.Object) bool {
	return len(workload.GetOwnerReferences()) > 0
}
