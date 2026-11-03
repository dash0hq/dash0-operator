// SPDX-FileCopyrightText: Copyright 2026 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package util

import (
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"

	dash0v1alpha1 "github.com/dash0hq/dash0-operator/api/operator/v1alpha1"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("profiling component settings", func() {
	DescribeTable("should merge and validate component settings without Helm conflicts",
		func(profiler bool, component string, memoryLimit string, memoryRequest string, cpuRequest string) {
			components := &dash0v1alpha1.OperatorConfigurationComponents{}
			var resources **dash0v1alpha1.ContainerResourcesWithGoMemLimit
			var workload *dash0v1alpha1.WorkloadSettings
			var priority *string
			if profiler {
				components.EbpfProfiler = &dash0v1alpha1.EbpfProfilerSettings{}
				resources = &components.EbpfProfiler.ContainerResources
				workload = &components.EbpfProfiler.WorkloadSettings
				priority = &components.EbpfProfiler.PriorityClassName
			} else {
				components.DebuginfoProxy = &dash0v1alpha1.DebuginfoProxySettings{}
				resources = &components.DebuginfoProxy.ContainerResources
				workload = &components.DebuginfoProxy.WorkloadSettings
				priority = &components.DebuginfoProxy.PriorityClassName
			}
			configuration := &dash0v1alpha1.Dash0OperatorConfiguration{}
			configuration.Spec.Components = components
			resolve := func() (ResourceRequirementsWithGoMemLimit, dash0v1alpha1.WorkloadSettings, string) {
				effective := EffectiveExtraConfig(ExtraConfigDefaults, ExtraConfigDefaults, configuration, nil)
				if profiler {
					return effective.EbpfProfilerContainerResources, dash0v1alpha1.WorkloadSettings{
						Labels: effective.EbpfProfilerLabels, Annotations: effective.EbpfProfilerAnnotations,
						PodLabels: effective.EbpfProfilerPodLabels, PodAnnotations: effective.EbpfProfilerPodAnnotations,
						Tolerations: effective.EbpfProfilerTolerations, NodeAffinity: effective.EbpfProfilerNodeAffinity,
					}, effective.EbpfProfilerPriorityClassName
				}
				return effective.DebuginfoProxyContainerResources, dash0v1alpha1.WorkloadSettings{
					Labels: effective.DebuginfoProxyLabels, Annotations: effective.DebuginfoProxyAnnotations,
					PodLabels: effective.DebuginfoProxyPodLabels, PodAnnotations: effective.DebuginfoProxyPodAnnotations,
					Tolerations: effective.DebuginfoProxyTolerations, NodeAffinity: effective.DebuginfoProxyNodeAffinity,
				}, effective.DebuginfoProxyPriorityClassName
			}
			check := func() ComponentSettingsCheck {
				return CheckComponentSettings(ExtraConfigDefaults, ExtraConfigDefaults, components, nil)
			}

			r, w, p := resolve()
			Expect(r.Limits.Memory().String()).To(Equal(memoryLimit))
			Expect(r.Limits).ToNot(HaveKey(corev1.ResourceCPU))
			Expect(r.Requests.Memory().String()).To(Equal(memoryRequest))
			Expect(r.Requests.Cpu().String()).To(Equal(cpuRequest))
			Expect(r.GoMemLimit).To(BeEmpty())
			Expect(w.NodeAffinity).To(Equal(defaultNodeAffinity()))
			Expect(p).To(BeEmpty())
			Expect(check().Invalid).To(BeEmpty())
			Expect(check().Conflicts).To(BeEmpty())

			*resources = &dash0v1alpha1.ContainerResourcesWithGoMemLimit{
				ContainerResources: dash0v1alpha1.ContainerResources{
					Limits:   corev1.ResourceList{corev1.ResourceMemory: resource.MustParse("1Gi")},
					Requests: corev1.ResourceList{corev1.ResourceCPU: resource.MustParse("-1")},
				},
				GoMemLimit: "900MiB",
			}
			*workload = dash0v1alpha1.WorkloadSettings{
				Labels: map[string]string{"component": component}, Annotations: map[string]string{"a": "b"},
				PodLabels: map[string]string{"pod": component}, PodAnnotations: map[string]string{"c": "d"},
				Tolerations: []corev1.Toleration{{Key: "dedicated", Operator: corev1.TolerationOpExists}},
				NodeAffinity: &corev1.NodeAffinity{
					RequiredDuringSchedulingIgnoredDuringExecution: &corev1.NodeSelector{},
				},
			}
			*priority = "profiling-priority"
			r, w, p = resolve()
			Expect(r.Limits.Memory().String()).To(Equal("1Gi"))
			Expect(r.Requests).ToNot(HaveKey(corev1.ResourceCPU))
			Expect(r.Requests.Memory().String()).To(Equal(memoryRequest))
			Expect(r.GoMemLimit).To(Equal("900MiB"))
			Expect(w.Labels).To(Equal(workload.Labels))
			Expect(w.Annotations).To(Equal(workload.Annotations))
			Expect(w.PodLabels).To(Equal(workload.PodLabels))
			Expect(w.PodAnnotations).To(Equal(workload.PodAnnotations))
			Expect(w.Tolerations).To(Equal(workload.Tolerations))
			Expect(w.NodeAffinity).To(BeNil())
			Expect(p).To(Equal("profiling-priority"))
			Expect(check().Invalid).To(BeEmpty())
			Expect(check().Conflicts).To(BeEmpty())
			w.Labels["component"] = "modified"
			w.Tolerations[0].Key = "modified"
			Expect(workload.Labels["component"]).To(Equal(component))
			Expect(workload.Tolerations[0].Key).To(Equal("dedicated"))
			Expect((*resources).Requests).To(HaveKey(corev1.ResourceCPU))

			(*resources).Limits[corev1.ResourceMemory] = resource.MustParse("8Mi")
			(*resources).GoMemLimit = "off"
			r, _, _ = resolve()
			Expect(r.Requests.Memory().String()).To(Equal("8Mi"))
			Expect(r.GoMemLimit).To(Equal("off"))
			Expect(check().Invalid).To(BeEmpty())

			(*resources).GoMemLimit = "8MiB"
			Expect(check().Invalid).To(ContainElement(ContainSubstring(
				"spec.components." + component + ".containerResources.gomemlimit")))
			(*resources).Limits[corev1.ResourceMemory] = resource.MustParse("-1")
			r, _, _ = resolve()
			Expect(r.Limits).To(BeNil())
			Expect(check().Invalid).To(BeEmpty())

			(*resources).Limits[corev1.ResourceMemory] = resource.MustParse("-2")
			workload.Labels = map[string]string{"invalid label": "value"}
			workload.Tolerations[0].Operator = "invalid"
			workload.NodeAffinity = &corev1.NodeAffinity{
				PreferredDuringSchedulingIgnoredDuringExecution: []corev1.PreferredSchedulingTerm{{Weight: 101}},
			}
			*priority = "invalid priority"
			invalid := check().Invalid
			for _, field := range []string{"containerResources.limits[memory]", "labels", "tolerations", "nodeAffinity",
				"priorityClassName"} {
				Expect(invalid).To(ContainElement(ContainSubstring("spec.components." + component + "." + field)))
			}
			Expect(check().Conflicts).To(BeEmpty())
		},
		Entry("eBPF profiler", true, "ebpfProfiler", "512Mi", "128Mi", "100m"),
		Entry("debug information proxy", false, "debuginfoProxy", "128Mi", "32Mi", "10m"),
	)

	It("should not report Helm conflicts for profiling settings without Helm values", func() {
		fileConfig := ExtraConfigDefaults
		fileConfig.EbpfProfilerLabels = map[string]string{"source": "file"}
		fileConfig.DebuginfoProxyPodLabels = map[string]string{"source": "file"}
		fileConfig.EbpfProfilerContainerResources = ResourceRequirementsWithGoMemLimit{}
		fileConfig.DebuginfoProxyContainerResources = ResourceRequirementsWithGoMemLimit{}
		components := &dash0v1alpha1.OperatorConfigurationComponents{
			EbpfProfiler: &dash0v1alpha1.EbpfProfilerSettings{
				ContainerResources: &dash0v1alpha1.ContainerResourcesWithGoMemLimit{},
				WorkloadSettings: dash0v1alpha1.WorkloadSettings{
					Labels: map[string]string{"source": "resource"},
				},
			},
			DebuginfoProxy: &dash0v1alpha1.DebuginfoProxySettings{
				ContainerResources: &dash0v1alpha1.ContainerResourcesWithGoMemLimit{},
				WorkloadSettings: dash0v1alpha1.WorkloadSettings{
					PodLabels: map[string]string{"source": "resource"},
				},
			},
		}
		check := CheckComponentSettings(fileConfig, ExtraConfigDefaults, components, nil)
		Expect(check.Conflicts).To(BeEmpty())
		Expect(check.Invalid).To(BeEmpty())
	})
})
