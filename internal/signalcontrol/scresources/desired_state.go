// SPDX-FileCopyrightText: Copyright 2026 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package scresources

import (
	"fmt"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"sigs.k8s.io/controller-runtime/pkg/client"

	dash0common "github.com/dash0hq/dash0-operator/api/operator/common"
	dash0v1alpha1 "github.com/dash0hq/dash0-operator/api/operator/v1alpha1"
	"github.com/dash0hq/dash0-operator/internal/collectors/otelcolresources"
	"github.com/dash0hq/dash0-operator/internal/util"
	"github.com/dash0hq/dash0-operator/internal/util/logd"
	"github.com/dash0hq/dash0-operator/internal/util/pointers"
)

const (
	edgeProxyComponentName = "edge-proxy"

	edgeProxyGrpcPort     = 8011
	edgeProxyInternalPort = 8012

	edgeProxyAuthTokenEnvVarName = "EDGE_PROXY_AUTH_TOKEN"

	// edgeProxyUserID is the numeric UID baked into the Edge Proxy image via `adduser -u 10001` in its Dockerfile.
	edgeProxyUserID int64 = 10001

	defaultDataset = "default"

	gkeAutopilotAllowlistLabelKey            = "cloud.google.com/matching-allowlist"
	gkeAutopilotAllowlistLabelEdgeProxyValue = "dash0-edge-proxy-v1.0.4"
)

var (
	edgeProxyMatchLabels = map[string]string{
		util.AppKubernetesIoNameLabel:      edgeProxyComponentName,
		util.AppKubernetesIoInstanceLabel:  "dash0-operator",
		util.AppKubernetesIoManagedByLabel: "dash0-operator",
	}
)

type clientObject struct {
	object client.Object
}

func assembleDesiredState(
	operatorNamespace string,
	namePrefix string,
	signalControlResource *dash0v1alpha1.Dash0SignalControl,
	operatorConfig *dash0v1alpha1.Dash0OperatorConfiguration,
	edgeProxyImage string,
	edgeProxyImagePullPolicy corev1.PullPolicy,
	operatorVersion string,
	extraConfig util.ExtraConfig,
	isGkeAutopilot bool,
	forDeletion bool,
	logger logd.Logger,
) []clientObject {
	edgeProxyEnabled := !forDeletion &&
		signalControlResource != nil &&
		pointers.ReadBoolPointerWithDefault(signalControlResource.Spec.EdgeProxy.Enabled, true)

	if edgeProxyEnabled && edgeProxyImage == "" {
		logger.Warn("The Edge Proxy is enabled but no Edge Proxy image is configured. The Edge Proxy will not be deployed.")
		edgeProxyEnabled = false
	}

	var desiredState []clientObject
	if forDeletion || edgeProxyEnabled {
		if edgeProxyEnabled {
			desiredState = append(desiredState,
				addCommonMetadata(assembleEdgeProxyDeployment(operatorNamespace, namePrefix, signalControlResource, operatorConfig, edgeProxyImage, edgeProxyImagePullPolicy, operatorVersion, extraConfig, isGkeAutopilot, logger)),
				addCommonMetadata(assembleEdgeProxyService(operatorNamespace, namePrefix)),
			)
		} else {
			desiredState = append(desiredState,
				addCommonMetadata(assembleEdgeProxyDeploymentForDeletion(operatorNamespace, namePrefix)),
				addCommonMetadata(assembleEdgeProxyServiceForDeletion(operatorNamespace, namePrefix)),
			)
		}
	}
	return desiredState
}

func assembleDesiredStateForDelete(
	operatorNamespace string,
	namePrefix string,
	logger logd.Logger,
) []clientObject {
	return assembleDesiredState(operatorNamespace, namePrefix, nil, nil, "", "", "", util.ExtraConfig{}, false, true, logger)
}

