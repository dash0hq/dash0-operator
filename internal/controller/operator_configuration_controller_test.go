// SPDX-FileCopyrightText: Copyright 2024 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package controller

import (
	"context"
	"fmt"
	"reflect"

	"github.com/go-logr/logr"
	json "github.com/json-iterator/go"
	"github.com/wI2L/jsondiff"
	appsv1 "k8s.io/api/apps/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	dash0v1alpha1 "github.com/dash0hq/dash0-operator/api/dash0monitoring/v1alpha1"
	"github.com/dash0hq/dash0-operator/internal/selfmonitoringapiaccess"
	"github.com/dash0hq/dash0-operator/internal/util"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	. "github.com/dash0hq/dash0-operator/test/util"
)

type SelfMonitoringAndApiAccessTestConfig struct {
	existingControllerDeployment               func() *appsv1.Deployment
	operatorConfigurationResourceSpec          dash0v1alpha1.Dash0OperatorConfigurationSpec
	expectedControllerDeploymentAfterReconcile func() *appsv1.Deployment
	expectK8sClientUpdate                      bool
}

type ApiClientSetRemoveTestConfig struct {
	operatorConfigurationResourceSpec dash0v1alpha1.Dash0OperatorConfigurationSpec
	dataset                           string
	expectSetApiEndpointAndDataset    bool
	expectRemoveApiEndpointAndDataset bool
}

type SelfMonitoringTestConfig struct {
	createExport func() dash0v1alpha1.Export
	verify       func(Gomega, selfmonitoringapiaccess.SelfMonitoringAndApiAccessConfiguration, *appsv1.Deployment)
}

var (
	reconciler *OperatorConfigurationReconciler
	apiClient1 *DummyApiClient
	apiClient2 *DummyApiClient
)

