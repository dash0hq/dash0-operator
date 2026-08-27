// SPDX-FileCopyrightText: Copyright 2026 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package a0cresources

import (
	"os"
	"path/filepath"
	"slices"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/yaml"

	"github.com/dash0hq/dash0-operator/internal/util"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	. "github.com/dash0hq/dash0-operator/test/util"
)

const (
	testOperatorNamespace = "dash0-system"
	testNamePrefix        = "dash0-operator-test"
	testImage             = "ghcr.io/dash0hq/agent0-connector:1.2.3"
	testPseudoClusterUid  = "test-cluster-uid"
)

const expectedSelfSubjectReviewApiGroup = "authorization.k8s.io"

var (
	expectedVerbs = []string{"get", "list"}

	expectedSelfSubjectReviewResources = []string{"selfsubjectaccessreviews", "selfsubjectrulesreviews"}
	expectedSelfSubjectReviewVerbs     = []string{"create"}

	authTokenEnvVar = &corev1.EnvVar{
		Name:  authTokenEnvVarName,
		Value: "dummy-token",
	}
)

func testConfig() *util.Agent0ConnectorConfig {
	return &util.Agent0ConnectorConfig{
		OperatorNamespace: testOperatorNamespace,
		NamePrefix:        testNamePrefix,
		PseudoClusterUid:  testPseudoClusterUid,
		ServerAddress:     Agent0ConnectorServerAddress,
		Images: util.Images{
			Agent0ConnectorImage:           testImage,
			Agent0ConnectorImagePullPolicy: corev1.PullAlways,
		},
	}
}

