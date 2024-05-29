// SPDX-FileCopyrightText: Copyright 2024 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package workloads

import (
	"fmt"
	"reflect"
	"slices"
	"strconv"
	"strings"

	"github.com/go-logr/logr"
	appsv1 "k8s.io/api/apps/v1"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/dash0hq/dash0-operator/internal/util"
)

const (
	initContainerName          = "dash0-instrumentation"
	initContainerImageTemplate = "dash0-instrumentation:%s"

	dash0VolumeName                   = "dash0-instrumentation"
	dash0DirectoryEnvVarName          = "DASH0_INSTRUMENTATION_FOLDER_DESTINATION"
	dash0InstrumentationBaseDirectory = "/opt/dash0"
	dash0InstrumentationDirectory     = "/opt/dash0/instrumentation"
	// envVarLdPreloadName  = "LD_PRELOAD"
	// envVarLdPreloadValue = "/opt/dash0/preload/inject.so"
	envVarNodeOptionsName                    = "NODE_OPTIONS"
	envVarNodeOptionsValue                   = "--require /opt/dash0/instrumentation/node.js/node_modules/@dash0/opentelemetry/src/index.js"
	envVarDash0CollectorBaseUrlName          = "DASH0_OTEL_COLLECTOR_BASE_URL"
	envVarDash0CollectorBaseUrlValueTemplate = "http://dash0-opentelemetry-collector-daemonset.%s.svc.cluster.local:4318"
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

func (m *ResourceModifier) AddLabelsToImmutableJob(job *batchv1.Job) {
	m.addInstrumentationLabels(&job.ObjectMeta, false)
}

func (m *ResourceModifier) ModifyReplicaSet(replicaSet *appsv1.ReplicaSet) bool {
	if m.hasDeploymentOwnerReference(replicaSet) {
		return false
	}
	return m.modifyResource(&replicaSet.Spec.Template, &replicaSet.ObjectMeta)
}

func (m *ResourceModifier) ModifyStatefulSet(statefulSet *appsv1.StatefulSet) bool {
	return m.modifyResource(&statefulSet.Spec.Template, &statefulSet.ObjectMeta)
}

func (m *ResourceModifier) modifyResource(podTemplateSpec *corev1.PodTemplateSpec, meta *metav1.ObjectMeta) bool {
	hasBeenModified := m.modifyPodSpec(
		&podTemplateSpec.Spec,
		fmt.Sprintf(envVarDash0CollectorBaseUrlValueTemplate, meta.Namespace),
	)
	if hasBeenModified {
		m.addInstrumentationLabels(meta, true)
		m.addInstrumentationLabels(&podTemplateSpec.ObjectMeta, true)
	}
	return hasBeenModified
}

func (m *ResourceModifier) modifyPodSpec(podSpec *corev1.PodSpec, dash0CollectorBaseUrl string) bool {
	originalSpec := podSpec.DeepCopy()
	m.addInstrumentationVolume(podSpec)
	m.addInitContainer(podSpec)
	for idx := range podSpec.Containers {
		container := &podSpec.Containers[idx]
		m.instrumentContainer(container, dash0CollectorBaseUrl)
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
				SizeLimit: resource.NewScaledQuantity(150, resource.Mega),
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
	// /dash0/instrumentation. Its main responsibility is to copy these files to the Kubernetes volume created and mounted in
	// addInstrumentationVolume (mounted at /opt/dash0/instrumentation in the init container and also in the target containers).

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

	return &corev1.Container{
		Name:  initContainerName,
		Image: fmt.Sprintf(initContainerImageTemplate, m.instrumentationMetadata.InitContainerImageVersion),
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
}

func (m *ResourceModifier) instrumentContainer(container *corev1.Container, dash0CollectorBaseUrl string) {
	perContainerLogger := m.logger.WithValues("container", container.Name)
	m.addMount(container)
	m.addEnvironmentVariables(container, dash0CollectorBaseUrl, perContainerLogger)
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

func (m *ResourceModifier) addEnvironmentVariables(container *corev1.Container, dash0CollectorBaseUrl string, perContainerLogger logr.Logger) {
	// For now, we directly modify NODE_OPTIONS. Consider migrating to an LD_PRELOAD hook at some point.
	m.addOrPrependToEnvironmentVariable(container, envVarNodeOptionsName, envVarNodeOptionsValue, perContainerLogger)

	m.addOrReplaceEnvironmentVariable(
		container,
		envVarDash0CollectorBaseUrlName,
		dash0CollectorBaseUrl,
	)
}

func (m *ResourceModifier) addOrPrependToEnvironmentVariable(
	container *corev1.Container,
	name string,
	value string,
	perContainerLogger logr.Logger,
) {
	if container.Env == nil {
		container.Env = make([]corev1.EnvVar, 0)
	}
	idx := slices.IndexFunc(container.Env, func(c corev1.EnvVar) bool {
		return c.Name == name
	})

	if idx < 0 {
		container.Env = append(container.Env, corev1.EnvVar{
			Name:  name,
			Value: value,
		})
	} else {
		envVar := container.Env[idx]
		previousValue := envVar.Value
		if previousValue == "" && envVar.ValueFrom != nil {
			perContainerLogger.Info(
				fmt.Sprintf(
					"Dash0 cannot prepend anything to the environment variable %s as it is specified via "+
						"ValueFrom. This container will not be instrumented.",
					name))
			return
		}
		container.Env[idx].Value = fmt.Sprintf("%s %s", value, previousValue)
	}
}

func (m *ResourceModifier) addOrReplaceEnvironmentVariable(container *corev1.Container, name string, value string) {
	if container.Env == nil {
		container.Env = make([]corev1.EnvVar, 0)
	}
	idx := slices.IndexFunc(container.Env, func(c corev1.EnvVar) bool {
		return c.Name == name
	})

	if idx < 0 {
		container.Env = append(container.Env, corev1.EnvVar{
			Name:  name,
			Value: value,
		})
	} else {
		container.Env[idx].ValueFrom = nil
		container.Env[idx].Value = value
	}
}

func (m *ResourceModifier) addInstrumentationLabels(
	meta *metav1.ObjectMeta,
	hasBeenInstrumented bool,
) {
	util.AddLabel(meta, util.InstrumentedLabelKey, strconv.FormatBool(hasBeenInstrumented))
	util.AddLabel(meta, util.OperatorVersionLabelKey, m.instrumentationMetadata.OperatorVersion)
	util.AddLabel(meta, util.InitContainerImageVersionLabelKey, m.instrumentationMetadata.InitContainerImageVersion)
	util.AddLabel(meta, util.InstrumentedByLabelKey, m.instrumentationMetadata.InstrumentedBy)
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

func (m *ResourceModifier) RevertJob(job *batchv1.Job) bool {
	return m.revertResource(&job.Spec.Template, &job.ObjectMeta)
}

func (m *ResourceModifier) RemoveLabelsFromImmutableJob(job *batchv1.Job) {
	m.removeInstrumentationLabels(&job.ObjectMeta)
}

func (m *ResourceModifier) RevertReplicaSet(replicaSet *appsv1.ReplicaSet) bool {
	if m.hasDeploymentOwnerReference(replicaSet) {
		return false
	}
	return m.revertResource(&replicaSet.Spec.Template, &replicaSet.ObjectMeta)
}

func (m *ResourceModifier) RevertStatefulSet(statefulSet *appsv1.StatefulSet) bool {
	return m.revertResource(&statefulSet.Spec.Template, &statefulSet.ObjectMeta)
}

func (m *ResourceModifier) revertResource(podTemplateSpec *corev1.PodTemplateSpec, meta *metav1.ObjectMeta) bool {
	if meta.GetLabels()[util.InstrumentedLabelKey] == "false" {
		// resource has never been instrumented successfully, only remove labels
		m.removeInstrumentationLabels(meta)
		m.removeInstrumentationLabels(&podTemplateSpec.ObjectMeta)
		return true
	}
	hasBeenModified := m.revertPodSpec(&podTemplateSpec.Spec)
	if hasBeenModified {
		m.removeInstrumentationLabels(meta)
		m.removeInstrumentationLabels(&podTemplateSpec.ObjectMeta)
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
	m.removeNodeOptions(container)
	m.removeEnvironmentVariable(container, envVarDash0CollectorBaseUrlName)
}

func (m *ResourceModifier) removeNodeOptions(container *corev1.Container) {
	if container.Env == nil {
		return
	}
	idx := slices.IndexFunc(container.Env, func(c corev1.EnvVar) bool {
		return c.Name == envVarNodeOptionsName
	})

	if idx < 0 {
		return
	} else {
		envVar := container.Env[idx]
		previousValue := envVar.Value
		if previousValue == "" && envVar.ValueFrom != nil {
			// Specified via ValueFrom, this has not been done by us, so we assume there is no Dash0-specific
			// NODE_OPTIONS part.
			return
		} else if previousValue == envVarNodeOptionsValue {
			container.Env = slices.Delete(container.Env, idx, idx+1)
		}

		container.Env[idx].Value = strings.Replace(previousValue, envVarNodeOptionsValue, "", -1)
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

func (m *ResourceModifier) removeInstrumentationLabels(meta *metav1.ObjectMeta) {
	m.removeLabel(meta, util.InstrumentedLabelKey)
	m.removeLabel(meta, util.OperatorVersionLabelKey)
	m.removeLabel(meta, util.InitContainerImageVersionLabelKey)
	m.removeLabel(meta, util.InstrumentedByLabelKey)
}

func (m *ResourceModifier) removeLabel(meta *metav1.ObjectMeta, key string) {
	delete(meta.Labels, key)
}

func (m *ResourceModifier) hasDeploymentOwnerReference(replicaSet *appsv1.ReplicaSet) bool {
	ownerReferences := replicaSet.ObjectMeta.OwnerReferences
	if len(ownerReferences) > 0 {
		for _, ownerReference := range ownerReferences {
			if ownerReference.APIVersion == "apps/v1" && ownerReference.Kind == "Deployment" {
				return true
			}
		}
	}
	return false
}
