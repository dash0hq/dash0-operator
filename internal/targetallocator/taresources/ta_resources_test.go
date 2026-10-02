// SPDX-FileCopyrightText: Copyright 2026 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package taresources

import (
	"context"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
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
})