func assembleEdgeProxyDeployment(
	operatorNamespace string,
	namePrefix string,
	signalControlResource *dash0v1alpha1.Dash0SignalControl,
	operatorConfig *dash0v1alpha1.Dash0OperatorConfiguration,
	edgeProxyImage string,
	edgeProxyImagePullPolicy corev1.PullPolicy,
	operatorVersion string,
	extraConfig util.ExtraConfig,
	isGkeAutopilot bool,
	logger logd.Logger,
) *appsv1.Deployment {
	replicas := extraConfig.EdgeProxyReplicas
	if replicas < 1 {
		// Default to a single replica when the extra config does not specify a value (e.g. older config maps).
		replicas = 1
	}

	dmEndpoint, authorization, dataset := deriveUpstreamConfig(operatorConfig)
	if signalControlResource.Spec.Sampling.DecisionMakerEndpoint != "" {
		dmEndpoint = signalControlResource.Spec.Sampling.DecisionMakerEndpoint
	}
	if dmEndpoint == "" {
		logger.Warn("No Decision Maker endpoint could be derived for the Edge Proxy. The Edge Proxy " +
			"will not be able to forward sampling decisions to the Decision Maker.")
	}
	authTokenEnvVar := assembleAuthTokenEnvVar(authorization, logger)

	edgeProxyContainer := corev1.Container{
		Name:  edgeProxyComponentName,
		Image: edgeProxyImage,
		SecurityContext: &corev1.SecurityContext{
			AllowPrivilegeEscalation: new(false),
			ReadOnlyRootFilesystem:   new(true),
			RunAsNonRoot:             new(true),
			Capabilities: &corev1.Capabilities{
				Drop: []corev1.Capability{"ALL"},
			},
			SeccompProfile: &corev1.SeccompProfile{
				Type: corev1.SeccompProfileTypeRuntimeDefault,
			},
		},
		Ports: []corev1.ContainerPort{
			{
				Name:          "grpc",
				ContainerPort: edgeProxyGrpcPort,
				Protocol:      corev1.ProtocolTCP,
			},
			{
				Name:          "internal",
				ContainerPort: edgeProxyInternalPort,
				Protocol:      corev1.ProtocolTCP,
			},
		},
		Env: []corev1.EnvVar{
			{
				Name:  util.EnvVarGoMemLimit,
				Value: extraConfig.EdgeProxyContainerResources.GoMemLimit,
			},
			authTokenEnvVar,
			{
				Name:  "UPSTREAM_ADDRESS",
				Value: dmEndpoint,
			},
			{
				Name:  "UPSTREAM_HEADERS",
				Value: fmt.Sprintf("authorization=Bearer $(%s),Dash0-Dataset=%s", edgeProxyAuthTokenEnvVarName, dataset),
			},
			{
				Name:  "LISTENADDRESS",
				Value: fmt.Sprintf(":%d", edgeProxyGrpcPort),
			},
			{
				Name:  "LISTENADDRESSINTERNAL",
				Value: fmt.Sprintf(":%d", edgeProxyInternalPort),
			},
		},
		Resources: extraConfig.EdgeProxyContainerResources.ToResourceRequirements(),
		LivenessProbe: &corev1.Probe{
			ProbeHandler: corev1.ProbeHandler{
				GRPC: &corev1.GRPCAction{
					Port:    edgeProxyGrpcPort,
					Service: new("liveness"),
				},
			},
			InitialDelaySeconds: 5,
			PeriodSeconds:       10,
		},
		ReadinessProbe: &corev1.Probe{
			ProbeHandler: corev1.ProbeHandler{
				GRPC: &corev1.GRPCAction{
					Port:    edgeProxyGrpcPort,
					Service: new("readiness"),
				},
			},
			InitialDelaySeconds: 5,
			PeriodSeconds:       10,
		},
	}

	if edgeProxyImagePullPolicy != "" {
		edgeProxyContainer.ImagePullPolicy = edgeProxyImagePullPolicy
	}

	spec := signalControlResource.Spec
	if spec.EdgeProxy.LogLevel != "" {
		edgeProxyContainer.Env = append(edgeProxyContainer.Env, corev1.EnvVar{
			Name:  "LOGLEVEL",
			Value: string(spec.EdgeProxy.LogLevel),
		})
	}
	if spec.EdgeProxy.Debug != nil && *spec.EdgeProxy.Debug {
		edgeProxyContainer.Env = append(edgeProxyContainer.Env, corev1.EnvVar{
			Name:  "DEBUG",
			Value: "true",
		})
	}
	if spec.EdgeProxy.Insecure != nil && *spec.EdgeProxy.Insecure {
		edgeProxyContainer.Env = append(edgeProxyContainer.Env, corev1.EnvVar{
			Name:  "UPSTREAM_INSECURE",
			Value: "true",
		})
	}
	if operatorConfig != nil && pointers.ReadBoolPointerWithDefault(operatorConfig.Spec.SelfMonitoring.Enabled, true) {
		edgeProxyContainer.Env = append(edgeProxyContainer.Env, assembleSelfMonitoringEnvVars(operatorVersion)...)
	}
	podSpec := corev1.PodSpec{
		AutomountServiceAccountToken: new(false),
		SecurityContext: &corev1.PodSecurityContext{
			RunAsNonRoot: new(true),
			RunAsUser:    new(edgeProxyUserID),
			SeccompProfile: &corev1.SeccompProfile{
				Type: corev1.SeccompProfileTypeRuntimeDefault,
			},
		},
		Tolerations: extraConfig.EdgeProxyTolerations,
		Containers: []corev1.Container{
			edgeProxyContainer,
		},
	}
	if extraConfig.EdgeProxyNodeAffinity != nil {
		podSpec.Affinity = &corev1.Affinity{
			NodeAffinity: extraConfig.EdgeProxyNodeAffinity,
		}
	}

	templateLabels := edgeProxyLabels()
	if isGkeAutopilot {
		templateLabels[gkeAutopilotAllowlistLabelKey] = gkeAutopilotAllowlistLabelEdgeProxyValue
	}

	deployment := assembleEdgeProxyDeploymentForDeletion(operatorNamespace, namePrefix)
	deployment.Spec = appsv1.DeploymentSpec{
		Replicas: &replicas,
		Selector: &metav1.LabelSelector{
			MatchLabels: edgeProxyMatchLabels,
		},
		Template: corev1.PodTemplateSpec{
			ObjectMeta: metav1.ObjectMeta{
				Labels: templateLabels,
			},
			Spec: podSpec,
		},
	}
	return deployment
}

