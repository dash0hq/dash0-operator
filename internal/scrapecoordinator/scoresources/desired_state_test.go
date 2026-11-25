// SPDX-FileCopyrightText: Copyright 2026 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package scoresources

import (
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	"k8s.io/apimachinery/pkg/util/intstr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/yaml"

	"github.com/dash0hq/dash0-operator/internal/selfmonitoringapiaccess"
	"github.com/dash0hq/dash0-operator/internal/util"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	. "github.com/dash0hq/dash0-operator/test/util"
)

const collectorComponent = "agent-collector"

var _ = Describe("The desired state of the scrape coordinator resources", func() {
	config := &util.ScrapeCoordinatorConfig{
		Images:             TestImages,
		OperatorNamespace:  OperatorNamespace,
		NamePrefix:         ManagedResourceNamePrefixTest,
		CollectorComponent: collectorComponent,
		PseudoClusterUid:   "cluster-uid",
	}

	assemble := func(
		namespaces []string,
		extraConfig util.ExtraConfig,
	) []clientObject {
		desiredState, err := assembleDesiredState(config, namespaces, extraConfig, selfMonitoringInput{})
		Expect(err).ToNot(HaveOccurred())
		return desiredState
	}

	It("should render all resources with the expected names", func() {
		desiredState := assemble([]string{"namespace-a"}, util.ExtraConfigDefaults)
		names := make([]string, 0, len(desiredState))
		for _, object := range desiredState {
			names = append(names, object.object.GetName())
		}
		Expect(names).To(ConsistOf(
			ServiceAccountName(ManagedResourceNamePrefixTest),
			ConfigMapName(ManagedResourceNamePrefixTest),
			ClusterRoleName(ManagedResourceNamePrefixTest),
			ClusterRoleBindingName(ManagedResourceNamePrefixTest),
			ServiceName(ManagedResourceNamePrefixTest),
			DeploymentName(ManagedResourceNamePrefixTest),
		))
	})

	It("should render the configuration file", func() {
		desiredState := assemble([]string{"namespace-b", "namespace-a", "namespace-b"}, util.ExtraConfigDefaults)
		cfg := parseConfig(getConfigMap(desiredState))

		Expect(cfg.CollectorNamespace).To(Equal(OperatorNamespace))
		Expect(cfg.Pools.PerNode.Selector.MatchLabels).To(
			Equal(map[string]string{util.AppKubernetesIoComponentLabel: collectorComponent}))
		Expect(cfg.PrometheusCRs.Namespaces).To(Equal([]string{"namespace-a", "namespace-b"}))
		Expect(cfg.PrometheusCRs.SecretNamespaces).To(BeEmpty())
		Expect(cfg.Server.ListenAddr).To(Equal(":8080"))
		Expect(cfg.Server.TLS).To(BeNil())
		Expect(cfg.Server.AllowInsecureAuthSecrets).To(BeFalse())
	})

	It("should restart the deployment when the configuration changes", func() {
		annotationA := getDeployment(assemble([]string{"namespace-a"}, util.ExtraConfigDefaults)).
			Spec.Template.Annotations[configHashAnnotation]
		annotationAAgain := getDeployment(assemble([]string{"namespace-a"}, util.ExtraConfigDefaults)).
			Spec.Template.Annotations[configHashAnnotation]
		annotationB := getDeployment(assemble([]string{"namespace-a", "namespace-b"}, util.ExtraConfigDefaults)).
			Spec.Template.Annotations[configHashAnnotation]

		Expect(annotationA).ToNot(BeEmpty())
		Expect(annotationAAgain).To(Equal(annotationA))
		Expect(annotationB).ToNot(Equal(annotationA))
	})

	It("should render the deployment", func() {
		deployment := getDeployment(assemble([]string{"namespace-a"}, util.ExtraConfigDefaults))

		Expect(*deployment.Spec.Replicas).To(Equal(int32(1)))
		podSpec := deployment.Spec.Template.Spec
		Expect(podSpec.ServiceAccountName).To(Equal(ServiceAccountName(ManagedResourceNamePrefixTest)))
		Expect(podSpec.Containers).To(HaveLen(1))
		container := podSpec.Containers[0]
		Expect(container.Image).To(Equal(ScrapeCoordinatorImageTest))
		Expect(container.ImagePullPolicy).To(Equal(corev1.PullAlways))
		Expect(container.Args).To(Equal([]string{"--config=/etc/scrape-coordinator/config.yaml"}))
		Expect(container.Ports).To(ConsistOf(MatchContainerPort("http", 8080)))
		Expect(container.VolumeMounts).To(ContainElement(MatchVolumeMount(configVolumeName, configDir)))
		Expect(container.LivenessProbe.HTTPGet.Path).To(Equal("/livez"))
		Expect(container.ReadinessProbe.HTTPGet.Path).To(Equal("/readyz"))
		Expect(*container.SecurityContext.ReadOnlyRootFilesystem).To(BeTrue())
		Expect(container.Env).To(ContainElement(MatchEnvVar("K8S_DEPLOYMENT_NAME",
			DeploymentName(ManagedResourceNamePrefixTest))))
		Expect(*podSpec.SecurityContext.RunAsUser).To(Equal(defaultUser))
		Expect(*podSpec.SecurityContext.RunAsGroup).To(Equal(defaultGroup))

		service := getService(assemble([]string{"namespace-a"}, util.ExtraConfigDefaults))
		Expect(service.Spec.Ports).To(ConsistOf(MatchServicePort("http", 80, intstr.FromString("http"))))
		Expect(service.Spec.Selector).To(Equal(deployment.Spec.Selector.MatchLabels))
		Expect(deployment.Spec.Template.Labels).To(HaveKeyWithValue(
			util.AppKubernetesIoNameLabel, appKubernetesIoNameValue))
	})

	It("omits the pod-level runAsUser on OpenShift", func() {
		openShiftConfig := *config
		openShiftConfig.IsOpenShift = true
		desiredState, err := assembleDesiredState(&openShiftConfig, nil, util.ExtraConfigDefaults, selfMonitoringInput{})
		Expect(err).ToNot(HaveOccurred())

		securityContext := getDeployment(desiredState).Spec.Template.Spec.SecurityContext
		Expect(securityContext.RunAsUser).To(BeNil())
		Expect(*securityContext.RunAsGroup).To(Equal(defaultGroup))
	})

	It("should render custom resource requirements, tolerations, node affinity, labels and annotations", func() {
		nodeAffinity := &corev1.NodeAffinity{
			RequiredDuringSchedulingIgnoredDuringExecution: &corev1.NodeSelector{
				NodeSelectorTerms: []corev1.NodeSelectorTerm{{
					MatchExpressions: []corev1.NodeSelectorRequirement{{
						Key:      "affinity-key",
						Operator: corev1.NodeSelectorOpIn,
						Values:   []string{"affinity-value"},
					}},
				}},
			},
		}
		deployment := getDeployment(assemble(nil, util.ExtraConfig{
			ScrapeCoordinatorContainerResources: util.ResourceRequirementsWithGoMemLimit{
				Limits: corev1.ResourceList{
					corev1.ResourceCPU:    resource.MustParse("500m"),
					corev1.ResourceMemory: resource.MustParse("1Gi"),
				},
				GoMemLimit: "800MiB",
				Requests: corev1.ResourceList{
					corev1.ResourceCPU:    resource.MustParse("200m"),
					corev1.ResourceMemory: resource.MustParse("500Mi"),
				},
			},
			ScrapeCoordinatorTolerations: []corev1.Toleration{{
				Key:      "key",
				Operator: corev1.TolerationOpExists,
				Effect:   corev1.TaintEffectNoSchedule,
			}},
			ScrapeCoordinatorNodeAffinity:   nodeAffinity,
			ScrapeCoordinatorLabels:         map[string]string{"label": "label-value"},
			ScrapeCoordinatorAnnotations:    map[string]string{"annotation": "annotation-value"},
			ScrapeCoordinatorPodLabels:      map[string]string{util.AppKubernetesIoNameLabel: "custom-value"},
			ScrapeCoordinatorPodAnnotations: map[string]string{"pod-annotation": "pod-annotation-value"},
		}))

		container := deployment.Spec.Template.Spec.Containers[0]
		Expect(container.Resources.Limits.Cpu().String()).To(Equal("500m"))
		Expect(container.Resources.Limits.Memory().String()).To(Equal("1Gi"))
		Expect(container.Resources.Requests.Cpu().String()).To(Equal("200m"))
		Expect(container.Resources.Requests.Memory().String()).To(Equal("500Mi"))
		Expect(container.Env).To(ContainElement(MatchEnvVar(util.EnvVarGoMemLimit, "800MiB")))

		podSpec := deployment.Spec.Template.Spec
		Expect(podSpec.Tolerations).To(HaveLen(1))
		Expect(podSpec.Tolerations[0].Key).To(Equal("key"))
		Expect(podSpec.Affinity.NodeAffinity).To(Equal(nodeAffinity))

		Expect(deployment.Labels).To(HaveKeyWithValue("label", "label-value"))
		Expect(deployment.Annotations).To(HaveKeyWithValue("annotation", "annotation-value"))
		// operator-managed labels win over additional pod labels
		Expect(deployment.Spec.Template.Labels).To(HaveKeyWithValue(
			util.AppKubernetesIoNameLabel, appKubernetesIoNameValue))
		Expect(deployment.Spec.Template.Annotations).To(HaveKeyWithValue("pod-annotation", "pod-annotation-value"))
		Expect(deployment.Spec.Template.Annotations).To(HaveKey(configHashAnnotation))
	})

	When("mTLS is enabled", func() {
		const certSecretName = "sco-mtls-server-cert-secret"
		var desiredState []clientObject

		BeforeEach(func() {
			desiredState = assemble(nil, util.ExtraConfig{
				ScrapeCoordinatorMtlsEnabled:              true,
				ScrapeCoordinatorMtlsServerCertSecretName: certSecretName,
			})
		})

		It("should mount the server certs and add the HTTPS ports", func() {
			podSpec := getDeployment(desiredState).Spec.Template.Spec
			Expect(podSpec.Volumes).To(ContainElement(MatchVolume(
				CertsVolumeName,
				"secret", map[string]string{
					"secretName": certSecretName,
				},
			)))
			Expect(podSpec.Containers[0].VolumeMounts).To(ContainElement(MatchVolumeMount(CertsVolumeName, certsDir)))
			Expect(podSpec.Containers[0].Ports).To(ContainElement(MatchContainerPort("https", 8443)))
			Expect(getService(desiredState).Spec.Ports).To(
				ContainElement(MatchServicePort("https", 443, intstr.FromString("https"))))
		})

		It("should add the TLS settings to the configuration file", func() {
			cfg := parseConfig(getConfigMap(desiredState))
			Expect(cfg.Server.TLS).ToNot(BeNil())
			Expect(cfg.Server.TLS.ListenAddr).To(Equal(":8443"))
			Expect(cfg.Server.TLS.CAFile).To(Equal(certsDir + "/ca.crt"))
			Expect(cfg.Server.TLS.CertFile).To(Equal(certsDir + "/tls.crt"))
			Expect(cfg.Server.TLS.KeyFile).To(Equal(certsDir + "/tls.key"))
		})
	})

	It("should allow insecure auth secrets if configured", func() {
		cfg := parseConfig(getConfigMap(assemble(nil, util.ExtraConfig{
			ScrapeCoordinatorAllowInsecureAuthSecrets: true,
		})))
		Expect(cfg.Server.AllowInsecureAuthSecrets).To(BeTrue())
	})

	It("should grant cluster-wide access to secrets if allowSecretAccessInNamespaces is not set", func() {
		desiredState := assemble([]string{"namespace-a"}, util.ExtraConfigDefaults)
		Expect(clusterRoleResources(getClusterRole(desiredState))).To(ContainElement("secrets"))
	})

	It("should restrict access to secrets to the namespaces in allowSecretAccessInNamespaces", func() {
		desiredState := assemble([]string{"namespace-a", "namespace-c"}, util.ExtraConfig{
			ScrapeCoordinatorAllowSecretAccessInNamespaces: []string{"namespace-b", "namespace-a", "namespace-b"},
		})

		resources := clusterRoleResources(getClusterRole(desiredState))
		Expect(resources).ToNot(ContainElement("secrets"))
		Expect(resources).To(ContainElements("namespaces", "pods", "servicemonitors", "probes"))
		cfg := parseConfig(getConfigMap(desiredState))
		Expect(cfg.PrometheusCRs.Namespaces).To(Equal([]string{"namespace-a", "namespace-c"}))
		Expect(cfg.PrometheusCRs.SecretNamespaces).To(Equal([]string{"namespace-a", "namespace-b"}))
	})

	It("should enable self-monitoring in the deployment", func() {
		desiredState, err := assembleDesiredState(config, nil, util.ExtraConfigDefaults, selfMonitoringInput{
			configuration: selfmonitoringapiaccess.SelfMonitoringConfiguration{
				SelfMonitoringEnabled: true,
				Export:                *Dash0ExportWithEndpointAndToken(),
			},
			clusterName: "cluster-name",
		})
		Expect(err).ToNot(HaveOccurred())

		container := getDeployment(desiredState).Spec.Template.Spec.Containers[0]
		Expect(container.Env).To(ContainElement(MatchEnvVar("K8S_CLUSTER_NAME", "cluster-name")))
		Expect(container.Env).To(ContainElement(MatchEnvVar("K8S_CLUSTER_UID", "cluster-uid")))
		Expect(container.Env).To(ContainElement(HaveField("Name", "DASH0_OPERATOR_NAMESPACE")))
		Expect(container.Env).To(ContainElement(HaveField("Name", "OTEL_EXPORTER_OTLP_ENDPOINT")))
	})

	It("should not enable self-monitoring in the deployment if it is disabled", func() {
		container := getDeployment(assemble(nil, util.ExtraConfigDefaults)).Spec.Template.Spec.Containers[0]
		Expect(container.Env).ToNot(ContainElement(HaveField("Name", "OTEL_EXPORTER_OTLP_ENDPOINT")))
	})

	It("should mark all resources so that ArgoCD does not prune them", func() {
		for _, object := range assemble(nil, util.ExtraConfigDefaults) {
			Expect(object.object.GetAnnotations()).To(
				HaveKeyWithValue("argocd.argoproj.io/sync-options", "Prune=false"))
		}
	})
})

