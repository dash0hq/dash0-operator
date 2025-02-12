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
			OTelColExtraConfig:      &otelcolresources.OTelExtraConfigDefaults,
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
		It("should fail if no endpoint is provided", func() {
			monitoringResource := &dash0v1alpha1.Dash0Monitoring{
				Spec: dash0v1alpha1.Dash0MonitoringSpec{
					Export: &dash0v1alpha1.Export{
						Dash0: &dash0v1alpha1.Dash0Configuration{
							Authorization: dash0v1alpha1.Authorization{
								Token: &AuthorizationTokenTest,
							},
						},
					},
				},
			}
			monitoringResource.EnsureResourceIsMarkedAsAvailable()
			err := manager.ReconcileOpenTelemetryCollector(
				ctx,
				TestImages,
				operatorNamespace,
				monitoringResource,
				TriggeredByDash0ResourceReconcile,
			)
			Expect(err).To(MatchError(
				"cannot assemble the exporters for the configuration: no endpoint provided for the Dash0 exporter, unable to create the OpenTelemetry collector"))
			VerifyCollectorResourcesDoNotExist(ctx, k8sClient, operatorNamespace)
		})

		It("should fail if neither authorization token nor secret ref are provided for Dash0 exporter", func() {
			monitoringResource := &dash0v1alpha1.Dash0Monitoring{
				Spec: dash0v1alpha1.Dash0MonitoringSpec{
					Export: &dash0v1alpha1.Export{
						Dash0: &dash0v1alpha1.Dash0Configuration{
							Endpoint:      EndpointDash0Test,
							Authorization: dash0v1alpha1.Authorization{},
						},
					},
				},
			}
			monitoringResource.EnsureResourceIsMarkedAsAvailable()
			err := manager.ReconcileOpenTelemetryCollector(
				ctx,
				TestImages,
				operatorNamespace,
				monitoringResource,
				TriggeredByDash0ResourceReconcile,
			)
			Expect(err).To(MatchError(
				"neither token nor secretRef provided for the Dash0 exporter"))
			VerifyCollectorResourcesDoNotExist(ctx, k8sClient, operatorNamespace)
		})
	})

	Describe("when creating OpenTelemetry collector resources", func() {

		AfterEach(func() {
			_, err := manager.OTelColResourceManager.DeleteResources(
				ctx,
				operatorNamespace,
				&logger,
			)
			Expect(err).ToNot(HaveOccurred())

			DeleteAllOperatorConfigurationResources(ctx, k8sClient)
		})

		It("should do nothing if there is no operator configuration resource and also no monitoring resource", func() {
			err := manager.ReconcileOpenTelemetryCollector(
				ctx,
				TestImages,
				operatorNamespace,
				nil,
				TriggeredByDash0ResourceReconcile,
			)
			Expect(err).ToNot(HaveOccurred())
			VerifyCollectorResourcesDoNotExist(ctx, k8sClient, operatorNamespace)
		})

		It("should create the Dash0 collectors based on the operator configuration's export settings ", func() {
			CreateDefaultOperatorConfigurationResource(
				ctx,
				k8sClient,
			)
			err := manager.ReconcileOpenTelemetryCollector(
				ctx,
				TestImages,
				operatorNamespace,
				nil,
				TriggeredByDash0ResourceReconcile,
			)
			Expect(err).ToNot(HaveOccurred())
			VerifyCollectorResources(ctx, k8sClient, operatorNamespace, EndpointDash0Test, AuthorizationTokenTest)
		})

		It("the operator configuration's export settings should have priority over the triggering monitoring source and the existing monitoring resources", func() {
			CreateDefaultOperatorConfigurationResource(
				ctx,
				k8sClient,
			)
			monitoringResource := EnsureMonitoringResourceWithSpecExistsAndIsAvailable(
				ctx,
				k8sClient,
				dash0v1alpha1.Dash0MonitoringSpec{
					Export: &dash0v1alpha1.Export{
						Dash0: &dash0v1alpha1.Dash0Configuration{
							Endpoint: EndpointDash0TestAlternative,
							Authorization: dash0v1alpha1.Authorization{
								Token: &AuthorizationTokenTestAlternative,
							},
						},
					},
				},
			)
			createdObjects = append(createdObjects, monitoringResource)
			err := manager.ReconcileOpenTelemetryCollector(
				ctx,
				TestImages,
				operatorNamespace,
				assembleMonitoringResource(EndpointDash0TestAlternative, AuthorizationTokenTestAlternative),
				TriggeredByDash0ResourceReconcile,
			)
			Expect(err).ToNot(HaveOccurred())
			VerifyCollectorResources(ctx, k8sClient, operatorNamespace, EndpointDash0Test, AuthorizationTokenTest)
		})

		It("should use the triggering monitoring resource's export settings if there is no operator configuration resource", func() {
			// this monitoring resource is just created to verify that the triggering resource takes priority
			monitoringResource := EnsureMonitoringResourceWithSpecExistsAndIsAvailable(
				ctx,
				k8sClient,
				dash0v1alpha1.Dash0MonitoringSpec{
					Export: &dash0v1alpha1.Export{
						Dash0: &dash0v1alpha1.Dash0Configuration{
							Endpoint: EndpointDash0TestAlternative,
							Authorization: dash0v1alpha1.Authorization{
								Token: &AuthorizationTokenTestAlternative,
							},
						},
					},
				},
			)
			createdObjects = append(createdObjects, monitoringResource)

			err := manager.ReconcileOpenTelemetryCollector(
				ctx,
				TestImages,
				operatorNamespace,
				assembleMonitoringResource(EndpointDash0Test, AuthorizationTokenTest),
				TriggeredByDash0ResourceReconcile,
			)
			Expect(err).ToNot(HaveOccurred())
			VerifyCollectorResources(ctx, k8sClient, operatorNamespace, EndpointDash0Test, AuthorizationTokenTest)
		})

		It("should use the export settings from an existing monitoring resource if there is no operator configuration resource and no triggering monitoring resource", func() {
			monitoringResource := EnsureMonitoringResourceExistsAndIsAvailable(
				ctx,
				k8sClient,
			)
			createdObjects = append(createdObjects, monitoringResource)
			err := manager.ReconcileOpenTelemetryCollector(
				ctx,
				TestImages,
				operatorNamespace,
				nil,
				TriggeredByDash0ResourceReconcile,
			)
			Expect(err).ToNot(HaveOccurred())
			VerifyCollectorResources(ctx, k8sClient, operatorNamespace, EndpointDash0Test, AuthorizationTokenTest)
		})

		It("should do nothing if there is no operator configuration resource and the triggering monitoring resource has no export", func() {
			monitoringResource := &dash0v1alpha1.Dash0Monitoring{
				Spec: dash0v1alpha1.Dash0MonitoringSpec{},
			}
			monitoringResource.EnsureResourceIsMarkedAsAvailable()
			err := manager.ReconcileOpenTelemetryCollector(
				ctx,
				TestImages,
				operatorNamespace,
				monitoringResource,
				TriggeredByDash0ResourceReconcile,
			)
			Expect(err).ToNot(HaveOccurred())
			VerifyCollectorResourcesDoNotExist(ctx, k8sClient, operatorNamespace)
		})

		It("should do nothing if there is no operator configuration resource and the triggering monitoring resource is not marked as available", func() {
			monitoringResource := &dash0v1alpha1.Dash0Monitoring{
				Spec: dash0v1alpha1.Dash0MonitoringSpec{
					Export: &dash0v1alpha1.Export{
						Dash0: &dash0v1alpha1.Dash0Configuration{
							Endpoint: EndpointDash0TestAlternative,
							Authorization: dash0v1alpha1.Authorization{
								Token: &AuthorizationTokenTestAlternative,
							},
						},
					},
				},
			}
			err := manager.ReconcileOpenTelemetryCollector(
				ctx,
				TestImages,
				operatorNamespace,
				monitoringResource,
				TriggeredByDash0ResourceReconcile,
			)
			Expect(err).ToNot(HaveOccurred())
			VerifyCollectorResourcesDoNotExist(ctx, k8sClient, operatorNamespace)
		})

		It("should do nothing if the operator configuration resource has no export and there is no monitoring resource", func() {
			CreateOperatorConfigurationResourceWithSpec(
				ctx,
				k8sClient,
				dash0v1alpha1.Dash0OperatorConfigurationSpec{},
			)
			err := manager.ReconcileOpenTelemetryCollector(
				ctx,
				TestImages,
				operatorNamespace,
				nil,
				TriggeredByDash0ResourceReconcile,
			)
			Expect(err).ToNot(HaveOccurred())
			VerifyCollectorResourcesDoNotExist(ctx, k8sClient, operatorNamespace)
		})

		It("should do nothing if the operator configuration resource has no export and the triggering monitoring resource has no export", func() {
			CreateOperatorConfigurationResourceWithSpec(
				ctx,
				k8sClient,
				dash0v1alpha1.Dash0OperatorConfigurationSpec{},
			)
			monitoringResource := &dash0v1alpha1.Dash0Monitoring{
				Spec: dash0v1alpha1.Dash0MonitoringSpec{},
			}
			monitoringResource.EnsureResourceIsMarkedAsAvailable()
			err := manager.ReconcileOpenTelemetryCollector(
				ctx,
				TestImages,
				operatorNamespace,
				monitoringResource,
				TriggeredByDash0ResourceReconcile,
			)
			Expect(err).ToNot(HaveOccurred())
			VerifyCollectorResourcesDoNotExist(ctx, k8sClient, operatorNamespace)
		})

		It("should do nothing if there is no operator configuration resource and neither the triggering nor the existing monitoring resource have an export", func() {
			existingMonitoringResource := EnsureMonitoringResourceExists(
				ctx,
				k8sClient,
			)
			createdObjects = append(createdObjects, existingMonitoringResource)
			triggeringMonitoringResource := &dash0v1alpha1.Dash0Monitoring{
				Spec: dash0v1alpha1.Dash0MonitoringSpec{
					Export: &dash0v1alpha1.Export{
						Dash0: &dash0v1alpha1.Dash0Configuration{
							Endpoint: EndpointDash0TestAlternative,
							Authorization: dash0v1alpha1.Authorization{
								Token: &AuthorizationTokenTestAlternative,
							},
						},
					},
				},
			}
			err := manager.ReconcileOpenTelemetryCollector(
				ctx,
				TestImages,
				operatorNamespace,
				triggeringMonitoringResource,
				TriggeredByDash0ResourceReconcile,
			)
			Expect(err).ToNot(HaveOccurred())
			VerifyCollectorResourcesDoNotExist(ctx, k8sClient, operatorNamespace)
		})

		It("should do nothing if there is no operator configuration resource and neither the triggering nor the existing monitoring are marked as available", func() {
			existingMonitoringResource := EnsureEmptyMonitoringResourceExistsAndIsAvailable(
				ctx,
				k8sClient,
			)
			createdObjects = append(createdObjects, existingMonitoringResource)
			triggeringMonitoringResource := &dash0v1alpha1.Dash0Monitoring{
				Spec: dash0v1alpha1.Dash0MonitoringSpec{},
			}
			triggeringMonitoringResource.EnsureResourceIsMarkedAsAvailable()
			err := manager.ReconcileOpenTelemetryCollector(
				ctx,
				TestImages,
				operatorNamespace,
				triggeringMonitoringResource,
				TriggeredByDash0ResourceReconcile,
			)
			Expect(err).ToNot(HaveOccurred())
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
				nil,
				TriggeredByDash0ResourceReconcile,
			)
			Expect(err).ToNot(HaveOccurred())
			VerifyCollectorResources(ctx, k8sClient, operatorNamespace, EndpointDash0Test, AuthorizationTokenTest)

			// verify that all wrong properties that we have set up initially have been removed
			cm := GetOTelColDaemonSetConfigMap(ctx, k8sClient, operatorNamespace)
			Expect(cm.Data).To(HaveKey("config.yaml"))
			Expect(cm.Data).ToNot(HaveKey("wrong-key"))
			Expect(cm.Labels).ToNot(HaveKey("wrong-key"))
			Expect(cm.Annotations).ToNot(HaveKey("wrong-key"))
		})
	})

	Describe("when deciding whether to delete the OpenTelemetry collector resources", func() {

		AfterEach(func() {
			_, err := manager.OTelColResourceManager.DeleteResources(
				ctx,
				operatorNamespace,
				&logger,
			)
			Expect(err).ToNot(HaveOccurred())

			DeleteAllOperatorConfigurationResources(ctx, k8sClient)
		})

		It("should not delete the collector if there is an operator configuration and the Dash0 monitoring resource that is being deleted is the only one left", func() {
			CreateDefaultOperatorConfigurationResource(
				ctx,
				k8sClient,
			)

			resourceName := types.NamespacedName{Namespace: TestNamespaceName, Name: "dash0-monitoring-test-resource-1"}
			monitoringResource := EnsureMonitoringResourceExistsInNamespaceAndIsAvailable(ctx, k8sClient, resourceName)
			createdObjects = append(createdObjects, monitoringResource)

			// Let the manager create the collector so there is something to delete.
			err := manager.ReconcileOpenTelemetryCollector(
				ctx,
				TestImages,
				operatorNamespace,
				monitoringResource,
				TriggeredByDash0ResourceReconcile,
			)
			Expect(err).ToNot(HaveOccurred())
			VerifyCollectorResources(ctx, k8sClient, operatorNamespace, EndpointDash0Test, AuthorizationTokenTest)

			err = manager.ReconcileOpenTelemetryCollector(
				ctx,
				TestImages,
				operatorNamespace,
				monitoringResource,
				TriggeredByDash0ResourceReconcile,
			)
			Expect(err).ToNot(HaveOccurred())
			// verify the collector is not deleted even if the monitoring resource provided as a parameter is the only
			// one left, when there is still an operator configuration left,
			VerifyCollectorResources(ctx, k8sClient, operatorNamespace, EndpointDash0Test, AuthorizationTokenTest)
		})

		It("should not delete the collector if there is an operator configuration resource and no monitoring resource exists", func() {
			CreateDefaultOperatorConfigurationResource(
				ctx,
				k8sClient,
			)

			// Let the manager create the collector so there is something to delete.
			monitoringResource := EnsureMonitoringResourceExistsAndIsAvailable(
				ctx,
				k8sClient,
			)
			err := manager.ReconcileOpenTelemetryCollector(
				ctx,
				TestImages,
				operatorNamespace,
				monitoringResource,
				TriggeredByDash0ResourceReconcile,
			)
			Expect(err).ToNot(HaveOccurred())
			VerifyCollectorResources(ctx, k8sClient, operatorNamespace, EndpointDash0Test, AuthorizationTokenTest)

			Expect(k8sClient.Delete(ctx, monitoringResource)).To(Succeed())

			err = manager.ReconcileOpenTelemetryCollector(
				ctx,
				TestImages,
				operatorNamespace,
				monitoringResource,
				TriggeredByDash0ResourceReconcile,
			)
			Expect(err).ToNot(HaveOccurred())
			// verify the collector is not deleted even if the last monitoring resource has been deleted, but there is
			// still an operator configuration left,
			VerifyCollectorResources(ctx, k8sClient, operatorNamespace, EndpointDash0Test, AuthorizationTokenTest)
		})

		It("should not delete the collector if there is no operator configuration resource but there are still monitoring resources", func() {
			// create multiple monitoring resources
			firstName := types.NamespacedName{Namespace: TestNamespaceName, Name: "dash0-monitoring-test-resource-1"}
			firstDash0MonitoringResource := EnsureMonitoringResourceExistsInNamespaceAndIsAvailable(ctx, k8sClient, firstName)
			createdObjects = append(createdObjects, firstDash0MonitoringResource)

			secondName := types.NamespacedName{Namespace: TestNamespaceName, Name: "dash0-monitoring-test-resource-2"}
			secondDash0MonitoringResource := EnsureMonitoringResourceExistsInNamespaceAndIsAvailable(ctx, k8sClient, secondName)
			createdObjects = append(createdObjects, secondDash0MonitoringResource)

			thirdName := types.NamespacedName{Namespace: TestNamespaceName, Name: "dash0-monitoring-test-resource-3"}
			thirdDash0MonitoringResource := EnsureMonitoringResourceExistsInNamespaceAndIsAvailable(ctx, k8sClient, thirdName)
			createdObjects = append(createdObjects, thirdDash0MonitoringResource)

			// Let the manager create the collector so there is something to delete.
			err := manager.ReconcileOpenTelemetryCollector(
				ctx,
				TestImages,
				operatorNamespace,
				secondDash0MonitoringResource,
				TriggeredByDash0ResourceReconcile,
			)
			Expect(err).ToNot(HaveOccurred())
			VerifyCollectorResources(ctx, k8sClient, operatorNamespace, EndpointDash0Test, AuthorizationTokenTest)

			err = manager.ReconcileOpenTelemetryCollector(
				ctx,
				TestImages,
				operatorNamespace,
				secondDash0MonitoringResource,
				TriggeredByDash0ResourceReconcile,
			)
			Expect(err).ToNot(HaveOccurred())
			// since other monitoring resources still exist, the collector resources should not be deleted
			VerifyCollectorResources(ctx, k8sClient, operatorNamespace, EndpointDash0Test, AuthorizationTokenTest)
		})

		It("should not delete the collector if there is no operator configuration resource, but if there one available resource with an export left", func() {
			resourceName := types.NamespacedName{Namespace: TestNamespaceName, Name: "dash0-monitoring-test-resource-1"}
			existingDash0MonitoringResource := EnsureMonitoringResourceExistsInNamespaceAndIsAvailable(ctx, k8sClient, resourceName)
			createdObjects = append(createdObjects, existingDash0MonitoringResource)

			// Let the manager create the collector so there is something to delete.
			err := manager.ReconcileOpenTelemetryCollector(
				ctx,
				TestImages,
				operatorNamespace,
				existingDash0MonitoringResource,
				TriggeredByDash0ResourceReconcile,
			)
			Expect(err).ToNot(HaveOccurred())
			VerifyCollectorResources(ctx, k8sClient, operatorNamespace, EndpointDash0Test, AuthorizationTokenTest)

			triggeringMonitoringResourceNotAvailable := &dash0v1alpha1.Dash0Monitoring{
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
			}
			err = manager.ReconcileOpenTelemetryCollector(
				ctx,
				TestImages,
				operatorNamespace,
				// Here the triggering monitoring resource has an export but is not marked as available, so it will not
				// contribute towards retaining the collector resources; the existing monitoring resource created above
				// has an export and is available, so ultimately, the collector resources are not deleted.
				triggeringMonitoringResourceNotAvailable,
				TriggeredByDash0ResourceReconcile,
			)
			Expect(err).ToNot(HaveOccurred())
			VerifyCollectorResources(ctx, k8sClient, operatorNamespace, EndpointDash0Test, AuthorizationTokenTest)
		})

		It("should delete the collector if the operator configuration is deleted and there are no monitoring resources", func() {
			operatorConfigurationResource := CreateDefaultOperatorConfigurationResource(
				ctx,
				k8sClient,
			)

			err := manager.ReconcileOpenTelemetryCollector(
				ctx,
				TestImages,
				operatorNamespace,
				nil,
				TriggeredByDash0ResourceReconcile,
			)
			Expect(err).ToNot(HaveOccurred())
			VerifyCollectorResources(ctx, k8sClient, operatorNamespace, EndpointDash0Test, AuthorizationTokenTest)

			Expect(k8sClient.Delete(ctx, operatorConfigurationResource)).To(Succeed())

			err = manager.ReconcileOpenTelemetryCollector(
				ctx,
				TestImages,
				operatorNamespace,
				nil,
				TriggeredByDash0ResourceReconcile,
			)
			Expect(err).ToNot(HaveOccurred())
			// verify the collector is not deleted even if the last monitoring resource has been deleted, but there is
			// still an operator configuration left,
			VerifyCollectorResourcesDoNotExist(ctx, k8sClient, operatorNamespace)
		})

		It("should delete the collector if the operator configuration is deleted and there are only monitoring resources without an export", func() {
			operatorConfigurationResource := CreateDefaultOperatorConfigurationResource(
				ctx,
				k8sClient,
			)

			// create multiple monitoring resources, all without export
			firstName := types.NamespacedName{Namespace: TestNamespaceName, Name: "dash0-monitoring-test-resource-1"}
			firstDash0MonitoringResource :=
				EnsureMonitoringResourceWithSpecExistsInNamespaceAndIsAvailable(ctx, k8sClient,
					dash0v1alpha1.Dash0MonitoringSpec{}, firstName)
			createdObjects = append(createdObjects, firstDash0MonitoringResource)

			secondName := types.NamespacedName{Namespace: TestNamespaceName, Name: "dash0-monitoring-test-resource-2"}
			secondDash0MonitoringResource :=
				EnsureMonitoringResourceWithSpecExistsInNamespaceAndIsAvailable(ctx, k8sClient,
					dash0v1alpha1.Dash0MonitoringSpec{}, secondName)
			createdObjects = append(createdObjects, secondDash0MonitoringResource)

			thirdName := types.NamespacedName{Namespace: TestNamespaceName, Name: "dash0-monitoring-test-resource-3"}
			thirdDash0MonitoringResource :=
				EnsureMonitoringResourceWithSpecExistsInNamespaceAndIsAvailable(ctx, k8sClient,
					dash0v1alpha1.Dash0MonitoringSpec{}, thirdName)
			createdObjects = append(createdObjects, thirdDash0MonitoringResource)

			err := manager.ReconcileOpenTelemetryCollector(
				ctx,
				TestImages,
				operatorNamespace,
				nil,
				TriggeredByDash0ResourceReconcile,
			)
			Expect(err).ToNot(HaveOccurred())
			VerifyCollectorResources(ctx, k8sClient, operatorNamespace, EndpointDash0Test, AuthorizationTokenTest)

			Expect(k8sClient.Delete(ctx, operatorConfigurationResource)).To(Succeed())

			err = manager.ReconcileOpenTelemetryCollector(
				ctx,
				TestImages,
				operatorNamespace,
				nil,
				TriggeredByDash0ResourceReconcile,
			)
			Expect(err).ToNot(HaveOccurred())
			// verify the collector is not deleted even if the last monitoring resource has been deleted, but there is
			// still an operator configuration left,
			VerifyCollectorResourcesDoNotExist(ctx, k8sClient, operatorNamespace)
		})

		It("should delete the collector if there is no operator configuration, and if the monitoring resource that is being deleted is the only one left", func() {
			resourceName := types.NamespacedName{Namespace: TestNamespaceName, Name: "dash0-monitoring-test-resource-1"}
			monitoringResource := EnsureMonitoringResourceExistsInNamespaceAndIsAvailable(ctx, k8sClient, resourceName)
			createdObjects = append(createdObjects, monitoringResource)

			// Let the manager create the collector so there is something to delete.
			err := manager.ReconcileOpenTelemetryCollector(
				ctx,
				TestImages,
				operatorNamespace,
				monitoringResource,
				TriggeredByDash0ResourceReconcile,
			)
			Expect(err).ToNot(HaveOccurred())
			VerifyCollectorResources(ctx, k8sClient, operatorNamespace, EndpointDash0Test, AuthorizationTokenTest)

			// When deleting the resource, it will be marked as about to be deleted=degraded in the finalizer handling
			// before calling manager.ReconcileOpenTelemetryCollector (this happens in
			// monitoring_controller.go#runCleanup). This is important for ReconcileOpenTelemetryCollector, so it does
			// not accidentally find an available monitoring resource.
			monitoringResource.EnsureResourceIsMarkedAsAboutToBeDeleted()
			Expect(k8sClient.Status().Update(ctx, monitoringResource)).To(Succeed())

			err = manager.ReconcileOpenTelemetryCollector(
				ctx,
				TestImages,
				operatorNamespace,
				monitoringResource,
				TriggeredByDash0ResourceReconcile,
			)
			Expect(err).ToNot(HaveOccurred())
			// verify the collector is deleted when the monitoring resource provided as a parameter is the only
			// one left
			VerifyCollectorResourcesDoNotExist(ctx, k8sClient, operatorNamespace)
		})

		It("should delete the collector if no operator configuration and no monitoring resource exists", func() {
			// Let the manager create the collector so there is something to delete.
			monitoringResource := EnsureMonitoringResourceExistsAndIsAvailable(
				ctx,
				k8sClient,
			)
			createdObjects = append(createdObjects, monitoringResource)
			err := manager.ReconcileOpenTelemetryCollector(
				ctx,
				TestImages,
				operatorNamespace,
				monitoringResource,
				TriggeredByDash0ResourceReconcile,
			)
			Expect(err).ToNot(HaveOccurred())
			VerifyCollectorResources(ctx, k8sClient, operatorNamespace, EndpointDash0Test, AuthorizationTokenTest)

			Expect(k8sClient.Delete(ctx, monitoringResource)).To(Succeed())
			createdObjects = createdObjects[0 : len(createdObjects)-1]

			err = manager.ReconcileOpenTelemetryCollector(
				ctx,
				TestImages,
				operatorNamespace,
				nil,
				TriggeredByDash0ResourceReconcile,
			)
			Expect(err).ToNot(HaveOccurred())
			VerifyCollectorResourcesDoNotExist(ctx, k8sClient, operatorNamespace)
		})
	})
})

func assembleMonitoringResource(endpoint string, authorizationToken string) *dash0v1alpha1.Dash0Monitoring {
	monitoringResource := &dash0v1alpha1.Dash0Monitoring{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "dash0-monitoring-test-resource",
			Namespace: TestNamespaceName,
		},
		Spec: dash0v1alpha1.Dash0MonitoringSpec{
			Export: &dash0v1alpha1.Export{
				Dash0: &dash0v1alpha1.Dash0Configuration{
					Endpoint: endpoint,
					Authorization: dash0v1alpha1.Authorization{
						Token: &authorizationToken,
					},
				},
			},
		},
	}
	monitoringResource.EnsureResourceIsMarkedAsAvailable()
	return monitoringResource
}
