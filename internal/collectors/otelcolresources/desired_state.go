// SPDX-FileCopyrightText: Copyright 2024 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package otelcolresources

import (
	"fmt"
	"path/filepath"
	"slices"
	"strings"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"

	dash0common "github.com/dash0hq/dash0-operator/api/operator/common"
	dash0v1beta1 "github.com/dash0hq/dash0-operator/api/operator/v1beta1"
	"github.com/dash0hq/dash0-operator/internal/selfmonitoringapiaccess"
	"github.com/dash0hq/dash0-operator/internal/util"
)

type oTelColConfig struct {
	// OperatorNamespace is the namespace of the Dash0 operator
	OperatorNamespace string

	// NamePrefix is used as a prefix for OTel collector Kubernetes resources created by the operator, set to value of
	// the environment variable OTEL_COLLECTOR_NAME_PREFIX, which is set to the Helm release name by the operator Helm
	// chart.
	NamePrefix                                       string
	DefaultExport                                    *dash0common.Export
	PerNamespaceExports                              map[string]dash0common.Export
	SendBatchMaxSize                                 *uint32
	SelfMonitoringConfiguration                      selfmonitoringapiaccess.SelfMonitoringConfiguration
	KubernetesInfrastructureMetricsCollectionEnabled bool
	CollectPodLabelsAndAnnotationsEnabled            bool
	KubeletStatsReceiverConfig                       KubeletStatsReceiverConfig
	UseHostMetricsReceiver                           bool
	DisableHostPorts                                 bool
	PseudoClusterUID                                 string
	ClusterName                                      string
	Images                                           util.Images
	IsIPv6Cluster                                    bool
	OffsetStorageVolume                              *corev1.Volume
	DevelopmentMode                                  bool
	DebugVerbosityDetailed                           bool
}

func (c *oTelColConfig) usesOffsetStorageVolume() bool {
	return c.OffsetStorageVolume != nil
}

// This type just exists to ensure all created objects go through addCommonMetadata.
type clientObject struct {
	object client.Object
}

type NamespacedFilter struct {
	Namespace string
	dash0common.Filter
}

type NamespacedTransform struct {
	Namespace string
	Transform dash0common.NormalizedTransformSpec
}

