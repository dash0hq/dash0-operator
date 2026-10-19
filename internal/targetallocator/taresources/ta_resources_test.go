// SPDX-FileCopyrightText: Copyright 2026 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package taresources

import (
	"context"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	"github.com/dash0hq/dash0-operator/internal/util"
	"github.com/dash0hq/dash0-operator/internal/util/logd"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	. "github.com/dash0hq/dash0-operator/test/util"
)

var _ = Describe("The target-allocator resource manager", func() {
	ctx := context.Background()
	logger := logd.FromContext(ctx)

	var k8sClient client.Client
	var resourceManager *TargetAllocatorResourceManager

	extraConfigWithMemory := func(memory string) util.ExtraConfig {
		return util.ExtraConfig{
			TargetAllocatorContainerResources: util.ResourceRequirementsWithGoMemLimit{
				Limits:   corev1.ResourceList{corev1.ResourceMemory: resource.MustParse(memory)},
				Requests: corev1.ResourceList{corev1.ResourceMemory: resource.MustParse(memory)},
			},
		}
	}

	getDeployment := func() *appsv1.Deployment {
		deployment := &appsv1.Deployment{}
		Expect(k8sClient.Get(ctx, types.NamespacedName{
			Namespace: OperatorNamespace,
			Name:      DeploymentName(TargetAllocatorPrefixTest),
		}, deployment)).To(Succeed())
		return deployment
	}

	BeforeEach(func() {
		k8sClient = fake.NewClientBuilder().WithScheme(clientgoscheme.Scheme).Build()
		resourceManager = NewTargetAllocatorResourceManager(
			k8sClient,
			clientgoscheme.Scheme,
			OperatorManagerDeployment,
			util.TargetAllocatorConfig{
				Images:                    TestImages,
				OperatorNamespace:         OperatorNamespace,
				TargetAllocatorNamePrefix: TargetAllocatorPrefixTest,
				IsGkeAutopilot:            true,
			},
		)
	})

	It("should not revert GKE Autopilot resource adjustments, but still apply changed resource settings", func() {
		resourcesHaveBeenCreated, _, err := resourceManager.CreateOrUpdateTargetAllocatorResources(
			ctx, extraConfigWithMemory("500Mi"), []string{"namespace"}, logger)
		Expect(err).ToNot(HaveOccurred())
		Expect(resourcesHaveBeenCreated).To(BeTrue())

		deployment := getDeployment()
		SimulateGkeAutopilotResourceAdjustment(deployment, &deployment.Spec.Template.Spec)
		Expect(k8sClient.Update(ctx, deployment)).To(Succeed())

		_, resourcesHaveBeenUpdated, err := resourceManager.CreateOrUpdateTargetAllocatorResources(
			ctx, extraConfigWithMemory("500Mi"), []string{"namespace"}, logger)
		Expect(err).ToNot(HaveOccurred())
		Expect(resourcesHaveBeenUpdated).To(BeFalse())
		Expect(getDeployment().Spec.Template.Spec).To(Equal(deployment.Spec.Template.Spec))

		_, resourcesHaveBeenUpdated, err = resourceManager.CreateOrUpdateTargetAllocatorResources(
			ctx, extraConfigWithMemory("1Gi"), []string{"namespace"}, logger)
		Expect(err).ToNot(HaveOccurred())
		Expect(resourcesHaveBeenUpdated).To(BeTrue())
		containerResources := getDeployment().Spec.Template.Spec.Containers[0].Resources
		Expect(containerResources.Limits.Memory().Equal(resource.MustParse("1Gi"))).To(BeTrue())
		Expect(containerResources.Requests.Memory().Equal(resource.MustParse("1Gi"))).To(BeTrue())
	})

	It("should keep the kubectl rollout restart annotation, also when applying changed resource settings", func() {
		resourcesHaveBeenCreated, _, err := resourceManager.CreateOrUpdateTargetAllocatorResources(
			ctx, extraConfigWithMemory("500Mi"), []string{"namespace"}, logger)
		Expect(err).ToNot(HaveOccurred())
		Expect(resourcesHaveBeenCreated).To(BeTrue())

		deployment := getDeployment()
		SimulateKubectlRolloutRestart(&deployment.Spec.Template)
		Expect(k8sClient.Update(ctx, deployment)).To(Succeed())

		_, resourcesHaveBeenUpdated, err := resourceManager.CreateOrUpdateTargetAllocatorResources(
			ctx, extraConfigWithMemory("500Mi"), []string{"namespace"}, logger)
		Expect(err).ToNot(HaveOccurred())
		Expect(resourcesHaveBeenUpdated).To(BeFalse())
		Expect(getDeployment().Spec.Template.Annotations).To(
			HaveKeyWithValue(KubectlRestartedAtAnnotationTest, KubectlRestartedAtTest))

		_, resourcesHaveBeenUpdated, err = resourceManager.CreateOrUpdateTargetAllocatorResources(
			ctx, extraConfigWithMemory("1Gi"), []string{"namespace"}, logger)
		Expect(err).ToNot(HaveOccurred())
		Expect(resourcesHaveBeenUpdated).To(BeTrue())
		reconciled := getDeployment()
		Expect(reconciled.Spec.Template.Spec.Containers[0].Resources.Limits.Memory().Equal(
			resource.MustParse("1Gi"))).To(BeTrue())
		Expect(reconciled.Spec.Template.Annotations).To(
			HaveKeyWithValue(KubectlRestartedAtAnnotationTest, KubectlRestartedAtTest))
	})

	It("should retry an update that conflicts with a concurrent modification", func() {
		configMap := &corev1.ConfigMap{
			TypeMeta:   metav1.TypeMeta{Kind: "ConfigMap", APIVersion: "v1"},
			ObjectMeta: metav1.ObjectMeta{Name: "test-config-map", Namespace: OperatorNamespace},
			Data:       map[string]string{"key": "value"},
		}
		Expect(resourceManager.createResource(ctx, configMap.DeepCopy(), logger)).To(Succeed())

		conflictingClient := &ConflictOnFirstUpdateClient{Client: k8sClient}
		managerWithConflicts := NewTargetAllocatorResourceManager(
			conflictingClient,
			clientgoscheme.Scheme,
			OperatorManagerDeployment,
			resourceManager.targetAllocatorConfig,
		)

		updated := configMap.DeepCopy()
		updated.Data["key"] = "value after a conflict"
		isNew, isChanged, err := managerWithConflicts.createOrUpdateResource(ctx, updated, logger)

		Expect(err).ToNot(HaveOccurred())
		Expect(isNew).To(BeFalse())
		Expect(isChanged).To(BeTrue())
		Expect(conflictingClient.UpdateCalls).To(Equal(2), "expected exactly one retry after the conflict")
		reconciled := &corev1.ConfigMap{}
		Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(configMap), reconciled)).To(Succeed())
		Expect(reconciled.Data).To(Equal(updated.Data))
	})
})
