// SPDX-FileCopyrightText: Copyright 2024 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package startup

import (
	"context"
	"time"

	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/log"

	"github.com/dash0hq/dash0-operator/api/operator/v1alpha1"
	"github.com/dash0hq/dash0-operator/internal/util"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	. "github.com/dash0hq/dash0-operator/test/util"
)

var (
	autoOperatorConfigurationResourceHandler *AutoOperatorConfigurationResourceHandler

	secretRef = SecretRef{
		Name: "test-secret",
		Key:  "test-key",
	}
	operatorConfigurationValuesWithToken = OperatorConfigurationValues{
		Endpoint: EndpointDash0Test,
		Token:    AuthorizationTokenTest,
	}
	operatorConfigurationValuesWithSecretRef = OperatorConfigurationValues{
		Endpoint:  EndpointDash0Test,
		SecretRef: secretRef,
	}
)

var _ = Describe(
	"Create an operator configuration resource at startup", Ordered, func() {

		ctx := context.Background()
		logger := log.FromContext(ctx)
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
				autoOperatorConfigurationResourceHandler =
					NewAutoOperatorConfigurationResourceHandler(k8sClient, readyCheckExecuter)
			},
		)

		AfterEach(
			func() {
				DeleteAllOperatorConfigurationResources(ctx, k8sClient)
			},
		)

		It(
			"should fail validation if no endpoint has been provided", func() {
				autoOperatorConfigurationResourceHandler.NotifiyOperatorManagerJustBecameLeader(ctx, &logger)
				_, err := autoOperatorConfigurationResourceHandler.CreateOrUpdateOperatorConfigurationResource(
					ctx, &OperatorConfigurationValues{
						Token: AuthorizationTokenTest,
					}, &logger,
				)
				Expect(err).To(
					MatchError(
						ContainSubstring(
							"invalid operator configuration: --operator-configuration-endpoint has not been provided",
						),
					),
				)
			},
		)

		It(
			"should fail validation if no token and no secret reference have been provided", func() {
				autoOperatorConfigurationResourceHandler.NotifiyOperatorManagerJustBecameLeader(ctx, &logger)
				_, err := autoOperatorConfigurationResourceHandler.CreateOrUpdateOperatorConfigurationResource(
					ctx, &OperatorConfigurationValues{
						Endpoint: AuthorizationTokenTest,
					}, &logger,
				)
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
				_, err := autoOperatorConfigurationResourceHandler.CreateOrUpdateOperatorConfigurationResource(
					ctx, &OperatorConfigurationValues{
						Endpoint: AuthorizationTokenTest,
						SecretRef: SecretRef{
							Name: "test-secret",
						},
					}, &logger,
				)
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
				autoOperatorConfigurationResourceHandler.NotifiyOperatorManagerJustBecameLeader(ctx, &logger)
				_, err := autoOperatorConfigurationResourceHandler.CreateOrUpdateOperatorConfigurationResource(
					ctx,
					&operatorConfigurationValuesWithToken,
					&logger,
				)
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
									"chart (Helm values operator.dash0Export.*), manual modifications to this resource (i.e. via " +
									"kubectl or k9s) will be overwritten when the operator manager is restarted or the operator is " +
									"updated to a new version. See https://github.com/dash0hq/dash0-operator/blob/main/helm-chart/" +
									"dash0-operator/README.md#notes-on-creating-the-operator-configuration-resource-via-helm.",
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
				autoOperatorConfigurationResourceHandler.NotifiyOperatorManagerJustBecameLeader(ctx, &logger)
				_, err := autoOperatorConfigurationResourceHandler.CreateOrUpdateOperatorConfigurationResource(
					ctx,
					&operatorConfigurationValuesWithSecretRef,
					&logger,
				)
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
				_, err :=
					autoOperatorConfigurationResourceHandler.CreateOrUpdateOperatorConfigurationResource(
						ctx,
						&operatorConfigurationValuesWithToken,
						&logger,
					)
				Expect(err).ToNot(HaveOccurred())

				// The autoOperatorConfigurationResourceHandler should not be able to proceed if we do notify the
				// autoOperatorConfigurationResourceHandler about getting elected as leader.
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

				// now make this replica the leader, which should allow the autoOperatorConfigurationResourceHandler to proceed
				autoOperatorConfigurationResourceHandler.NotifiyOperatorManagerJustBecameLeader(ctx, &logger)

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
				autoOperatorConfigurationResourceHandler.NotifiyOperatorManagerJustBecameLeader(ctx, &logger)
				readyCheckExecuter.bypassWebhookCheck = false
				_, err :=
					autoOperatorConfigurationResourceHandler.CreateOrUpdateOperatorConfigurationResource(
						ctx,
						&operatorConfigurationValuesWithToken,
						&logger,
					)
				Expect(err).ToNot(HaveOccurred())

				// The autoOperatorConfigurationResourceHandler should not be able to proceed if we do not skip the webhook
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
				autoOperatorConfigurationResourceHandler.NotifiyOperatorManagerJustBecameLeader(ctx, &logger)
				_, err := autoOperatorConfigurationResourceHandler.CreateOrUpdateOperatorConfigurationResource(
					ctx, &OperatorConfigurationValues{
						Endpoint:    EndpointDash0Test,
						Token:       AuthorizationTokenTest,
						ApiEndpoint: ApiEndpointTest,
					}, &logger,
				)
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
				autoOperatorConfigurationResourceHandler.NotifiyOperatorManagerJustBecameLeader(ctx, &logger)
				_, err := autoOperatorConfigurationResourceHandler.CreateOrUpdateOperatorConfigurationResource(
					ctx, &OperatorConfigurationValues{
						Endpoint: EndpointDash0Test,
						Token:    AuthorizationTokenTest,
						Dataset:  "custom",
					}, &logger,
				)
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
			"should set the cluster name", func() {
				autoOperatorConfigurationResourceHandler.NotifiyOperatorManagerJustBecameLeader(ctx, &logger)
				_, err := autoOperatorConfigurationResourceHandler.CreateOrUpdateOperatorConfigurationResource(
					ctx, &OperatorConfigurationValues{
						Endpoint:    EndpointDash0Test,
						Token:       AuthorizationTokenTest,
						ClusterName: "cluster-name",
					}, &logger,
				)
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
			"should update the existing resource if there already is an auto-operator-configuration-resource", func() {
				autoOperatorConfigurationResourceHandler.NotifiyOperatorManagerJustBecameLeader(ctx, &logger)
				_, err := autoOperatorConfigurationResourceHandler.CreateOrUpdateOperatorConfigurationResource(
					ctx, &OperatorConfigurationValues{
						Endpoint:              "endpoint-1.dash0.com:4317",
						Token:                 AuthorizationTokenTest,
						ApiEndpoint:           "https://api-1.dash0.com",
						Dataset:               "dataset-1",
						SelfMonitoringEnabled: false,
						KubernetesInfrastructureMetricsCollectionEnabled: true,
						CollectPodLabelsAndAnnotationsEnabled:            true,
						PrometheusCrdSupportEnabled:                      false,
					}, &logger,
				)
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
					}, 5*time.Second, 100*time.Millisecond,
				).Should(Succeed())

				// Now call the handler a second time, simulating a new startup of the operator manager process, with different
				// operator-configuration-xxx flags
				_, err = autoOperatorConfigurationResourceHandler.CreateOrUpdateOperatorConfigurationResource(
					ctx,
					&OperatorConfigurationValues{
						Endpoint:              "endpoint-2.dash0.com:4317",
						SecretRef:             secretRef,
						ApiEndpoint:           "https://api-2.dash0.com",
						Dataset:               "dataset-2",
						SelfMonitoringEnabled: true,
						KubernetesInfrastructureMetricsCollectionEnabled: false,
						CollectPodLabelsAndAnnotationsEnabled:            false,
						PrometheusCrdSupportEnabled:                      true,
					}, &logger,
				)
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
					}, 5*time.Second, 100*time.Millisecond,
				).Should(Succeed())
			},
		)
	},
)
