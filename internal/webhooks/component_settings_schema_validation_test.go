// SPDX-FileCopyrightText: Copyright 2026 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package webhooks

import (
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	dash0v1alpha1 "github.com/dash0hq/dash0-operator/api/operator/v1alpha1"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	. "github.com/dash0hq/dash0-operator/test/util"
)

var _ = Describe("The schema validation of spec.components", func() {
	withComponents := func(
		components *dash0v1alpha1.OperatorConfigurationComponents,
	) *dash0v1alpha1.Dash0OperatorConfiguration {
		spec := OperatorConfigurationResourceDefaultSpec
		spec.Components = components
		return &dash0v1alpha1.Dash0OperatorConfiguration{
			ObjectMeta: metav1.ObjectMeta{Name: "component-settings-schema-validation"},
			Spec:       spec,
		}
	}

	withProbes := func(probes *dash0v1alpha1.CollectorProbeSettings) *dash0v1alpha1.Dash0OperatorConfiguration {
		return withComponents(&dash0v1alpha1.OperatorConfigurationComponents{
			DaemonSetCollector: &dash0v1alpha1.DaemonSetCollectorSettings{
				CollectorPodSettings: dash0v1alpha1.CollectorPodSettings{Probes: probes},
			},
		})
	}

	withMTls := func(mTls *dash0v1alpha1.TargetAllocatorMTls) *dash0v1alpha1.Dash0OperatorConfiguration {
		return withComponents(&dash0v1alpha1.OperatorConfigurationComponents{
			TargetAllocator: &dash0v1alpha1.TargetAllocatorSettings{MTls: mTls},
		})
	}

	withOffsetVolume := func(volume *dash0v1alpha1.FilelogOffsetStorageVolume) *dash0v1alpha1.Dash0OperatorConfiguration {
		return withComponents(&dash0v1alpha1.OperatorConfigurationComponents{
			DaemonSetCollector: &dash0v1alpha1.DaemonSetCollectorSettings{FilelogOffsetStorageVolume: volume},
		})
	}

	DescribeTable("should accept valid and reject invalid settings", func(
		operatorConfigurationResource *dash0v1alpha1.Dash0OperatorConfiguration,
		expectedMessage string,
	) {
		err := k8sClient.Create(ctx, operatorConfigurationResource, client.DryRunAll)
		if expectedMessage == "" {
			Expect(err).ToNot(HaveOccurred())
		} else {
			Expect(err).To(MatchError(ContainSubstring(expectedMessage)))
		}
	},
		Entry("liveness probe with successThreshold 1 and terminationGracePeriodSeconds",
			withProbes(&dash0v1alpha1.CollectorProbeSettings{
				Liveness: &dash0v1alpha1.ProbeSettings{
					SuccessThreshold:              new(int32(1)),
					TerminationGracePeriodSeconds: new(int64(10)),
				},
			}),
			"",
		),
		Entry("liveness probe with successThreshold 2",
			withProbes(&dash0v1alpha1.CollectorProbeSettings{
				Liveness: &dash0v1alpha1.ProbeSettings{SuccessThreshold: new(int32(2))},
			}),
			"liveness.successThreshold must be 1",
		),
		Entry("startup probe with successThreshold 2",
			withProbes(&dash0v1alpha1.CollectorProbeSettings{
				Startup: &dash0v1alpha1.ProbeSettings{SuccessThreshold: new(int32(2))},
			}),
			"startup.successThreshold must be 1",
		),
		Entry("readiness probe with successThreshold 2",
			withProbes(&dash0v1alpha1.CollectorProbeSettings{
				Readiness: &dash0v1alpha1.ProbeSettings{SuccessThreshold: new(int32(2))},
			}),
			"",
		),
		Entry("readiness probe with terminationGracePeriodSeconds",
			withProbes(&dash0v1alpha1.CollectorProbeSettings{
				Readiness: &dash0v1alpha1.ProbeSettings{TerminationGracePeriodSeconds: new(int64(10))},
			}),
			"readiness.terminationGracePeriodSeconds must not be set",
		),
		Entry("mTLS enabled with both secret names",
			withMTls(&dash0v1alpha1.TargetAllocatorMTls{
				Enabled:              new(true),
				ServerCertSecretName: "server-cert",
				ClientCertSecretName: "client-cert",
			}),
			"",
		),
		Entry("mTLS enabled without the client certificate secret name",
			withMTls(&dash0v1alpha1.TargetAllocatorMTls{
				Enabled:              new(true),
				ServerCertSecretName: "server-cert",
			}),
			"serverCertSecretName and clientCertSecretName are required when mTLS is enabled",
		),
		Entry("mTLS disabled without secret names",
			withMTls(&dash0v1alpha1.TargetAllocatorMTls{Enabled: new(false)}),
			"",
		),
		Entry("offset volume with a persistent volume claim",
			withOffsetVolume(&dash0v1alpha1.FilelogOffsetStorageVolume{
				PersistentVolumeClaim: &dash0v1alpha1.FilelogOffsetStoragePersistentVolumeClaim{ClaimName: "offsets"},
			}),
			"",
		),
		Entry("offset volume with a host path",
			withOffsetVolume(&dash0v1alpha1.FilelogOffsetStorageVolume{
				HostPath: &dash0v1alpha1.FilelogOffsetStorageHostPath{Path: "/var/lib/dash0..offsets/./x"},
			}),
			"",
		),
		Entry("offset volume with a persistent volume claim and a host path",
			withOffsetVolume(&dash0v1alpha1.FilelogOffsetStorageVolume{
				PersistentVolumeClaim: &dash0v1alpha1.FilelogOffsetStoragePersistentVolumeClaim{ClaimName: "offsets"},
				HostPath:              &dash0v1alpha1.FilelogOffsetStorageHostPath{Path: "/var/lib/dash0"},
			}),
			"exactly one of persistentVolumeClaim and hostPath must be set",
		),
		Entry("offset volume without a persistent volume claim and a host path",
			withOffsetVolume(&dash0v1alpha1.FilelogOffsetStorageVolume{}),
			"exactly one of persistentVolumeClaim and hostPath must be set",
		),
		Entry("offset volume with a host path that contains '..' in the middle",
			withOffsetVolume(&dash0v1alpha1.FilelogOffsetStorageVolume{
				HostPath: &dash0v1alpha1.FilelogOffsetStorageHostPath{Path: "/var/../etc"},
			}),
			"path must not contain '..'",
		),
		Entry("offset volume with a host path that ends with '..'",
			withOffsetVolume(&dash0v1alpha1.FilelogOffsetStorageVolume{
				HostPath: &dash0v1alpha1.FilelogOffsetStorageHostPath{Path: "/var/.."},
			}),
			"path must not contain '..'",
		),
		Entry("offset volume with the host path '..'",
			withOffsetVolume(&dash0v1alpha1.FilelogOffsetStorageVolume{
				HostPath: &dash0v1alpha1.FilelogOffsetStorageHostPath{Path: ".."},
			}),
			"path must not contain '..'",
		),
		Entry("offset volume with an empty host path",
			withOffsetVolume(&dash0v1alpha1.FilelogOffsetStorageVolume{
				HostPath: &dash0v1alpha1.FilelogOffsetStorageHostPath{},
			}),
			"spec.components.daemonSetCollector.filelogOffsetStorageVolume.hostPath.path",
		),
	)

	withValuesThatRemoveADefault := func() *dash0v1alpha1.Dash0OperatorConfiguration {
		return withComponents(&dash0v1alpha1.OperatorConfigurationComponents{
			DaemonSetCollector: &dash0v1alpha1.DaemonSetCollectorSettings{
				CollectorContainerResources: &dash0v1alpha1.ContainerResourcesWithGoMemLimit{
					ContainerResources: dash0v1alpha1.ContainerResources{
						Limits: corev1.ResourceList{corev1.ResourceMemory: resource.MustParse("-1")},
					},
				},
				WorkloadSettings: dash0v1alpha1.WorkloadSettings{
					NodeAffinity: &corev1.NodeAffinity{
						RequiredDuringSchedulingIgnoredDuringExecution: &corev1.NodeSelector{
							NodeSelectorTerms: []corev1.NodeSelectorTerm{},
						},
					},
				},
				SELinuxOptions: &corev1.SELinuxOptions{},
			},
		})
	}

	It("should accept and keep the values that remove a default", func() {
		operatorConfigurationResource := withValuesThatRemoveADefault()
		Expect(k8sClient.Create(ctx, operatorConfigurationResource, client.DryRunAll)).To(Succeed())
		daemonSetCollector := operatorConfigurationResource.Spec.Components.DaemonSetCollector
		Expect(daemonSetCollector.CollectorContainerResources.Limits.Memory().String()).To(Equal("-1"))
		Expect(daemonSetCollector.NodeAffinity.RequiredDuringSchedulingIgnoredDuringExecution).ToNot(BeNil())
		Expect(daemonSetCollector.NodeAffinity.RequiredDuringSchedulingIgnoredDuringExecution.NodeSelectorTerms).To(
			BeEmpty())
		Expect(daemonSetCollector.SELinuxOptions).To(Equal(&corev1.SELinuxOptions{}))
	})

	It("should keep the values that remove a default with server-side apply", func() {
		content, err := runtime.DefaultUnstructuredConverter.ToUnstructured(withValuesThatRemoveADefault())
		Expect(err).ToNot(HaveOccurred())
		applied := &unstructured.Unstructured{Object: content}
		applied.SetGroupVersionKind(dash0v1alpha1.GroupVersion.WithKind("Dash0OperatorConfiguration"))
		Expect(k8sClient.Apply(
			ctx,
			client.ApplyConfigurationFromUnstructured(applied),
			client.FieldOwner("component-settings-schema-validation"),
			client.DryRunAll,
		)).To(Succeed())

		memoryLimit, found, err := unstructured.NestedString(applied.Object,
			"spec", "components", "daemonSetCollector", "collectorContainerResources", "limits", "memory")
		Expect(err).ToNot(HaveOccurred())
		Expect(found).To(BeTrue())
		Expect(memoryLimit).To(Equal("-1"))
		nodeSelectorTerms, found, err := unstructured.NestedSlice(applied.Object,
			"spec", "components", "daemonSetCollector", "nodeAffinity", "requiredDuringSchedulingIgnoredDuringExecution",
			"nodeSelectorTerms")
		Expect(err).ToNot(HaveOccurred())
		Expect(found).To(BeTrue())
		Expect(nodeSelectorTerms).To(BeEmpty())
		seLinuxOptions, found, err := unstructured.NestedMap(applied.Object,
			"spec", "components", "daemonSetCollector", "seLinuxOptions")
		Expect(err).ToNot(HaveOccurred())
		Expect(found).To(BeTrue())
		Expect(seLinuxOptions).To(BeEmpty())
	})

	It("should reject a readiness probe with terminationGracePeriodSeconds in the Signal Control resource", func() {
		signalControlResource := &dash0v1alpha1.Dash0SignalControl{
			ObjectMeta: metav1.ObjectMeta{Name: "component-settings-schema-validation"},
			Spec: dash0v1alpha1.Dash0SignalControlSpec{
				Components: &dash0v1alpha1.SignalControlComponents{
					Collector: &dash0v1alpha1.SignalControlCollectorSettings{
						CollectorPodSettings: dash0v1alpha1.CollectorPodSettings{
							Probes: &dash0v1alpha1.CollectorProbeSettings{
								Readiness: &dash0v1alpha1.ProbeSettings{TerminationGracePeriodSeconds: new(int64(10))},
							},
						},
					},
				},
			},
		}
		Expect(k8sClient.Create(ctx, signalControlResource, client.DryRunAll)).To(
			MatchError(ContainSubstring("readiness.terminationGracePeriodSeconds must not be set")))
	})
})
