// SPDX-FileCopyrightText: Copyright 2024 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package otelcolresources

import (
	"fmt"
	"path/filepath"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"

	dash0v1alpha1 "github.com/dash0hq/dash0-operator/api/dash0monitoring/v1alpha1"
	"github.com/dash0hq/dash0-operator/internal/dash0/selfmonitoring"
	"github.com/dash0hq/dash0-operator/internal/dash0/util"
)

type oTelColConfig struct {
	Namespace                   string
	NamePrefix                  string
	Export                      dash0v1alpha1.Export
	SelfMonitoringConfiguration selfmonitoring.SelfMonitoringConfiguration
	Images                      util.Images
	DevelopmentMode             bool
}

type collectorConfigurationTemplateValues struct {
	Exporters                []OtlpExporter
	IgnoreLogsFromNamespaces []string
}

const (
	OtlpGrpcHostPort = 40317
	OtlpHttpHostPort = 40318
	// ^ We deliberately do not use the default grpc/http ports as host ports. If there is another OTel collector
	// daemonset in the cluster (which is not managed by the operator), it will very likely use the 4317/4318 as host
	// ports. When the operator creates its daemonset, the pods of one of the two otelcol daemonsets would fail to start
	// due to port conflicts.

	rbacApiVersion   = "rbac.authorization.k8s.io/v1"
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
	otlpGrpcPort = 4317
	otlpHttpPort = 4318

	probesHttpPort = 13133
)

var (
	daemonSetMatchLabels = map[string]string{
		appKubernetesIoNameKey:           appKubernetesIoNameValue,
		appKubernetesIoInstanceKey:       appKubernetesIoInstanceValue,
		appKubernetesIoComponentLabelKey: serviceComponent,
	}
)

func assembleDesiredState(config *oTelColConfig) ([]client.Object, error) {
	var desiredState []client.Object
	desiredState = append(desiredState, serviceAccount(config))
	collectorConfigMap, err := assembleCollectorConfigMap(config)
	if err != nil {
		return desiredState, err
	}
	desiredState = append(desiredState, collectorConfigMap)
	desiredState = append(desiredState, assembleFilelogOffsetsConfigMap(config))
	desiredState = append(desiredState, assembleClusterRole(config))
	desiredState = append(desiredState, assembleClusterRoleBinding(config))
	desiredState = append(desiredState, assembleRole(config))
	desiredState = append(desiredState, assembleRoleBinding(config))
	desiredState = append(desiredState, assembleService(config))
	collectorDaemonSet, err := assembleDaemonSet(config)
	if err != nil {
		return desiredState, err
	}
	desiredState = append(desiredState, collectorDaemonSet)
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

func assembleFilelogOffsetsConfigMap(config *oTelColConfig) *corev1.ConfigMap {
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
	}
}

func assembleRole(config *oTelColConfig) *rbacv1.Role {
	return &rbacv1.Role{
		TypeMeta: metav1.TypeMeta{
			Kind:       "Role",
			APIVersion: rbacApiVersion,
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

func assembleRoleBinding(config *oTelColConfig) *rbacv1.RoleBinding {
	return &rbacv1.RoleBinding{
		TypeMeta: metav1.TypeMeta{
			Kind:       "RoleBinding",
			APIVersion: rbacApiVersion,
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

func assembleClusterRole(config *oTelColConfig) *rbacv1.ClusterRole {
	return &rbacv1.ClusterRole{
		TypeMeta: metav1.TypeMeta{
			Kind:       "ClusterRole",
			APIVersion: rbacApiVersion,
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

func assembleClusterRoleBinding(config *oTelColConfig) *rbacv1.ClusterRoleBinding {
	return &rbacv1.ClusterRoleBinding{
		TypeMeta: metav1.TypeMeta{
			Kind:       "ClusterRoleBinding",
			APIVersion: rbacApiVersion,
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

func assembleService(config *oTelColConfig) *corev1.Service {
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

func assembleDaemonSet(config *oTelColConfig) (*appsv1.DaemonSet, error) {
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
		// are symlinked to this folder.
		{
			Name:      "node-docker-container-logs",
			MountPath: "/var/lib/docker/containers",
			ReadOnly:  true,
		},
		filelogReceiverOffsetsVolumeMount,
	}

	nodeNameFieldSpec := corev1.ObjectFieldSelector{
		FieldPath: "spec.nodeName",
	}

	podUidFieldSpec := corev1.ObjectFieldSelector{
		FieldPath: "metadata.uid",
	}

	k8sNodeNameEnvVar := corev1.EnvVar{
		Name: "K8S_NODE_NAME",
		ValueFrom: &corev1.EnvVarSource{
			FieldRef: &nodeNameFieldSpec,
		},
	}

	k8sPodUidEnvVar := corev1.EnvVar{
		Name: "K8S_POD_UID",
		ValueFrom: &corev1.EnvVarSource{
			FieldRef: &podUidFieldSpec,
		},
	}

	collectorEnv := []corev1.EnvVar{
		{
			Name: "MY_POD_IP",
			ValueFrom: &corev1.EnvVarSource{
				FieldRef: &corev1.ObjectFieldSelector{
					FieldPath: "status.podIP",
				},
			},
		},
		k8sNodeNameEnvVar,
		k8sPodUidEnvVar,
		{
			Name:  "DASH0_COLLECTOR_PID_FILE",
			Value: collectorPidFilePath,
		},
		{
			Name:  "GOMEMLIMIT",
			Value: "400MiB",
		},
	}

	if config.Export.Dash0 != nil {
		authTokenEnvVar, err := util.CreateEnvVarForAuthorization(
			*config.Export.Dash0,
			authTokenEnvVarName,
		)
		if err != nil {
			return nil, err
		}
		collectorEnv = append(collectorEnv, authTokenEnvVar)
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
				HostPort:      int32(OtlpGrpcHostPort),
			},
			{
				Name:          "otlp-http",
				Protocol:      corev1.ProtocolTCP,
				ContainerPort: otlpHttpPort,
				HostPort:      int32(OtlpHttpHostPort),
			},
		},
		Env:            collectorEnv,
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
			k8sNodeNameEnvVar,
			k8sPodUidEnvVar,
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
			k8sNodeNameEnvVar,
			k8sPodUidEnvVar,
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
			k8sNodeNameEnvVar,
			k8sPodUidEnvVar,
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

	collectorDaemonSet := &appsv1.DaemonSet{
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

	if config.SelfMonitoringConfiguration.Enabled {
		err := selfmonitoring.EnableSelfMonitoringInCollectorDaemonSet(
			collectorDaemonSet,
			config.SelfMonitoringConfiguration,
			config.Images.GetOperatorVersion(),
			config.DevelopmentMode,
		)
		if err != nil {
			return nil, err
		}
	}

	return collectorDaemonSet, nil
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
