// SPDX-FileCopyrightText: Copyright 2024 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package util

import (
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/dash0hq/dash0-operator/internal/util"
)

func CreateControllerDeploymentWithoutSelfMonitoringWithoutAuth() *appsv1.Deployment {
	return createControllerDeployment(createDefaultEnvVars())
}

func CreateControllerDeploymentWithoutSelfMonitoringWithToken() *appsv1.Deployment {
	tokenEnvVar := corev1.EnvVar{
		Name:  util.SelfMonitoringAndApiAuthTokenEnvVarName,
		Value: AuthorizationTokenTest,
	}
	env := append([]corev1.EnvVar{tokenEnvVar}, createDefaultEnvVars()...)
	return createControllerDeployment(env)
}

func CreateControllerDeploymentWithoutSelfMonitoringWithSecretRef() *appsv1.Deployment {
	env := append([]corev1.EnvVar{createSecretRefEnvVar()}, createDefaultEnvVars()...)
	return createControllerDeployment(env)
}

func CreateControllerDeploymentWithSelfMonitoringWithToken() *appsv1.Deployment {
	tokenEnvVar := corev1.EnvVar{
		Name:  util.SelfMonitoringAndApiAuthTokenEnvVarName,
		Value: AuthorizationTokenTest,
	}
	env := append([]corev1.EnvVar{tokenEnvVar}, createDefaultEnvVars()...)
	return createControllerDeployment(appendSelfMonitoringEnvVars(env))
}

func CreateControllerDeploymentWithSelfMonitoringWithSecretRef() *appsv1.Deployment {
	env := append([]corev1.EnvVar{createSecretRefEnvVar()}, createDefaultEnvVars()...)
	return createControllerDeployment(appendSelfMonitoringEnvVars(env))
}

func createSecretRefEnvVar() corev1.EnvVar {
	return corev1.EnvVar{
		Name: util.SelfMonitoringAndApiAuthTokenEnvVarName,
		ValueFrom: &corev1.EnvVarSource{
			SecretKeyRef: &corev1.SecretKeySelector{
				LocalObjectReference: corev1.LocalObjectReference{
					Name: SecretRefTest.Name,
				},
				Key: SecretRefTest.Key,
			},
		},
	}
}

func appendSelfMonitoringEnvVars(env []corev1.EnvVar) []corev1.EnvVar {
	return append(env,
		corev1.EnvVar{
			Name:  "OTEL_EXPORTER_OTLP_ENDPOINT",
			Value: EndpointDash0WithProtocolTest,
		},
		corev1.EnvVar{
			Name:  "OTEL_EXPORTER_OTLP_HEADERS",
			Value: "Authorization=Bearer $(SELF_MONITORING_AND_API_AUTH_TOKEN)",
		},
		corev1.EnvVar{
			Name:  "OTEL_EXPORTER_OTLP_PROTOCOL",
			Value: "grpc",
		},
		corev1.EnvVar{
			Name:  "OTEL_RESOURCE_ATTRIBUTES",
			Value: "service.namespace=dash0.operator,service.name=manager,service.version=1.2.3",
		},
	)
}

func createDefaultEnvVars() []corev1.EnvVar {
	return []corev1.EnvVar{
		{
			Name: "DASH0_OPERATOR_NAMESPACE",
			ValueFrom: &corev1.EnvVarSource{
				FieldRef: &corev1.ObjectFieldSelector{
					APIVersion: "v1",
					FieldPath:  "metadata.namespace",
				},
			},
		},
		{
			Name:  "DASH0_DEPLOYMENT_NAME",
			Value: OperatorDeploymentName,
		},
		{
			Name:  "DASH0_WEBHOOK_SERVICE_NAME",
			Value: OperatorWebhookServiceName,
		},
		{
			Name:  "OTEL_COLLECTOR_NAME_PREFIX",
			Value: "dash0-system",
		},
		{
			Name:  "DASH0_INIT_CONTAINER_IMAGE",
			Value: "ghcr.io/dash0hq/instrumentation",
		},
		{
			Name:  "DASH0_INIT_CONTAINER_IMAGE_PULL_POLICY",
			Value: "",
		},
		{
			Name:  "DASH0_COLLECTOR_IMAGE",
			Value: "ghcr.io/dash0hq/collector",
		},
		{
			Name:  "DASH0_COLLECTOR_IMAGE_PULL_POLICY",
			Value: "",
		},
		{
			Name:  "DASH0_CONFIGURATION_RELOADER_IMAGE",
			Value: "ghcr.io/dash0hq/configuration-reloader@latest",
		},
		{
			Name:  "DASH0_CONFIGURATION_RELOADER_IMAGE_PULL_POLICY",
			Value: "",
		},
		{
			Name:  "DASH0_FILELOG_OFFSET_SYNCH_IMAGE",
			Value: "ghcr.io/dash0hq/filelog-offset-synch",
		},
		{
			Name:  "DASH0_FILELOG_OFFSET_SYNCH_IMAGE_PULL_POLICY",
			Value: "",
		},
		{
			Name:  "DASH0_DEVELOPMENT_MODE",
			Value: "false",
		},
	}
}

