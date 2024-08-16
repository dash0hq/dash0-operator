// SPDX-FileCopyrightText: Copyright 2024 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package backendconnection

import (
	"context"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	dash0v1alpha1 "github.com/dash0hq/dash0-operator/api/dash0monitoring/v1alpha1"
	"github.com/dash0hq/dash0-operator/internal/backendconnection/otelcolresources"

	. "github.com/dash0hq/dash0-operator/test/util"
)

var (
	operatorNamespace = Dash0OperatorNamespace

	dash0MonitoringResource = &dash0v1alpha1.Dash0Monitoring{
		Spec: dash0v1alpha1.Dash0MonitoringSpec{
			IngressEndpoint:    IngressEndpointTest,
			AuthorizationToken: AuthorizationTokenTest,
		},
	}
)

var _ = Describe("The backend connection manager", Ordered, func() {
	ctx := context.Background()
	logger := log.FromContext(ctx)

	var createdObjects []client.Object

	var manager *BackendConnectionManager

	BeforeAll(func() {
		EnsureDash0OperatorNamespaceExists(ctx, k8sClient)
		EnsureTestNamespaceExists(ctx, k8sClient)
	})

	BeforeEach(func() {
		createdObjects = make([]client.Object, 0)
		oTelColResourceManager := &otelcolresources.OTelColResourceManager{
			Client:                  k8sClient,
			Scheme:                  k8sClient.Scheme(),
			DeploymentSelfReference: DeploymentSelfReference,
			OTelCollectorNamePrefix: "unit-test",
		}
		manager = &BackendConnectionManager{
			Client:                 k8sClient,
			Clientset:              clientset,
			OTelColResourceManager: oTelColResourceManager,
		}
	})

	AfterEach(func() {
		createdObjects = DeleteAllCreatedObjects(ctx, k8sClient, createdObjects)
		DeleteAllEvents(ctx, clientset, operatorNamespace)
		err := k8sClient.DeleteAllOf(ctx, &corev1.ConfigMap{}, client.InNamespace(operatorNamespace))
		Expect(err).ToNot(HaveOccurred())
	})

	Describe("when validation checks fail", func() {
		It("should fail if no ingress endpoint is provided", func() {
			err := manager.EnsureOpenTelemetryCollectorIsDeployedInDash0OperatorNamespace(
				ctx,
				TestImages,
				operatorNamespace,
				&dash0v1alpha1.Dash0Monitoring{
					Spec: dash0v1alpha1.Dash0MonitoringSpec{
						AuthorizationToken: AuthorizationTokenTest,
					}})
			Expect(err).To(HaveOccurred())
			VerifyCollectorResourcesDoNotExist(ctx, k8sClient, operatorNamespace)
		})

		It("should not fail if neither authorization token nor secret ref are provided", func() {
			err := manager.EnsureOpenTelemetryCollectorIsDeployedInDash0OperatorNamespace(
				ctx,
				TestImages,
				operatorNamespace,
				&dash0v1alpha1.Dash0Monitoring{
					Spec: dash0v1alpha1.Dash0MonitoringSpec{
						IngressEndpoint: IngressEndpointTest,
					}})
			Expect(err).NotTo(HaveOccurred())
			VerifyCollectorResourcesExist(ctx, k8sClient, operatorNamespace)
		})
	})

	Describe("when creating OpenTelemetry collector resources", func() {

		AfterEach(func() {
			err := manager.OTelColResourceManager.DeleteResources(
				ctx,
				operatorNamespace,
				TestImages,
				dash0MonitoringResource,
				&logger,
			)
			Expect(err).ToNot(HaveOccurred())
		})

		It("should create all resources", func() {
			err := manager.EnsureOpenTelemetryCollectorIsDeployedInDash0OperatorNamespace(
				ctx,
				TestImages,
				operatorNamespace,
				dash0MonitoringResource,
			)
			Expect(err).ToNot(HaveOccurred())
			VerifyCollectorResourcesExist(ctx, k8sClient, operatorNamespace)
		})
	})

	Describe("when updating OpenTelemetry collector resources", func() {
		It("should update the resources", func() {
			err := k8sClient.Create(ctx, &corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Name:      ExpectedConfigMapName,
					Namespace: operatorNamespace,
					Labels: map[string]string{
						"wrong-key": "value",
					},
					Annotations: map[string]string{
						"wrong-key": "value",
					},
				},
				Data: map[string]string{
					"wrong-key": "{}",
				},
			})
			Expect(err).ToNot(HaveOccurred())

			err = manager.EnsureOpenTelemetryCollectorIsDeployedInDash0OperatorNamespace(
				ctx,
				TestImages,
				operatorNamespace,
				dash0MonitoringResource,
			)
			Expect(err).ToNot(HaveOccurred())
			VerifyCollectorResourcesExist(ctx, k8sClient, operatorNamespace)

			// verify that all wrong properties that we have set up initially have been removed
			cm := VerifyCollectorConfigMapExists(ctx, k8sClient, operatorNamespace)
			Expect(cm.Data).To(HaveKey("config.yaml"))
			Expect(cm.Data).ToNot(HaveKey("wrong-key"))
			Expect(cm.Labels).ToNot(HaveKey("wrong-key"))
			Expect(cm.Annotations).ToNot(HaveKey("wrong-key"))
		})
	})

	Describe("when cleaning up OpenTelemetry collector resources when the resource is deleted", func() {
		It("should not delete the collector if there are still Dash0 monitoring resources", func() {
			// create multiple Dash0 monitoring resources
			firstName := types.NamespacedName{Namespace: TestNamespaceName, Name: "das0-monitoring-test-resource-1"}
			firstDash0MonitoringResource := CreateDash0MonitoringResource(ctx, k8sClient, firstName)
			createdObjects = append(createdObjects, firstDash0MonitoringResource)

			secondName := types.NamespacedName{Namespace: TestNamespaceName, Name: "das0-monitoring-test-resource-2"}
			secondDash0MonitoringResource := CreateDash0MonitoringResource(ctx, k8sClient, secondName)
			createdObjects = append(createdObjects, secondDash0MonitoringResource)

			thirdName := types.NamespacedName{Namespace: TestNamespaceName, Name: "das0-monitoring-test-resource-3"}
			thirdDash0MonitoringResource := CreateDash0MonitoringResource(ctx, k8sClient, thirdName)
			createdObjects = append(createdObjects, thirdDash0MonitoringResource)

			// Let the manager create the collector so there is something to delete.
			err := manager.EnsureOpenTelemetryCollectorIsDeployedInDash0OperatorNamespace(
				ctx,
				TestImages,
				operatorNamespace,
				secondDash0MonitoringResource,
			)
			Expect(err).ToNot(HaveOccurred())
			VerifyCollectorResourcesExist(ctx, k8sClient, operatorNamespace)

			err = manager.RemoveOpenTelemetryCollectorIfNoDash0MonitoringResourceIsLeft(
				ctx,
				TestImages,
				operatorNamespace,
				secondDash0MonitoringResource,
			)
			Expect(err).ToNot(HaveOccurred())
			// since other Dash0 monitoring resources still exist, the collector resources should not be deleted
			VerifyCollectorResourcesExist(ctx, k8sClient, operatorNamespace)
		})

		It("should not delete the collector if there is only one Dash0 monitoring resource left but it is not the one being deleted", func() {
			resourceName := types.NamespacedName{Namespace: TestNamespaceName, Name: "das0-monitoring-test-resource-1"}
			existingDash0MonitoringResource := CreateDash0MonitoringResource(ctx, k8sClient, resourceName)
			createdObjects = append(createdObjects, existingDash0MonitoringResource)

			// Let the manager create the collector so there is something to delete.
			err := manager.EnsureOpenTelemetryCollectorIsDeployedInDash0OperatorNamespace(
				ctx,
				TestImages,
				operatorNamespace,
				existingDash0MonitoringResource,
			)
			Expect(err).ToNot(HaveOccurred())
			VerifyCollectorResourcesExist(ctx, k8sClient, operatorNamespace)

			err = manager.RemoveOpenTelemetryCollectorIfNoDash0MonitoringResourceIsLeft(
				ctx,
				TestImages,
				operatorNamespace,
				// We deliberately pass a different resource here, not the one that actually exists in the cluster.
				// The existing resource should be found and compared to the one that we pass in, and since they do
				// not match, the collector should not be deleted
				&dash0v1alpha1.Dash0Monitoring{
					ObjectMeta: metav1.ObjectMeta{
						Namespace: "some-other-namespace",
						Name:      "name",
						UID:       "3c0e72bb-26a7-40a4-bbdd-b1c978278fc5",
					},
					Spec: dash0v1alpha1.Dash0MonitoringSpec{
						IngressEndpoint:    IngressEndpointTest,
						AuthorizationToken: AuthorizationTokenTest,
					},
				},
			)
			Expect(err).ToNot(HaveOccurred())
			VerifyCollectorResourcesExist(ctx, k8sClient, operatorNamespace)
		})

		It("should delete the collector if the Dash0 monitoring resource that is being deleted is the only one left", func() {
			// create multiple Dash0 monitoring resources
			resourceName := types.NamespacedName{Namespace: TestNamespaceName, Name: "das0-monitoring-test-resource-1"}
			dash0MonitoringResource := CreateDash0MonitoringResource(ctx, k8sClient, resourceName)
			createdObjects = append(createdObjects, dash0MonitoringResource)

			// Let the manager create the collector so there is something to delete.
			err := manager.EnsureOpenTelemetryCollectorIsDeployedInDash0OperatorNamespace(
				ctx,
				TestImages,
				operatorNamespace,
				dash0MonitoringResource,
			)
			Expect(err).ToNot(HaveOccurred())
			VerifyCollectorResourcesExist(ctx, k8sClient, operatorNamespace)

			err = manager.RemoveOpenTelemetryCollectorIfNoDash0MonitoringResourceIsLeft(
				ctx,
				TestImages,
				operatorNamespace,
				dash0MonitoringResource,
			)
			Expect(err).ToNot(HaveOccurred())
			// verify the collector is deleted when the Dash0 monitoring resource provided as a parameter is the only
			// one left
			VerifyCollectorResourcesDoNotExist(ctx, k8sClient, operatorNamespace)
		})

		It("should delete the collector if no Dash0 monitoring resource exists", func() {
			// Let the manager create the collector so there is something to delete.
			err := manager.EnsureOpenTelemetryCollectorIsDeployedInDash0OperatorNamespace(
				ctx,
				TestImages,
				operatorNamespace,
				dash0MonitoringResource,
			)
			Expect(err).ToNot(HaveOccurred())
			VerifyCollectorResourcesExist(ctx, k8sClient, operatorNamespace)

			err = manager.RemoveOpenTelemetryCollectorIfNoDash0MonitoringResourceIsLeft(
				ctx,
				TestImages,
				operatorNamespace,
				dash0MonitoringResource,
			)
			Expect(err).ToNot(HaveOccurred())
			VerifyCollectorResourcesDoNotExist(ctx, k8sClient, operatorNamespace)
		})
	})
})
