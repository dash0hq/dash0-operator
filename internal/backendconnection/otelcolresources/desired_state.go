// SPDX-FileCopyrightText: Copyright 2024 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package otelcolresources

import (
	"bytes"
	_ "embed"
	"fmt"
	"net/url"
	"path/filepath"
	"text/template"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/dash0hq/dash0-operator/internal/dash0/util"
)

type oTelColConfig struct {
	Namespace          string
	NamePrefix         string
	IngressEndpoint    string
	AuthorizationToken string
	SecretRef          string
	Images             util.Images
}

func (c *oTelColConfig) hasAuthentication() bool {
	return c.SecretRef != "" || c.AuthorizationToken != ""
}

type exportProtocol string

const (
	grpcExportProtocol exportProtocol = "grpc"
	httpExportProtocol exportProtocol = "http"
)

type collectorConfigurationTemplateValues struct {
	HasExportAuthentication  bool
	IngressEndpoint          string
	ExportProtocol           exportProtocol
	IgnoreLogsFromNamespaces []string
}

const (
	serviceComponent = "agent-collector"

	openTelemetryCollector      = "opentelemetry-collector"
	openTelemetryCollectorAgent = "opentelemetry-collector-agent"

	configReloader = "configuration-reloader"

	// label keys
	appKubernetesIoNameKey           = "app.kubernetes.io/name"
	appKubernetesIoInstanceKey       = "app.kubernetes.io/instance"
	appKubernetesIoComponentLabelKey = "app.kubernetes.io/component"
	appKubernetesIoManagedByKey      = "app.kubernetes.io/managed-by"
	dash0OptOutLabelKey              = "dash0.com/enable"

	// label values
	appKubernetesIoNameValue      = openTelemetryCollector
	appKubernetesIoInstanceValue  = "dash0-operator"
	appKubernetesIoManagedByValue = "dash0-operator"

	authTokenEnvVarName = "AUTH_TOKEN"

	collectorConfigurationYaml = "config.yaml"

	pidFileVolumeName = "opentelemetry-collector-pidfile"

	offsetsDirPath = "/var/otelcol/filelogreceiver_offsets"
)

const (
	otlpGrpcPort   = 4317
	otlpHttpPort   = 4318
	probesHttpPort = 13133
)

var (
	daemonSetMatchLabels = map[string]string{
		appKubernetesIoNameKey:           appKubernetesIoNameValue,
		appKubernetesIoInstanceKey:       appKubernetesIoInstanceValue,
		appKubernetesIoComponentLabelKey: serviceComponent,
	}

	//go:embed config.yaml.template
	collectorConfigurationTemplateSource string
	collectorConfigurationTemplate       = template.Must(template.New("collector-configuration").Parse(collectorConfigurationTemplateSource))
)

func assembleDesiredState(config *oTelColConfig) ([]client.Object, error) {
	if config.IngressEndpoint == "" {
		return nil, fmt.Errorf("no ingress endpoint provided, unable to create the OpenTelemetry collector")
	}

	var desiredState []client.Object
	desiredState = append(desiredState, serviceAccount(config))

	collectorConfigMap, err := collectorConfigConfigMap(config)
	if err != nil {
		return desiredState, err
	}
	desiredState = append(desiredState, collectorConfigMap)

	filelogOffsetsConfigMap, err := filelogOffsetsConfigMap(config)
	if err != nil {
		return desiredState, err
	}
	desiredState = append(desiredState, filelogOffsetsConfigMap)

	desiredState = append(desiredState, clusterRole(config))
	desiredState = append(desiredState, clusterRoleBinding(config))
	desiredState = append(desiredState, role(config))
	desiredState = append(desiredState, roleBinding(config))
	desiredState = append(desiredState, service(config))
	desiredState = append(desiredState, daemonSet(config))
	return desiredState, nil
}

