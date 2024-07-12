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

type E2eTestConfig struct {
	Enabled   bool
	ExportDir string
}

type oTelColConfig struct {
	namespace      string
	namePrefix     string
	oTelColVersion string
	e2eTest        E2eTestConfig
}

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
        traces:
          processors:
          - k8sattributes
          - memory_limiter
          - batch
          receivers:
          - otlp
          exporters:
          - otlp
        metrics:
          processors:
          - k8sattributes
          - memory_limiter
          - batch
          receivers:
          - otlp
          exporters:
          - otlp
        logs:
          processors:
          - k8sattributes
          - memory_limiter
          - batch
          receivers:
          - otlp
          exporters:
          - otlp

      telemetry:
        metrics:
          address: ${env:MY_POD_IP}:8888
`))

	// With multiple config files (if activated), maps are merged recursively, other types (strings, arrays etc.) are
	// not merged but overwritten. Anything defined in the extra config file will overwrite the base config file.
	// See https://github.com/knadh/koanf/blob/c53f381935963555ce8986061bb765f415ae5cb7/maps/maps.go#L107-L138.
	// The extra config file is only used for testing purposes and is not active in production.
	extraConfigTemplate = template.Must(template.New("extra-collector-config").Parse(`
    exporters:
      file/traces:
        path: /collector-received-data/traces.jsonl
        flush_interval: 100ms
      file/metrics:
        path: /collector-received-data/metrics.jsonl
        flush_interval: 100ms
      file/logs:
        path: /collector-received-data/logs.jsonl
        flush_interval: 100ms

    service:
      extensions:
        - health_check
        # remove the reference to the bearertokenauth/dash0 extension by overwriting the extension array with a an
        # array that only has one element

      # add file exporters
      pipelines:
        traces:
          exporters:
            - file/traces
        metrics:
          exporters:
            - file/metrics
        logs:
          exporters:
            - file/logs