var _ = Describe("The operation configuration resource controller", Ordered, func() {
	ctx := context.Background()
	var controllerDeployment *appsv1.Deployment

	BeforeAll(func() {
		EnsureTestNamespaceExists(ctx, k8sClient)
		EnsureOperatorNamespaceExists(ctx, k8sClient)
	})

	BeforeEach(func() {
		apiClient1 = &DummyApiClient{}
		apiClient2 = &DummyApiClient{}
	})

	Describe("updates the controller deployment", func() {
		AfterEach(func() {
			RemoveOperatorConfigurationResource(ctx, k8sClient)
			EnsureControllerDeploymentDoesNotExist(ctx, k8sClient, controllerDeployment)
		})

		DescribeTable("to reflect self-monitoring and API access auth settings:", func(config SelfMonitoringAndApiAccessTestConfig) {
			controllerDeployment = config.existingControllerDeployment()
			EnsureControllerDeploymentExists(ctx, k8sClient, controllerDeployment)
			reconciler = createReconciler(controllerDeployment)

			initialVersion := controllerDeployment.ResourceVersion

			CreateOperatorConfigurationResourceWithSpec(
				ctx,
				k8sClient,
				config.operatorConfigurationResourceSpec,
			)

			triggerOperatorConfigurationReconcileRequest(ctx, reconciler)
			verifyOperatorConfigurationResourceIsAvailable(ctx)

			expectedDeploymentAfterReconcile := config.expectedControllerDeploymentAfterReconcile()
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
				actualDeploymentAfterReconcile := LoadOperatorDeploymentOrFail(ctx, k8sClient, g)

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

			// | previous deployment state             | operator config res | expected deployment afterwards   |
			// |---------------------------------------|---------------------|----------------------------------|
			// | no self-monitoring, no authorization  | no sm, no auth      | no sm, no auth                   |
			// | no self-monitoring, no authorization  | no sm, token        | no sm, auth via token            |
			// | no self-monitoring, no authorization  | no sm, secret-ref   | no sm, auth via secret-ref       |
			// | no self-monitoring, no authorization  | with sm, token      | has sm, auth via token           |
			// | no self-monitoring, no authorization  | with sm, secret-ref | has sm, auth via secret-ref      |
			// | --------------------------------------|---------------------|----------------------------------|
			// | no self-monitoring, but token         | no sm, no auth      | no sm, no auth	                |
			// | no self-monitoring, but secret-ref    | no sm, no auth      | no sm, no auth                   |
			// | no self-monitoring, but token         | no sm, token        | no sm, auth via token            |
			// | no self-monitoring, but token         | no sm, secret-ref   | no sm, auth via secret-ref       |
			// | no self-monitoring, but secret-ref    | no sm, token        | no sm, auth via token            |
			// | no self-monitoring, but secret-ref    | no sm, secret-ref   | no sm, auth via secret-ref       |
			// | no self-monitoring, but token         | with sm, token      | has sm, auth via token           |
			// | no self-monitoring, but token         | with sm, secret-ref | has sm, auth via secret-ref      |
			// | no self-monitoring, but secret-ref    | with sm, token      | has sm, auth via token           |
			// | no self-monitoring, but secret-ref    | with sm, secret-ref | has sm, auth via secret-ref      |
			// | --------------------------------------|---------------------|----------------------------------|
			// | self-monitoring with token            | no sm, no auth      | no sm, no auth                   |
			// | self-monitoring with secret-ref       | no sm, no auth      | no sm, no auth                   |
			// | self-monitoring with token            | no sm, token        | no sm, auth via token            |
			// | self-monitoring with token            | no sm, secret-ref   | no sm, auth via secret-ref       |
			// | self-monitoring with secret-ref       | no sm, token        | no sm, auth via token            |
			// | self-monitoring with secret-ref       | no sm, secret-ref   | no sm, auth via secret-ref       |
			// | self-monitoring with token            | with sm, token      | has sm, auth va token            |
			// | self-monitoring with token            | with sm, secret-ref | has sm, auth va secret-ref       |
			// | self-monitoring with secret-ref       | with sm, token      | has sm, auth via token           |
			// | self-monitoring with secret-ref       | with sm, secret-ref | has sm, auth via secret-ref      |
			// | --------------------------------------|---------------------|----------------------------------|

			// |---------------------------------------|---------------------|----------------------------------|
			// | no self-monitoring, no authorization  | no sm, no auth      | no sm, no auth                   |
			Entry("no self-monitoring, no auth -> no self-monitoring, no auth", SelfMonitoringAndApiAccessTestConfig{
				existingControllerDeployment:               CreateControllerDeploymentWithoutSelfMonitoringWithoutAuth,
				operatorConfigurationResourceSpec:          OperatorConfigurationResourceWithoutSelfMonitoringWithoutAuth,
				expectedControllerDeploymentAfterReconcile: CreateControllerDeploymentWithoutSelfMonitoringWithoutAuth,
				expectK8sClientUpdate:                      false,
			}),
			// | no self-monitoring, no authorization  | no sm, token        | no sm, auth via token            |
			Entry("no self-monitoring, no auth -> no self-monitoring, token", SelfMonitoringAndApiAccessTestConfig{
				existingControllerDeployment:               CreateControllerDeploymentWithoutSelfMonitoringWithoutAuth,
				operatorConfigurationResourceSpec:          OperatorConfigurationResourceWithoutSelfMonitoringWithToken,
				expectedControllerDeploymentAfterReconcile: CreateControllerDeploymentWithoutSelfMonitoringWithToken,
				expectK8sClientUpdate:                      true,
			}),
			// | no self-monitoring, no authorization  | no sm, secret-ref   | no sm, auth via secret-ref       |
			Entry("no self-monitoring, no auth -> no self-monitoring, secret-ref", SelfMonitoringAndApiAccessTestConfig{
				existingControllerDeployment:               CreateControllerDeploymentWithoutSelfMonitoringWithoutAuth,
				operatorConfigurationResourceSpec:          OperatorConfigurationResourceWithoutSelfMonitoringWithSecretRef,
				expectedControllerDeploymentAfterReconcile: CreateControllerDeploymentWithoutSelfMonitoringWithSecretRef,
				expectK8sClientUpdate:                      true,
			}),
			// | no self-monitoring, no authorization  | with sm, token      | has sm, auth via token            |
			Entry("no self-monitoring, no auth -> self-monitoring, token", SelfMonitoringAndApiAccessTestConfig{
				existingControllerDeployment:               CreateControllerDeploymentWithoutSelfMonitoringWithoutAuth,
				operatorConfigurationResourceSpec:          OperatorConfigurationResourceWithSelfMonitoringWithToken,
				expectedControllerDeploymentAfterReconcile: CreateControllerDeploymentWithSelfMonitoringWithToken,
				expectK8sClientUpdate:                      true,
			}),
			// | no self-monitoring, no authorization  | with sm, secret-ref | has sm, auth via secret-ref       |
			Entry("no self-monitoring, no auth -> self-monitoring, secret-ref", SelfMonitoringAndApiAccessTestConfig{
				existingControllerDeployment:               CreateControllerDeploymentWithoutSelfMonitoringWithoutAuth,
				operatorConfigurationResourceSpec:          OperatorConfigurationResourceWithSelfMonitoringWithSecretRef,
				expectedControllerDeploymentAfterReconcile: CreateControllerDeploymentWithSelfMonitoringWithSecretRef,
				expectK8sClientUpdate:                      true,
			}),

			// | --------------------------------------|---------------------|----------------------------------|
			// | no self-monitoring, but token         | no sm, no auth      | no sm, no auth	                |
			Entry("no self-monitoring, token -> no self-monitoring, no auth", SelfMonitoringAndApiAccessTestConfig{
				existingControllerDeployment:               CreateControllerDeploymentWithoutSelfMonitoringWithToken,
				operatorConfigurationResourceSpec:          OperatorConfigurationResourceWithoutSelfMonitoringWithoutAuth,
				expectedControllerDeploymentAfterReconcile: CreateControllerDeploymentWithoutSelfMonitoringWithoutAuth,
				expectK8sClientUpdate:                      true,
			}),
			// | no self-monitoring, but secret-ref    | no sm, no auth      | no sm, no auth                   |
			Entry("no self-monitoring, secret-ref -> no self-monitoring, no auth", SelfMonitoringAndApiAccessTestConfig{
				existingControllerDeployment:               CreateControllerDeploymentWithoutSelfMonitoringWithSecretRef,
				operatorConfigurationResourceSpec:          OperatorConfigurationResourceWithoutSelfMonitoringWithoutAuth,
				expectedControllerDeploymentAfterReconcile: CreateControllerDeploymentWithoutSelfMonitoringWithoutAuth,
				expectK8sClientUpdate:                      true,
			}),
			// | no self-monitoring, but token         | no sm, token        | no sm, auth via token            |
			Entry("no self-monitoring, token -> no self-monitoring, token", SelfMonitoringAndApiAccessTestConfig{
				existingControllerDeployment:               CreateControllerDeploymentWithoutSelfMonitoringWithToken,
				operatorConfigurationResourceSpec:          OperatorConfigurationResourceWithoutSelfMonitoringWithToken,
				expectedControllerDeploymentAfterReconcile: CreateControllerDeploymentWithoutSelfMonitoringWithToken,
				expectK8sClientUpdate:                      false,
			}),
			// | no self-monitoring, but token         | no sm, secret-ref   | no sm, auth via secret-ref       |
			Entry("no self-monitoring, token -> no self-monitoring, secret-ref", SelfMonitoringAndApiAccessTestConfig{
				existingControllerDeployment:               CreateControllerDeploymentWithoutSelfMonitoringWithToken,
				operatorConfigurationResourceSpec:          OperatorConfigurationResourceWithoutSelfMonitoringWithSecretRef,
				expectedControllerDeploymentAfterReconcile: CreateControllerDeploymentWithoutSelfMonitoringWithSecretRef,
				expectK8sClientUpdate:                      true,
			}),
			// | no self-monitoring, but secret-ref    | no sm, token        | no sm, auth via token            |
			Entry("no self-monitoring, secret-ref -> no self-monitoring, token", SelfMonitoringAndApiAccessTestConfig{
				existingControllerDeployment:               CreateControllerDeploymentWithoutSelfMonitoringWithSecretRef,
				operatorConfigurationResourceSpec:          OperatorConfigurationResourceWithoutSelfMonitoringWithToken,
				expectedControllerDeploymentAfterReconcile: CreateControllerDeploymentWithoutSelfMonitoringWithToken,
				expectK8sClientUpdate:                      true,
			}),
			// | no self-monitoring, but secret-ref    | no sm, secret-ref   | no sm, auth via secret-ref       |
			Entry("no self-monitoring, secret-ref -> no self-monitoring, secret-ref", SelfMonitoringAndApiAccessTestConfig{
				existingControllerDeployment:               CreateControllerDeploymentWithoutSelfMonitoringWithSecretRef,
				operatorConfigurationResourceSpec:          OperatorConfigurationResourceWithoutSelfMonitoringWithSecretRef,
				expectedControllerDeploymentAfterReconcile: CreateControllerDeploymentWithoutSelfMonitoringWithSecretRef,
				expectK8sClientUpdate:                      false,
			}),
			// | no self-monitoring, but token         | with sm, token      | has sm, auth via token           |
			Entry("no self-monitoring, token -> self-monitoring, token", SelfMonitoringAndApiAccessTestConfig{
				existingControllerDeployment:               CreateControllerDeploymentWithoutSelfMonitoringWithToken,
				operatorConfigurationResourceSpec:          OperatorConfigurationResourceWithSelfMonitoringWithToken,
				expectedControllerDeploymentAfterReconcile: CreateControllerDeploymentWithSelfMonitoringWithToken,
				expectK8sClientUpdate:                      true,
			}),
			// | no self-monitoring, but token         | with sm, secret-ref | has sm, auth via secret-ref      |
			Entry("no self-monitoring, token -> self-monitoring, secret-ref", SelfMonitoringAndApiAccessTestConfig{
				existingControllerDeployment:               CreateControllerDeploymentWithoutSelfMonitoringWithToken,
				operatorConfigurationResourceSpec:          OperatorConfigurationResourceWithSelfMonitoringWithSecretRef,
				expectedControllerDeploymentAfterReconcile: CreateControllerDeploymentWithSelfMonitoringWithSecretRef,
				expectK8sClientUpdate:                      true,
			}),
			// | no self-monitoring, but secret-ref    | with sm, token      | has sm, auth via token           |
			Entry("no self-monitoring, secret-ref -> self-monitoring, token", SelfMonitoringAndApiAccessTestConfig{
				existingControllerDeployment:               CreateControllerDeploymentWithoutSelfMonitoringWithSecretRef,
				operatorConfigurationResourceSpec:          OperatorConfigurationResourceWithSelfMonitoringWithToken,
				expectedControllerDeploymentAfterReconcile: CreateControllerDeploymentWithSelfMonitoringWithToken,
				expectK8sClientUpdate:                      true,
			}),
			// | no self-monitoring, but secret-ref    | with sm, secret-ref | has sm, auth via secret-ref      |
			Entry("no self-monitoring, secret-ref -> self-monitoring, token", SelfMonitoringAndApiAccessTestConfig{
				existingControllerDeployment:               CreateControllerDeploymentWithoutSelfMonitoringWithSecretRef,
				operatorConfigurationResourceSpec:          OperatorConfigurationResourceWithSelfMonitoringWithSecretRef,
				expectedControllerDeploymentAfterReconcile: CreateControllerDeploymentWithSelfMonitoringWithSecretRef,
				expectK8sClientUpdate:                      true,
			}),

			// | --------------------------------------|---------------------|----------------------------------|
			// | self-monitoring with token            | no sm, no auth      | no sm, no auth                   |
			Entry("self-monitoring, token -> no self-monitoring, no auth", SelfMonitoringAndApiAccessTestConfig{
				existingControllerDeployment:               CreateControllerDeploymentWithSelfMonitoringWithToken,
				operatorConfigurationResourceSpec:          OperatorConfigurationResourceWithoutSelfMonitoringWithoutAuth,
				expectedControllerDeploymentAfterReconcile: CreateControllerDeploymentWithoutSelfMonitoringWithoutAuth,
				expectK8sClientUpdate:                      true,
			}),
			// | self-monitoring with secret-ref       | no sm, no auth      | no sm, no auth                   |
			Entry("self-monitoring, secret-ref -> no self-monitoring, no auth", SelfMonitoringAndApiAccessTestConfig{
				existingControllerDeployment:               CreateControllerDeploymentWithSelfMonitoringWithSecretRef,
				operatorConfigurationResourceSpec:          OperatorConfigurationResourceWithoutSelfMonitoringWithoutAuth,
				expectedControllerDeploymentAfterReconcile: CreateControllerDeploymentWithoutSelfMonitoringWithoutAuth,
				expectK8sClientUpdate:                      true,
			}),
			// | self-monitoring with token            | no sm, token        | no sm, auth via token            |
			Entry("self-monitoring, token -> no self-monitoring, token", SelfMonitoringAndApiAccessTestConfig{
				existingControllerDeployment:               CreateControllerDeploymentWithSelfMonitoringWithToken,
				operatorConfigurationResourceSpec:          OperatorConfigurationResourceWithoutSelfMonitoringWithToken,
				expectedControllerDeploymentAfterReconcile: CreateControllerDeploymentWithoutSelfMonitoringWithToken,
				expectK8sClientUpdate:                      true,
			}),
			// | self-monitoring with token            | no sm, secret-ref   | no sm, auth via secret-ref       |
			Entry("self-monitoring, token -> no self-monitoring, secret-ref", SelfMonitoringAndApiAccessTestConfig{
				existingControllerDeployment:               CreateControllerDeploymentWithSelfMonitoringWithToken,
				operatorConfigurationResourceSpec:          OperatorConfigurationResourceWithoutSelfMonitoringWithSecretRef,
				expectedControllerDeploymentAfterReconcile: CreateControllerDeploymentWithoutSelfMonitoringWithSecretRef,
				expectK8sClientUpdate:                      true,
			}),
			// | self-monitoring with secret-ref       | no sm, token        | no sm, auth via token            |
			Entry("self-monitoring, secret-ref -> no self-monitoring, token", SelfMonitoringAndApiAccessTestConfig{
				existingControllerDeployment:               CreateControllerDeploymentWithSelfMonitoringWithSecretRef,
				operatorConfigurationResourceSpec:          OperatorConfigurationResourceWithoutSelfMonitoringWithToken,
				expectedControllerDeploymentAfterReconcile: CreateControllerDeploymentWithoutSelfMonitoringWithToken,
				expectK8sClientUpdate:                      true,
			}),
			// | self-monitoring with secret-ref       | no sm, secret-ref   | no sm, auth via secret-ref       |
			Entry("self-monitoring, secret-ref -> no self-monitoring, secret-ref", SelfMonitoringAndApiAccessTestConfig{
				existingControllerDeployment:               CreateControllerDeploymentWithSelfMonitoringWithSecretRef,
				operatorConfigurationResourceSpec:          OperatorConfigurationResourceWithoutSelfMonitoringWithSecretRef,
				expectedControllerDeploymentAfterReconcile: CreateControllerDeploymentWithoutSelfMonitoringWithSecretRef,
				expectK8sClientUpdate:                      true,
			}),
			// | self-monitoring with token            | with sm, token      | has sm, auth via token            |
			Entry("self-monitoring, token -> self-monitoring, token", SelfMonitoringAndApiAccessTestConfig{
				existingControllerDeployment:               CreateControllerDeploymentWithSelfMonitoringWithToken,
				operatorConfigurationResourceSpec:          OperatorConfigurationResourceWithSelfMonitoringWithToken,
				expectedControllerDeploymentAfterReconcile: CreateControllerDeploymentWithSelfMonitoringWithToken,
				expectK8sClientUpdate:                      false,
			}),
			// | self-monitoring with token            | with sm, secret-ref | has sm, auth via secret-ref       |
			Entry("self-monitoring, token -> self-monitoring, secret-ref", SelfMonitoringAndApiAccessTestConfig{
				existingControllerDeployment:               CreateControllerDeploymentWithSelfMonitoringWithToken,
				operatorConfigurationResourceSpec:          OperatorConfigurationResourceWithSelfMonitoringWithSecretRef,
				expectedControllerDeploymentAfterReconcile: CreateControllerDeploymentWithSelfMonitoringWithSecretRef,
				expectK8sClientUpdate:                      true,
			}),
			// | self-monitoring with secret-ref       | with sm, token      | has sm, auth via token           |
			Entry("self-monitoring, secret-ref -> self-monitoring, token", SelfMonitoringAndApiAccessTestConfig{
				existingControllerDeployment:               CreateControllerDeploymentWithSelfMonitoringWithSecretRef,
				operatorConfigurationResourceSpec:          OperatorConfigurationResourceWithSelfMonitoringWithToken,
				expectedControllerDeploymentAfterReconcile: CreateControllerDeploymentWithSelfMonitoringWithToken,
				expectK8sClientUpdate:                      true,
			}),
			// | self-monitoring with secret-ref       | with sm, secret-ref | has sm, auth via secret-ref      |
			Entry("self-monitoring, secret-ref -> self-monitoring, secret-ref", SelfMonitoringAndApiAccessTestConfig{
				existingControllerDeployment:               CreateControllerDeploymentWithSelfMonitoringWithSecretRef,
				operatorConfigurationResourceSpec:          OperatorConfigurationResourceWithSelfMonitoringWithSecretRef,
				expectedControllerDeploymentAfterReconcile: CreateControllerDeploymentWithSelfMonitoringWithSecretRef,
				expectK8sClientUpdate:                      false,
			}),
		)
	})

	Describe("updates all registered API clients", func() {
		AfterEach(func() {
			RemoveOperatorConfigurationResource(ctx, k8sClient)
		})

		DescribeTable("by settings or removing the API config", func(config ApiClientSetRemoveTestConfig) {
			controllerDeployment = EnsureControllerDeploymentExists(
				ctx,
				k8sClient,
				CreateControllerDeploymentWithoutSelfMonitoringWithoutAuth(),
			)
			reconciler = createReconciler(controllerDeployment)

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
					Expect(apiClient.setCalls).To(Equal(1))
					Expect(apiClient.removeCalls).To(Equal(0))
					Expect(apiClient.apiConfig.Endpoint).To(Equal(ApiEndpointTest))
					Expect(apiClient.apiConfig.Dataset).To(Equal(expectedDataset))
				}
				if config.expectRemoveApiEndpointAndDataset {
					Expect(apiClient.setCalls).To(Equal(0))
					Expect(apiClient.removeCalls).To(Equal(1))
					Expect(apiClient.apiConfig).To(BeNil())
				}
			}
		},

			// | operator config resource                  | expected calls |
			// |-------------------------------------------|----------------|
			// | no Dash0 export                           | remove         |
			// | no API endpoint, token                    | remove         |
			// | no API endpoint, secret-ref               | remove         |
			// | API endpoint, token                       | set            |
			// | API endpoint, secret-ref                  | set            |

			// | no Dash0 export                           | remove         |
			Entry("no API endpoint, no Dash0 export", ApiClientSetRemoveTestConfig{
				operatorConfigurationResourceSpec: OperatorConfigurationResourceWithoutExport,
				expectSetApiEndpointAndDataset:    false,
				expectRemoveApiEndpointAndDataset: true,
			}),
			// | no API endpoint, token                    | remove         |
			Entry("no API endpoint, Dash0 export with token", ApiClientSetRemoveTestConfig{
				operatorConfigurationResourceSpec: OperatorConfigurationResourceDash0ExportWithoutApiEndpointWithToken,
				expectSetApiEndpointAndDataset:    false,
				expectRemoveApiEndpointAndDataset: true,
			}),
			// | no API endpoint, secret-ref               | remove         |
			Entry("no API endpoint, Dash0 export with secret-ref", ApiClientSetRemoveTestConfig{
				operatorConfigurationResourceSpec: OperatorConfigurationResourceDash0ExportWithoutApiEndpointWithSecretRef,
				expectSetApiEndpointAndDataset:    false,
				expectRemoveApiEndpointAndDataset: true,
			}),
			// | API endpoint, token                       | set            |
			Entry("API endpoint, Dash0 export with token", ApiClientSetRemoveTestConfig{
				operatorConfigurationResourceSpec: OperatorConfigurationResourceDash0ExportWithApiEndpointWithToken,
				expectSetApiEndpointAndDataset:    true,
				expectRemoveApiEndpointAndDataset: false,
			}),
			// | API endpoint, secret-ref                  | set            |
			Entry("API endpoint, Dash0 export with secret-ref", ApiClientSetRemoveTestConfig{
				operatorConfigurationResourceSpec: OperatorConfigurationResourceDash0ExportWithApiEndpointWithSecretRef,
				expectSetApiEndpointAndDataset:    true,
				expectRemoveApiEndpointAndDataset: false,
			}),
			// | API endpoint, token, custom dataset       | set            |
			Entry("API endpoint, Dash0 export with token, custom dataset", ApiClientSetRemoveTestConfig{
				operatorConfigurationResourceSpec: OperatorConfigurationResourceDash0ExportWithApiEndpointWithToken,
				dataset:                           "custom-dataset",
				expectSetApiEndpointAndDataset:    true,
				expectRemoveApiEndpointAndDataset: false,
			}),
			// | API endpoint, secret-ref, custom dataset  | set            |
			Entry("API endpoint, Dash0 export with secret-ref, custom dataset", ApiClientSetRemoveTestConfig{
				operatorConfigurationResourceSpec: OperatorConfigurationResourceDash0ExportWithApiEndpointWithSecretRef,
				dataset:                           "custom-dataset",
				expectSetApiEndpointAndDataset:    true,
				expectRemoveApiEndpointAndDataset: false,
			}),
		)
	})

	Describe("when creating the operator configuration resource", func() {

		BeforeEach(func() {
			// When creating the resource, we assume the operator has no
			// self-monitoring enabled
			controllerDeployment = CreateControllerDeploymentWithoutSelfMonitoringWithoutAuth()
			EnsureControllerDeploymentExists(ctx, k8sClient, controllerDeployment)
			reconciler = createReconciler(controllerDeployment)
		})

		AfterEach(func() {
			RemoveOperatorConfigurationResource(ctx, k8sClient)
			EnsureControllerDeploymentDoesNotExist(ctx, k8sClient, controllerDeployment)
		})

		Describe("enabling self-monitoring", func() {

			DescribeTable("it enables self-monitoring in the controller deployment",
				func(config SelfMonitoringTestConfig) {
					CreateOperatorConfigurationResourceWithSpec(
						ctx,
						k8sClient,
						dash0v1alpha1.Dash0OperatorConfigurationSpec{
							Export: ExportToPrt(config.createExport()),
							SelfMonitoring: dash0v1alpha1.SelfMonitoring{
								Enabled: ptr.To(true),
							},
						},
					)

					triggerOperatorConfigurationReconcileRequest(ctx, reconciler)
					verifyOperatorConfigurationResourceIsAvailable(ctx)
					Eventually(func(g Gomega) {
						updatedDeployment := LoadOperatorDeploymentOrFail(ctx, k8sClient, g)
						selfMonitoringAndApiAccessConfiguration, err :=
							selfmonitoringapiaccess.GetSelfMonitoringAndApiAccessConfigurationFromControllerDeployment(
								updatedDeployment,
								ControllerContainerName,
							)
						g.Expect(err).NotTo(HaveOccurred())
						g.Expect(selfMonitoringAndApiAccessConfiguration.SelfMonitoringEnabled).To(BeTrue())
						config.verify(g, selfMonitoringAndApiAccessConfiguration, updatedDeployment)
					}, timeout, pollingInterval).Should(Succeed())
				},
				Entry("with a Dash0 export with a token", SelfMonitoringTestConfig{
					createExport: Dash0ExportWithEndpointAndToken,
					verify:       verifySelfMonitoringConfigurationDash0Token,
				}),
				Entry("with a Dash0 export with a secret ref", SelfMonitoringTestConfig{
					createExport: Dash0ExportWithEndpointAndSecretRef,
					verify:       verifySelfMonitoringConfigurationDash0SecretRef,
				}),
				Entry("with a Grpc export", SelfMonitoringTestConfig{
					createExport: GrpcExportTest,
					verify:       verifySelfMonitoringConfigurationGrpc,
				}),
				Entry("with an HTTP export", SelfMonitoringTestConfig{
					createExport: HttpExportTest,
					verify:       verifySelfMonitoringConfigurationHttp,
				}),
			)
		})

		DescribeTable("it adds the auth token to the controller deployment even if self-monitoring is not enabled",
			func(config SelfMonitoringTestConfig) {
				CreateOperatorConfigurationResourceWithSpec(
					ctx,
					k8sClient,
					dash0v1alpha1.Dash0OperatorConfigurationSpec{
						Export: ExportToPrt(config.createExport()),
						SelfMonitoring: dash0v1alpha1.SelfMonitoring{
							Enabled: ptr.To(false),
						},
					},
				)

				triggerOperatorConfigurationReconcileRequest(ctx, reconciler)
				verifyOperatorConfigurationResourceIsAvailable(ctx)
				Eventually(func(g Gomega) {
					updatedDeployment := LoadOperatorDeploymentOrFail(ctx, k8sClient, g)
					selfMonitoringAndApiAccessConfiguration, err :=
						selfmonitoringapiaccess.GetSelfMonitoringAndApiAccessConfigurationFromControllerDeployment(
							updatedDeployment,
							ControllerContainerName,
						)
					g.Expect(err).NotTo(HaveOccurred())
					g.Expect(selfMonitoringAndApiAccessConfiguration.SelfMonitoringEnabled).To(BeFalse())
					config.verify(g, selfMonitoringAndApiAccessConfiguration, updatedDeployment)
				}, timeout, pollingInterval).Should(Succeed())
			},
			Entry("with a Dash0 export with a token", SelfMonitoringTestConfig{
				createExport: Dash0ExportWithEndpointAndTokenAndApiEndpoint,
				verify:       verifyNoSelfMontoringButAuthTokenEnvVarFromToken,
			}),
			Entry("with a Dash0 export with a secret ref", SelfMonitoringTestConfig{
				createExport: Dash0ExportWithEndpointAndSecretRefAndApiEndpoint,
				verify:       verifyNoSelfMonitoringButAuthTokenEnvVarFromSecretRef,
			}),
		)

		Describe("disabling self-monitoring", func() {

			It("it does not change the controller deployment (because self-monitoring was not enabled in the first place)", func() {
				CreateOperatorConfigurationResourceWithSpec(
					ctx,
					k8sClient,
					dash0v1alpha1.Dash0OperatorConfigurationSpec{
						Export: ExportToPrt(Dash0ExportWithEndpointAndToken()),
						SelfMonitoring: dash0v1alpha1.SelfMonitoring{
							Enabled: ptr.To(false),
						},
					},
				)

				triggerOperatorConfigurationReconcileRequest(ctx, reconciler)
				verifyOperatorConfigurationResourceIsAvailable(ctx)
				Consistently(func(g Gomega) {
					updatedDeployment := LoadOperatorDeploymentOrFail(ctx, k8sClient, g)
					selfMonitoringAndApiAccessConfiguration, err :=
						selfmonitoringapiaccess.GetSelfMonitoringAndApiAccessConfigurationFromControllerDeployment(
							updatedDeployment,
							ControllerContainerName,
						)
					g.Expect(err).NotTo(HaveOccurred())
					g.Expect(selfMonitoringAndApiAccessConfiguration.SelfMonitoringEnabled).To(BeFalse())
				}, consistentlyTimeout, pollingInterval).Should(Succeed())
			})
		})
	})

	Describe("when updating the operator configuration resource", func() {

		Describe("enabling self-monitoring", func() {

			Describe("when self-monitoring is already enabled", func() {

				BeforeEach(func() {
					// When creating the resource, we assume the operator has
					// self-monitoring enabled
					controllerDeployment = CreateControllerDeploymentWithSelfMonitoringWithToken()
					EnsureControllerDeploymentExists(ctx, k8sClient, controllerDeployment)
					reconciler = createReconciler(controllerDeployment)
				})

				AfterEach(func() {
					RemoveOperatorConfigurationResource(ctx, k8sClient)
					EnsureControllerDeploymentDoesNotExist(ctx, k8sClient, controllerDeployment)
				})

				It("it does not change the controller deployment", func() {
					CreateOperatorConfigurationResourceWithSpec(
						ctx,
						k8sClient,
						dash0v1alpha1.Dash0OperatorConfigurationSpec{
							Export: ExportToPrt(Dash0ExportWithEndpointAndToken()),
							SelfMonitoring: dash0v1alpha1.SelfMonitoring{
								Enabled: ptr.To(true),
							},
						},
					)

					triggerOperatorConfigurationReconcileRequest(ctx, reconciler)
					verifyOperatorConfigurationResourceIsAvailable(ctx)

					Consistently(func(g Gomega) {
						updatedDeployment := LoadOperatorDeploymentOrFail(ctx, k8sClient, g)
						selfMonitoringAndApiAccessConfiguration, err :=
							selfmonitoringapiaccess.GetSelfMonitoringAndApiAccessConfigurationFromControllerDeployment(
								updatedDeployment,
								ControllerContainerName,
							)
						g.Expect(err).NotTo(HaveOccurred())
						g.Expect(selfMonitoringAndApiAccessConfiguration.SelfMonitoringEnabled).To(BeTrue())
					}, consistentlyTimeout, pollingInterval).Should(Succeed())
				})
			})

			Describe("when self-monitoring was previously disabled", func() {

				BeforeEach(func() {
					controllerDeployment = CreateControllerDeploymentWithoutSelfMonitoringWithoutAuth()
					EnsureControllerDeploymentExists(ctx, k8sClient, controllerDeployment)
					reconciler = createReconciler(controllerDeployment)

					CreateOperatorConfigurationResourceWithSpec(
						ctx,
						k8sClient,
						dash0v1alpha1.Dash0OperatorConfigurationSpec{
							Export: ExportToPrt(Dash0ExportWithEndpointAndToken()),
							SelfMonitoring: dash0v1alpha1.SelfMonitoring{
								Enabled: ptr.To(false),
							},
						},
					)
				})

				AfterEach(func() {
					RemoveOperatorConfigurationResource(ctx, k8sClient)
					EnsureControllerDeploymentDoesNotExist(ctx, k8sClient, controllerDeployment)
				})

				It("it enables self-monitoring in the controller deployment", func() {
					resource := LoadOperatorConfigurationResourceOrFail(ctx, k8sClient, Default)
					Expect(*resource.Spec.SelfMonitoring.Enabled).To(BeFalse())

					resource.Spec.SelfMonitoring.Enabled = ptr.To(true)

					Expect(k8sClient.Update(ctx, resource)).To(Succeed())

					triggerOperatorConfigurationReconcileRequest(ctx, reconciler)
					verifyOperatorConfigurationResourceIsAvailable(ctx)
					Eventually(func(g Gomega) {
						updatedDeployment := LoadOperatorDeploymentOrFail(ctx, k8sClient, g)
						selfMonitoringAndApiAccessConfiguration, err :=
							selfmonitoringapiaccess.GetSelfMonitoringAndApiAccessConfigurationFromControllerDeployment(
								updatedDeployment,
								ControllerContainerName,
							)
						g.Expect(err).NotTo(HaveOccurred())
						g.Expect(selfMonitoringAndApiAccessConfiguration.SelfMonitoringEnabled).To(BeTrue())
					}, timeout, pollingInterval).Should(Succeed())
				})
			})
		})

		Describe("disabling self-monitoring", func() {

			Describe("when self-monitoring is enabled", func() {

				BeforeEach(func() {
					CreateOperatorConfigurationResourceWithSpec(
						ctx,
						k8sClient,
						dash0v1alpha1.Dash0OperatorConfigurationSpec{
							Export: ExportToPrt(Dash0ExportWithEndpointAndToken()),
							SelfMonitoring: dash0v1alpha1.SelfMonitoring{
								Enabled: ptr.To(true),
							},
						},
					)

					controllerDeployment = CreateControllerDeploymentWithSelfMonitoringWithToken()
					EnsureControllerDeploymentExists(ctx, k8sClient, controllerDeployment)
					reconciler = createReconciler(controllerDeployment)
				})

				AfterEach(func() {
					RemoveOperatorConfigurationResource(ctx, k8sClient)
					EnsureControllerDeploymentDoesNotExist(ctx, k8sClient, controllerDeployment)
				})

				It("it disables self-monitoring in the controller deployment", func() {
					resource := LoadOperatorConfigurationResourceOrFail(ctx, k8sClient, Default)
					Expect(*resource.Spec.SelfMonitoring.Enabled).To(BeTrue())

					resource.Spec.SelfMonitoring.Enabled = ptr.To(false)

					Expect(k8sClient.Update(ctx, resource)).To(Succeed())

					triggerOperatorConfigurationReconcileRequest(ctx, reconciler)
					verifyOperatorConfigurationResourceIsAvailable(ctx)
					Eventually(func(g Gomega) {
						updatedDeployment := LoadOperatorDeploymentOrFail(ctx, k8sClient, g)
						selfMonitoringAndApiAccessConfiguration, err :=
							selfmonitoringapiaccess.GetSelfMonitoringAndApiAccessConfigurationFromControllerDeployment(
								updatedDeployment,
								ControllerContainerName,
							)
						g.Expect(err).NotTo(HaveOccurred())
						g.Expect(selfMonitoringAndApiAccessConfiguration.SelfMonitoringEnabled).To(BeFalse())
					}, timeout, pollingInterval).Should(Succeed())
				})
			})

			Describe("when self-monitoring is already disabled", func() {

				BeforeEach(func() {
					CreateOperatorConfigurationResourceWithSpec(
						ctx,
						k8sClient,
						dash0v1alpha1.Dash0OperatorConfigurationSpec{
							Export: ExportToPrt(Dash0ExportWithEndpointAndToken()),
							SelfMonitoring: dash0v1alpha1.SelfMonitoring{
								Enabled: ptr.To(false),
							},
						},
					)

					controllerDeployment = CreateControllerDeploymentWithoutSelfMonitoringWithoutAuth()
					EnsureControllerDeploymentExists(ctx, k8sClient, controllerDeployment)
					reconciler = createReconciler(controllerDeployment)
				})

				AfterEach(func() {
					RemoveOperatorConfigurationResource(ctx, k8sClient)
					EnsureControllerDeploymentDoesNotExist(ctx, k8sClient, controllerDeployment)
				})

				It("it does not change the controller deployment", func() {
					resource := LoadOperatorConfigurationResourceOrFail(ctx, k8sClient, Default)
					Expect(*resource.Spec.SelfMonitoring.Enabled).To(BeFalse())

					resource.Spec.SelfMonitoring.Enabled = ptr.To(false)

					Expect(k8sClient.Update(ctx, resource)).To(Succeed())

					triggerOperatorConfigurationReconcileRequest(ctx, reconciler)
					verifyOperatorConfigurationResourceIsAvailable(ctx)
					Consistently(func(g Gomega) {
						updatedDeployment := LoadOperatorDeploymentOrFail(ctx, k8sClient, g)
						selfMonitoringAndApiAccessConfiguration, err :=
							selfmonitoringapiaccess.GetSelfMonitoringAndApiAccessConfigurationFromControllerDeployment(
								updatedDeployment,
								ControllerContainerName,
							)
						g.Expect(err).NotTo(HaveOccurred())
						g.Expect(selfMonitoringAndApiAccessConfiguration.SelfMonitoringEnabled).To(BeFalse())
					}, consistentlyTimeout, pollingInterval).Should(Succeed())
				})
			})
		})
	})

	Describe("when deleting the operator configuration resource", func() {

		Describe("when self-monitoring is enabled", func() {

			BeforeEach(func() {
				CreateOperatorConfigurationResourceWithSpec(
					ctx,
					k8sClient,
					dash0v1alpha1.Dash0OperatorConfigurationSpec{
						Export: ExportToPrt(Dash0ExportWithEndpointAndToken()),
						SelfMonitoring: dash0v1alpha1.SelfMonitoring{
							Enabled: ptr.To(true),
						},
					},
				)

				controllerDeployment = CreateControllerDeploymentWithSelfMonitoringWithToken()
				EnsureControllerDeploymentExists(ctx, k8sClient, controllerDeployment)
				reconciler = createReconciler(controllerDeployment)
			})

			AfterEach(func() {
				RemoveOperatorConfigurationResource(ctx, k8sClient)
				EnsureControllerDeploymentDoesNotExist(ctx, k8sClient, controllerDeployment)
			})

			It("it disables self-monitoring in the controller deployment", func() {
				selfMonitoringAndApiAccessConfiguration, err :=
					selfmonitoringapiaccess.GetSelfMonitoringAndApiAccessConfigurationFromControllerDeployment(
						controllerDeployment,
						ControllerContainerName,
					)
				Expect(err).NotTo(HaveOccurred())
				Expect(selfMonitoringAndApiAccessConfiguration.SelfMonitoringEnabled).To(BeTrue())

				resource := LoadOperatorConfigurationResourceOrFail(ctx, k8sClient, Default)
				Expect(k8sClient.Delete(ctx, resource)).To(Succeed())

				triggerOperatorConfigurationReconcileRequest(ctx, reconciler)
				VerifyOperatorConfigurationResourceByNameDoesNotExist(ctx, k8sClient, Default, resource.Name)

				for _, apiClient := range []*DummyApiClient{apiClient1, apiClient2} {
					Expect(apiClient.setCalls).To(Equal(0))
					Expect(apiClient.removeCalls).To(Equal(1))
					Expect(apiClient.apiConfig).To(BeNil())
				}

				Eventually(func(g Gomega) {
					updatedDeployment := LoadOperatorDeploymentOrFail(ctx, k8sClient, g)
					selfMonitoringAndApiAccessConfiguration, err :=
						selfmonitoringapiaccess.GetSelfMonitoringAndApiAccessConfigurationFromControllerDeployment(
							updatedDeployment,
							ControllerContainerName,
						)
					g.Expect(err).NotTo(HaveOccurred())
					g.Expect(selfMonitoringAndApiAccessConfiguration.SelfMonitoringEnabled).To(BeFalse())
				}, timeout, pollingInterval).Should(Succeed())
			})
		})

		Describe("when self-monitoring is disabled", func() {

			BeforeEach(func() {
				CreateOperatorConfigurationResourceWithSpec(
					ctx,
					k8sClient,
					dash0v1alpha1.Dash0OperatorConfigurationSpec{
						SelfMonitoring: dash0v1alpha1.SelfMonitoring{
							Enabled: ptr.To(false),
						},
					},
				)

				controllerDeployment = CreateControllerDeploymentWithoutSelfMonitoringWithoutAuth()
				EnsureControllerDeploymentExists(ctx, k8sClient, controllerDeployment)
				reconciler = createReconciler(controllerDeployment)
			})

			AfterEach(func() {
				RemoveOperatorConfigurationResource(ctx, k8sClient)
				EnsureControllerDeploymentDoesNotExist(ctx, k8sClient, controllerDeployment)
			})

			It("it does not change the controller deployment", func() {
				selfMonitoringAndApiAccessConfiguration, err :=
					selfmonitoringapiaccess.GetSelfMonitoringAndApiAccessConfigurationFromControllerDeployment(
						controllerDeployment,
						ControllerContainerName,
					)
				Expect(err).NotTo(HaveOccurred())
				Expect(selfMonitoringAndApiAccessConfiguration.SelfMonitoringEnabled).To(BeFalse())

				resource := LoadOperatorConfigurationResourceOrFail(ctx, k8sClient, Default)
				Expect(*resource.Spec.SelfMonitoring.Enabled).To(BeFalse())

				Expect(k8sClient.Delete(ctx, resource)).To(Succeed())

				triggerOperatorConfigurationReconcileRequest(ctx, reconciler)
				VerifyOperatorConfigurationResourceByNameDoesNotExist(ctx, k8sClient, Default, resource.Name)

				for _, apiClient := range []*DummyApiClient{apiClient1, apiClient2} {
					Expect(apiClient.setCalls).To(Equal(0))
					Expect(apiClient.removeCalls).To(Equal(1))
					Expect(apiClient.apiConfig).To(BeNil())
				}

				Consistently(func(g Gomega) {
					updatedDeployment := LoadOperatorDeploymentOrFail(ctx, k8sClient, g)
					selfMonitoringAndApiAccessConfiguration, err :=
						selfmonitoringapiaccess.GetSelfMonitoringAndApiAccessConfigurationFromControllerDeployment(
							updatedDeployment,
							ControllerContainerName,
						)
					g.Expect(err).NotTo(HaveOccurred())
					g.Expect(selfMonitoringAndApiAccessConfiguration.SelfMonitoringEnabled).To(BeFalse())
				}, consistentlyTimeout, pollingInterval).Should(Succeed())
			})
		})
	})
})

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