func serviceAccount(config *oTelColConfig) *corev1.ServiceAccount {
	return &corev1.ServiceAccount{
		TypeMeta: metav1.TypeMeta{
			Kind:       "ServiceAccount",
			APIVersion: "v1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      serviceAccountName(config.NamePrefix),
			Namespace: config.Namespace,
			Labels:    labels(false),
		},
	}
}

func renderCollectorConfigs(templateValues *collectorConfigurationTemplateValues) (string, error) {
	var collectorConfiguration bytes.Buffer
	if err := collectorConfigurationTemplate.Execute(&collectorConfiguration, templateValues); err != nil {
		return "", err
	}

	return collectorConfiguration.String(), nil
}

func collectorConfigConfigMap(config *oTelColConfig) (*corev1.ConfigMap, error) {
	ingressEndpoint := config.IngressEndpoint
	exportProtocol := grpcExportProtocol
	if url, err := url.ParseRequestURI(ingressEndpoint); err != nil {
		// Not a valid URL, assume it's grpc
	} else if url.Scheme == "https" || url.Scheme == "http" {
		exportProtocol = httpExportProtocol
	}

	collectorConfiguration, err := renderCollectorConfigs(&collectorConfigurationTemplateValues{
		IngressEndpoint:         ingressEndpoint,
		ExportProtocol:          exportProtocol,
		HasExportAuthentication: config.hasAuthentication(),
		IgnoreLogsFromNamespaces: []string{
			// Skipping kube-system, it requires bespoke filtering work
			"kube-system",
			// Skipping logs from the operator and the daemonset, otherwise
			// logs will compound in case of log parsing errors
			config.Namespace,
		},
	})
	if err != nil {
		return nil, fmt.Errorf("cannot render the collector configuration template: %w", err)
	}

	configMapData := map[string]string{
		collectorConfigurationYaml: collectorConfiguration,
	}

	return &corev1.ConfigMap{
		TypeMeta: metav1.TypeMeta{
			Kind:       "ConfigMap",
			APIVersion: "v1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      collectorConfigConfigMapName(config.NamePrefix),
			Namespace: config.Namespace,
			Labels:    labels(false),
		},
		Data: configMapData,
	}, nil
}

func filelogOffsetsConfigMap(config *oTelColConfig) (*corev1.ConfigMap, error) {
	return &corev1.ConfigMap{
		TypeMeta: metav1.TypeMeta{
			Kind:       "ConfigMap",
			APIVersion: "v1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      filelogReceiverOffsetsConfigMapName(config.NamePrefix),
			Namespace: config.Namespace,
			Labels:    labels(false),
		},
	}, nil
}

func role(config *oTelColConfig) *rbacv1.Role {
	return &rbacv1.Role{
		TypeMeta: metav1.TypeMeta{
			Kind:       "Role",
			APIVersion: "rbac.authorization.k8s.io/v1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      roleName(config.NamePrefix),
			Namespace: config.Namespace,
			Labels:    labels(false),
		},
		Rules: []rbacv1.PolicyRule{
			{
				APIGroups: []string{""},
				Resources: []string{"configmaps"},
				Verbs:     []string{"get", "watch", "list", "update", "patch"},
			},
		},
	}
}

func roleBinding(config *oTelColConfig) *rbacv1.RoleBinding {
	return &rbacv1.RoleBinding{
		TypeMeta: metav1.TypeMeta{
			Kind:       "RoleBinding",
			APIVersion: "rbac.authorization.k8s.io/v1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      name(config.NamePrefix, openTelemetryCollector),
			Namespace: config.Namespace,
			Labels:    labels(false),
		},
		RoleRef: rbacv1.RoleRef{
			APIGroup: "rbac.authorization.k8s.io",
			Kind:     "Role",
			Name:     roleName(config.NamePrefix),
		},
		Subjects: []rbacv1.Subject{{
			Kind:      "ServiceAccount",
			Name:      serviceAccountName(config.NamePrefix),
			Namespace: config.Namespace,
		}},
	}
}

