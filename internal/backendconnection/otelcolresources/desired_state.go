// SPDX-FileCopyrightText: Copyright 2024 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package otelcolresources

import (
	"fmt"
	"path/filepath"
	"strings"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"

	dash0v1alpha1 "github.com/dash0hq/dash0-operator/api/dash0monitoring/v1alpha1"
	"github.com/dash0hq/dash0-operator/internal/selfmonitoringapiaccess"
	"github.com/dash0hq/dash0-operator/internal/util"
)

type oTelColConfig struct {
	Namespace                                        string
	NamePrefix                                       string
	Export                                           dash0v1alpha1.Export
	SelfMonitoringAndApiAccessConfiguration          selfmonitoringapiaccess.SelfMonitoringAndApiAccessConfiguration
	KubernetesInfrastructureMetricsCollectionEnabled bool
	Images                                           util.Images
	IsIPv6Cluster                                    bool
	DevelopmentMode                                  bool
}

// This type just exists to ensure all created objects go through addCommonMetadata.
type clientObject struct {
	object client.Object
}

const (
	OtlpGrpcHostPort = 40317
	OtlpHttpHostPort = 40318
	// ^ We deliberately do not use the default grpc/http ports as host ports. If there is another OTel collector
	// daemonset in the cluster (which is not managed by the operator), it will very likely use the 4317/4318 as host
	// ports. When the operator creates its daemonset, the pods of one of the two otelcol daemonsets would fail to start
	// due to port conflicts.

	otlpGrpcPort = 4317
	otlpHttpPort = 4318

	probesHttpPort = 13133

	rbacApiGroup = "rbac.authorization.k8s.io"

	openTelemetryCollector                     = "opentelemetry-collector"
	openTelemetryCollectorDaemonSetNameSuffix  = "opentelemetry-collector-agent"
	openTelemetryCollectorDeploymentNameSuffix = "cluster-metrics-collector"

	daemonSetServiceComponent  = "agent-collector"
	deploymentServiceComponent = openTelemetryCollectorDeploymentNameSuffix

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

	configMapVolumeName            = "opentelemetry-collector-configmap"
	collectorConfigurationYaml     = "config.yaml"
	collectorConfigurationFilePath = "/etc/otelcol/conf/" + collectorConfigurationYaml

	collectorPidFilePath = "/etc/otelcol/run/pid.file"
	pidFileVolumeName    = "opentelemetry-collector-pidfile"
	offsetsDirPath       = "/var/otelcol/filelogreceiver_offsets"
)

var (
	rbacApiVersion = fmt.Sprintf("%s/v1", rbacApiGroup)

	daemonSetMatchLabels = map[string]string{
		appKubernetesIoNameKey:           appKubernetesIoNameValue,
		appKubernetesIoInstanceKey:       appKubernetesIoInstanceValue,
		appKubernetesIoComponentLabelKey: daemonSetServiceComponent,
	}
	deploymentMatchLabels = map[string]string{
		appKubernetesIoNameKey:           appKubernetesIoNameValue,
		appKubernetesIoInstanceKey:       appKubernetesIoInstanceValue,
		appKubernetesIoComponentLabelKey: deploymentServiceComponent,
	}

	nodeNameFieldSpec = corev1.ObjectFieldSelector{
		FieldPath: "spec.nodeName",
	}
	podUidFieldSpec = corev1.ObjectFieldSelector{
		FieldPath: "metadata.uid",
	}
	k8sNodeNameEnvVar = corev1.EnvVar{
		Name: "K8S_NODE_NAME",
		ValueFrom: &corev1.EnvVarSource{
			FieldRef: &nodeNameFieldSpec,
		},
	}
	k8sPodUidEnvVar = corev1.EnvVar{
		Name: "K8S_POD_UID",
		ValueFrom: &corev1.EnvVarSource{
			FieldRef: &podUidFieldSpec,
		},
	}

	configMapItems = []corev1.KeyToPath{{
		Key:  collectorConfigurationYaml,
		Path: collectorConfigurationYaml,
	}}

	collectorConfigVolume = corev1.VolumeMount{
		Name:      configMapVolumeName,
		MountPath: "/etc/otelcol/conf",
		ReadOnly:  true,
	}
	collectorPidFileMountRW = corev1.VolumeMount{
		Name:      pidFileVolumeName,
		MountPath: filepath.Dir(collectorPidFilePath),
		ReadOnly:  false,
	}
	filelogReceiverOffsetsVolumeMount = corev1.VolumeMount{
		Name:      "filelogreceiver-offsets",
		MountPath: offsetsDirPath,
		ReadOnly:  false,
	}

	collectorProbe = corev1.Probe{
		ProbeHandler: corev1.ProbeHandler{
			HTTPGet: &corev1.HTTPGetAction{
				Path: "/",
				Port: intstr.FromInt32(probesHttpPort),
			},
		},
	}

	deploymentReplicas int32 = 1
)