func createControllerDeployment(env []corev1.EnvVar) *appsv1.Deployment {
	replicaCount := int32(2)
	falsy := false
	truthy := true
	terminationGracePeriodSeconds := int64(10)
	secretMode := corev1.SecretVolumeSourceDefaultMode

	return &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      OperatorDeploymentName,
			Namespace: OperatorNamespace,
			Labels: map[string]string{
				"app.kubernetes.io/name":      "dash0-operator",
				"app.kubernetes.io/component": "controller",
				"app.kubernetes.io/instance":  "deployment",
				"dash0.com/enable":            "false",
			},
		},
		Spec: appsv1.DeploymentSpec{
			Replicas: &replicaCount,
			Selector: &metav1.LabelSelector{
				MatchLabels: map[string]string{
					"app.kubernetes.io/name":      "dash0-operator",
					"app.kubernetes.io/component": "controller",
				},
			},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: map[string]string{
						"kubectl.kubernetes.io/default-container": "manager",
					},
					Labels: map[string]string{
						"app.kubernetes.io/name":      "dash0-operator",
						"app.kubernetes.io/component": "controller",
						"dash0.cert-digest":           "1234567890",
					},
				},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name:    "manager",
							Image:   "ghcr.io/dash0hq/operator-controller@latest",
							Command: []string{"/manager"},
							Args: []string{
								"--health-probe-bind-address=:8081",
								"--metrics-bind-address=127.0.0.1:8080",
								"--leader-elect",
							},
							Env: env,
							Ports: []corev1.ContainerPort{
								{
									Name:          "webhook-server",
									ContainerPort: 9443,
									Protocol:      "TCP",
								},
							},
							VolumeMounts: []corev1.VolumeMount{
								{
									Name:      "certificates",
									MountPath: "/tmp/k8s-webhook-server/serving-certs",
									ReadOnly:  true,
								},
							},
						},
						{
							Name:  "kube-rbac-proxy",
							Image: "quay.io/brancz/kube-rbac-proxy:v0.18.0",
							Args: []string{
								"--secure-listen-address=0.0.0.0:8443",
								"--upstream=http://127.0.0.1:8080/",
								"--logtostderr=true",
								"--v=0",
							},
							Ports: []corev1.ContainerPort{
								{
									Name:          "https",
									ContainerPort: 8443,
									Protocol:      "TCP",
								},
							},
							SecurityContext: &corev1.SecurityContext{
								AllowPrivilegeEscalation: &falsy,
								Capabilities: &corev1.Capabilities{
									Drop: []corev1.Capability{"ALL"},
								},
							},
						},
					},
					SecurityContext: &corev1.PodSecurityContext{
						RunAsNonRoot: &truthy,
						SeccompProfile: &corev1.SeccompProfile{
							Type: corev1.SeccompProfileTypeRuntimeDefault,
						},
					},
					ServiceAccountName:            "dash0-operator-service-account",
					AutomountServiceAccountToken:  &truthy,
					TerminationGracePeriodSeconds: &terminationGracePeriodSeconds,
					Volumes: []corev1.Volume{
						{
							Name: "certificates",
							VolumeSource: corev1.VolumeSource{
								Secret: &corev1.SecretVolumeSource{
									DefaultMode: &secretMode,
									SecretName:  "dash0-operator-certificates",
								},
							},
						},
					},
				},
			},
		},
	}
}
