// SPDX-FileCopyrightText: Copyright 2026 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package scresources

import (
	"context"

	"github.com/cisco-open/k8s-objectmatcher/patch"
	corev1 "k8s.io/api/core/v1"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	"github.com/dash0hq/dash0-operator/internal/util"
	"github.com/dash0hq/dash0-operator/internal/util/cluster"
	"github.com/dash0hq/dash0-operator/internal/util/logd"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	. "github.com/dash0hq/dash0-operator/test/util"
)

const testNamePrefix = "test-prefix"

var _ = Describe("The signal control resource manager", func() {
	ctx := context.Background()
	logger := logd.FromContext(ctx)

	var k8sClient client.Client
	var resourceManager *SignalControlResourceManager

	BeforeEach(func() {
		k8sClient = fake.NewClientBuilder().WithScheme(clientgoscheme.Scheme).Build()
		resourceManager = NewSignalControlResourceManager(
			k8sClient,
			clientgoscheme.Scheme,
			OperatorManagerDeployment,
			OperatorNamespace,
			testNamePrefix,
			"edge-proxy:latest",
			corev1.PullIfNotPresent,
			testOperatorVersion,
			testOtlpGrpcHostPort,
			cluster.KubernetesVersionInfo{},
			false,
			false,
		)
	})

	desiredObjects := func() []client.Object {
		desiredState := assembleDesiredState(
			OperatorNamespace, testNamePrefix, minimalSignalControl, DefaultOperatorConfigurationResourceWithUid(),
			"edge-proxy:latest", corev1.PullIfNotPresent, testOperatorVersion, testOtlpGrpcHostPort,
			cluster.KubernetesVersionInfo{}, util.ExtraConfig{}, false, false, false, logger,
		)
		objects := make([]client.Object, 0, len(desiredState))
		for _, wrapper := range desiredState {
			objects = append(objects, wrapper.object)
		}
		Expect(objects).ToNot(BeEmpty())
		return objects
	}

	verifyOwnedByOperatorManagerDeployment := func(desired client.Object) {
		object := desired.DeepCopyObject().(client.Object)
		Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(desired), object)).To(Succeed())
		ownerReferences := object.GetOwnerReferences()
		Expect(ownerReferences).To(HaveLen(1))
		Expect(ownerReferences[0].Kind).To(Equal("Deployment"))
		Expect(ownerReferences[0].Name).To(Equal(OperatorManagerDeploymentName))
		Expect(ownerReferences[0].UID).To(Equal(OperatorManagerDeploymentUID))
		Expect(ownerReferences[0].Controller).To(BeNil())
		Expect(*ownerReferences[0].BlockOwnerDeletion).To(BeTrue())
	}

	It("should make the operator manager deployment the owner of all resources", func() {
		resourcesHaveBeenCreated, _, err := resourceManager.CreateOrUpdateResources(
			ctx,
			minimalSignalControl,
			DefaultOperatorConfigurationResourceWithUid(),
			util.ExtraConfig{},
			logger,
		)
		Expect(err).ToNot(HaveOccurred())
		Expect(resourcesHaveBeenCreated).To(BeTrue())

		for _, desired := range desiredObjects() {
			verifyOwnedByOperatorManagerDeployment(desired)
		}
	})

	It("should add the owner reference to existing resources that do not have one", func() {
		existingService := assembleEdgeProxyServiceForDeletion(OperatorNamespace, testNamePrefix)
		Expect(patch.DefaultAnnotator.SetLastAppliedAnnotation(existingService)).To(Succeed())
		Expect(k8sClient.Create(ctx, existingService)).To(Succeed())

		_, resourcesHaveBeenUpdated, err := resourceManager.CreateOrUpdateResources(
			ctx,
			minimalSignalControl,
			DefaultOperatorConfigurationResourceWithUid(),
			util.ExtraConfig{},
			logger,
		)
		Expect(err).ToNot(HaveOccurred())
		Expect(resourcesHaveBeenUpdated).To(BeTrue())

		verifyOwnedByOperatorManagerDeployment(existingService)
	})
})