func assembleDesiredStateForUpsert(
	config *oTelColConfig,
	allMonitoringResources []dash0v1alpha1.Dash0Monitoring,
	resourceSpecs *OTelColResourceSpecs,
) ([]clientObject, error) {
	namespacesWithPrometheusScraping := make([]string, 0, len(allMonitoringResources))
	for _, monitoringResource := range allMonitoringResources {
		if util.ReadBoolPointerWithDefault(monitoringResource.Spec.PrometheusScrapingEnabled, true) {
			namespacesWithPrometheusScraping = append(namespacesWithPrometheusScraping, monitoringResource.Namespace)
		}
	}
	return assembleDesiredState(
		config,
		namespacesWithPrometheusScraping,
		resourceSpecs,
		false,
	)
}

func assembleDesiredStateForDelete(
	config *oTelColConfig,
	resourceSpecs *OTelColResourceSpecs,
) ([]clientObject, error) {
	return assembleDesiredState(
		config,
		nil,
		resourceSpecs,
		true,
	)
}

func assembleDesiredState(
	config *oTelColConfig,
	namespacesWithPrometheusScraping []string,
	resourceSpecs *OTelColResourceSpecs,
	forDeletion bool,
) ([]clientObject, error) {
	var desiredState []clientObject
	desiredState = append(desiredState, addCommonMetadata(assembleServiceAccountForDaemonSet(config)))
	daemonSetCollectorConfigMap, err := assembleDaemonSetCollectorConfigMap(
		config,
		namespacesWithPrometheusScraping,
		forDeletion,
	)
	if err != nil {
		return desiredState, err
	}
	desiredState = append(desiredState, addCommonMetadata(daemonSetCollectorConfigMap))
	desiredState = append(desiredState, addCommonMetadata(assembleFilelogOffsetsConfigMap(config)))
	desiredState = append(desiredState, addCommonMetadata(assembleClusterRoleForDaemonSet(config)))
	desiredState = append(desiredState, addCommonMetadata(assembleClusterRoleBindingForDaemonSet(config)))
	desiredState = append(desiredState, addCommonMetadata(assembleRole(config)))
	desiredState = append(desiredState, addCommonMetadata(assembleRoleBinding(config)))
	desiredState = append(desiredState, addCommonMetadata(assembleService(config)))
	collectorDaemonSet, err := assembleCollectorDaemonSet(config, resourceSpecs)
	if err != nil {
		return desiredState, err
	}
	desiredState = append(desiredState, addCommonMetadata(collectorDaemonSet))

	if config.KubernetesInfrastructureMetricsCollectionEnabled {
		desiredState = append(desiredState, addCommonMetadata(assembleServiceAccountForDeployment(config)))
		desiredState = append(desiredState, addCommonMetadata(assembleClusterRoleForDeployment(config)))
		desiredState = append(desiredState, addCommonMetadata(assembleClusterRoleBindingForDeployment(config)))
		deploymentCollectorConfigMap, err := assembleDeploymentCollectorConfigMap(config, forDeletion)
		if err != nil {
			return desiredState, err
		}
		desiredState = append(desiredState, addCommonMetadata(deploymentCollectorConfigMap))
		collectorDeployment, err := assembleCollectorDeployment(config, resourceSpecs)
		if err != nil {
			return desiredState, err
		}
		desiredState = append(desiredState, addCommonMetadata(collectorDeployment))
	}

	return desiredState, nil
}

