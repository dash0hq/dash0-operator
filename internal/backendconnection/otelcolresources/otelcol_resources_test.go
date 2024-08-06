// SPDX-FileCopyrightText: Copyright 2024 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package otelcolresources

import (
	"context"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/dash0hq/dash0-operator/internal/dash0/util"
	. "github.com/dash0hq/dash0-operator/test/util"
)

var (
	expectedConfigMapName = "unit-test-opentelemetry-collector-agent"

	testObject = &corev1.ConfigMap{
		TypeMeta: metav1.TypeMeta{
			Kind:       "ConfigMap",
			APIVersion: "v1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-config-map",
			Namespace: Dash0OperatorNamespace,
			Labels: map[string]string{
				"label": "value",
			},
		},
		Data: map[string]string{
			"key": "value",
		},
	}
)

var _ = Describe("The OpenTelemetry Collector resource manager", Ordered, func() {
	ctx := context.Background()
	logger := log.FromContext(ctx)

	var oTelColResourceManager *OTelColResourceManager

	BeforeAll(func() {
		EnsureDash0OperatorNamespaceExists(ctx, k8sClient)
	})

	BeforeEach(func() {
		oTelColResourceManager = &OTelColResourceManager{
			Client:                  k8sClient,
			OTelCollectorNamePrefix: "unit-test",
		}
	})

	AfterEach(func() {
		err := k8sClient.DeleteAllOf(ctx, &corev1.ConfigMap{}, client.InNamespace(Dash0OperatorNamespace))
		Expect(err).ToNot(HaveOccurred())
	})

	Describe("when dealing with individual resources", func() {
		It("should create a single resource", func() {
			isNew, isChanged, err := oTelColResourceManager.createOrUpdateResource(ctx, testObject.DeepCopy(), &logger)
			Expect(err).ToNot(HaveOccurred())
			Expect(isNew).To(BeTrue())
			Expect(isChanged).To(BeFalse())
			verifyObject(ctx, testObject)
		})

		It("should update a single object", func() {
			err := oTelColResourceManager.createResource(ctx, testObject.DeepCopy(), &logger)
			Expect(err).ToNot(HaveOccurred())

			updated := testObject.DeepCopy()
			updated.Data["key"] = "updated value"
			isNew, isChanged, err := oTelColResourceManager.createOrUpdateResource(ctx, updated, &logger)

			Expect(err).ToNot(HaveOccurred())
			Expect(isNew).To(BeFalse())
			Expect(isChanged).To(BeTrue())
			verifyObject(ctx, updated)
		})

		It("should report that nothing has changed for a single object", func() {
			err := oTelColResourceManager.createResource(ctx, testObject.DeepCopy(), &logger)
			Expect(err).ToNot(HaveOccurred())

			isNew, isChanged, err := oTelColResourceManager.createOrUpdateResource(
				ctx,
				testObject.DeepCopy(),
				&logger,
			)

			Expect(err).ToNot(HaveOccurred())
			Expect(isNew).To(BeFalse())
			Expect(isChanged).To(BeFalse())
			verifyObject(ctx, testObject)
		})
	})

	Describe("when creating all OpenTelemetry collector resources", func() {
		It("should create the resources", func() {
			resourcesHaveBeenCreated, resourcesHaveBeenUpdated, err :=
				oTelColResourceManager.CreateOrUpdateOpenTelemetryCollectorResources(
					ctx,
					util.Images{},
					Dash0OperatorNamespace,
					IngressEndpoint,
					AuthorizationToken,
					SecretRefEmpty,
					&logger,
				)
			Expect(err).ToNot(HaveOccurred())
			Expect(resourcesHaveBeenCreated).To(BeTrue())
			Expect(resourcesHaveBeenUpdated).To(BeFalse())

			VerifyCollectorResourcesExist(ctx, k8sClient, Dash0OperatorNamespace)
		})
	})

	Describe("when updating all OpenTelemetry collector resources", func() {
		It("should update the resources", func() {
			err := k8sClient.Create(ctx, &corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Name:      expectedConfigMapName,
					Namespace: Dash0OperatorNamespace,
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
			resourcesHaveBeenCreated, resourcesHaveBeenUpdated, err :=
				oTelColResourceManager.CreateOrUpdateOpenTelemetryCollectorResources(
					ctx,
					util.Images{},
					Dash0OperatorNamespace,
					IngressEndpoint,
					AuthorizationToken,
					SecretRefEmpty,
					&logger,
				)
			Expect(err).ToNot(HaveOccurred())
			Expect(resourcesHaveBeenCreated).To(BeFalse())
			Expect(resourcesHaveBeenUpdated).To(BeTrue())

			VerifyCollectorResourcesExist(ctx, k8sClient, Dash0OperatorNamespace)
		})
	})

	Describe("when all OpenTelemetry collector resources are up to date", func() {
		It("should report that nothing has changed", func() {
			// create resources (so we are sure that everything is in the desired state)
			_, _, err := oTelColResourceManager.CreateOrUpdateOpenTelemetryCollectorResources(
				ctx,
				util.Images{},
				Dash0OperatorNamespace,
				IngressEndpoint,
				AuthorizationToken,
				SecretRefEmpty,
				&logger,
			)
			Expect(err).ToNot(HaveOccurred())

			// now run another create/update, to make sure resourcesHaveBeenCreated/resourcesHaveBeenUpdated come back
			// as false
			resourcesHaveBeenCreated, resourcesHaveBeenUpdated, err :=
				oTelColResourceManager.CreateOrUpdateOpenTelemetryCollectorResources(
					ctx,
					util.Images{},
					Dash0OperatorNamespace,
					IngressEndpoint,
					AuthorizationToken,
					SecretRefEmpty,
					&logger,
				)
			Expect(err).ToNot(HaveOccurred())
			Expect(resourcesHaveBeenCreated).To(BeFalse())
			Expect(resourcesHaveBeenUpdated).To(BeFalse())

			VerifyCollectorResourcesExist(ctx, k8sClient, Dash0OperatorNamespace)
		})
	})

	Describe("when deleting all OpenTelemetry collector resources", func() {
		It("should delete the resources", func() {
			// create resources (so there is something to delete)
			_, _, err := oTelColResourceManager.CreateOrUpdateOpenTelemetryCollectorResources(
				ctx,
				util.Images{},
				Dash0OperatorNamespace,
				IngressEndpoint,
				AuthorizationToken,
				SecretRefEmpty,
				&logger,
			)
			Expect(err).ToNot(HaveOccurred())
			VerifyCollectorResourcesExist(ctx, k8sClient, Dash0OperatorNamespace)

			// delete everything again
			err = oTelColResourceManager.DeleteResources(
				ctx,
				util.Images{},
				Dash0OperatorNamespace,
				IngressEndpoint,
				AuthorizationToken,
				SecretRefEmpty,
				&logger,
			)
			Expect(err).ToNot(HaveOccurred())

			VerifyCollectorResourcesDoNotExist(ctx, k8sClient, Dash0OperatorNamespace)
		})
	})
})

func verifyObject(ctx context.Context, testObject *corev1.ConfigMap) {
	object := &corev1.ConfigMap{}
	err := k8sClient.Get(ctx, client.ObjectKeyFromObject(testObject), object)
	Expect(err).ToNot(HaveOccurred())
	Expect(object.Name).To(Equal(testObject.Name))
	Expect(object.Namespace).To(Equal(testObject.Namespace))
	Expect(object.Labels).To(Equal(testObject.Labels))
	Expect(object.Data).To(Equal(testObject.Data))
}
