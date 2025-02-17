// SPDX-FileCopyrightText: Copyright 2025 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package selfmonitoringapiaccess

import (
	"context"
	"time"

	"github.com/dash0hq/dash0-operator/images/pkg/common"
	"github.com/dash0hq/dash0-operator/internal/util"
	"github.com/go-logr/logr"
	otelmetric "go.opentelemetry.io/otel/metric"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/log"

	dash0v1alpha1 "github.com/dash0hq/dash0-operator/api/dash0monitoring/v1alpha1"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	. "github.com/dash0hq/dash0-operator/test/util"
)

type DummyClient struct {
	hasBeenCalled int
}

const (
	defaultTimeout = 50 * time.Millisecond
)

func (c *DummyClient) InitializeSelfMonitoringMetrics(_ otelmetric.Meter, _ string, _ *logr.Logger) {
	c.hasBeenCalled++
}

var _ = Describe("The OTel SDK starter", func() {

	ctx := context.Background()
	logger := ptr.To(log.FromContext(ctx))

	It("should start with empty values", func() {
		oTelSdkStarter := NewOTelSdkStarter()
		Expect(oTelSdkStarter.sdkIsActive.Load()).To(BeFalse())
		Expect(*oTelSdkStarter.oTelSdkConfigInput.Load()).To(Equal(OTelSdkConfigInput{}))
		Expect(oTelSdkStarter.activeOTelSdkConfig.Load()).To(BeNil())
		Expect(*oTelSdkStarter.authTokenFromSecretRef.Load()).To(Equal(""))
	})

	Describe("should start and stop when pre-conditions are met / no longer met", func() {
		var oTelSdkStarter *OTelSdkStarter
		var mockChannel chan *common.OTelSdkConfig

		BeforeEach(func() {
			oTelSdkStarter = NewOTelSdkStarter()
			mockChannel = make(chan *common.OTelSdkConfig, 100)
			oTelSdkStarter.startOrRestartOTelSdkChannel = mockChannel
		})

		It("should start when there is an export with auth token", func() {
			oTelSdkStarter.SetOTelSdkParameters(
				ctx,
				*Dash0ExportWithEndpointAndToken(),
				OperatorManagerDeploymentUID,
				OperatorVersionTest,
				false,
				logger,
			)
			config := readFromChannelWithTimeout(oTelSdkStarter, mockChannel)
			Expect(config).NotTo(BeNil())
			Expect(config.Endpoint).To(Equal(EndpointDash0Test))
			Expect(config.Protocol).To(Equal(common.ProtocolGrpc))
			Expect(config.Headers).To(HaveLen(1))
			Expect(config.Headers[util.AuthorizationHeaderName]).To(Equal(AuthorizationHeaderTest))
			Expect(config.LogLevel).To(BeEmpty())
		})

		It("should not start when there is an export with a secret ref and the auth token has not been resolved yet", func() {
			oTelSdkStarter.SetOTelSdkParameters(
				ctx,
				*Dash0ExportWithEndpointAndSecretRef(),
				OperatorManagerDeploymentUID,
				OperatorVersionTest,
				false,
				logger,
			)
			config := readFromChannelWithTimeout(oTelSdkStarter, mockChannel)
			Expect(config).To(BeNil())
		})

		It("should start when there is an export with a secret ref and the auth token has been resolved", func() {
			oTelSdkStarter.SetOTelSdkParameters(
				ctx,
				*Dash0ExportWithEndpointAndSecretRef(),
				OperatorManagerDeploymentUID,
				OperatorVersionTest,
				false,
				logger,
			)
			oTelSdkStarter.SetAuthToken(ctx, AuthorizationTokenTest, logger)
			config := readFromChannelWithTimeout(oTelSdkStarter, mockChannel)
			Expect(config).NotTo(BeNil())
			Expect(config.Endpoint).To(Equal(EndpointDash0Test))
			Expect(config.Protocol).To(Equal(common.ProtocolGrpc))
			Expect(config.Headers).To(HaveLen(1))
			Expect(config.Headers[util.AuthorizationHeaderName]).To(Equal(AuthorizationHeaderTest))
			Expect(config.LogLevel).To(BeEmpty())
		})

		It("should stop when the export is removed", func() {
			oTelSdkStarter.SetOTelSdkParameters(
				ctx,
				*Dash0ExportWithEndpointAndToken(),
				OperatorManagerDeploymentUID,
				OperatorVersionTest,
				false,
				logger,
			)
			config := readFromChannelWithTimeout(oTelSdkStarter, mockChannel)
			Expect(config).NotTo(BeNil())

			oTelSdkStarter.RemoveOTelSdkParameters(ctx, logger)
			Expect(oTelSdkStarter.sdkIsActive.Load()).To(BeFalse())
		})

		It("should stop when the auth token from the resolved secret ref is removed", func() {
			oTelSdkStarter.SetOTelSdkParameters(
				ctx,
				*Dash0ExportWithEndpointAndSecretRef(),
				OperatorManagerDeploymentUID,
				OperatorVersionTest,
				false,
				logger,
			)
			oTelSdkStarter.SetAuthToken(ctx, AuthorizationTokenTest, logger)
			config := readFromChannelWithTimeout(oTelSdkStarter, mockChannel)
			Expect(config).NotTo(BeNil())

			oTelSdkStarter.RemoveAuthToken(ctx, logger)
			Expect(oTelSdkStarter.sdkIsActive.Load()).To(BeFalse())
		})

		It("should not stop when the auth token from the resolved secret ref is removed but the export has a token", func() {
			oTelSdkStarter.SetOTelSdkParameters(
				ctx,
				*Dash0ExportWithEndpointAndToken(),
				OperatorManagerDeploymentUID,
				OperatorVersionTest,
				false,
				logger,
			)
			oTelSdkStarter.SetAuthToken(ctx, AuthorizationTokenTest, logger)
			config := readFromChannelWithTimeout(oTelSdkStarter, mockChannel)
			Expect(config).NotTo(BeNil())

			oTelSdkStarter.RemoveAuthToken(ctx, logger)
			Expect(oTelSdkStarter.sdkIsActive.Load()).To(BeTrue())
		})

		It("should restart when endpoint changes", func() {
			oTelSdkStarter.SetOTelSdkParameters(
				ctx,
				*Dash0ExportWithEndpointAndToken(),
				OperatorManagerDeploymentUID,
				OperatorVersionTest,
				false,
				logger,
			)
			config := readFromChannelWithTimeout(oTelSdkStarter, mockChannel)
			Expect(config).NotTo(BeNil())
			Expect(config.Endpoint).To(Equal(EndpointDash0Test))

			oTelSdkStarter.SetOTelSdkParameters(
				ctx,
				dash0v1alpha1.Export{
					Dash0: &dash0v1alpha1.Dash0Configuration{
						Endpoint: EndpointDash0TestAlternative,
						Authorization: dash0v1alpha1.Authorization{
							Token: &AuthorizationTokenTest,
						},
					},
				},
				OperatorManagerDeploymentUID,
				OperatorVersionTest,
				false,
				logger,
			)
			// shutdown happens synchronously
			Expect(oTelSdkStarter.sdkIsActive.Load()).To(BeFalse())

			// restart happens asynchronously
			config = readFromChannelWithTimeout(oTelSdkStarter, mockChannel)
			Expect(config).NotTo(BeNil())
			Expect(config.Endpoint).To(Equal(EndpointDash0TestAlternative))
			Expect(config.Headers[util.AuthorizationHeaderName]).To(Equal(AuthorizationHeaderTest))
		})

		It("should restart when token changes", func() {
			oTelSdkStarter.SetOTelSdkParameters(
				ctx,
				*Dash0ExportWithEndpointAndToken(),
				OperatorManagerDeploymentUID,
				OperatorVersionTest,
				false,
				logger,
			)
			config := readFromChannelWithTimeout(oTelSdkStarter, mockChannel)
			Expect(config).NotTo(BeNil())
			Expect(config.Endpoint).To(Equal(EndpointDash0Test))

			oTelSdkStarter.SetOTelSdkParameters(
				ctx,
				dash0v1alpha1.Export{
					Dash0: &dash0v1alpha1.Dash0Configuration{
						Endpoint: EndpointDash0Test,
						Authorization: dash0v1alpha1.Authorization{
							Token: &AuthorizationTokenTestAlternative,
						},
					},
				},
				OperatorManagerDeploymentUID,
				OperatorVersionTest,
				false,
				logger,
			)
			// shutdown happens synchronously
			Expect(oTelSdkStarter.sdkIsActive.Load()).To(BeFalse())

			// restart happens asynchronously
			config = readFromChannelWithTimeout(oTelSdkStarter, mockChannel)
			Expect(config).NotTo(BeNil())
			Expect(config.Endpoint).To(Equal(EndpointDash0Test))
			Expect(config.Headers[util.AuthorizationHeaderName]).To(Equal(AuthorizationHeaderTestAlternative))
		})

	})

	Describe("should notify self monitoring clients", func() {
		var oTelSdkStarter *OTelSdkStarter

		BeforeEach(func() {
			oTelSdkStarter = NewOTelSdkStarter()
		})

		It("should notify clients when there is an export with auth token", func() {
			dummyClient1 := &DummyClient{}
			dummyClient2 := &DummyClient{}
			oTelSdkStarter.WaitForOTelConfig([]SelfMonitoringClient{
				dummyClient1,
				dummyClient2,
			})

			oTelSdkStarter.SetOTelSdkParameters(
				ctx,
				*Dash0ExportWithEndpointAndToken(),
				OperatorManagerDeploymentUID,
				OperatorVersionTest,
				false,
				logger,
			)

			Eventually(func(g Gomega) {
				g.Expect(dummyClient1.hasBeenCalled).To(Equal(1))
				g.Expect(dummyClient2.hasBeenCalled).To(Equal(1))
			}).Should(Succeed())
		})
	})
})

func readFromChannelWithTimeout(
	oTelSdkStarter *OTelSdkStarter,
	mockChannel <-chan *common.OTelSdkConfig,
) *common.OTelSdkConfig {
	select {
	case config := <-mockChannel:
		// Simulate parts of what waitForCompleteOTelSDKConfiguration does when it receives the config. We override the
		// channel so these state changes are not executed in tests.
		oTelSdkStarter.UpdateOTelSdkState(true)
		oTelSdkStarter.activeOTelSdkConfig.Store(config)
		return config
	case <-time.After(defaultTimeout):
		return nil
	}
}
