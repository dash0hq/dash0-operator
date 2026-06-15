// SPDX-FileCopyrightText: Copyright 2026 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package a0cresources

import (
	"slices"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

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

// readOnlyVerbs is the set of verbs that are acceptable for a strictly read-only role. kubectl get/describe/logs only
// require these verbs.
var (
	readOnlyVerbs = []string{"get", "list"}

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
		desiredState := assembleDesiredState(testConfig(), authTokenEnvVar)

		Expect(desiredState).To(HaveLen(4))
		Expect(getServiceAccount(desiredState).Name).To(Equal(testNamePrefix + "-agent0-connector-sa"))
		Expect(getClusterRole(desiredState).Name).To(Equal(testNamePrefix + "-agent0-connector-cr"))
		Expect(getClusterRoleBinding(desiredState).Name).To(Equal(testNamePrefix + "-agent0-connector-crb"))
		// The deployment name must be exactly "<namePrefix>-agent0-connector".
		Expect(getDeployment(desiredState).Name).To(Equal(testNamePrefix + "-agent0-connector"))
	})

	It("deploys the namespaced resources into the operator namespace", func() {
		desiredState := assembleDesiredState(testConfig(), authTokenEnvVar)

		Expect(getServiceAccount(desiredState).Namespace).To(Equal(testOperatorNamespace))
		Expect(getDeployment(desiredState).Namespace).To(Equal(testOperatorNamespace))
	})

	It("adds the ArgoCD prune/compare annotations to all resources", func() {
		desiredState := assembleDesiredState(testConfig(), authTokenEnvVar)

		for _, wrapper := range desiredState {
			annotations := wrapper.object.GetAnnotations()
			Expect(annotations).To(HaveKeyWithValue("argocd.argoproj.io/sync-options", "Prune=false"))
			Expect(annotations).To(HaveKeyWithValue("argocd.argoproj.io/compare-options", "IgnoreExtraneous"))
		}
	})

	Describe("the cluster role", func() {
		It("grants cluster-wide read-only access and no write access", func() {
			clusterRole := getClusterRole(assembleDesiredState(testConfig(), authTokenEnvVar))

			Expect(clusterRole.Rules).ToNot(BeEmpty())
			for _, rule := range clusterRole.Rules {
				for _, verb := range rule.Verbs {
					// The only verbs that may appear in a strictly read-only role are get/list/watch. The presence
					// of any other verb (create, update, patch, delete, deletecollection, ...) would allow writes via
					// kubectl and must never happen. The watch verb is also disallowed, streaming commands are not supported.
					Expect(slices.Contains(readOnlyVerbs, verb)).To(
						BeTrue(),
						"cluster role contains non-read-only verb %q",
						verb,
					)
				}
			}
		})

		It("covers all resources in all API groups plus non-resource URLs", func() {
			clusterRole := getClusterRole(assembleDesiredState(testConfig(), authTokenEnvVar))

			hasResourceWildcard := false
			hasNonResourceURLs := false
			for _, rule := range clusterRole.Rules {
				if slices.Contains(rule.APIGroups, "*") && slices.Contains(rule.Resources, "*") {
					hasResourceWildcard = true
					Expect(rule.Verbs).To(ConsistOf("get", "list"))
				}
				if len(rule.NonResourceURLs) > 0 {
					hasNonResourceURLs = true
					Expect(rule.NonResourceURLs).To(ContainElement("*"))
					Expect(rule.Verbs).To(ConsistOf("get"))
				}
			}
			Expect(hasResourceWildcard).To(BeTrue(), "cluster role must grant get/list on */*")
			Expect(hasNonResourceURLs).To(BeTrue(), "cluster role must grant read access to non-resource URLs")
		})
	})

	It("binds the cluster role to the agent0-connector service account", func() {
		desiredState := assembleDesiredState(testConfig(), authTokenEnvVar)
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
			desiredState := assembleDesiredState(testConfig(), authTokenEnvVar)
			deployment := getDeployment(desiredState)

			Expect(*deployment.Spec.Replicas).To(Equal(int32(1)))
			Expect(deployment.Spec.Template.Spec.ServiceAccountName).To(Equal(getServiceAccount(desiredState).Name))
			Expect(deployment.Spec.Template.Spec.Containers).To(HaveLen(1))
			container := deployment.Spec.Template.Spec.Containers[0]
			Expect(container.Image).To(Equal(testImage))
			Expect(container.ImagePullPolicy).To(Equal(corev1.PullAlways))
		})

		It("passes the pseudo cluster UID as the K8S_CLUSTER_UID environment variable", func() {
			container := getDeployment(assembleDesiredState(testConfig(), authTokenEnvVar)).Spec.Template.Spec.Containers[0]
			Expect(container.Env).To(ContainElement(corev1.EnvVar{Name: "K8S_CLUSTER_UID", Value: testPseudoClusterUid}))
		})

		It("passes the server address as the DASH0_AGENT0_CONNECTOR_SERVER_ADDRESS environment variable", func() {
			container := getDeployment(assembleDesiredState(testConfig(), authTokenEnvVar)).Spec.Template.Spec.Containers[0]
			Expect(container.Env).To(ContainElement(
				corev1.EnvVar{Name: "DASH0_AGENT0_CONNECTOR_SERVER_ADDRESS", Value: Agent0ConnectorServerAddress}))
		})

		It("does not set the DASH0_AGENT0_CONNECTOR_INSECURE environment variable by default", func() {
			container := getDeployment(assembleDesiredState(testConfig(), authTokenEnvVar)).Spec.Template.Spec.Containers[0]
			for _, envVar := range container.Env {
				Expect(envVar.Name).ToNot(Equal("DASH0_AGENT0_CONNECTOR_INSECURE"))
			}
		})

		It("sets DASH0_AGENT0_CONNECTOR_INSECURE when TLS is disabled", func() {
			config := testConfig()
			config.Insecure = true
			container := getDeployment(assembleDesiredState(config, authTokenEnvVar)).Spec.Template.Spec.Containers[0]
			Expect(container.Env).To(ContainElement(
				corev1.EnvVar{Name: "DASH0_AGENT0_CONNECTOR_INSECURE", Value: "true"}))
		})

		It("does not set the DASH0_AGENT0_CONNECTOR_AUTH_TOKEN environment variable when no authorization is configured", func() {
			container := getDeployment(assembleDesiredState(testConfig(), nil)).Spec.Template.Spec.Containers[0]
			for _, envVar := range container.Env {
				Expect(envVar.Name).ToNot(Equal("DASH0_AGENT0_CONNECTOR_AUTH_TOKEN"))
			}
		})

		It("mounts a writable tmp volume for kubectl's cache", func() {
			podSpec := getDeployment(assembleDesiredState(testConfig(), authTokenEnvVar)).Spec.Template.Spec
			container := podSpec.Containers[0]
			Expect(container.Env).To(ContainElement(corev1.EnvVar{Name: "DASH0_KUBECTL_TMP", Value: "/tmp"}))
			Expect(container.VolumeMounts).To(ContainElement(corev1.VolumeMount{Name: "tmp", MountPath: "/tmp"}))
			Expect(podSpec.Volumes).To(ContainElement(corev1.Volume{
				Name:         "tmp",
				VolumeSource: corev1.VolumeSource{EmptyDir: &corev1.EmptyDirVolumeSource{}},
			}))
		})

		It("applies a restrictive container security context", func() {
			container := getDeployment(assembleDesiredState(testConfig(), authTokenEnvVar)).Spec.Template.Spec.Containers[0]
			sc := container.SecurityContext
			Expect(sc).ToNot(BeNil())
			Expect(*sc.AllowPrivilegeEscalation).To(BeFalse())
			Expect(*sc.ReadOnlyRootFilesystem).To(BeTrue())
			Expect(*sc.RunAsNonRoot).To(BeTrue())
			Expect(sc.Capabilities.Drop).To(ConsistOf(corev1.Capability("ALL")))
			Expect(sc.SeccompProfile.Type).To(Equal(corev1.SeccompProfileTypeRuntimeDefault))
		})

		It("applies a restrictive pod security context", func() {
			podSpec := getDeployment(assembleDesiredState(testConfig(), authTokenEnvVar)).Spec.Template.Spec
			sc := podSpec.SecurityContext
			Expect(sc).ToNot(BeNil())
			Expect(*sc.RunAsNonRoot).To(BeTrue())
			Expect(*sc.RunAsUser).To(Equal(int64(65532)))
			Expect(*sc.RunAsGroup).To(Equal(int64(0)))
			Expect(sc.SeccompProfile.Type).To(Equal(corev1.SeccompProfileTypeRuntimeDefault))
		})
	})
})

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
