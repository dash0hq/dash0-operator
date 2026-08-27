// SPDX-FileCopyrightText: Copyright 2026 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package a0cresources

import (
	"errors"
	"fmt"
	"slices"
	"strings"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
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

func assembleDesiredState(
	config *util.Agent0ConnectorConfig,
	authTokenEnvVar *corev1.EnvVar,
	extraConfig util.ExtraConfig,
) []clientObject {
	desiredState := make([]clientObject, 0, 4)
	desiredState = append(desiredState, addCommonMetadata(assembleServiceAccount(config)))
	desiredState = append(desiredState, addCommonMetadata(assembleClusterRole(config, extraConfig)))
	desiredState = append(desiredState, addCommonMetadata(assembleClusterRoleBinding(config)))
	desiredState = append(desiredState, addCommonMetadata(assembleDeployment(config, authTokenEnvVar, extraConfig)))
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

// allowedVerbs are the only verbs the agent0-connector's cluster role grants. The role is restricted to read-only
// verbs. The verb "watch" would be read-only as well, but streaming commands are not supported.
var allowedVerbs = []string{"get", "list"}

// selfSubjectReviewVerbs are the verbs granted for the self subject review API, the only exception from allowedVerbs.
// Creating a SelfSubjectAccessReview or a SelfSubjectRulesReview does not persist an object: the API server evaluates
// the request and answers it, so this "create" reads the agent0-connector's own permissions and cannot modify any
// cluster state. Both reviews always report on the caller's own service account, they cannot be used to inspect the
// permissions of another identity.
var selfSubjectReviewVerbs = []string{"create"}

// selfSubjectReviewApiGroup and selfSubjectReviewResources identify the self subject review API, the only place where
// selfSubjectReviewVerbs may be granted.
const selfSubjectReviewApiGroup = "authorization.k8s.io"

var selfSubjectReviewResources = []string{"selfsubjectaccessreviews", "selfsubjectrulesreviews"}

// defaultAgent0ConnectorRbacRules is the default list of RBAC policy rules the agent0-connector's cluster role grants.
//
// The Helm chart keeps its own copy of these rules in
// helm-chart/dash0-operator/files/agent0-connector-default-cluster-role-rules.yaml, from which it renders the
// -manager-agent0-connector-ro cluster role. Kubernetes' privilege escalation prevention only allows the operator to
// grant permissions it holds itself, so both lists always need to be changed together.
//
// Both lists are kept in the same order, so that they can be diffed side by side. The order itself carries no meaning.
var defaultAgent0ConnectorRbacRules = []rbacv1.PolicyRule{
	{
		APIGroups: []string{""},
		Resources: []string{
			"componentstatuses",
			"endpoints",
			"events",
			"limitranges",
			"namespaces",
			"nodes",
			"persistentvolumeclaims",
			"persistentvolumes",
			"podtemplates",
			"pods",
			// required by "kubectl logs"
			"pods/log",
			"replicationcontrollers",
			"resourcequotas",
			"serviceaccounts",
			"services",
		},
		Verbs: allowedVerbs,
	},
	{
		APIGroups: []string{"apps"},
		Resources: []string{
			"controllerrevisions",
			"daemonsets",
			"deployments",
			"replicasets",
			"statefulsets",
		},
		Verbs: allowedVerbs,
	},
	{
		APIGroups: []string{"batch"},
		Resources: []string{
			"cronjobs",
			"jobs",
		},
		Verbs: allowedVerbs,
	},
	{
		APIGroups: []string{"autoscaling"},
		Resources: []string{"horizontalpodautoscalers"},
		Verbs:     allowedVerbs,
	},
	{
		APIGroups: []string{"policy"},
		Resources: []string{"poddisruptionbudgets"},
		Verbs:     allowedVerbs,
	},
	{
		APIGroups: []string{"networking.k8s.io"},
		Resources: []string{
			"ingressclasses",
			"ingresses",
			"ipaddresses",
			"networkpolicies",
			"servicecidrs",
		},
		Verbs: allowedVerbs,
	},
	{
		APIGroups: []string{"discovery.k8s.io"},
		Resources: []string{"endpointslices"},
		Verbs:     allowedVerbs,
	},
	{
		APIGroups: []string{"storage.k8s.io"},
		Resources: []string{
			"csidrivers",
			"csinodes",
			"csistoragecapacities",
			"storageclasses",
			"volumeattachments",
			"volumeattributesclasses",
		},
		Verbs: allowedVerbs,
	},
	{
		APIGroups: []string{"scheduling.k8s.io"},
		Resources: []string{"priorityclasses"},
		Verbs:     allowedVerbs,
	},
	{
		APIGroups: []string{"node.k8s.io"},
		Resources: []string{"runtimeclasses"},
		Verbs:     allowedVerbs,
	},
	{
		APIGroups: []string{"coordination.k8s.io"},
		Resources: []string{"leases"},
		Verbs:     allowedVerbs,
	},
	{
		APIGroups: []string{"certificates.k8s.io"},
		Resources: []string{"certificatesigningrequests"},
		Verbs:     allowedVerbs,
	},
	{
		APIGroups: []string{"flowcontrol.apiserver.k8s.io"},
		Resources: []string{
			"flowschemas",
			"prioritylevelconfigurations",
		},
		Verbs: allowedVerbs,
	},
	{
		// the dynamic resource allocation types
		APIGroups: []string{"resource.k8s.io"},
		Resources: []string{
			"deviceclasses",
			"resourceclaims",
			"resourceclaimtemplates",
			"resourceslices",
		},
		Verbs: allowedVerbs,
	},
	{
		APIGroups: []string{"events.k8s.io"},
		Resources: []string{"events"},
		Verbs:     allowedVerbs,
	},
	{
		// required by "kubectl top"
		APIGroups: []string{"metrics.k8s.io"},
		Resources: []string{
			"nodes",
			"pods",
		},
		Verbs: allowedVerbs,
	},
	{
		APIGroups: []string{"apiextensions.k8s.io"},
		Resources: []string{"customresourcedefinitions"},
		Verbs:     allowedVerbs,
	},
	{
		APIGroups: []string{"apiregistration.k8s.io"},
		Resources: []string{"apiservices"},
		Verbs:     allowedVerbs,
	},
	{
		APIGroups: []string{"admissionregistration.k8s.io"},
		Resources: []string{
			"mutatingadmissionpolicies",
			"mutatingadmissionpolicybindings",
			"mutatingwebhookconfigurations",
			"validatingadmissionpolicies",
			"validatingadmissionpolicybindings",
			"validatingwebhookconfigurations",
		},
		Verbs: allowedVerbs,
	},
	{
		// Reading the RBAC objects contains no credentials, but it allows to explain why a request has been rejected
		// with a "forbidden" error.
		APIGroups: []string{"rbac.authorization.k8s.io"},
		Resources: []string{
			"clusterrolebindings",
			"clusterroles",
			"rolebindings",
			"roles",
		},
		Verbs: allowedVerbs,
	},
	{
		// required by "kubectl auth can-i" and "kubectl auth can-i --list", which let the agent0-connector report its
		// own permissions.
		APIGroups: []string{"authorization.k8s.io"},
		Resources: []string{
			"selfsubjectaccessreviews",
			"selfsubjectrulesreviews",
		},
		Verbs: selfSubjectReviewVerbs,
	},
	{
		// Dash0 CRDs
		APIGroups: []string{"dash0.com"},
		Resources: []string{"dash0teams"},
		Verbs:     allowedVerbs,
	},
	{
		// Dash0 CRDs
		APIGroups: []string{"operator.dash0.com"},
		Resources: []string{
			"dash0monitorings",
			"dash0notificationchannels",
			"dash0operatorconfigurations",
			"dash0samplingrules",
			"dash0signalcontrols",
			"dash0signaltometrics",
			"dash0spamfilters",
			"dash0syntheticchecks",
			"dash0views",
		},
		Verbs: allowedVerbs,
	},
	{
		// The third-party resource types the operator itself reconciles, so that the agent0-connector can diagnose the
		// corresponding operator features.
		APIGroups: []string{"monitoring.coreos.com"},
		Resources: []string{
			"podmonitors",
			"probes",
			"prometheusrules",
			"scrapeconfigs",
			"servicemonitors",
		},
		Verbs: allowedVerbs,
	},
	{
		APIGroups: []string{"perses.dev"},
		Resources: []string{"persesdashboards"},
		Verbs:     allowedVerbs,
	},

	// Read access to non-resource URLs is required as well, like the following:
	//  - /api, /apis, /apis/<group> - API discovery
	//  - /openapi/v2, /openapi/v3 - the OpenAPI schema describing every resource type
	//  - /version - server version info
	//  - /healthz, /livez, /readyz - health endpoints
	//
	// kubectl performs API discovery on essentially every command. Before kubectl converts "get pods" into an HTTP
	// GET to /api/v1/.../pods, the client hits /api, /apis, /openapi/v3, ...
	{
		NonResourceURLs: []string{"*"},
		Verbs:           []string{"get"},
	},
}

// assembleClusterRole creates a cluster-wide, strictly read-only role. By default, it grants get & list on the
// well-known resource types listed in defaultAgent0ConnectorRbacRules plus read access to non-resource URLs, which is
// what read-only kubectl commands (kubectl get, kubectl describe, kubectl logs, ...) require for those resource types.
// The default RBAC rules can be overridden with custom rules via the Helm chart to replace the default rules. Either
// way, the role contains no verbs (update, patch, delete, deletecollection, and create only for self subject review
// API), that would allow modifying cluster state.
func assembleClusterRole(c *util.Agent0ConnectorConfig, extraConfig util.ExtraConfig) *rbacv1.ClusterRole {
	return &rbacv1.ClusterRole{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "rbac.authorization.k8s.io/v1",
			Kind:       "ClusterRole",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:   ClusterRoleName(c.NamePrefix),
			Labels: labels(),
		},
		Rules: clusterRoleRules(extraConfig),
	}
}

// clusterRoleRules returns the rules for the agent0-connector's cluster role: the custom rules configured via the Helm
// chart value operator.agent0Connector.clusterRole.rules if there are any, the default RBAC rules
// (defaultAgent0ConnectorRbacRules) otherwise.
func clusterRoleRules(extraConfig util.ExtraConfig) []rbacv1.PolicyRule {
	if len(extraConfig.Agent0ConnectorClusterRoleRules) > 0 {
		return cloneRbacRules(extraConfig.Agent0ConnectorClusterRoleRules)
	}
	return cloneRbacRules(defaultAgent0ConnectorRbacRules)
}

// validateClusterRoleRules verifies that the given custom cluster role rules grant only read access and that they
// grant read access to the non-resource URLs. It returns an error describing every violation otherwise. The Helm chart
// applies the same two checks, before the rules ever reach the cluster; the checks here also cover an extra config map
// that has been edited directly.
//
// The allowed verbs and the self subject review carve-out are duplicated in the named template
// dash0-operator.agent0ConnectorCustomClusterRoleRules in helm-chart/dash0-operator/templates/_helpers.tpl. Both need
// to be changed together: a verb that only the Helm chart rejects fails the installation, while a verb that only this
// function rejects makes the operator skip the agent0-connector without surfacing an error, see ErrMisconfigured.
func validateClusterRoleRules(rules []rbacv1.PolicyRule) error {
	if len(rules) == 0 {
		// No custom rules, the default rules apply.
		return nil
	}

	var allErrors []error
	nonResourceUrlReadAccessGranted := false
	for ruleIdx, rule := range rules {
		if len(rule.NonResourceURLs) > 0 && slices.Contains(rule.Verbs, "get") {
			nonResourceUrlReadAccessGranted = true
		}
		allowedVerbsForRule := allowedVerbs
		if grantsSelfSubjectReviewsOnly(rule) {
			allowedVerbsForRule = append(slices.Clone(allowedVerbs), selfSubjectReviewVerbs...)
		}
		for _, verb := range rule.Verbs {
			if !slices.Contains(allowedVerbsForRule, verb) {
				allErrors = append(allErrors, fmt.Errorf(
					"rules[%d]: the verb %q is not allowed, the agent0-connector's cluster role must be read-only: only "+
						"the verbs %s are allowed, plus %s for the resources %s in the API group %q",
					ruleIdx,
					verb,
					quoteAndJoin(allowedVerbs),
					quoteAndJoin(selfSubjectReviewVerbs),
					quoteAndJoin(selfSubjectReviewResources),
					selfSubjectReviewApiGroup,
				))
			}
		}
	}

	if !nonResourceUrlReadAccessGranted {
		allErrors = append(allErrors, errors.New(
			"none of the rules grants the verb \"get\" for nonResourceURLs: kubectl performs API discovery (/api, "+
				"/apis, /openapi/v3, ...) on virtually every command, so without read access to the non-resource URLs "+
				"no kubectl command would work at all",
		))
	}

	return errors.Join(allErrors...)
}

// grantsSelfSubjectReviewsOnly reports whether a rule addresses nothing but the self subject review API, which is the
// only API for which selfSubjectReviewVerbs may be granted. A rule that also addresses another API group or another
// resource type is not eligible, otherwise it could smuggle a "create" for that resource type.
func grantsSelfSubjectReviewsOnly(rule rbacv1.PolicyRule) bool {
	if len(rule.APIGroups) == 0 || len(rule.Resources) == 0 || len(rule.NonResourceURLs) > 0 {
		return false
	}
	for _, apiGroup := range rule.APIGroups {
		if apiGroup != selfSubjectReviewApiGroup {
			return false
		}
	}
	for _, resource := range rule.Resources {
		if !slices.Contains(selfSubjectReviewResources, resource) {
			return false
		}
	}
	return true
}

func quoteAndJoin(values []string) string {
	quoted := make([]string, 0, len(values))
	for _, value := range values {
		quoted = append(quoted, fmt.Sprintf("%q", value))
	}
	return strings.Join(quoted, ", ")
}

// cloneRbacRules returns a deep copy of the given rules. The rules must not be shared with the package-level slice or
// with the extra config: the Kubernetes client decodes the API server's response into the object it was given, and the
// JSON decoder writes into the existing backing arrays instead of allocating new ones. A shared rule would therefore be
// overwritten by whatever the API server returns, for every subsequent reconcile of the process.
func cloneRbacRules(source []rbacv1.PolicyRule) []rbacv1.PolicyRule {
	rules := make([]rbacv1.PolicyRule, 0, len(source))
	for _, rule := range source {
		rules = append(rules, *rule.DeepCopy())
	}
	return rules
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

func assembleDeployment(
	c *util.Agent0ConnectorConfig,
	authTokenEnvVar *corev1.EnvVar,
	extraConfig util.ExtraConfig,
) *appsv1.Deployment {
	replicas := int32(1)

	container := corev1.Container{
		Name:  containerName,
		Image: c.Images.Agent0ConnectorImage,
		Env: []corev1.EnvVar{
			{
				Name:  util.EnvVarGoMemLimit,
				Value: extraConfig.Agent0ConnectorContainerResources.GoMemLimit,
			},
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
		Resources: extraConfig.Agent0ConnectorContainerResources.ToResourceRequirements(),
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
		Tolerations:        extraConfig.Agent0ConnectorTolerations,
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

	if extraConfig.Agent0ConnectorNodeAffinity != nil {
		podSpec.Affinity = &corev1.Affinity{
			NodeAffinity: extraConfig.Agent0ConnectorNodeAffinity,
		}
	}

	return &appsv1.Deployment{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "apps/v1",
			Kind:       "Deployment",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:        DeploymentName(c.NamePrefix),
			Namespace:   c.OperatorNamespace,
			Labels:      util.MergeMaps(labels(), extraConfig.Agent0ConnectorLabels),
			Annotations: util.MergeMaps(nil, extraConfig.Agent0ConnectorAnnotations),
		},
		Spec: appsv1.DeploymentSpec{
			Replicas: &replicas,
			Selector: &metav1.LabelSelector{
				MatchLabels: deploymentMatchLabels,
			},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels:      util.MergeMaps(labels(), extraConfig.Agent0ConnectorPodLabels),
					Annotations: util.MergeMaps(nil, extraConfig.Agent0ConnectorPodAnnotations),
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