// assembleSelfMonitoringEnvVars returns env vars that point the Edge Proxy's OTel SDK exporter at the node-local daemonset
// collector's OTLP gRPC host-port. DASH0_NODE_IP is resolved via the downward API (status.hostIP) and must be defined
// before OTEL_EXPORTER_OTLP_ENDPOINT, which references it.
func assembleSelfMonitoringEnvVars(operatorVersion string) []corev1.EnvVar {
	return []corev1.EnvVar{
		{
			Name: util.EnvVarDash0NodeIp,
			ValueFrom: &corev1.EnvVarSource{
				FieldRef: &corev1.ObjectFieldSelector{
					FieldPath: "status.hostIP",
				},
			},
		},
		{
			// http:// scheme signals plaintext to the OTel Go SDK's gRPC exporter; dns:// or a bare endpoint would
			// default to TLS, which the node-local daemonset collector does not terminate.
			Name:  "OTEL_EXPORTER_OTLP_ENDPOINT",
			Value: fmt.Sprintf("http://$(%s):%d", util.EnvVarDash0NodeIp, otelcolresources.OtlpGrpcHostPort),
		},
		{
			Name:  "OTEL_EXPORTER_OTLP_PROTOCOL",
			Value: "grpc",
		},
		{
			Name: "OTEL_RESOURCE_ATTRIBUTES",
			Value: fmt.Sprintf(
				"service.namespace=dash0-operator,service.name=%s,service.version=%s",
				edgeProxyComponentName,
				operatorVersion,
			),
		},
	}
}

