// SPDX-FileCopyrightText: Copyright 2024 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package otelcolresources

import (
	"bytes"
	"fmt"
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
)

type E2eTestConfig struct {
	Enabled   bool
	ExportDir string
}

type oTelColConfig struct {
	Namespace                  string
	NamePrefix                 string
	IngressEndpoint            string
	AuthorizationToken         string
	SecretRef                  string
	CollectorImage             string
	ConfigurationReloaderImage string
	e2eTest                    E2eTestConfig
}

type oTelConfigTemplateValues struct {
	TokenOrFilename string
	oTelColConfig
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
	appKubernetesIoVersionKey        = "app.kubernetes.io/version"
	appKubernetesIoManagedByKey      = "app.kubernetes.io/managed-by"
	dash0OptOutLabelKey              = "dash0.com/enable"

	// label values
	appKubernetesIoNameValue      = openTelemetryCollector
	appKubernetesIoInstanceValue  = "dash0-operator"
	appKubernetesIoManagedByValue = "dash0-operator"

	collectorYaml      = "collector.yaml"
	collectorExtraYaml = "collector-extra.yaml"
)

var (
	daemonSetMatchLabels = map[string]string{
		appKubernetesIoNameKey:           appKubernetesIoNameValue,
		appKubernetesIoInstanceKey:       appKubernetesIoInstanceValue,
		appKubernetesIoComponentLabelKey: serviceComponent,
	}

	authWithTokenTemplate    = template.Must(template.New("auth-with-token").Parse("token: {{ .AuthorizationToken }}"))
	authWithFilenameTemplate = template.Must(template.New("auth-with-filename").Parse("filename: /etc/dash0/secret-volume/dash0-authorization-token"))

	configTemplate = template.Must(template.New("collector-config").Parse(`
    exporters:
      debug: {}
      # debug:
      #   verbosity: detailed
      otlp:
        auth:
          authenticator: bearertokenauth/dash0
        endpoint: {{ .IngressEndpoint }}

    extensions:
      bearertokenauth/dash0:
        scheme: Bearer
        {{ .TokenOrFilename }}
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
	if config.IngressEndpoint == "" {
		return nil, fmt.Errorf("no ingress endpoint provided, unable to create the OpenTelemetry collector")
	}
	useSecretRef := false
	if config.AuthorizationToken == "" && config.SecretRef == "" {
		return nil, fmt.Errorf("neither an authorization token nor a reference to a Kubernetes secret has been " +
			"provided, unable to create the OpenTelemetry collector")
	} else if config.AuthorizationToken == "" {
		useSecretRef = true
	}

	var desiredState []client.Object
	desiredState = append(desiredState, serviceAccount(config))
	cnfgMap, err := configMap(config, useSecretRef)
	if err != nil {
		return desiredState, err
	}
	desiredState = append(desiredState, cnfgMap)
	desiredState = append(desiredState, clusterRole(config))
	desiredState = append(desiredState, clusterRoleBinding(config))
	desiredState = append(desiredState, service(config))
	desiredState = append(desiredState, daemonSet(config, useSecretRef))
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

func configMap(config *oTelColConfig, useSecretRef bool) (*corev1.ConfigMap, error) {
	var tokenOrFilename string
	if useSecretRef {
		var authWithFilename bytes.Buffer
		err := authWithFilenameTemplate.Execute(&authWithFilename, config)
		if err != nil {
			return nil, err
		}
		tokenOrFilename = authWithFilename.String()
	} else {
		var authWithToken bytes.Buffer
		err := authWithTokenTemplate.Execute(&authWithToken, config)
		if err != nil {
			return nil, err
		}
		tokenOrFilename = authWithToken.String()
	}

	values := oTelConfigTemplateValues{
		TokenOrFilename: tokenOrFilename,
		oTelColConfig:   *config,
	}
	var collectorYamlContent bytes.Buffer
	err := configTemplate.Execute(&collectorYamlContent, values)
	if err != nil {
		return nil, err
	}

	configMapData := map[string]string{
		collectorYaml: collectorYamlContent.String(),
	}
	if config.e2eTest.Enabled {
		var collectorExtraYamlContent bytes.Buffer
		err = extraConfigTemplate.Execute(&collectorExtraYamlContent, values)
		if err != nil {
			return nil, err
		}
		configMapData[collectorExtraYaml] = collectorExtraYamlContent.String()
	}

	return &corev1.ConfigMap{
		TypeMeta: metav1.TypeMeta{
			Kind:       "ConfigMap",
			APIVersion: "v1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      configMapName(config.NamePrefix),
			Namespace: config.Namespace,
			Labels:    labels(false),
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
				appKubernetesIoNameKey:           appKubernetesIoNameValue,
				appKubernetesIoInstanceKey:       appKubernetesIoInstanceValue,
				appKubernetesIoComponentLabelKey: serviceComponent,
			},
			InternalTrafficPolicy: ptr.To(corev1.ServiceInternalTrafficPolicyLocal),
		},
	}
}

func daemonSet(config *oTelColConfig, useSecretRef bool) *appsv1.DaemonSet {
	configFiles := []string{
		"/etc/otelcol/conf/collector.yaml",
	}

	if config.e2eTest.Enabled {
		configFiles = append(configFiles, "/etc/otelcol/conf/collector-extra.yaml")
	}

	oTelColArgs := []string{}
	for _, configFile := range configFiles {
		oTelColArgs = append(oTelColArgs, "--config=file:/"+configFile)
	}

	collectorPidFilePath := "/etc/otelcol/run/pid.file"
	volumeMounts := []corev1.VolumeMount{
		{
			Name:      "opentelemetry-collector-configmap",
			MountPath: "/etc/otelcol/conf",
			ReadOnly:  true,
		},
		{
			Name:      "opentelemetry-collector-pidfile",
			MountPath: filepath.Dir(collectorPidFilePath),
			ReadOnly:  false,
		},
	}
	if !config.e2eTest.Enabled {
		if useSecretRef {
			volumeMounts = append(volumeMounts, corev1.VolumeMount{
				Name:      "dash0-secret-volume",
				MountPath: "/etc/dash0/secret-volume",
				ReadOnly:  true,
			})
		}
	} else {
		volumeMounts = append(volumeMounts, corev1.VolumeMount{
			Name:      "telemetry-file-export",
			MountPath: "/collector-received-data",
			ReadOnly:  false,
		})
	}

	configMapItems := []corev1.KeyToPath{{
		Key:  collectorYaml,
		Path: collectorYaml,
	}}
	if config.e2eTest.Enabled {
		configMapItems = append(configMapItems, corev1.KeyToPath{
			Key:  collectorExtraYaml,
			Path: collectorExtraYaml,
		})
	}

	pidFileVolumeSizeLimit := resource.MustParse("1M")
	directoryOrCreate := corev1.HostPathDirectoryOrCreate
	volumes := []corev1.Volume{
		{
			Name: "opentelemetry-collector-configmap",
			VolumeSource: corev1.VolumeSource{
				ConfigMap: &corev1.ConfigMapVolumeSource{
					LocalObjectReference: corev1.LocalObjectReference{
						Name: configMapName(config.NamePrefix),
					},
					Items: configMapItems,
				},
			},
		},
		{
			Name: "opentelemetry-collector-pidfile",
			VolumeSource: corev1.VolumeSource{
				EmptyDir: &corev1.EmptyDirVolumeSource{
					SizeLimit: &pidFileVolumeSizeLimit,
				},
			},
		},
	}
	if !config.e2eTest.Enabled {
		if useSecretRef {
			volumes = append(volumes, corev1.Volume{
				Name: "dash0-secret-volume",
				VolumeSource: corev1.VolumeSource{
					Secret: &corev1.SecretVolumeSource{
						SecretName: config.SecretRef,
					},
				},
			})
		}
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

	truthy := true

	configReloaderArgs := []string{
		"--pidfile=" + collectorPidFilePath,
	}
	configReloaderArgs = append(configReloaderArgs, configFiles...)

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
					// Enable configuration processor to send Unix signals to the collector processor
					ShareProcessNamespace: &truthy,
					Containers: []corev1.Container{
						{
							Name:            openTelemetryCollector,
							Args:            oTelColArgs,
							SecurityContext: &corev1.SecurityContext{},
							Image:           config.CollectorImage,
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
								{
									Name:          "k8s-probes",
									ContainerPort: 13133,
									Protocol:      corev1.ProtocolTCP,
									HostPort:      13133,
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
									Name:  "DASH0_COLLECTOR_PID_FILE",
									Value: collectorPidFilePath,
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
						{
							Name:            configReloader,
							Args:            configReloaderArgs,
							SecurityContext: &corev1.SecurityContext{},
							Image:           config.ConfigurationReloaderImage,
							ImagePullPolicy: corev1.PullIfNotPresent,
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
	return name(namePrefix, openTelemetryCollector)
}

func configMapName(namePrefix string) string {
	return name(namePrefix, openTelemetryCollectorAgent)
}

func clusterRoleName(namePrefix string) string {
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
