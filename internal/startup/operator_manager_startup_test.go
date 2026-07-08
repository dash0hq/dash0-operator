// SPDX-FileCopyrightText: Copyright 2026 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package startup

import (
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
})