func clusterRole(config *oTelColConfig) *rbacv1.ClusterRole {
	return &rbacv1.ClusterRole{
		TypeMeta: metav1.TypeMeta{
			Kind:       "ClusterRole",
			APIVersion: "rbac.authorization.k8s.io/v1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      clusterRoleName(config.NamePrefix),
			Namespace: config.Namespace,
			Labels:    labels(false),
		},
		Rules: []rbacv1.PolicyRule{
			{
				APIGroups: []string{""},
				Resources: []string{"pods", "namespaces"},
				Verbs:     []string{"get", "watch", "list"},
			},
			{
				APIGroups: []string{"apps"},
				Resources: []string{"replicasets"},
				Verbs:     []string{"get", "watch", "list"},
			},
			{
				APIGroups: []string{"extensions"},
				Resources: []string{"replicasets"},
				Verbs:     []string{"get", "watch", "list"},
			},
		},
	}
}

func clusterRoleBinding(config *oTelColConfig) *rbacv1.ClusterRoleBinding {
	return &rbacv1.ClusterRoleBinding{
		TypeMeta: metav1.TypeMeta{
			Kind:       "ClusterRoleBinding",
			APIVersion: "rbac.authorization.k8s.io/v1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      name(config.NamePrefix, openTelemetryCollector),
			Namespace: config.Namespace,
			Labels:    labels(false),
		},
		RoleRef: rbacv1.RoleRef{
			APIGroup: "rbac.authorization.k8s.io",
			Kind:     "ClusterRole",
			Name:     clusterRoleName(config.NamePrefix),
		},
		Subjects: []rbacv1.Subject{{
			Kind:      "ServiceAccount",
			Name:      serviceAccountName(config.NamePrefix),
			Namespace: config.Namespace,
		}},
	}
}

func service(config *oTelColConfig) *corev1.Service {
	return &corev1.Service{
		TypeMeta: metav1.TypeMeta{
			Kind:       "Service",
			APIVersion: "v1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      name(config.NamePrefix, openTelemetryCollector),
			Namespace: config.Namespace,
			Labels:    serviceLabels(),
		},
		Spec: corev1.ServiceSpec{
			Type: corev1.ServiceTypeClusterIP,
			Ports: []corev1.ServicePort{
				{
					Name:        "otlp",
					Port:        otlpGrpcPort,
					TargetPort:  intstr.FromInt32(otlpGrpcPort),
					Protocol:    corev1.ProtocolTCP,
					AppProtocol: ptr.To("grpc"),
				},
				{
					Name:       "otlp-http",
					Port:       otlpHttpPort,
					TargetPort: intstr.FromInt32(otlpHttpPort),
					Protocol:   corev1.ProtocolTCP,
				},
			},
			Selector: map[string]string{
				appKubernetesIoNameKey:           appKubernetesIoNameValue,
				appKubernetesIoInstanceKey:       appKubernetesIoInstanceValue,
				appKubernetesIoComponentLabelKey: serviceComponent,
			},
			InternalTrafficPolicy: ptr.To(corev1.ServiceInternalTrafficPolicyLocal),
		},
	}
}

