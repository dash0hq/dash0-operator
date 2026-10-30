// SPDX-FileCopyrightText: Copyright 2026 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package webhooks

import (
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	dash0v1alpha1 "github.com/dash0hq/dash0-operator/api/operator/v1alpha1"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	. "github.com/dash0hq/dash0-operator/test/util"
)

var _ = Describe("The schema validation of the eBPF profiler", func() {
	resource := func() *unstructured.Unstructured {
		content, err := runtime.DefaultUnstructuredConverter.ToUnstructured(&dash0v1alpha1.Dash0OperatorConfiguration{
			TypeMeta: metav1.TypeMeta{APIVersion: "operator.dash0.com/v1alpha1", Kind: "Dash0OperatorConfiguration"},
			ObjectMeta: metav1.ObjectMeta{
				Name: "ebpf-profiler-schema-validation",
			},
			Spec: OperatorConfigurationResourceDefaultSpec,
		})
		Expect(err).NotTo(HaveOccurred())
		r := &unstructured.Unstructured{Object: content}
		Expect(k8sClient.Create(ctx, r, client.DryRunAll)).To(Succeed())
		return r
	}

	DescribeTable("should enforce profiler field constraints", func(path []string, value any, valid bool) {
		r := resource()
		fields := append([]string{"spec", "profiling", "ebpfProfiler"}, path...)
		Expect(unstructured.SetNestedField(r.Object, value, fields...)).To(Succeed())
		err := k8sClient.Create(ctx, r, client.DryRunAll)
		if valid {
			Expect(err).NotTo(HaveOccurred())
		} else {
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring(path[len(path)-1]))
		}
	},
		Entry("positive CPU frequency", []string{"cpuSamplingFrequency"}, int64(1), true),
		Entry("zero CPU frequency", []string{"cpuSamplingFrequency"}, int64(0), false),
		Entry("negative CPU frequency", []string{"cpuSamplingFrequency"}, int64(-1), false),
		Entry("off-CPU disabled", []string{"offCPUThreshold"}, float64(0), true),
		Entry("off-CPU maximum", []string{"offCPUThreshold"}, float64(1), true),
		Entry("off-CPU fraction", []string{"offCPUThreshold"}, 0.25, true),
		Entry("off-CPU below zero", []string{"offCPUThreshold"}, -0.01, false),
		Entry("off-CPU above one", []string{"offCPUThreshold"}, 1.01, false),
		Entry("minimum interval", []string{"probabilisticSampling", "interval"}, "1m", true),
		Entry("maximum interval", []string{"probabilisticSampling", "interval"}, "5m", true),
		Entry("equivalent interval", []string{"probabilisticSampling", "interval"}, "90s", true),
		Entry("short interval", []string{"probabilisticSampling", "interval"}, "59s", false),
		Entry("long interval", []string{"probabilisticSampling", "interval"}, "301s", false),
		Entry("minimum threshold", []string{"probabilisticSampling", "threshold"}, int64(1), true),
		Entry("integer threshold", []string{"probabilisticSampling", "threshold"}, int64(23), true),
		Entry("maximum threshold", []string{"probabilisticSampling", "threshold"}, int64(100), true),
		Entry("zero threshold", []string{"probabilisticSampling", "threshold"}, int64(0), false),
		Entry("negative threshold", []string{"probabilisticSampling", "threshold"}, int64(-1), false),
		Entry("threshold above maximum", []string{"probabilisticSampling", "threshold"}, int64(101), false),
		Entry("fractional threshold", []string{"probabilisticSampling", "threshold"}, 0.25, false),
		Entry("minimum map scale", []string{"mapScaleFactor"}, int64(0), true),
		Entry("maximum map scale", []string{"mapScaleFactor"}, int64(8), true),
		Entry("negative map scale", []string{"mapScaleFactor"}, int64(-1), false),
		Entry("excessive map scale", []string{"mapScaleFactor"}, int64(9), false),
		Entry("positive upload concurrency", []string{"debuginfo", "maxConcurrentUploads"}, int64(1), true),
		Entry("zero upload concurrency", []string{"debuginfo", "maxConcurrentUploads"}, int64(0), false),
		Entry("positive queue size", []string{"debuginfo", "queueSize"}, int64(1), true),
		Entry("zero queue size", []string{"debuginfo", "queueSize"}, int64(0), false),
		Entry("upstream tracer names", []string{"tracers"}, []any{
			"all", "native", "python", "hotspot", "php", "ruby", "perl", "v8", "dotnet", "cuda", "labels", "beam", "luajit",
		}, true),
		Entry("in-agent Go symbolization is not supported", []string{"tracers"}, []any{"go"}, false),
		Entry("unsupported none tracer", []string{"tracers"}, []any{"none"}, false),
		Entry("unknown tracer", []string{"tracers"}, []any{"unknown"}, false),
		Entry("no PID namespace translation", []string{"pidNamespaceTranslationMode"}, "none", true),
		Entry("automatic PID namespace translation", []string{"pidNamespaceTranslationMode"}, "auto", true),
		Entry("exact PID namespace translation", []string{"pidNamespaceTranslationMode"}, "exact", true),
		Entry("recursive PID namespace translation", []string{"pidNamespaceTranslationMode"}, "recursive", true),
		Entry("unknown PID namespace translation", []string{"pidNamespaceTranslationMode"}, "unknown", false),
		Entry("empty PID namespace translation", []string{"pidNamespaceTranslationMode"}, "", false),
		Entry("numeric PID namespace translation", []string{"pidNamespaceTranslationMode"}, int64(1), false),
		Entry("debug log level", []string{"logLevel"}, "debug", true),
		Entry("unsupported log level", []string{"logLevel"}, "trace", false),
	)

	It("should reject an interval that cannot be decoded as a duration", func() {
		r := resource()
		Expect(unstructured.SetNestedField(r.Object, "invalid",
			"spec", "profiling", "ebpfProfiler", "probabilisticSampling", "interval")).To(Succeed())
		Expect(k8sClient.Create(ctx, r, client.DryRunAll)).To(MatchError(ContainSubstring(`invalid duration "invalid"`)))
	})

	It("should default optional profiler fields without opting in", func() {
		r := resource()
		Expect(unstructured.SetNestedField(r.Object, map[string]any{
			"debuginfo":             map[string]any{},
			"probabilisticSampling": map[string]any{},
		}, "spec", "profiling", "ebpfProfiler")).To(Succeed())
		Expect(k8sClient.Create(ctx, r, client.DryRunAll)).To(Succeed())
		var typed dash0v1alpha1.Dash0OperatorConfiguration
		Expect(runtime.DefaultUnstructuredConverter.FromUnstructured(r.Object, &typed)).To(Succeed())
		profiler := typed.Spec.Profiling.EbpfProfiler
		Expect(profiler).NotTo(BeNil())
		Expect(profiler.Enabled).To(HaveValue(BeFalse()))
		Expect(profiler.PIDNamespaceTranslationMode).To(BeEmpty())
		Expect(profiler.CPUSamplingFrequency).To(HaveValue(Equal(int32(19))))
		Expect(profiler.OffCPUThreshold).To(HaveValue(Equal(float64(0))))
		Expect(profiler.EnableErrorFrames).To(HaveValue(BeFalse()))
		Expect(profiler.MapScaleFactor).To(HaveValue(Equal(int32(0))))
		Expect(profiler.Debuginfo.UploadEnabled).To(HaveValue(BeTrue()))
		Expect(profiler.Debuginfo.MaxConcurrentUploads).To(HaveValue(Equal(int32(25))))
		Expect(profiler.Debuginfo.QueueSize).To(HaveValue(Equal(int32(4096))))
		Expect(profiler.ProbabilisticSampling.Interval.Duration).To(Equal(time.Minute))
		Expect(profiler.ProbabilisticSampling.Threshold).To(HaveValue(Equal(int32(100))))
	})
})
