// SPDX-FileCopyrightText: Copyright 2024 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package otelcolresources

import (
	"context"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	. "github.com/dash0hq/dash0-operator/test/util"
)

var (
	namespace             = TestNamespaceName
	expectedConfigMapName = "unit-test-opentelemetry-collector-agent"

	testObject = &corev1.ConfigMap{
		TypeMeta: metav1.TypeMeta{
			Kind:       "ConfigMap",
			APIVersion: "v1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-config-map",
			Namespace: namespace,
			Labels: map[string]string{
				"label": "value",
			},
		},
		Data: map[string]string{
			"key": "value",
		},
	}
)

var _ = Describe("The BackendConnection Controller", Ordered, func() {
	ctx := context.Background()
	logger := log.FromContext(ctx)

	var oTelColResourceManager *OTelColResourceManager

	BeforeAll(func() {
		EnsureTestNamespaceExists(ctx, k8sClient)
	})

	BeforeEach(func() {
		oTelColResourceManager = &OTelColResourceManager{
			Client:                  k8sClient,
			OTelCollectorNamePrefix: "unit-test",
		}
	})

	AfterEach(func() {
		err := k8sClient.DeleteAllOf(ctx, &corev1.ConfigMap{}, client.InNamespace(namespace))
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
					namespace,
					"ingress.endpoint.dash0.com:4317",
					"authorization-token",
					"secret-ref",
					&logger,
				)
			Expect(err).ToNot(HaveOccurred())
			Expect(resourcesHaveBeenCreated).To(BeTrue())
			Expect(resourcesHaveBeenUpdated).To(BeFalse())
			verifyConfigMap(ctx)
		})
	})

	Describe("when updating all OpenTelemetry collector resources", func() {
		It("should update the resources", func() {
			err := k8sClient.Create(ctx, &corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Name:      expectedConfigMapName,
					Namespace: namespace,
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
					namespace,
					"ingress.endpoint.dash0.com:4317",
					"authorization-token",
					"secret-ref",
					&logger,
				)
			Expect(err).ToNot(HaveOccurred())
			Expect(resourcesHaveBeenCreated).To(BeFalse())
			Expect(resourcesHaveBeenUpdated).To(BeTrue())
			verifyConfigMap(ctx)
		})
	})

	Describe("when all OpenTelemetry collector resources are up to date", func() {
		It("should report that nothing has changed", func() {
			// create resources (so we are sure that everything is in the desired state)
			_, _, err := oTelColResourceManager.CreateOrUpdateOpenTelemetryCollectorResources(
				ctx,
				namespace,
				"ingress.endpoint.dash0.com:4317",
				"authorization-token",
				"secret-ref",
				&logger,
			)
			Expect(err).ToNot(HaveOccurred())

			// now run another create/update, to make sure resourcesHaveBeenCreated/resourcesHaveBeenUpdated come back
			// as false
			resourcesHaveBeenCreated, resourcesHaveBeenUpdated, err :=
				oTelColResourceManager.CreateOrUpdateOpenTelemetryCollectorResources(
					ctx,
					namespace,
					"ingress.endpoint.dash0.com:4317",
					"authorization-token",
					"secret-ref",
					&logger,
				)
			Expect(err).ToNot(HaveOccurred())
			Expect(resourcesHaveBeenCreated).To(BeFalse())
			Expect(resourcesHaveBeenUpdated).To(BeFalse())
			verifyConfigMap(ctx)
		})
	})

	Describe("when deleting all OpenTelemetry collector resources", func() {
		It("should delete the resources", func() {
			// create resources (so there is something to delete)
			_, _, err := oTelColResourceManager.CreateOrUpdateOpenTelemetryCollectorResources(
				ctx,
				namespace,
				"ingress.endpoint.dash0.com:4317",
				"authorization-token",
				"secret-ref",
				&logger,
			)
			Expect(err).ToNot(HaveOccurred())

			// delete everything again
			err = oTelColResourceManager.DeleteResources(
				ctx,
				namespace,
				"ingress.endpoint.dash0.com:4317",
				"authorization-token",
				"secret-ref",
				&logger,
			)
			Expect(err).ToNot(HaveOccurred())

			verifyNoConfigMapExists(ctx)
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

func verifyConfigMap(ctx context.Context) {
	key := client.ObjectKey{Name: expectedConfigMapName, Namespace: namespace}
	cm := &corev1.ConfigMap{}
	err := k8sClient.Get(ctx, key, cm)
	Expect(err).ToNot(HaveOccurred())
	Expect(cm.Labels).ToNot(HaveKey("wrong-key"))
	Expect(cm.Annotations).ToNot(HaveKey("wrong-key"))
	Expect(cm.Data).To(HaveKey("collector.yaml"))
	Expect(cm.Data).ToNot(HaveKey("wrong-key"))
}

func verifyNoConfigMapExists(ctx context.Context) {
	key := client.ObjectKey{Name: expectedConfigMapName, Namespace: namespace}
	cm := &corev1.ConfigMap{}
	err := k8sClient.Get(ctx, key, cm)
	Expect(err).To(HaveOccurred(), "the config map still exists although it should have been deleted")
	Expect(apierrors.IsNotFound(err)).To(BeTrue(),
		fmt.Sprintf("loading the config map failed with an unexpected error: %v", err))
}
