// SPDX-FileCopyrightText: Copyright 2026 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package startup

import (
	"os"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	appsv1 "k8s.io/api/apps/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/scheme"
	ctrl "sigs.k8s.io/controller-runtime"
	crzap "sigs.k8s.io/controller-runtime/pkg/log/zap"

	"github.com/dash0hq/dash0-operator/internal/util"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	. "github.com/dash0hq/dash0-operator/test/util"
)

var _ = Describe("operator manager startup", func() {
	var mgr ctrl.Manager

	BeforeEach(func() {
		var err error
		mgr, err = ctrl.NewManager(cfg, ctrl.Options{Scheme: scheme.Scheme})
		Expect(err).NotTo(HaveOccurred())
		Expect(mgr).NotTo(BeNil())
	})

	Context("agent0 connector", func() {
		It("does not create the manager or register the controller when the agent0-connector feature is disabled", func() {
			envVars := environmentVariables{
				operatorNamespace:       OperatorNamespace,
				oTelCollectorNamePrefix: "dash0-operator-test",
				agent0ConnectorEnabled:  false,
			}
			agent0ConnectorManager, err := setupAgent0ConnectorManager(
				mgr,
				k8sClient,
				envVars,
				util.Images{},
				&appsv1.Deployment{},
				"cluster-uid",
				false,
			)
			Expect(err).NotTo(HaveOccurred())
			Expect(agent0ConnectorManager).To(BeNil())
		})

		It("creates the manager and registers the controller when the agent0-connector feature is enabled", func() {
			envVars := environmentVariables{
				operatorNamespace:            OperatorNamespace,
				oTelCollectorNamePrefix:      "dash0-operator-test",
				agent0ConnectorEnabled:       true,
				agent0ConnectorServerAddress: "example.com:4317",
			}
			agent0ConnectorManager, err := setupAgent0ConnectorManager(
				mgr,
				k8sClient,
				envVars,
				util.Images{},
				&appsv1.Deployment{},
				types.UID("cluster-uid"),
				false,
			)
			Expect(err).NotTo(HaveOccurred())
			Expect(agent0ConnectorManager).NotTo(BeNil())
		})
	})

	Context("logging", func() {
		DescribeTable("mirrors the resolved stdout log level onto the OTel bridge to Dash0",
			func(level zapcore.Level) {
				wrapper := setUpLogging(crzap.Level(zap.NewAtomicLevelAt(level)))
				bridge := wrapper.RootDelegatingZapCore

				// No delegate is set yet, so Enabled(lvl) reflects the buffering level: lvl >= dc.level.
				Expect(bridge.Enabled(level)).To(BeTrue())
				Expect(bridge.Enabled(level - 1)).To(BeFalse())
			},
			Entry("debug", zapcore.DebugLevel),
			Entry("info", zapcore.InfoLevel),
			Entry("warn", zapcore.WarnLevel),
			Entry("error", zapcore.ErrorLevel),
		)
	})

	Context("readKubeletStatsReceiverConfigFromEnv", func() {
		AfterEach(func() {
			for _, key := range []string{
				kubeletStatsAutoDetectEndpointEnvVarName,
				kubeletStatsEndpointEnvVarName,
				kubeletStatsAuthTypeEnvVarName,
				kubeletStatsInsecureSkipVerifyEnvVarName,
			} {
				Expect(os.Unsetenv(key)).To(Succeed())
			}
		})

		type readKubeletStatsEnvTest struct {
			env                map[string]string
			expectedAutoDetect bool
			expectedConfig     *util.KubeletStatsReceiverConfig
		}

		DescribeTable("derives the auto-detection flag and fixed config from the environment",
			func(t readKubeletStatsEnvTest) {
				for key, value := range t.env {
					Expect(os.Setenv(key, value)).To(Succeed())
				}

				autoDetect, config := readKubeletStatsReceiverConfigFromEnv()

				Expect(autoDetect).To(Equal(t.expectedAutoDetect))
				if t.expectedConfig == nil {
					Expect(config).To(BeNil())
				} else {
					Expect(config).To(Equal(t.expectedConfig))
				}
			},
			Entry("enables auto-detection when no env vars are set", readKubeletStatsEnvTest{
				env:                map[string]string{},
				expectedAutoDetect: true,
				expectedConfig:     nil,
			}),
			Entry(`keeps auto-detection enabled for the explicit flag "true"`, readKubeletStatsEnvTest{
				env:                map[string]string{kubeletStatsAutoDetectEndpointEnvVarName: "true"},
				expectedAutoDetect: true,
				expectedConfig:     nil,
			}),
			Entry(`keeps auto-detection enabled for any value other than "false"`, readKubeletStatsEnvTest{
				env:                map[string]string{kubeletStatsAutoDetectEndpointEnvVarName: "0"},
				expectedAutoDetect: true,
				expectedConfig:     nil,
			}),
			Entry(`disables auto-detection and reads the fixed config for "false"`, readKubeletStatsEnvTest{
				env: map[string]string{
					kubeletStatsAutoDetectEndpointEnvVarName: "false",
					kubeletStatsEndpointEnvVarName:           "https://${env:K8S_NODE_IP}:10250",
					kubeletStatsAuthTypeEnvVarName:           "serviceAccount",
					kubeletStatsInsecureSkipVerifyEnvVarName: "true",
				},
				expectedAutoDetect: false,
				expectedConfig: &util.KubeletStatsReceiverConfig{
					Enabled:            true,
					Endpoint:           "https://${env:K8S_NODE_IP}:10250",
					AuthType:           "serviceAccount",
					InsecureSkipVerify: true,
				},
			}),
			Entry("parses the auto-detection flag case-insensitively", readKubeletStatsEnvTest{
				env: map[string]string{
					kubeletStatsAutoDetectEndpointEnvVarName: "FALSE",
					kubeletStatsEndpointEnvVarName:           "http://${env:K8S_NODE_IP}:10255",
					kubeletStatsAuthTypeEnvVarName:           "none",
				},
				expectedAutoDetect: false,
				expectedConfig: &util.KubeletStatsReceiverConfig{
					Enabled:            true,
					Endpoint:           "http://${env:K8S_NODE_IP}:10255",
					AuthType:           "none",
					InsecureSkipVerify: false,
				},
			}),
			Entry("parses insecureSkipVerify case-insensitively", readKubeletStatsEnvTest{
				env: map[string]string{
					kubeletStatsAutoDetectEndpointEnvVarName: "false",
					kubeletStatsEndpointEnvVarName:           "https://${env:K8S_NODE_IP}:10250",
					kubeletStatsAuthTypeEnvVarName:           "serviceAccount",
					kubeletStatsInsecureSkipVerifyEnvVarName: "TRUE",
				},
				expectedAutoDetect: false,
				expectedConfig: &util.KubeletStatsReceiverConfig{
					Enabled:            true,
					Endpoint:           "https://${env:K8S_NODE_IP}:10250",
					AuthType:           "serviceAccount",
					InsecureSkipVerify: true,
				},
			}),
			Entry("defaults insecureSkipVerify to false when it is unset", readKubeletStatsEnvTest{
				env: map[string]string{
					kubeletStatsAutoDetectEndpointEnvVarName: "false",
					kubeletStatsEndpointEnvVarName:           "https://${env:K8S_NODE_IP}:10250",
					kubeletStatsAuthTypeEnvVarName:           "serviceAccount",
				},
				expectedAutoDetect: false,
				expectedConfig: &util.KubeletStatsReceiverConfig{
					Enabled:            true,
					Endpoint:           "https://${env:K8S_NODE_IP}:10250",
					AuthType:           "serviceAccount",
					InsecureSkipVerify: false,
				},
			}),
			// The function itself does not validate the endpoint; it returns a non-nil config with an empty endpoint and
			// relies on determineKubeletstatsReceiverEndpoint to disable the receiver in that case (see the tests in
			// otelcol_resources_test.go). This documents that boundary.
			Entry("returns a non-nil config with an empty endpoint when only the flag is set", readKubeletStatsEnvTest{
				env: map[string]string{
					kubeletStatsAutoDetectEndpointEnvVarName: "false",
				},
				expectedAutoDetect: false,
				expectedConfig: &util.KubeletStatsReceiverConfig{
					Enabled:            true,
					Endpoint:           "",
					AuthType:           "",
					InsecureSkipVerify: false,
				},
			}),
		)
	})
})