var _ = Describe("The desired state of the agent0-connector resources", func() {
	It("renders exactly the expected set of resources with the expected names", func() {
		desiredState := assembleDesiredState(testConfig(), authTokenEnvVar, util.ExtraConfig{})

		Expect(desiredState).To(HaveLen(4))
		Expect(getServiceAccount(desiredState).Name).To(Equal(testNamePrefix + "-agent0-connector-sa"))
		Expect(getClusterRole(desiredState).Name).To(Equal(testNamePrefix + "-agent0-connector-cr"))
		Expect(getClusterRoleBinding(desiredState).Name).To(Equal(testNamePrefix + "-agent0-connector-crb"))
		// The deployment name must be exactly "<namePrefix>-agent0-connector".
		Expect(getDeployment(desiredState).Name).To(Equal(testNamePrefix + "-agent0-connector"))
	})

	It("deploys the namespaced resources into the operator namespace", func() {
		desiredState := assembleDesiredState(testConfig(), authTokenEnvVar, util.ExtraConfig{})

		Expect(getServiceAccount(desiredState).Namespace).To(Equal(testOperatorNamespace))
		Expect(getDeployment(desiredState).Namespace).To(Equal(testOperatorNamespace))
	})

	It("adds the ArgoCD prune/compare annotations to all resources", func() {
		desiredState := assembleDesiredState(testConfig(), authTokenEnvVar, util.ExtraConfig{})

		for _, wrapper := range desiredState {
			annotations := wrapper.object.GetAnnotations()
			Expect(annotations).To(HaveKeyWithValue("argocd.argoproj.io/sync-options", "Prune=false"))
			Expect(annotations).To(HaveKeyWithValue("argocd.argoproj.io/compare-options", "IgnoreExtraneous"))
		}
	})

	Describe("the cluster role", func() {
		It("grants cluster-wide read-only access and no write access", func() {
			clusterRole := getClusterRole(assembleDesiredState(testConfig(), authTokenEnvVar, util.ExtraConfig{}))

			Expect(clusterRole.Rules).ToNot(BeEmpty())
			for _, rule := range clusterRole.Rules {
				expectedVerbsForRule := expectedVerbs
				if slices.Contains(rule.APIGroups, expectedSelfSubjectReviewApiGroup) {
					expectedVerbsForRule = expectedSelfSubjectReviewVerbs
				}
				for _, verb := range rule.Verbs {
					Expect(slices.Contains(expectedVerbsForRule, verb)).To(
						BeTrue(),
						"cluster role contains unexpected verb %q for API groups %v",
						verb,
						rule.APIGroups,
					)
				}
			}
		})

		It("grants the create verb exclusively for the self subject review API", func() {
			// "kubectl auth can-i" requires creating a SelfSubjectAccessReview/SelfSubjectRulesReview, which does not
			// persist an object. Every other occurrence of a write verb would allow modifying cluster state.
			clusterRole := getClusterRole(assembleDesiredState(testConfig(), authTokenEnvVar, util.ExtraConfig{}))

			selfSubjectReviewRules := 0
			for _, rule := range clusterRole.Rules {
				if !slices.Contains(rule.Verbs, "create") {
					continue
				}
				selfSubjectReviewRules++
				Expect(rule.APIGroups).To(ConsistOf(expectedSelfSubjectReviewApiGroup))
				Expect(rule.Resources).To(ConsistOf(expectedSelfSubjectReviewResources))
				Expect(rule.Verbs).To(ConsistOf("create"))
			}
			Expect(selfSubjectReviewRules).To(
				Equal(1),
				"exactly one rule may grant the create verb, for the self subject review API",
			)
		})

		It("grants read access to well-known resource types and to non-resource URLs", func() {
			clusterRole := getClusterRole(assembleDesiredState(testConfig(), authTokenEnvVar, util.ExtraConfig{}))

			resourcesPerApiGroup := make(map[string][]string)
			hasNonResourceURLs := false
			for _, rule := range clusterRole.Rules {
				for _, apiGroup := range rule.APIGroups {
					resourcesPerApiGroup[apiGroup] = append(resourcesPerApiGroup[apiGroup], rule.Resources...)
				}
				if len(rule.NonResourceURLs) > 0 {
					hasNonResourceURLs = true
					Expect(rule.NonResourceURLs).To(ContainElement("*"))
					Expect(rule.Verbs).To(ConsistOf("get"))
				}
			}

			Expect(resourcesPerApiGroup[""]).To(ContainElements(
				"endpoints",
				"events",
				"namespaces",
				"nodes",
				"persistentvolumeclaims",
				"persistentvolumes",
				"pods",
				"pods/log",
				"services",
			))
			Expect(resourcesPerApiGroup["apps"]).To(ContainElements("daemonsets", "deployments", "replicasets", "statefulsets"))
			Expect(resourcesPerApiGroup["batch"]).To(ContainElements("cronjobs", "jobs"))
			Expect(resourcesPerApiGroup["metrics.k8s.io"]).To(ContainElements("nodes", "pods"))
			Expect(resourcesPerApiGroup["apiextensions.k8s.io"]).To(ContainElement("customresourcedefinitions"))
			Expect(resourcesPerApiGroup["rbac.authorization.k8s.io"]).To(ContainElements("clusterroles", "roles"))
			Expect(resourcesPerApiGroup["certificates.k8s.io"]).To(ContainElement("certificatesigningrequests"))
			Expect(resourcesPerApiGroup["flowcontrol.apiserver.k8s.io"]).To(ContainElements("flowschemas"))
			Expect(resourcesPerApiGroup["resource.k8s.io"]).To(ContainElements("deviceclasses", "resourceclaims"))
			Expect(resourcesPerApiGroup["monitoring.coreos.com"]).To(ContainElements("prometheusrules", "scrapeconfigs"))
			Expect(resourcesPerApiGroup["perses.dev"]).To(ContainElement("persesdashboards"))
			Expect(hasNonResourceURLs).To(BeTrue(), "cluster role must grant read access to non-resource URLs")
		})

		It("grants read access to every Dash0 custom resource type", func() {
			// The expected resource types are read from the generated custom resource definitions instead of being
			// listed here, so that a new Dash0 CRD which has not been added to defaultAgent0ConnectorRbacRules fails this
			// test. Without the rule the agent0-connector cannot read the new resource type at all.
			clusterRole := getClusterRole(assembleDesiredState(testConfig(), authTokenEnvVar, util.ExtraConfig{}))

			for _, crd := range readDash0CustomResourceDefinitions() {
				Expect(slices.ContainsFunc(clusterRole.Rules, func(rule rbacv1.PolicyRule) bool {
					return slices.Contains(rule.APIGroups, crd.apiGroup) &&
						slices.Contains(rule.Resources, crd.plural) &&
						slices.Contains(rule.Verbs, "get") &&
						slices.Contains(rule.Verbs, "list")
				})).To(
					BeTrue(),
					"cluster role must grant get & list on %s.%s; add the resource type to "+
						"defaultAgent0ConnectorRbacRules and to the -manager-agent0-connector-ro cluster role of the Helm chart",
					crd.plural,
					crd.apiGroup,
				)
			}
		})

		It("does not grant access to secrets, config maps, or to any wildcard", func() {
			clusterRole := getClusterRole(assembleDesiredState(testConfig(), authTokenEnvVar, util.ExtraConfig{}))

			for _, rule := range clusterRole.Rules {
				Expect(rule.APIGroups).ToNot(
					ContainElement("*"),
					"cluster role must not grant access to all API groups",
				)
				Expect(rule.Resources).ToNot(
					ContainElement("*"),
					"cluster role must not grant access to all resource types of API group(s) %v",
					rule.APIGroups,
				)
				if slices.Contains(rule.APIGroups, "") {
					Expect(rule.Resources).ToNot(ContainElement("secrets"))
					Expect(rule.Resources).ToNot(ContainElement("configmaps"))
				}
			}
		})

		Describe("with custom rules from the Helm chart", func() {
			It("replaces the default rules instead of extending them", func() {
				customRules := []rbacv1.PolicyRule{
					{
						APIGroups: []string{""},
						Resources: []string{"configmaps", "pods"},
						Verbs:     []string{"get", "list"},
					},
					{
						NonResourceURLs: []string{"*"},
						Verbs:           []string{"get"},
					},
				}

				clusterRole := getClusterRole(assembleDesiredState(testConfig(), authTokenEnvVar, util.ExtraConfig{
					Agent0ConnectorClusterRoleRules: customRules,
				}))

				Expect(clusterRole.Rules).To(HaveLen(len(customRules)))
				Expect(clusterRole.Rules[0].Resources).To(ConsistOf("configmaps", "pods"))
				// A resource type of the default rules which the custom rules do not list must not be granted.
				for _, rule := range clusterRole.Rules {
					Expect(rule.Resources).ToNot(ContainElement("nodes"))
				}
			})

			It("falls back to the default rules when the custom rule list is empty", func() {
				clusterRole := getClusterRole(assembleDesiredState(testConfig(), authTokenEnvVar, util.ExtraConfig{
					Agent0ConnectorClusterRoleRules: []rbacv1.PolicyRule{},
				}))

				Expect(clusterRole.Rules).To(HaveLen(len(defaultAgent0ConnectorRbacRules)))
			})

			It("does not share the custom rules with the extra config", func() {
				// The extra config outlives the reconcile that reads it, so the assembled cluster role must not alias its
				// rules; see cloneRbacRules.
				extraConfig := util.ExtraConfig{
					Agent0ConnectorClusterRoleRules: []rbacv1.PolicyRule{
						{
							APIGroups: []string{""},
							Resources: []string{"pods"},
							Verbs:     []string{"get", "list"},
						},
					},
				}

				clusterRole := getClusterRole(assembleDesiredState(testConfig(), authTokenEnvVar, extraConfig))
				clusterRole.Rules[0].APIGroups[0] = "modified"
				clusterRole.Rules[0].Resources[0] = "modified"
				clusterRole.Rules[0].Verbs[0] = "modified"

				Expect(extraConfig.Agent0ConnectorClusterRoleRules[0].APIGroups).To(ConsistOf(""))
				Expect(extraConfig.Agent0ConnectorClusterRoleRules[0].Resources).To(ConsistOf("pods"))
				Expect(extraConfig.Agent0ConnectorClusterRoleRules[0].Verbs).To(ConsistOf("get", "list"))
			})
		})

		Describe("validating custom rules", func() {
			It("accepts an empty rule list", func() {
				Expect(validateClusterRoleRules(nil)).To(Succeed())
			})

			It("accepts read-only rules", func() {
				Expect(validateClusterRoleRules([]rbacv1.PolicyRule{
					{
						APIGroups: []string{"", "apps"},
						Resources: []string{"pods", "deployments"},
						Verbs:     []string{"get", "list"},
					},
					{
						NonResourceURLs: []string{"*"},
						Verbs:           []string{"get"},
					},
				})).To(Succeed())
			})

			It("accepts the default rules of the operator", func() {
				Expect(validateClusterRoleRules(defaultAgent0ConnectorRbacRules)).To(Succeed())
			})

			It("accepts create for the self subject review API", func() {
				Expect(validateClusterRoleRules([]rbacv1.PolicyRule{
					{
						APIGroups: []string{expectedSelfSubjectReviewApiGroup},
						Resources: expectedSelfSubjectReviewResources,
						Verbs:     []string{"create"},
					},
				})).To(Succeed())
			})

			DescribeTable(
				"rejects a rule which grants more than read-only access",
				func(rule rbacv1.PolicyRule, expectedVerbInMessage string) {
					err := validateClusterRoleRules([]rbacv1.PolicyRule{rule})

					Expect(err).To(HaveOccurred())
					Expect(err.Error()).To(ContainSubstring("rules[0]"))
					Expect(err.Error()).To(ContainSubstring(expectedVerbInMessage))
				},
				Entry("a write verb", rbacv1.PolicyRule{
					APIGroups: []string{""},
					Resources: []string{"pods"},
					Verbs:     []string{"get", "list", "delete"},
				}, `the verb "delete" is not allowed`),
				Entry("the wildcard verb", rbacv1.PolicyRule{
					APIGroups: []string{"*"},
					Resources: []string{"*"},
					Verbs:     []string{"*"},
				}, `the verb "*" is not allowed`),
				Entry("the escalate verb", rbacv1.PolicyRule{
					APIGroups: []string{"rbac.authorization.k8s.io"},
					Resources: []string{"clusterroles"},
					Verbs:     []string{"escalate"},
				}, `the verb "escalate" is not allowed`),
				Entry("the watch verb, since streaming commands are not supported", rbacv1.PolicyRule{
					APIGroups: []string{""},
					Resources: []string{"pods"},
					Verbs:     []string{"get", "list", "watch"},
				}, `the verb "watch" is not allowed`),
				Entry("create for a resource type which is not a self subject review", rbacv1.PolicyRule{
					APIGroups: []string{expectedSelfSubjectReviewApiGroup},
					Resources: []string{"subjectaccessreviews"},
					Verbs:     []string{"create"},
				}, `the verb "create" is not allowed`),
				Entry("create for a rule which also addresses another API group", rbacv1.PolicyRule{
					APIGroups: []string{expectedSelfSubjectReviewApiGroup, ""},
					Resources: []string{"selfsubjectaccessreviews", "selfsubjectrulesreviews", "pods"},
					Verbs:     []string{"create"},
				}, `the verb "create" is not allowed`),
				Entry("create for non-resource URLs", rbacv1.PolicyRule{
					NonResourceURLs: []string{"*"},
					Verbs:           []string{"create"},
				}, `the verb "create" is not allowed`),
			)

			It("reports every violation instead of only the first one", func() {
				err := validateClusterRoleRules([]rbacv1.PolicyRule{
					{
						APIGroups: []string{""},
						Resources: []string{"pods"},
						Verbs:     []string{"get", "patch"},
					},
					{
						APIGroups: []string{"apps"},
						Resources: []string{"deployments"},
						Verbs:     []string{"get", "list"},
					},
					{
						APIGroups: []string{"apps"},
						Resources: []string{"daemonsets"},
						Verbs:     []string{"deletecollection"},
					},
				})

				Expect(err).To(HaveOccurred())
				Expect(err.Error()).To(ContainSubstring(`rules[0]: the verb "patch"`))
				Expect(err.Error()).To(ContainSubstring(`rules[2]: the verb "deletecollection"`))
				Expect(err.Error()).ToNot(ContainSubstring("rules[1]"))
			})
		})

		It("drift protection: the default rules of the Helm chart are in sync with desired_state.go", func() {
			// The Helm chart has its own copy of the default rules for the -manager-agent0-connector-ro cluster
			// role.
			Expect(clusterRoleRules(util.ExtraConfig{})).To(
				ConsistOf(readDefaultClusterRoleRulesOfHelmChart()),
				"the default rules of the operator and of the Helm chart have diverged; reconcile "+
					"defaultAgent0ConnectorRbacRules with %s",
				defaultClusterRoleRulesOfHelmChart,
			)
		})

		It("does not share the rules with other cluster role instances", func() {
			// The Kubernetes client decodes the API server's response into the object it was given, and the JSON
			// decoder reuses the existing backing arrays. Sharing the package-level rules would let such a response
			// corrupt the desired state of every subsequent reconcile.
			clusterRole := getClusterRole(assembleDesiredState(testConfig(), authTokenEnvVar, util.ExtraConfig{}))
			clusterRole.Rules[0].APIGroups[0] = "modified"
			clusterRole.Rules[0].Resources[0] = "modified"
			clusterRole.Rules[0].Verbs[0] = "modified"

			secondClusterRole := getClusterRole(assembleDesiredState(testConfig(), authTokenEnvVar, util.ExtraConfig{}))
			Expect(secondClusterRole.Rules[0].APIGroups).ToNot(ContainElement("modified"))
			Expect(secondClusterRole.Rules[0].Resources).ToNot(ContainElement("modified"))
			for _, rule := range secondClusterRole.Rules {
				Expect(rule.Verbs).ToNot(ContainElement("modified"))
			}
		})
	})

	It("binds the cluster role to the agent0-connector service account", func() {
		desiredState := assembleDesiredState(testConfig(), authTokenEnvVar, util.ExtraConfig{})
		clusterRoleBinding := getClusterRoleBinding(desiredState)

		Expect(clusterRoleBinding.RoleRef.Kind).To(Equal("ClusterRole"))
		Expect(clusterRoleBinding.RoleRef.Name).To(Equal(getClusterRole(desiredState).Name))
		Expect(clusterRoleBinding.Subjects).To(HaveLen(1))
		Expect(clusterRoleBinding.Subjects[0].Kind).To(Equal("ServiceAccount"))
		Expect(clusterRoleBinding.Subjects[0].Name).To(Equal(getServiceAccount(desiredState).Name))
		Expect(clusterRoleBinding.Subjects[0].Namespace).To(Equal(testOperatorNamespace))
	})

	Describe("the deployment", func() {
		It("uses the configured image, pull policy, and service account", func() {
			desiredState := assembleDesiredState(testConfig(), authTokenEnvVar, util.ExtraConfig{})
			deployment := getDeployment(desiredState)

			Expect(*deployment.Spec.Replicas).To(Equal(int32(1)))
			Expect(deployment.Spec.Template.Spec.ServiceAccountName).To(Equal(getServiceAccount(desiredState).Name))
			Expect(deployment.Spec.Template.Spec.Containers).To(HaveLen(1))
			container := deployment.Spec.Template.Spec.Containers[0]
			Expect(container.Image).To(Equal(testImage))
			Expect(container.ImagePullPolicy).To(Equal(corev1.PullAlways))
		})

		It("passes the pseudo cluster UID as the K8S_CLUSTER_UID environment variable", func() {
			container := getDeployment(assembleDesiredState(testConfig(), authTokenEnvVar, util.ExtraConfig{})).Spec.Template.Spec.Containers[0]
			Expect(container.Env).To(ContainElement(corev1.EnvVar{Name: "K8S_CLUSTER_UID", Value: testPseudoClusterUid}))
		})

		It("passes the server address as the DASH0_AGENT0_CONNECTOR_SERVER_ADDRESS environment variable", func() {
			container := getDeployment(assembleDesiredState(testConfig(), authTokenEnvVar, util.ExtraConfig{})).Spec.Template.Spec.Containers[0]
			Expect(container.Env).To(ContainElement(
				corev1.EnvVar{Name: "DASH0_AGENT0_CONNECTOR_SERVER_ADDRESS", Value: Agent0ConnectorServerAddress}))
		})

		It("does not set the DASH0_AGENT0_CONNECTOR_INSECURE environment variable by default", func() {
			container := getDeployment(assembleDesiredState(testConfig(), authTokenEnvVar, util.ExtraConfig{})).Spec.Template.Spec.Containers[0]
			for _, envVar := range container.Env {
				Expect(envVar.Name).ToNot(Equal("DASH0_AGENT0_CONNECTOR_INSECURE"))
			}
		})

		It("sets DASH0_AGENT0_CONNECTOR_INSECURE when TLS is disabled", func() {
			config := testConfig()
			config.Insecure = true
			container := getDeployment(assembleDesiredState(config, authTokenEnvVar, util.ExtraConfig{})).Spec.Template.Spec.Containers[0]
			Expect(container.Env).To(ContainElement(
				corev1.EnvVar{Name: "DASH0_AGENT0_CONNECTOR_INSECURE", Value: "true"}))
		})

		It("does not set the DASH0_AGENT0_CONNECTOR_AUTH_TOKEN environment variable when no authorization is configured", func() {
			container := getDeployment(assembleDesiredState(testConfig(), nil, util.ExtraConfig{})).Spec.Template.Spec.Containers[0]
			for _, envVar := range container.Env {
				Expect(envVar.Name).ToNot(Equal("DASH0_AGENT0_CONNECTOR_AUTH_TOKEN"))
			}
		})

		It("mounts a writable tmp volume for kubectl's cache", func() {
			podSpec := getDeployment(assembleDesiredState(testConfig(), authTokenEnvVar, util.ExtraConfig{})).Spec.Template.Spec
			container := podSpec.Containers[0]
			Expect(container.Env).To(ContainElement(corev1.EnvVar{Name: "DASH0_KUBECTL_TMP", Value: "/tmp"}))
			Expect(container.VolumeMounts).To(ContainElement(corev1.VolumeMount{Name: "tmp", MountPath: "/tmp"}))
			Expect(podSpec.Volumes).To(ContainElement(corev1.Volume{
				Name:         "tmp",
				VolumeSource: corev1.VolumeSource{EmptyDir: &corev1.EmptyDirVolumeSource{}},
			}))
		})

		It("applies a restrictive container security context", func() {
			container := getDeployment(assembleDesiredState(testConfig(), authTokenEnvVar, util.ExtraConfig{})).Spec.Template.Spec.Containers[0]
			sc := container.SecurityContext
			Expect(sc).ToNot(BeNil())
			Expect(*sc.AllowPrivilegeEscalation).To(BeFalse())
			Expect(*sc.ReadOnlyRootFilesystem).To(BeTrue())
			Expect(*sc.RunAsNonRoot).To(BeTrue())
			Expect(sc.Capabilities.Drop).To(ConsistOf(corev1.Capability("ALL")))
			Expect(sc.SeccompProfile.Type).To(Equal(corev1.SeccompProfileTypeRuntimeDefault))
		})

		It("applies a restrictive pod security context", func() {
			podSpec := getDeployment(assembleDesiredState(testConfig(), authTokenEnvVar, util.ExtraConfig{})).Spec.Template.Spec
			sc := podSpec.SecurityContext
			Expect(sc).ToNot(BeNil())
			Expect(*sc.RunAsNonRoot).To(BeTrue())
			Expect(*sc.RunAsUser).To(Equal(int64(65532)))
			Expect(*sc.RunAsGroup).To(Equal(int64(0)))
			Expect(sc.SeccompProfile.Type).To(Equal(corev1.SeccompProfileTypeRuntimeDefault))
		})

		It("renders additional labels and annotations on the workload and the pods", func() {
			deployment := getDeployment(assembleDesiredState(testConfig(), authTokenEnvVar, util.ExtraConfig{
				Agent0ConnectorLabels:         map[string]string{"a0c-label": "a0c-label-value"},
				Agent0ConnectorAnnotations:    map[string]string{"a0c-annotation": "a0c-annotation-value"},
				Agent0ConnectorPodLabels:      map[string]string{"a0c-pod-label": "a0c-pod-label-value"},
				Agent0ConnectorPodAnnotations: map[string]string{"a0c-pod-annotation": "a0c-pod-annotation-value"},
			}))

			Expect(deployment.ObjectMeta.Labels).To(HaveKeyWithValue("a0c-label", "a0c-label-value"))
			// operator-managed labels are still present
			Expect(deployment.ObjectMeta.Labels).To(HaveKeyWithValue(util.AppKubernetesIoNameLabel, appKubernetesIoNameValue))
			Expect(deployment.ObjectMeta.Annotations).To(HaveKeyWithValue("a0c-annotation", "a0c-annotation-value"))
			Expect(deployment.Spec.Template.Labels).To(HaveKeyWithValue("a0c-pod-label", "a0c-pod-label-value"))
			Expect(deployment.Spec.Template.Labels).To(HaveKeyWithValue(util.AppKubernetesIoNameLabel, appKubernetesIoNameValue))
			Expect(deployment.Spec.Template.Annotations).To(HaveKeyWithValue("a0c-pod-annotation", "a0c-pod-annotation-value"))
		})

		It("renders the container resources and GOMEMLIMIT from the extra config", func() {
			extraConfig := util.ExtraConfig{
				Agent0ConnectorContainerResources: util.ResourceRequirementsWithGoMemLimit{
					Requests: corev1.ResourceList{
						corev1.ResourceMemory: resource.MustParse("32Mi"),
					},
					Limits: corev1.ResourceList{
						corev1.ResourceCPU:    resource.MustParse("500m"),
						corev1.ResourceMemory: resource.MustParse("256Mi"),
					},
					GoMemLimit: "128MiB",
				},
			}

			container := getDeployment(assembleDesiredState(testConfig(), authTokenEnvVar, extraConfig)).Spec.Template.Spec.Containers[0]

			Expect(container.Resources.Requests).To(HaveLen(1))
			Expect(container.Resources.Requests.Memory().String()).To(Equal("32Mi"))
			Expect(container.Resources.Limits).To(HaveLen(2))
			Expect(container.Resources.Limits.Cpu().String()).To(Equal("500m"))
			Expect(container.Resources.Limits.Memory().String()).To(Equal("256Mi"))
			Expect(container.Env).To(ContainElement(corev1.EnvVar{Name: "GOMEMLIMIT", Value: "128MiB"}))
		})

		It("renders tolerations and node affinity from the extra config", func() {
			extraConfig := util.ExtraConfig{
				Agent0ConnectorTolerations: []corev1.Toleration{
					{
						Key:      "agent0-connector-key",
						Operator: corev1.TolerationOpEqual,
						Value:    "agent0-connector-value",
						Effect:   corev1.TaintEffectNoSchedule,
					},
				},
				Agent0ConnectorNodeAffinity: &corev1.NodeAffinity{
					RequiredDuringSchedulingIgnoredDuringExecution: &corev1.NodeSelector{
						NodeSelectorTerms: []corev1.NodeSelectorTerm{
							{
								MatchExpressions: []corev1.NodeSelectorRequirement{
									{
										Key:      "dash0.com/enable",
										Operator: corev1.NodeSelectorOpNotIn,
										Values:   []string{"false"},
									},
								},
							},
						},
					},
				},
			}

			podSpec := getDeployment(assembleDesiredState(testConfig(), authTokenEnvVar, extraConfig)).Spec.Template.Spec

			Expect(podSpec.Tolerations).To(HaveLen(1))
			Expect(podSpec.Tolerations[0].Key).To(Equal("agent0-connector-key"))
			Expect(podSpec.Tolerations[0].Operator).To(Equal(corev1.TolerationOpEqual))
			Expect(podSpec.Tolerations[0].Value).To(Equal("agent0-connector-value"))
			Expect(podSpec.Tolerations[0].Effect).To(Equal(corev1.TaintEffectNoSchedule))

			Expect(podSpec.Affinity).ToNot(BeNil())
			Expect(podSpec.Affinity.NodeAffinity).To(Equal(extraConfig.Agent0ConnectorNodeAffinity))
		})

		It("leaves tolerations and affinity unset when the extra config has neither", func() {
			podSpec := getDeployment(assembleDesiredState(testConfig(), authTokenEnvVar, util.ExtraConfig{})).Spec.Template.Spec

			Expect(podSpec.Tolerations).To(BeEmpty())
			Expect(podSpec.Affinity).To(BeNil())
		})

		It("does not let additional labels override operator-managed labels", func() {
			deployment := getDeployment(assembleDesiredState(testConfig(), authTokenEnvVar, util.ExtraConfig{
				Agent0ConnectorLabels:    map[string]string{util.AppKubernetesIoNameLabel: "custom-value"},
				Agent0ConnectorPodLabels: map[string]string{util.AppKubernetesIoNameLabel: "custom-value"},
			}))

			// the operator-managed value wins over the additional label
			Expect(deployment.ObjectMeta.Labels).To(HaveKeyWithValue(util.AppKubernetesIoNameLabel, appKubernetesIoNameValue))
			Expect(deployment.Spec.Template.Labels).To(HaveKeyWithValue(util.AppKubernetesIoNameLabel, appKubernetesIoNameValue))
		})
	})
})

