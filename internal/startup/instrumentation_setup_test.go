// SPDX-FileCopyrightText: Copyright 2026 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package startup

import (
	"context"
	"net/http"
	"net/http/httptest"

	"k8s.io/client-go/kubernetes/scheme"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/manager"

	dash0v1alpha1 "github.com/dash0hq/dash0-operator/api/operator/v1alpha1"
	"github.com/dash0hq/dash0-operator/internal/util"
	"github.com/dash0hq/dash0-operator/internal/util/logd"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	. "github.com/dash0hq/dash0-operator/test/util"
)

// runnableRecordingManager records the runnables added to the manager instead of running them.
type runnableRecordingManager struct {
	ctrl.Manager
	addedRunnables []manager.Runnable
}

func (m *runnableRecordingManager) Add(runnable manager.Runnable) error {
	m.addedRunnables = append(m.addedRunnables, runnable)
	return nil
}

var _ = Describe("instrumentation setup", func() {
	var mgr *runnableRecordingManager

	clusterInstrumentationConfig := func(instrumentationEnabled bool) *util.ClusterInstrumentationConfig {
		return util.NewClusterInstrumentationConfig(
			TestImages,
			PossibleCollectorUrlsTest,
			OTelCollectorNodeLocalBaseUrlTest,
			util.ExtraConfigDefaults,
			dash0v1alpha1.InstrumentationDeliveryInitContainer,
			nil,
			false,
			false,
			false,
			instrumentationEnabled,
		)
	}

	BeforeEach(func() {
		realManager, err := ctrl.NewManager(cfg, ctrl.Options{Scheme: scheme.Scheme})
		Expect(err).NotTo(HaveOccurred())
		mgr = &runnableRecordingManager{Manager: realManager}
	})

	Describe("setupInstrumenters", func() {
		It("registers the instrument-at-startup task if workload instrumentation is enabled", func() {
			instrumenter, err := setupInstrumenters(
				mgr,
				k8sClient,
				nil,
				clusterInstrumentationConfig(true),
				func(context.Context, logd.Logger) {},
			)
			Expect(err).NotTo(HaveOccurred())
			Expect(instrumenter).NotTo(BeNil())
			Expect(mgr.addedRunnables).To(HaveLen(1))
			Expect(mgr.addedRunnables[0]).To(BeAssignableToTypeOf(&InstrumentAtStartupRunnable{}))
		})

		It("does not register the instrument-at-startup task if workload instrumentation is disabled", func() {
			instrumenter, err := setupInstrumenters(
				mgr,
				k8sClient,
				nil,
				clusterInstrumentationConfig(false),
				func(context.Context, logd.Logger) {},
			)
			Expect(err).NotTo(HaveOccurred())
			Expect(instrumenter).NotTo(BeNil())
			Expect(mgr.addedRunnables).To(BeEmpty())
		})
	})

	Describe("setupInstrumentationWebhook", func() {
		registeredPattern := func() string {
			webhookMux := mgr.GetWebhookServer().WebhookMux()
			if webhookMux == nil {
				// The webhook server only creates its mux when the first handler is registered.
				return ""
			}
			_, pattern := webhookMux.Handler(
				httptest.NewRequest(http.MethodPost, "/workloads/inject", nil),
			)
			return pattern
		}

		It("registers the instrumentation webhook handler if workload instrumentation is enabled", func() {
			Expect(setupInstrumentationWebhook(mgr, k8sClient, clusterInstrumentationConfig(true))).To(Succeed())
			Expect(registeredPattern()).To(Equal("/workloads/inject"))
		})

		It("does not register the instrumentation webhook handler if workload instrumentation is disabled", func() {
			Expect(setupInstrumentationWebhook(mgr, k8sClient, clusterInstrumentationConfig(false))).To(Succeed())
			Expect(registeredPattern()).To(BeEmpty())
		})
	})
})
