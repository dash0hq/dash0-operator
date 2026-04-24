// SPDX-FileCopyrightText: Copyright 2026 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package ieresources

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
)

const (
	barkerComponentName = "barker"

	barkerGrpcPort     = 8011
	barkerInternalPort = 8012

	barkerAuthTokenEnvVarName = "BARKER_AUTH_TOKEN"

	// barkerUserID is the numeric UID baked into the barker image via `adduser -u 10001` in its Dockerfile.
	barkerUserID int64 = 10001

	defaultDataset = "default"
)

var (
	barkerMatchLabels = map[string]string{
		util.AppKubernetesIoNameLabel:      barkerComponentName,
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
	intelligentEdgeResource *dash0v1alpha1.Dash0IntelligentEdge,
	operatorConfig *dash0v1alpha1.Dash0OperatorConfiguration,
	barkerImage string,
	barkerImagePullPolicy corev1.PullPolicy,
	operatorVersion string,
	forDeletion bool,
	logger logd.Logger,
) []clientObject {
	barkerEnabled := !forDeletion &&
		intelligentEdgeResource != nil &&
		util.ReadBoolPointerWithDefault(intelligentEdgeResource.Spec.Barker.Enabled, true)

	if barkerEnabled && barkerImage == "" {
		logger.Info("Warning: Barker is enabled but no barker image is configured. The barker proxy will not be deployed.")
		barkerEnabled = false
	}

	var desiredState []clientObject
	if forDeletion || barkerEnabled {
		if barkerEnabled {
			desiredState = append(desiredState,
				addCommonMetadata(assembleBarkerDeployment(operatorNamespace, namePrefix, intelligentEdgeResource, operatorConfig, barkerImage, barkerImagePullPolicy, operatorVersion, logger)),
				addCommonMetadata(assembleBarkerService(operatorNamespace, namePrefix)),
			)
		} else {
			desiredState = append(desiredState,
				addCommonMetadata(assembleBarkerDeploymentForDeletion(operatorNamespace, namePrefix)),
				addCommonMetadata(assembleBarkerServiceForDeletion(operatorNamespace, namePrefix)),
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
	return assembleDesiredState(operatorNamespace, namePrefix, nil, nil, "", "", "", true, logger)
}

func assembleBarkerDeployment(
	operatorNamespace string,
	namePrefix string,
	intelligentEdgeResource *dash0v1alpha1.Dash0IntelligentEdge,
	operatorConfig *dash0v1alpha1.Dash0OperatorConfiguration,
	barkerImage string,
	barkerImagePullPolicy corev1.PullPolicy,
	operatorVersion string,
	logger logd.Logger,
) *appsv1.Deployment {
	replicas := int32(1)

	dmEndpoint, authorization, dataset := deriveUpstreamConfig(operatorConfig)
	if intelligentEdgeResource.Spec.Sampling.DecisionMakerEndpoint != "" {
		dmEndpoint = intelligentEdgeResource.Spec.Sampling.DecisionMakerEndpoint
	}
	if dmEndpoint == "" {
		logger.Info("Warning: No Decision Maker endpoint could be derived for the barker proxy. The barker " +
			"will not be able to forward sampling decisions to the Decision Maker.")
	}
	authTokenEnvVar := assembleAuthTokenEnvVar(authorization, logger)

	barkerContainer := corev1.Container{
		Name:  barkerComponentName,
		Image: barkerImage,
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
				ContainerPort: barkerGrpcPort,
				Protocol:      corev1.ProtocolTCP,
			},
			{
				Name:          "internal",
				ContainerPort: barkerInternalPort,
				Protocol:      corev1.ProtocolTCP,
			},
		},
		Env: []corev1.EnvVar{
			authTokenEnvVar,
			{
				Name:  "UPSTREAM_ADDRESS",
				Value: dmEndpoint,
			},
			{
				Name:  "UPSTREAM_HEADERS",
				Value: fmt.Sprintf("authorization=Bearer $(%s),Dash0-Dataset=%s", barkerAuthTokenEnvVarName, dataset),
			},
			{
				Name:  "LISTENADDRESS",
				Value: fmt.Sprintf(":%d", barkerGrpcPort),
			},
			{
				Name:  "LISTENADDRESSINTERNAL",
				Value: fmt.Sprintf(":%d", barkerInternalPort),
			},
		},
		LivenessProbe: &corev1.Probe{
			ProbeHandler: corev1.ProbeHandler{
				GRPC: &corev1.GRPCAction{
					Port:    barkerGrpcPort,
					Service: new("liveness"),
				},
			},
			InitialDelaySeconds: 5,
			PeriodSeconds:       10,
		},
		ReadinessProbe: &corev1.Probe{
			ProbeHandler: corev1.ProbeHandler{
				GRPC: &corev1.GRPCAction{
					Port:    barkerGrpcPort,
					Service: new("readiness"),
				},
			},
			InitialDelaySeconds: 5,
			PeriodSeconds:       10,
		},
	}

	if barkerImagePullPolicy != "" {
		barkerContainer.ImagePullPolicy = barkerImagePullPolicy
	}

	spec := intelligentEdgeResource.Spec
	if spec.Barker.LogLevel != "" {
		barkerContainer.Env = append(barkerContainer.Env, corev1.EnvVar{
			Name:  "LOGLEVEL",
			Value: string(spec.Barker.LogLevel),
		})
	}
	if spec.Barker.Debug != nil && *spec.Barker.Debug {
		barkerContainer.Env = append(barkerContainer.Env, corev1.EnvVar{
			Name:  "DEBUG",
			Value: "true",
		})
	}
	if operatorConfig != nil && util.ReadBoolPointerWithDefault(operatorConfig.Spec.SelfMonitoring.Enabled, true) {
		barkerContainer.Env = append(barkerContainer.Env, assembleSelfMonitoringEnvVars(operatorVersion)...)
	}
	deployment := assembleBarkerDeploymentForDeletion(operatorNamespace, namePrefix)
	deployment.Spec = appsv1.DeploymentSpec{
		Replicas: &replicas,
		Selector: &metav1.LabelSelector{
			MatchLabels: barkerMatchLabels,
		},
		Template: corev1.PodTemplateSpec{
			ObjectMeta: metav1.ObjectMeta{
				Labels: barkerLabels(),
			},
			Spec: corev1.PodSpec{
				AutomountServiceAccountToken: new(false),
				SecurityContext: &corev1.PodSecurityContext{
					RunAsNonRoot: new(true),
					RunAsUser:    new(barkerUserID),
					SeccompProfile: &corev1.SeccompProfile{
						Type: corev1.SeccompProfileTypeRuntimeDefault,
					},
				},
				Containers: []corev1.Container{
					barkerContainer,
				},
			},
		},
	}
	return deployment
}

// assembleSelfMonitoringEnvVars returns env vars that point barker's OTel SDK exporter at the node-local daemonset
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
				barkerComponentName,
				operatorVersion,
			),
		},
	}
}

