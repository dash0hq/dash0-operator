// SPDX-FileCopyrightText: Copyright 2024 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package k8sresources

import (
	"fmt"
	"reflect"
	"slices"
	"strconv"

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

	instrumentedLabelKey              = "dash0.instrumented"
	operatorVersionLabelKey           = "dash0.operator.version"
	initContainerImageVersionLabelKey = "dash0.initcontainer.image.version"
	instrumentedByLabelKey            = "dash0.instrumented.by"
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

func (m *ResourceModifier) ModifyCronJob(cronJob *batchv1.CronJob, namespace string) bool {
	return m.modifyResource(&cronJob.Spec.JobTemplate.Spec.Template, &cronJob.ObjectMeta, namespace)
}

func (m *ResourceModifier) ModifyDaemonSet(daemonSet *appsv1.DaemonSet, namespace string) bool {
	return m.modifyResource(&daemonSet.Spec.Template, &daemonSet.ObjectMeta, namespace)
}

func (m *ResourceModifier) ModifyDeployment(deployment *appsv1.Deployment, namespace string) bool {
	return m.modifyResource(&deployment.Spec.Template, &deployment.ObjectMeta, namespace)
}

func (m *ResourceModifier) ModifyJob(job *batchv1.Job, namespace string) bool {
	return m.modifyResource(&job.Spec.Template, &job.ObjectMeta, namespace)
}

func (m *ResourceModifier) AddLabelsToImmutableJob(job *batchv1.Job) {
	m.addInstrumentationLabels(&job.ObjectMeta, false)
}

func (m *ResourceModifier) ModifyReplicaSet(replicaSet *appsv1.ReplicaSet, namespace string) bool {
	ownerReferences := replicaSet.ObjectMeta.OwnerReferences
	if len(ownerReferences) > 0 {
		for _, ownerReference := range ownerReferences {
			if ownerReference.APIVersion == "apps/v1" && ownerReference.Kind == "Deployment" {
				return false
			}
		}
	}

	return m.modifyResource(&replicaSet.Spec.Template, &replicaSet.ObjectMeta, namespace)
}

func (m *ResourceModifier) ModifyStatefulSet(statefulSet *appsv1.StatefulSet, namespace string) bool {
	return m.modifyResource(&statefulSet.Spec.Template, &statefulSet.ObjectMeta, namespace)
}

func (m *ResourceModifier) modifyResource(podTemplateSpec *corev1.PodTemplateSpec, meta *metav1.ObjectMeta, namespace string) bool {
	hasBeenModified := m.modifyPodSpec(
		&podTemplateSpec.Spec,
		fmt.Sprintf(envVarDash0CollectorBaseUrlValueTemplate, namespace),
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
	m.addLabel(meta, instrumentedLabelKey, strconv.FormatBool(hasBeenInstrumented))
	m.addLabel(meta, operatorVersionLabelKey, m.instrumentationMetadata.OperatorVersion)
	m.addLabel(meta, initContainerImageVersionLabelKey, m.instrumentationMetadata.InitContainerImageVersion)
	m.addLabel(meta, instrumentedByLabelKey, m.instrumentationMetadata.InstrumentedBy)
}

func (m *ResourceModifier) addLabel(meta *metav1.ObjectMeta, key string, value string) {
	if meta.Labels == nil {
		meta.Labels = make(map[string]string, 1)
	}
	meta.Labels[key] = value
}
