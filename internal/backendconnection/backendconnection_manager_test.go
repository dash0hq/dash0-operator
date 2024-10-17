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

	dash0v1alpha1 "github.com/dash0hq/dash0-operator/api/dash0monitoring/v1alpha1"
	"github.com/dash0hq/dash0-operator/internal/backendconnection/otelcolresources"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	. "github.com/dash0hq/dash0-operator/test/util"
)

var (
	operatorNamespace = OperatorNamespace
)

var _ = Describe("The backend connection manager", Ordered, func() {
	ctx := context.Background()
	logger := log.FromContext(ctx)

	var createdObjects []client.Object

	var manager *BackendConnectionManager

	BeforeAll(func() {
		EnsureOperatorNamespaceExists(ctx, k8sClient)
		EnsureTestNamespaceExists(ctx, k8sClient)
	})

	BeforeEach(func() {
		createdObjects = make([]client.Object, 0)
		oTelColResourceManager := &otelcolresources.OTelColResourceManager{
			Client:                  k8sClient,
			Scheme:                  k8sClient.Scheme(),
			DeploymentSelfReference: DeploymentSelfReference,
			OTelCollectorNamePrefix: OTelCollectorNamePrefixTest,
			OTelColResourceSpecs:    &otelcolresources.DefaultOTelColResourceSpecs,
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
		BeforeEach(func() {
			// creating a valid monitoring resource beforehand, just so we get past the
			// m.findAllMonitoringResources step.
			resource := EnsureMonitoringResourceExistsAndIsAvailable(
				ctx,
				k8sClient,
			)
			createdObjects = append(createdObjects, resource)
		})

		It("should fail if no endpoint is provided", func() {
			err := manager.ReconcileOpenTelemetryCollector(
				ctx,
				TestImages,
				operatorNamespace,
				&dash0v1alpha1.Dash0Monitoring{
					Spec: dash0v1alpha1.Dash0MonitoringSpec{
						Export: &dash0v1alpha1.Export{
							Dash0: &dash0v1alpha1.Dash0Configuration{
								Authorization: dash0v1alpha1.Authorization{
									Token: &AuthorizationTokenTest,
								},
							},
						},
					},
				},
				TriggeredByDash0Resource,
			)
			Expect(err).To(MatchError(
				"cannot assemble the exporters for the configuration: no endpoint provided for the Dash0 exporter, unable to create the OpenTelemetry collector"))
			VerifyCollectorResourcesDoNotExist(ctx, k8sClient, operatorNamespace)
		})

		It("should fail if neither authorization token nor secret ref are provided for Dash0 exporter", func() {
			err := manager.ReconcileOpenTelemetryCollector(
				ctx,
				TestImages,
				operatorNamespace,
				&dash0v1alpha1.Dash0Monitoring{
					Spec: dash0v1alpha1.Dash0MonitoringSpec{
						Export: &dash0v1alpha1.Export{
							Dash0: &dash0v1alpha1.Dash0Configuration{
								Endpoint:      EndpointDash0Test,
								Authorization: dash0v1alpha1.Authorization{},
							},
						},
					},
				},
				TriggeredByDash0Resource,
			)
			Expect(err).To(MatchError(
				"neither token nor secretRef provided for the Dash0 exporter"))
			VerifyCollectorResourcesDoNotExist(ctx, k8sClient, operatorNamespace)
		})
	})

	Describe("when creating OpenTelemetry collector resources", func() {

		BeforeEach(func() {
			// creating a valid monitoring resource beforehand, just so we get past the
			// m.findAllMonitoringResources step.
			resource := EnsureMonitoringResourceExistsAndIsAvailable(
				ctx,
				k8sClient,
			)
			createdObjects = append(createdObjects, resource)
		})

		AfterEach(func() {
			err := manager.OTelColResourceManager.DeleteResources(
				ctx,
				operatorNamespace,
				&logger,
			)
			Expect(err).ToNot(HaveOccurred())

			DeleteOperatorConfigurationResource(ctx, k8sClient)
		})

		It("should create all resources", func() {
			err := manager.ReconcileOpenTelemetryCollector(
				ctx,
				TestImages,
				operatorNamespace,
				assembleMonitoringResource(),
				TriggeredByDash0Resource,
			)
			Expect(err).ToNot(HaveOccurred())
			VerifyCollectorResources(ctx, k8sClient, operatorNamespace)
		})

		It("should fall back to the operator configuration export settings if the monitoring resource has no export", func() {
			CreateDefaultOperatorConfigurationResource(
				ctx,
				k8sClient,
			)
			err := manager.ReconcileOpenTelemetryCollector(
				ctx,
				TestImages,
				operatorNamespace,
				&dash0v1alpha1.Dash0Monitoring{
					Spec: dash0v1alpha1.Dash0MonitoringSpec{},
				},
				TriggeredByDash0Resource,
			)
			Expect(err).ToNot(HaveOccurred())
			VerifyCollectorResources(ctx, k8sClient, operatorNamespace)
		})

		It("should fail if the monitoring resource has no export and there is no operator configuration resource", func() {
			err := manager.ReconcileOpenTelemetryCollector(
				ctx,
				TestImages,
				operatorNamespace,
				&dash0v1alpha1.Dash0Monitoring{
					Spec: dash0v1alpha1.Dash0MonitoringSpec{},
				},
				TriggeredByDash0Resource,
			)
			Expect(err).To(
				MatchError(
					"the provided Dash0Monitoring resource does not have an export configuration and no " +
						"Dash0OperatorConfiguration resource has been found"))
			VerifyCollectorResourcesDoNotExist(ctx, k8sClient, operatorNamespace)
		})

		It("should fail if the monitoring resource has no export and the existing operator configuration "+
			"resource has no export either", func() {
			CreateOperatorConfigurationResourceWithSpec(
				ctx,
				k8sClient,
				dash0v1alpha1.Dash0OperatorConfigurationSpec{},
			)
			err := manager.ReconcileOpenTelemetryCollector(
				ctx,
				TestImages,
				operatorNamespace,
				&dash0v1alpha1.Dash0Monitoring{
					Spec: dash0v1alpha1.Dash0MonitoringSpec{},
				},
				TriggeredByDash0Resource,
			)
			Expect(err).To(MatchError("the provided Dash0Monitoring resource does not have an export configuration " +
				"and the Dash0OperatorConfiguration resource does not have one either"))
			VerifyCollectorResourcesDoNotExist(ctx, k8sClient, operatorNamespace)
		})
	})

	Describe("when updating OpenTelemetry collector resources", func() {

		BeforeEach(func() {
			// creating a valid monitoring resource beforehand, just so we get past the
			// m.findAllMonitoringResources step.
			resource := EnsureMonitoringResourceExistsAndIsAvailable(
				ctx,
				k8sClient,
			)
			createdObjects = append(createdObjects, resource)
		})

		It("should update the resources", func() {
			err := k8sClient.Create(ctx, &corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Name:      ExpectedDaemonSetCollectorConfigMapName,
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

			err = manager.ReconcileOpenTelemetryCollector(
				ctx,
				TestImages,
				operatorNamespace,
				assembleMonitoringResource(),
				TriggeredByDash0Resource,
			)
			Expect(err).ToNot(HaveOccurred())
			VerifyCollectorResources(ctx, k8sClient, operatorNamespace)

			// verify that all wrong properties that we have set up initially have been removed
			cm := GetOTelColDaemonSetConfigMap(ctx, k8sClient, operatorNamespace)
			Expect(cm.Data).To(HaveKey("config.yaml"))
			Expect(cm.Data).ToNot(HaveKey("wrong-key"))
			Expect(cm.Labels).ToNot(HaveKey("wrong-key"))
			Expect(cm.Annotations).ToNot(HaveKey("wrong-key"))
		})
	})

	Describe("when cleaning up OpenTelemetry collector resources when the resource is deleted", func() {
		It("should not delete the collector if there are still Dash0 monitoring resources", func() {
			// create multiple Dash0 monitoring resources
			firstName := types.NamespacedName{Namespace: TestNamespaceName, Name: "dash0-monitoring-test-resource-1"}
			firstDash0MonitoringResource := CreateDefaultMonitoringResource(ctx, k8sClient, firstName)
			createdObjects = append(createdObjects, firstDash0MonitoringResource)
			setAvailable(ctx, k8sClient, firstDash0MonitoringResource)

			secondName := types.NamespacedName{Namespace: TestNamespaceName, Name: "dash0-monitoring-test-resource-2"}
			secondDash0MonitoringResource := CreateDefaultMonitoringResource(ctx, k8sClient, secondName)
			createdObjects = append(createdObjects, secondDash0MonitoringResource)
			setAvailable(ctx, k8sClient, secondDash0MonitoringResource)

			thirdName := types.NamespacedName{Namespace: TestNamespaceName, Name: "dash0-monitoring-test-resource-3"}
			thirdDash0MonitoringResource := CreateDefaultMonitoringResource(ctx, k8sClient, thirdName)
			createdObjects = append(createdObjects, thirdDash0MonitoringResource)
			setAvailable(ctx, k8sClient, thirdDash0MonitoringResource)

			// Let the manager create the collector so there is something to delete.
			err := manager.ReconcileOpenTelemetryCollector(
				ctx,
				TestImages,
				operatorNamespace,
				secondDash0MonitoringResource,
				TriggeredByDash0Resource,
			)
			Expect(err).ToNot(HaveOccurred())
			VerifyCollectorResources(ctx, k8sClient, operatorNamespace)

			err = manager.RemoveOpenTelemetryCollectorIfNoMonitoringResourceIsLeft(
				ctx,
				operatorNamespace,
				secondDash0MonitoringResource,
			)
			Expect(err).ToNot(HaveOccurred())
			// since other Dash0 monitoring resources still exist, the collector resources should not be deleted
			VerifyCollectorResources(ctx, k8sClient, operatorNamespace)
		})

		It("should not delete the collector if there is only one Dash0 monitoring resource left but it is not the one being deleted", func() {
			resourceName := types.NamespacedName{Namespace: TestNamespaceName, Name: "dash0-monitoring-test-resource-1"}
			existingDash0MonitoringResource := CreateDefaultMonitoringResource(ctx, k8sClient, resourceName)
			createdObjects = append(createdObjects, existingDash0MonitoringResource)
			setAvailable(ctx, k8sClient, existingDash0MonitoringResource)

			// Let the manager create the collector so there is something to delete.
			err := manager.ReconcileOpenTelemetryCollector(
				ctx,
				TestImages,
				operatorNamespace,
				existingDash0MonitoringResource,
				TriggeredByDash0Resource,
			)
			Expect(err).ToNot(HaveOccurred())
			VerifyCollectorResources(ctx, k8sClient, operatorNamespace)

			err = manager.RemoveOpenTelemetryCollectorIfNoMonitoringResourceIsLeft(
				ctx,
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
						Export: &dash0v1alpha1.Export{
							Dash0: &dash0v1alpha1.Dash0Configuration{
								Endpoint: EndpointDash0Test,
								Authorization: dash0v1alpha1.Authorization{
									Token: &AuthorizationTokenTest,
								},
							},
						},
					},
				},
			)
			Expect(err).ToNot(HaveOccurred())
			VerifyCollectorResources(ctx, k8sClient, operatorNamespace)
		})

		It("should delete the collector if the Dash0 monitoring resource that is being deleted is the only one left", func() {
			resourceName := types.NamespacedName{Namespace: TestNamespaceName, Name: "dash0-monitoring-test-resource-1"}
			monitoringResource := CreateDefaultMonitoringResource(ctx, k8sClient, resourceName)
			createdObjects = append(createdObjects, monitoringResource)
			setAvailable(ctx, k8sClient, monitoringResource)

			// Let the manager create the collector so there is something to delete.
			err := manager.ReconcileOpenTelemetryCollector(
				ctx,
				TestImages,
				operatorNamespace,
				monitoringResource,
				TriggeredByDash0Resource,
			)
			Expect(err).ToNot(HaveOccurred())
			VerifyCollectorResources(ctx, k8sClient, operatorNamespace)

			err = manager.RemoveOpenTelemetryCollectorIfNoMonitoringResourceIsLeft(
				ctx,
				operatorNamespace,
				monitoringResource,
			)
			Expect(err).ToNot(HaveOccurred())
			// verify the collector is deleted when the Dash0 monitoring resource provided as a parameter is the only
			// one left
			VerifyCollectorResourcesDoNotExist(ctx, k8sClient, operatorNamespace)
		})

		It("should delete the collector if no Dash0 monitoring resource exists", func() {
			// Let the manager create the collector so there is something to delete.
			resource := EnsureMonitoringResourceExistsAndIsAvailable(
				ctx,
				k8sClient,
			)
			err := manager.ReconcileOpenTelemetryCollector(
				ctx,
				TestImages,
				operatorNamespace,
				resource,
				TriggeredByDash0Resource,
			)
			Expect(err).ToNot(HaveOccurred())
			VerifyCollectorResources(ctx, k8sClient, operatorNamespace)

			Expect(k8sClient.Delete(ctx, resource)).To(Succeed())

			err = manager.RemoveOpenTelemetryCollectorIfNoMonitoringResourceIsLeft(
				ctx,
				operatorNamespace,
				resource,
			)
			Expect(err).ToNot(HaveOccurred())
			VerifyCollectorResourcesDoNotExist(ctx, k8sClient, operatorNamespace)
		})
	})
})

func assembleMonitoringResource() *dash0v1alpha1.Dash0Monitoring {
	return &dash0v1alpha1.Dash0Monitoring{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "dash0-monitoring-test-resource",
			Namespace: TestNamespaceName,
		},
		Spec: dash0v1alpha1.Dash0MonitoringSpec{
			Export: &dash0v1alpha1.Export{
				Dash0: &dash0v1alpha1.Dash0Configuration{
					Endpoint: EndpointDash0Test,
					Authorization: dash0v1alpha1.Authorization{
						Token: &AuthorizationTokenTest,
					},
				},
			},
		},
	}
}

func setAvailable(ctx context.Context, k8sClient client.Client, monitoringResource *dash0v1alpha1.Dash0Monitoring) {
	monitoringResource.EnsureResourceIsMarkedAsAvailable()
	Expect(k8sClient.Status().Update(ctx, monitoringResource)).To(Succeed())
}