func assembleServiceAccountForDaemonSet(config *oTelColConfig) *corev1.ServiceAccount {
	return &corev1.ServiceAccount{
		TypeMeta: metav1.TypeMeta{
			Kind:       "ServiceAccount",
			APIVersion: "v1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      daemonsetServiceAccountName(config.NamePrefix),
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
			Name:      FilelogReceiverOffsetsConfigMapName(config.NamePrefix),
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
			Name:      roleBindingName(config.NamePrefix),
			Namespace: config.Namespace,
			Labels:    labels(false),
		},
		RoleRef: rbacv1.RoleRef{
			APIGroup: rbacApiGroup,
			Kind:     "Role",
			Name:     roleName(config.NamePrefix),
		},
		Subjects: []rbacv1.Subject{{
			Kind:      "ServiceAccount",
			Name:      daemonsetServiceAccountName(config.NamePrefix),
			Namespace: config.Namespace,
		}},
	}
}

func assembleClusterRoleForDaemonSet(config *oTelColConfig) *rbacv1.ClusterRole {
	return &rbacv1.ClusterRole{
		TypeMeta: metav1.TypeMeta{
			Kind:       "ClusterRole",
			APIVersion: rbacApiVersion,
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:   DaemonSetClusterRoleName(config.NamePrefix),
			Labels: labels(false),
		},
		Rules: []rbacv1.PolicyRule{
			{
				APIGroups: []string{""},
				Resources: []string{
					"pods",
					"namespaces",
					"nodes",
					"configmaps",
					// required for Kubelet Metrics/Kubeletstats receiver
					"nodes/stats",
					// required for Prometheus receiver
					"endpoints",
					"services",
				},
				Verbs: []string{"get", "watch", "list"},
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
			{
				// Required for the EKS resource detector, to read the config map aws-auth in the namespace kube-system.
				APIGroups:     []string{""},
				Resources:     []string{"configmaps"},
				Verbs:         []string{"get"},
				ResourceNames: []string{"kube-system/aws-auth"},
			},
		},
	}
}

func assembleClusterRoleBindingForDaemonSet(config *oTelColConfig) *rbacv1.ClusterRoleBinding {
	return &rbacv1.ClusterRoleBinding{
		TypeMeta: metav1.TypeMeta{
			Kind:       "ClusterRoleBinding",
			APIVersion: rbacApiVersion,
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:   DaemonSetClusterRoleBindingName(config.NamePrefix),
			Labels: labels(false),
		},
		RoleRef: rbacv1.RoleRef{
			APIGroup: rbacApiGroup,
			Kind:     "ClusterRole",
			Name:     DaemonSetClusterRoleName(config.NamePrefix),
		},
		Subjects: []rbacv1.Subject{{
			Kind:      "ServiceAccount",
			Name:      daemonsetServiceAccountName(config.NamePrefix),
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
			Name:      ServiceName(config.NamePrefix),
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
				appKubernetesIoComponentLabelKey: daemonSetServiceComponent,
			},
			InternalTrafficPolicy: ptr.To(corev1.ServiceInternalTrafficPolicyLocal),
		},
	}
}

func assembleCollectorDaemonSet(config *oTelColConfig, resourceSpecs *OTelColResourceSpecs) (*appsv1.DaemonSet, error) {
	collectorContainer, err := assembleDaemonSetCollectorContainer(
		config,
		resourceSpecs.CollectorDaemonSetCollectorContainerResources,
	)
	if err != nil {
		return nil, err
	}

	collectorDaemonSet := &appsv1.DaemonSet{
		TypeMeta: metav1.TypeMeta{
			Kind:       "DaemonSet",
			APIVersion: "apps/v1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      DaemonSetName(config.NamePrefix),
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
					ServiceAccountName: daemonsetServiceAccountName(config.NamePrefix),
					SecurityContext:    &corev1.PodSecurityContext{},
					// This setting is required to enable the configuration reloader process to send Unix signals to the
					// collector process.
					ShareProcessNamespace: ptr.To(true),
					InitContainers: []corev1.Container{assembleFileLogOffsetSynchInitContainer(
						config,
						resourceSpecs.CollectorDaemonSetFileLogOffsetSynchContainerResources,
					)},
					Containers: []corev1.Container{
						collectorContainer,
						assembleConfigurationReloaderContainer(
							config,
							resourceSpecs.CollectorDaemonSetConfigurationReloaderContainerResources,
						),
						assembleFileLogOffsetSynchContainer(
							config,
							resourceSpecs.CollectorDaemonSetFileLogOffsetSynchContainerResources,
						),
					},
					Volumes:     assembleCollectorDaemonSetVolumes(config, configMapItems),
					HostNetwork: false,
				},
			},
		},
	}

	if config.SelfMonitoringAndApiAccessConfiguration.SelfMonitoringEnabled {
		err = selfmonitoringapiaccess.EnableSelfMonitoringInCollectorDaemonSet(
			collectorDaemonSet,
			config.SelfMonitoringAndApiAccessConfiguration,
			config.Images.GetOperatorVersion(),
			config.DevelopmentMode,
		)
		if err != nil {
			return nil, err
		}
	}

	return collectorDaemonSet, nil
}