`))
)

func assembleDesiredState(config *oTelColConfig) ([]client.Object, error) {
	var desiredState []client.Object
	desiredState = append(desiredState, serviceAccount(config))
	cnfgMap, err := configMap(config)
	if err != nil {
		return desiredState, err
	}
	desiredState = append(desiredState, cnfgMap)
	desiredState = append(desiredState, clusterRole(config))
	desiredState = append(desiredState, clusterRoleBinding(config))
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
			Name:      serviceAccountName(config.namePrefix),
			Namespace: config.namespace,
			Labels:    labels(config.oTelColVersion, false),
		},
	}
}

func configMap(config *oTelColConfig) (*corev1.ConfigMap, error) {
	var collectorYaml bytes.Buffer
	err := configTemplate.Execute(&collectorYaml, nil)
	if err != nil {
		return nil, err
	}

	configMapData := map[string]string{
		"collector.yaml": collectorYaml.String(),
	}
	if config.e2eTest.Enabled {
		var collectorExtraYaml bytes.Buffer
		err = extraConfigTemplate.Execute(&collectorExtraYaml, nil)
		if err != nil {
			return nil, err
		}
		configMapData["collector-extra.yaml"] = collectorExtraYaml.String()
	}

	return &corev1.ConfigMap{
		TypeMeta: metav1.TypeMeta{
			Kind:       "ConfigMap",
			APIVersion: "v1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      configMapName(config.namePrefix),
			Namespace: config.namespace,
			Labels:    labels(config.oTelColVersion, false),
		},
		Data: configMapData,
	}, nil
}

func clusterRole(config *oTelColConfig) *rbacv1.ClusterRole {
	return &rbacv1.ClusterRole{
		TypeMeta: metav1.TypeMeta{
			Kind:       "ClusterRole",
			APIVersion: "rbac.authorization.k8s.io/v1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      clusterRoleName(config.namePrefix),
			Namespace: config.namespace,
			Labels:    labels(config.oTelColVersion, false),
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
			Name:      name(config.namePrefix, "opentelemetry-collector"),
			Namespace: config.namespace,
			Labels:    labels(config.oTelColVersion, false),
		},
		RoleRef: rbacv1.RoleRef{
			APIGroup: "rbac.authorization.k8s.io",
			Kind:     "ClusterRole",
			Name:     clusterRoleName(config.namePrefix),
		},
		Subjects: []rbacv1.Subject{{
			Kind:      "ServiceAccount",
			Name:      serviceAccountName(config.namePrefix),
			Namespace: config.namespace,
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
			Name:      name(config.namePrefix, "opentelemetry-collector"),
			Namespace: config.namespace,
			Labels:    serviceLabels(config.oTelColVersion),
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

func daemonSet(config *oTelColConfig) *appsv1.DaemonSet {
	oTelColArgs := []string{
		"--config=file:/conf/collector.yaml",
	}
	if config.e2eTest.Enabled {
		oTelColArgs = append(oTelColArgs, "--config=file:/conf/collector-extra.yaml")
	}

	volumeMounts := []corev1.VolumeMount{{
		Name:      "opentelemetry-collector-configmap",
		MountPath: "/conf",
	}}
	if !config.e2eTest.Enabled {
		volumeMounts = append(volumeMounts, corev1.VolumeMount{
			Name:      "dash0-secret-volume",
			MountPath: "/etc/dash0/secret-volume",
			ReadOnly:  true,
		})
	} else {
		volumeMounts = append(volumeMounts, corev1.VolumeMount{
			Name:      "telemetry-file-export",
			MountPath: "/collector-received-data",
		})
	}

	configMapItems := []corev1.KeyToPath{{
		Key:  "collector.yaml",
		Path: "collector.yaml",
	}}
	if config.e2eTest.Enabled {
		configMapItems = append(configMapItems, corev1.KeyToPath{
			Key:  "collector-extra.yaml",
			Path: "collector-extra.yaml",
		})
	}

	directoryOrCreate := corev1.HostPathDirectoryOrCreate
	volumes := []corev1.Volume{
		{
			Name: "opentelemetry-collector-configmap",
			VolumeSource: corev1.VolumeSource{
				ConfigMap: &corev1.ConfigMapVolumeSource{
					LocalObjectReference: corev1.LocalObjectReference{
						Name: configMapName(config.namePrefix),
					},
					Items: configMapItems,
				},
			},
		},
	}
	if !config.e2eTest.Enabled {
		volumes = append(volumes, corev1.Volume{
			Name: "dash0-secret-volume",
			VolumeSource: corev1.VolumeSource{
				Secret: &corev1.SecretVolumeSource{
					SecretName: dash0AuthorizationSecretName,
				},
			},
		})
	} else {
		volumes = append(volumes, corev1.Volume{
			Name: "telemetry-file-export",
			VolumeSource: corev1.VolumeSource{
				HostPath: &corev1.HostPathVolumeSource{
					Path: config.e2eTest.ExportDir,
					Type: &directoryOrCreate,
				},
			},
		})
	}

	return &appsv1.DaemonSet{
		TypeMeta: metav1.TypeMeta{
			Kind:       "DaemonSet",
			APIVersion: "apps/v1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      name(config.namePrefix, "opentelemetry-collector-agent"),
			Namespace: config.namespace,
			Labels:    labels(config.oTelColVersion, true),
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
					ServiceAccountName: serviceAccountName(config.namePrefix),
					SecurityContext:    &corev1.PodSecurityContext{},
					Containers: []corev1.Container{
						{
							Name:            "opentelemetry-collector",
							Args:            oTelColArgs,
							SecurityContext: &corev1.SecurityContext{},
							Image:           fmt.Sprintf("otel/opentelemetry-collector-k8s:%s", config.oTelColVersion),
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
							VolumeMounts: volumeMounts,
						},
					},
					Volumes:     volumes,
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