var _ = Describe("validateOtlpHostPorts", func() {
	It("accepts the default ports", func() {
		Expect(validateOtlpHostPorts(40317, 40318)).To(Succeed())
	})

	It("accepts the standard OTLP ports", func() {
		Expect(validateOtlpHostPorts(4317, 4318)).To(Succeed())
	})

	It("names the Helm value that is out of range", func() {
		Expect(validateOtlpHostPorts(70000, 40318)).To(
			MatchError("operator.collectors.otlpGrpcHostPort must be in the range 1-65535, but was 70000"))
		Expect(validateOtlpHostPorts(40317, 0)).To(
			MatchError("operator.collectors.otlpHttpHostPort must be in the range 1-65535, but was 0"))
	})

	It("names both Helm values when the ports are equal", func() {
		Expect(validateOtlpHostPorts(4317, 4317)).To(MatchError(
			"operator.collectors.otlpGrpcHostPort and operator.collectors.otlpHttpHostPort must be different, " +
				"both are set to 4317"))
	})

	DescribeTable("rejects invalid input",
		func(grpcPort int, httpPort int) {
			Expect(validateOtlpHostPorts(grpcPort, httpPort)).To(HaveOccurred())
		},
		Entry("grpc port is zero", 0, 40318),
		Entry("grpc port is negative", -1, 40318),
		Entry("grpc port is above the valid range", 65536, 40318),
		Entry("http port is zero", 40317, 0),
		Entry("http port is above the valid range", 40317, 65536),
		Entry("grpc and http ports are equal", 4317, 4317),
	)
})
