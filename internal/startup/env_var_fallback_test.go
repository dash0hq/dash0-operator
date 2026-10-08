// SPDX-FileCopyrightText: Copyright 2026 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package startup

import (
	"flag"
	"fmt"
	"os"
	"strings"

	crzap "sigs.k8s.io/controller-runtime/pkg/log/zap"

	"github.com/dash0hq/dash0-operator/images/pkg/common"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var envVarsSetInFallbackTests = []string{
	"DASH0_OPERATOR_CONFIGURATION_ENDPOINT",
	"DASH0_LOG_LEVEL",
	"DASH0_LEADER_ELECT",
	"DASH0_OTEL_COLLECTOR_OTLP_GRPC_HOST_PORT",
	"DASH0_INSTRUMENTATION_DELAY_AFTER_EACH_WORKLOAD_MILLIS",
	"DASH0_OPERATOR_CONFIGURATION_INSTRUMENTATION_DELIVERY",
	"DASH0_ZAP_LOG_LEVEL",
}

var _ = Describe("environment variable fallback for command-line flags", func() {
	AfterEach(func() {
		for _, key := range envVarsSetInFallbackTests {
			Expect(os.Unsetenv(key)).To(Succeed())
		}
	})

	Context("applyEnvironmentVariableDefaults", func() {
		It("applies an env var to a flag that was not set on the command line", func() {
			Expect(os.Setenv("DASH0_OPERATOR_CONFIGURATION_ENDPOINT", "https://example.com:4317")).To(Succeed())

			fs, cliArgs := freshOperatorFlagSet()
			Expect(fs.Parse(nil)).To(Succeed())
			applied, err := applyEnvironmentVariableDefaults(fs)

			Expect(err).NotTo(HaveOccurred())
			Expect(cliArgs.operatorConfigurationEndpoint).To(Equal("https://example.com:4317"))
			Expect(applied).To(ContainElement(appliedEnvVar{
				flag:   "operator-configuration-endpoint",
				envVar: "DASH0_OPERATOR_CONFIGURATION_ENDPOINT",
				value:  "https://example.com:4317",
			}))
		})

		It("prefers an explicit command-line argument over the env var", func() {
			Expect(os.Setenv("DASH0_LOG_LEVEL", "debug")).To(Succeed())

			fs, cliArgs := freshOperatorFlagSet()
			Expect(fs.Parse([]string{"--dash0-log-level=warn"})).To(Succeed())
			applied, err := applyEnvironmentVariableDefaults(fs)

			Expect(err).NotTo(HaveOccurred())
			Expect(cliArgs.logLevel).To(Equal("warn"))
			Expect(appliedFlagNames(applied)).NotTo(ContainElement("dash0-log-level"))
		})

		It("treats a present-but-empty env var as absent", func() {
			Expect(os.Setenv("DASH0_LEADER_ELECT", "")).To(Succeed())

			fs, cliArgs := freshOperatorFlagSet()
			Expect(fs.Parse(nil)).To(Succeed())
			applied, err := applyEnvironmentVariableDefaults(fs)

			Expect(err).NotTo(HaveOccurred())
			Expect(cliArgs.enableLeaderElection).To(BeFalse())
			Expect(appliedFlagNames(applied)).NotTo(ContainElement("leader-elect"))
		})

		It("applies an env var to a flag.Func flag", func() {
			Expect(os.Setenv("DASH0_OPERATOR_CONFIGURATION_INSTRUMENTATION_DELIVERY", "init-container")).To(Succeed())

			fs, cliArgs := freshOperatorFlagSet()
			Expect(fs.Parse(nil)).To(Succeed())
			applied, err := applyEnvironmentVariableDefaults(fs)

			Expect(err).NotTo(HaveOccurred())
			Expect(string(cliArgs.operatorConfigurationInstrumentationDelivery)).To(Equal("init-container"))
			Expect(appliedFlagNames(applied)).To(ContainElement("operator-configuration-instrumentation-delivery"))
		})

		It("never applies env vars to controller-runtime zap flags", func() {
			Expect(os.Setenv("DASH0_ZAP_LOG_LEVEL", "debug")).To(Succeed())

			fs := freshOperatorFlagSetWithZap()
			Expect(fs.Parse(nil)).To(Succeed())
			applied, err := applyEnvironmentVariableDefaults(fs)

			Expect(err).NotTo(HaveOccurred())
			Expect(appliedFlagNames(applied)).NotTo(ContainElement("zap-log-level"))
			Expect(fs.Lookup("zap-log-level").Value.String()).NotTo(Equal("debug"))
		})

		DescribeTable("returns an error when the env var value is invalid for the flag type",
			func(envVar, value string) {
				Expect(os.Setenv(envVar, value)).To(Succeed())

				fs, _ := freshOperatorFlagSet()
				Expect(fs.Parse(nil)).To(Succeed())
				_, err := applyEnvironmentVariableDefaults(fs)

				Expect(err).To(HaveOccurred())
			},
			Entry("invalid bool", "DASH0_LEADER_ELECT", "yes"),
			Entry("invalid int", "DASH0_OTEL_COLLECTOR_OTLP_GRPC_HOST_PORT", "abc"),
			Entry("invalid uint64", "DASH0_INSTRUMENTATION_DELAY_AFTER_EACH_WORKLOAD_MILLIS", "-1"),
		)
	})

	Context("envVarNameForFlag", func() {
		DescribeTable("derives the fallback env var name from the flag name",
			func(flagName, expected string) {
				Expect(envVarNameForFlag(flagName)).To(Equal(expected))
			},
			Entry("trims the dash0- prefix", "dash0-log-level", "DASH0_LOG_LEVEL"),
			Entry("adds the DASH0_ prefix to an unprefixed flag", "leader-elect", "DASH0_LEADER_ELECT"),
			Entry("uppercases and replaces hyphens",
				"operator-configuration-endpoint", "DASH0_OPERATOR_CONFIGURATION_ENDPOINT"),
			Entry("handles a long flag name",
				"instrumentation-delay-after-each-workload-millis",
				"DASH0_INSTRUMENTATION_DELAY_AFTER_EACH_WORKLOAD_MILLIS"),
		)
	})

	Context("loggableValue", func() {
		It("redacts the value of a token flag", func() {
			a := appliedEnvVar{flag: "operator-configuration-token", envVar: "DASH0_OPERATOR_CONFIGURATION_TOKEN", value: "secret"}
			Expect(a.loggableValue()).To(Equal("(redacted)"))
		})

		It("does not redact the value of a non-token flag", func() {
			a := appliedEnvVar{flag: "operator-configuration-endpoint", envVar: "DASH0_OPERATOR_CONFIGURATION_ENDPOINT", value: "https://example.com:4317"}
			Expect(a.loggableValue()).To(Equal("https://example.com:4317"))
		})
	})

	Context("derived name integrity across the full flag surface", func() {
		It("derives a unique env var name for every non-zap flag", func() {
			seen := map[string]string{}
			freshOperatorFlagSetWithZap().VisitAll(func(f *flag.Flag) {
				if strings.HasPrefix(f.Name, "zap-") {
					return
				}
				name := envVarNameForFlag(f.Name)
				if other, dup := seen[name]; dup {
					Fail(fmt.Sprintf("env var %s is derived from both --%s and --%s", name, other, f.Name))
				}
				seen[name] = f.Name
			})
		})

		It("derives no env var name that collides with a directly-read env var", func() {
			reserved := reservedEnvVarNames()
			freshOperatorFlagSetWithZap().VisitAll(func(f *flag.Flag) {
				if strings.HasPrefix(f.Name, "zap-") {
					return
				}
				name := envVarNameForFlag(f.Name)
				Expect(reserved).NotTo(
					HaveKey(name),
					"flag --%s derives to reserved env var %s", f.Name, name)
			})
		})
	})
})

func freshOperatorFlagSet() (*flag.FlagSet, *commandLineArguments) {
	fs := flag.NewFlagSet("test", flag.ContinueOnError)
	cliArgs := defineCommandLineArguments(fs)
	return fs, cliArgs
}

func freshOperatorFlagSetWithZap() *flag.FlagSet {
	fs, _ := freshOperatorFlagSet()
	var opts crzap.Options
	opts.BindFlags(fs)
	return fs
}

func appliedFlagNames(applied []appliedEnvVar) []string {
	names := make([]string, 0, len(applied))
	for _, a := range applied {
		names = append(names, a.flag)
	}
	return names
}

// reservedEnvVarNames returns the set of environment variables the operator reads directly (not via the flag fallback).
// It references the existing constants so the set tracks any change to their values.
func reservedEnvVarNames() map[string]bool {
	names := []string{
		operatorNamespaceEnvVarName,
		deploymentNameEnvVarName,
		webhookServiceNameEnvVarName,
		webhookServicePortEnvVarName,
		persesDashboardAutoPatchConversionWebhookEnvVarName,
		periodicRetryEnabledEnvVarName,
		periodicRetryIntervalEnvVarName,
		oTelCollectorNamePrefixEnvVarName,
		targetAllocatorNamePrefixEnvVarName,
		operatorImageEnvVarName,
		instrumentationImageEnvVarName,
		instrumentationImagePullPolicyEnvVarName,
		collectorImageEnvVarName,
		collectorImageImagePullPolicyEnvVarName,
		signalControlCollectorImageEnvVarName,
		signalControlCollectorImagePullPolicyEnvVarName,
		targetAllocatorImageEnvVarName,
		targetAllocatorImageImagePullPolicyEnvVarName,
		configurationReloaderImageEnvVarName,
		configurationReloaderImagePullPolicyEnvVarName,
		filelogOffsetSyncImageEnvVarName,
		filelogOffsetSyncImagePullPolicyEnvVarName,
		filelogOffsetVolumeOwnershipImageEnvVarName,
		filelogOffsetVolumeOwnershipImagePullPolicyEnvVarName,
		edgeProxyImageEnvVarName,
		edgeProxyImagePullPolicyEnvVarName,
		agent0ConnectorImageEnvVarName,
		agent0ConnectorImagePullPolicyEnvVarName,
		agent0ConnectorEnabledEnvVarName,
		agent0ConnectorServerAddressEnvVarName,
		agent0ConnectorInsecureEnvVarName,
		agent0ConnectorTokenEnvVarName,
		agent0ConnectorSecretRefNameEnvVarName,
		agent0ConnectorSecretRefKeyEnvVarName,
		k8sNodeIpEnvVarName,
		k8sNodeNameEnvVarName,
		k8sPodIpEnvVarName,
		developmentModeEnvVarName,
		instrumentationDebugEnvVarName,
		enablePythonAutoInstrumentationEnvVarName,
		enableRubyAutoInstrumentationEnvVarName,
		disableCollectorResourceWatchesEnvVarName,
		debugVerbosityDetailedEnvVarName,
		sendBatchSizeEnvVarName,
		sendBatchMaxSizeEnvVarName,
		k8sAttributesDisableReplicasetInformerEnvVarName,
		k8sAttributesWaitForMetadataEnvVarName,
		k8sAttributesWaitForMetadataTimeoutEnvVarName,
		enablePprofExtensionEnvVarName,
		compressConfigMapsEnvVarName,
		kubeletStatsAutoDetectEndpointEnvVarName,
		kubeletStatsEndpointEnvVarName,
		kubeletStatsAuthTypeEnvVarName,
		kubeletStatsInsecureSkipVerifyEnvVarName,
		common.PprofPortEnvVarName,
	}
	set := make(map[string]bool, len(names))
	for _, n := range names {
		set[n] = true
	}
	return set
}
