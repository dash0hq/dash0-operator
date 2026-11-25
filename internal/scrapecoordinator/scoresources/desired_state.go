// SPDX-FileCopyrightText: Copyright 2026 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package scoresources

import (
	"crypto/sha256"
	"fmt"
	"slices"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/yaml"

	"github.com/dash0hq/dash0-operator/internal/selfmonitoringapiaccess"
	"github.com/dash0hq/dash0-operator/internal/util"
	"github.com/dash0hq/dash0-operator/internal/util/resources"
)

const (
	scrapeCoordinator = "scrape-coordinator"
	containerName     = "scrape-coordinator"

	configVolumeName = "config"
	configDir        = "/etc/scrape-coordinator"
	configFileName   = "config.yaml"

	// CertsVolumeName is the name of the volume with the server certificate for mTLS.
	CertsVolumeName = "mtls-certs"
	certsDir        = "/etc/certs/scrape-coordinator-server"

	httpPort  = 8080
	httpsPort = 8443

	configHashAnnotation = "dash0.com/scrape-coordinator-config-sha"

	appKubernetesIoNameValue      = scrapeCoordinator
	appKubernetesIoInstanceValue  = "dash0-operator"
	appKubernetesIoManagedByValue = "dash0-operator"

	defaultUser  int64 = 65532
	defaultGroup int64 = 65532
)

var (
	deploymentMatchLabels = map[string]string{
		util.AppKubernetesIoNameLabel:     appKubernetesIoNameValue,
		util.AppKubernetesIoInstanceLabel: appKubernetesIoInstanceValue,
	}

	// The environment variables from which the OpenTelemetry Go SDK of the scrape coordinator derives the Kubernetes
	// resource attributes of its self-monitoring telemetry, see images/pkg/common/otel.go#assembleResource. They are
	// set no matter whether self-monitoring is enabled.
	k8sNodeNameEnvVar = corev1.EnvVar{
		Name: "K8S_NODE_NAME",
		ValueFrom: &corev1.EnvVarSource{
			FieldRef: &corev1.ObjectFieldSelector{FieldPath: "spec.nodeName"},
		},
	}
	operatorNamespaceEnvVar = corev1.EnvVar{
		Name: "DASH0_OPERATOR_NAMESPACE",
		ValueFrom: &corev1.EnvVarSource{
			FieldRef: &corev1.ObjectFieldSelector{FieldPath: "metadata.namespace"},
		},
	}
	k8sPodNameEnvVar = corev1.EnvVar{
		Name: "K8S_POD_NAME",
		ValueFrom: &corev1.EnvVarSource{
			FieldRef: &corev1.ObjectFieldSelector{FieldPath: "metadata.name"},
		},
	}
	k8sPodUidEnvVar = corev1.EnvVar{
		Name: "K8S_POD_UID",
		ValueFrom: &corev1.EnvVarSource{
			FieldRef: &corev1.ObjectFieldSelector{FieldPath: "metadata.uid"},
		},
	}
)

// This type just exists to ensure all created objects go through addCommonMetadata.
type clientObject struct {
	object client.Object
}

// selfMonitoringInput carries everything the self-monitoring of the scrape coordinator is derived from. It comes from
// the Dash0OperatorConfiguration resource and is therefore read again for every reconciliation.
type selfMonitoringInput struct {
	configuration selfmonitoringapiaccess.SelfMonitoringConfiguration
	clusterName   string
}

// coordinatorConfig is the configuration file of the scrape coordinator, see
// images/scrape-coordinator/src/internal/config/config.go.
type coordinatorConfig struct {
	CollectorNamespace string                   `json:"collectorNamespace"`
	Pools              coordinatorPools         `json:"pools"`
	PrometheusCRs      coordinatorPrometheusCRs `json:"prometheusCRs"`
	Server             coordinatorServer        `json:"server"`
}

type coordinatorPools struct {
	PerNode coordinatorPool `json:"perNode"`
}

type coordinatorPool struct {
	Selector metav1.LabelSelector `json:"selector"`
}

type coordinatorPrometheusCRs struct {
	Namespaces       []string `json:"namespaces,omitempty"`
	SecretNamespaces []string `json:"secretNamespaces,omitempty"`
}

