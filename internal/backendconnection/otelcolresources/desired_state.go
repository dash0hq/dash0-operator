// SPDX-FileCopyrightText: Copyright 2024 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package otelcolresources

import (
	"bytes"
	"fmt"
	"text/template"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	serviceComponent = "agent-collector"

	// label keys
	appKubernetesIoNameKey      = "app.kubernetes.io/name"
	appKubernetesIoInstanceKey  = "app.kubernetes.io/instance"
	appKubernetesIoVersionKey   = "app.kubernetes.io/version"
	appKubernetesIoManagedByKey = "app.kubernetes.io/managed-by"
	dash0OptOutLabelKey         = "dash0.com/enable"
	componentLabelKey           = "component"

	// label values
	appKubernetesIoNameValue      = "opentelemetry-collector"
	appKubernetesIoInstanceValue  = "dash0-operator"
	appKubernetesIoManagedByValue = "dash0-operator"

	dash0AuthorizationSecretName = "dash0-authorization-secret"
)

var (
	daemonSetMatchLabels = map[string]string{
		appKubernetesIoNameKey:     appKubernetesIoNameValue,
		appKubernetesIoInstanceKey: appKubernetesIoInstanceValue,
		componentLabelKey:          serviceComponent,
	}

	// TODO make configurable
	configTemplate = template.Must(template.New("collector-config").Parse(`
    exporters:
      debug: {}
      otlp:
        auth:
          authenticator: bearertokenauth/dash0
        endpoint: ingress.eu-west-1.aws.dash0-dev.com:4317

    extensions:
      bearertokenauth/dash0:
        filename: /etc/dash0/secret-volume/dash0-authorization-token
        scheme: Bearer
      health_check:
        endpoint: ${env:MY_POD_IP}:13133

    processors:
      batch: {}
      k8sattributes:
        extract:
          metadata:
          - k8s.namespace.name
          - k8s.deployment.name
          - k8s.statefulset.name
          - k8s.daemonset.name
          - k8s.cronjob.name
          - k8s.job.name
          - k8s.node.name
          - k8s.pod.name
          - k8s.pod.uid
          - k8s.pod.start_time
        filter:
          node_from_env_var: K8S_NODE_NAME
        passthrough: false
        pod_association:
        - sources:
          - from: resource_attribute
            name: k8s.pod.ip
        - sources:
          - from: resource_attribute
            name: k8s.pod.uid
        - sources:
          - from: connection
      memory_limiter:
        check_interval: 5s
        limit_percentage: 80
        spike_limit_percentage: 25

    receivers:
      otlp:
        protocols:
          grpc:
            endpoint: ${env:MY_POD_IP}:4317
          http:
            endpoint: ${env:MY_POD_IP}:4318

    service:
      extensions:
      - health_check
      - bearertokenauth/dash0
      pipelines:
        logs:
          exporters:
          - otlp
          processors:
          - k8sattributes
          - memory_limiter
          - batch
          receivers:
          - otlp
        metrics:
          exporters:
          - otlp
          processors:
          - k8sattributes
          - memory_limiter
          - batch
          receivers:
          - otlp
        traces:
          exporters:
          - otlp
          processors:
          - k8sattributes
          - memory_limiter
          - batch
          receivers:
          - otlp

      telemetry:
        metrics:
          address: ${env:MY_POD_IP}:8888
`))
)

func assembleDesiredState(namespace string, namePrefix string, oTelColVersion string) ([]client.Object, error) {
	var desiredState []client.Object
	desiredState = append(desiredState, serviceAccount(namespace, namePrefix, oTelColVersion))
	cnfgMap, err := configMap(namespace, namePrefix, oTelColVersion)
	if err != nil {
		return desiredState, err
	}
	desiredState = append(desiredState, cnfgMap)
	desiredState = append(desiredState, clusterRole(namespace, namePrefix, oTelColVersion))
	desiredState = append(desiredState, clusterRoleBinding(namespace, namePrefix, oTelColVersion))
	desiredState = append(desiredState, service(namespace, namePrefix, oTelColVersion))
	desiredState = append(desiredState, daemonSet(namespace, namePrefix, oTelColVersion))
	return desiredState, nil
}

