// SPDX-FileCopyrightText: Copyright 2024 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package webhook

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"reflect"
	"slices"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"
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
	log     = logf.Log.WithName("dash0-webhook")
	decoder = scheme.Codecs.UniversalDecoder()

	defaultInitContainerUser              int64 = 1302
	defaultInitContainerGroup             int64 = 1302
	initContainerAllowPrivilegeEscalation       = false
	initContainerPrivileged                     = false
	initContainerReadOnlyRootFilesystem         = true
)

type WebhookHandler struct {
	client.Client
	*admission.Decoder
	record.EventRecorder
}

func (wh *WebhookHandler) SetupWebhookWithManager(mgr ctrl.Manager) error {
	webhook := &admission.Webhook{
		Handler: wh,
	}

	handler, err := admission.StandaloneWebhook(webhook, admission.StandaloneOptions{})
	if err != nil {
		return err
	}
	mgr.GetWebhookServer().Register("/v1alpha1/inject/dash0", handler)

	return nil
}

func (wh *WebhookHandler) Handle(ctx context.Context, request admission.Request) admission.Response {
	l := log.WithValues("gvk", request.Kind)
	l.Info("incoming admission request")

	gkv := request.Kind
	group := gkv.Group
	version := gkv.Version
	kind := gkv.Kind

	if group == "apps" && version == "v1" && kind == "Deployment" {
		deployment := &appsv1.Deployment{}
		if _, _, err := decoder.Decode(request.Object.Raw, nil, deployment); err != nil {
			err := fmt.Errorf("cannot parse resource into a %s/%s.%s: %w", group, version, kind, err)
			return admission.Errored(http.StatusInternalServerError, fmt.Errorf("error while parsing the resource: %w", err))
		}

		deploymentSpecTemplate := &deployment.Spec.Template
		originalSpec := deploymentSpecTemplate.Spec.DeepCopy()
		if err := wh.modifyPodSpec(&deploymentSpecTemplate.Spec); err != nil {
			return admission.Errored(http.StatusInternalServerError, fmt.Errorf("injecting into pod spec failed: %w", err))
		}
		if reflect.DeepEqual(originalSpec, &deploymentSpecTemplate.Spec) {
			return admission.Allowed("no changes")
		}

		marshalled, err := json.Marshal(deployment)
		if err != nil {
			return admission.Allowed(fmt.Errorf("error when marshalling modfied resource to JSON: %w", err).Error())
		}
		return admission.PatchResponseFromRaw(request.Object.Raw, marshalled)
	} else {
		l.Info("resource type not supported", "group", group, "version", version, "kind", kind)
		return admission.Allowed("unknown resource type")
	}
}

func (wh *WebhookHandler) modifyPodSpec(podSpec *corev1.PodSpec) error {
	wh.addInstrumentationVolume(podSpec)
	wh.addInitContainer(podSpec)
	for idx, _ := range podSpec.Containers {
		container := &podSpec.Containers[idx]
		wh.instrumentContainer(container)
	}
	return nil
}

func (wh *WebhookHandler) addInstrumentationVolume(podSpec *corev1.PodSpec) {
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

func (wh *WebhookHandler) addInitContainer(podSpec *corev1.PodSpec) {
	// The init container has all the instrumentation packages (e.g. the Dash0 Node.js distribution etc.), stored under
	// /dash0/instrumentation. Its main responsibility is to copy these files to the Kubernetes volume created and mounted in
	// addInstrumentationVolume (mounted at /opt/dash0/instrumentation in the init container and also in the target containers).

	if podSpec.InitContainers == nil {
		podSpec.InitContainers = make([]corev1.Container, 0)
	}
	idx := slices.IndexFunc(podSpec.InitContainers, func(c corev1.Container) bool {
		return c.Name == initContainerName
	})
	initContainer := wh.createInitContainer(podSpec)
	if idx < 0 {
		podSpec.InitContainers = append(podSpec.InitContainers, *initContainer)
	} else {
		podSpec.InitContainers[idx] = *initContainer
	}
}

func (wh *WebhookHandler) createInitContainer(podSpec *corev1.PodSpec) *corev1.Container {
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

func (wh *WebhookHandler) instrumentContainer(container *corev1.Container) {
	wh.addMount(container)
	wh.addEnvironmentVariables(container)
}

func (wh *WebhookHandler) addMount(container *corev1.Container) {
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

func (wh *WebhookHandler) addEnvironmentVariables(container *corev1.Container) {
	// For now, we directly modify NODE_OPTIONS. Consider migrating to an LD_PRELOAD hook at some point.
	wh.addEnvironmentVariable(container, envVarNodeOptionsName, envVarNodeOptionsValue)
}

func (wh *WebhookHandler) addEnvironmentVariable(container *corev1.Container, name string, value string) {
	if container.Env == nil {
		container.Env = make([]corev1.EnvVar, 0)
	}
	idx := slices.IndexFunc(container.Env, func(c corev1.EnvVar) bool {
		return c.Name == name
	})

	envVar := &corev1.EnvVar{
		Name:  name,
		Value: value,
	}
	if idx < 0 {
		container.Env = append(container.Env, *envVar)
	} else {
		container.Env[idx] = *envVar
	}
}

func (wh *WebhookHandler) InjectClient(c client.Client) error {
	wh.Client = c
	return nil
}

func (wh *WebhookHandler) InjectDecoder(d *admission.Decoder) error {
	wh.Decoder = d
	return nil
}
