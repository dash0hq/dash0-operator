// SPDX-FileCopyrightText: Copyright 2025 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package taresources

import (
	"bytes"
	"fmt"
	"html/template"
	"strings"

	"github.com/dash0hq/dash0-operator/internal/util"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	targetAllocator                     = "opentelemetry-target-allocator"
	targetAllocatorDeploymentNameSuffix = "opentelemetry-target-allocator"

	// label values
	appKubernetesIoNameValue      = targetAllocator
	appKubernetesIoInstanceValue  = "dash0-operator"
	appKubernetesIoManagedByValue = "dash0-operator"
)

var (
	deploymentMatchLabels = map[string]string{
		util.AppKubernetesIoNameLabel:     appKubernetesIoNameValue,
		util.AppKubernetesIoInstanceLabel: appKubernetesIoInstanceValue,
	}
)

type targetAllocatorConfig struct {
	// OperatorNamespace is the namespace of the Dash0 operator
	OperatorNamespace string

	// NamePrefix is used as a prefix for OTel target-allocator Kubernetes resources created by the operator, set to value of
	// the environment variable OTEL_TARGET_ALLOCATOR_NAME_PREFIX, which is set to the Helm release name by the operator Helm
	// chart.
	NamePrefix string
}

// This type just exists to ensure all created objects go through addCommonMetadata.
type clientObject struct {
	object client.Object
}

type ConfigMapParams struct {
	CollectorNamespace string
	CollectorComponent string
}

// todo: allow/deny namespaces based on config
const targetAllocatorTemplate = `allocation_strategy: per-node
collector_namespace: {{ .CollectorNamespace }}
collector_selector:
  matchLabels:
    app.kubernetes.io/component: {{ .CollectorComponent }}
config:
  scrape_configs: []
filter_strategy: relabel-config
prometheus_cr:
  enabled: true
  pod_monitor_selector: {}
  scrape_config_selector: {}
  scrapeInterval: 30s
  service_monitor_selector: {}
`

func assembleDesiredStateForUpsert(
	config *targetAllocatorConfig,
) ([]clientObject, error) {
	return assembleDesiredState(config, false)
}

func assembleDesiredStateForDelete(
	config *targetAllocatorConfig,
) ([]clientObject, error) {
	return assembleDesiredState(config, true)
}

func assembleDesiredState(config *targetAllocatorConfig, forDeletion bool) ([]clientObject, error) {
	// todo: deletion
	_ = forDeletion
	var desiredState []clientObject
	desiredState = append(desiredState, addCommonMetadata(assembleServiceAccount(config)))
	cm, err := assembleConfigMap(config)
	if err != nil {
		return desiredState, err
	}
	desiredState = append(desiredState, addCommonMetadata(cm))
	desiredState = append(desiredState, addCommonMetadata(assembleClusterRole(config)))
	desiredState = append(desiredState, addCommonMetadata(assembleClusterRoleBinding(config)))
	desiredState = append(desiredState, addCommonMetadata(assembleService(config)))
	desiredState = append(desiredState, addCommonMetadata(assembleDeployment(config)))
	return desiredState, nil
}

func assembleConfigMap(c *targetAllocatorConfig) (*corev1.ConfigMap, error) {
	tmpl, err := template.New("targetallocator").Parse(targetAllocatorTemplate)
	if err != nil {
		return nil, fmt.Errorf("failed to parse target-allocator config template: %w", err)
	}

	// todo: decide what needs to be configurable (scrapeInterval,...)
	cmp := ConfigMapParams{
		CollectorNamespace: c.OperatorNamespace,
		CollectorComponent: "agent-collector", // todo: this needs to be kept in-sync with the label of the daemonset collector
	}

	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, cmp); err != nil {
		return nil, fmt.Errorf("failed to execute target-allocator config template: %w", err)
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
			"targetallocator.yaml": buf.String(),
		},
	}, nil
}

func assembleServiceAccount(c *targetAllocatorConfig) *corev1.ServiceAccount {
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
		AutomountServiceAccountToken: ptr.To(false),
	}
}