func serviceAccount(namespace string, namePrefix string, oTelColVersion string) *corev1.ServiceAccount {
	return &corev1.ServiceAccount{
		TypeMeta: metav1.TypeMeta{
			Kind:       "ServiceAccount",
			APIVersion: "v1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      serviceAccountName(namePrefix),
			Namespace: namespace,
			Labels:    labels(oTelColVersion, false),
		},
	}
}

func configMap(namespace string, namePrefix string, oTelColVersion string) (*corev1.ConfigMap, error) {
	var renderedConfig bytes.Buffer
	err := configTemplate.Execute(&renderedConfig, nil)
	if err != nil {
		return nil, err
	}
	return &corev1.ConfigMap{
		TypeMeta: metav1.TypeMeta{
			Kind:       "ConfigMap",
			APIVersion: "v1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      configMapName(namePrefix),
			Namespace: namespace,
			Labels:    labels(oTelColVersion, false),
		},
		Data: map[string]string{
			"collector.yaml": renderedConfig.String(),
		},
	}, nil
}

func clusterRole(namespace string, namePrefix string, oTelColVersion string) *rbacv1.ClusterRole {
	return &rbacv1.ClusterRole{
		TypeMeta: metav1.TypeMeta{
			Kind:       "ClusterRole",
			APIVersion: "rbac.authorization.k8s.io/v1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      clusterRoleName(namePrefix),
			Namespace: namespace,
			Labels:    labels(oTelColVersion, false),
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

func clusterRoleBinding(namespace string, namePrefix string, oTelColVersion string) *rbacv1.ClusterRoleBinding {
	return &rbacv1.ClusterRoleBinding{
		TypeMeta: metav1.TypeMeta{
			Kind:       "ClusterRoleBinding",
			APIVersion: "rbac.authorization.k8s.io/v1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      name(namePrefix, "opentelemetry-collector"),
			Namespace: namespace,
			Labels:    labels(oTelColVersion, false),
		},
		RoleRef: rbacv1.RoleRef{
			APIGroup: "rbac.authorization.k8s.io",
			Kind:     "ClusterRole",
			Name:     clusterRoleName(namePrefix),
		},
		Subjects: []rbacv1.Subject{{
			Kind:      "ServiceAccount",
			Name:      serviceAccountName(namePrefix),
			Namespace: namespace,
		}},
	}
}

func service(namespace string, namePrefix string, oTelColVersion string) *corev1.Service {
	return &corev1.Service{
		TypeMeta: metav1.TypeMeta{
			Kind:       "Service",
			APIVersion: "v1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      name(namePrefix, "opentelemetry-collector"),
			Namespace: namespace,
			Labels:    serviceLabels(oTelColVersion),
		},
		Spec: corev1.ServiceSpec{
			Type: corev1.ServiceTypeClusterIP,
			Ports: []corev1.ServicePort{
				{
					Name:        "otlp",
					Port:        4317,
					TargetPort:  intstr.FromInt32(4317),
					Protocol:    corev1.ProtocolTCP,
					AppProtocol: ptr.To("grpc"),
				},
				{
					Name:       "otlp-http",
					Port:       4318,
					TargetPort: intstr.FromInt32(4318),
					Protocol:   corev1.ProtocolTCP,
				},
			},
			Selector: map[string]string{
				appKubernetesIoNameKey:     appKubernetesIoNameValue,
				appKubernetesIoInstanceKey: appKubernetesIoInstanceValue,
				componentLabelKey:          serviceComponent,
			},
			InternalTrafficPolicy: ptr.To(corev1.ServiceInternalTrafficPolicyLocal),
		},
	}
}

func daemonSet(namespace string, namePrefix string, oTelColVersion string) *appsv1.DaemonSet {
	return &appsv1.DaemonSet{
		TypeMeta: metav1.TypeMeta{
			Kind:       "DaemonSet",
			APIVersion: "apps/v1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      name(namePrefix, "opentelemetry-collector-agent"),
			Namespace: namespace,
			Labels:    labels(oTelColVersion, true),
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
					ServiceAccountName: serviceAccountName(namePrefix),
					SecurityContext:    &corev1.PodSecurityContext{},
					Containers: []corev1.Container{
						{
							Name: "opentelemetry-collector",
							Args: []string{
								"--config=/conf/relay.yaml",
							},
							SecurityContext: &corev1.SecurityContext{},
							Image:           fmt.Sprintf("otel/opentelemetry-collector-k8s:%s", oTelColVersion),
							ImagePullPolicy: corev1.PullIfNotPresent,
							Ports: []corev1.ContainerPort{
								{
									Name:          "otlp",
									ContainerPort: 4317,
									Protocol:      corev1.ProtocolTCP,
									HostPort:      4317,
								},
								{
									Name:          "otlp-http",
									ContainerPort: 4318,
									Protocol:      corev1.ProtocolTCP,
									HostPort:      4318,
								},
							},
							Env: []corev1.EnvVar{
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
										FieldRef: &corev1.ObjectFieldSelector{
											FieldPath: "spec.nodeName",
										},
									},
								},
								{
									Name:  "GOMEMLIMIT",
									Value: "400MiB",
								},
							},
							LivenessProbe: &corev1.Probe{
								ProbeHandler: corev1.ProbeHandler{
									HTTPGet: &corev1.HTTPGetAction{
										Path: "/",
										Port: intstr.FromInt32(13133),
									},
								},
							},
							ReadinessProbe: &corev1.Probe{
								ProbeHandler: corev1.ProbeHandler{
									HTTPGet: &corev1.HTTPGetAction{
										Path: "/",
										Port: intstr.FromInt32(13133),
									},
								},
							},
							Resources: corev1.ResourceRequirements{
								Limits: corev1.ResourceList{
									corev1.ResourceMemory: resource.MustParse("500Mi"),
								},
							},
							VolumeMounts: []corev1.VolumeMount{
								{
									Name:      "opentelemetry-collector-configmap",
									MountPath: "/conf",
								},
								{
									Name:      "dash0-secret-volume",
									MountPath: "/etc/dash0/secret-volume",
									ReadOnly:  true,
								},
							},
						},
					},
					Volumes: []corev1.Volume{
						{
							Name: "opentelemetry-collector-configmap",
							VolumeSource: corev1.VolumeSource{
								ConfigMap: &corev1.ConfigMapVolumeSource{
									LocalObjectReference: corev1.LocalObjectReference{
										Name: configMapName(namePrefix),
									},
									Items: []corev1.KeyToPath{
										{
											Key:  "relay",
											Path: "relay.yaml",
										},
									},
								},
							},
						},
						{
							Name: "dash0-secret-volume",
							VolumeSource: corev1.VolumeSource{
								Secret: &corev1.SecretVolumeSource{
									SecretName: dash0AuthorizationSecretName,
								},
							},
						},
					},
					HostNetwork: false,
				},
			},
		},
	}
}

func serviceAccountName(namePrefix string) string {
	return name(namePrefix, "opentelemetry-collector")
}

func configMapName(namePrefix string) string {
	return name(namePrefix, "opentelemetry-collector-agent")
}

func clusterRoleName(namePrefix string) string {
	return name(namePrefix, "opentelemetry-collector")
}

func serviceLabels(oTelColVersion string) map[string]string {
	lbls := labels(oTelColVersion, false)
	lbls[componentLabelKey] = serviceComponent
	return lbls
}

func name(prefix string, suffix string) string {
	return fmt.Sprintf("%s-%s", prefix, suffix)
}

func labels(oTelColVersion string, addOptOutLabel bool) map[string]string {
	lbls := map[string]string{
		appKubernetesIoNameKey:      appKubernetesIoNameValue,
		appKubernetesIoInstanceKey:  appKubernetesIoInstanceValue,
		appKubernetesIoVersionKey:   oTelColVersion,
		appKubernetesIoManagedByKey: appKubernetesIoManagedByValue,
	}
	if addOptOutLabel {
		lbls[dash0OptOutLabelKey] = "false"
	}
	return lbls
}
