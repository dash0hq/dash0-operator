// SPDX-FileCopyrightText: Copyright 2024 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package controller

import (
	"context"
	"time"

	"github.com/go-logr/logr"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	dash0common "github.com/dash0hq/dash0-operator/api/operator/common"
	dash0v1alpha1 "github.com/dash0hq/dash0-operator/api/operator/v1alpha1"
	"github.com/dash0hq/dash0-operator/images/pkg/common"
	"github.com/dash0hq/dash0-operator/internal/collectors"
	"github.com/dash0hq/dash0-operator/internal/collectors/otelcolresources"
	"github.com/dash0hq/dash0-operator/internal/selfmonitoringapiaccess"
	"github.com/dash0hq/dash0-operator/internal/targetallocator"
	"github.com/dash0hq/dash0-operator/internal/targetallocator/taresources"
	"github.com/dash0hq/dash0-operator/internal/util"
	zaputil "github.com/dash0hq/dash0-operator/internal/util/zap"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	. "github.com/dash0hq/dash0-operator/test/util"
)

type ApiClientSetRemoveTestConfig struct {
	operatorConfigurationResourceSpec dash0v1alpha1.Dash0OperatorConfigurationSpec
	dataset                           string
	createSecret                      *corev1.Secret
	expectSetApiEndpointAndDataset    bool
	expectRemoveApiEndpointAndDataset bool
	expectSetAuthToken                bool
	expectedAuthToken                 string
	expectRemoveAuthToken             bool
}

type SelfMonitoringTestConfig struct {
	createExport          func() *dash0common.Export
	createSecret          *corev1.Secret
	selfMonitoringEnabled bool
	expectedSdkIsActive   bool
	expectHasConfig       bool
	expectedEndpoint      string
	expectedProtocol      string
	expectedHeaders       map[string]string
}

var (
	reconciler                                        *OperatorConfigurationReconciler
	delegatingZapCoreWrapper                          *zaputil.DelegatingZapCoreWrapper
	createdObjectsOperatorConfigurationControllerTest []client.Object
	apiClient1                                        *DummyApiClient
	apiClient2                                        *DummyApiClient
	authTokenClient1                                  *DummyAuthTokenClient
	authTokenClient2                                  *DummyAuthTokenClient
	selfMonitoringMetricsClient1                      *DummySelfMonitoringMetricsClient
	selfMonitoringMetricsClient2                      *DummySelfMonitoringMetricsClient
)

