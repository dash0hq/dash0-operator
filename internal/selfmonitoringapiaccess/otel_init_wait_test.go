// SPDX-FileCopyrightText: Copyright 2025 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package selfmonitoringapiaccess

import (
	"github.com/dash0hq/dash0-operator/images/pkg/common"
	"github.com/go-logr/logr"

	otelmetric "go.opentelemetry.io/otel/metric"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

type DummyClient struct {
	hasBeenCalled int
}

func (c *DummyClient) InitializeSelfMonitoringMetrics(_ otelmetric.Meter, _ string, _ *logr.Logger) {
	c.hasBeenCalled++
}

var _ = Describe("The OTel SDK starter", func() {
	It("should call its clients when the config becomes available", func() {
		oTelSdkStarter := NewOTelSdkStarter()
		dummyClient := &DummyClient{}
		oTelSdkStarter.WaitForOTelConfig([]SelfMonitoringClient{dummyClient})
		oTelSdkStarter.UpdateConfig(&common.OTelSdkConfig{})
		Eventually(func(g Gomega) {
			g.Expect(dummyClient.hasBeenCalled).To(Equal(1))
		}).Should(Succeed())
	})
})