func assembleFileLogOffsetSynchContainer(
	config *oTelColConfig,
	resourceRequirements ResourceRequirementsWithGoMemLimit,
) corev1.Container {
	filelogOffsetSynchContainer := corev1.Container{
		Name:            "filelog-offset-synch",
		Args:            []string{"--mode=synch"},
		SecurityContext: &corev1.SecurityContext{},
		Image:           config.Images.FilelogOffsetSynchImage,
		Env: []corev1.EnvVar{
			{
				Name:  "GOMEMLIMIT",
				Value: resourceRequirements.GoMemLimit,
			},
			{
				Name:  "K8S_CONFIGMAP_NAMESPACE",
				Value: config.Namespace,
			},
			{
				Name:  "K8S_CONFIGMAP_NAME",
				Value: FilelogReceiverOffsetsConfigMapName(config.NamePrefix),
			},

			{
				Name:  "FILELOG_OFFSET_DIRECTORY_PATH",
				Value: offsetsDirPath,
			},
			k8sNodeNameEnvVar,
			k8sPodUidEnvVar,
		},
		Resources:    resourceRequirements.ToResourceRequirements(),
		VolumeMounts: []corev1.VolumeMount{filelogReceiverOffsetsVolumeMount},
	}
	if config.Images.FilelogOffsetSynchImagePullPolicy != "" {
		filelogOffsetSynchContainer.ImagePullPolicy = config.Images.FilelogOffsetSynchImagePullPolicy
	}
	return filelogOffsetSynchContainer
}

func assembleCollectorDaemonSetVolumes(
	config *oTelColConfig,
	configMapItems []corev1.KeyToPath,
) []corev1.Volume {
	pidFileVolumeSizeLimit := resource.MustParse("1M")
	offsetsVolumeSizeLimit := resource.MustParse("10M")
	return []corev1.Volume{
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
			Name: configMapVolumeName,
			VolumeSource: corev1.VolumeSource{
				ConfigMap: &corev1.ConfigMapVolumeSource{
					LocalObjectReference: corev1.LocalObjectReference{
						Name: DaemonSetCollectorConfigConfigMapName(config.NamePrefix),
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
}

func assembleCollectorDaemonSetVolumeMounts() []corev1.VolumeMount {
	return []corev1.VolumeMount{
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
}

func assembleCollectorEnvVars(config *oTelColConfig, goMemLimit string) ([]corev1.EnvVar, error) {
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
			Value: goMemLimit,
		},
	}

	if config.Export.Dash0 != nil {
		authTokenEnvVar, err := util.CreateEnvVarForAuthorization(
			(*(config.Export.Dash0)).Authorization,
			authTokenEnvVarName,
		)
		if err != nil {
			return nil, err
		}
		collectorEnv = append(collectorEnv, authTokenEnvVar)
	}

	return collectorEnv, nil
}

func assembleDaemonSetCollectorContainer(
	config *oTelColConfig,
	resourceRequirements ResourceRequirementsWithGoMemLimit,
) (corev1.Container, error) {
	collectorVolumeMounts := assembleCollectorDaemonSetVolumeMounts()
	collectorEnv, err := assembleCollectorEnvVars(config, resourceRequirements.GoMemLimit)
	if err != nil {
		return corev1.Container{}, err
	}

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
		LivenessProbe:  &collectorProbe,
		ReadinessProbe: &collectorProbe,
		Resources:      resourceRequirements.ToResourceRequirements(),
		VolumeMounts:   collectorVolumeMounts,
	}
	if config.Images.CollectorImagePullPolicy != "" {
		collectorContainer.ImagePullPolicy = config.Images.CollectorImagePullPolicy
	}
	return collectorContainer, nil
}

func assembleConfigurationReloaderContainer(config *oTelColConfig, resourceRequirements ResourceRequirementsWithGoMemLimit) corev1.Container {
	collectorPidFileMountRO := collectorPidFileMountRW
	collectorPidFileMountRO.ReadOnly = true
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
				Value: resourceRequirements.GoMemLimit,
			},
			k8sNodeNameEnvVar,
			k8sPodUidEnvVar,
		},
		Resources:    resourceRequirements.ToResourceRequirements(),
		VolumeMounts: []corev1.VolumeMount{collectorConfigVolume, collectorPidFileMountRO},
	}
	if config.Images.ConfigurationReloaderImagePullPolicy != "" {
		configurationReloaderContainer.ImagePullPolicy = config.Images.ConfigurationReloaderImagePullPolicy
	}
	return configurationReloaderContainer
}