var _ = Describe("The operation configuration resource controller", Ordered, func() {
	ctx := context.Background()

	BeforeAll(func() {
		EnsureTestNamespaceExists(ctx, k8sClient)
		EnsureOperatorNamespaceExists(ctx, k8sClient)
	})

	BeforeEach(func() {
		authTokenClient1 = &DummyAuthTokenClient{}
		authTokenClient2 = &DummyAuthTokenClient{}
		apiClient1 = &DummyApiClient{}
		apiClient2 = &DummyApiClient{}
		selfMonitoringMetricsClient1 = &DummySelfMonitoringMetricsClient{}
		selfMonitoringMetricsClient2 = &DummySelfMonitoringMetricsClient{}
	})

	AfterEach(func() {
		for _, apiClient := range []*DummyApiClient{apiClient1, apiClient2} {
			apiClient.Reset()
		}
		for _, authTokenClient := range []*DummyAuthTokenClient{authTokenClient1, authTokenClient2} {
			authTokenClient.Reset()
		}
		for _, selfMonitoringMetricsClient := range []*DummySelfMonitoringMetricsClient{
			selfMonitoringMetricsClient1,
			selfMonitoringMetricsClient2,
		} {
			selfMonitoringMetricsClient.Reset()
		}
		createdObjectsOperatorConfigurationControllerTest = DeleteAllCreatedObjects(ctx, k8sClient, createdObjectsOperatorConfigurationControllerTest)
	})

	Describe("updates all registered API and auth token clients", func() {
		AfterEach(func() {
			RemoveOperatorConfigurationResource(ctx, k8sClient)
		})

		DescribeTable("when setting or removing the API config or authorization", func(config ApiClientSetRemoveTestConfig) {
			if config.createSecret != nil {
				Expect(k8sClient.Create(ctx, config.createSecret)).To(Succeed())
				createdObjectsOperatorConfigurationControllerTest = append(createdObjectsOperatorConfigurationControllerTest, config.createSecret)
			}

			reconciler = createReconciler()
			operatorConfigurationResource := CreateOperatorConfigurationResourceWithSpec(
				ctx,
				k8sClient,
				config.operatorConfigurationResourceSpec,
			)

			expectedDataset := "default"
			if config.dataset != "" {
				operatorConfigurationResource.Spec.Export.Dash0.Dataset = config.dataset
				Expect(k8sClient.Update(ctx, operatorConfigurationResource)).To(Succeed())
				expectedDataset = config.dataset
			}

			triggerOperatorConfigurationReconcileRequest(ctx, reconciler, OperatorConfigurationResourceName)
			verifyOperatorConfigurationResourceIsAvailable(ctx)

			for _, apiClient := range []*DummyApiClient{apiClient1, apiClient2} {
				if config.expectSetApiEndpointAndDataset {
					Expect(apiClient.setApiEndpointCalls).To(Equal(1))
					Expect(apiClient.removeApiEndpointCalls).To(Equal(0))
					Expect(apiClient.apiConfig).ToNot(BeNil())
					Expect(apiClient.apiConfig.Endpoint).To(Equal(ApiEndpointTest))
					Expect(apiClient.apiConfig.Dataset).To(Equal(expectedDataset))
				}
				if config.expectRemoveApiEndpointAndDataset {
					Expect(apiClient.setApiEndpointCalls).To(Equal(0))
					Expect(apiClient.removeApiEndpointCalls).To(Equal(1))
					Expect(apiClient.apiConfig).To(BeNil())
				}
			}
			for _, authTokenClient := range []*DummyAuthTokenClient{authTokenClient1, authTokenClient2} {
				if config.expectSetAuthToken {
					Expect(authTokenClient.SetAuthTokenCalls).To(Equal(1))
					Expect(authTokenClient.RemoveAuthTokenCalls).To(Equal(0))
				}
				if config.expectRemoveAuthToken {
					Expect(authTokenClient.SetAuthTokenCalls).To(Equal(0))
					Expect(authTokenClient.RemoveAuthTokenCalls).To(Equal(1))
				}
				Expect(authTokenClient.AuthToken).To(Equal(config.expectedAuthToken))
			}
		},

			Entry("no API endpoint, no Dash0 export", ApiClientSetRemoveTestConfig{
				operatorConfigurationResourceSpec: OperatorConfigurationResourceWithoutExport,
				expectSetApiEndpointAndDataset:    false,
				expectRemoveApiEndpointAndDataset: true,
				expectSetAuthToken:                false,
				expectRemoveAuthToken:             true,
			}),
			Entry("no API endpoint, Dash0 export with token", ApiClientSetRemoveTestConfig{
				operatorConfigurationResourceSpec: OperatorConfigurationResourceDash0ExportWithoutApiEndpointWithToken,
				expectSetApiEndpointAndDataset:    false,
				expectRemoveApiEndpointAndDataset: true,
				expectSetAuthToken:                true,
				expectedAuthToken:                 AuthorizationTokenTest,
				expectRemoveAuthToken:             false,
			}),
			Entry("no API endpoint, Dash0 export with secret ref", ApiClientSetRemoveTestConfig{
				createSecret:                      DefaultSecret(),
				operatorConfigurationResourceSpec: OperatorConfigurationResourceDash0ExportWithoutApiEndpointWithSecretRef,
				expectSetApiEndpointAndDataset:    false,
				expectRemoveApiEndpointAndDataset: true,
				expectSetAuthToken:                true,
				expectedAuthToken:                 AuthorizationTokenTestFromSecret,
				expectRemoveAuthToken:             false,
			}),
			Entry("API endpoint, Dash0 export with token", ApiClientSetRemoveTestConfig{
				operatorConfigurationResourceSpec: OperatorConfigurationResourceDash0ExportWithApiEndpointWithToken,
				expectSetApiEndpointAndDataset:    true,
				expectRemoveApiEndpointAndDataset: false,
				expectSetAuthToken:                true,
				expectedAuthToken:                 AuthorizationTokenTest,
				expectRemoveAuthToken:             false,
			}),
			Entry("API endpoint, Dash0 export with secret ref", ApiClientSetRemoveTestConfig{
				operatorConfigurationResourceSpec: OperatorConfigurationResourceDash0ExportWithApiEndpointWithSecretRef,
				createSecret:                      DefaultSecret(),
				expectSetApiEndpointAndDataset:    true,
				expectRemoveApiEndpointAndDataset: false,
				expectSetAuthToken:                true,
				expectedAuthToken:                 AuthorizationTokenTestFromSecret,
				expectRemoveAuthToken:             false,
			}),
			Entry("API endpoint, Dash0 export with token, custom dataset", ApiClientSetRemoveTestConfig{
				operatorConfigurationResourceSpec: OperatorConfigurationResourceDash0ExportWithApiEndpointWithToken,
				dataset:                           "custom-dataset",
				expectSetApiEndpointAndDataset:    true,
				expectRemoveApiEndpointAndDataset: false,
				expectSetAuthToken:                true,
				expectedAuthToken:                 AuthorizationTokenTest,
				expectRemoveAuthToken:             false,
			}),
			Entry("API endpoint, Dash0 export with secret ref, custom dataset", ApiClientSetRemoveTestConfig{
				operatorConfigurationResourceSpec: OperatorConfigurationResourceDash0ExportWithApiEndpointWithSecretRef,
				dataset:                           "custom-dataset",
				createSecret:                      DefaultSecret(),
				expectSetApiEndpointAndDataset:    true,
				expectRemoveApiEndpointAndDataset: false,
				expectSetAuthToken:                true,
				expectedAuthToken:                 AuthorizationTokenTestFromSecret,
				expectRemoveAuthToken:             false,
			}),
		)
	})

	Describe("when creating the operator configuration resource", func() {
		BeforeEach(func() {
			reconciler = createReconciler()
		})

		AfterEach(func() {
			RemoveOperatorConfigurationResource(ctx, k8sClient)
		})

		Describe("when creating the collector resources", func() {

			It("should add the collector resources", func() {
				CreateOperatorConfigurationResourceWithSpec(
					ctx,
					k8sClient,
					dash0v1alpha1.Dash0OperatorConfigurationSpec{
						Export: Dash0ExportWithEndpointAndToken(),
						SelfMonitoring: dash0v1alpha1.SelfMonitoring{
							Enabled: ptr.To(false),
						},
						ClusterName: ClusterNameTest,
					},
				)

				triggerOperatorConfigurationReconcileRequest(ctx, reconciler, OperatorConfigurationResourceName)

				VerifyCollectorResources(ctx, k8sClient, operatorNamespace, EndpointDash0Test, AuthorizationDefaultEnvVar, AuthorizationTokenTest)
			})

			DescribeTable("it starts the OTel SDK for self-monitoring in the operator manager deployment",
				func(config SelfMonitoringTestConfig) {
					if config.createSecret != nil {
						Expect(k8sClient.Create(ctx, config.createSecret)).To(Succeed())
						createdObjectsOperatorConfigurationControllerTest = append(createdObjectsOperatorConfigurationControllerTest, config.createSecret)
					}

					CreateOperatorConfigurationResourceWithSpec(
						ctx,
						k8sClient,
						dash0v1alpha1.Dash0OperatorConfigurationSpec{
							Export: config.createExport(),
							SelfMonitoring: dash0v1alpha1.SelfMonitoring{
								Enabled: ptr.To(config.selfMonitoringEnabled),
							},
							ClusterName: ClusterNameTest,
						},
					)

					reconciler.oTelSdkStarter.WaitForOTelConfig([]selfmonitoringapiaccess.SelfMonitoringMetricsClient{
						selfMonitoringMetricsClient1,
						selfMonitoringMetricsClient2,
					})
					triggerOperatorConfigurationReconcileRequest(ctx, reconciler, OperatorConfigurationResourceName)
					verifyOperatorConfigurationResourceIsAvailable(ctx)

					Eventually(func(g Gomega) {
						sdkIsActive, activeOTelSdkConfig, _ :=
							reconciler.oTelSdkStarter.ForTestOnlyGetState()
						g.Expect(sdkIsActive).To(Equal(config.expectedSdkIsActive))
						if config.expectHasConfig {
							g.Expect(activeOTelSdkConfig).ToNot(BeNil())
							g.Expect(activeOTelSdkConfig.Endpoint).To(Equal(config.expectedEndpoint))
							g.Expect(activeOTelSdkConfig.Protocol).To(Equal(config.expectedProtocol))
							verifyOperatorManagerResourceAttributes(g, activeOTelSdkConfig)

							g.Expect(activeOTelSdkConfig.Headers).To(HaveLen(len(config.expectedHeaders)))
							for key, value := range config.expectedHeaders {
								g.Expect(activeOTelSdkConfig.Headers[key]).To(Equal(value))
							}

						} else {
							g.Expect(activeOTelSdkConfig).To(BeNil())
						}

						for _, smc := range []*DummySelfMonitoringMetricsClient{
							selfMonitoringMetricsClient1,
							selfMonitoringMetricsClient2,
						} {
							if config.expectedSdkIsActive {
								g.Expect(delegatingZapCoreWrapper.RootDelegatingZapCore.ForTestOnlyHasDelegate()).To(BeTrue())
								g.Expect(smc.InitializeSelfMonitoringMetricsCalls).To(Equal(1))
							} else {
								g.Expect(delegatingZapCoreWrapper.RootDelegatingZapCore.ForTestOnlyHasDelegate()).To(BeFalse())
								g.Expect(smc.InitializeSelfMonitoringMetricsCalls).To(Equal(0))
							}
						}
					}, 1*time.Second, pollingInterval).Should(Succeed())
				},

				Entry("without an export", SelfMonitoringTestConfig{
					createExport:          NoExport,
					selfMonitoringEnabled: false,
					expectedSdkIsActive:   false,
					expectHasConfig:       false,
				}),
				Entry("with a Dash0 export with a token", SelfMonitoringTestConfig{
					createExport:          Dash0ExportWithEndpointAndToken,
					selfMonitoringEnabled: true,
					expectedSdkIsActive:   true,
					expectHasConfig:       true,
					expectedEndpoint:      EndpointDash0Test,
					expectedProtocol:      common.ProtocolGrpc,
					expectedHeaders: map[string]string{
						util.AuthorizationHeaderName: AuthorizationHeaderTest,
					},
				}),
				Entry("with a Dash0 export with a token and a custom dataset", SelfMonitoringTestConfig{
					createExport:          Dash0ExportWithEndpointTokenAndCustomDataset,
					selfMonitoringEnabled: true,
					expectedSdkIsActive:   true,
					expectHasConfig:       true,
					expectedEndpoint:      EndpointDash0Test,
					expectedProtocol:      common.ProtocolGrpc,
					expectedHeaders: map[string]string{
						util.AuthorizationHeaderName: AuthorizationHeaderTest,
						util.Dash0DatasetHeaderName:  DatasetCustomTest,
					},
				}),
				Entry("with a Dash0 export with a secret ref", SelfMonitoringTestConfig{
					createExport:          Dash0ExportWithEndpointAndSecretRef,
					createSecret:          DefaultSecret(),
					selfMonitoringEnabled: true,
					expectedSdkIsActive:   true,
					expectHasConfig:       true,
					expectedEndpoint:      EndpointDash0Test,
					expectedProtocol:      common.ProtocolGrpc,
					expectedHeaders: map[string]string{
						util.AuthorizationHeaderName: AuthorizationHeaderTestFromSecret,
					},
				}),
				Entry("with a Grpc export", SelfMonitoringTestConfig{
					createExport:          GrpcExportTest,
					selfMonitoringEnabled: true,
					expectedSdkIsActive:   true,
					expectHasConfig:       true,
					expectedEndpoint:      EndpointGrpcTest,
					expectedProtocol:      common.ProtocolGrpc,
					expectedHeaders: map[string]string{
						"Key": "Value",
					},
				}),
				Entry("with an HTTP export", SelfMonitoringTestConfig{
					createExport:          HttpExportTest,
					selfMonitoringEnabled: true,
					expectedSdkIsActive:   true,
					expectHasConfig:       true,
					expectedEndpoint:      EndpointHttpTest,
					expectedProtocol:      common.ProtocolHttpProtobuf,
					expectedHeaders: map[string]string{
						"Key": "Value",
					},
				}),
			)
		})
	})

	Describe("when updating or deleting the existing operator configuration resource", func() {

		BeforeEach(func() {
			reconciler = createReconciler()

			// Reconcile once with self-monitoring enabled and an export, so the tests below start in a state where the
			// OTel SDK is already active.
			CreateOperatorConfigurationResourceWithSpec(
				ctx,
				k8sClient,
				dash0v1alpha1.Dash0OperatorConfigurationSpec{
					Export: Dash0ExportWithEndpointAndToken(),
					SelfMonitoring: dash0v1alpha1.SelfMonitoring{
						Enabled: ptr.To(true),
					},
					ClusterName: ClusterNameTest,
				},
			)

			reconciler.oTelSdkStarter.WaitForOTelConfig([]selfmonitoringapiaccess.SelfMonitoringMetricsClient{
				selfMonitoringMetricsClient1,
				selfMonitoringMetricsClient2,
			})
			triggerOperatorConfigurationReconcileRequest(ctx, reconciler, OperatorConfigurationResourceName)
			verifyOperatorConfigurationResourceIsAvailable(ctx)

			Eventually(func(g Gomega) {
				g.Expect(delegatingZapCoreWrapper.RootDelegatingZapCore.ForTestOnlyHasDelegate()).To(BeTrue())
				sdkIsActive, activeOTelSdkConfig, _ :=
					reconciler.oTelSdkStarter.ForTestOnlyGetState()
				g.Expect(sdkIsActive).To(BeTrue())
				g.Expect(activeOTelSdkConfig).ToNot(BeNil())
			}, 1*time.Second, pollingInterval).Should(Succeed())

			resetCallCounts()
		})

		AfterEach(func() {
			RemoveOperatorConfigurationResource(ctx, k8sClient)
		})

		It("shuts down the OTel SDK when self-monitoring is disabled or the export config removed", func() {
			// update operator configuration, removing the export and disabling self-monitoring
			updatedOperatorConfigurationResource := LoadOperatorConfigurationResourceOrFail(ctx, k8sClient, Default)
			updatedOperatorConfigurationResource.Spec.SelfMonitoring.Enabled = ptr.To(false)
			updatedOperatorConfigurationResource.Spec.Export = nil
			Expect(k8sClient.Update(ctx, updatedOperatorConfigurationResource)).To(Succeed())

			triggerOperatorConfigurationReconcileRequest(ctx, reconciler, OperatorConfigurationResourceName)
			verifyOperatorConfigurationResourceIsAvailable(ctx)

			// verify the OTel SDK has been shut down
			Eventually(func(g Gomega) {
				g.Expect(delegatingZapCoreWrapper.RootDelegatingZapCore.ForTestOnlyHasDelegate()).To(BeFalse())
				sdkIsActive, activeOTelSdkConfig, _ :=
					reconciler.oTelSdkStarter.ForTestOnlyGetState()
				g.Expect(activeOTelSdkConfig).ToNot(BeNil())
				g.Expect(sdkIsActive).To(BeFalse())
			}, 1*time.Second, pollingInterval).Should(Succeed())
		})

		It("restarts the OTel SDK after shutting it down previously", func() {
			// update operator configuration, removing the export and disabling self-monitoring
			updatedOperatorConfigurationResource := LoadOperatorConfigurationResourceOrFail(ctx, k8sClient, Default)
			updatedOperatorConfigurationResource.Spec.SelfMonitoring.Enabled = ptr.To(false)
			updatedOperatorConfigurationResource.Spec.Export = nil
			Expect(k8sClient.Update(ctx, updatedOperatorConfigurationResource)).To(Succeed())

			triggerOperatorConfigurationReconcileRequest(ctx, reconciler, OperatorConfigurationResourceName)
			verifyOperatorConfigurationResourceIsAvailable(ctx)

			// verify the OTel SDK has been shut down
			Eventually(func(g Gomega) {
				g.Expect(delegatingZapCoreWrapper.RootDelegatingZapCore.ForTestOnlyHasDelegate()).To(BeFalse())
				sdkIsActive, _, _ :=
					reconciler.oTelSdkStarter.ForTestOnlyGetState()
				g.Expect(sdkIsActive).To(BeFalse())
			}, 1*time.Second, pollingInterval).Should(Succeed())

			// update operator configuration _once again_, adding the export config back and enabling self-monitoring
			// again
			updatedOperatorConfigurationResource = LoadOperatorConfigurationResourceOrFail(ctx, k8sClient, Default)
			updatedOperatorConfigurationResource.Spec.SelfMonitoring.Enabled = ptr.To(true)
			updatedOperatorConfigurationResource.Spec.Export = Dash0ExportWithEndpointAndToken()
			Expect(k8sClient.Update(ctx, updatedOperatorConfigurationResource)).To(Succeed())

			triggerOperatorConfigurationReconcileRequest(ctx, reconciler, OperatorConfigurationResourceName)
			verifyOperatorConfigurationResourceIsAvailable(ctx)

			// verify the OTel SDK has been started again
			Eventually(func(g Gomega) {
				g.Expect(delegatingZapCoreWrapper.RootDelegatingZapCore.ForTestOnlyHasDelegate()).To(BeTrue())
				sdkIsActive, activeOTelSdkConfig, _ :=
					reconciler.oTelSdkStarter.ForTestOnlyGetState()
				g.Expect(sdkIsActive).To(BeTrue())

				g.Expect(activeOTelSdkConfig).ToNot(BeNil())
				g.Expect(activeOTelSdkConfig.Endpoint).To(Equal(EndpointDash0Test))
				g.Expect(activeOTelSdkConfig.Protocol).To(Equal(common.ProtocolGrpc))
				verifyOperatorManagerResourceAttributes(g, activeOTelSdkConfig)

				g.Expect(activeOTelSdkConfig.Headers).To(HaveLen(1))
				g.Expect(activeOTelSdkConfig.Headers[util.AuthorizationHeaderName]).To(Equal(AuthorizationHeaderTest))
			}, 1*time.Second, pollingInterval).Should(Succeed())
		})

		It("restarts the OTel SDK when it is already running and there is a configuration change", func() {

			// update operator configuration, but do not disable self-monitoring, only change endpoint, dataset and
			// token
			updatedOperatorConfigurationResource := LoadOperatorConfigurationResourceOrFail(ctx, k8sClient, Default)
			updatedOperatorConfigurationResource.Spec.Export = &dash0common.Export{
				Dash0: &dash0common.Dash0Configuration{
					Endpoint: EndpointDash0TestAlternative,
					Authorization: dash0common.Authorization{
						Token: &AuthorizationTokenTestAlternative,
					},
					Dataset: DatasetCustomTest,
				},
			}
			Expect(k8sClient.Update(ctx, updatedOperatorConfigurationResource)).To(Succeed())

			triggerOperatorConfigurationReconcileRequest(ctx, reconciler, OperatorConfigurationResourceName)
			verifyOperatorConfigurationResourceIsAvailable(ctx)

			for _, authTokenClient := range []*DummyAuthTokenClient{authTokenClient1, authTokenClient2} {
				Expect(authTokenClient.SetAuthTokenCalls).To(Equal(1))
				Expect(authTokenClient.AuthToken).To(Equal(AuthorizationTokenTestAlternative))
			}

			// Verify the OTel SDK has been restarted - well, within this test case we can not actually verify that the
			// SDK has been shut down before being restarted, but we can verify the new config values. The aspect of
			// shutting it down and actually restarting it is covered in otel_init_wait_test.go
			Eventually(func(g Gomega) {
				g.Expect(delegatingZapCoreWrapper.RootDelegatingZapCore.ForTestOnlyHasDelegate()).To(BeTrue())

				sdkIsActive, activeOTelSdkConfig, _ :=
					reconciler.oTelSdkStarter.ForTestOnlyGetState()
				g.Expect(sdkIsActive).To(BeTrue())

				g.Expect(activeOTelSdkConfig).ToNot(BeNil())
				g.Expect(activeOTelSdkConfig.Endpoint).To(Equal(EndpointDash0TestAlternative))
				g.Expect(activeOTelSdkConfig.Protocol).To(Equal(common.ProtocolGrpc))
				verifyOperatorManagerResourceAttributes(g, activeOTelSdkConfig)

				g.Expect(activeOTelSdkConfig.Headers).To(HaveLen(2))
				g.Expect(activeOTelSdkConfig.Headers[util.AuthorizationHeaderName]).To(
					Equal(AuthorizationHeaderTestAlternative))
				g.Expect(activeOTelSdkConfig.Headers[util.Dash0DatasetHeaderName]).To(Equal(DatasetCustomTest))
			}, 1*time.Second, pollingInterval).Should(Succeed())
		})

		It("shuts down the OTel SDK and removes config values from API clients when the operator configuration resource is deleted", func() {
			resource := LoadOperatorConfigurationResourceOrFail(ctx, k8sClient, Default)
			Expect(k8sClient.Delete(ctx, resource)).To(Succeed())

			triggerOperatorConfigurationReconcileRequest(ctx, reconciler, OperatorConfigurationResourceName)
			VerifyOperatorConfigurationResourceByNameDoesNotExist(ctx, k8sClient, Default, resource.Name)

			for _, apiClient := range []*DummyApiClient{apiClient1, apiClient2} {
				Expect(apiClient.setApiEndpointCalls).To(Equal(0))
				Expect(apiClient.removeApiEndpointCalls).To(Equal(1))
				Expect(apiClient.apiConfig).To(BeNil())
			}
			for _, authTokenClient := range []*DummyAuthTokenClient{authTokenClient1, authTokenClient2} {
				Expect(authTokenClient.SetAuthTokenCalls).To(Equal(0))
				Expect(authTokenClient.RemoveAuthTokenCalls).To(Equal(1))
				Expect(authTokenClient.AuthToken).To(Equal(""))
			}

			Eventually(func(g Gomega) {
				g.Expect(delegatingZapCoreWrapper.RootDelegatingZapCore.ForTestOnlyHasDelegate()).To(BeFalse())
				sdkIsActive, activeOTelSdkConfig, authTokenFromSecretRef :=
					reconciler.oTelSdkStarter.ForTestOnlyGetState()
				g.Expect(sdkIsActive).To(BeFalse())
				g.Expect(activeOTelSdkConfig).To(BeNil())
				g.Expect(authTokenFromSecretRef).To(BeNil())
			})
		})

		It("should remove the collector resources when the operator configuration resource is deleted", func() {
			VerifyCollectorResources(ctx, k8sClient, operatorNamespace, EndpointDash0Test, AuthorizationDefaultEnvVar, AuthorizationTokenTest)

			resource := LoadOperatorConfigurationResourceOrFail(ctx, k8sClient, Default)
			Expect(k8sClient.Delete(ctx, resource)).To(Succeed())

			triggerOperatorConfigurationReconcileRequest(ctx, reconciler, OperatorConfigurationResourceName)
			VerifyOperatorConfigurationResourceByNameDoesNotExist(ctx, k8sClient, Default, resource.Name)

			VerifyCollectorResourcesDoNotExist(ctx, k8sClient, operatorNamespace)
		})
	})

	Describe("when self-healing a degraded resource state", func() {

		BeforeEach(func() {
			reconciler = createReconciler()

			CreateOperatorConfigurationResourceWithSpec(
				ctx,
				k8sClient,
				dash0v1alpha1.Dash0OperatorConfigurationSpec{
					Export: Dash0ExportWithEndpointAndToken(),
					SelfMonitoring: dash0v1alpha1.SelfMonitoring{
						Enabled: ptr.To(false),
					},
					ClusterName: ClusterNameTest,
				},
			)
			triggerOperatorConfigurationReconcileRequest(ctx, reconciler, OperatorConfigurationResourceName)
			verifyOperatorConfigurationResourceIsAvailable(ctx)

			// start test in a degraded state
			resource := LoadOperatorConfigurationResourceOrFail(ctx, k8sClient, Default)
			resource.EnsureResourceIsMarkedAsDegraded("TestReason", "This is a test message.")
			Expect(k8sClient.Status().Update(ctx, resource)).To(Succeed())
			verifyOperatorConfigurationResourceIsDegraded(ctx)
		})

		AfterEach(func() {
			RemoveOperatorConfigurationResource(ctx, k8sClient)
		})

		It("reconciling should self-heal the degraded state", func() {
			triggerOperatorConfigurationReconcileRequest(ctx, reconciler, OperatorConfigurationResourceName)
			verifyOperatorConfigurationResourceIsAvailable(ctx)
		})
	})

	Describe("uniqueness check", func() {
		It("should mark only the most recent resource as available and the other ones as degraded when multiple resources exist", func() {
			reconciler = createReconciler()
			firstName := types.NamespacedName{Name: "resource-1"}
			firstResource := CreateOperatorConfigurationResourceWithName(
				ctx,
				k8sClient,
				firstName.Name,
				OperatorConfigurationResourceWithoutSelfMonitoringWithToken,
			)
			createdObjectsOperatorConfigurationControllerTest = append(createdObjectsOperatorConfigurationControllerTest, firstResource)
			time.Sleep(10 * time.Millisecond)

			secondName := types.NamespacedName{Name: "resource-2"}
			secondResource := CreateOperatorConfigurationResourceWithName(
				ctx,
				k8sClient,
				secondName.Name,
				OperatorConfigurationResourceWithoutSelfMonitoringWithToken,
			)
			createdObjectsOperatorConfigurationControllerTest = append(createdObjectsOperatorConfigurationControllerTest, secondResource)
			time.Sleep(10 * time.Millisecond)

			thirdName := types.NamespacedName{Name: "resource-3"}
			thirdResource := CreateOperatorConfigurationResourceWithName(
				ctx,
				k8sClient,
				thirdName.Name,
				OperatorConfigurationResourceWithoutSelfMonitoringWithToken,
			)
			createdObjectsOperatorConfigurationControllerTest = append(createdObjectsOperatorConfigurationControllerTest, thirdResource)
			time.Sleep(10 * time.Millisecond)

			triggerOperatorConfigurationReconcileRequest(ctx, reconciler, firstName.Name)
			triggerOperatorConfigurationReconcileRequest(ctx, reconciler, secondName.Name)
			triggerOperatorConfigurationReconcileRequest(ctx, reconciler, thirdName.Name)

			Eventually(func(g Gomega) {
				resource1Available := LoadOperatorConfigurationResourceStatusCondition(ctx, k8sClient, firstName.Name, dash0common.ConditionTypeAvailable)
				resource1Degraded := LoadOperatorConfigurationResourceStatusCondition(ctx, k8sClient, firstName.Name, dash0common.ConditionTypeDegraded)
				resource2Available := LoadOperatorConfigurationResourceStatusCondition(ctx, k8sClient, secondName.Name, dash0common.ConditionTypeAvailable)
				resource2Degraded := LoadOperatorConfigurationResourceStatusCondition(ctx, k8sClient, secondName.Name, dash0common.ConditionTypeDegraded)
				resource3Available := LoadOperatorConfigurationResourceStatusCondition(ctx, k8sClient, thirdName.Name, dash0common.ConditionTypeAvailable)
				resource3Degraded := LoadOperatorConfigurationResourceStatusCondition(ctx, k8sClient, thirdName.Name, dash0common.ConditionTypeDegraded)

				// The first two resource should have been marked as degraded.
				VerifyResourceStatusCondition(
					g,
					resource1Available,
					metav1.ConditionFalse,
					"NewerResourceIsPresent",
					"There is a more recently created Dash0 operator configuration resource in this cluster, please "+
						"remove all but one resource instance.",
				)
				VerifyResourceStatusCondition(
					g,
					resource1Degraded,
					metav1.ConditionTrue,
					"NewerResourceIsPresent",
					"There is a more recently created Dash0 operator configuration resource in this cluster, please "+
						"remove all but one resource instance.",
				)
				VerifyResourceStatusCondition(g, resource2Available, metav1.ConditionFalse, "NewerResourceIsPresent",
					"There is a more recently created Dash0 operator configuration resource in this cluster, please "+
						"remove all but one resource instance.")
				VerifyResourceStatusCondition(g, resource2Degraded, metav1.ConditionTrue, "NewerResourceIsPresent",
					"There is a more recently created Dash0 operator configuration resource in this cluster, please "+
						"remove all but one resource instance.")

				// The third (and most recent) resource should have been marked as available.
				VerifyResourceStatusCondition(
					g,
					resource3Available,
					metav1.ConditionTrue,
					"ReconcileFinished",
					"Dash0 operator configuration is available in this cluster now.",
				)
				g.Expect(resource3Degraded).To(BeNil())

			}, timeout, pollingInterval).Should(Succeed())
		})
	})
})