const (
	EnvVarDash0NodeIp = "DASH0_NODE_IP"

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

	defaultUser  int64 = 65532
	defaultGroup int64 = 0

	openTelemetryCollector                     = "opentelemetry-collector"
	openTelemetryCollectorDaemonSetNameSuffix  = "opentelemetry-collector-agent"
	openTelemetryCollectorDeploymentNameSuffix = "cluster-metrics-collector"

	daemonSetServiceComponent  = "agent-collector"
	deploymentServiceComponent = openTelemetryCollectorDeploymentNameSuffix

	configReloader    = "configuration-reloader"
	fileLogOffsetSync = "filelog-offset-sync"

	// label keys
	dash0OptOutLabelKey = "dash0.com/enable"

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
		util.AppKubernetesIoNameLabel:      appKubernetesIoNameValue,
		util.AppKubernetesIoInstanceLabel:  appKubernetesIoInstanceValue,
		util.AppKubernetesIoComponentLabel: daemonSetServiceComponent,
	}
	deploymentMatchLabels = map[string]string{
		util.AppKubernetesIoNameLabel:      appKubernetesIoNameValue,
		util.AppKubernetesIoInstanceLabel:  appKubernetesIoInstanceValue,
		util.AppKubernetesIoComponentLabel: deploymentServiceComponent,
	}

	nodeIpFieldSpec = corev1.ObjectFieldSelector{
		FieldPath: "status.hostIP",
	}
	k8sNodeIpEnvVar = corev1.EnvVar{
		Name: "K8S_NODE_IP",
		ValueFrom: &corev1.EnvVarSource{
			FieldRef: &nodeIpFieldSpec,
		},
	}
	nodeNameFieldSpec = corev1.ObjectFieldSelector{
		FieldPath: "spec.nodeName",
	}
	k8sNodeNameEnvVar = corev1.EnvVar{
		Name: "K8S_NODE_NAME",
		ValueFrom: &corev1.EnvVarSource{
			FieldRef: &nodeNameFieldSpec,
		},
	}
	namespaceFieldSpec = corev1.ObjectFieldSelector{
		FieldPath: "metadata.namespace",
	}
	namespaceEnvVar = corev1.EnvVar{
		Name: "DASH0_OPERATOR_NAMESPACE",
		ValueFrom: &corev1.EnvVarSource{
			FieldRef: &namespaceFieldSpec,
		},
	}
	podUidFieldSpec = corev1.ObjectFieldSelector{
		FieldPath: "metadata.uid",
	}
	k8sPodUidEnvVar = corev1.EnvVar{
		Name: "K8S_POD_UID",
		ValueFrom: &corev1.EnvVarSource{
			FieldRef: &podUidFieldSpec,
		},
	}
	podNameFieldSpec = corev1.ObjectFieldSelector{
		FieldPath: "metadata.name",
	}
	k8sPodNameEnvVar = corev1.EnvVar{
		Name: "K8S_POD_NAME",
		ValueFrom: &corev1.EnvVarSource{
			FieldRef: &podNameFieldSpec,
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
	defaultFilelogReceiverOffsetsVolumeMount = corev1.VolumeMount{
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
	collectorStartupProbe = corev1.Probe{
		PeriodSeconds:    2,
		FailureThreshold: 30,
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
	allMonitoringResources []dash0v1beta1.Dash0Monitoring,
	extraConfig util.ExtraConfig,
) ([]clientObject, error) {
	monitoredNamespaces := make([]string, 0, len(allMonitoringResources))
	namespacesWithLogCollection := make([]string, 0, len(allMonitoringResources))
	namespacesWithPrometheusScraping := make([]string, 0, len(allMonitoringResources))
	filters := make([]NamespacedFilter, 0, len(allMonitoringResources))
	transforms := make([]NamespacedTransform, 0, len(allMonitoringResources))
	for _, monitoringResource := range allMonitoringResources {
		namespace := monitoringResource.Namespace
		monitoredNamespaces = append(monitoredNamespaces, namespace)
		if util.ReadBoolPointerWithDefault(monitoringResource.Spec.LogCollection.Enabled, true) &&
			// We deliberately do not allow collecting logs from the operator namespace, these are available via
			// self-monitoring. Furthermore, if logs from the operator would be enabled via the normal log collection
			// pipeline, and there was a log parsing error that is being logged into the collector log, this might
			// create a feedback cycle.
			namespace != config.OperatorNamespace {
			namespacesWithLogCollection = append(namespacesWithLogCollection, namespace)
		}
		if util.ReadBoolPointerWithDefault(monitoringResource.Spec.PrometheusScraping.Enabled, true) {
			namespacesWithPrometheusScraping = append(namespacesWithPrometheusScraping, namespace)
		}

		filterForNamespace := monitoringResource.Spec.Filter
		if filterForNamespace != nil && filterForNamespace.HasAnyFilters() {
			filters = append(filters, NamespacedFilter{
				Namespace: namespace,
				Filter:    *filterForNamespace,
			})
		}
		transformForNamespace := monitoringResource.Spec.NormalizedTransformSpec
		if transformForNamespace != nil && transformForNamespace.HasAnyStatements() {
			transforms = append(transforms, NamespacedTransform{
				Namespace: namespace,
				Transform: *transformForNamespace,
			})
		}
	}
	return assembleDesiredState(
		config,
		monitoredNamespaces,
		namespacesWithLogCollection,
		namespacesWithPrometheusScraping,
		filters,
		transforms,
		extraConfig,
		false,
	)
}

func assembleDesiredStateForDelete(
	config *oTelColConfig,
	extraConfig util.ExtraConfig,
) ([]clientObject, error) {
	return assembleDesiredState(
		config,
		nil,
		nil,
		nil,
		nil,
		nil,
		extraConfig,
		true,
	)
}

func assembleDesiredState(
	config *oTelColConfig,
	monitoredNamespaces []string,
	namespacesWithLogCollection []string,
	namespacesWithPrometheusScraping []string,
	filters []NamespacedFilter,
	transforms []NamespacedTransform,
	extraConfig util.ExtraConfig,
	forDeletion bool,
) ([]clientObject, error) {
	// Make sure the resulting objects (in particular the config maps) are do not depend on the (potentially non-stable)
	// sort order of the input slices.
	slices.Sort(monitoredNamespaces)
	slices.Sort(namespacesWithLogCollection)
	slices.Sort(namespacesWithPrometheusScraping)
	slices.SortFunc(filters, func(ns1 NamespacedFilter, ns2 NamespacedFilter) int {
		return strings.Compare(ns1.Namespace, ns2.Namespace)
	})
	slices.SortFunc(transforms, func(ns1 NamespacedTransform, ns2 NamespacedTransform) int {
		return strings.Compare(ns1.Namespace, ns2.Namespace)
	})

	var desiredState []clientObject
	desiredState = append(desiredState, addCommonMetadata(assembleServiceAccountForDaemonSet(config)))
	daemonSetCollectorConfigMap, err := assembleDaemonSetCollectorConfigMap(
		config,
		monitoredNamespaces,
		namespacesWithLogCollection,
		namespacesWithPrometheusScraping,
		filters,
		transforms,
		forDeletion,
	)
	if err != nil {
		return desiredState, err
	}
	desiredState = append(desiredState, addCommonMetadata(daemonSetCollectorConfigMap))
	if !config.usesOffsetStorageVolume() {
		desiredState = append(desiredState, addCommonMetadata(assembleFilelogOffsetsConfigMap(config)))
	}
	desiredState = append(desiredState, addCommonMetadata(assembleClusterRoleForDaemonSet(config)))
	desiredState = append(desiredState, addCommonMetadata(assembleClusterRoleBindingForDaemonSet(config)))
	desiredState = append(desiredState, addCommonMetadata(assembleRole(config)))
	desiredState = append(desiredState, addCommonMetadata(assembleRoleBinding(config)))
	desiredState = append(desiredState, addCommonMetadata(assembleService(config)))
	collectorDaemonSet, err := assembleCollectorDaemonSet(config, extraConfig)
	if err != nil {
		return desiredState, err
	}
	desiredState = append(desiredState, addCommonMetadata(collectorDaemonSet))

	if config.KubernetesInfrastructureMetricsCollectionEnabled {
		desiredState = append(desiredState, addCommonMetadata(assembleServiceAccountForDeployment(config)))
		desiredState = append(desiredState, addCommonMetadata(assembleClusterRoleForDeployment(config)))
		desiredState = append(desiredState, addCommonMetadata(assembleClusterRoleBindingForDeployment(config)))
		deploymentCollectorConfigMap, err := assembleDeploymentCollectorConfigMap(
			config,
			monitoredNamespaces,
			filters,
			transforms,
			forDeletion,
		)
		if err != nil {
			return desiredState, err
		}
		desiredState = append(desiredState, addCommonMetadata(deploymentCollectorConfigMap))
		collectorDeployment, err := assembleCollectorDeployment(config, extraConfig)
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
			APIVersion: util.K8sApiVersionCoreV1,
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      daemonsetServiceAccountName(config.NamePrefix),
			Namespace: config.OperatorNamespace,
			Labels:    labels(false),
		},
	}
}

func assembleFilelogOffsetsConfigMap(config *oTelColConfig) *corev1.ConfigMap {
	return &corev1.ConfigMap{
		TypeMeta: metav1.TypeMeta{
			Kind:       "ConfigMap",
			APIVersion: util.K8sApiVersionCoreV1,
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      FilelogReceiverOffsetsConfigMapName(config.NamePrefix),
			Namespace: config.OperatorNamespace,
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
			Namespace: config.OperatorNamespace,
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
			Namespace: config.OperatorNamespace,
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
			Namespace: config.OperatorNamespace,
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
					"persistentvolumes",
					"persistentvolumeclaims",
					// required for Prometheus receiver
					"endpoints",
					"services",
				},
				Verbs: []string{"get", "watch", "list"},
			},
			{
				APIGroups: []string{""},
				Resources: []string{
					// required for Kubeletstats receiver ({request|limit}_utilization metrics)
					"nodes/proxy",
				},
				Verbs: []string{"get"},
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
			Namespace: config.OperatorNamespace,
		}},
	}
}