func daemonSet(config *oTelColConfig) *appsv1.DaemonSet {
	configMapItems := []corev1.KeyToPath{{
		Key:  collectorConfigurationYaml,
		Path: collectorConfigurationYaml,
	}}

	collectorPidFilePath := "/etc/otelcol/run/pid.file"

	pidFileVolumeSizeLimit := resource.MustParse("1M")
	offsetsVolumeSizeLimit := resource.MustParse("10M")
	volumes := []corev1.Volume{
		{
			Name: "filelogreceiver-offsets",
			VolumeSource: corev1.VolumeSource{
				EmptyDir: &corev1.EmptyDirVolumeSource{
					SizeLimit: &offsetsVolumeSizeLimit,
				},
			},
		},
		{
			Name: "node-pod-logs",
			VolumeSource: corev1.VolumeSource{
				HostPath: &corev1.HostPathVolumeSource{
					Path: "/var/log/pods/",
				},
			},
		},
		{
			Name: "node-docker-container-logs",
			VolumeSource: corev1.VolumeSource{
				HostPath: &corev1.HostPathVolumeSource{
					Path: "/var/lib/docker/containers",
				},
			},
		},
		{
			Name: "opentelemetry-collector-configmap",
			VolumeSource: corev1.VolumeSource{
				ConfigMap: &corev1.ConfigMapVolumeSource{
					LocalObjectReference: corev1.LocalObjectReference{
						Name: collectorConfigConfigMapName(config.NamePrefix),
					},
					Items: configMapItems,
				},
			},
		},
		{
			Name: pidFileVolumeName,
			VolumeSource: corev1.VolumeSource{
				EmptyDir: &corev1.EmptyDirVolumeSource{
					SizeLimit: &pidFileVolumeSizeLimit,
				},
			},
		},
	}

	collectorConfigVolume := corev1.VolumeMount{
		Name:      "opentelemetry-collector-configmap",
		MountPath: "/etc/otelcol/conf",
		ReadOnly:  true,
	}

	collectorPidFileMountRW := corev1.VolumeMount{
		Name:      pidFileVolumeName,
		MountPath: filepath.Dir(collectorPidFilePath),
		ReadOnly:  false,
	}

	collectorPidFileMountRO := collectorPidFileMountRW
	collectorPidFileMountRO.ReadOnly = true

	filelogReceiverOffsetsVolumeMount := corev1.VolumeMount{
		Name:      "filelogreceiver-offsets",
		MountPath: offsetsDirPath,
		ReadOnly:  false,
	}

	collectorVolumeMounts := []corev1.VolumeMount{
		collectorConfigVolume,
		collectorPidFileMountRW,
		{
			Name:      "node-pod-logs",
			MountPath: "/var/log/pods",
			ReadOnly:  true,
		},
		// On Docker desktop and other runtimes using docker, the files in /var/log/pods
		// are simlinked to this folder
		{
			Name:      "node-docker-container-logs",
			MountPath: "/var/lib/docker/containers",
			ReadOnly:  true,
		},
		filelogReceiverOffsetsVolumeMount,
	}

	nodeNameFieldSpec := &corev1.ObjectFieldSelector{
		FieldPath: "spec.nodeName",
	}

	env := []corev1.EnvVar{
		{
			Name: "MY_POD_IP",
			ValueFrom: &corev1.EnvVarSource{
				FieldRef: &corev1.ObjectFieldSelector{
					FieldPath: "status.podIP",
				},
			},
		},
		{
			Name: "K8S_NODE_NAME",
			ValueFrom: &corev1.EnvVarSource{
				FieldRef: nodeNameFieldSpec,
			},
		},
		{
			Name:  "DASH0_COLLECTOR_PID_FILE",
			Value: collectorPidFilePath,
		},
		{
			Name:  "GOMEMLIMIT",
			Value: "400MiB",
		},
	}

	if config.hasAuthentication() {
		var authTokenEnvVar corev1.EnvVar

		if config.AuthorizationToken != "" {
			authTokenEnvVar = corev1.EnvVar{
				Name:  authTokenEnvVarName,
				Value: config.AuthorizationToken,
			}
		} else {
			authTokenEnvVar = corev1.EnvVar{
				Name: authTokenEnvVarName,
				ValueFrom: &corev1.EnvVarSource{
					SecretKeyRef: &corev1.SecretKeySelector{
						LocalObjectReference: corev1.LocalObjectReference{
							Name: config.SecretRef,
						},
						Key: "dash0-authorization-token", // TODO Make configurable
					},
				},
			}
		}

		env = append(env, authTokenEnvVar)
	}

	probe := corev1.Probe{
		ProbeHandler: corev1.ProbeHandler{
			HTTPGet: &corev1.HTTPGetAction{
				Path: "/",
				Port: intstr.FromInt32(probesHttpPort),
			},
		},
	}

	collectorConfigurationFilePath := "/etc/otelcol/conf/" + collectorConfigurationYaml

	collectorContainer := corev1.Container{
		Name:            openTelemetryCollector,
		Args:            []string{"--config=file:" + collectorConfigurationFilePath},
		SecurityContext: &corev1.SecurityContext{},
		Image:           config.Images.CollectorImage,
		Ports: []corev1.ContainerPort{
			{
				Name:          "otlp",
				Protocol:      corev1.ProtocolTCP,
				ContainerPort: otlpGrpcPort,
				HostPort:      otlpGrpcPort,
			},
			{
				Name:          "otlp-http",
				Protocol:      corev1.ProtocolTCP,
				ContainerPort: otlpHttpPort,
				HostPort:      otlpHttpPort,
			},
		},
		Env:            env,
		LivenessProbe:  &probe,
		ReadinessProbe: &probe,
		Resources: corev1.ResourceRequirements{
			Limits: corev1.ResourceList{
				corev1.ResourceMemory: resource.MustParse("500Mi"),
			},
		},
		VolumeMounts: collectorVolumeMounts,
	}
	if config.Images.CollectorImagePullPolicy != "" {
		collectorContainer.ImagePullPolicy = config.Images.CollectorImagePullPolicy
	}

	configurationReloaderContainer := corev1.Container{
		Name: configReloader,
		Args: []string{
			"--pidfile=" + collectorPidFilePath,
			collectorConfigurationFilePath,
		},
		SecurityContext: &corev1.SecurityContext{},
		Image:           config.Images.ConfigurationReloaderImage,
		Env: []corev1.EnvVar{
			{
				Name:  "GOMEMLIMIT",
				Value: "4MiB",
			},
		},
		Resources: corev1.ResourceRequirements{
			Limits: corev1.ResourceList{
				corev1.ResourceMemory: resource.MustParse("12Mi"),
			},
		},
		VolumeMounts: []corev1.VolumeMount{collectorConfigVolume, collectorPidFileMountRO},
	}
	if config.Images.ConfigurationReloaderImagePullPolicy != "" {
		configurationReloaderContainer.ImagePullPolicy = config.Images.ConfigurationReloaderImagePullPolicy
	}

	initFilelogOffsetSynchContainer := corev1.Container{
		Name:            "filelog-offset-init",
		Args:            []string{"--mode=init"},
		SecurityContext: &corev1.SecurityContext{},
		Image:           config.Images.FilelogOffsetSynchImage,
		Env: []corev1.EnvVar{
			{
				Name:  "GOMEMLIMIT",
				Value: "4MiB",
			},
			{
				Name:  "K8S_CONFIGMAP_NAMESPACE",
				Value: config.Namespace,
			},
			{
				Name:  "K8S_CONFIGMAP_NAME",
				Value: filelogReceiverOffsetsConfigMapName(config.NamePrefix),
			},

			{
				Name:  "FILELOG_OFFSET_DIRECTORY_PATH",
				Value: offsetsDirPath,
			},
			{
				Name: "K8S_NODE_NAME",
				ValueFrom: &corev1.EnvVarSource{
					FieldRef: nodeNameFieldSpec,
				},
			},
		},
		Resources: corev1.ResourceRequirements{
			Limits: corev1.ResourceList{
				corev1.ResourceMemory: resource.MustParse("12Mi"),
			},
		},
		VolumeMounts: []corev1.VolumeMount{filelogReceiverOffsetsVolumeMount},
	}
	if config.Images.FilelogOffsetSynchImagePullPolicy != "" {
		initFilelogOffsetSynchContainer.ImagePullPolicy = config.Images.FilelogOffsetSynchImagePullPolicy
	}

	filelogOffsetSynchContainer := corev1.Container{
		Name:            "filelog-offset-synch",
		Args:            []string{"--mode=synch"},
		SecurityContext: &corev1.SecurityContext{},
		Image:           config.Images.FilelogOffsetSynchImage,
		Env: []corev1.EnvVar{
			{
				Name:  "GOMEMLIMIT",
				Value: "4MiB",
			},
			{
				Name:  "K8S_CONFIGMAP_NAMESPACE",
				Value: config.Namespace,
			},
			{
				Name:  "K8S_CONFIGMAP_NAME",
				Value: filelogReceiverOffsetsConfigMapName(config.NamePrefix),
			},

			{
				Name:  "FILELOG_OFFSET_DIRECTORY_PATH",
				Value: offsetsDirPath,
			},
			{
				Name: "K8S_NODE_NAME",
				ValueFrom: &corev1.EnvVarSource{
					FieldRef: nodeNameFieldSpec,
				},
			},
		},
		Resources: corev1.ResourceRequirements{
			Limits: corev1.ResourceList{
				corev1.ResourceMemory: resource.MustParse("12Mi"),
			},
		},
		VolumeMounts: []corev1.VolumeMount{filelogReceiverOffsetsVolumeMount},
	}
	if config.Images.FilelogOffsetSynchImagePullPolicy != "" {
		filelogOffsetSynchContainer.ImagePullPolicy = config.Images.FilelogOffsetSynchImagePullPolicy
	}

	return &appsv1.DaemonSet{
		TypeMeta: metav1.TypeMeta{
			Kind:       "DaemonSet",
			APIVersion: "apps/v1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      name(config.NamePrefix, openTelemetryCollectorAgent),
			Namespace: config.Namespace,
			Labels:    labels(true),
		},
		Spec: appsv1.DaemonSetSpec{
			Selector: &metav1.LabelSelector{
				MatchLabels: daemonSetMatchLabels,
			},
			UpdateStrategy: appsv1.DaemonSetUpdateStrategy{
				Type: appsv1.RollingUpdateDaemonSetStrategyType,
			},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: daemonSetMatchLabels,
				},
				Spec: corev1.PodSpec{
					ServiceAccountName: serviceAccountName(config.NamePrefix),
					SecurityContext:    &corev1.PodSecurityContext{},
					// This setting is required to enable the configuration reloader process to send Unix signals to the
					// collector process.
					ShareProcessNamespace: &util.True,
					InitContainers:        []corev1.Container{initFilelogOffsetSynchContainer},
					Containers: []corev1.Container{
						collectorContainer,
						configurationReloaderContainer,
						filelogOffsetSynchContainer,
					},
					Volumes:     volumes,
					HostNetwork: false,
				},
			},
		},
	}
}

func serviceAccountName(namePrefix string) string {
	return name(namePrefix, openTelemetryCollector)
}

func filelogReceiverOffsetsConfigMapName(namePrefix string) string {
	return name(namePrefix, "filelogoffsets")
}

func collectorConfigConfigMapName(namePrefix string) string {
	return name(namePrefix, openTelemetryCollectorAgent)
}

func clusterRoleName(namePrefix string) string {
	return name(namePrefix, openTelemetryCollector)
}

func roleName(namePrefix string) string {
	return name(namePrefix, openTelemetryCollector)
}

func serviceLabels() map[string]string {
	lbls := labels(false)
	lbls[appKubernetesIoComponentLabelKey] = serviceComponent
	return lbls
}

func name(prefix string, suffix string) string {
	return fmt.Sprintf("%s-%s", prefix, suffix)
}

func labels(addOptOutLabel bool) map[string]string {
	lbls := map[string]string{
		appKubernetesIoNameKey:      appKubernetesIoNameValue,
		appKubernetesIoInstanceKey:  appKubernetesIoInstanceValue,
		appKubernetesIoManagedByKey: appKubernetesIoManagedByValue,
	}
	if addOptOutLabel {
		lbls[dash0OptOutLabelKey] = "false"
	}
	return lbls
}
