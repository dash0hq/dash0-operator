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
	"sigs.k8s.io/controller-runtime/pkg/log"

	dash0common "github.com/dash0hq/dash0-operator/api/operator/common"
	zaputil "github.com/dash0hq/dash0-operator/internal/util/zap"

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

func (c *DummyClient) InitializeSelfMonitoringMetrics(_ otelmetric.Meter, _ string, _ logr.Logger) {
	c.hasBeenCalled++
}

var _ = Describe(
	"The OTel SDK starter", func() {

		ctx := context.Background()
		logger := log.FromContext(ctx)

		It(
			"should start with empty values", func() {
				oTelSdkStarter := NewOTelSdkStarter(zaputil.NewDelegatingZapCoreWrapper())
				Expect(oTelSdkStarter.sdkIsActive.Load()).To(BeFalse())
				Expect(*oTelSdkStarter.oTelSdkConfigInput.Load()).To(Equal(OTelSdkConfigInput{}))
				Expect(oTelSdkStarter.activeOTelSdkConfig.Load()).To(BeNil())
			},
		)

		Describe(
			"should start and stop when pre-conditions are met / no longer met", func() {
				var oTelSdkStarter *OTelSdkStarter
				var mockChannel chan *common.OTelSdkConfig

				BeforeEach(
					func() {
						oTelSdkStarter = NewOTelSdkStarter(zaputil.NewDelegatingZapCoreWrapper())
						mockChannel = make(chan *common.OTelSdkConfig, 100)
						oTelSdkStarter.startOrRestartOTelSdkChannel = mockChannel
					},
				)

				It(
					"should start when there is an export with auth token", func() {
						oTelSdkStarter.SetOTelSdkParameters(
							ctx,
							*Dash0ExportWithEndpointAndToken(),
							&AuthorizationTokenTest,
							ClusterUidTest,
							ClusterNameTest,
							OperatorNamespace,
							OperatorManagerDeploymentUID,
							OperatorManagerDeploymentName,
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
					},
				)

				It(
					"should not start when there is an export but no token", func() {
						oTelSdkStarter.SetOTelSdkParameters(
							ctx,
							*Dash0ExportWithEndpointAndSecretRef(),
							nil,
							ClusterUidTest,
							ClusterNameTest,
							OperatorNamespace,
							OperatorManagerDeploymentUID,
							OperatorManagerDeploymentName,
							OperatorVersionTest,
							false,
							logger,
						)
						config := readFromChannelWithTimeout(oTelSdkStarter, mockChannel)
						Expect(config).To(BeNil())
					},
				)

				It(
					"should start when there is an export with a resolved secret ref token", func() {
						oTelSdkStarter.SetOTelSdkParameters(
							ctx,
							*Dash0ExportWithEndpointAndSecretRef(),
							&AuthorizationTokenTest,
							ClusterUidTest,
							ClusterNameTest,
							OperatorNamespace,
							OperatorManagerDeploymentUID,
							OperatorManagerDeploymentName,
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
					},
				)

				It(
					"should stop when the export is removed", func() {
						oTelSdkStarter.SetOTelSdkParameters(
							ctx,
							*Dash0ExportWithEndpointAndToken(),
							&AuthorizationTokenTest,
							ClusterUidTest,
							ClusterNameTest,
							OperatorNamespace,
							OperatorManagerDeploymentUID,
							OperatorManagerDeploymentName,
							OperatorVersionTest,
							false,
							logger,
						)
						config := readFromChannelWithTimeout(oTelSdkStarter, mockChannel)
						Expect(config).NotTo(BeNil())

						oTelSdkStarter.RemoveOTelSdkParameters(ctx, logger)
						Expect(oTelSdkStarter.sdkIsActive.Load()).To(BeFalse())
					},
				)

				It(
					"should stop when the token is removed", func() {
						oTelSdkStarter.SetOTelSdkParameters(
							ctx,
							*Dash0ExportWithEndpointAndSecretRef(),
							&AuthorizationTokenTest,
							ClusterUidTest,
							ClusterNameTest,
							OperatorNamespace,
							OperatorManagerDeploymentUID,
							OperatorManagerDeploymentName,
							OperatorVersionTest,
							false,
							logger,
						)
						config := readFromChannelWithTimeout(oTelSdkStarter, mockChannel)
						Expect(config).NotTo(BeNil())

						// Re-set parameters with nil token to simulate token removal
						oTelSdkStarter.SetOTelSdkParameters(
							ctx,
							*Dash0ExportWithEndpointAndSecretRef(),
							nil,
							ClusterUidTest,
							ClusterNameTest,
							OperatorNamespace,
							OperatorManagerDeploymentUID,
							OperatorManagerDeploymentName,
							OperatorVersionTest,
							false,
							logger,
						)
						Expect(oTelSdkStarter.sdkIsActive.Load()).To(BeFalse())
					},
				)

				It(
					"should restart when endpoint changes", func() {
						oTelSdkStarter.SetOTelSdkParameters(
							ctx,
							*Dash0ExportWithEndpointAndToken(),
							&AuthorizationTokenTest,
							ClusterUidTest,
							ClusterNameTest,
							OperatorNamespace,
							OperatorManagerDeploymentUID,
							OperatorManagerDeploymentName,
							OperatorVersionTest,
							false,
							logger,
						)
						config := readFromChannelWithTimeout(oTelSdkStarter, mockChannel)
						Expect(config).NotTo(BeNil())
						Expect(config.Endpoint).To(Equal(EndpointDash0Test))

						oTelSdkStarter.SetOTelSdkParameters(
							ctx,
							dash0common.Export{
								Dash0: &dash0common.Dash0Configuration{
									Endpoint: EndpointDash0TestAlternative,
									Authorization: dash0common.Authorization{
										Token: &AuthorizationTokenTest,
									},
								},
							},
							&AuthorizationTokenTest,
							ClusterUidTest,
							ClusterNameTest,
							OperatorNamespace,
							OperatorManagerDeploymentUID,
							OperatorManagerDeploymentName,
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
					},
				)

				It(
					"should restart when token changes", func() {
						oTelSdkStarter.SetOTelSdkParameters(
							ctx,
							*Dash0ExportWithEndpointAndToken(),
							&AuthorizationTokenTest,
							ClusterUidTest,
							ClusterNameTest,
							OperatorNamespace,
							OperatorManagerDeploymentUID,
							OperatorManagerDeploymentName,
							OperatorVersionTest,
							false,
							logger,
						)
						config := readFromChannelWithTimeout(oTelSdkStarter, mockChannel)
						Expect(config).NotTo(BeNil())
						Expect(config.Endpoint).To(Equal(EndpointDash0Test))

						oTelSdkStarter.SetOTelSdkParameters(
							ctx,
							dash0common.Export{
								Dash0: &dash0common.Dash0Configuration{
									Endpoint: EndpointDash0Test,
									Authorization: dash0common.Authorization{
										Token: &AuthorizationTokenTestAlternative,
									},
								},
							},
							&AuthorizationTokenTestAlternative,
							ClusterUidTest,
							ClusterNameTest,
							OperatorNamespace,
							OperatorManagerDeploymentUID,
							OperatorManagerDeploymentName,
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
					},
				)

			},
		)

		Describe(
			"should notify self monitoring clients and set the logging delegate", func() {
				var oTelSdkStarter *OTelSdkStarter
				var delegatingZapCoreWrapper *zaputil.DelegatingZapCoreWrapper

				BeforeEach(
					func() {
						delegatingZapCoreWrapper = zaputil.NewDelegatingZapCoreWrapper()
						oTelSdkStarter = NewOTelSdkStarter(delegatingZapCoreWrapper)
					},
				)

				It(
					"should notify clients when there is an export with auth token", func() {

						dummyClient1 := &DummyClient{}
						dummyClient2 := &DummyClient{}
						oTelSdkStarter.WaitForOTelConfig(
							[]SelfMonitoringMetricsClient{
								dummyClient1,
								dummyClient2,
							},
						)

						oTelSdkStarter.SetOTelSdkParameters(
							ctx,
							*Dash0ExportWithEndpointAndToken(),
							&AuthorizationTokenTest,
							ClusterUidTest,
							ClusterNameTest,
							OperatorNamespace,
							OperatorManagerDeploymentUID,
							OperatorManagerDeploymentName,
							OperatorVersionTest,
							false,
							logger,
						)

						Eventually(
							func(g Gomega) {
								g.Expect(delegatingZapCoreWrapper.RootDelegatingZapCore.ForTestOnlyHasDelegate()).To(BeTrue())
								g.Expect(dummyClient1.hasBeenCalled).To(Equal(1))
								g.Expect(dummyClient2.hasBeenCalled).To(Equal(1))
							},
						).Should(Succeed())
					},
				)
			},
		)
	},
)

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
