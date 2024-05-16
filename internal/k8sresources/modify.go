// SPDX-FileCopyrightText: Copyright 2024 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package k8sresources

import (
	"fmt"
	"reflect"
	"slices"

	"github.com/go-logr/logr"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
)

const (
	initContainerName  = "dash0-instrumentation"
	initContainerImage = "dash0-instrumentation:1.0.0"

	dash0VolumeName                   = "dash0-instrumentation"
	dash0DirectoryEnvVarName          = "DASH0_INSTRUMENTATION_FOLDER_DESTINATION"
	dash0InstrumentationBaseDirectory = "/opt/dash0"
	dash0InstrumentationDirectory     = "/opt/dash0/instrumentation"
	// envVarLdPreloadName  = "LD_PRELOAD"
	// envVarLdPreloadValue = "/opt/dash0/preload/inject.so"
	envVarNodeOptionsName  = "NODE_OPTIONS"
	envVarNodeOptionsValue = "--require /opt/dash0/instrumentation/node.js/node_modules/@dash0/opentelemetry/src/index.js"
)

var (
	defaultInitContainerUser              int64 = 1302
	defaultInitContainerGroup             int64 = 1302
	initContainerAllowPrivilegeEscalation       = false
	initContainerPrivileged                     = false
	initContainerReadOnlyRootFilesystem         = true
)

func ModifyPodSpec(podSpec *corev1.PodSpec, logger logr.Logger) bool {
	originalSpec := podSpec.DeepCopy()
	addInstrumentationVolume(podSpec)
	addInitContainer(podSpec)
	for idx := range podSpec.Containers {
		container := &podSpec.Containers[idx]
		instrumentContainer(container, logger)
	}
	return !reflect.DeepEqual(originalSpec, podSpec)
}

func addInstrumentationVolume(podSpec *corev1.PodSpec) {
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

func addInitContainer(podSpec *corev1.PodSpec) {
	// The init container has all the instrumentation packages (e.g. the Dash0 Node.js distribution etc.), stored under
	// /dash0/instrumentation. Its main responsibility is to copy these files to the Kubernetes volume created and mounted in
	// addInstrumentationVolume (mounted at /opt/dash0/instrumentation in the init container and also in the target containers).

	if podSpec.InitContainers == nil {
		podSpec.InitContainers = make([]corev1.Container, 0)
	}
	idx := slices.IndexFunc(podSpec.InitContainers, func(c corev1.Container) bool {
		return c.Name == initContainerName
	})
	initContainer := createInitContainer(podSpec)
	if idx < 0 {
		podSpec.InitContainers = append(podSpec.InitContainers, *initContainer)
	} else {
		podSpec.InitContainers[idx] = *initContainer
	}
}

func createInitContainer(podSpec *corev1.PodSpec) *corev1.Container {
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
		Image: initContainerImage,
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

func instrumentContainer(container *corev1.Container, logger logr.Logger) {
	logger = logger.WithValues("container", container.Name)
	addMount(container)
	addEnvironmentVariables(container, logger)
}

func addMount(container *corev1.Container) {
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

func addEnvironmentVariables(container *corev1.Container, logger logr.Logger) {
	// For now, we directly modify NODE_OPTIONS. Consider migrating to an LD_PRELOAD hook at some point.
	addOrPrependToEnvironmentVariable(container, envVarNodeOptionsName, envVarNodeOptionsValue, logger)
}

func addOrPrependToEnvironmentVariable(container *corev1.Container, name string, value string, logger logr.Logger) {
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
			logger.Info(
				fmt.Sprintf(
					"Dash0 cannot prepend anything to the environment variable %s as it is specified via "+
						"ValueFrom. This container will not be instrumented.",
					name))
			return
		}
		container.Env[idx].Value = fmt.Sprintf("%s %s", value, previousValue)
	}
}