func assembleFileLogOffsetSynchInitContainer(config *oTelColConfig, resourceRequirements ResourceRequirementsWithGoMemLimit) corev1.Container {
	initFilelogOffsetSynchContainer := corev1.Container{
		Name:            "filelog-offset-init",
		Args:            []string{"--mode=init"},
		SecurityContext: &corev1.SecurityContext{},
		Image:           config.Images.FilelogOffsetSynchImage,
		Env: []corev1.EnvVar{
			{
				Name:  "GOMEMLIMIT",
				Value: resourceRequirements.GoMemLimit,
			},
			{
				Name:  "K8S_CONFIGMAP_NAMESPACE",
				Value: config.Namespace,
			},
			{
				Name:  "K8S_CONFIGMAP_NAME",
				Value: FilelogReceiverOffsetsConfigMapName(config.NamePrefix),
			},

			{
				Name:  "FILELOG_OFFSET_DIRECTORY_PATH",
				Value: offsetsDirPath,
			},
			k8sNodeNameEnvVar,
			k8sPodUidEnvVar,
		},
		Resources:    resourceRequirements.ToResourceRequirements(),
		VolumeMounts: []corev1.VolumeMount{filelogReceiverOffsetsVolumeMount},
	}
	if config.Images.FilelogOffsetSynchImagePullPolicy != "" {
		initFilelogOffsetSynchContainer.ImagePullPolicy = config.Images.FilelogOffsetSynchImagePullPolicy
	}
	return initFilelogOffsetSynchContainer
}

func assembleServiceAccountForDeployment(config *oTelColConfig) *corev1.ServiceAccount {
	return &corev1.ServiceAccount{
		TypeMeta: metav1.TypeMeta{
			Kind:       "ServiceAccount",
			APIVersion: "v1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      deploymentServiceAccountName(config.NamePrefix),
			Namespace: config.Namespace,
			Labels:    labels(false),
		},
	}
}

func assembleClusterRoleForDeployment(config *oTelColConfig) *rbacv1.ClusterRole {
	return &rbacv1.ClusterRole{
		TypeMeta: metav1.TypeMeta{
			Kind:       "ClusterRole",
			APIVersion: rbacApiVersion,
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:   DeploymentClusterRoleName(config.NamePrefix),
			Labels: labels(false),
		},
		Rules: []rbacv1.PolicyRule{
			{
				APIGroups: []string{""},
				Resources: []string{
					"events",
					"namespaces",
					"namespaces/status",
					"nodes",
					"nodes/spec",
					"pods",
					"pods/status",
					"replicationcontrollers",
					"replicationcontrollers/status",
					"resourcequotas",
					"services",
				},
				Verbs: []string{
					"get",
					"list",
					"watch",
				},
			},
			{
				APIGroups: []string{"apps"},
				Resources: []string{
					"daemonsets",
					"deployments",
					"replicasets",
					"statefulsets",
				},
				Verbs: []string{
					"get",
					"list",
					"watch",
				},
			},
			{
				APIGroups: []string{"extensions"},
				Resources: []string{
					"daemonsets",
					"deployments",
					"replicasets",
				},
				Verbs: []string{
					"get",
					"list",
					"watch",
				},
			},
			{
				APIGroups: []string{"batch"},
				Resources: []string{
					"jobs",
					"cronjobs",
				},
				Verbs: []string{
					"get",
					"list",
					"watch",
				},
			},
			{
				APIGroups: []string{"autoscaling"},
				Resources: []string{
					"horizontalpodautoscalers",
				},
				Verbs: []string{
					"get",
					"list",
					"watch",
				},
			},
		},
	}
}

