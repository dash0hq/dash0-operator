// SPDX-FileCopyrightText: Copyright 2026 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package taresources

import (
	"context"

	"github.com/cisco-open/k8s-objectmatcher/patch"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	dash0v1alpha1 "github.com/dash0hq/dash0-operator/api/operator/v1alpha1"
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
		scheme := runtime.NewScheme()
		Expect(clientgoscheme.AddToScheme(scheme)).To(Succeed())
		Expect(dash0v1alpha1.AddToScheme(scheme)).To(Succeed())
		k8sClient = fake.NewClientBuilder().WithScheme(scheme).Build()
		resourceManager = NewTargetAllocatorResourceManager(
			k8sClient,
			scheme,
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
			ctx, extraConfigWithMemory("500Mi"), nil, []string{"namespace"}, logger)
		Expect(err).ToNot(HaveOccurred())
		Expect(resourcesHaveBeenCreated).To(BeTrue())

		deployment := getDeployment()
		SimulateGkeAutopilotResourceAdjustment(deployment, &deployment.Spec.Template.Spec)
		Expect(k8sClient.Update(ctx, deployment)).To(Succeed())

		_, resourcesHaveBeenUpdated, err := resourceManager.CreateOrUpdateTargetAllocatorResources(
			ctx, extraConfigWithMemory("500Mi"), nil, []string{"namespace"}, logger)
		Expect(err).ToNot(HaveOccurred())
		Expect(resourcesHaveBeenUpdated).To(BeFalse())
		Expect(getDeployment().Spec.Template.Spec).To(Equal(deployment.Spec.Template.Spec))

		_, resourcesHaveBeenUpdated, err = resourceManager.CreateOrUpdateTargetAllocatorResources(
			ctx, extraConfigWithMemory("1Gi"), nil, []string{"namespace"}, logger)
		Expect(err).ToNot(HaveOccurred())
		Expect(resourcesHaveBeenUpdated).To(BeTrue())
		containerResources := getDeployment().Spec.Template.Spec.Containers[0].Resources
		Expect(containerResources.Limits.Memory().Equal(resource.MustParse("1Gi"))).To(BeTrue())
		Expect(containerResources.Requests.Memory().Equal(resource.MustParse("1Gi"))).To(BeTrue())
	})

	It("should make the operator configuration resource the owner of cluster-scoped resources", func() {
		_, _, err := resourceManager.CreateOrUpdateTargetAllocatorResources(
			ctx, util.ExtraConfigDefaults, DefaultOperatorConfigurationResourceWithUid(), []string{"namespace"}, logger)
		Expect(err).ToNot(HaveOccurred())

		for _, object := range []client.Object{
			&rbacv1.ClusterRole{ObjectMeta: metav1.ObjectMeta{Name: ClusterRoleName(TargetAllocatorPrefixTest)}},
			&rbacv1.ClusterRoleBinding{ObjectMeta: metav1.ObjectMeta{
				Name: ClusterRoleBindingName(TargetAllocatorPrefixTest),
			}},
		} {
			Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(object), object)).To(Succeed())
			ownerReferences := object.GetOwnerReferences()
			Expect(ownerReferences).To(HaveLen(1))
			Expect(ownerReferences[0].Kind).To(Equal("Dash0OperatorConfiguration"))
			Expect(ownerReferences[0].UID).To(Equal(OperatorConfigurationResourceUid))
			Expect(ownerReferences[0].Controller).To(BeNil())
			Expect(*ownerReferences[0].BlockOwnerDeletion).To(BeTrue())
		}

		for _, object := range []client.Object{
			&corev1.ServiceAccount{ObjectMeta: metav1.ObjectMeta{
				Namespace: OperatorNamespace,
				Name:      ServiceAccountName(TargetAllocatorPrefixTest),
			}},
			getDeployment(),
		} {
			Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(object), object)).To(Succeed())
			ownerReferences := object.GetOwnerReferences()
			Expect(ownerReferences).To(HaveLen(1))
			Expect(ownerReferences[0].UID).To(Equal(OperatorManagerDeploymentUID))
		}
	})

	It("should add the owner reference to existing cluster-scoped resources that do not have one", func() {
		clusterRoleName := ClusterRoleName(TargetAllocatorPrefixTest)
		existingClusterRole := &rbacv1.ClusterRole{ObjectMeta: metav1.ObjectMeta{Name: clusterRoleName}}
		Expect(patch.DefaultAnnotator.SetLastAppliedAnnotation(existingClusterRole)).To(Succeed())
		Expect(k8sClient.Create(ctx, existingClusterRole)).To(Succeed())

		_, resourcesHaveBeenUpdated, err := resourceManager.CreateOrUpdateTargetAllocatorResources(
			ctx, util.ExtraConfigDefaults, DefaultOperatorConfigurationResourceWithUid(), []string{"namespace"}, logger)
		Expect(err).ToNot(HaveOccurred())
		Expect(resourcesHaveBeenUpdated).To(BeTrue())

		clusterRole := &rbacv1.ClusterRole{}
		Expect(k8sClient.Get(ctx, client.ObjectKey{Name: clusterRoleName}, clusterRole)).To(Succeed())
		Expect(clusterRole.GetOwnerReferences()).To(HaveLen(1))
		Expect(clusterRole.GetOwnerReferences()[0].UID).To(Equal(OperatorConfigurationResourceUid))
	})

	It("should keep the kubectl rollout restart annotation, also when applying changed resource settings", func() {
		resourcesHaveBeenCreated, _, err := resourceManager.CreateOrUpdateTargetAllocatorResources(
			ctx, extraConfigWithMemory("500Mi"), nil, []string{"namespace"}, logger)
		Expect(err).ToNot(HaveOccurred())
		Expect(resourcesHaveBeenCreated).To(BeTrue())

		deployment := getDeployment()
		SimulateKubectlRolloutRestart(&deployment.Spec.Template)
		Expect(k8sClient.Update(ctx, deployment)).To(Succeed())

		_, resourcesHaveBeenUpdated, err := resourceManager.CreateOrUpdateTargetAllocatorResources(
			ctx, extraConfigWithMemory("500Mi"), nil, []string{"namespace"}, logger)
		Expect(err).ToNot(HaveOccurred())
		Expect(resourcesHaveBeenUpdated).To(BeFalse())
		Expect(getDeployment().Spec.Template.Annotations).To(
			HaveKeyWithValue(KubectlRestartedAtAnnotationTest, KubectlRestartedAtTest))

		_, resourcesHaveBeenUpdated, err = resourceManager.CreateOrUpdateTargetAllocatorResources(
			ctx, extraConfigWithMemory("1Gi"), nil, []string{"namespace"}, logger)
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
