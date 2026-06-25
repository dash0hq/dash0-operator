// SPDX-FileCopyrightText: Copyright 2025 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package taresources

import (
	"fmt"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	"k8s.io/apimachinery/pkg/util/intstr"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/dash0hq/dash0-operator/internal/util"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	. "github.com/dash0hq/dash0-operator/test/util"
	tatest "github.com/dash0hq/dash0-operator/test/util/targetallocator"
)

var _ = Describe("The desired state of the OpenTelemetry TargetAllocator resources", func() {
	It("should render custom resource requirements, tolerations, and node affinity", func() {
		desiredState, err := assembleDesiredStateForUpsert(&targetAllocatorConfig{
			OperatorNamespace: OperatorNamespace,
			NamePrefix:        TargetAllocatorPrefixTest,
			Images:            TestImages,
		}, nil, util.ExtraConfig{
			TargetAllocatorContainerResources: util.ResourceRequirementsWithGoMemLimit{
				Limits: corev1.ResourceList{
					corev1.ResourceCPU:              resource.MustParse("500m"),
					corev1.ResourceMemory:           resource.MustParse("1Gi"),
					corev1.ResourceEphemeralStorage: resource.MustParse("5Gi"),
				},
				GoMemLimit: "800MiB",
				Requests: corev1.ResourceList{
					corev1.ResourceCPU:              resource.MustParse("200m"),
					corev1.ResourceMemory:           resource.MustParse("500Mi"),
					corev1.ResourceEphemeralStorage: resource.MustParse("2Gi"),
				},
			},
			TargetAllocatorTolerations: []corev1.Toleration{
				{
					Key:      "key3",
					Operator: corev1.TolerationOpEqual,
					Value:    "value3",
					Effect:   corev1.TaintEffectNoSchedule,
				},
				{
					Key:      "key4",
					Operator: corev1.TolerationOpExists,
					Effect:   corev1.TaintEffectNoSchedule,
				},
			},
			TargetAllocatorNodeAffinity: &corev1.NodeAffinity{
				RequiredDuringSchedulingIgnoredDuringExecution: &corev1.NodeSelector{
					NodeSelectorTerms: []corev1.NodeSelectorTerm{
						{
							MatchExpressions: []corev1.NodeSelectorRequirement{
								{
									Key:      "affinity-key1",
									Operator: corev1.NodeSelectorOpIn,
									Values:   []string{"affinity-key1-value1"},
								},
							},
						},
					},
				},
				PreferredDuringSchedulingIgnoredDuringExecution: []corev1.PreferredSchedulingTerm{
					{
						Preference: corev1.NodeSelectorTerm{
							MatchExpressions: []corev1.NodeSelectorRequirement{
								{
									Key:      "affinity-key2",
									Operator: corev1.NodeSelectorOpIn,
									Values:   []string{"affinity-key2-value1", "affinity-key2-value2"},
								},
							},
						},
					},
				},
			},
		})

		Expect(err).ToNot(HaveOccurred())

		deploymentPodSpec := getDeployment(desiredState).Spec.Template.Spec

		Expect(deploymentPodSpec.Containers[0].Resources.Limits.Cpu().String()).To(Equal("500m"))
		Expect(deploymentPodSpec.Containers[0].Resources.Limits.Memory().String()).To(Equal("1Gi"))
		Expect(deploymentPodSpec.Containers[0].Resources.Limits.StorageEphemeral().String()).To(Equal("5Gi"))
		Expect(deploymentPodSpec.Containers[0].Resources.Requests.Cpu().String()).To(Equal("200m"))
		Expect(deploymentPodSpec.Containers[0].Resources.Requests.Memory().String()).To(Equal("500Mi"))
		Expect(deploymentPodSpec.Containers[0].Resources.Requests.StorageEphemeral().String()).To(Equal("2Gi"))
		Expect(deploymentPodSpec.Containers[0].Env).To(ContainElement(MatchEnvVar(util.EnvVarGoMemLimit, "800MiB")))

		Expect(deploymentPodSpec.Tolerations).To(HaveLen(2))
		Expect(deploymentPodSpec.Tolerations[0].Key).To(Equal("key3"))
		Expect(deploymentPodSpec.Tolerations[0].Operator).To(Equal(corev1.TolerationOpEqual))
		Expect(deploymentPodSpec.Tolerations[0].Value).To(Equal("value3"))
		Expect(deploymentPodSpec.Tolerations[0].Effect).To(Equal(corev1.TaintEffectNoSchedule))
		Expect(deploymentPodSpec.Tolerations[0].TolerationSeconds).To(BeNil())
		Expect(deploymentPodSpec.Tolerations[1].Key).To(Equal("key4"))
		Expect(deploymentPodSpec.Tolerations[1].Operator).To(Equal(corev1.TolerationOpExists))
		Expect(deploymentPodSpec.Tolerations[1].Value).To(BeEmpty())
		Expect(deploymentPodSpec.Tolerations[1].Effect).To(Equal(corev1.TaintEffectNoSchedule))
		Expect(deploymentPodSpec.Tolerations[1].TolerationSeconds).To(BeNil())

		deploymentAffinityReq := deploymentPodSpec.Affinity.NodeAffinity.RequiredDuringSchedulingIgnoredDuringExecution.NodeSelectorTerms
		Expect(deploymentAffinityReq).To(HaveLen(1))
		Expect(deploymentAffinityReq[0].MatchExpressions).To(HaveLen(1))
		Expect(deploymentAffinityReq[0].MatchExpressions[0].Key).To(Equal("affinity-key1"))
		Expect(deploymentAffinityReq[0].MatchExpressions[0].Operator).To(Equal(corev1.NodeSelectorOpIn))
		Expect(deploymentAffinityReq[0].MatchExpressions[0].Values).To(HaveLen(1))
		Expect(deploymentAffinityReq[0].MatchExpressions[0].Values[0]).To(Equal("affinity-key1-value1"))
		deploymentAffinityPref := deploymentPodSpec.Affinity.NodeAffinity.PreferredDuringSchedulingIgnoredDuringExecution
		Expect(deploymentAffinityPref).To(HaveLen(1))
		Expect(deploymentAffinityPref[0].Preference.MatchExpressions).To(HaveLen(1))
		Expect(deploymentAffinityPref[0].Preference.MatchExpressions[0].Key).To(Equal("affinity-key2"))
		Expect(deploymentAffinityPref[0].Preference.MatchExpressions[0].Operator).To(Equal(corev1.NodeSelectorOpIn))
		Expect(deploymentAffinityPref[0].Preference.MatchExpressions[0].Values).To(HaveLen(2))
		Expect(deploymentAffinityPref[0].Preference.MatchExpressions[0].Values[0]).To(Equal("affinity-key2-value1"))
		Expect(deploymentAffinityPref[0].Preference.MatchExpressions[0].Values[1]).To(Equal("affinity-key2-value2"))
	})

	It("should render additional labels and annotations on the workload and the pods", func() {
		desiredState, err := assembleDesiredStateForUpsert(&targetAllocatorConfig{
			OperatorNamespace: OperatorNamespace,
			NamePrefix:        TargetAllocatorPrefixTest,
			Images:            TestImages,
		}, nil, util.ExtraConfig{
			TargetAllocatorLabels:         map[string]string{"ta-label": "ta-label-value"},
			TargetAllocatorAnnotations:    map[string]string{"ta-annotation": "ta-annotation-value"},
			TargetAllocatorPodLabels:      map[string]string{"ta-pod-label": "ta-pod-label-value"},
			TargetAllocatorPodAnnotations: map[string]string{"ta-pod-annotation": "ta-pod-annotation-value"},
		})
		Expect(err).ToNot(HaveOccurred())

		deployment := getDeployment(desiredState)
		Expect(deployment.ObjectMeta.Labels).To(HaveKeyWithValue("ta-label", "ta-label-value"))
		// operator-managed labels are still present
		Expect(deployment.ObjectMeta.Labels).To(HaveKeyWithValue(util.AppKubernetesIoNameLabel, AppKubernetesIoNameValue))
		Expect(deployment.ObjectMeta.Annotations).To(HaveKeyWithValue("ta-annotation", "ta-annotation-value"))
		Expect(deployment.Spec.Template.Labels).To(HaveKeyWithValue("ta-pod-label", "ta-pod-label-value"))
		Expect(deployment.Spec.Template.Labels).To(HaveKeyWithValue(util.AppKubernetesIoNameLabel, AppKubernetesIoNameValue))
		Expect(deployment.Spec.Template.Annotations).To(HaveKeyWithValue("ta-pod-annotation", "ta-pod-annotation-value"))
		// operator-managed pod annotations are still present
		Expect(deployment.Spec.Template.Annotations).To(HaveKey("ta-config-sha"))
	})

	It("should not let additional labels override operator-managed labels", func() {
		desiredState, err := assembleDesiredStateForUpsert(&targetAllocatorConfig{
			OperatorNamespace: OperatorNamespace,
			NamePrefix:        TargetAllocatorPrefixTest,
			Images:            TestImages,
		}, nil, util.ExtraConfig{
			TargetAllocatorPodLabels: map[string]string{util.AppKubernetesIoNameLabel: "custom-value"},
		})
		Expect(err).ToNot(HaveOccurred())

		// the operator-managed value wins over the additional label
		Expect(getDeployment(desiredState).Spec.Template.Labels).
			To(HaveKeyWithValue(util.AppKubernetesIoNameLabel, AppKubernetesIoNameValue))
	})

	It("should add GKE Autopilot allowlist match labels", func() {
		desiredState, err := assembleDesiredStateForUpsert(&targetAllocatorConfig{
			OperatorNamespace: OperatorNamespace,
			NamePrefix:        TargetAllocatorPrefixTest,
			Images:            TestImages,
			IsGkeAutopilot:    true,
		}, nil, util.ExtraConfig{})
		Expect(err).ToNot(HaveOccurred())

		deploymentTemplateLabels := getDeployment(desiredState).Spec.Template.Labels
		value, ok := deploymentTemplateLabels[gkeAutopilotAllowlistKey]
		Expect(ok).To(BeTrue())
		Expect(value).To(Equal(gkeAutopilotAllowlistValue))
	})

	When("mTLS is enabled", Ordered, func() {
		const certSecretName = "ta-mtls-server-cert-secret"
		var desiredState []clientObject

		BeforeAll(func() {
			var err error
			desiredState, err = assembleDesiredStateForUpsert(&targetAllocatorConfig{
				OperatorNamespace: OperatorNamespace,
				NamePrefix:        TargetAllocatorPrefixTest,
				Images:            TestImages,
			}, nil, util.ExtraConfig{
				TargetAllocatorMtlsEnabled:              true,
				TargetAllocatorMtlsServerCertSecretName: certSecretName,
			})
			Expect(err).ToNot(HaveOccurred())
		})

		It("should mount the TLS certs and configure additional ports when mTLS is enabled", func() {
			podSpec := getDeployment(desiredState).Spec.Template.Spec
			service := getService(desiredState)

			Expect(podSpec).ToNot(BeNil())
			Expect(podSpec.Volumes).To(ContainElement(MatchVolume(
				targetAllocatorCertsVolumeName,
				"secret", map[string]string{
					"secretName": certSecretName,
				},
			)))
			Expect(podSpec.Containers[0].VolumeMounts).To(ContainElement(MatchVolumeMount(
				targetAllocatorCertsVolumeName,
				targetAllocatorCertsVolumeDir,
			)))
			Expect(podSpec.Containers[0].Ports).To(ContainElement(MatchContainerPort("https", 8443)))

			Expect(service).ToNot(BeNil())
			Expect(service.Spec.Ports).To(ContainElement(MatchServicePort("https", 443, intstr.FromString("https"))))
		})

		It("should add config for mTLS to the ConfigMap", func() {
			configMap := getConfigMap(desiredState)

			Expect(configMap).ToNot(BeNil())
			taConfig := parseConfigMapContent(configMap)
			httpsConfig, ok := ReadFromMap(taConfig, []string{"https"}).(map[string]any)
			Expect(ok).To(BeTrue())
			Expect(httpsConfig["enabled"]).To(Equal(true))
			Expect(httpsConfig["ca_file_path"]).To(Equal(fmt.Sprintf("%s/ca.crt", targetAllocatorCertsVolumeDir)))
			Expect(httpsConfig["tls_cert_file_path"]).To(Equal(fmt.Sprintf("%s/tls.crt", targetAllocatorCertsVolumeDir)))
			Expect(httpsConfig["tls_key_file_path"]).To(Equal(fmt.Sprintf("%s/tls.key", targetAllocatorCertsVolumeDir)))
		})
	})
})

func getConfigMap(desiredState []clientObject) *corev1.ConfigMap {
	if cm := findObjectByName(desiredState, tatest.ExpectedTargetAllocatorConfigMapName); cm != nil {
		return cm.(*corev1.ConfigMap)
	}
	return nil
}

func getDeployment(desiredState []clientObject) *appsv1.Deployment {
	if deployment := findObjectByName(desiredState, tatest.ExpectedTargetAllocatorDeploymentName); deployment != nil {
		return deployment.(*appsv1.Deployment)
	}
	return nil
}

func getService(desiredState []clientObject) *corev1.Service {
	if service := findObjectByName(desiredState, tatest.ExpectedTargetAllocatorServiceName); service != nil {
		return service.(*corev1.Service)
	}
	return nil
}

func findObjectByName(desiredState []clientObject, name string) client.Object {
	for _, object := range desiredState {
		if object.object.GetName() == name {
			return object.object
		}
	}
	return nil
}

func parseConfigMapContent(configMap *corev1.ConfigMap) map[string]any {
	return ParseConfigMapContent(configMap, "targetallocator.yaml")
}