func verifyOperatorManagerResourceAttributes(g Gomega, oTelSdkConfig *common.OTelSdkConfig) {
	g.Expect(oTelSdkConfig.ServiceName).To(Equal("operator-manager"))
	g.Expect(oTelSdkConfig.ServiceVersion).To(Equal("1.2.3"))
	g.Expect(oTelSdkConfig.PseudoClusterUid).To(Equal(ClusterUidTest))
	g.Expect(oTelSdkConfig.ClusterName).To(Equal(ClusterNameTest))
	g.Expect(oTelSdkConfig.DeploymentUid).To(Equal(OperatorManagerDeploymentUIDStr))
	g.Expect(oTelSdkConfig.DeploymentName).To(Equal(OperatorManagerDeploymentName))
	g.Expect(oTelSdkConfig.ContainerName).To(Equal("operator-manager"))
}

func createReconciler() *OperatorConfigurationReconciler {
	oTelColResourceManager := otelcolresources.NewOTelColResourceManager(
		k8sClient,
		k8sClient.Scheme(),
		OperatorManagerDeployment,
		util.CollectorConfig{
			Images:                    TestImages,
			OperatorNamespace:         operatorNamespace,
			OTelCollectorNamePrefix:   OTelCollectorNamePrefixTest,
			TargetAllocatorNamePrefix: TargetAllocatorPrefixTest,
		},
	)
	collectorManager := collectors.NewCollectorManager(
		k8sClient,
		clientset,
		util.ExtraConfigDefaults,
		false,
		oTelColResourceManager,
	)
	targetallocatorResourceManager := taresources.NewTargetAllocatorResourceManager(
		k8sClient,
		k8sClient.Scheme(),
		OperatorManagerDeployment,
		util.TargetAllocatorConfig{
			Images:                    TestImages,
			OperatorNamespace:         operatorNamespace,
			TargetAllocatorNamePrefix: TargetAllocatorPrefixTest,
			CollectorComponent:        otelcolresources.CollectorDaemonSetServiceComponent(),
		})
	targetallocatorManager := targetallocator.NewTargetAllocatorManager(
		k8sClient, clientset, util.ExtraConfigDefaults, false, targetallocatorResourceManager,
	)
	delegatingZapCoreWrapper = zaputil.NewDelegatingZapCoreWrapper()
	otelSdkStarter := selfmonitoringapiaccess.NewOTelSdkStarter(delegatingZapCoreWrapper)

	operatorConfigurationReconciler := NewOperatorConfigurationReconciler(
		k8sClient,
		clientset,
		[]ApiClient{
			apiClient1,
			apiClient2,
		},
		collectorManager,
		targetallocatorManager,
		ClusterUidTest,
		OperatorManagerDeployment.Namespace,
		OperatorManagerDeployment.UID,
		OperatorManagerDeployment.Name,
		otelSdkStarter,
		TestImages,
		OperatorNamespace,
		false,
	)
	operatorConfigurationReconciler.SetAuthTokenClients([]selfmonitoringapiaccess.AuthTokenClient{
		otelSdkStarter,
		authTokenClient1,
		authTokenClient2,
	})
	return operatorConfigurationReconciler
}