type coordinatorServer struct {
	ListenAddr               string          `json:"listenAddr"`
	TLS                      *coordinatorTLS `json:"tls,omitempty"`
	AllowInsecureAuthSecrets bool            `json:"allowInsecureAuthSecrets,omitempty"`
}

type coordinatorTLS struct {
	ListenAddr string `json:"listenAddr"`
	CAFile     string `json:"caFile"`
	CertFile   string `json:"certFile"`
	KeyFile    string `json:"keyFile"`
}

func assembleDesiredState(
	config *util.ScrapeCoordinatorConfig,
	namespacesWithPrometheusScraping []string,
	extraConfig util.ExtraConfig,
	selfMonitoring selfMonitoringInput,
) ([]clientObject, error) {
	cm, err := assembleConfigMap(config, namespacesWithPrometheusScraping, extraConfig)
	if err != nil {
		return nil, err
	}
	deployment, err := assembleDeployment(config, cm, extraConfig, selfMonitoring)
	if err != nil {
		return nil, err
	}
	desiredState := make([]clientObject, 0, 6)
	desiredState = append(desiredState, addCommonMetadata(assembleServiceAccount(config)))
	desiredState = append(desiredState, addCommonMetadata(cm))
	desiredState = append(desiredState, addCommonMetadata(assembleClusterRole(config, extraConfig)))
	desiredState = append(desiredState, addCommonMetadata(assembleClusterRoleBinding(config)))
	desiredState = append(desiredState, addCommonMetadata(assembleService(config, extraConfig)))
	desiredState = append(desiredState, addCommonMetadata(deployment))
	return desiredState, nil
}

// sortedAndDeduplicated returns a sorted copy without duplicates, so that the configuration, and with it the pod
// template, only changes on actual changes.
func sortedAndDeduplicated(values []string) []string {
	result := slices.Clone(values)
	slices.Sort(result)
	return slices.Compact(result)
}

func assembleConfigMap(
	c *util.ScrapeCoordinatorConfig,
	namespacesWithPrometheusScraping []string,
	extraConfig util.ExtraConfig,
) (*corev1.ConfigMap, error) {
	cfg := coordinatorConfig{
		CollectorNamespace: c.OperatorNamespace,
		Pools: coordinatorPools{
			PerNode: coordinatorPool{
				Selector: metav1.LabelSelector{
					MatchLabels: map[string]string{util.AppKubernetesIoComponentLabel: c.CollectorComponent},
				},
			},
		},
		PrometheusCRs: coordinatorPrometheusCRs{
			Namespaces:       sortedAndDeduplicated(namespacesWithPrometheusScraping),
			SecretNamespaces: sortedAndDeduplicated(extraConfig.ScrapeCoordinatorAllowSecretAccessInNamespaces),
		},
		Server: coordinatorServer{
			ListenAddr:               fmt.Sprintf(":%d", httpPort),
			AllowInsecureAuthSecrets: extraConfig.ScrapeCoordinatorAllowInsecureAuthSecrets,
		},
	}
	if extraConfig.ScrapeCoordinatorMtlsEnabled {
		cfg.Server.TLS = &coordinatorTLS{
			ListenAddr: fmt.Sprintf(":%d", httpsPort),
			CAFile:     certsDir + "/ca.crt",
			CertFile:   certsDir + "/tls.crt",
			KeyFile:    certsDir + "/tls.key",
		}
	}
	content, err := yaml.Marshal(cfg)
	if err != nil {
		return nil, fmt.Errorf("cannot render the scrape coordinator configuration: %w", err)
	}

	return &corev1.ConfigMap{
		TypeMeta: metav1.TypeMeta{
			APIVersion: util.K8sApiVersionCoreV1,
			Kind:       "ConfigMap",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      ConfigMapName(c.NamePrefix),
			Namespace: c.OperatorNamespace,
			Labels:    labels(),
		},
		Data: map[string]string{
			configFileName: string(content),
		},
	}, nil
}