func assembleClusterRoleBindingForDeployment(config *oTelColConfig) *rbacv1.ClusterRoleBinding {
	return &rbacv1.ClusterRoleBinding{
		TypeMeta: metav1.TypeMeta{
			Kind:       "ClusterRoleBinding",
			APIVersion: rbacApiVersion,
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:   DeploymentClusterRoleBindingName(config.NamePrefix),
			Labels: labels(false),
		},
		RoleRef: rbacv1.RoleRef{
			APIGroup: rbacApiGroup,
			Kind:     "ClusterRole",
			Name:     DeploymentClusterRoleName(config.NamePrefix),
		},
		Subjects: []rbacv1.Subject{{
			Kind:      "ServiceAccount",
			Name:      deploymentServiceAccountName(config.NamePrefix),
			Namespace: config.Namespace,
		}},
	}
}

func assembleCollectorDeployment(
	config *oTelColConfig,
	resourceSpecs *OTelColResourceSpecs,
) (*appsv1.Deployment, error) {
	collectorContainer, err := assembleDeploymentCollectorContainer(
		config,
		resourceSpecs.CollectorDeploymentCollectorContainerResources,
	)
	if err != nil {
		return nil, err
	}

	collectorDeployment := &appsv1.Deployment{
		TypeMeta: metav1.TypeMeta{
			Kind:       "Deployment",
			APIVersion: "apps/v1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      DeploymentName(config.NamePrefix),
			Namespace: config.Namespace,
			Labels:    labels(true),
		},
		Spec: appsv1.DeploymentSpec{
			Replicas: &deploymentReplicas,
			Selector: &metav1.LabelSelector{
				MatchLabels: deploymentMatchLabels,
			},
			Strategy: appsv1.DeploymentStrategy{
				Type: appsv1.RollingUpdateDeploymentStrategyType,
			},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: deploymentMatchLabels,
				},
				Spec: corev1.PodSpec{
					ServiceAccountName: deploymentServiceAccountName(config.NamePrefix),
					SecurityContext:    &corev1.PodSecurityContext{},
					// This setting is required to enable the configuration reloader process to send Unix signals to the
					// collector process.
					ShareProcessNamespace: ptr.To(true),
					Containers: []corev1.Container{
						collectorContainer,
						assembleConfigurationReloaderContainer(
							config,
							resourceSpecs.CollectorDeploymentConfigurationReloaderContainerResources,
						),
					},
					Volumes:     assembleCollectorDeploymentVolumes(config, configMapItems),
					HostNetwork: false,
				},
			},
		},
	}

	if config.SelfMonitoringAndApiAccessConfiguration.SelfMonitoringEnabled {
		err = selfmonitoringapiaccess.EnableSelfMonitoringInCollectorDeployment(
			collectorDeployment,
			config.SelfMonitoringAndApiAccessConfiguration,
			config.Images.GetOperatorVersion(),
			config.DevelopmentMode,
		)
		if err != nil {
			return nil, err
		}
	}

	return collectorDeployment, nil
}