func assembleAuthTokenEnvVar(authorization *dash0common.Authorization, logger logd.Logger) corev1.EnvVar {
	if authorization == nil {
		logger.Warn("No Dash0 authorization configured. The Edge Proxy will not be able to " +
			"authenticate with the Decision Maker.")
		return corev1.EnvVar{
			Name:  edgeProxyAuthTokenEnvVarName,
			Value: "",
		}
	}
	envVar, err := util.CreateEnvVarForAuthorization(*authorization, edgeProxyAuthTokenEnvVarName)
	if err != nil {
		logger.Error(err, "Failed to create Edge Proxy auth token env var, the Edge Proxy will not be able to "+
			"authenticate with the Decision Maker.")
		return corev1.EnvVar{
			Name:  edgeProxyAuthTokenEnvVarName,
			Value: "",
		}
	}
	return envVar
}

func assembleEdgeProxyDeploymentForDeletion(operatorNamespace string, namePrefix string) *appsv1.Deployment {
	return &appsv1.Deployment{
		TypeMeta: metav1.TypeMeta{
			APIVersion: util.K8sApiVersionAppsV1,
			Kind:       "Deployment",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      DeploymentName(namePrefix),
			Namespace: operatorNamespace,
		},
	}
}

func assembleEdgeProxyService(operatorNamespace string, namePrefix string) *corev1.Service {
	service := assembleEdgeProxyServiceForDeletion(operatorNamespace, namePrefix)
	service.Spec = corev1.ServiceSpec{
		Selector: edgeProxyMatchLabels,
		Ports: []corev1.ServicePort{
			{
				Name:       "grpc",
				Port:       edgeProxyGrpcPort,
				TargetPort: intstr.FromInt32(edgeProxyGrpcPort),
				Protocol:   corev1.ProtocolTCP,
			},
		},
	}
	return service
}

func assembleEdgeProxyServiceForDeletion(operatorNamespace string, namePrefix string) *corev1.Service {
	return &corev1.Service{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "v1",
			Kind:       "Service",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      ServiceName(namePrefix),
			Namespace: operatorNamespace,
		},
	}
}

func deriveUpstreamConfig(
	operatorConfig *dash0v1alpha1.Dash0OperatorConfiguration,
) (string, *dash0common.Authorization, string) {
	if operatorConfig == nil {
		return "", nil, defaultDataset
	}
	for _, export := range operatorConfig.EffectiveExports() {
		if export.Dash0 != nil {
			endpoint := util.DeriveDecisionMakerEndpoint(export.Dash0.Endpoint)
			dataset := export.Dash0.Dataset
			if dataset == "" {
				dataset = defaultDataset
			}
			return endpoint, &export.Dash0.Authorization, dataset
		}
	}
	return "", nil, defaultDataset
}

func DeploymentName(namePrefix string) string {
	return namePrefix + "-edge-proxy"
}

func ServiceName(namePrefix string) string {
	return namePrefix + "-edge-proxy"
}

func edgeProxyLabels() map[string]string {
	return map[string]string{
		util.AppKubernetesIoNameLabel:      edgeProxyComponentName,
		util.AppKubernetesIoInstanceLabel:  "dash0-operator",
		util.AppKubernetesIoManagedByLabel: "dash0-operator",
		util.AppKubernetesIoComponentLabel: edgeProxyComponentName,
	}
}

func addCommonMetadata(object client.Object) clientObject {
	if object.GetAnnotations() == nil {
		object.SetAnnotations(map[string]string{})
	}
	object.GetAnnotations()["argocd.argoproj.io/sync-options"] = "Prune=false"
	object.GetAnnotations()["argocd.argoproj.io/compare-options"] = "IgnoreExtraneous"
	return clientObject{
		object: object,
	}
}
