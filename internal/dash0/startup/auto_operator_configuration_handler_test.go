// SPDX-FileCopyrightText: Copyright 2024 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package startup

import (
	"context"
	"time"

	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/log"

	"github.com/dash0hq/dash0-operator/api/dash0monitoring/v1alpha1"

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
	operatorConfigurationValuesWithSecretRef = OperatorConfigurationValues{
		Endpoint:  EndpointDash0Test,
		SecretRef: secretRef,
	}
)

var _ = Describe("Create an operator configuration resource at startup", Ordered, func() {

	ctx := context.Background()
	logger := log.FromContext(ctx)

	BeforeAll(func() {
		EnsureDash0OperatorNamespaceExists(ctx, k8sClient)
	})

	AfterEach(func() {
		DeleteAllOperatorConfigurationResources(ctx, k8sClient)
	})

	It("should not do anything if there already is an operator configuration resource in the cluster", func() {
		CreateDefaultOperatorConfigurationResource(ctx, k8sClient)
		// verify that there is only one resource
		list := v1alpha1.Dash0OperatorConfigurationList{}
		Expect(k8sClient.List(ctx, &list)).To(Succeed())
		Expect(list.Items).To(HaveLen(1))
		Expect(list.Items[0].Name).To(Equal(OperatorConfigurationResourceName))

		Expect(handler.CreateOperatorConfigurationResource(ctx, &OperatorConfigurationValues{}, &logger)).To(Succeed())
		// verify that there is _still_ only one resource, and that its name is not the one that would be automatically
		// created by AutoOperatorConfigurationResourceHandler.
		Expect(k8sClient.List(ctx, &list)).To(Succeed())
		Expect(list.Items).To(HaveLen(1))
		Expect(list.Items[0].Name).To(Equal(OperatorConfigurationResourceName))
	})

	It("should fail validation if no endpoint has been provided", func() {
		Expect(handler.CreateOperatorConfigurationResource(ctx, &OperatorConfigurationValues{
			Token: AuthorizationTokenTest,
		}, &logger)).To(
			MatchError(
				ContainSubstring(
					"invalid operator configuration: --operator-configuration-endpoint has not been provided")))
	})

	It("should fail validation if no token and no secret reference have been provided", func() {
		Expect(handler.CreateOperatorConfigurationResource(ctx, &OperatorConfigurationValues{
			Endpoint: AuthorizationTokenTest,
		}, &logger)).To(
			MatchError(
				ContainSubstring(
					"neither --operator-configuration-token nor --operator-configuration-secret-ref-name have " +
						"been provided")))
	})

	It("should fail validation if no token and no secret reference key have been provided", func() {
		Expect(handler.CreateOperatorConfigurationResource(ctx, &OperatorConfigurationValues{
			Endpoint: AuthorizationTokenTest,
			SecretRef: SecretRef{
				Name: "test-secret",
			},
		}, &logger)).To(
			MatchError(
				ContainSubstring(
					"neither --operator-configuration-token nor --operator-configuration-secret-ref-key have " +
						"been provided")))
	})

	It("should create an operator configuration resource with a token", func() {
		Expect(
			handler.CreateOperatorConfigurationResource(ctx, &operatorConfigurationValuesWithToken, &logger),
		).To(Succeed())

		Eventually(func(g Gomega) {
			operatorConfiguration := v1alpha1.Dash0OperatorConfiguration{}
			err := k8sClient.Get(ctx, types.NamespacedName{
				Name: operatorConfigurationAutoResourceName,
			}, &operatorConfiguration)

			g.Expect(err).ToNot(HaveOccurred())
			export := operatorConfiguration.Spec.Export
			g.Expect(export).ToNot(BeNil())
			dash0Export := export.Dash0
			g.Expect(dash0Export).ToNot(BeNil())
			g.Expect(export.Grpc).To(BeNil())
			g.Expect(export.Http).To(BeNil())
			g.Expect(dash0Export.Endpoint).To(Equal(EndpointDash0Test))
			g.Expect(dash0Export.Authorization.Token).ToNot(BeNil())
			g.Expect(*dash0Export.Authorization.Token).To(Equal(AuthorizationTokenTest))
			g.Expect(dash0Export.Authorization.SecretRef).To(BeNil())
		}, 5*time.Second, 100*time.Millisecond).Should(Succeed())
	})

	It("should create an operator configuration resource with a secret reference", func() {
		Expect(
			handler.CreateOperatorConfigurationResource(ctx, &operatorConfigurationValuesWithSecretRef, &logger),
		).To(Succeed())

		Eventually(func(g Gomega) {
			operatorConfiguration := v1alpha1.Dash0OperatorConfiguration{}
			err := k8sClient.Get(ctx, types.NamespacedName{
				Name: operatorConfigurationAutoResourceName,
			}, &operatorConfiguration)

			g.Expect(err).ToNot(HaveOccurred())
			export := operatorConfiguration.Spec.Export
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
		}, 5*time.Second, 100*time.Millisecond).Should(Succeed())
	})
})