func assembleCollectorDeploymentVolumes(
	config *oTelColConfig,
	configMapItems []corev1.KeyToPath,
) []corev1.Volume {
	pidFileVolumeSizeLimit := resource.MustParse("1M")
	return []corev1.Volume{
		{
			Name: configMapVolumeName,
			VolumeSource: corev1.VolumeSource{
				ConfigMap: &corev1.ConfigMapVolumeSource{
					LocalObjectReference: corev1.LocalObjectReference{
						Name: DeploymentCollectorConfigConfigMapName(config.NamePrefix),
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
}

func assembleDeploymentCollectorContainer(
	config *oTelColConfig,
	resourceRequirements ResourceRequirementsWithGoMemLimit,
) (corev1.Container, error) {
	collectorVolumeMounts := []corev1.VolumeMount{
		collectorConfigVolume,
		collectorPidFileMountRW,
	}
	collectorEnv, err := assembleCollectorEnvVars(config, resourceRequirements.GoMemLimit)
	if err != nil {
		return corev1.Container{}, err
	}

	collectorContainer := corev1.Container{
		Name:            openTelemetryCollector,
		Args:            []string{"--config=file:" + collectorConfigurationFilePath},
		SecurityContext: &corev1.SecurityContext{},
		Image:           config.Images.CollectorImage,
		Env:             collectorEnv,
		LivenessProbe:   &collectorProbe,
		ReadinessProbe:  &collectorProbe,
		Resources:       resourceRequirements.ToResourceRequirements(),
		VolumeMounts:    collectorVolumeMounts,
	}
	if config.Images.CollectorImagePullPolicy != "" {
		collectorContainer.ImagePullPolicy = config.Images.CollectorImagePullPolicy
	}
	return collectorContainer, nil
}

// Maintenance note: Names for Kubernetes objects _must_ be unique, otherwise the logic in
// otelcol_resources.go#deleteResourcesThatAreNoLongerDesired does not work correctly.

func daemonsetServiceAccountName(namePrefix string) string {
	return renderName(namePrefix, openTelemetryCollector, "sa")
}

func deploymentServiceAccountName(namePrefix string) string {
	return renderName(namePrefix, openTelemetryCollectorDeploymentNameSuffix, "sa")
}

func FilelogReceiverOffsetsConfigMapName(namePrefix string) string {
	return renderName(namePrefix, "filelogoffsets", "cm")
}

func DaemonSetCollectorConfigConfigMapName(namePrefix string) string {
	return renderName(namePrefix, openTelemetryCollectorDaemonSetNameSuffix, "cm")
}

func DeploymentCollectorConfigConfigMapName(namePrefix string) string {
	return renderName(namePrefix, openTelemetryCollectorDeploymentNameSuffix, "cm")
}

func DaemonSetClusterRoleName(namePrefix string) string {
	return renderName(namePrefix, openTelemetryCollector, "cr")
}

func DeploymentClusterRoleName(namePrefix string) string {
	return renderName(namePrefix, openTelemetryCollectorDeploymentNameSuffix, "cr")
}

func DaemonSetClusterRoleBindingName(namePrefix string) string {
	return renderName(namePrefix, openTelemetryCollector, "crb")
}

func DeploymentClusterRoleBindingName(namePrefix string) string {
	return renderName(namePrefix, openTelemetryCollectorDeploymentNameSuffix, "crb")
}

func roleName(namePrefix string) string {
	return renderName(namePrefix, openTelemetryCollector, "role")
}

func roleBindingName(namePrefix string) string {
	return renderName(namePrefix, openTelemetryCollector, "rolebinding")
}

func ServiceName(namePrefix string) string {
	return renderName(namePrefix, openTelemetryCollector, "service")
}

func serviceLabels() map[string]string {
	lbls := labels(false)
	lbls[appKubernetesIoComponentLabelKey] = daemonSetServiceComponent
	return lbls
}

func DaemonSetName(namePrefix string) string {
	return renderName(namePrefix, openTelemetryCollectorDaemonSetNameSuffix, "daemonset")
}

func DeploymentName(namePrefix string) string {
	return renderName(namePrefix, openTelemetryCollectorDeploymentNameSuffix, "deployment")
}

func renderName(prefix string, parts ...string) string {
	return strings.Join(append([]string{prefix}, parts...), "-")
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

func addCommonMetadata(object client.Object) clientObject {
	// For clusters managed by ArgoCD, we need to prevent ArgoCD to prune resources that have no owner reference
	// which are all cluster-scoped resources, like cluster roles & cluster role bindings. We could add the annotation
	// to achieve that only to the cluster-scoped resources, but instead we just apply it to all resources we manage.
	// * https://github.com/argoproj/argo-cd/issues/4764#issuecomment-722661940 -- this is where they say that only top
	//   level resources are pruned (that is basically the same as resources without an owner reference).
	// * The docs for preventing this on a resource level are here:
	//   https://argo-cd.readthedocs.io/en/stable/user-guide/sync-options/#no-prune-resources
	if object.GetAnnotations() == nil {
		object.SetAnnotations(map[string]string{})
	}
	object.GetAnnotations()["argocd.argoproj.io/sync-options"] = "Prune=false"
	return clientObject{
		object: object,
	}
}

func compileObsoleteResources(namespace string, namePrefix string) []client.Object {
	openTelemetryCollectorSuffix := "opentelemetry-collector"
	openTelemetryCollectorAgentSuffix := "opentelemetry-collector-agent"
	clusterMetricsCollectorSuffix := "cluster-metrics-collector"

	return []client.Object{
		// K8s resources that were created by the operator in versions 0.9.0 to 0.16.0, becoming obsolete with
		// version 0.17.0:
		&corev1.ServiceAccount{
			ObjectMeta: obsoleteResourceObjectMeta(
				namespace, fmt.Sprintf("%s-%s", namePrefix, openTelemetryCollectorSuffix)),
		},
		&corev1.ConfigMap{
			ObjectMeta: obsoleteResourceObjectMeta(
				namespace, fmt.Sprintf("%s-%s", namePrefix, openTelemetryCollectorAgentSuffix)),
		},
		&corev1.ConfigMap{
			ObjectMeta: obsoleteResourceObjectMeta(
				namespace, fmt.Sprintf("%s-%s", namePrefix, "filelogoffsets")),
		},
		&rbacv1.ClusterRole{
			ObjectMeta: metav1.ObjectMeta{
				Name: fmt.Sprintf("%s-%s", namePrefix, openTelemetryCollectorSuffix),
			},
		},
		&rbacv1.ClusterRoleBinding{
			ObjectMeta: metav1.ObjectMeta{
				Name: fmt.Sprintf("%s-%s", namePrefix, openTelemetryCollectorSuffix),
			},
		},
		&rbacv1.Role{
			ObjectMeta: obsoleteResourceObjectMeta(
				namespace, fmt.Sprintf("%s-%s", namePrefix, openTelemetryCollectorSuffix)),
		},
		&rbacv1.RoleBinding{
			ObjectMeta: obsoleteResourceObjectMeta(
				namespace, fmt.Sprintf("%s-%s", namePrefix, openTelemetryCollectorSuffix)),
		},
		&corev1.Service{
			ObjectMeta: obsoleteResourceObjectMeta(
				namespace, fmt.Sprintf("%s-%s", namePrefix, openTelemetryCollectorSuffix)),
		},
		&appsv1.DaemonSet{
			ObjectMeta: obsoleteResourceObjectMeta(
				namespace, fmt.Sprintf("%s-%s", namePrefix, openTelemetryCollectorAgentSuffix)),
		},

		// Additional deployment related resources that were only created in version 0.16.0, also obsolete starting at
		// version 0.17.0:
		&corev1.ServiceAccount{
			ObjectMeta: obsoleteResourceObjectMeta(
				namespace, fmt.Sprintf("%s-%s", namePrefix, clusterMetricsCollectorSuffix)),
		},
		&rbacv1.ClusterRole{
			ObjectMeta: metav1.ObjectMeta{
				Name: fmt.Sprintf("%s-%s", namePrefix, clusterMetricsCollectorSuffix),
			},
		},
		&rbacv1.ClusterRoleBinding{
			ObjectMeta: metav1.ObjectMeta{
				Name: fmt.Sprintf("%s-%s", namePrefix, clusterMetricsCollectorSuffix),
			},
		},
		&corev1.ConfigMap{
			ObjectMeta: obsoleteResourceObjectMeta(
				namespace, fmt.Sprintf("%s-%s", namePrefix, clusterMetricsCollectorSuffix)),
		},
		&appsv1.Deployment{
			ObjectMeta: obsoleteResourceObjectMeta(
				namespace, fmt.Sprintf("%s-%s", namePrefix, clusterMetricsCollectorSuffix)),
		},
	}
}

func obsoleteResourceObjectMeta(namespace string, name string) metav1.ObjectMeta {
	return metav1.ObjectMeta{
		Name:      name,
		Namespace: namespace,
	}
}
