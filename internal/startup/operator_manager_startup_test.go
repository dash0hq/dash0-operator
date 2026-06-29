// SPDX-FileCopyrightText: Copyright 2026 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package startup

import (
	appsv1 "k8s.io/api/apps/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/scheme"
	ctrl "sigs.k8s.io/controller-runtime"

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
})