func assembleServiceAccount(c *util.ScrapeCoordinatorConfig) *corev1.ServiceAccount {
	return &corev1.ServiceAccount{
		TypeMeta: metav1.TypeMeta{
			APIVersion: util.K8sApiVersionCoreV1,
			Kind:       "ServiceAccount",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      ServiceAccountName(c.NamePrefix),
			Namespace: c.OperatorNamespace,
			Labels:    labels(),
		},
	}
}

func assembleClusterRole(c *util.ScrapeCoordinatorConfig, extraConfig util.ExtraConfig) *rbacv1.ClusterRole {
	coreResources := []string{
		"namespaces",
		"nodes",
		"nodes/metrics",
		"services",
		"endpoints",
		"pods",
	}
	// With allowSecretAccessInNamespaces set, the Helm chart grants access to secrets via namespaced role bindings.
	if len(extraConfig.ScrapeCoordinatorAllowSecretAccessInNamespaces) == 0 {
		coreResources = append(coreResources, "secrets")
	}
	return &rbacv1.ClusterRole{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "rbac.authorization.k8s.io/v1",
			Kind:       "ClusterRole",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:   ClusterRoleName(c.NamePrefix),
			Labels: labels(),
		},
		Rules: []rbacv1.PolicyRule{
			{
				APIGroups: []string{"monitoring.coreos.com"},
				Resources: []string{
					"servicemonitors",
					"podmonitors",
					"prometheusrules",
					"probes",
					"scrapeconfigs",
				},
				Verbs: []string{"get", "list", "watch"},
			},
			{
				APIGroups: []string{"apps"},
				Resources: []string{"statefulsets"},
				Verbs:     []string{"get", "list", "watch"},
			},
			{
				APIGroups: []string{""},
				Resources: coreResources,
				Verbs:     []string{"get", "list", "watch"},
			},
			{
				APIGroups: []string{""},
				Resources: []string{"configmaps"},
				Verbs:     []string{"get"},
			},
			{
				APIGroups: []string{"discovery.k8s.io"},
				Resources: []string{"endpointslices"},
				Verbs:     []string{"get", "list", "watch"},
			},
			{
				APIGroups: []string{"networking.k8s.io"},
				Resources: []string{"ingresses"},
				Verbs:     []string{"get", "list", "watch"},
			},
			{
				NonResourceURLs: []string{"/metrics"},
				Verbs:           []string{"get"},
			},
		},
	}
}

func assembleClusterRoleBinding(c *util.ScrapeCoordinatorConfig) *rbacv1.ClusterRoleBinding {
	return &rbacv1.ClusterRoleBinding{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "rbac.authorization.k8s.io/v1",
			Kind:       "ClusterRoleBinding",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:   ClusterRoleBindingName(c.NamePrefix),
			Labels: labels(),
		},
		RoleRef: rbacv1.RoleRef{
			APIGroup: "rbac.authorization.k8s.io",
			Kind:     "ClusterRole",
			Name:     ClusterRoleName(c.NamePrefix),
		},
		Subjects: []rbacv1.Subject{
			{
				Kind:      "ServiceAccount",
				Name:      ServiceAccountName(c.NamePrefix),
				Namespace: c.OperatorNamespace,
			},
		},
	}
}

func assembleService(c *util.ScrapeCoordinatorConfig, extraConfig util.ExtraConfig) *corev1.Service {
	ports := []corev1.ServicePort{
		{
			Name:       "http",
			Protocol:   corev1.ProtocolTCP,
			Port:       80,
			TargetPort: intstr.FromString("http"),
		},
	}
	if extraConfig.ScrapeCoordinatorMtlsEnabled {
		ports = append(ports, corev1.ServicePort{
			Name:       "https",
			Protocol:   corev1.ProtocolTCP,
			Port:       443,
			TargetPort: intstr.FromString("https"),
		})
	}
	return &corev1.Service{
		TypeMeta: metav1.TypeMeta{
			APIVersion: util.K8sApiVersionCoreV1,
			Kind:       "Service",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      ServiceName(c.NamePrefix),
			Namespace: c.OperatorNamespace,
			Labels:    labels(),
		},
		Spec: corev1.ServiceSpec{
			Selector: deploymentMatchLabels,
			Ports:    ports,
		},
	}
}

