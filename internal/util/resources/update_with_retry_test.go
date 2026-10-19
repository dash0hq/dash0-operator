// SPDX-FileCopyrightText: Copyright 2026 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package resources

import (
	"context"
	"errors"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/util/retry"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("UpdateWithRetryOnConflict", func() {
	ctx := context.Background()
	conflict := apierrors.NewConflict(
		schema.GroupResource{Resource: "configmaps"},
		"config-map",
		errors.New("the object has been modified"),
	)

	var k8sClient client.Client

	configMap := func() *corev1.ConfigMap {
		return &corev1.ConfigMap{
			TypeMeta:   metav1.TypeMeta{Kind: "ConfigMap", APIVersion: "v1"},
			ObjectMeta: metav1.ObjectMeta{Name: "config-map", Namespace: "namespace"},
			Data:       map[string]string{"key": "value"},
		}
	}

	BeforeEach(func() {
		k8sClient = fake.NewClientBuilder().WithScheme(clientgoscheme.Scheme).WithObjects(configMap()).Build()
	})

	It("retries a conflicting update with a fresh read and an untouched copy of the desired object", func() {
		var existingValues, desiredValues []string
		hasChanged, err := UpdateWithRetryOnConflict(ctx, k8sClient, configMap(), func(
			existingResource client.Object,
			desiredResource client.Object,
		) (bool, error) {
			existingValues = append(existingValues, existingResource.(*corev1.ConfigMap).Data["key"])
			desiredValues = append(desiredValues, desiredResource.(*corev1.ConfigMap).Data["key"])
			if len(existingValues) > 1 {
				return true, nil
			}
			desiredResource.(*corev1.ConfigMap).Data["key"] = "modified by the first attempt"
			concurrentlyModified := existingResource.DeepCopyObject().(*corev1.ConfigMap)
			concurrentlyModified.Data["key"] = "concurrently modified"
			Expect(k8sClient.Update(ctx, concurrentlyModified)).To(Succeed())
			return false, conflict
		})

		Expect(err).ToNot(HaveOccurred())
		Expect(hasChanged).To(BeTrue())
		Expect(existingValues).To(Equal([]string{"value", "concurrently modified"}))
		Expect(desiredValues).To(Equal([]string{"value", "value"}))
	})

	It("does not retry errors other than conflicts", func() {
		calls := 0
		_, err := UpdateWithRetryOnConflict(ctx, k8sClient, configMap(), func(client.Object, client.Object) (bool, error) {
			calls++
			return false, errors.New("update failed")
		})

		Expect(err).To(MatchError("update failed"))
		Expect(calls).To(Equal(1))
	})

	It("gives up after the last attempt", func() {
		calls := 0
		_, err := UpdateWithRetryOnConflict(ctx, k8sClient, configMap(), func(client.Object, client.Object) (bool, error) {
			calls++
			return false, conflict
		})

		Expect(apierrors.IsConflict(err)).To(BeTrue())
		Expect(calls).To(Equal(retry.DefaultBackoff.Steps))
	})

	It("returns the error of reading the existing object without attempting an update", func() {
		k8sClient = fake.NewClientBuilder().WithScheme(clientgoscheme.Scheme).Build()
		calls := 0
		_, err := UpdateWithRetryOnConflict(ctx, k8sClient, configMap(), func(client.Object, client.Object) (bool, error) {
			calls++
			return true, nil
		})

		Expect(apierrors.IsNotFound(err)).To(BeTrue())
		Expect(calls).To(Equal(0))
	})
})