func triggerOperatorConfigurationReconcileRequest(ctx context.Context, reconciler *OperatorConfigurationReconciler, name string) {
	triggerOperatorReconcileRequestForName(ctx, reconciler, name)
}

func triggerOperatorReconcileRequestForName(
	ctx context.Context,
	reconciler *OperatorConfigurationReconciler,
	dash0OperatorResourceName string,
) {
	By("Triggering an operator configuration resource reconcile request")
	_, err := reconciler.Reconcile(ctx, reconcile.Request{
		NamespacedName: types.NamespacedName{Name: dash0OperatorResourceName},
	})
	Expect(err).NotTo(HaveOccurred())
}

func verifyOperatorConfigurationResourceIsAvailable(ctx context.Context) {
	var availableCondition *metav1.Condition
	By("Verifying status conditions")
	Eventually(func(g Gomega) {
		resource := LoadOperatorConfigurationResourceOrFail(ctx, k8sClient, g)
		availableCondition = meta.FindStatusCondition(resource.Status.Conditions, string(dash0common.ConditionTypeAvailable))
		g.Expect(availableCondition.Status).To(Equal(metav1.ConditionTrue))
		degradedCondition := meta.FindStatusCondition(resource.Status.Conditions, string(dash0common.ConditionTypeDegraded))
		g.Expect(degradedCondition).To(BeNil())
	}, timeout, pollingInterval).Should(Succeed())
}