func assembleDeployment(
	c *util.ScrapeCoordinatorConfig,
	configMap *corev1.ConfigMap,
	extraConfig util.ExtraConfig,
	selfMonitoring selfMonitoringInput,
) (*appsv1.Deployment, error) {
	replicas := int32(1)
	deploymentName := DeploymentName(c.NamePrefix)

	ports := []corev1.ContainerPort{
		{
			Name:          "http",
			ContainerPort: httpPort,
			Protocol:      corev1.ProtocolTCP,
		},
	}
	volumeMounts := []corev1.VolumeMount{
		{
			Name:      configVolumeName,
			MountPath: configDir,
			ReadOnly:  true,
		},
	}
	volumes := []corev1.Volume{
		{
			Name: configVolumeName,
			VolumeSource: corev1.VolumeSource{
				ConfigMap: &corev1.ConfigMapVolumeSource{
					LocalObjectReference: corev1.LocalObjectReference{
						Name: ConfigMapName(c.NamePrefix),
					},
				},
			},
		},
	}
	if extraConfig.ScrapeCoordinatorMtlsEnabled {
		ports = append(ports, corev1.ContainerPort{
			Name:          "https",
			ContainerPort: httpsPort,
			Protocol:      corev1.ProtocolTCP,
		})
		volumeMounts = append(volumeMounts, corev1.VolumeMount{
			Name:      CertsVolumeName,
			MountPath: certsDir,
			ReadOnly:  true,
		})
		volumes = append(volumes, corev1.Volume{
			Name: CertsVolumeName,
			VolumeSource: corev1.VolumeSource{
				Secret: &corev1.SecretVolumeSource{
					SecretName: extraConfig.ScrapeCoordinatorMtlsServerCertSecretName,
				},
			},
		})
	}

	container := corev1.Container{
		Name:         containerName,
		Image:        c.Images.ScrapeCoordinatorImage,
		Args:         []string{"--config=" + configDir + "/" + configFileName},
		Ports:        ports,
		VolumeMounts: volumeMounts,
		Env: []corev1.EnvVar{
			{
				Name:  util.EnvVarGoMemLimit,
				Value: extraConfig.ScrapeCoordinatorContainerResources.EffectiveGoMemLimitPercent(util.GoMemLimitDefaultPercent),
			},
			{
				Name:  "K8S_CLUSTER_UID",
				Value: string(c.PseudoClusterUid),
			},
			{
				Name:  "K8S_CLUSTER_NAME",
				Value: selfMonitoring.clusterName,
			},
			{
				Name:  "K8S_DEPLOYMENT_NAME",
				Value: deploymentName,
			},
			k8sNodeNameEnvVar,
			operatorNamespaceEnvVar,
			k8sPodUidEnvVar,
			k8sPodNameEnvVar,
		},
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
		LivenessProbe: &corev1.Probe{
			ProbeHandler: corev1.ProbeHandler{
				HTTPGet: &corev1.HTTPGetAction{
					Path: "/livez",
					Port: intstr.FromInt32(httpPort),
				},
			},
			InitialDelaySeconds: 15,
			PeriodSeconds:       20,
		},
		ReadinessProbe: &corev1.Probe{
			ProbeHandler: corev1.ProbeHandler{
				HTTPGet: &corev1.HTTPGetAction{
					Path: "/readyz",
					Port: intstr.FromInt32(httpPort),
				},
			},
			InitialDelaySeconds: 5,
			PeriodSeconds:       10,
		},
		Resources: extraConfig.ScrapeCoordinatorContainerResources.ToResourceRequirements(),
	}
	if c.Images.ScrapeCoordinatorPullPolicy != "" {
		container.ImagePullPolicy = c.Images.ScrapeCoordinatorPullPolicy
	}

	podSpec := corev1.PodSpec{
		ServiceAccountName: ServiceAccountName(c.NamePrefix),
		Tolerations:        extraConfig.ScrapeCoordinatorTolerations,
		Containers:         []corev1.Container{container},
		SecurityContext: &corev1.PodSecurityContext{
			RunAsNonRoot: new(true),
			SeccompProfile: &corev1.SeccompProfile{
				Type: corev1.SeccompProfileTypeRuntimeDefault,
			},
			RunAsUser:  util.RunAsID(c.IsOpenShift, defaultUser),
			RunAsGroup: new(defaultGroup),
		},
		Volumes: volumes,
	}
	if extraConfig.ScrapeCoordinatorNodeAffinity != nil {
		podSpec.Affinity = &corev1.Affinity{
			NodeAffinity: extraConfig.ScrapeCoordinatorNodeAffinity,
		}
	}

	// The scrape coordinator reads its configuration file once at startup, the hash restarts it on every change.
	podTemplateAnnotations := map[string]string{
		configHashAnnotation: configMapHash(configMap),
	}

	deployment := &appsv1.Deployment{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "apps/v1",
			Kind:       "Deployment",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:        deploymentName,
			Namespace:   c.OperatorNamespace,
			Labels:      util.MergeMaps(labels(), extraConfig.ScrapeCoordinatorLabels),
			Annotations: util.MergeMaps(nil, extraConfig.ScrapeCoordinatorAnnotations),
		},
		Spec: appsv1.DeploymentSpec{
			Replicas: &replicas,
			Selector: &metav1.LabelSelector{
				MatchLabels: deploymentMatchLabels,
			},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels:      util.MergeMaps(labels(), extraConfig.ScrapeCoordinatorPodLabels),
					Annotations: util.MergeMaps(podTemplateAnnotations, extraConfig.ScrapeCoordinatorPodAnnotations),
				},
				Spec: podSpec,
			},
		},
	}

	if selfMonitoring.configuration.SelfMonitoringEnabled {
		if err := selfmonitoringapiaccess.EnableSelfMonitoringInDeployment(
			deployment,
			selfMonitoring.configuration,
			c.Images.GetOperatorVersion(),
			c.DevelopmentMode,
			"",
		); err != nil {
			return nil, err
		}
	}

	return deployment, nil
}

