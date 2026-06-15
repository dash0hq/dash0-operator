// SPDX-FileCopyrightText: Copyright 2026 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package a0cresources

import (
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/dash0hq/dash0-operator/internal/util"
	"github.com/dash0hq/dash0-operator/internal/util/resources"
)

const (
	agent0Connector           = "dash0-agent0-connector"
	agent0ConnectorNameSuffix = "agent0-connector"

	containerName = "agent0-connector"

	// authTokenEnvVarName is the environment variable through which the agent0-connector workload receives the Dash0
	// authorization token (either a literal token value or resolved from a Kubernetes secret reference).
	authTokenEnvVarName = "DASH0_AGENT0_CONNECTOR_AUTH_TOKEN"

	// label values
	appKubernetesIoNameValue      = agent0Connector
	appKubernetesIoInstanceValue  = "dash0-operator"
	appKubernetesIoManagedByValue = "dash0-operator"

	defaultUser  int64 = 65532
	defaultGroup int64 = 0
)

var (
	deploymentMatchLabels = map[string]string{
		util.AppKubernetesIoNameLabel:     appKubernetesIoNameValue,
		util.AppKubernetesIoInstanceLabel: appKubernetesIoInstanceValue,
	}
)

// This type just exists to ensure all created objects go through addCommonMetadata.
type clientObject struct {
	object client.Object
}

func assembleDesiredState(config *util.Agent0ConnectorConfig, authTokenEnvVar *corev1.EnvVar) []clientObject {
	desiredState := make([]clientObject, 0, 4)
	desiredState = append(desiredState, addCommonMetadata(assembleServiceAccount(config)))
	desiredState = append(desiredState, addCommonMetadata(assembleClusterRole(config)))
	desiredState = append(desiredState, addCommonMetadata(assembleClusterRoleBinding(config)))
	desiredState = append(desiredState, addCommonMetadata(assembleDeployment(config, authTokenEnvVar)))
	return desiredState
}

func assembleServiceAccount(c *util.Agent0ConnectorConfig) *corev1.ServiceAccount {
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

// assembleClusterRole creates a cluster-wide, strictly read-only role. It grants get & list on every resource in
// every API group (including custom resources installed now or in the future) plus read access to non-resource URLs,
// which is exactly what read-only kubectl commands (kubectl get, kubectl describe, kubectl logs, ...) require. It
// deliberately contains no write verbs (create, update, patch, delete, deletecollection), so it cannot be used to
// modify any cluster state via kubectl.
func assembleClusterRole(c *util.Agent0ConnectorConfig) *rbacv1.ClusterRole {
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
			// Allow non-streaming read access to all CRD types.
			{
				APIGroups: []string{"*"},
				Resources: []string{"*"},
				// "watch" would be read-only as well, but we deliberately do not support any streaming commands, hence "watch"
				// is not needed.
				Verbs: []string{"get", "list"},
			},

			// We also need to allow read access to NonResourceURLs, like the following:
			//  - /api, /apis, /apis/<group> — API discovery
			//  - /openapi/v2, /openapi/v3 — the OpenAPI schema describing every resource type
			//  - /version — server version info
			//  - /healthz, /livez, /readyz — health endpoints
			//
			// kubectl performs API discovery on essentially every command. Before kubectl converts "get pods" into an HTTP
			// GET to /api/v1/.../pods, the client hits /api, /apis, /openapi/v3, ...
			{
				NonResourceURLs: []string{"*"},
				Verbs:           []string{"get"},
			},
		},
	}
}