func getClusterRole(desiredState []clientObject) *rbacv1.ClusterRole {
	return findObjectByName(desiredState, ClusterRoleName(ManagedResourceNamePrefixTest)).(*rbacv1.ClusterRole)
}

func clusterRoleResources(clusterRole *rbacv1.ClusterRole) []string {
	var resources []string
	for _, rule := range clusterRole.Rules {
		resources = append(resources, rule.Resources...)
	}
	return resources
}

func getConfigMap(desiredState []clientObject) *corev1.ConfigMap {
	return findObjectByName(desiredState, ConfigMapName(ManagedResourceNamePrefixTest)).(*corev1.ConfigMap)
}

func getDeployment(desiredState []clientObject) *appsv1.Deployment {
	return findObjectByName(desiredState, DeploymentName(ManagedResourceNamePrefixTest)).(*appsv1.Deployment)
}

func getService(desiredState []clientObject) *corev1.Service {
	return findObjectByName(desiredState, ServiceName(ManagedResourceNamePrefixTest)).(*corev1.Service)
}

func findObjectByName(desiredState []clientObject, name string) client.Object {
	for _, object := range desiredState {
		if object.object.GetName() == name {
			return object.object
		}
	}
	Fail("object not found in the desired state: " + name)
	return nil
}

func parseConfig(configMap *corev1.ConfigMap) coordinatorConfig {
	var cfg coordinatorConfig
	Expect(yaml.UnmarshalStrict([]byte(configMap.Data[configFileName]), &cfg)).To(Succeed())
	return cfg
}
