// SPDX-FileCopyrightText: Copyright 2024 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package controller

import (
	"context"
	"fmt"
	"reflect"
	"time"

	"github.com/dash0hq/dash0-operator/images/pkg/common"
	"github.com/go-logr/logr"
	json "github.com/json-iterator/go"
	"github.com/wI2L/jsondiff"
	"go.opentelemetry.io/otel/attribute"
	otelmetric "go.opentelemetry.io/otel/metric"
	appsv1 "k8s.io/api/apps/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	dash0v1alpha1 "github.com/dash0hq/dash0-operator/api/dash0monitoring/v1alpha1"
	"github.com/dash0hq/dash0-operator/internal/backendconnection"
	"github.com/dash0hq/dash0-operator/internal/backendconnection/otelcolresources"
	"github.com/dash0hq/dash0-operator/internal/selfmonitoringapiaccess"
	"github.com/dash0hq/dash0-operator/internal/util"
	zaputil "github.com/dash0hq/dash0-operator/internal/util/zap"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	. "github.com/dash0hq/dash0-operator/test/util"
)

type SelfMonitoringAndApiAccessTestConfig struct {
	existingSecretRefResolverDeployment               func() *appsv1.Deployment
	operatorConfigurationResourceSpec                 dash0v1alpha1.Dash0OperatorConfigurationSpec
	expectedSecretRefResolverDeploymentAfterReconcile func() *appsv1.Deployment
	expectK8sClientUpdate                             bool
}

type ApiClientSetRemoveTestConfig struct {
	operatorConfigurationResourceSpec dash0v1alpha1.Dash0OperatorConfigurationSpec
	dataset                           string
	expectSetApiEndpointAndDataset    bool
	expectRemoveApiEndpointAndDataset bool
	expectSetAuthToken                bool
	expectRemoveAuthToken             bool
}

type SelfMonitoringTestConfig struct {
	createExport             func() *dash0v1alpha1.Export
	selfMonitoringEnabled    bool
	simulateSecretRefResolve bool
	expectedSdkIsActive      bool
	expectHasConfig          bool
	expectedEndpoint         string
	expectedProtocol         string
	expectedHeaders          map[string]string
}

var (
	reconciler                   *OperatorConfigurationReconciler
	delegatingZapCoreWrapper     *zaputil.DelegatingZapCoreWrapper
	apiClient1                   *DummyApiClient
	apiClient2                   *DummyApiClient
	selfMonitoringMetricsClient1 *DummySelfMonitoringMetricsClient
	selfMonitoringMetricsClient2 *DummySelfMonitoringMetricsClient
)