func assembleClusterRoleBinding(c *util.Agent0ConnectorConfig) *rbacv1.ClusterRoleBinding {
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

func assembleDeployment(c *util.Agent0ConnectorConfig, authTokenEnvVar *corev1.EnvVar) *appsv1.Deployment {
	replicas := int32(1)

	container := corev1.Container{
		Name:  containerName,
		Image: c.Images.Agent0ConnectorImage,
		Env: []corev1.EnvVar{
			{
				// The agent0-connector workload uses the pseudo cluster UID as its client ID when connecting to the
				// Dash0 backend.
				Name:  "K8S_CLUSTER_UID",
				Value: string(c.PseudoClusterUid),
			},
			{
				// The address of the Dash0 backend service the agent0-connector workload connects to.
				Name:  "DASH0_AGENT0_CONNECTOR_SERVER_ADDRESS",
				Value: c.ServerAddress,
			},
			{
				// kubectl writes its discovery cache below $HOME; point it at the writable tmp volume since the root
				// filesystem is read-only.
				Name:  "DASH0_KUBECTL_TMP",
				Value: "/tmp",
			},
		},
		VolumeMounts: []corev1.VolumeMount{
			{
				Name:      "tmp",
				MountPath: "/tmp",
			},
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
		Resources: corev1.ResourceRequirements{
			Requests: corev1.ResourceList{
				corev1.ResourceMemory: resource.MustParse("32Mi"),
			},
			Limits: corev1.ResourceList{
				corev1.ResourceMemory: resource.MustParse("128Mi"),
			},
		},
	}

	if authTokenEnvVar != nil {
		// A missing authorization token is tolerated here so the desired state can still be assembled for DeleteResources
		// (where the token is irrelevant).
		container.Env = append(container.Env, *authTokenEnvVar)
	}

	if c.Insecure {
		container.Env = append(container.Env, corev1.EnvVar{
			// Disables TLS for the connection to the Dash0 backend; only intended for local development.
			Name:  "DASH0_AGENT0_CONNECTOR_INSECURE",
			Value: "true",
		})
	}

	if c.Images.Agent0ConnectorImagePullPolicy != "" {
		container.ImagePullPolicy = c.Images.Agent0ConnectorImagePullPolicy
	}

	podSpec := corev1.PodSpec{
		ServiceAccountName: ServiceAccountName(c.NamePrefix),
		Containers: []corev1.Container{
			container,
		},
		Volumes: []corev1.Volume{
			{
				// Writable scratch volume for kubectl's discovery cache, given the read-only root filesystem.
				Name: "tmp",
				VolumeSource: corev1.VolumeSource{
					EmptyDir: &corev1.EmptyDirVolumeSource{},
				},
			},
		},
		SecurityContext: &corev1.PodSecurityContext{
			RunAsNonRoot: new(true),
			SeccompProfile: &corev1.SeccompProfile{
				Type: corev1.SeccompProfileTypeRuntimeDefault,
			},
			RunAsUser:  new(defaultUser),
			RunAsGroup: new(defaultGroup),
		},
	}

	return &appsv1.Deployment{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "apps/v1",
			Kind:       "Deployment",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      DeploymentName(c.NamePrefix),
			Namespace: c.OperatorNamespace,
			Labels:    labels(),
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
				Spec: podSpec,
			},
		},
	}
}

// ---utils---

func ServiceAccountName(namePrefix string) string {
	return resources.RenderName(namePrefix, agent0ConnectorNameSuffix, "sa")
}

func ClusterRoleName(namePrefix string) string {
	return resources.RenderName(namePrefix, agent0ConnectorNameSuffix, "cr")
}

func ClusterRoleBindingName(namePrefix string) string {
	return resources.RenderName(namePrefix, agent0ConnectorNameSuffix, "crb")
}

// DeploymentName returns the name of the agent0-connector deployment, which is "<namePrefix>-agent0-connector".
func DeploymentName(namePrefix string) string {
	return resources.RenderName(namePrefix, agent0ConnectorNameSuffix)
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

func labels() map[string]string {
	return map[string]string{
		util.AppKubernetesIoNameLabel:      appKubernetesIoNameValue,
		util.AppKubernetesIoInstanceLabel:  appKubernetesIoInstanceValue,
		util.AppKubernetesIoManagedByLabel: appKubernetesIoManagedByValue,
	}
}