// defaultClusterRoleRulesOfHelmChart is the file from which the Helm chart renders the default rules of the
// agent0-connector's cluster role, relative to the directory of this package.
var defaultClusterRoleRulesOfHelmChart = filepath.Join(
	"..",
	"..",
	"..",
	"helm-chart",
	"dash0-operator",
	"files",
	"agent0-connector-default-cluster-role-rules.yaml",
)

// readDefaultClusterRoleRulesOfHelmChart parses the default cluster role rules of the Helm chart.
func readDefaultClusterRoleRulesOfHelmChart() []rbacv1.PolicyRule {
	GinkgoHelper()
	content, err := os.ReadFile(defaultClusterRoleRulesOfHelmChart)
	Expect(err).ToNot(HaveOccurred())
	var rules []rbacv1.PolicyRule
	Expect(yaml.UnmarshalStrict(content, &rules)).To(Succeed())
	Expect(rules).ToNot(BeEmpty(), "%s contains no rules", defaultClusterRoleRulesOfHelmChart)
	return rules
}

// readDash0CustomResourceDefinitions returns the API group and the plural name of every Dash0 custom resource type,
// read from the custom resource definitions generated from the Go types in api (via "make manifests").
func readDash0CustomResourceDefinitions() []dash0CustomResourceType {
	GinkgoHelper()
	manifests, err := filepath.Glob(filepath.Join("..", "..", "..", "config", "crd", "bases", "*.yaml"))
	Expect(err).ToNot(HaveOccurred())
	Expect(manifests).ToNot(BeEmpty(), "found no custom resource definition manifests")

	crds := make([]dash0CustomResourceType, 0, len(manifests))
	for _, manifest := range manifests {
		content, err := os.ReadFile(manifest)
		Expect(err).ToNot(HaveOccurred())
		var crd customResourceDefinition
		Expect(yaml.Unmarshal(content, &crd)).To(Succeed())
		Expect(crd.Spec.Group).ToNot(BeEmpty(), "%s declares no API group", manifest)
		Expect(crd.Spec.Names.Plural).ToNot(BeEmpty(), "%s declares no plural name", manifest)
		crds = append(crds, dash0CustomResourceType{apiGroup: crd.Spec.Group, plural: crd.Spec.Names.Plural})
	}
	return crds
}

type dash0CustomResourceType struct {
	apiGroup string
	plural   string
}

// customResourceDefinition covers the attributes of a CustomResourceDefinition manifest this test needs.
type customResourceDefinition struct {
	Spec struct {
		Group string `json:"group"`
		Names struct {
			Plural string `json:"plural"`
		} `json:"names"`
	} `json:"spec"`
}

func findObject[T client.Object](desiredState []clientObject) T {
	GinkgoHelper()
	for _, wrapper := range desiredState {
		if typed, ok := wrapper.object.(T); ok {
			return typed
		}
	}
	Fail("could not find the expected object in the desired state")
	var zero T
	return zero
}

func getServiceAccount(desiredState []clientObject) *corev1.ServiceAccount {
	return findObject[*corev1.ServiceAccount](desiredState)
}

func getClusterRole(desiredState []clientObject) *rbacv1.ClusterRole {
	return findObject[*rbacv1.ClusterRole](desiredState)
}

func getClusterRoleBinding(desiredState []clientObject) *rbacv1.ClusterRoleBinding {
	return findObject[*rbacv1.ClusterRoleBinding](desiredState)
}

func getDeployment(desiredState []clientObject) *appsv1.Deployment {
	return findObject[*appsv1.Deployment](desiredState)
}