var _ = Describe("The operation configuration resource controller", Ordered, func() {
	ctx := context.Background()
	var secretRefResolverDeployment *appsv1.Deployment

	BeforeAll(func() {
		EnsureTestNamespaceExists(ctx, k8sClient)
		EnsureOperatorNamespaceExists(ctx, k8sClient)
	})

	BeforeEach(func() {
		apiClient1 = &DummyApiClient{}
		apiClient2 = &DummyApiClient{}
		selfMonitoringMetricsClient1 = &DummySelfMonitoringMetricsClient{}
		selfMonitoringMetricsClient2 = &DummySelfMonitoringMetricsClient{}
	})

	Describe("updates the secret ref resolver deployment", func() {
		AfterEach(func() {
			RemoveOperatorConfigurationResource(ctx, k8sClient)
			EnsureSecretRefResolverDeploymentDoesNotExist(ctx, k8sClient, secretRefResolverDeployment)
		})

		DescribeTable("to add or remove the secret ref from the operator configuration resource:", func(config SelfMonitoringAndApiAccessTestConfig) {
			secretRefResolverDeployment =
				EnsureSecretRefResolverDeploymentExists(ctx, k8sClient, config.existingSecretRefResolverDeployment())
			reconciler = createReconciler()

			initialVersion := secretRefResolverDeployment.ResourceVersion

			CreateOperatorConfigurationResourceWithSpec(
				ctx,
				k8sClient,
				config.operatorConfigurationResourceSpec,
			)

			triggerOperatorConfigurationReconcileRequest(ctx, reconciler)
			verifyOperatorConfigurationResourceIsAvailable(ctx)

			expectedDeploymentAfterReconcile := config.expectedSecretRefResolverDeploymentAfterReconcile()
			gomegaTimeout := timeout
			gomgaWrapper := Eventually
			if !config.expectK8sClientUpdate {
				// For test cases where the initial controller deployment is already in the expected state (that is, it
				// matches what the operator configuration resource says), we use gomega's Consistently instead of
				// Eventually to make the test meaningful. We need to make sure that we do not update the controller
				// deployment at all for these cases.
				gomgaWrapper = Consistently
				gomegaTimeout = consistentlyTimeout
			}

			gomgaWrapper(func(g Gomega) {
				actualDeploymentAfterReconcile := LoadSecretRefResolverDeploymentOrFail(ctx, k8sClient, g)

				expectedSpec := expectedDeploymentAfterReconcile.Spec
				actualSpec := actualDeploymentAfterReconcile.Spec

				for _, spec := range []*appsv1.DeploymentSpec{&expectedSpec, &actualSpec} {
					// clean up defaults set by K8s, which are not relevant for the test
					cleanUpDeploymentSpecForDiff(spec)
				}

				matchesExpectations := reflect.DeepEqual(expectedSpec, actualSpec)
				if !matchesExpectations {
					patch, err := jsondiff.Compare(expectedSpec, actualSpec)
					g.Expect(err).ToNot(HaveOccurred())
					humanReadableDiff, err := json.MarshalIndent(patch, "", "  ")
					g.Expect(err).ToNot(HaveOccurred())

					g.Expect(matchesExpectations).To(
						BeTrue(),
						fmt.Sprintf("resulting deployment does not match expectations, here is a JSON patch of the differences:\n%s", humanReadableDiff),
					)
				}

				if !config.expectK8sClientUpdate {
					// make sure we did not execute an unnecessary update
					g.Expect(actualDeploymentAfterReconcile.ResourceVersion).To(Equal(initialVersion))
				} else {
					g.Expect(actualDeploymentAfterReconcile.ResourceVersion).ToNot(Equal(initialVersion))
				}

			}, gomegaTimeout, pollingInterval).Should(Succeed())
		},

			// | previous deployment state | operator config res | expected deployment afterwards     |
			// |---------------------------|---------------------|------------------------------------|
			// | no secret ref env var     | no secret ref       | no secret ref env var (no change)  |
			// | no secret ref env var     | has secret ref      | has secret ref env var (add)       |
			// | has secret ref env var    | no secret ref       | no secret ref env var (remove)     |
			// | has secret ref env var    | has secret ref      | has secret ref env var (no change) |

			// | no secret ref env var     | no secret ref       | no secret ref env var (no change)  |
			Entry("no secret ref env var -> no secret ref env var", SelfMonitoringAndApiAccessTestConfig{
				existingSecretRefResolverDeployment:               CreateSecretRefResolverDeploymentWithoutSecretRefEnvVar,
				operatorConfigurationResourceSpec:                 OperatorConfigurationResourceWithoutSelfMonitoringWithoutAuth,
				expectedSecretRefResolverDeploymentAfterReconcile: CreateSecretRefResolverDeploymentWithoutSecretRefEnvVar,
				expectK8sClientUpdate:                             false,
			}),

			// | no secret ref env var     | has secret ref      | has secret ref env var  (add)      |
			Entry("no secret ref env var -> has secret ref env var (add)", SelfMonitoringAndApiAccessTestConfig{
				existingSecretRefResolverDeployment:               CreateSecretRefResolverDeploymentWithoutSecretRefEnvVar,
				operatorConfigurationResourceSpec:                 OperatorConfigurationResourceDash0ExportWithoutApiEndpointWithSecretRef,
				expectedSecretRefResolverDeploymentAfterReconcile: CreateSecretRefResolverDeploymentWithSecretRefEnvVar,
				expectK8sClientUpdate:                             true,
			}),

			// | has secret ref env var    | no secret ref       | no secret ref env var (remove)     |
			Entry("has secret ref env var -> no secret ref env var (remove)", SelfMonitoringAndApiAccessTestConfig{
				existingSecretRefResolverDeployment:               CreateSecretRefResolverDeploymentWithSecretRefEnvVar,
				operatorConfigurationResourceSpec:                 OperatorConfigurationResourceWithoutSelfMonitoringWithoutAuth,
				expectedSecretRefResolverDeploymentAfterReconcile: CreateSecretRefResolverDeploymentWithoutSecretRefEnvVar,
				expectK8sClientUpdate:                             true,
			}),

			// | has secret ref env var    | has secret ref      | has secret ref env var (no change) |
			Entry("has secret ref env var -> has secret ref env var (no change)", SelfMonitoringAndApiAccessTestConfig{
				existingSecretRefResolverDeployment:               CreateSecretRefResolverDeploymentWithSecretRefEnvVar,
				operatorConfigurationResourceSpec:                 OperatorConfigurationResourceDash0ExportWithoutApiEndpointWithSecretRef,
				expectedSecretRefResolverDeploymentAfterReconcile: CreateSecretRefResolverDeploymentWithSecretRefEnvVar,
				expectK8sClientUpdate:                             false,
			}),
		)
	})

	Describe("updates all registered API clients", func() {
		BeforeAll(func() {
			secretRefResolverDeployment = CreateSecretRefResolverDeploymentWithoutSecretRefEnvVar()
			EnsureSecretRefResolverDeploymentExists(ctx, k8sClient, secretRefResolverDeployment)
		})

		AfterAll(func() {
			EnsureSecretRefResolverDeploymentDoesNotExist(ctx, k8sClient, secretRefResolverDeployment)
		})

		AfterEach(func() {
			RemoveOperatorConfigurationResource(ctx, k8sClient)
		})

		DescribeTable("by settings or removing the API config", func(config ApiClientSetRemoveTestConfig) {
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

			triggerOperatorConfigurationReconcileRequest(ctx, reconciler)
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
				if config.expectSetAuthToken {
					Expect(apiClient.setAuthTokenCalls).To(Equal(1))
					Expect(apiClient.removeAuthTokenCalls).To(Equal(0))
					Expect(apiClient.authToken).To(Equal(AuthorizationTokenTest))
				}
				if config.expectRemoveAuthToken {
					Expect(apiClient.setAuthTokenCalls).To(Equal(0))
					Expect(apiClient.removeAuthTokenCalls).To(Equal(1))
					Expect(apiClient.authToken).To(Equal(""))
				}
			}
		},

			// | no Dash0 export                           | remove         |
			Entry("no API endpoint, no Dash0 export", ApiClientSetRemoveTestConfig{
				operatorConfigurationResourceSpec: OperatorConfigurationResourceWithoutExport,
				expectSetApiEndpointAndDataset:    false,
				expectRemoveApiEndpointAndDataset: true,
				expectSetAuthToken:                false,
				expectRemoveAuthToken:             true,
			}),
			// | no API endpoint, token                    | remove         |
			Entry("no API endpoint, Dash0 export with token", ApiClientSetRemoveTestConfig{
				operatorConfigurationResourceSpec: OperatorConfigurationResourceDash0ExportWithoutApiEndpointWithToken,
				expectSetApiEndpointAndDataset:    false,
				expectRemoveApiEndpointAndDataset: true,
				expectSetAuthToken:                true,
				expectRemoveAuthToken:             false,
			}),
			// | no API endpoint, secret ref               | remove         |
			Entry("no API endpoint, Dash0 export with secret ref", ApiClientSetRemoveTestConfig{
				operatorConfigurationResourceSpec: OperatorConfigurationResourceDash0ExportWithoutApiEndpointWithSecretRef,
				expectSetApiEndpointAndDataset:    false,
				expectRemoveApiEndpointAndDataset: true,
				expectSetAuthToken:                false,
				expectRemoveAuthToken:             false,
			}),
			// | API endpoint, token                       | set            |
			Entry("API endpoint, Dash0 export with token", ApiClientSetRemoveTestConfig{
				operatorConfigurationResourceSpec: OperatorConfigurationResourceDash0ExportWithApiEndpointWithToken,
				expectSetApiEndpointAndDataset:    true,
				expectRemoveApiEndpointAndDataset: false,
				expectSetAuthToken:                true,
				expectRemoveAuthToken:             false,
			}),
			// | API endpoint, secret ref                  | set            |
			Entry("API endpoint, Dash0 export with secret ref", ApiClientSetRemoveTestConfig{
				operatorConfigurationResourceSpec: OperatorConfigurationResourceDash0ExportWithApiEndpointWithSecretRef,
				expectSetApiEndpointAndDataset:    true,
				expectRemoveApiEndpointAndDataset: false,
				expectSetAuthToken:                false,
				expectRemoveAuthToken:             false,
			}),
			// | API endpoint, token, custom dataset       | set            |
			Entry("API endpoint, Dash0 export with token, custom dataset", ApiClientSetRemoveTestConfig{
				operatorConfigurationResourceSpec: OperatorConfigurationResourceDash0ExportWithApiEndpointWithToken,
				dataset:                           "custom-dataset",
				expectSetApiEndpointAndDataset:    true,
				expectRemoveApiEndpointAndDataset: false,
				expectSetAuthToken:                true,
				expectRemoveAuthToken:             false,
			}),
			// | API endpoint, secret ref, custom dataset  | set            |
			Entry("API endpoint, Dash0 export with secret ref, custom dataset", ApiClientSetRemoveTestConfig{
				operatorConfigurationResourceSpec: OperatorConfigurationResourceDash0ExportWithApiEndpointWithSecretRef,
				dataset:                           "custom-dataset",
				expectSetApiEndpointAndDataset:    true,
				expectRemoveApiEndpointAndDataset: false,
				expectSetAuthToken:                false,
				expectRemoveAuthToken:             false,
			}),
		)
	})

	Describe("when creating the operator configuration resource", func() {

		BeforeAll(func() {
			secretRefResolverDeployment = CreateSecretRefResolverDeploymentWithoutSecretRefEnvVar()
			EnsureSecretRefResolverDeploymentExists(ctx, k8sClient, secretRefResolverDeployment)
		})

		AfterAll(func() {
			EnsureSecretRefResolverDeploymentDoesNotExist(ctx, k8sClient, secretRefResolverDeployment)
		})

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

				triggerOperatorConfigurationReconcileRequest(ctx, reconciler)

				VerifyCollectorResources(ctx, k8sClient, operatorNamespace, EndpointDash0Test, AuthorizationTokenTest)
			})

			DescribeTable("it starts the OTel SDK for self-monitoring in the operator manager deployment",
				func(config SelfMonitoringTestConfig) {
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

					reconciler.OTelSdkStarter.WaitForOTelConfig([]selfmonitoringapiaccess.SelfMonitoringMetricsClient{
						selfMonitoringMetricsClient1,
						selfMonitoringMetricsClient2,
					})
					triggerOperatorConfigurationReconcileRequest(ctx, reconciler)
					verifyOperatorConfigurationResourceIsAvailable(ctx)

					go func() {
						time.Sleep(30 * time.Millisecond)
						if config.simulateSecretRefResolve {
							ctx := context.Background()
							logger := log.FromContext(ctx)
							reconciler.OTelSdkStarter.SetAuthToken(ctx, AuthorizationTokenTest, &logger)
						}
					}()

					Eventually(func(g Gomega) {
						sdkIsActive, activeOTelSdkConfig, _ :=
							reconciler.OTelSdkStarter.ForTestOnlyGetState()
						g.Expect(sdkIsActive).To(Equal(config.expectedSdkIsActive))
						if config.expectHasConfig {
							g.Expect(activeOTelSdkConfig).ToNot(BeNil())
							g.Expect(activeOTelSdkConfig.Endpoint).To(Equal(config.expectedEndpoint))
							g.Expect(activeOTelSdkConfig.Protocol).To(Equal(config.expectedProtocol))
							verifyOperatorManagerResourceAttributes(g, activeOTelSdkConfig.ResourceAttributes)

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
								g.Expect(smc.initializeSelfMonitoringMetrics).To(Equal(1))
							} else {
								g.Expect(delegatingZapCoreWrapper.RootDelegatingZapCore.ForTestOnlyHasDelegate()).To(BeFalse())
								g.Expect(smc.initializeSelfMonitoringMetrics).To(Equal(0))
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
					createExport:             Dash0ExportWithEndpointAndSecretRef,
					selfMonitoringEnabled:    true,
					simulateSecretRefResolve: true,
					expectedSdkIsActive:      true,
					expectHasConfig:          true,
					expectedEndpoint:         EndpointDash0Test,
					expectedProtocol:         common.ProtocolGrpc,
					expectedHeaders: map[string]string{
						util.AuthorizationHeaderName: AuthorizationHeaderTest,
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

		BeforeAll(func() {
			secretRefResolverDeployment = CreateSecretRefResolverDeploymentWithoutSecretRefEnvVar()
			EnsureSecretRefResolverDeploymentExists(ctx, k8sClient, secretRefResolverDeployment)
		})

		AfterAll(func() {
			EnsureSecretRefResolverDeploymentDoesNotExist(ctx, k8sClient, secretRefResolverDeployment)
		})

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

			reconciler.OTelSdkStarter.WaitForOTelConfig([]selfmonitoringapiaccess.SelfMonitoringMetricsClient{
				selfMonitoringMetricsClient1,
				selfMonitoringMetricsClient2,
			})
			triggerOperatorConfigurationReconcileRequest(ctx, reconciler)
			verifyOperatorConfigurationResourceIsAvailable(ctx)

			Eventually(func(g Gomega) {
				g.Expect(delegatingZapCoreWrapper.RootDelegatingZapCore.ForTestOnlyHasDelegate()).To(BeTrue())
				sdkIsActive, activeOTelSdkConfig, _ :=
					reconciler.OTelSdkStarter.ForTestOnlyGetState()
				g.Expect(sdkIsActive).To(BeTrue())
				g.Expect(activeOTelSdkConfig).ToNot(BeNil())
			}, 1*time.Second, pollingInterval).Should(Succeed())
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

			triggerOperatorConfigurationReconcileRequest(ctx, reconciler)
			verifyOperatorConfigurationResourceIsAvailable(ctx)

			// verify the OTel SDK has been shut down
			Eventually(func(g Gomega) {
				g.Expect(delegatingZapCoreWrapper.RootDelegatingZapCore.ForTestOnlyHasDelegate()).To(BeFalse())
				sdkIsActive, activeOTelSdkConfig, _ :=
					reconciler.OTelSdkStarter.ForTestOnlyGetState()
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

			triggerOperatorConfigurationReconcileRequest(ctx, reconciler)
			verifyOperatorConfigurationResourceIsAvailable(ctx)

			// verify the OTel SDK has been shut down
			Eventually(func(g Gomega) {
				g.Expect(delegatingZapCoreWrapper.RootDelegatingZapCore.ForTestOnlyHasDelegate()).To(BeFalse())
				sdkIsActive, _, _ :=
					reconciler.OTelSdkStarter.ForTestOnlyGetState()
				g.Expect(sdkIsActive).To(BeFalse())
			}, 1*time.Second, pollingInterval).Should(Succeed())

			// update operator configuration _once again_, adding the export config back and enabling self-monitoring
			// again
			updatedOperatorConfigurationResource = LoadOperatorConfigurationResourceOrFail(ctx, k8sClient, Default)
			updatedOperatorConfigurationResource.Spec.SelfMonitoring.Enabled = ptr.To(true)
			updatedOperatorConfigurationResource.Spec.Export = Dash0ExportWithEndpointAndToken()
			Expect(k8sClient.Update(ctx, updatedOperatorConfigurationResource)).To(Succeed())

			triggerOperatorConfigurationReconcileRequest(ctx, reconciler)
			verifyOperatorConfigurationResourceIsAvailable(ctx)

			// verify the OTel SDK has been started again
			Eventually(func(g Gomega) {
				g.Expect(delegatingZapCoreWrapper.RootDelegatingZapCore.ForTestOnlyHasDelegate()).To(BeTrue())
				sdkIsActive, activeOTelSdkConfig, _ :=
					reconciler.OTelSdkStarter.ForTestOnlyGetState()
				g.Expect(sdkIsActive).To(BeTrue())

				g.Expect(activeOTelSdkConfig).ToNot(BeNil())
				g.Expect(activeOTelSdkConfig.Endpoint).To(Equal(EndpointDash0Test))
				g.Expect(activeOTelSdkConfig.Protocol).To(Equal(common.ProtocolGrpc))
				verifyOperatorManagerResourceAttributes(g, activeOTelSdkConfig.ResourceAttributes)

				g.Expect(activeOTelSdkConfig.Headers).To(HaveLen(1))
				g.Expect(activeOTelSdkConfig.Headers[util.AuthorizationHeaderName]).To(Equal(AuthorizationHeaderTest))
			}, 1*time.Second, pollingInterval).Should(Succeed())
		})

		It("restarts the OTel SDK when it is already running and there is a configuration change", func() {
			for _, apiClient := range []*DummyApiClient{apiClient1, apiClient2} {
				apiClient.ResetCallCounts()
			}

			// update operator configuration, but do not disable self-monitoring, only change endpoint, dataset and
			// token
			updatedOperatorConfigurationResource := LoadOperatorConfigurationResourceOrFail(ctx, k8sClient, Default)
			updatedOperatorConfigurationResource.Spec.Export = &dash0v1alpha1.Export{
				Dash0: &dash0v1alpha1.Dash0Configuration{
					Endpoint: EndpointDash0TestAlternative,
					Authorization: dash0v1alpha1.Authorization{
						Token: &AuthorizationTokenTestAlternative,
					},
					Dataset: DatasetCustomTest,
				},
			}
			Expect(k8sClient.Update(ctx, updatedOperatorConfigurationResource)).To(Succeed())

			triggerOperatorConfigurationReconcileRequest(ctx, reconciler)
			verifyOperatorConfigurationResourceIsAvailable(ctx)

			for _, apiClient := range []*DummyApiClient{apiClient1, apiClient2} {
				Expect(apiClient.setAuthTokenCalls).To(Equal(1))
			}

			// Verify the OTel SDK has been restarted - well, within this test case we can not actually verify that the
			// SDK has been shut down before being restarted, but we can verify the new config values. The aspect of
			// shutting it down and actually restarting it is covered in otel_init_wait_test.go
			Eventually(func(g Gomega) {
				g.Expect(delegatingZapCoreWrapper.RootDelegatingZapCore.ForTestOnlyHasDelegate()).To(BeTrue())

				sdkIsActive, activeOTelSdkConfig, _ :=
					reconciler.OTelSdkStarter.ForTestOnlyGetState()
				g.Expect(sdkIsActive).To(BeTrue())

				g.Expect(activeOTelSdkConfig).ToNot(BeNil())
				g.Expect(activeOTelSdkConfig.Endpoint).To(Equal(EndpointDash0TestAlternative))
				g.Expect(activeOTelSdkConfig.Protocol).To(Equal(common.ProtocolGrpc))
				verifyOperatorManagerResourceAttributes(g, activeOTelSdkConfig.ResourceAttributes)

				g.Expect(activeOTelSdkConfig.Headers).To(HaveLen(2))
				g.Expect(activeOTelSdkConfig.Headers[util.AuthorizationHeaderName]).To(
					Equal(AuthorizationHeaderTestAlternative))
				g.Expect(activeOTelSdkConfig.Headers[util.Dash0DatasetHeaderName]).To(Equal(DatasetCustomTest))
			}, 1*time.Second, pollingInterval).Should(Succeed())
		})

		It("shuts down the OTel SDK and removes config values from API clients when the operator configuration resource is deleted", func() {
			for _, apiClient := range []*DummyApiClient{apiClient1, apiClient2} {
				apiClient.ResetCallCounts()
			}
			resource := LoadOperatorConfigurationResourceOrFail(ctx, k8sClient, Default)
			Expect(k8sClient.Delete(ctx, resource)).To(Succeed())

			triggerOperatorConfigurationReconcileRequest(ctx, reconciler)
			VerifyOperatorConfigurationResourceByNameDoesNotExist(ctx, k8sClient, Default, resource.Name)

			for _, apiClient := range []*DummyApiClient{apiClient1, apiClient2} {
				Expect(apiClient.setApiEndpointCalls).To(Equal(0))
				Expect(apiClient.setAuthTokenCalls).To(Equal(0))
				Expect(apiClient.removeApiEndpointCalls).To(Equal(1))
				Expect(apiClient.removeAuthTokenCalls).To(Equal(1))
				Expect(apiClient.apiConfig).To(BeNil())
			}

			Eventually(func(g Gomega) {
				g.Expect(delegatingZapCoreWrapper.RootDelegatingZapCore.ForTestOnlyHasDelegate()).To(BeFalse())
				sdkIsActive, activeOTelSdkConfig, authTokenFromSecretRef :=
					reconciler.OTelSdkStarter.ForTestOnlyGetState()
				g.Expect(sdkIsActive).To(BeFalse())
				g.Expect(activeOTelSdkConfig).To(BeNil())
				g.Expect(authTokenFromSecretRef).To(BeNil())
			})
		})

		It("should remove the collector resources when the operator configuration resource is deleted", func() {
			VerifyCollectorResources(ctx, k8sClient, operatorNamespace, EndpointDash0Test, AuthorizationTokenTest)

			resource := LoadOperatorConfigurationResourceOrFail(ctx, k8sClient, Default)
			Expect(k8sClient.Delete(ctx, resource)).To(Succeed())

			triggerOperatorConfigurationReconcileRequest(ctx, reconciler)
			VerifyOperatorConfigurationResourceByNameDoesNotExist(ctx, k8sClient, Default, resource.Name)

			VerifyCollectorResourcesDoNotExist(ctx, k8sClient, operatorNamespace)
		})
	})
})

func verifyOperatorManagerResourceAttributes(g Gomega, resourceAttributes []attribute.KeyValue) {
	g.Expect(resourceAttributes).To(HaveLen(9))
	g.Expect(
		getResourceAttribute(resourceAttributes, "service.namespace")).To(
		Equal("dash0-operator"))
	g.Expect(
		getResourceAttribute(resourceAttributes, "service.name")).To(
		Equal("operator-manager"))
	g.Expect(
		getResourceAttribute(resourceAttributes, "service.version")).To(
		Equal("1.2.3"))
	g.Expect(
		getResourceAttribute(resourceAttributes, "k8s.cluster.uid")).To(
		Equal(ClusterUIDTest))
	g.Expect(
		getResourceAttribute(resourceAttributes, "k8s.cluster.name")).To(
		Equal(ClusterNameTest))
	g.Expect(
		getResourceAttribute(resourceAttributes, "k8s.namespace.name")).To(
		Equal(OperatorNamespace))
	g.Expect(
		getResourceAttribute(resourceAttributes, "k8s.deployment.uid")).To(
		Equal(OperatorManagerDeploymentUIDStr))
	g.Expect(
		getResourceAttribute(resourceAttributes, "k8s.deployment.name")).To(
		Equal(OperatorManagerDeploymentName))
	g.Expect(
		getResourceAttribute(resourceAttributes, "k8s.pod.name")).To(
		Equal(OperatorPodName))
}

func getResourceAttribute(attributes []attribute.KeyValue, key string) string {
	for _, a := range attributes {
		if string(a.Key) == key {
			return a.Value.AsString()
		}
	}
	return ""
}

func cleanUpDeploymentSpecForDiff(spec *appsv1.DeploymentSpec) {
	for i := range spec.Template.Spec.Containers {
		spec.Template.Spec.Containers[i].TerminationMessagePath = ""
		spec.Template.Spec.Containers[i].TerminationMessagePolicy = ""
		spec.Template.Spec.Containers[i].ImagePullPolicy = ""
	}
	spec.Template.Spec.RestartPolicy = ""
	spec.Template.Spec.DNSPolicy = ""
	spec.Template.Spec.DeprecatedServiceAccount = ""
	spec.Template.Spec.SchedulerName = ""
	spec.Strategy = appsv1.DeploymentStrategy{}
	spec.RevisionHistoryLimit = nil
	spec.ProgressDeadlineSeconds = nil
}

func createReconciler() *OperatorConfigurationReconciler {
	oTelColResourceManager := &otelcolresources.OTelColResourceManager{
		Client:                    k8sClient,
		Scheme:                    k8sClient.Scheme(),
		OperatorManagerDeployment: OperatorManagerDeployment,
		OTelCollectorNamePrefix:   OTelCollectorNamePrefixTest,
		OTelColExtraConfig:        &otelcolresources.OTelExtraConfigDefaults,
	}
	backendConnectionManager := &backendconnection.BackendConnectionManager{
		Client:                 k8sClient,
		Clientset:              clientset,
		OTelColResourceManager: oTelColResourceManager,
	}
	delegatingZapCoreWrapper = zaputil.NewDelegatingZapCoreWrapper()
	otelSdkStarter := selfmonitoringapiaccess.NewOTelSdkStarter(delegatingZapCoreWrapper)

	return &OperatorConfigurationReconciler{
		Client:    k8sClient,
		Clientset: clientset,
		Recorder:  recorder,
		ApiClients: []ApiClient{
			apiClient1,
			apiClient2,
		},
		BackendConnectionManager:        backendConnectionManager,
		PseudoClusterUID:                ClusterUIDTest,
		OperatorDeploymentNamespace:     OperatorManagerDeployment.Namespace,
		OperatorDeploymentUID:           OperatorManagerDeployment.UID,
		OperatorDeploymentName:          OperatorManagerDeployment.Name,
		OperatorManagerPodName:          OperatorPodName,
		OTelSdkStarter:                  otelSdkStarter,
		DanglingEventsTimeouts:          &DanglingEventsTimeoutsTest,
		Images:                          TestImages,
		OperatorNamespace:               OperatorNamespace,
		SecretRefResolverDeploymentName: SecretRefResolverDeploymentName,
	}
}

func triggerOperatorConfigurationReconcileRequest(ctx context.Context, reconciler *OperatorConfigurationReconciler) {
	triggerOperatorReconcileRequestForName(ctx, reconciler, OperatorConfigurationResourceName)
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
		availableCondition = meta.FindStatusCondition(resource.Status.Conditions, string(dash0v1alpha1.ConditionTypeAvailable))
		g.Expect(availableCondition.Status).To(Equal(metav1.ConditionTrue))
		degraded := meta.FindStatusCondition(resource.Status.Conditions, string(dash0v1alpha1.ConditionTypeDegraded))
		g.Expect(degraded).To(BeNil())
	}, timeout, pollingInterval).Should(Succeed())
}

type DummyApiClient struct {
	setApiEndpointCalls    int
	removeApiEndpointCalls int
	apiConfig              *ApiConfig
	setAuthTokenCalls      int
	removeAuthTokenCalls   int
	authToken              string
}

func (c *DummyApiClient) SetApiEndpointAndDataset(_ context.Context, apiConfig *ApiConfig, _ *logr.Logger) {
	c.setApiEndpointCalls++
	c.apiConfig = apiConfig
}

func (c *DummyApiClient) RemoveApiEndpointAndDataset(_ context.Context, _ *logr.Logger) {
	c.removeApiEndpointCalls++
	c.apiConfig = nil
}

func (c *DummyApiClient) SetAuthToken(_ context.Context, authToken string, _ *logr.Logger) {
	c.setAuthTokenCalls++
	c.authToken = authToken
}

func (c *DummyApiClient) RemoveAuthToken(_ context.Context, _ *logr.Logger) {
	c.removeAuthTokenCalls++
	c.authToken = ""
}

func (c *DummyApiClient) ResetCallCounts() {
	c.setApiEndpointCalls = 0
	c.removeApiEndpointCalls = 0
	c.setAuthTokenCalls = 0
	c.removeAuthTokenCalls = 0
}

type DummySelfMonitoringMetricsClient struct {
	initializeSelfMonitoringMetrics int
}

func (c *DummySelfMonitoringMetricsClient) InitializeSelfMonitoringMetrics(_ otelmetric.Meter, _ string, _ *logr.Logger) {
	c.initializeSelfMonitoringMetrics++
}
