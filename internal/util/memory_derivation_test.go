// SPDX-FileCopyrightText: Copyright 2026 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package util

import (
	"strconv"
	"strings"

	"github.com/go-logr/logr/funcr"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"

	dash0v1alpha1 "github.com/dash0hq/dash0-operator/api/operator/v1alpha1"
	"github.com/dash0hq/dash0-operator/internal/util/logd"

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

	Describe("GoMemLimitInversionReporter", func() {
		var logged []string
		var recordingLogger logd.Logger

		BeforeEach(func() {
			logged = nil
			recordingLogger = logd.NewLogger(funcr.New(func(_ string, args string) {
				logged = append(logged, args)
			}, funcr.Options{}))
		})

		configWithDaemonSetGoMemLimit := func(goMemLimit string) ExtraConfig {
			ec := ExtraConfigDefaults
			ec.CollectorDaemonSetCollectorContainerResources = ResourceRequirementsWithGoMemLimit{
				Limits:     corev1.ResourceList{corev1.ResourceMemory: resource.MustParse("500Mi")},
				GoMemLimit: goMemLimit,
			}
			return ec
		}

		allDeployed := DeployedCollectors{DaemonSetCollector: true, DeploymentCollector: true, SignalControlCollector: true}
		withDaemonSetCollectorResources := &dash0v1alpha1.OperatorConfigurationComponents{
			DaemonSetCollector: &dash0v1alpha1.DaemonSetCollectorSettings{
				CollectorContainerResources: &dash0v1alpha1.ContainerResourcesWithGoMemLimit{},
			},
		}

		// sources describes an effective configuration with the given GOMEMLIMIT for the DaemonSet collector, whose
		// resources come from spec.components.
		sources := func(goMemLimit string) GoMemLimitInversionSources {
			return GoMemLimitInversionSources{
				EffectiveConfig:                 configWithDaemonSetGoMemLimit(goMemLimit),
				Deployed:                        allDeployed,
				OperatorConfigurationComponents: withDaemonSetCollectorResources,
			}
		}

		It("logs an inversion that spec.components introduces only once while it stays the same", func() {
			reporter := &GoMemLimitInversionReporter{}
			reporter.Report(sources("450MiB"), recordingLogger)
			Expect(logged).To(HaveLen(1))
			Expect(logged[0]).To(ContainSubstring("450MiB"))
			Expect(logged[0]).To(ContainSubstring(`"setting"="Dash0OperatorConfiguration ` +
				`spec.components.daemonSetCollector.collectorContainerResources.gomemlimit"`))
			reporter.Report(sources("450MiB"), recordingLogger)
			Expect(logged).To(HaveLen(1))
			Expect(HasCollectorGoMemLimitInversions(sources("450MiB"))).To(BeTrue())
		})

		It("logs an inversion of the extra config map with the name of the Helm value", func() {
			reporter := &GoMemLimitInversionReporter{}
			helmOnly := sources("450MiB")
			helmOnly.OperatorConfigurationComponents = &dash0v1alpha1.OperatorConfigurationComponents{
				DeploymentCollector: &dash0v1alpha1.DeploymentCollectorSettings{
					CollectorContainerResources: &dash0v1alpha1.ContainerResourcesWithGoMemLimit{},
				},
			}
			reporter.Report(helmOnly, recordingLogger)
			Expect(logged).To(HaveLen(1))
			Expect(logged[0]).To(ContainSubstring(`"setting"="operator.collectors.daemonSetCollectorContainerResources ` +
				`(or operator.collectorDaemonSetCollectorContainerResources)"`))
			Expect(HasCollectorGoMemLimitInversions(helmOnly)).To(BeTrue())
		})

		It("does not log an inversion that the effective configuration does not have", func() {
			reporter := &GoMemLimitInversionReporter{}
			reporter.Report(sources("300MiB"), recordingLogger)
			Expect(logged).To(BeEmpty())
			Expect(HasCollectorGoMemLimitInversions(sources("300MiB"))).To(BeFalse())
		})

		It("does not log an inversion of a collector that is not deployed", func() {
			config := ExtraConfigDefaults
			config.SignalControlCollectorContainerResources = ResourceRequirementsWithGoMemLimit{
				Limits:     corev1.ResourceList{corev1.ResourceMemory: resource.MustParse("500Mi")},
				GoMemLimit: "450MiB",
			}
			notDeployed := GoMemLimitInversionSources{
				EffectiveConfig: config,
				Deployed:        DeployedCollectors{DaemonSetCollector: true, DeploymentCollector: true},
				SignalControlComponents: &dash0v1alpha1.SignalControlComponents{
					Collector: &dash0v1alpha1.SignalControlCollectorSettings{
						CollectorContainerResources: &dash0v1alpha1.ContainerResourcesWithGoMemLimit{},
					},
				},
			}
			reporter := &GoMemLimitInversionReporter{}
			reporter.Report(notDeployed, recordingLogger)
			Expect(logged).To(BeEmpty())
			Expect(HasCollectorGoMemLimitInversions(notDeployed)).To(BeFalse())

			deployed := notDeployed
			deployed.Deployed = allDeployed
			reporter.Report(deployed, recordingLogger)
			Expect(logged).To(HaveLen(1))
			Expect(logged[0]).To(ContainSubstring(`"setting"="Dash0SignalControl ` +
				`spec.components.collector.collectorContainerResources.gomemlimit"`))
		})

		It("describes every main collector", func() {
			collectors := goMemLimitCollectors(GoMemLimitInversionSources{})
			for name := range (&ExtraConfig{}).mainCollectorMemoryResources() {
				Expect(collectors).To(HaveKey(name))
			}
		})

		It("logs again when the inversion changes", func() {
			reporter := &GoMemLimitInversionReporter{}
			reporter.Report(sources("450MiB"), recordingLogger)
			reporter.Report(sources("400MiB"), recordingLogger)
			Expect(logged).To(HaveLen(2))
			Expect(logged[1]).To(ContainSubstring("400MiB"))
		})

		It("logs an inversion again after it has been resolved in between", func() {
			reporter := &GoMemLimitInversionReporter{}
			reporter.Report(sources("450MiB"), recordingLogger)
			reporter.Report(sources("300MiB"), recordingLogger)
			Expect(logged).To(HaveLen(1))
			reporter.Report(sources("450MiB"), recordingLogger)
			Expect(logged).To(HaveLen(2))
		})

		It("logs an unchanged inversion again after a reset", func() {
			reporter := &GoMemLimitInversionReporter{}
			reporter.Report(sources("450MiB"), recordingLogger)
			reporter.Reset()
			reporter.Report(sources("450MiB"), recordingLogger)
			Expect(logged).To(HaveLen(2))
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

	DescribeTable("parseGoMemLimitBytes",
		func(value string, expectBytes int64, expectOk bool) {
			bytes, ok := parseGoMemLimitBytes(value)
			Expect(ok).To(Equal(expectOk))
			if expectOk {
				Expect(bytes).To(Equal(expectBytes))
			}
		},
		Entry("MiB suffix", "348MiB", int64(348<<20), true),
		Entry("GiB suffix", "2GiB", int64(2<<30), true),
		Entry("plain byte suffix", "2097152B", int64(2097152), true),
		Entry("bare byte count", "2097152", int64(2097152), true),
		Entry("fractional value is rejected", "12.5MiB", int64(0), false),
		Entry("empty value is rejected", "", int64(0), false),
		Entry("off is rejected", "off", int64(0), false),
		Entry("negative value is rejected", "-1MiB", int64(0), false),
		Entry("decimal unit is rejected", "400MB", int64(0), false),
		Entry("overflow is rejected", "8589934592TiB", int64(0), false),
		Entry("whitespace is rejected", "1 GiB", int64(0), false),
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