func assembleAuthTokenEnvVar(authorization *dash0common.Authorization, logger logd.Logger) corev1.EnvVar {
	if authorization == nil {
		logger.Info("Warning: No Dash0 authorization configured. The barker proxy will not be able to " +
			"authenticate with the Decision Maker.")
		return corev1.EnvVar{
			Name:  barkerAuthTokenEnvVarName,
			Value: "",
		}
	}
	envVar, err := util.CreateEnvVarForAuthorization(*authorization, barkerAuthTokenEnvVarName)
	if err != nil {
		logger.Error(err, "Failed to create barker auth token env var, the barker proxy will not be able to "+
			"authenticate with the Decision Maker.")
		return corev1.EnvVar{
			Name:  barkerAuthTokenEnvVarName,
			Value: "",
		}
	}
	return envVar
}

func assembleBarkerDeploymentForDeletion(operatorNamespace string, namePrefix string) *appsv1.Deployment {
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

func assembleBarkerService(operatorNamespace string, namePrefix string) *corev1.Service {
	service := assembleBarkerServiceForDeletion(operatorNamespace, namePrefix)
	service.Spec = corev1.ServiceSpec{
		Selector: barkerMatchLabels,
		Ports: []corev1.ServicePort{
			{
				Name:       "grpc",
				Port:       barkerGrpcPort,
				TargetPort: intstr.FromInt32(barkerGrpcPort),
				Protocol:   corev1.ProtocolTCP,
			},
		},
	}
	return service
}

func assembleBarkerServiceForDeletion(operatorNamespace string, namePrefix string) *corev1.Service {
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
	return namePrefix + "-barker"
}

func ServiceName(namePrefix string) string {
	return namePrefix + "-barker"
}

func barkerLabels() map[string]string {
	return map[string]string{
		util.AppKubernetesIoNameLabel:      barkerComponentName,
		util.AppKubernetesIoInstanceLabel:  "dash0-operator",
		util.AppKubernetesIoManagedByLabel: "dash0-operator",
		util.AppKubernetesIoComponentLabel: barkerComponentName,
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