func assembleService(config *oTelColConfig) *corev1.Service {
	return &corev1.Service{
		TypeMeta: metav1.TypeMeta{
			Kind:       "Service",
			APIVersion: util.K8sApiVersionCoreV1,
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      ServiceName(config.NamePrefix),
			Namespace: config.OperatorNamespace,
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
				util.AppKubernetesIoNameLabel:      appKubernetesIoNameValue,
				util.AppKubernetesIoInstanceLabel:  appKubernetesIoInstanceValue,
				util.AppKubernetesIoComponentLabel: daemonSetServiceComponent,
			},
			InternalTrafficPolicy: ptr.To(corev1.ServiceInternalTrafficPolicyLocal),
		},
	}
}

func assembleCollectorDaemonSet(config *oTelColConfig, extraConfig util.ExtraConfig) (*appsv1.DaemonSet, error) {
	daemonSetName := DaemonSetName(config.NamePrefix)
	workloadNameEnvVar := corev1.EnvVar{
		Name:  "K8S_DAEMONSET_NAME",
		Value: daemonSetName,
	}

	volumes, filelogOffsetsVolume := assembleCollectorDaemonSetVolumes(config, configMapItems)
	collectorContainer, err := assembleDaemonSetCollectorContainer(
		config,
		workloadNameEnvVar,
		filelogOffsetsVolume,
		extraConfig.CollectorDaemonSetCollectorContainerResources,
	)
	if err != nil {
		return nil, err
	}

	podSpec := corev1.PodSpec{
		Affinity: &corev1.Affinity{
			NodeAffinity: &corev1.NodeAffinity{
				RequiredDuringSchedulingIgnoredDuringExecution: &corev1.NodeSelector{
					NodeSelectorTerms: []corev1.NodeSelectorTerm{
						{
							MatchExpressions: []corev1.NodeSelectorRequirement{
								{
									Key:      dash0OptOutLabelKey,
									Operator: corev1.NodeSelectorOpNotIn,
									Values:   []string{"false"},
								},
							},
						},
					},
				},
			},
		},
		Tolerations:        extraConfig.DaemonSetTolerations,
		ServiceAccountName: daemonsetServiceAccountName(config.NamePrefix),
		SecurityContext: &corev1.PodSecurityContext{
			RunAsNonRoot: ptr.To(true),
			SeccompProfile: &corev1.SeccompProfile{
				Type: corev1.SeccompProfileTypeRuntimeDefault,
			},
			RunAsUser:  ptr.To(defaultUser),
			RunAsGroup: ptr.To(defaultGroup),
		},
		// This setting is required to enable the configuration reloader process to send Unix signals to the
		// collector process.
		ShareProcessNamespace: ptr.To(true),

		Containers: []corev1.Container{
			collectorContainer,
			assembleConfigurationReloaderContainer(
				config,
				workloadNameEnvVar,
				extraConfig.CollectorDaemonSetConfigurationReloaderContainerResources,
			),
		},
		Volumes:     volumes,
		HostNetwork: false,
	}

	if config.usesOffsetStorageVolume() {
		if config.OffsetStorageVolume.HostPath != nil {
			// Host path volumes and sub paths in host path volumes are, by default, created for root:root with
			// permissions set to 755. The OpenTelemetry collector container does not run as root, so it wouldn't be
			// able to write into the mounted volume. We fix the permissions with an init container.
			podSpec.InitContainers = []corev1.Container{
				assembleFileLogVolumeOwnershipInitContainer(filelogOffsetsVolume),
			}
		}
	} else {
		podSpec.InitContainers = []corev1.Container{
			assembleFileLogOffsetSyncInitContainer(
				config,
				workloadNameEnvVar,
				extraConfig.CollectorDaemonSetFileLogOffsetSyncContainerResources,
			),
		}
		podSpec.Containers = append(
			podSpec.Containers,
			assembleFileLogOffsetSyncContainer(
				config,
				workloadNameEnvVar,
				extraConfig.CollectorDaemonSetFileLogOffsetSyncContainerResources,
			),
		)
	}

	collectorDaemonSet := &appsv1.DaemonSet{
		TypeMeta: util.K8sTypeMetaDaemonSet,
		ObjectMeta: metav1.ObjectMeta{
			Name:      daemonSetName,
			Namespace: config.OperatorNamespace,
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
				Spec: podSpec,
			},
		},
	}

	if config.SelfMonitoringConfiguration.SelfMonitoringEnabled {
		err = selfmonitoringapiaccess.EnableSelfMonitoringInCollectorDaemonSet(
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

func assembleFileLogOffsetSyncContainer(
	config *oTelColConfig,
	workloadNameEnvVar corev1.EnvVar,
	resourceRequirements util.ResourceRequirementsWithGoMemLimit,
) corev1.Container {
	filelogOffsetSyncContainer := corev1.Container{
		Name: fileLogOffsetSync,
		Args: []string{"--mode=sync"},
		SecurityContext: &corev1.SecurityContext{
			AllowPrivilegeEscalation: ptr.To(false),
			ReadOnlyRootFilesystem:   ptr.To(false),
			RunAsNonRoot:             ptr.To(true),
			Capabilities: &corev1.Capabilities{
				Drop: []corev1.Capability{"ALL"},
			},
			SeccompProfile: &corev1.SeccompProfile{
				Type: corev1.SeccompProfileTypeRuntimeDefault,
			},
		},
		Image: config.Images.FilelogOffsetSyncImage,
		Env: []corev1.EnvVar{
			{
				Name:  "GOMEMLIMIT",
				Value: resourceRequirements.GoMemLimit,
			},
			{
				Name:  "K8S_CONFIGMAP_NAMESPACE",
				Value: config.OperatorNamespace,
			},
			{
				Name:  "K8S_CONFIGMAP_NAME",
				Value: FilelogReceiverOffsetsConfigMapName(config.NamePrefix),
			},
			{
				Name:  "FILELOG_OFFSET_DIRECTORY_PATH",
				Value: offsetsDirPath,
			},
			{
				Name:  "SERVICE_VERSION",
				Value: config.Images.GetOperatorVersion(),
			},
			{
				Name:  "K8S_CLUSTER_UID",
				Value: config.PseudoClusterUID,
			},
			{
				Name:  "K8S_CLUSTER_NAME",
				Value: config.ClusterName,
			},
			k8sNodeNameEnvVar,
			namespaceEnvVar,
			workloadNameEnvVar,
			k8sPodUidEnvVar,
			k8sPodNameEnvVar,
		},
		Resources:    resourceRequirements.ToResourceRequirements(),
		VolumeMounts: []corev1.VolumeMount{defaultFilelogReceiverOffsetsVolumeMount},
	}
	if config.Images.FilelogOffsetSyncImagePullPolicy != "" {
		filelogOffsetSyncContainer.ImagePullPolicy = config.Images.FilelogOffsetSyncImagePullPolicy
	}
	return filelogOffsetSyncContainer
}

func assembleCollectorDaemonSetVolumes(
	config *oTelColConfig,
	configMapItems []corev1.KeyToPath,
) ([]corev1.Volume, corev1.Volume) {
	var filelogOffsetsVolume corev1.Volume
	if config.usesOffsetStorageVolume() {
		filelogOffsetsVolume = *config.OffsetStorageVolume
	} else {
		offsetsVolumeSizeLimit := resource.MustParse("10M")
		filelogOffsetsVolume = corev1.Volume{
			Name: "filelogreceiver-offsets",
			VolumeSource: corev1.VolumeSource{
				EmptyDir: &corev1.EmptyDirVolumeSource{
					SizeLimit: &offsetsVolumeSizeLimit,
				},
			},
		}
	}

	pidFileVolumeSizeLimit := resource.MustParse("1M")
	volumes := []corev1.Volume{
		filelogOffsetsVolume,
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

	if config.UseHostMetricsReceiver {
		// Mounting the entire host file system is required for the hostmetrics receiver, see
		// https://github.com/open-telemetry/opentelemetry-collector-contrib/blob/main/receiver/hostmetricsreceiver/README.md#collecting-host-metrics-from-inside-a-container-linux-only
		volumes = append(volumes, corev1.Volume{
			Name: "hostfs",
			VolumeSource: corev1.VolumeSource{
				HostPath: &corev1.HostPathVolumeSource{
					Path: "/",
				},
			},
		})
	}
	return volumes, filelogOffsetsVolume
}

func assembleCollectorDaemonSetVolumeMounts(
	config *oTelColConfig,
	filelogOffsetsVolume corev1.Volume,
) []corev1.VolumeMount {
	var filelogOffsetVolumeMount corev1.VolumeMount
	if config.usesOffsetStorageVolume() {
		filelogOffsetVolumeMount = createVolumeMountForUserProvidedFileLogOffsetVolume(filelogOffsetsVolume)
	} else {
		filelogOffsetVolumeMount = defaultFilelogReceiverOffsetsVolumeMount
	}
	volumeMounts := []corev1.VolumeMount{
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
		filelogOffsetVolumeMount,
	}
	if config.UseHostMetricsReceiver {
		// Mounting the entire host file system is required for the hostmetrics receiver, see
		// https://github.com/open-telemetry/opentelemetry-collector-contrib/blob/main/receiver/hostmetricsreceiver/README.md#collecting-host-metrics-from-inside-a-container-linux-only
		volumeMounts = append(volumeMounts, corev1.VolumeMount{
			Name:             "hostfs",
			MountPath:        "/hostfs",
			ReadOnly:         true,
			MountPropagation: ptr.To(corev1.MountPropagationHostToContainer),
		})
	}
	return volumeMounts
}

func createVolumeMountForUserProvidedFileLogOffsetVolume(filelogOffsetsVolume corev1.Volume) corev1.VolumeMount {
	return corev1.VolumeMount{
		Name:        filelogOffsetsVolume.Name,
		MountPath:   offsetsDirPath,
		ReadOnly:    false,
		SubPathExpr: "$(K8S_NODE_NAME)",
	}
}

func assembleCollectorEnvVars(
	config *oTelColConfig,
	workloadNameEnvVar corev1.EnvVar,
	goMemLimit string,
) ([]corev1.EnvVar, error) {
	collectorEnv := []corev1.EnvVar{
		{
			Name: "K8S_POD_IP",
			ValueFrom: &corev1.EnvVarSource{
				FieldRef: &corev1.ObjectFieldSelector{
					FieldPath: "status.podIP",
				},
			},
		},
		k8sNodeIpEnvVar,
		k8sNodeNameEnvVar,
		namespaceEnvVar,
		workloadNameEnvVar,
		k8sPodUidEnvVar,
		k8sPodNameEnvVar,
		{
			Name:  "DASH0_COLLECTOR_PID_FILE",
			Value: collectorPidFilePath,
		},
		{
			Name:  "GOMEMLIMIT",
			Value: goMemLimit,
		},
	}

	if config.DefaultExport.Dash0 != nil {
		authTokenEnvVar, err := util.CreateEnvVarForAuthorization(
			(*(config.DefaultExport.Dash0)).Authorization,
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
	workloadNameEnvVar corev1.EnvVar,
	filelogOffsetsVolume corev1.Volume,
	resourceRequirements util.ResourceRequirementsWithGoMemLimit,
) (corev1.Container, error) {
	collectorVolumeMounts := assembleCollectorDaemonSetVolumeMounts(config, filelogOffsetsVolume)
	collectorEnv, err := assembleCollectorEnvVars(config, workloadNameEnvVar, resourceRequirements.GoMemLimit)
	if err != nil {
		return corev1.Container{}, err
	}

	otlpPort := corev1.ContainerPort{
		Name:          "otlp",
		Protocol:      corev1.ProtocolTCP,
		ContainerPort: otlpGrpcPort,
	}
	httpPort := corev1.ContainerPort{
		Name:          "otlp-http",
		Protocol:      corev1.ProtocolTCP,
		ContainerPort: otlpHttpPort,
	}
	if !config.DisableHostPorts {
		otlpPort.HostPort = int32(OtlpGrpcHostPort)
		httpPort.HostPort = int32(OtlpHttpHostPort)
	}

	collectorContainer := corev1.Container{
		Name: openTelemetryCollector,
		Args: []string{"--config=file:" + collectorConfigurationFilePath},
		SecurityContext: &corev1.SecurityContext{
			AllowPrivilegeEscalation: ptr.To(false),
			ReadOnlyRootFilesystem:   ptr.To(false),
			RunAsNonRoot:             ptr.To(true),
			Capabilities: &corev1.Capabilities{
				Drop: []corev1.Capability{"ALL"},
			},
			SeccompProfile: &corev1.SeccompProfile{
				Type: corev1.SeccompProfileTypeRuntimeDefault,
			},
		},
		Image:          config.Images.CollectorImage,
		Ports:          []corev1.ContainerPort{otlpPort, httpPort},
		Env:            collectorEnv,
		LivenessProbe:  &collectorProbe,
		StartupProbe:   &collectorStartupProbe,
		ReadinessProbe: &collectorProbe,
		Resources:      resourceRequirements.ToResourceRequirements(),
		VolumeMounts:   collectorVolumeMounts,
	}
	if config.Images.CollectorImagePullPolicy != "" {
		collectorContainer.ImagePullPolicy = config.Images.CollectorImagePullPolicy
	}
	return collectorContainer, nil
}

func assembleConfigurationReloaderContainer(
	config *oTelColConfig,
	workloadNameEnvVar corev1.EnvVar,
	resourceRequirements util.ResourceRequirementsWithGoMemLimit,
) corev1.Container {
	collectorPidFileMountRO := collectorPidFileMountRW
	collectorPidFileMountRO.ReadOnly = true
	configurationReloaderContainer := corev1.Container{
		Name: configReloader,
		Args: []string{
			"--pidfile=" + collectorPidFilePath,
			collectorConfigurationFilePath,
		},
		SecurityContext: &corev1.SecurityContext{
			AllowPrivilegeEscalation: ptr.To(false),
			ReadOnlyRootFilesystem:   ptr.To(false),
			RunAsNonRoot:             ptr.To(true),
			Capabilities: &corev1.Capabilities{
				Drop: []corev1.Capability{"ALL"},
			},
			SeccompProfile: &corev1.SeccompProfile{
				Type: corev1.SeccompProfileTypeRuntimeDefault,
			},
		},
		Image: config.Images.ConfigurationReloaderImage,
		Env: []corev1.EnvVar{
			{
				Name:  "GOMEMLIMIT",
				Value: resourceRequirements.GoMemLimit,
			},
			{
				Name:  "K8S_CLUSTER_UID",
				Value: config.PseudoClusterUID,
			},
			{
				Name:  "K8S_CLUSTER_NAME",
				Value: config.ClusterName,
			},
			k8sNodeNameEnvVar,
			namespaceEnvVar,
			workloadNameEnvVar,
			k8sPodUidEnvVar,
			k8sPodNameEnvVar,
		},
		Resources:    resourceRequirements.ToResourceRequirements(),
		VolumeMounts: []corev1.VolumeMount{collectorConfigVolume, collectorPidFileMountRO},
	}
	if config.Images.ConfigurationReloaderImagePullPolicy != "" {
		configurationReloaderContainer.ImagePullPolicy = config.Images.ConfigurationReloaderImagePullPolicy
	}
	return configurationReloaderContainer
}

func assembleFileLogOffsetSyncInitContainer(
	config *oTelColConfig,
	workloadNameEnvVar corev1.EnvVar,
	resourceRequirements util.ResourceRequirementsWithGoMemLimit,
) corev1.Container {
	initFilelogOffsetSyncContainer := corev1.Container{
		Name: "filelog-offset-init",
		Args: []string{"--mode=init"},
		SecurityContext: &corev1.SecurityContext{
			AllowPrivilegeEscalation: ptr.To(false),
			ReadOnlyRootFilesystem:   ptr.To(false),
			RunAsNonRoot:             ptr.To(true),
			Capabilities: &corev1.Capabilities{
				Drop: []corev1.Capability{"ALL"},
			},
			SeccompProfile: &corev1.SeccompProfile{
				Type: corev1.SeccompProfileTypeRuntimeDefault,
			},
		},
		Image: config.Images.FilelogOffsetSyncImage,
		Env: []corev1.EnvVar{
			{
				Name:  "GOMEMLIMIT",
				Value: resourceRequirements.GoMemLimit,
			},
			{
				Name:  "K8S_CONFIGMAP_NAMESPACE",
				Value: config.OperatorNamespace,
			},
			{
				Name:  "K8S_CONFIGMAP_NAME",
				Value: FilelogReceiverOffsetsConfigMapName(config.NamePrefix),
			},
			{
				Name:  "FILELOG_OFFSET_DIRECTORY_PATH",
				Value: offsetsDirPath,
			},
			{
				Name:  "K8S_CLUSTER_UID",
				Value: config.PseudoClusterUID,
			},
			{
				Name:  "K8S_CLUSTER_NAME",
				Value: config.ClusterName,
			},
			k8sNodeNameEnvVar,
			namespaceEnvVar,
			workloadNameEnvVar,
			k8sPodUidEnvVar,
			k8sPodNameEnvVar,
		},
		Resources:    resourceRequirements.ToResourceRequirements(),
		VolumeMounts: []corev1.VolumeMount{defaultFilelogReceiverOffsetsVolumeMount},
	}
	if config.Images.FilelogOffsetSyncImagePullPolicy != "" {
		initFilelogOffsetSyncContainer.ImagePullPolicy = config.Images.FilelogOffsetSyncImagePullPolicy
	}
	return initFilelogOffsetSyncContainer
}

func assembleFileLogVolumeOwnershipInitContainer(filelogOffsetsVolume corev1.Volume) corev1.Container {
	initFilelogOffsetVolumeOwnershipContainer := corev1.Container{
		Name:  "filelog-offset-volume-ownership",
		Image: "busybox:1.37.0-glibc",
		Command: []string{
			"/bin/chown",
			"-R",
			fmt.Sprintf("%d:%d", defaultUser, defaultGroup),
			offsetsDirPath,
		},
		Env: []corev1.EnvVar{
			k8sNodeNameEnvVar,
		},
		SecurityContext: &corev1.SecurityContext{
			// this container needs to run as root
			RunAsUser:                ptr.To(int64(0)),
			RunAsNonRoot:             ptr.To(false),
			AllowPrivilegeEscalation: ptr.To(false),
			ReadOnlyRootFilesystem:   ptr.To(false),
			Privileged:               ptr.To(false),
			Capabilities: &corev1.Capabilities{
				Drop: []corev1.Capability{"ALL"},
				Add:  []corev1.Capability{"CHOWN"},
			},
			SeccompProfile: &corev1.SeccompProfile{
				Type: corev1.SeccompProfileTypeRuntimeDefault,
			},
		},
		Resources: corev1.ResourceRequirements{
			Limits: corev1.ResourceList{
				corev1.ResourceMemory: resource.MustParse("32Mi"),
			},
			Requests: corev1.ResourceList{
				corev1.ResourceCPU:    resource.MustParse("100m"),
				corev1.ResourceMemory: resource.MustParse("10Mi"),
			},
		},
		VolumeMounts: []corev1.VolumeMount{
			createVolumeMountForUserProvidedFileLogOffsetVolume(filelogOffsetsVolume),
		},
	}
	return initFilelogOffsetVolumeOwnershipContainer
}

func assembleServiceAccountForDeployment(config *oTelColConfig) *corev1.ServiceAccount {
	return &corev1.ServiceAccount{
		TypeMeta: metav1.TypeMeta{
			Kind:       "ServiceAccount",
			APIVersion: util.K8sApiVersionCoreV1,
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      deploymentServiceAccountName(config.NamePrefix),
			Namespace: config.OperatorNamespace,
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
			Namespace: config.OperatorNamespace,
		}},
	}
}

func assembleCollectorDeployment(
	config *oTelColConfig,
	extraConfig util.ExtraConfig,
) (*appsv1.Deployment, error) {
	deploymentName := DeploymentName(config.NamePrefix)
	workloadNameEnvVar := corev1.EnvVar{
		Name:  "K8S_DEPLOYMENT_NAME",
		Value: deploymentName,
	}
	collectorContainer, err := assembleDeploymentCollectorContainer(
		config,
		workloadNameEnvVar,
		extraConfig.CollectorDeploymentCollectorContainerResources,
	)
	if err != nil {
		return nil, err
	}
	collectorDeployment := &appsv1.Deployment{
		TypeMeta: util.K8sTypeMetaDeployment,
		ObjectMeta: metav1.ObjectMeta{
			Name:      deploymentName,
			Namespace: config.OperatorNamespace,
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
					Affinity: &corev1.Affinity{
						NodeAffinity: &corev1.NodeAffinity{
							RequiredDuringSchedulingIgnoredDuringExecution: &corev1.NodeSelector{
								NodeSelectorTerms: []corev1.NodeSelectorTerm{
									{
										MatchExpressions: []corev1.NodeSelectorRequirement{
											{
												Key:      dash0OptOutLabelKey,
												Operator: corev1.NodeSelectorOpNotIn,
												Values:   []string{"false"},
											},
										},
									},
								},
							},
						},
					},
					ServiceAccountName: deploymentServiceAccountName(config.NamePrefix),
					SecurityContext: &corev1.PodSecurityContext{
						RunAsNonRoot: ptr.To(true),
						SeccompProfile: &corev1.SeccompProfile{
							Type: corev1.SeccompProfileTypeRuntimeDefault,
						},
					},
					// This setting is required to enable the configuration reloader process to send Unix signals to the
					// collector process.
					ShareProcessNamespace: ptr.To(true),
					Containers: []corev1.Container{
						collectorContainer,
						assembleConfigurationReloaderContainer(
							config,
							workloadNameEnvVar,
							extraConfig.CollectorDeploymentConfigurationReloaderContainerResources,
						),
					},
					Volumes:     assembleCollectorDeploymentVolumes(config, configMapItems),
					HostNetwork: false,
				},
			},
		},
	}

	if config.SelfMonitoringConfiguration.SelfMonitoringEnabled {
		err = selfmonitoringapiaccess.EnableSelfMonitoringInCollectorDeployment(
			collectorDeployment,
			config.SelfMonitoringConfiguration,
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
	workloadNameEnvVar corev1.EnvVar,
	resourceRequirements util.ResourceRequirementsWithGoMemLimit,
) (corev1.Container, error) {
	collectorVolumeMounts := []corev1.VolumeMount{
		collectorConfigVolume,
		collectorPidFileMountRW,
	}
	collectorEnv, err := assembleCollectorEnvVars(config, workloadNameEnvVar, resourceRequirements.GoMemLimit)
	if err != nil {
		return corev1.Container{}, err
	}

	collectorContainer := corev1.Container{
		Name: openTelemetryCollector,
		Args: []string{"--config=file:" + collectorConfigurationFilePath},
		SecurityContext: &corev1.SecurityContext{
			AllowPrivilegeEscalation: ptr.To(false),
			ReadOnlyRootFilesystem:   ptr.To(false),
			RunAsNonRoot:             ptr.To(true),
			Capabilities: &corev1.Capabilities{
				Drop: []corev1.Capability{"ALL"},
			},
			SeccompProfile: &corev1.SeccompProfile{
				Type: corev1.SeccompProfileTypeRuntimeDefault,
			},
		},
		Image:          config.Images.CollectorImage,
		Env:            collectorEnv,
		LivenessProbe:  &collectorProbe,
		StartupProbe:   &collectorStartupProbe,
		ReadinessProbe: &collectorProbe,
		Resources:      resourceRequirements.ToResourceRequirements(),
		VolumeMounts:   collectorVolumeMounts,
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

func OperatorExtracConfigConfigMapName(namePrefix string) string {
	return renderName(namePrefix, "extra-config")
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
	lbls[util.AppKubernetesIoComponentLabel] = daemonSetServiceComponent
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
		util.AppKubernetesIoNameLabel:      appKubernetesIoNameValue,
		util.AppKubernetesIoInstanceLabel:  appKubernetesIoInstanceValue,
		util.AppKubernetesIoManagedByLabel: appKubernetesIoManagedByValue,
	}
	if addOptOutLabel {
		lbls[dash0OptOutLabelKey] = "false"
	}
	return lbls
}

func addCommonMetadata(object client.Object) clientObject {
	// For clusters managed by ArgoCD, we need to prevent ArgoCD to sync or prune resources that have no owner
	// reference, which are all cluster-scoped resources, like cluster roles & cluster role bindings. We could add the
	// annotation to achieve that only to the cluster-scoped resources, but instead we just apply it to all resources we
	// manage.
	// * https://github.com/argoproj/argo-cd/issues/4764#issuecomment-722661940 -- this is where they say that only top
	//   level resources are pruned (that is basically the same as resources without an owner reference).
	// * The docs for preventing this on a resource level are here:
	//   https://argo-cd.readthedocs.io/en/stable/user-guide/sync-options/#no-prune-resources
	//   https://argo-cd.readthedocs.io/en/stable/user-guide/compare-options/#ignoring-resources-that-are-extraneous
	if object.GetAnnotations() == nil {
		object.SetAnnotations(map[string]string{})
	}
	object.GetAnnotations()["argocd.argoproj.io/sync-options"] = "Prune=false"
	object.GetAnnotations()["argocd.argoproj.io/compare-options"] = "IgnoreExtraneous"
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