func verifyOperatorConfigurationResourceIsDegraded(ctx context.Context) {
	var availableCondition *metav1.Condition
	By("Verifying status conditions")
	Eventually(func(g Gomega) {
		resource := LoadOperatorConfigurationResourceOrFail(ctx, k8sClient, g)
		availableCondition = meta.FindStatusCondition(resource.Status.Conditions, string(dash0common.ConditionTypeAvailable))
		g.Expect(availableCondition.Status).To(Equal(metav1.ConditionFalse))
		degradedCondition := meta.FindStatusCondition(resource.Status.Conditions, string(dash0common.ConditionTypeDegraded))
		g.Expect(degradedCondition.Status).To(Equal(metav1.ConditionTrue))
	}, timeout, pollingInterval).Should(Succeed())
}

func resetCallCounts() {
	for _, apiClient := range []*DummyApiClient{apiClient1, apiClient2} {
		apiClient.ResetCallCounts()
	}
	for _, authTokenClient := range []*DummyAuthTokenClient{authTokenClient1, authTokenClient2} {
		authTokenClient.ResetCallCounts()
	}
	for _, selfMonitoringMetricsClient := range []*DummySelfMonitoringMetricsClient{
		selfMonitoringMetricsClient1,
		selfMonitoringMetricsClient2,
	} {
		selfMonitoringMetricsClient.ResetCallCounts()
	}
}

type DummyApiClient struct {
	setApiEndpointCalls    int
	removeApiEndpointCalls int
	apiConfig              *ApiConfig
}

func (c *DummyApiClient) SetApiEndpointAndDataset(_ context.Context, apiConfig *ApiConfig, _ *logr.Logger) {
	c.setApiEndpointCalls++
	c.apiConfig = apiConfig
}

func (c *DummyApiClient) RemoveApiEndpointAndDataset(_ context.Context, _ *logr.Logger) {
	c.removeApiEndpointCalls++
	c.apiConfig = nil
}

func (c *DummyApiClient) Reset() {
	c.ResetCallCounts()
	c.apiConfig = nil
}

func (c *DummyApiClient) ResetCallCounts() {
	c.setApiEndpointCalls = 0
	c.removeApiEndpointCalls = 0
}