func createReconciler(controllerDeployment *appsv1.Deployment) *OperatorConfigurationReconciler {

	return &OperatorConfigurationReconciler{
		Client:    k8sClient,
		Clientset: clientset,
		Recorder:  recorder,
		ApiClients: []ApiClient{
			apiClient1,
			apiClient2,
		},
		DeploymentSelfReference: controllerDeployment,
		DanglingEventsTimeouts:  &DanglingEventsTimeoutsTest,
		Images:                  TestImages,
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

func verifySelfMonitoringConfigurationDash0Token(
	g Gomega,
	selfMonitoringAndApiAccessConfiguration selfmonitoringapiaccess.SelfMonitoringAndApiAccessConfiguration,
	_ *appsv1.Deployment,
) {
	dash0ExportConfiguration := selfMonitoringAndApiAccessConfiguration.Export.Dash0
	g.Expect(dash0ExportConfiguration).NotTo(BeNil())
	g.Expect(dash0ExportConfiguration.Endpoint).To(Equal(EndpointDash0WithProtocolTest))
	g.Expect(dash0ExportConfiguration.Dataset).To(Equal(util.DatasetInsights))
	authorization := dash0ExportConfiguration.Authorization
	g.Expect(authorization).ToNot(BeNil())
	g.Expect(*authorization.Token).To(Equal(AuthorizationTokenTest))
	g.Expect(authorization.SecretRef).To(BeNil())
	g.Expect(selfMonitoringAndApiAccessConfiguration.Export.Grpc).To(BeNil())
	g.Expect(selfMonitoringAndApiAccessConfiguration.Export.Http).To(BeNil())
}

func verifySelfMonitoringConfigurationDash0SecretRef(
	g Gomega,
	selfMonitoringAndApiAccessConfiguration selfmonitoringapiaccess.SelfMonitoringAndApiAccessConfiguration,
	_ *appsv1.Deployment,
) {
	dash0ExportConfiguration := selfMonitoringAndApiAccessConfiguration.Export.Dash0
	g.Expect(dash0ExportConfiguration).NotTo(BeNil())
	g.Expect(dash0ExportConfiguration.Endpoint).To(Equal(EndpointDash0WithProtocolTest))
	g.Expect(dash0ExportConfiguration.Dataset).To(Equal(util.DatasetInsights))
	authorization := dash0ExportConfiguration.Authorization
	g.Expect(authorization.Token).To(BeNil())
	g.Expect(authorization.SecretRef).ToNot(BeNil())
	g.Expect(authorization.SecretRef.Name).To(Equal(SecretRefTest.Name))
	g.Expect(authorization.SecretRef.Key).To(Equal(SecretRefTest.Key))
	g.Expect(selfMonitoringAndApiAccessConfiguration.Export.Grpc).To(BeNil())
	g.Expect(selfMonitoringAndApiAccessConfiguration.Export.Http).To(BeNil())
}

func verifySelfMonitoringConfigurationGrpc(
	g Gomega,
	selfMonitoringAndApiAccessConfiguration selfmonitoringapiaccess.SelfMonitoringAndApiAccessConfiguration,
	_ *appsv1.Deployment,
) {
	grpcExportConfiguration := selfMonitoringAndApiAccessConfiguration.Export.Grpc
	g.Expect(grpcExportConfiguration).NotTo(BeNil())
	g.Expect(grpcExportConfiguration.Endpoint).To(Equal("dns://" + EndpointGrpcTest))
	headers := grpcExportConfiguration.Headers
	g.Expect(headers).To(HaveLen(2))
	g.Expect(headers[0].Name).To(Equal("Key"))
	g.Expect(headers[0].Value).To(Equal("Value"))
	g.Expect(headers[1].Name).To(Equal(util.Dash0DatasetHeaderName))
	g.Expect(headers[1].Value).To(Equal(util.DatasetInsights))
	g.Expect(selfMonitoringAndApiAccessConfiguration.Export.Dash0).To(BeNil())
	g.Expect(selfMonitoringAndApiAccessConfiguration.Export.Http).To(BeNil())
}

func verifySelfMonitoringConfigurationHttp(
	g Gomega,
	selfMonitoringAndApiAccessConfiguration selfmonitoringapiaccess.SelfMonitoringAndApiAccessConfiguration,
	_ *appsv1.Deployment,
) {
	httpExportConfiguration := selfMonitoringAndApiAccessConfiguration.Export.Http
	g.Expect(httpExportConfiguration).NotTo(BeNil())
	g.Expect(httpExportConfiguration.Endpoint).To(Equal(EndpointHttpTest))
	g.Expect(httpExportConfiguration.Encoding).To(Equal(dash0v1alpha1.Proto))
	headers := httpExportConfiguration.Headers
	g.Expect(headers).To(HaveLen(2))
	g.Expect(headers[0].Name).To(Equal("Key"))
	g.Expect(headers[0].Value).To(Equal("Value"))
	g.Expect(headers[1].Name).To(Equal(util.Dash0DatasetHeaderName))
	g.Expect(headers[1].Value).To(Equal(util.DatasetInsights))
	g.Expect(selfMonitoringAndApiAccessConfiguration.Export.Dash0).To(BeNil())
	g.Expect(selfMonitoringAndApiAccessConfiguration.Export.Grpc).To(BeNil())
}

func verifyNoSelfMontoringButAuthTokenEnvVarFromToken(
	g Gomega,
	selfMonitoringAndApiAccessConfiguration selfmonitoringapiaccess.SelfMonitoringAndApiAccessConfiguration,
	controllerDeployment *appsv1.Deployment,
) {
	g.Expect(selfMonitoringAndApiAccessConfiguration.SelfMonitoringEnabled).To(BeFalse())
	container := controllerDeployment.Spec.Template.Spec.Containers[0]
	g.Expect(container.Env).To(
		ContainElement(MatchEnvVar("SELF_MONITORING_AND_API_AUTH_TOKEN", AuthorizationTokenTest)))
}

func verifyNoSelfMonitoringButAuthTokenEnvVarFromSecretRef(
	g Gomega,
	selfMonitoringAndApiAccessConfiguration selfmonitoringapiaccess.SelfMonitoringAndApiAccessConfiguration,
	controllerDeployment *appsv1.Deployment,
) {
	g.Expect(selfMonitoringAndApiAccessConfiguration.SelfMonitoringEnabled).To(BeFalse())
	container := controllerDeployment.Spec.Template.Spec.Containers[0]
	g.Expect(container.Env).To(
		ContainElement(MatchEnvVarValueFrom("SELF_MONITORING_AND_API_AUTH_TOKEN", "secret-ref", "key")))
}

type DummyApiClient struct {
	setCalls    int
	removeCalls int
	apiConfig   *ApiConfig
}

func (c *DummyApiClient) SetApiEndpointAndDataset(apiConfig *ApiConfig, _ *logr.Logger) {
	c.setCalls++
	c.apiConfig = apiConfig
}

func (c *DummyApiClient) RemoveApiEndpointAndDataset() {
	c.removeCalls++
	c.apiConfig = nil
}
