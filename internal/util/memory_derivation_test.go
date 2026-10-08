// SPDX-FileCopyrightText: Copyright 2026 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package util

import (
	"strconv"
	"strings"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("collector memory derivation", func() {

	type derivationTest struct {
		limit      string
		expectOk   bool
		goMemLimit string
		limitMiB   int
		spikeMiB   int
		softMiB    int
	}

	DescribeTable("DeriveCollectorMemorySettings",
		func(t derivationTest) {
			limitQuantity := resource.MustParse(t.limit)
			settings, ok := DeriveCollectorMemorySettings(limitQuantity)
			Expect(ok).To(Equal(t.expectOk))
			if !t.expectOk {
				return
			}
			Expect(settings.GoMemLimit).To(Equal(t.goMemLimit))
			Expect(settings.LimitMiB).To(Equal(t.limitMiB))
			Expect(settings.SpikeMiB).To(Equal(t.spikeMiB))
			Expect(settings.SoftMiB).To(Equal(t.softMiB))

			// The whole point of the derivation: GOMEMLIMIT < soft < hard < limit.
			goMemNum, err := strconv.Atoi(strings.TrimSuffix(settings.GoMemLimit, "MiB"))
			Expect(err).ToNot(HaveOccurred())
			Expect(goMemNum).To(BeNumerically("<", settings.SoftMiB))
			Expect(settings.SoftMiB).To(BeNumerically("<", settings.LimitMiB))
			Expect(settings.LimitMiB).To(BeNumerically("<", int(limitQuantity.Value()/mibBytes)))
		},
		// Floors dominate at the small end (500Mi -> ~81% hard); the percentage slope takes over from ~1Gi
		// upwards, converging to ~90% hard / ~82% gomemlimit.
		Entry("500Mi (daemonset/deployment default)",
			derivationTest{limit: "500Mi", expectOk: true, goMemLimit: "348MiB", limitMiB: 404, spikeMiB: 32, softMiB: 372}),
		Entry("1Gi (signal-control default)",
			derivationTest{limit: "1Gi", expectOk: true, goMemLimit: "841MiB", limitMiB: 922, spikeMiB: 51, softMiB: 871}),
		Entry("2Gi",
			derivationTest{limit: "2Gi", expectOk: true, goMemLimit: "1681MiB", limitMiB: 1844, spikeMiB: 102, softMiB: 1742}),
		Entry("4Gi",
			derivationTest{limit: "4Gi", expectOk: true, goMemLimit: "3361MiB", limitMiB: 3687, spikeMiB: 204, softMiB: 3483}),
		Entry("no limit set -> percentage fallback", derivationTest{limit: "0", expectOk: false}),
		Entry("limit too small for the floors -> percentage fallback", derivationTest{limit: "128Mi", expectOk: false}),
	)

	Describe("EffectiveGoMemLimit", func() {
		It("returns an explicitly configured value verbatim", func() {
			rr := ResourceRequirementsWithGoMemLimit{
				Limits:     corev1.ResourceList{corev1.ResourceMemory: resource.MustParse("500Mi")},
				GoMemLimit: "123MiB",
			}
			Expect(rr.EffectiveGoMemLimit()).To(Equal("123MiB"))
		})

		It("derives the value from the memory limit when none is configured", func() {
			rr := ResourceRequirementsWithGoMemLimit{
				Limits: corev1.ResourceList{corev1.ResourceMemory: resource.MustParse("500Mi")},
			}
			Expect(rr.EffectiveGoMemLimit()).To(Equal("348MiB"))
		})

		It("returns an empty string when neither a value nor a limit is set", func() {
			rr := ResourceRequirementsWithGoMemLimit{}
			Expect(rr.EffectiveGoMemLimit()).To(BeEmpty())
		})
	})

	Describe("collectorGoMemLimitInversions", func() {
		// The soft limit derived for a 500Mi limit is 372MiB (see the DeriveCollectorMemorySettings table above).
		configWithDaemonSetGoMemLimit := func(goMemLimit, limit string) ExtraConfig {
			ec := ExtraConfigDefaults
			ec.CollectorDaemonSetCollectorContainerResources = ResourceRequirementsWithGoMemLimit{
				Limits:     corev1.ResourceList{corev1.ResourceMemory: resource.MustParse(limit)},
				GoMemLimit: goMemLimit,
			}
			return ec
		}

		It("reports no inversion when the explicit GOMEMLIMIT is below the soft limit", func() {
			Expect(configWithDaemonSetGoMemLimit("300MiB", "500Mi").collectorGoMemLimitInversions()).To(BeEmpty())
		})

		It("reports an inversion when the explicit GOMEMLIMIT equals the soft limit", func() {
			inversions := configWithDaemonSetGoMemLimit("372MiB", "500Mi").collectorGoMemLimitInversions()
			Expect(inversions).To(HaveLen(1))
			Expect(inversions[0].Collector).To(Equal("daemonset collector"))
			Expect(inversions[0].GoMemLimit).To(Equal("372MiB"))
			Expect(inversions[0].SoftMiB).To(Equal(372))
		})

		It("reports an inversion when the explicit GOMEMLIMIT exceeds the soft limit", func() {
			inversions := configWithDaemonSetGoMemLimit("450MiB", "500Mi").collectorGoMemLimitInversions()
			Expect(inversions).To(HaveLen(1))
			Expect(inversions[0].SoftMiB).To(Equal(372))
		})

		It("recognises the GiB unit when comparing against the soft limit", func() {
			// 1GiB = 1024MiB, below the 1742MiB soft limit derived for a 2Gi container.
			Expect(configWithDaemonSetGoMemLimit("1GiB", "2Gi").collectorGoMemLimitInversions()).To(BeEmpty())
		})

		It("skips a collector with no explicit GOMEMLIMIT", func() {
			Expect(configWithDaemonSetGoMemLimit("", "500Mi").collectorGoMemLimitInversions()).To(BeEmpty())
		})

		It("skips a collector whose limit is too small to derive a soft limit", func() {
			Expect(configWithDaemonSetGoMemLimit("300MiB", "128Mi").collectorGoMemLimitInversions()).To(BeEmpty())
		})

		It("skips a collector with an unparseable GOMEMLIMIT", func() {
			Expect(configWithDaemonSetGoMemLimit("not-a-number", "500Mi").collectorGoMemLimitInversions()).To(BeEmpty())
		})
	})

	DescribeTable("parseGoMemLimitMiB",
		func(value string, expectMiB int, expectOk bool) {
			mib, ok := parseGoMemLimitMiB(value)
			Expect(ok).To(Equal(expectOk))
			if expectOk {
				Expect(mib).To(Equal(expectMiB))
			}
		},
		Entry("MiB suffix", "348MiB", 348, true),
		Entry("GiB suffix", "2GiB", 2048, true),
		Entry("TiB suffix", "1TiB", 1024*1024, true),
		Entry("KiB suffix", "1024KiB", 1, true),
		Entry("plain byte suffix", "2097152B", 2, true),
		Entry("bare byte count", "2097152", 2, true),
		Entry("fractional value is rejected", "12.5MiB", 0, false),
		Entry("non-numeric value is rejected", "abc", 0, false),
		Entry("empty value is rejected", "", 0, false),
	)

	DescribeTable("DeriveGoMemLimitFromLimit",
		func(limit string, percent, expectMiB int, expectOk bool) {
			goMemLimit, ok := DeriveGoMemLimitFromLimit(resource.MustParse(limit), percent)
			Expect(ok).To(Equal(expectOk))
			if expectOk {
				Expect(goMemLimit).To(Equal(strconv.Itoa(expectMiB) + "MiB"))
			}
		},
		// The 8MiB headroom floor dominates below ~40Mi, the percentage above it.
		Entry("config reloader (26Mi, floor-bound)", "26Mi", GoMemLimitDefaultPercent, 18, true),
		Entry("filelog offset sync (32Mi, floor-bound)", "32Mi", GoMemLimitDefaultPercent, 24, true),
		Entry("target-allocator (500Mi)", "500Mi", GoMemLimitDefaultPercent, 400, true),
		Entry("edge-proxy (512Mi)", "512Mi", GoMemLimitDefaultPercent, 409, true),
		Entry("agent0-connector (256Mi, 60%)", "256Mi", Agent0ConnectorGoMemLimitPercent, 153, true),
		Entry("percentage dominates well above the floor", "256Mi", GoMemLimitDefaultPercent, 204, true),
		Entry("floor leaves a sliver at a tiny limit", "10Mi", GoMemLimitDefaultPercent, 2, true),
		Entry("limit equal to the floor -> false", "8Mi", GoMemLimitDefaultPercent, 0, false),
		Entry("no limit -> false", "0", GoMemLimitDefaultPercent, 0, false),
	)

	Describe("EffectiveGoMemLimitPercent", func() {
		It("returns an explicitly configured value verbatim", func() {
			rr := ResourceRequirementsWithGoMemLimit{
				Limits:     corev1.ResourceList{corev1.ResourceMemory: resource.MustParse("500Mi")},
				GoMemLimit: "123MiB",
			}
			Expect(rr.EffectiveGoMemLimitPercent(GoMemLimitDefaultPercent)).To(Equal("123MiB"))
		})

		It("derives the value from the memory limit when none is configured", func() {
			rr := ResourceRequirementsWithGoMemLimit{
				Limits: corev1.ResourceList{corev1.ResourceMemory: resource.MustParse("256Mi")},
			}
			Expect(rr.EffectiveGoMemLimitPercent(Agent0ConnectorGoMemLimitPercent)).To(Equal("153MiB"))
		})

		It("returns an empty string when neither a value nor a limit is set", func() {
			rr := ResourceRequirementsWithGoMemLimit{}
			Expect(rr.EffectiveGoMemLimitPercent(GoMemLimitDefaultPercent)).To(BeEmpty())
		})
	})
})
