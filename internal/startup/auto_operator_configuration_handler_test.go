// SPDX-FileCopyrightText: Copyright 2024 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package startup

import (
	"context"
	"encoding/json"
	"time"

	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"

	dash0common "github.com/dash0hq/dash0-operator/api/operator/common"
	"github.com/dash0hq/dash0-operator/api/operator/v1alpha1"
	"github.com/dash0hq/dash0-operator/internal/util"
	"github.com/dash0hq/dash0-operator/internal/util/logd"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	. "github.com/dash0hq/dash0-operator/test/util"
)

var (
	secretRef = SecretRef{
		Name: "test-secret",
		Key:  "test-key",
	}
	operatorConfigurationValuesWithToken = OperatorConfigurationValues{
		Endpoint: EndpointDash0Test,
		Token:    AuthorizationTokenTest,
	}
	operatorConfigurationValuesWithTokenAndTelemetryCollection = OperatorConfigurationValues{
		Endpoint:                   EndpointDash0Test,
		Token:                      AuthorizationTokenTest,
		TelemetryCollectionEnabled: true,
	}
	operatorConfigurationValuesWithSecretRef = OperatorConfigurationValues{
		Endpoint:  EndpointDash0Test,
		SecretRef: secretRef,
	}
)

var _ = Describe(
	"Create an operator configuration resource at startup", Ordered, func() {

		ctx := context.Background()
		logger := logd.FromContext(ctx)
		var readyCheckExecuter *ReadyCheckExecuter

		BeforeAll(
			func() {
				EnsureOperatorNamespaceExists(ctx, k8sClient)
			},
		)

		BeforeEach(
			func() {
				readyCheckExecuter = NewReadyCheckExecuter(
					k8sClient,
					OperatorNamespace,
					OperatorWebhookServiceName,
				)
				readyCheckExecuter.bypassWebhookCheck = true
			},
		)

		AfterEach(
			func() {
				DeleteAllOperatorConfigurationResources(ctx, k8sClient)
			},
		)

		It(
			"should fail validation if neither an endpoint nor exports have been provided", func() {
				handler := NewAutoOperatorConfigurationResourceHandler(
					k8sClient,
					readyCheckExecuter,
					OperatorConfigurationValues{
						Token: AuthorizationTokenTest,
					},
					util.ExtraConfig{},
				)
				handler.NotifyOperatorManagerJustBecameLeader(ctx, logger)
				_, err := handler.CreateOrUpdateOperatorConfigurationResource(ctx, logger)
				Expect(err).To(
					MatchError(
						ContainSubstring(
							"invalid operator configuration: the operator configuration resource is managed via Helm, but " +
								"neither --operator-configuration-endpoint (Helm value operator.dash0Export.endpoint) nor " +
								"any export (Helm value operator.exports) has been provided",
						),
					),
				)
			},
		)

		It(
			"should fail validation if no token and no secret reference have been provided", func() {
				handler := NewAutoOperatorConfigurationResourceHandler(
					k8sClient,
					readyCheckExecuter,
					OperatorConfigurationValues{
						Endpoint: AuthorizationTokenTest,
					},
					util.ExtraConfig{},
				)
				handler.NotifyOperatorManagerJustBecameLeader(ctx, logger)
				_, err := handler.CreateOrUpdateOperatorConfigurationResource(ctx, logger)
				Expect(err).To(
					MatchError(
						ContainSubstring(
							"neither --operator-configuration-token nor --operator-configuration-secret-ref-name have " +
								"been provided",
						),
					),
				)
			},
		)

		It(
			"should fail validation if no token and no secret reference key have been provided", func() {
				handler := NewAutoOperatorConfigurationResourceHandler(
					k8sClient,
					readyCheckExecuter,
					OperatorConfigurationValues{
						Endpoint: AuthorizationTokenTest,
						SecretRef: SecretRef{
							Name: "test-secret",
						},
					},
					util.ExtraConfig{},
				)
				_, err := handler.CreateOrUpdateOperatorConfigurationResource(ctx, logger)
				Expect(err).To(
					MatchError(
						ContainSubstring(
							"neither --operator-configuration-token nor --operator-configuration-secret-ref-key have " +
								"been provided",
						),
					),
				)
			},
		)

		It(
			"should create a new operator configuration resource with a token", func() {
				handler := NewAutoOperatorConfigurationResourceHandler(
					k8sClient,
					readyCheckExecuter,
					operatorConfigurationValuesWithToken,
					util.ExtraConfig{},
				)
				handler.NotifyOperatorManagerJustBecameLeader(ctx, logger)
				_, err := handler.CreateOrUpdateOperatorConfigurationResource(ctx, logger)
				Expect(err).ToNot(HaveOccurred())

				Eventually(
					func(g Gomega) {
						operatorConfiguration := v1alpha1.Dash0OperatorConfiguration{}
						err := k8sClient.Get(
							ctx, types.NamespacedName{
								Name: util.OperatorConfigurationAutoResourceName,
							}, &operatorConfiguration,
						)
						g.Expect(err).ToNot(HaveOccurred())

						g.Expect(operatorConfiguration.Annotations).To(HaveLen(3))
						g.Expect(operatorConfiguration.Annotations["argocd.argoproj.io/sync-options"]).To(Equal("Prune=false"))
						g.Expect(operatorConfiguration.Annotations["argocd.argoproj.io/compare-options"]).To(Equal("IgnoreExtraneous"))
						g.Expect(operatorConfiguration.Annotations[managedByHelmAnnotationKey]).To(
							Equal(
								"DO NOT EDIT THIS RESOURCE. This operator configuration resource is managed by the operator Helm " +
									"chart (Helm values operator.dash0Export.* and operator.exports), manual modifications to this " +
									"resource (i.e. via kubectl or k9s) will be overwritten when the operator manager is restarted " +
									"or the operator is updated to a new version. See " +
									"https://github.com/dash0hq/dash0-operator/blob/main/helm-chart/" +
									"dash0-operator/docs/configuration.md#" +
									"notes-on-creating-the-operator-configuration-resource-via-helm.",
							),
						)

						spec := operatorConfiguration.Spec
						export := spec.Exports[0]
						g.Expect(export).ToNot(BeNil())
						dash0Export := export.Dash0
						g.Expect(dash0Export).ToNot(BeNil())
						g.Expect(export.Grpc).To(BeNil())
						g.Expect(export.Http).To(BeNil())
						g.Expect(dash0Export.Endpoint).To(Equal(EndpointDash0Test))
						g.Expect(dash0Export.Authorization.Token).ToNot(BeNil())
						g.Expect(*dash0Export.Authorization.Token).To(Equal(AuthorizationTokenTest))
						g.Expect(dash0Export.Authorization.SecretRef).To(BeNil())
						g.Expect(dash0Export.Authorization.SecretRef).To(BeNil())
						g.Expect(*spec.SelfMonitoring.Enabled).To(BeFalse())
						g.Expect(*spec.KubernetesInfrastructureMetricsCollection.Enabled).To(BeFalse())
						g.Expect(*spec.CollectPodLabelsAndAnnotations.Enabled).To(BeFalse())
						g.Expect(spec.ClusterName).To(BeEmpty())
					}, 5*time.Second, 100*time.Millisecond,
				).Should(Succeed())
			},
		)

		It(
			"should create a new operator configuration resource with a secret reference", func() {
				handler := NewAutoOperatorConfigurationResourceHandler(
					k8sClient,
					readyCheckExecuter,
					operatorConfigurationValuesWithSecretRef,
					util.ExtraConfig{},
				)
				handler.NotifyOperatorManagerJustBecameLeader(ctx, logger)
				_, err := handler.CreateOrUpdateOperatorConfigurationResource(ctx, logger)
				Expect(err).ToNot(HaveOccurred())

				Eventually(
					func(g Gomega) {
						operatorConfiguration := v1alpha1.Dash0OperatorConfiguration{}
						err := k8sClient.Get(
							ctx, types.NamespacedName{
								Name: util.OperatorConfigurationAutoResourceName,
							}, &operatorConfiguration,
						)
						g.Expect(err).ToNot(HaveOccurred())

						g.Expect(operatorConfiguration.Annotations).To(HaveLen(3))
						g.Expect(operatorConfiguration.Annotations["argocd.argoproj.io/sync-options"]).To(Equal("Prune=false"))
						g.Expect(operatorConfiguration.Annotations["argocd.argoproj.io/compare-options"]).To(Equal("IgnoreExtraneous"))

						export := operatorConfiguration.Spec.Exports[0]
						g.Expect(export).ToNot(BeNil())
						dash0Export := export.Dash0
						g.Expect(dash0Export).ToNot(BeNil())
						g.Expect(export.Grpc).To(BeNil())
						g.Expect(export.Http).To(BeNil())
						g.Expect(dash0Export.Endpoint).To(Equal(EndpointDash0Test))
						g.Expect(dash0Export.Authorization.Token).To(BeNil())
						g.Expect(dash0Export.Authorization.SecretRef).ToNot(BeNil())
						g.Expect(dash0Export.Authorization.SecretRef.Name).To(Equal("test-secret"))
						g.Expect(dash0Export.Authorization.SecretRef.Key).To(Equal("test-key"))
					}, 5*time.Second, 100*time.Millisecond,
				).Should(Succeed())
			},
		)

		It(
			"should wait for the replica to become leader", func() {
				handler := NewAutoOperatorConfigurationResourceHandler(
					k8sClient,
					readyCheckExecuter,
					operatorConfigurationValuesWithToken,
					util.ExtraConfig{},
				)
				_, err := handler.CreateOrUpdateOperatorConfigurationResource(ctx, logger)
				Expect(err).ToNot(HaveOccurred())

				// The handler should not be able to proceed if we do not notify it about getting elected as leader.
				Consistently(
					func(g Gomega) {
						operatorConfiguration := v1alpha1.Dash0OperatorConfiguration{}
						err := k8sClient.Get(
							ctx, types.NamespacedName{
								Name: util.OperatorConfigurationAutoResourceName,
							}, &operatorConfiguration,
						)
						g.Expect(err).To(MatchError("dash0operatorconfigurations.operator.dash0.com \"dash0-operator-configuration-auto-resource\" not found"))
					}, 500*time.Millisecond, 100*time.Millisecond,
				).Should(Succeed())

				// now make this replica the leader, which should allow the handler to proceed
				handler.NotifyOperatorManagerJustBecameLeader(ctx, logger)

				Eventually(
					func(g Gomega) {
						operatorConfiguration := v1alpha1.Dash0OperatorConfiguration{}
						err := k8sClient.Get(
							ctx, types.NamespacedName{
								Name: util.OperatorConfigurationAutoResourceName,
							}, &operatorConfiguration,
						)
						g.Expect(err).ToNot(HaveOccurred())
					}, 5*time.Second, 100*time.Millisecond,
				).Should(Succeed())
			},
		)

		It(
			"should wait for the webhook service endpoint ready check", func() {
				readyCheckExecuter.bypassWebhookCheck = false
				handler := NewAutoOperatorConfigurationResourceHandler(
					k8sClient,
					readyCheckExecuter,
					operatorConfigurationValuesWithToken,
					util.ExtraConfig{},
				)
				handler.NotifyOperatorManagerJustBecameLeader(ctx, logger)
				_, err := handler.CreateOrUpdateOperatorConfigurationResource(ctx, logger)
				Expect(err).ToNot(HaveOccurred())

				// The handler should not be able to proceed if we do not skip the webhook
				// check (we never start the ready check executer in the test).
				Consistently(
					func(g Gomega) {
						operatorConfiguration := v1alpha1.Dash0OperatorConfiguration{}
						err := k8sClient.Get(
							ctx, types.NamespacedName{
								Name: util.OperatorConfigurationAutoResourceName,
							}, &operatorConfiguration,
						)
						g.Expect(err).To(MatchError("dash0operatorconfigurations.operator.dash0.com \"dash0-operator-configuration-auto-resource\" not found"))
					}, 500*time.Millisecond, 100*time.Millisecond,
				).Should(Succeed())
			},
		)

		It(
			"should set the API endpoint", func() {
				handler := NewAutoOperatorConfigurationResourceHandler(
					k8sClient,
					readyCheckExecuter,
					OperatorConfigurationValues{
						Endpoint:    EndpointDash0Test,
						Token:       AuthorizationTokenTest,
						ApiEndpoint: ApiEndpointTest,
					},
					util.ExtraConfig{},
				)
				handler.NotifyOperatorManagerJustBecameLeader(ctx, logger)
				_, err := handler.CreateOrUpdateOperatorConfigurationResource(ctx, logger)
				Expect(err).ToNot(HaveOccurred())

				Eventually(
					func(g Gomega) {
						operatorConfiguration := v1alpha1.Dash0OperatorConfiguration{}
						err := k8sClient.Get(
							ctx, types.NamespacedName{
								Name: util.OperatorConfigurationAutoResourceName,
							}, &operatorConfiguration,
						)
						g.Expect(err).ToNot(HaveOccurred())

						g.Expect(operatorConfiguration.Annotations).To(HaveLen(3))
						g.Expect(operatorConfiguration.Annotations["argocd.argoproj.io/sync-options"]).To(Equal("Prune=false"))
						g.Expect(operatorConfiguration.Annotations["argocd.argoproj.io/compare-options"]).To(Equal("IgnoreExtraneous"))

						export := operatorConfiguration.Spec.Exports[0]
						g.Expect(export).ToNot(BeNil())
						dash0Export := export.Dash0
						g.Expect(dash0Export).ToNot(BeNil())
						g.Expect(dash0Export.ApiEndpoint).To(Equal(ApiEndpointTest))
					}, 5*time.Second, 100*time.Millisecond,
				).Should(Succeed())
			},
		)

		It(
			"should set a custom dataset", func() {
				handler := NewAutoOperatorConfigurationResourceHandler(
					k8sClient,
					readyCheckExecuter,
					OperatorConfigurationValues{
						Endpoint: EndpointDash0Test,
						Token:    AuthorizationTokenTest,
						Dataset:  "custom",
					},
					util.ExtraConfig{},
				)
				handler.NotifyOperatorManagerJustBecameLeader(ctx, logger)
				_, err := handler.CreateOrUpdateOperatorConfigurationResource(ctx, logger)
				Expect(err).ToNot(HaveOccurred())

				Eventually(
					func(g Gomega) {
						operatorConfiguration := v1alpha1.Dash0OperatorConfiguration{}
						err := k8sClient.Get(
							ctx, types.NamespacedName{
								Name: util.OperatorConfigurationAutoResourceName,
							}, &operatorConfiguration,
						)
						g.Expect(err).ToNot(HaveOccurred())

						g.Expect(operatorConfiguration.Annotations).To(HaveLen(3))
						g.Expect(operatorConfiguration.Annotations["argocd.argoproj.io/sync-options"]).To(Equal("Prune=false"))
						g.Expect(operatorConfiguration.Annotations["argocd.argoproj.io/compare-options"]).To(Equal("IgnoreExtraneous"))

						export := operatorConfiguration.Spec.Exports[0]
						g.Expect(export).ToNot(BeNil())
						dash0Export := export.Dash0
						g.Expect(dash0Export).ToNot(BeNil())
						g.Expect(dash0Export.Dataset).To(Equal("custom"))
					}, 5*time.Second, 100*time.Millisecond,
				).Should(Succeed())
			},
		)

		It(
			"should set keepalive configuration", func() {
				handler := NewAutoOperatorConfigurationResourceHandler(
					k8sClient,
					readyCheckExecuter,
					OperatorConfigurationValues{
						Endpoint:                     EndpointDash0Test,
						Token:                        AuthorizationTokenTest,
						KeepaliveTime:                "30s",
						KeepaliveTimeout:             "10s",
						KeepalivePermitWithoutStream: true,
					},
					util.ExtraConfig{},
				)
				handler.NotifyOperatorManagerJustBecameLeader(ctx, logger)
				_, err := handler.CreateOrUpdateOperatorConfigurationResource(ctx, logger)
				Expect(err).ToNot(HaveOccurred())

				Eventually(
					func(g Gomega) {
						operatorConfiguration := v1alpha1.Dash0OperatorConfiguration{}
						err := k8sClient.Get(
							ctx, types.NamespacedName{
								Name: util.OperatorConfigurationAutoResourceName,
							}, &operatorConfiguration,
						)
						g.Expect(err).ToNot(HaveOccurred())

						export := operatorConfiguration.Spec.Exports[0]
						dash0Export := export.Dash0
						g.Expect(dash0Export).ToNot(BeNil())
						g.Expect(dash0Export.Keepalive).ToNot(BeNil())
						g.Expect(dash0Export.Keepalive.Time).ToNot(BeNil())
						g.Expect(*dash0Export.Keepalive.Time).To(Equal("30s"))
						g.Expect(dash0Export.Keepalive.Timeout).ToNot(BeNil())
						g.Expect(*dash0Export.Keepalive.Timeout).To(Equal("10s"))
						g.Expect(dash0Export.Keepalive.PermitWithoutStream).ToNot(BeNil())
						g.Expect(*dash0Export.Keepalive.PermitWithoutStream).To(BeTrue())
					}, 5*time.Second, 100*time.Millisecond,
				).Should(Succeed())
			},
		)

		It(
			"should not set keepalive when no keepalive values are provided", func() {
				handler := NewAutoOperatorConfigurationResourceHandler(
					k8sClient,
					readyCheckExecuter,
					operatorConfigurationValuesWithToken,
					util.ExtraConfig{},
				)
				handler.NotifyOperatorManagerJustBecameLeader(ctx, logger)
				_, err := handler.CreateOrUpdateOperatorConfigurationResource(ctx, logger)
				Expect(err).ToNot(HaveOccurred())

				Eventually(
					func(g Gomega) {
						operatorConfiguration := v1alpha1.Dash0OperatorConfiguration{}
						err := k8sClient.Get(
							ctx, types.NamespacedName{
								Name: util.OperatorConfigurationAutoResourceName,
							}, &operatorConfiguration,
						)
						g.Expect(err).ToNot(HaveOccurred())

						export := operatorConfiguration.Spec.Exports[0]
						dash0Export := export.Dash0
						g.Expect(dash0Export).ToNot(BeNil())
						g.Expect(dash0Export.Keepalive).To(BeNil())
					}, 5*time.Second, 100*time.Millisecond,
				).Should(Succeed())
			},
		)

		It(
			"should set partial keepalive configuration", func() {
				handler := NewAutoOperatorConfigurationResourceHandler(
					k8sClient,
					readyCheckExecuter,
					OperatorConfigurationValues{
						Endpoint:      EndpointDash0Test,
						Token:         AuthorizationTokenTest,
						KeepaliveTime: "60s",
					},
					util.ExtraConfig{},
				)
				handler.NotifyOperatorManagerJustBecameLeader(ctx, logger)
				_, err := handler.CreateOrUpdateOperatorConfigurationResource(ctx, logger)
				Expect(err).ToNot(HaveOccurred())

				Eventually(
					func(g Gomega) {
						operatorConfiguration := v1alpha1.Dash0OperatorConfiguration{}
						err := k8sClient.Get(
							ctx, types.NamespacedName{
								Name: util.OperatorConfigurationAutoResourceName,
							}, &operatorConfiguration,
						)
						g.Expect(err).ToNot(HaveOccurred())

						export := operatorConfiguration.Spec.Exports[0]
						dash0Export := export.Dash0
						g.Expect(dash0Export).ToNot(BeNil())
						g.Expect(dash0Export.Keepalive).ToNot(BeNil())
						g.Expect(dash0Export.Keepalive.Time).ToNot(BeNil())
						g.Expect(*dash0Export.Keepalive.Time).To(Equal("60s"))
						g.Expect(dash0Export.Keepalive.Timeout).To(BeNil())
						g.Expect(dash0Export.Keepalive.PermitWithoutStream).To(BeNil())
					}, 5*time.Second, 100*time.Millisecond,
				).Should(Succeed())
			},
		)

		It(
			"should set the cluster name", func() {
				handler := NewAutoOperatorConfigurationResourceHandler(
					k8sClient,
					readyCheckExecuter,
					OperatorConfigurationValues{
						Endpoint:    EndpointDash0Test,
						Token:       AuthorizationTokenTest,
						ClusterName: "cluster-name",
					},
					util.ExtraConfig{},
				)
				handler.NotifyOperatorManagerJustBecameLeader(ctx, logger)
				_, err := handler.CreateOrUpdateOperatorConfigurationResource(ctx, logger)
				Expect(err).ToNot(HaveOccurred())

				Eventually(
					func(g Gomega) {
						operatorConfiguration := v1alpha1.Dash0OperatorConfiguration{}
						err := k8sClient.Get(
							ctx, types.NamespacedName{
								Name: util.OperatorConfigurationAutoResourceName,
							}, &operatorConfiguration,
						)
						g.Expect(err).ToNot(HaveOccurred())

						g.Expect(operatorConfiguration.Annotations).To(HaveLen(3))
						g.Expect(operatorConfiguration.Annotations["argocd.argoproj.io/sync-options"]).To(Equal("Prune=false"))
						g.Expect(operatorConfiguration.Annotations["argocd.argoproj.io/compare-options"]).To(Equal("IgnoreExtraneous"))

						spec := operatorConfiguration.Spec
						export := spec.Exports[0]
						g.Expect(export).ToNot(BeNil())
						dash0Export := export.Dash0
						g.Expect(dash0Export).ToNot(BeNil())
						g.Expect(spec.ClusterName).To(Equal("cluster-name"))
					}, 5*time.Second, 100*time.Millisecond,
				).Should(Succeed())
			},
		)

		It(
			"should set telemetry-dependent settings to false when telemetryCollectionEnabled is false", func() {
				handler := NewAutoOperatorConfigurationResourceHandler(
					k8sClient,
					readyCheckExecuter,
					OperatorConfigurationValues{
						Endpoint:              EndpointDash0Test,
						Token:                 AuthorizationTokenTest,
						SelfMonitoringEnabled: true,
						KubernetesInfrastructureMetricsCollectionEnabled: true,
						CollectPodLabelsAndAnnotationsEnabled:            true,
						CollectNamespaceLabelsAndAnnotationsEnabled:      true,
						CollectNodeLabelsAndAnnotationsEnabled:           true,
						PrometheusCrdSupportEnabled:                      true,
						ProfilingEnabled:                                 true,
						AutoMonitorNamespacesEnabled:                     true,
						TelemetryCollectionEnabled:                       false,
					},
					util.ExtraConfig{},
				)
				handler.NotifyOperatorManagerJustBecameLeader(ctx, logger)
				_, err := handler.CreateOrUpdateOperatorConfigurationResource(ctx, logger)
				Expect(err).ToNot(HaveOccurred())

				Eventually(
					func(g Gomega) {
						operatorConfiguration := v1alpha1.Dash0OperatorConfiguration{}
						err := k8sClient.Get(
							ctx, types.NamespacedName{
								Name: util.OperatorConfigurationAutoResourceName,
							}, &operatorConfiguration,
						)
						g.Expect(err).ToNot(HaveOccurred())

						spec := operatorConfiguration.Spec
						g.Expect(spec.SelfMonitoring.Enabled).ToNot(BeNil())
						g.Expect(*spec.SelfMonitoring.Enabled).To(BeTrue())
						g.Expect(spec.KubernetesInfrastructureMetricsCollection.Enabled).ToNot(BeNil())
						g.Expect(*spec.KubernetesInfrastructureMetricsCollection.Enabled).To(BeFalse())
						g.Expect(spec.CollectPodLabelsAndAnnotations.Enabled).ToNot(BeNil())
						g.Expect(*spec.CollectPodLabelsAndAnnotations.Enabled).To(BeFalse())
						g.Expect(spec.CollectNamespaceLabelsAndAnnotations.Enabled).ToNot(BeNil())
						g.Expect(*spec.CollectNamespaceLabelsAndAnnotations.Enabled).To(BeFalse())
						g.Expect(spec.CollectNodeLabelsAndAnnotations.Enabled).ToNot(BeNil())
						g.Expect(*spec.CollectNodeLabelsAndAnnotations.Enabled).To(BeFalse())
						g.Expect(spec.PrometheusCrdSupport.Enabled).ToNot(BeNil())
						g.Expect(*spec.PrometheusCrdSupport.Enabled).To(BeFalse())
						g.Expect(spec.Profiling).ToNot(BeNil())
						g.Expect(spec.Profiling.Enabled).ToNot(BeNil())
						g.Expect(*spec.Profiling.Enabled).To(BeFalse())
						g.Expect(spec.AutoMonitorNamespaces.IsEnabled()).To(BeFalse())
						g.Expect(spec.TelemetryCollection.Enabled).ToNot(BeNil())
						g.Expect(*spec.TelemetryCollection.Enabled).To(BeFalse())
					}, 5*time.Second, 100*time.Millisecond,
				).Should(Succeed())
			},
		)

		It(
			"should create a new operator configuration resource with a monitoring template", func() {
				monitoringTemplateJSON := json.RawMessage(`{"spec":{"instrumentWorkloads":{"mode":"none"}}}`)
				handler := NewAutoOperatorConfigurationResourceHandler(
					k8sClient,
					readyCheckExecuter,
					operatorConfigurationValuesWithToken,
					util.ExtraConfig{MonitoringTemplateRaw: &monitoringTemplateJSON},
				)
				handler.NotifyOperatorManagerJustBecameLeader(ctx, logger)
				_, err := handler.CreateOrUpdateOperatorConfigurationResource(ctx, logger)
				Expect(err).ToNot(HaveOccurred())

				Eventually(
					func(g Gomega) {
						operatorConfiguration := v1alpha1.Dash0OperatorConfiguration{}
						err := k8sClient.Get(
							ctx, types.NamespacedName{
								Name: util.OperatorConfigurationAutoResourceName,
							}, &operatorConfiguration,
						)
						g.Expect(err).ToNot(HaveOccurred())

						monitoringTemplate := operatorConfiguration.Spec.MonitoringTemplate
						g.Expect(monitoringTemplate).ToNot(BeNil())
						g.Expect(monitoringTemplate.Spec.InstrumentWorkloads.Mode).To(BeEquivalentTo("none"))
					}, 5*time.Second, 100*time.Millisecond,
				).Should(Succeed())
			},
		)

		It(
			"should fail to create the resource if the monitoring template is invalid JSON", func() {
				invalid := json.RawMessage(`{not valid json}`)
				handler := NewAutoOperatorConfigurationResourceHandler(
					k8sClient,
					readyCheckExecuter,
					operatorConfigurationValuesWithToken,
					util.ExtraConfig{MonitoringTemplateRaw: &invalid},
				)
				handler.NotifyOperatorManagerJustBecameLeader(ctx, logger)
				_, err := handler.CreateOrUpdateOperatorConfigurationResource(ctx, logger)
				Expect(err).To(
					MatchError(
						ContainSubstring("invalid operator configuration: the monitoring template cannot be parsed"),
					),
				)
			},
		)

		It(
			"should update the existing resource when UpdateExtraConfig is called with a new monitoring template", func() {
				// Start without a monitoring template
				handler := NewAutoOperatorConfigurationResourceHandler(
					k8sClient,
					readyCheckExecuter,
					operatorConfigurationValuesWithToken,
					util.ExtraConfig{},
				)
				handler.NotifyOperatorManagerJustBecameLeader(ctx, logger)
				_, err := handler.CreateOrUpdateOperatorConfigurationResource(ctx, logger)
				Expect(err).ToNot(HaveOccurred())

				Eventually(
					func(g Gomega) {
						operatorConfiguration := v1alpha1.Dash0OperatorConfiguration{}
						err := k8sClient.Get(
							ctx, types.NamespacedName{Name: util.OperatorConfigurationAutoResourceName},
							&operatorConfiguration,
						)
						g.Expect(err).ToNot(HaveOccurred())
						g.Expect(operatorConfiguration.Spec.MonitoringTemplate).To(BeNil())
					}, 5*time.Second, 100*time.Millisecond,
				).Should(Succeed())

				// Now call UpdateExtraConfig with a non-nil monitoring template.
				monitoringTemplateJSON := json.RawMessage(`{"spec":{"instrumentWorkloads":{"mode":"none"}}}`)
				handler.UpdateExtraConfig(ctx, util.ExtraConfig{MonitoringTemplateRaw: &monitoringTemplateJSON}, logger)

				Eventually(
					func(g Gomega) {
						operatorConfiguration := v1alpha1.Dash0OperatorConfiguration{}
						err := k8sClient.Get(
							ctx, types.NamespacedName{Name: util.OperatorConfigurationAutoResourceName},
							&operatorConfiguration,
						)
						g.Expect(err).ToNot(HaveOccurred())
						monitoringTemplate := operatorConfiguration.Spec.MonitoringTemplate
						g.Expect(monitoringTemplate).ToNot(BeNil())
						g.Expect(monitoringTemplate.Spec.InstrumentWorkloads.Mode).To(BeEquivalentTo("none"))
					}, 5*time.Second, 100*time.Millisecond,
				).Should(Succeed())
			},
		)

		It(
			"should update the existing resource when UpdateExtraConfig is called removing the monitoring template", func() {
				monitoringTemplateJSON := json.RawMessage(`{"spec":{"instrumentWorkloads":{"mode":"none"}}}`)
				handler := NewAutoOperatorConfigurationResourceHandler(
					k8sClient,
					readyCheckExecuter,
					operatorConfigurationValuesWithToken,
					util.ExtraConfig{MonitoringTemplateRaw: &monitoringTemplateJSON},
				)
				handler.NotifyOperatorManagerJustBecameLeader(ctx, logger)
				_, err := handler.CreateOrUpdateOperatorConfigurationResource(ctx, logger)
				Expect(err).ToNot(HaveOccurred())

				Eventually(
					func(g Gomega) {
						operatorConfiguration := v1alpha1.Dash0OperatorConfiguration{}
						err := k8sClient.Get(
							ctx, types.NamespacedName{Name: util.OperatorConfigurationAutoResourceName},
							&operatorConfiguration,
						)
						g.Expect(err).ToNot(HaveOccurred())
						g.Expect(operatorConfiguration.Spec.MonitoringTemplate).ToNot(BeNil())
					}, 5*time.Second, 100*time.Millisecond,
				).Should(Succeed())

				// Now call UpdateExtraConfig with a nil monitoring template (removing it)
				handler.UpdateExtraConfig(ctx, util.ExtraConfig{MonitoringTemplateRaw: nil}, logger)

				Eventually(
					func(g Gomega) {
						operatorConfiguration := v1alpha1.Dash0OperatorConfiguration{}
						err := k8sClient.Get(
							ctx, types.NamespacedName{Name: util.OperatorConfigurationAutoResourceName},
							&operatorConfiguration,
						)
						g.Expect(err).ToNot(HaveOccurred())
						g.Expect(operatorConfiguration.Spec.MonitoringTemplate).To(BeNil())
					}, 5*time.Second, 100*time.Millisecond,
				).Should(Succeed())
			},
		)

		It(
			"should update the existing resource when UpdateExtraConfig is called with a changed monitoring template", func() {
				monitoringTemplateJSON := json.RawMessage(`{"spec":{"instrumentWorkloads":{"mode":"none"}}}`)
				handler := NewAutoOperatorConfigurationResourceHandler(
					k8sClient,
					readyCheckExecuter,
					operatorConfigurationValuesWithToken,
					util.ExtraConfig{MonitoringTemplateRaw: &monitoringTemplateJSON},
				)
				handler.NotifyOperatorManagerJustBecameLeader(ctx, logger)
				_, err := handler.CreateOrUpdateOperatorConfigurationResource(ctx, logger)
				Expect(err).ToNot(HaveOccurred())

				Eventually(
					func(g Gomega) {
						operatorConfiguration := v1alpha1.Dash0OperatorConfiguration{}
						err := k8sClient.Get(
							ctx, types.NamespacedName{Name: util.OperatorConfigurationAutoResourceName},
							&operatorConfiguration,
						)
						g.Expect(err).ToNot(HaveOccurred())
						g.Expect(operatorConfiguration.Spec.MonitoringTemplate).ToNot(BeNil())
						g.Expect(operatorConfiguration.Spec.MonitoringTemplate.Spec.InstrumentWorkloads.Mode).To(BeEquivalentTo("none"))
					}, 5*time.Second, 100*time.Millisecond,
				).Should(Succeed())

				// Now call UpdateExtraConfig with an updated monitoring template
				updatedMonitoringTemplateJSON := json.RawMessage(`{"spec":{"instrumentWorkloads":{"mode":"all"}}}`)
				handler.UpdateExtraConfig(ctx, util.ExtraConfig{MonitoringTemplateRaw: &updatedMonitoringTemplateJSON}, logger)

				Eventually(
					func(g Gomega) {
						operatorConfiguration := v1alpha1.Dash0OperatorConfiguration{}
						err := k8sClient.Get(
							ctx, types.NamespacedName{Name: util.OperatorConfigurationAutoResourceName},
							&operatorConfiguration,
						)
						g.Expect(err).ToNot(HaveOccurred())
						monitoringTemplate := operatorConfiguration.Spec.MonitoringTemplate
						g.Expect(monitoringTemplate).ToNot(BeNil())
						g.Expect(monitoringTemplate.Spec.InstrumentWorkloads.Mode).To(BeEquivalentTo("all"))
					}, 5*time.Second, 100*time.Millisecond,
				).Should(Succeed())
			},
		)

		It(
			"should not update the resource when UpdateExtraConfig is called with the same monitoring template", func() {
				monitoringTemplateJSON := json.RawMessage(`{"spec":{"instrumentWorkloads":{"mode":"none"}}}`)
				handler := NewAutoOperatorConfigurationResourceHandler(
					k8sClient,
					readyCheckExecuter,
					operatorConfigurationValuesWithToken,
					util.ExtraConfig{MonitoringTemplateRaw: &monitoringTemplateJSON},
				)
				handler.NotifyOperatorManagerJustBecameLeader(ctx, logger)
				_, err := handler.CreateOrUpdateOperatorConfigurationResource(ctx, logger)
				Expect(err).ToNot(HaveOccurred())

				var resourceVersionAfterCreate string
				Eventually(
					func(g Gomega) {
						operatorConfiguration := v1alpha1.Dash0OperatorConfiguration{}
						err := k8sClient.Get(
							ctx, types.NamespacedName{Name: util.OperatorConfigurationAutoResourceName},
							&operatorConfiguration,
						)
						g.Expect(err).ToNot(HaveOccurred())
						resourceVersionAfterCreate = operatorConfiguration.ResourceVersion
					}, 5*time.Second, 100*time.Millisecond,
				).Should(Succeed())

				// Call UpdateExtraConfig with the exact same content - should be a no-op
				sameMonitoringTemplateJSON := json.RawMessage(`{"spec":{"instrumentWorkloads":{"mode":"none"}}}`)
				handler.UpdateExtraConfig(ctx, util.ExtraConfig{MonitoringTemplateRaw: &sameMonitoringTemplateJSON}, logger)

				// The resource version should remain unchanged since no update was triggered
				Consistently(
					func(g Gomega) {
						operatorConfiguration := v1alpha1.Dash0OperatorConfiguration{}
						err := k8sClient.Get(
							ctx, types.NamespacedName{Name: util.OperatorConfigurationAutoResourceName},
							&operatorConfiguration,
						)
						g.Expect(err).ToNot(HaveOccurred())
						g.Expect(operatorConfiguration.ResourceVersion).To(Equal(resourceVersionAfterCreate))
					}, 500*time.Millisecond, 100*time.Millisecond,
				).Should(Succeed())
			},
		)

		It(
			"should not update the resource when UpdateExtraConfig is called with both old and new monitoring templates being nil", func() {
				handler := NewAutoOperatorConfigurationResourceHandler(
					k8sClient,
					readyCheckExecuter,
					operatorConfigurationValuesWithToken,
					util.ExtraConfig{},
				)
				handler.NotifyOperatorManagerJustBecameLeader(ctx, logger)
				_, err := handler.CreateOrUpdateOperatorConfigurationResource(ctx, logger)
				Expect(err).ToNot(HaveOccurred())

				var resourceVersionAfterCreate string
				Eventually(
					func(g Gomega) {
						operatorConfiguration := v1alpha1.Dash0OperatorConfiguration{}
						err := k8sClient.Get(
							ctx, types.NamespacedName{Name: util.OperatorConfigurationAutoResourceName},
							&operatorConfiguration,
						)
						g.Expect(err).ToNot(HaveOccurred())
						resourceVersionAfterCreate = operatorConfiguration.ResourceVersion
					}, 5*time.Second, 100*time.Millisecond,
				).Should(Succeed())

				// Call UpdateExtraConfig with nil monitoring template (both old and new are nil) - should be a no-op
				handler.UpdateExtraConfig(ctx, util.ExtraConfig{MonitoringTemplateRaw: nil}, logger)

				// The resource version should remain unchanged since no update was triggered
				Consistently(
					func(g Gomega) {
						operatorConfiguration := v1alpha1.Dash0OperatorConfiguration{}
						err := k8sClient.Get(
							ctx, types.NamespacedName{Name: util.OperatorConfigurationAutoResourceName},
							&operatorConfiguration,
						)
						g.Expect(err).ToNot(HaveOccurred())
						g.Expect(operatorConfiguration.ResourceVersion).To(Equal(resourceVersionAfterCreate))
					}, 500*time.Millisecond, 100*time.Millisecond,
				).Should(Succeed())
			},
		)

		It(
			"should create a new operator configuration resource with cluster-wide filters and transformations",
			func() {
				filterJSON := json.RawMessage(
					`{"traces":{"span":["attributes[\"http.route\"] == \"/ready\""]}}`)
				transformJSON := json.RawMessage(
					`{"trace_statements":["truncate_all(span.attributes, 128)"]}`)
				handler := NewAutoOperatorConfigurationResourceHandler(
					k8sClient,
					readyCheckExecuter,
					operatorConfigurationValuesWithTokenAndTelemetryCollection,
					util.ExtraConfig{FilterRaw: &filterJSON, TransformRaw: &transformJSON},
				)
				handler.NotifyOperatorManagerJustBecameLeader(ctx, logger)
				_, err := handler.CreateOrUpdateOperatorConfigurationResource(ctx, logger)
				Expect(err).ToNot(HaveOccurred())

				Eventually(
					func(g Gomega) {
						operatorConfiguration := v1alpha1.Dash0OperatorConfiguration{}
						err := k8sClient.Get(
							ctx, types.NamespacedName{Name: util.OperatorConfigurationAutoResourceName},
							&operatorConfiguration,
						)
						g.Expect(err).ToNot(HaveOccurred())
						filter := operatorConfiguration.Spec.Filter
						g.Expect(filter).ToNot(BeNil())
						g.Expect(filter.Traces).ToNot(BeNil())
						g.Expect(filter.Traces.SpanFilter).To(
							ConsistOf(`attributes["http.route"] == "/ready"`))
						transform := operatorConfiguration.Spec.Transform
						g.Expect(transform).ToNot(BeNil())
						g.Expect(transform.Traces).To(HaveLen(1))
						g.Expect(string(transform.Traces[0])).To(
							Equal(`"truncate_all(span.attributes, 128)"`))
					}, 5*time.Second, 100*time.Millisecond,
				).Should(Succeed())
			},
		)

		It(
			"should drop cluster-wide filters and transformations if telemetry collection is disabled", func() {
				filterJSON := json.RawMessage(
					`{"traces":{"span":["attributes[\"http.route\"] == \"/ready\""]}}`)
				transformJSON := json.RawMessage(
					`{"trace_statements":["truncate_all(span.attributes, 128)"]}`)
				handler := NewAutoOperatorConfigurationResourceHandler(
					k8sClient,
					readyCheckExecuter,
					operatorConfigurationValuesWithToken,
					util.ExtraConfig{FilterRaw: &filterJSON, TransformRaw: &transformJSON},
				)
				handler.NotifyOperatorManagerJustBecameLeader(ctx, logger)
				_, err := handler.CreateOrUpdateOperatorConfigurationResource(ctx, logger)
				Expect(err).ToNot(HaveOccurred())

				Eventually(
					func(g Gomega) {
						operatorConfiguration := v1alpha1.Dash0OperatorConfiguration{}
						err := k8sClient.Get(
							ctx, types.NamespacedName{Name: util.OperatorConfigurationAutoResourceName},
							&operatorConfiguration,
						)
						g.Expect(err).ToNot(HaveOccurred())
						g.Expect(*operatorConfiguration.Spec.TelemetryCollection.Enabled).To(BeFalse())
						g.Expect(operatorConfiguration.Spec.Filter).To(BeNil())
						g.Expect(operatorConfiguration.Spec.Transform).To(BeNil())
					}, 5*time.Second, 100*time.Millisecond,
				).Should(Succeed())
			},
		)

		It(
			"should fail to create the resource if the filter is invalid JSON", func() {
				invalid := json.RawMessage(`{not valid json}`)
				handler := NewAutoOperatorConfigurationResourceHandler(
					k8sClient,
					readyCheckExecuter,
					operatorConfigurationValuesWithTokenAndTelemetryCollection,
					util.ExtraConfig{FilterRaw: &invalid},
				)
				handler.NotifyOperatorManagerJustBecameLeader(ctx, logger)
				_, err := handler.CreateOrUpdateOperatorConfigurationResource(ctx, logger)
				Expect(err).To(
					MatchError(
						ContainSubstring("invalid operator configuration: the filter cannot be parsed"),
					),
				)
			},
		)

		It(
			"should update the existing resource when UpdateExtraConfig is called with a changed filter", func() {
				filterJSON := json.RawMessage(
					`{"traces":{"span":["attributes[\"http.route\"] == \"/ready\""]}}`)
				handler := NewAutoOperatorConfigurationResourceHandler(
					k8sClient,
					readyCheckExecuter,
					operatorConfigurationValuesWithTokenAndTelemetryCollection,
					util.ExtraConfig{FilterRaw: &filterJSON},
				)
				handler.NotifyOperatorManagerJustBecameLeader(ctx, logger)
				_, err := handler.CreateOrUpdateOperatorConfigurationResource(ctx, logger)
				Expect(err).ToNot(HaveOccurred())

				Eventually(
					func(g Gomega) {
						operatorConfiguration := v1alpha1.Dash0OperatorConfiguration{}
						err := k8sClient.Get(
							ctx, types.NamespacedName{Name: util.OperatorConfigurationAutoResourceName},
							&operatorConfiguration,
						)
						g.Expect(err).ToNot(HaveOccurred())
						g.Expect(operatorConfiguration.Spec.Filter).ToNot(BeNil())
					}, 5*time.Second, 100*time.Millisecond,
				).Should(Succeed())

				updatedFilterJSON := json.RawMessage(
					`{"traces":{"span":["attributes[\"http.route\"] == \"/metrics\""]}}`)
				handler.UpdateExtraConfig(ctx, util.ExtraConfig{FilterRaw: &updatedFilterJSON}, logger)

				Eventually(
					func(g Gomega) {
						operatorConfiguration := v1alpha1.Dash0OperatorConfiguration{}
						err := k8sClient.Get(
							ctx, types.NamespacedName{Name: util.OperatorConfigurationAutoResourceName},
							&operatorConfiguration,
						)
						g.Expect(err).ToNot(HaveOccurred())
						filter := operatorConfiguration.Spec.Filter
						g.Expect(filter).ToNot(BeNil())
						g.Expect(filter.Traces).ToNot(BeNil())
						g.Expect(filter.Traces.SpanFilter).To(
							ConsistOf(`attributes["http.route"] == "/metrics"`))
					}, 5*time.Second, 100*time.Millisecond,
				).Should(Succeed())
			},
		)

		It(
			"should update the existing resource if there already is an auto-operator-configuration-resource", func() {
				handler1 := NewAutoOperatorConfigurationResourceHandler(
					k8sClient,
					readyCheckExecuter,
					OperatorConfigurationValues{
						Endpoint:              "endpoint-1.dash0.com:4317",
						Token:                 AuthorizationTokenTest,
						ApiEndpoint:           "https://api-1.dash0.com",
						Dataset:               "dataset-1",
						SelfMonitoringEnabled: false,
						KubernetesInfrastructureMetricsCollectionEnabled: true,
						CollectPodLabelsAndAnnotationsEnabled:            true,
						PrometheusCrdSupportEnabled:                      false,
						ProfilingEnabled:                                 false,
						TelemetryCollectionEnabled:                       true,
					},
					util.ExtraConfig{},
				)
				handler1.NotifyOperatorManagerJustBecameLeader(ctx, logger)
				_, err := handler1.CreateOrUpdateOperatorConfigurationResource(ctx, logger)
				Expect(err).ToNot(HaveOccurred())

				Eventually(
					func(g Gomega) {
						list := v1alpha1.Dash0OperatorConfigurationList{}
						g.Expect(k8sClient.List(ctx, &list)).To(Succeed())
						g.Expect(list.Items).To(HaveLen(1))
						operatorConfiguration := list.Items[0]
						g.Expect(operatorConfiguration.Name).To(Equal(util.OperatorConfigurationAutoResourceName))
						g.Expect(operatorConfiguration.Annotations).To(HaveLen(3))
						g.Expect(operatorConfiguration.Annotations["argocd.argoproj.io/sync-options"]).To(Equal("Prune=false"))
						g.Expect(operatorConfiguration.Annotations["argocd.argoproj.io/compare-options"]).To(Equal("IgnoreExtraneous"))

						export := operatorConfiguration.Spec.Exports[0]
						g.Expect(export).ToNot(BeNil())
						dash0Export := export.Dash0
						g.Expect(dash0Export).ToNot(BeNil())
						g.Expect(export.Grpc).To(BeNil())
						g.Expect(export.Http).To(BeNil())
						g.Expect(dash0Export.Endpoint).To(Equal("endpoint-1.dash0.com:4317"))
						g.Expect(dash0Export.Authorization.Token).ToNot(BeNil())
						g.Expect(*dash0Export.Authorization.Token).To(Equal(AuthorizationTokenTest))
						g.Expect(dash0Export.Authorization.SecretRef).To(BeNil())
						g.Expect(dash0Export.ApiEndpoint).To(Equal("https://api-1.dash0.com"))
						g.Expect(dash0Export.Dataset).To(Equal("dataset-1"))
						g.Expect(*operatorConfiguration.Spec.SelfMonitoring.Enabled).To(BeFalse())
						g.Expect(*operatorConfiguration.Spec.KubernetesInfrastructureMetricsCollection.Enabled).To(BeTrue())
						g.Expect(*operatorConfiguration.Spec.CollectPodLabelsAndAnnotations.Enabled).To(BeTrue())
						g.Expect(*operatorConfiguration.Spec.PrometheusCrdSupport.Enabled).To(BeFalse())
						g.Expect(operatorConfiguration.Spec.Profiling).ToNot(BeNil())
						g.Expect(*operatorConfiguration.Spec.Profiling.Enabled).To(BeFalse())
					}, 5*time.Second, 100*time.Millisecond,
				).Should(Succeed())

				// Now simulate a new startup of the operator manager process with different operator-configuration-xxx flags
				handler2 := NewAutoOperatorConfigurationResourceHandler(
					k8sClient,
					readyCheckExecuter,
					OperatorConfigurationValues{
						Endpoint:              "endpoint-2.dash0.com:4317",
						SecretRef:             secretRef,
						ApiEndpoint:           "https://api-2.dash0.com",
						Dataset:               "dataset-2",
						SelfMonitoringEnabled: true,
						KubernetesInfrastructureMetricsCollectionEnabled: false,
						CollectPodLabelsAndAnnotationsEnabled:            false,
						PrometheusCrdSupportEnabled:                      true,
						ProfilingEnabled:                                 true,
						TelemetryCollectionEnabled:                       true,
					},
					util.ExtraConfig{},
				)
				handler2.NotifyOperatorManagerJustBecameLeader(ctx, logger)
				_, err = handler2.CreateOrUpdateOperatorConfigurationResource(ctx, logger)
				Expect(err).ToNot(HaveOccurred())

				// verify that there is _still_ only one resource, and that its settings have been updated.
				Eventually(
					func(g Gomega) {
						list := v1alpha1.Dash0OperatorConfigurationList{}
						g.Expect(k8sClient.List(ctx, &list)).To(Succeed())
						g.Expect(list.Items).To(HaveLen(1))
						operatorConfiguration := list.Items[0]
						g.Expect(operatorConfiguration.Name).To(Equal(util.OperatorConfigurationAutoResourceName))
						g.Expect(operatorConfiguration.Annotations).To(HaveLen(3))
						g.Expect(operatorConfiguration.Annotations["argocd.argoproj.io/sync-options"]).To(Equal("Prune=false"))
						g.Expect(operatorConfiguration.Annotations["argocd.argoproj.io/compare-options"]).To(Equal("IgnoreExtraneous"))
						export := operatorConfiguration.Spec.Exports[0]
						g.Expect(export).ToNot(BeNil())
						dash0Export := export.Dash0
						g.Expect(dash0Export).ToNot(BeNil())
						g.Expect(export.Grpc).To(BeNil())
						g.Expect(export.Http).To(BeNil())
						g.Expect(dash0Export.Endpoint).To(Equal("endpoint-2.dash0.com:4317"))
						g.Expect(dash0Export.Authorization.Token).To(BeNil())
						g.Expect(dash0Export.Authorization.SecretRef).ToNot(BeNil())
						g.Expect(dash0Export.Authorization.SecretRef.Name).To(Equal("test-secret"))
						g.Expect(dash0Export.Authorization.SecretRef.Key).To(Equal("test-key"))
						g.Expect(dash0Export.ApiEndpoint).To(Equal("https://api-2.dash0.com"))
						g.Expect(dash0Export.Dataset).To(Equal("dataset-2"))
						g.Expect(*operatorConfiguration.Spec.SelfMonitoring.Enabled).To(BeTrue())
						g.Expect(*operatorConfiguration.Spec.KubernetesInfrastructureMetricsCollection.Enabled).To(BeFalse())
						g.Expect(*operatorConfiguration.Spec.CollectPodLabelsAndAnnotations.Enabled).To(BeFalse())
						g.Expect(*operatorConfiguration.Spec.PrometheusCrdSupport.Enabled).To(BeTrue())
						g.Expect(operatorConfiguration.Spec.Profiling).ToNot(BeNil())
						g.Expect(*operatorConfiguration.Spec.Profiling.Enabled).To(BeTrue())
					}, 5*time.Second, 100*time.Millisecond,
				).Should(Succeed())
			},
		)

		It(
			"should create a new operator configuration resource with exports only", func() {
				handler := NewAutoOperatorConfigurationResourceHandler(
					k8sClient,
					readyCheckExecuter,
					OperatorConfigurationValues{},
					util.ExtraConfig{Exports: []dash0common.Export{
						{
							Grpc: &dash0common.GrpcConfiguration{
								Endpoint: "otel-collector.other-namespace.svc.cluster.local:4317",
								Insecure: ptr.To(true),
								Headers: []dash0common.Header{{
									Name:  "x-tenant",
									Value: "tenant-1",
								}},
							},
						},
					}},
				)
				handler.NotifyOperatorManagerJustBecameLeader(ctx, logger)
				_, err := handler.CreateOrUpdateOperatorConfigurationResource(ctx, logger)
				Expect(err).ToNot(HaveOccurred())

				Eventually(
					func(g Gomega) {
						operatorConfiguration := v1alpha1.Dash0OperatorConfiguration{}
						g.Expect(
							k8sClient.Get(
								ctx, types.NamespacedName{Name: util.OperatorConfigurationAutoResourceName},
								&operatorConfiguration,
							),
						).To(Succeed())

						exports := operatorConfiguration.Spec.Exports
						g.Expect(exports).To(HaveLen(1))
						g.Expect(exports[0].Dash0).To(BeNil())
						g.Expect(exports[0].Http).To(BeNil())
						grpcExport := exports[0].Grpc
						g.Expect(grpcExport).ToNot(BeNil())
						g.Expect(grpcExport.Endpoint).To(Equal("otel-collector.other-namespace.svc.cluster.local:4317"))
						g.Expect(*grpcExport.Insecure).To(BeTrue())
						g.Expect(grpcExport.Headers).To(HaveLen(1))
						g.Expect(grpcExport.Headers[0].Name).To(Equal("x-tenant"))
						g.Expect(grpcExport.Headers[0].Value).To(Equal("tenant-1"))
					}, 5*time.Second, 100*time.Millisecond,
				).Should(Succeed())
			},
		)

		It(
			"should put the Dash0 export first and default the encoding of an http export", func() {
				handler := NewAutoOperatorConfigurationResourceHandler(
					k8sClient,
					readyCheckExecuter,
					operatorConfigurationValuesWithToken,
					util.ExtraConfig{Exports: []dash0common.Export{
						{Http: &dash0common.HttpConfiguration{Endpoint: "https://otlp.example.com"}},
						{Grpc: &dash0common.GrpcConfiguration{Endpoint: "otelcol:4317"}},
					}},
				)
				handler.NotifyOperatorManagerJustBecameLeader(ctx, logger)
				_, err := handler.CreateOrUpdateOperatorConfigurationResource(ctx, logger)
				Expect(err).ToNot(HaveOccurred())

				Eventually(
					func(g Gomega) {
						operatorConfiguration := v1alpha1.Dash0OperatorConfiguration{}
						g.Expect(
							k8sClient.Get(
								ctx, types.NamespacedName{Name: util.OperatorConfigurationAutoResourceName},
								&operatorConfiguration,
							),
						).To(Succeed())

						exports := operatorConfiguration.Spec.Exports
						g.Expect(exports).To(HaveLen(3))
						g.Expect(exports[0].Dash0).ToNot(BeNil())
						g.Expect(exports[0].Dash0.Endpoint).To(Equal(EndpointDash0Test))
						g.Expect(exports[1].Http).ToNot(BeNil())
						g.Expect(exports[1].Http.Encoding).To(Equal(dash0common.Proto))
						g.Expect(exports[2].Grpc).ToNot(BeNil())
					}, 5*time.Second, 100*time.Millisecond,
				).Should(Succeed())
			},
		)

		DescribeTable(
			"should fail validation for an invalid export",
			func(export dash0common.Export, expectedErrorMessage string) {
				handler := NewAutoOperatorConfigurationResourceHandler(
					k8sClient,
					readyCheckExecuter,
					OperatorConfigurationValues{},
					util.ExtraConfig{Exports: []dash0common.Export{export}},
				)
				handler.NotifyOperatorManagerJustBecameLeader(ctx, logger)
				_, err := handler.CreateOrUpdateOperatorConfigurationResource(ctx, logger)
				Expect(err).To(MatchError(ContainSubstring(expectedErrorMessage)))
			},
			Entry(
				"no exporter at all",
				dash0common.Export{},
				"operator.exports[0] has none of dash0, grpc or http set",
			),
			Entry(
				"dash0 without endpoint",
				dash0common.Export{Dash0: &dash0common.Dash0Configuration{}},
				"operator.exports[0].dash0 has no endpoint",
			),
			Entry(
				"grpc without endpoint",
				dash0common.Export{Grpc: &dash0common.GrpcConfiguration{}},
				"operator.exports[0].grpc has no endpoint",
			),
			Entry(
				"http without endpoint",
				dash0common.Export{Http: &dash0common.HttpConfiguration{}},
				"operator.exports[0].http has no endpoint",
			),
		)

		It(
			"should update the existing resource when UpdateExtraConfig is called with new exports", func() {
				handler := NewAutoOperatorConfigurationResourceHandler(
					k8sClient,
					readyCheckExecuter,
					operatorConfigurationValuesWithToken,
					util.ExtraConfig{},
				)
				handler.NotifyOperatorManagerJustBecameLeader(ctx, logger)
				_, err := handler.CreateOrUpdateOperatorConfigurationResource(ctx, logger)
				Expect(err).ToNot(HaveOccurred())

				Eventually(
					func(g Gomega) {
						operatorConfiguration := v1alpha1.Dash0OperatorConfiguration{}
						g.Expect(
							k8sClient.Get(
								ctx, types.NamespacedName{Name: util.OperatorConfigurationAutoResourceName},
								&operatorConfiguration,
							),
						).To(Succeed())
						g.Expect(operatorConfiguration.Spec.Exports).To(HaveLen(1))
					}, 5*time.Second, 100*time.Millisecond,
				).Should(Succeed())

				handler.UpdateExtraConfig(
					ctx,
					util.ExtraConfig{
						Exports: []dash0common.Export{
							{Grpc: &dash0common.GrpcConfiguration{Endpoint: "otelcol:4317"}},
						},
					},
					logger,
				)

				Eventually(
					func(g Gomega) {
						operatorConfiguration := v1alpha1.Dash0OperatorConfiguration{}
						g.Expect(
							k8sClient.Get(
								ctx, types.NamespacedName{Name: util.OperatorConfigurationAutoResourceName},
								&operatorConfiguration,
							),
						).To(Succeed())
						exports := operatorConfiguration.Spec.Exports
						g.Expect(exports).To(HaveLen(2))
						g.Expect(exports[0].Dash0).ToNot(BeNil())
						g.Expect(exports[1].Grpc).ToNot(BeNil())
						g.Expect(exports[1].Grpc.Endpoint).To(Equal("otelcol:4317"))
					}, 5*time.Second, 100*time.Millisecond,
				).Should(Succeed())
			},
		)
	},
)