func assembleClusterRole(c *targetAllocatorConfig) *rbacv1.ClusterRole {
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
				Resources: []string{ // todo: removed some resources that we (hopefully) don't need, ensure this still works as extected
					"servicemonitors",
					"podmonitors",
					"prometheusrules",
					"probes",
					"scrapeconfigs",
				},
				Verbs: []string{"get", "list", "watch"}, // todo: this used to be '*', change it back if it causes errors
			},
			{
				APIGroups: []string{"apps"},
				Resources: []string{"statefulsets"},
				Verbs:     []string{"get", "list", "watch"},
			},
			{
				APIGroups: []string{""},
				Resources: []string{
					"namespaces",
					"nodes",
					"nodes/metrics",
					"services",
					"endpoints",
					"pods",
				},
				Verbs: []string{"get", "list", "watch"}, // todo: this used to be '*', change it back if it causes errors
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

func assembleClusterRoleBinding(c *targetAllocatorConfig) *rbacv1.ClusterRoleBinding {
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

func assembleService(c *targetAllocatorConfig) *corev1.Service {
	return &corev1.Service{
		TypeMeta: metav1.TypeMeta{
			APIVersion: util.K8sApiVersionCoreV1,
			Kind:       "Service",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      ServiceName(c.NamePrefix),
			Namespace: c.OperatorNamespace,
		},
		Spec: corev1.ServiceSpec{
			Selector: deploymentMatchLabels,
			Ports: []corev1.ServicePort{
				{
					Name:       "http-port",
					Protocol:   corev1.ProtocolTCP,
					Port:       80,
					TargetPort: intstr.FromString("http-port"),
				},
			},
		},
	}
}

func assembleDeployment(c *targetAllocatorConfig) *appsv1.Deployment {
	replicas := int32(1)
	defaultMode := int32(0444)

	return &appsv1.Deployment{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "apps/v1",
			Kind:       "Deployment",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      DeploymentName(c.NamePrefix),
			Namespace: c.OperatorNamespace,
		},
		Spec: appsv1.DeploymentSpec{
			Replicas: &replicas,
			Selector: &metav1.LabelSelector{
				MatchLabels: deploymentMatchLabels,
			},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: labels(),
				},
				Spec: corev1.PodSpec{
					ServiceAccountName:           ServiceAccountName(c.NamePrefix),
					AutomountServiceAccountToken: ptr.To(false),
					Containers: []corev1.Container{
						{
							Name:  "targetallocator",
							Image: "ghcr.io/open-telemetry/opentelemetry-operator/target-allocator:0.137.0",
							Ports: []corev1.ContainerPort{
								{
									ContainerPort: 8080,
									Name:          "http-port",
								},
							},
							VolumeMounts: []corev1.VolumeMount{
								{
									Name:      "config-volume",
									MountPath: "/conf/",
								},
								{
									Name:      "serviceaccount-token",
									MountPath: "/var/run/secrets/kubernetes.io/serviceaccount",
									ReadOnly:  true,
								},
							},
							Env: []corev1.EnvVar{
								{
									Name:  "OTELCOL_NAMESPACE",
									Value: c.OperatorNamespace,
								},
							},
							LivenessProbe: &corev1.Probe{
								ProbeHandler: corev1.ProbeHandler{
									HTTPGet: &corev1.HTTPGetAction{
										Path: "/livez",
										Port: intstr.FromInt(8080),
									},
								},
								InitialDelaySeconds: 15,
								PeriodSeconds:       20,
							},
							ReadinessProbe: &corev1.Probe{
								ProbeHandler: corev1.ProbeHandler{
									HTTPGet: &corev1.HTTPGetAction{
										Path: "/readyz",
										Port: intstr.FromInt(8080),
									},
								},
								InitialDelaySeconds: 5,
								PeriodSeconds:       10,
							},
						},
					},
					Volumes: []corev1.Volume{
						{
							Name: "config-volume",
							VolumeSource: corev1.VolumeSource{
								ConfigMap: &corev1.ConfigMapVolumeSource{
									LocalObjectReference: corev1.LocalObjectReference{
										Name: ConfigMapName(c.NamePrefix),
									},
								},
							},
						},
						{
							Name: "serviceaccount-token",
							VolumeSource: corev1.VolumeSource{
								Projected: &corev1.ProjectedVolumeSource{
									DefaultMode: &defaultMode,
									Sources: []corev1.VolumeProjection{
										{
											ServiceAccountToken: &corev1.ServiceAccountTokenProjection{
												Path: "token",
											},
										},
										{
											ConfigMap: &corev1.ConfigMapProjection{
												LocalObjectReference: corev1.LocalObjectReference{
													Name: "kube-root-ca.crt",
												},
												Items: []corev1.KeyToPath{
													{
														Key:  "ca.crt",
														Path: "ca.crt",
													},
												},
											},
										},
										{
											DownwardAPI: &corev1.DownwardAPIProjection{
												Items: []corev1.DownwardAPIVolumeFile{
													{
														Path: "namespace",
														FieldRef: &corev1.ObjectFieldSelector{
															APIVersion: "v1",
															FieldPath:  "metadata.namespace",
														},
													},
												},
											},
										},
									},
								},
							},
						},
					},
				},
			},
		},
	}
}

// ---utils---

func ConfigMapName(namePrefix string) string {
	return renderName(namePrefix, targetAllocatorDeploymentNameSuffix, "cm")
}

func ServiceAccountName(namePrefix string) string {
	return renderName(namePrefix, targetAllocatorDeploymentNameSuffix, "sa")
}

func ClusterRoleName(namePrefix string) string {
	return renderName(namePrefix, targetAllocatorDeploymentNameSuffix, "cr")
}

func ClusterRoleBindingName(namePrefix string) string {
	return renderName(namePrefix, targetAllocatorDeploymentNameSuffix, "crb")
}

func ServiceName(namePrefix string) string {
	return renderName(namePrefix, targetAllocatorDeploymentNameSuffix, "service")
}

func DeploymentName(namePrefix string) string {
	return renderName(namePrefix, targetAllocatorDeploymentNameSuffix, "deployment")
}

// todo: move to resources util
func renderName(prefix string, parts ...string) string {
	return strings.Join(append([]string{prefix}, parts...), "-")
}

// todo: move to resources util
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

func labels() map[string]string {
	lbls := map[string]string{
		util.AppKubernetesIoNameLabel:      appKubernetesIoNameValue,
		util.AppKubernetesIoInstanceLabel:  appKubernetesIoInstanceValue,
		util.AppKubernetesIoManagedByLabel: appKubernetesIoManagedByValue,
	}
	return lbls
}