func configMapHash(configMap *corev1.ConfigMap) string {
	return fmt.Sprintf("%x", sha256.Sum256([]byte(configMap.Data[configFileName])))
}

// ---utils---

// ConfigMapName returns the name of the config map with the scrape coordinator configuration.
func ConfigMapName(namePrefix string) string {
	return resources.RenderName(namePrefix, scrapeCoordinator, "cm")
}

// ServiceAccountName returns the name of the service account of the scrape coordinator.
func ServiceAccountName(namePrefix string) string {
	return resources.RenderName(namePrefix, scrapeCoordinator, "sa")
}

// ClusterRoleName returns the name of the cluster role of the scrape coordinator.
func ClusterRoleName(namePrefix string) string {
	return resources.RenderName(namePrefix, scrapeCoordinator, "cr")
}

// ClusterRoleBindingName returns the name of the cluster role binding of the scrape coordinator.
func ClusterRoleBindingName(namePrefix string) string {
	return resources.RenderName(namePrefix, scrapeCoordinator, "crb")
}

// ServiceName returns the name of the service the collectors query the scrape coordinator through.
func ServiceName(namePrefix string) string {
	return resources.RenderName(namePrefix, scrapeCoordinator, "service")
}

// DeploymentName returns the name of the scrape coordinator deployment.
func DeploymentName(namePrefix string) string {
	return resources.RenderName(namePrefix, scrapeCoordinator, "deployment")
}

func addCommonMetadata(object client.Object) clientObject {
	// Cluster-scoped resources have no owner reference, ArgoCD would prune them, see the target-allocator resources.
	if object.GetAnnotations() == nil {
		object.SetAnnotations(map[string]string{})
	}
	object.GetAnnotations()["argocd.argoproj.io/sync-options"] = "Prune=false"
	object.GetAnnotations()["argocd.argoproj.io/compare-options"] = "IgnoreExtraneous"
	return clientObject{
		object: object,
	}
}

func labels() map[string]string {
	return map[string]string{
		util.AppKubernetesIoNameLabel:      appKubernetesIoNameValue,
		util.AppKubernetesIoInstanceLabel:  appKubernetesIoInstanceValue,
		util.AppKubernetesIoManagedByLabel: appKubernetesIoManagedByValue,
	}
}
