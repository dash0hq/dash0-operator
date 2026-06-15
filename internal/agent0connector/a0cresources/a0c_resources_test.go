// SPDX-FileCopyrightText: Copyright 2026 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package a0cresources

import (
	"context"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	dash0common "github.com/dash0hq/dash0-operator/api/operator/common"
	"github.com/dash0hq/dash0-operator/internal/util"
	"github.com/dash0hq/dash0-operator/internal/util/logd"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	. "github.com/dash0hq/dash0-operator/test/util"
)

var (
	agent0ConnectorAuthToken = "agent0-connector-auth-token"

	agent0ConnectorTestResource = &corev1.ConfigMap{
		TypeMeta: metav1.TypeMeta{
			Kind:       "ConfigMap",
			APIVersion: "v1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-config-map",
			Namespace: OperatorNamespace,
			Labels: map[string]string{
				"label": "value",
			},
		},
		Data: map[string]string{
			"key": "value",
		},
	}
)

var _ = Describe("The agent0-connector resource manager", Ordered, func() {
	ctx := context.Background()
	logger := logd.FromContext(ctx)

	var manager *Agent0ConnectorResourceManager

	BeforeAll(func() {
		EnsureOperatorNamespaceExists(ctx, k8sClient)
	})

	BeforeEach(func() {
		manager = newAgent0ConnectorResourceManager(dash0common.Authorization{Token: &agent0ConnectorAuthToken})
	})

	AfterEach(func() {
		_, err := manager.DeleteResources(ctx, logger)
		Expect(err).ToNot(HaveOccurred())
		Eventually(func(g Gomega) {
			verifyAgent0ConnectorResourcesDoNotExist(ctx, g)
		}, 500*time.Millisecond, 20*time.Millisecond).Should(Succeed())
		Expect(k8sClient.DeleteAllOf(ctx, &corev1.ConfigMap{}, client.InNamespace(OperatorNamespace))).To(Succeed())
	})

	Context("when dealing with individual resources", func() {
		It("should create a single resource", func() {
			isNew, isChanged, err := manager.createOrUpdateResource(ctx, agent0ConnectorTestResource.DeepCopy(), logger)
			Expect(err).ToNot(HaveOccurred())
			Expect(isNew).To(BeTrue())
			Expect(isChanged).To(BeFalse())
			verifyConfigMap(ctx, agent0ConnectorTestResource)
		})

		It("should update a single object", func() {
			err := manager.createResource(ctx, agent0ConnectorTestResource.DeepCopy(), logger)
			Expect(err).ToNot(HaveOccurred())

			updated := agent0ConnectorTestResource.DeepCopy()
			updated.Data["key"] = "updated value"
			isNew, isChanged, err := manager.createOrUpdateResource(ctx, updated, logger)

			Expect(err).ToNot(HaveOccurred())
			Expect(isNew).To(BeFalse())
			Expect(isChanged).To(BeTrue())
			verifyConfigMap(ctx, updated)
		})

		It("should report that nothing has changed for a single object", func() {
			err := manager.createResource(ctx, agent0ConnectorTestResource.DeepCopy(), logger)
			Expect(err).ToNot(HaveOccurred())

			isNew, isChanged, err := manager.createOrUpdateResource(ctx, agent0ConnectorTestResource.DeepCopy(), logger)

			Expect(err).ToNot(HaveOccurred())
			Expect(isNew).To(BeFalse())
			Expect(isChanged).To(BeFalse())
			verifyConfigMap(ctx, agent0ConnectorTestResource)
		})
	})

	Context("when creating all agent0-connector resources", func() {
		It("should create the service account, cluster role, cluster role binding, and deployment", func() {
			created, updated, err := manager.CreateOrUpdateAgent0ConnectorResources(ctx, logger)
			Expect(err).ToNot(HaveOccurred())
			Expect(created).To(BeTrue())
			Expect(updated).To(BeFalse())

			verifyAgent0ConnectorResourcesExist(ctx)
		})
	})

	Context("when resolving the authorization for the agent0-connector workload", func() {
		It("passes a literal token as the DASH0_AGENT0_CONNECTOR_AUTH_TOKEN environment variable", func() {
			created, _, err := manager.CreateOrUpdateAgent0ConnectorResources(ctx, logger)
			Expect(err).ToNot(HaveOccurred())
			Expect(created).To(BeTrue())

			container := getDeployedAgent0ConnectorContainer(ctx)
			Expect(container.Env).To(ContainElement(
				corev1.EnvVar{Name: authTokenEnvVarName, Value: agent0ConnectorAuthToken}))
		})

		It("resolves a secret ref into the DASH0_AGENT0_CONNECTOR_AUTH_TOKEN environment variable", func() {
			manager = newAgent0ConnectorResourceManager(dash0common.Authorization{
				SecretRef: &dash0common.SecretRef{
					Name: "dash0-agent0-connector-authorization-secret",
					Key:  "token",
				},
			})
			created, _, err := manager.CreateOrUpdateAgent0ConnectorResources(ctx, logger)
			Expect(err).ToNot(HaveOccurred())
			Expect(created).To(BeTrue())

			container := getDeployedAgent0ConnectorContainer(ctx)
			tokenEnvVar := util.GetEnvVar(&container, authTokenEnvVarName)
			Expect(tokenEnvVar).ToNot(BeNil())
			Expect(tokenEnvVar.Value).To(BeEmpty())
			Expect(tokenEnvVar.ValueFrom).ToNot(BeNil())
			Expect(tokenEnvVar.ValueFrom.SecretKeyRef).ToNot(BeNil())
			Expect(tokenEnvVar.ValueFrom.SecretKeyRef.Name).To(Equal("dash0-agent0-connector-authorization-secret"))
			Expect(tokenEnvVar.ValueFrom.SecretKeyRef.Key).To(Equal("token"))
		})

		It("aborts and returns an error when no authorization token is available", func() {
			// Authorization is deliberately left unset (neither token nor secretRef).
			manager = newAgent0ConnectorResourceManager(dash0common.Authorization{})

			created, updated, err := manager.CreateOrUpdateAgent0ConnectorResources(ctx, logger)

			Expect(err).To(HaveOccurred())
			Expect(created).To(BeFalse())
			Expect(updated).To(BeFalse())
			verifyAgent0ConnectorResourcesDoNotExist(ctx, Default)
		})
	})

	Context("when agent0-connector resources have been modified externally", func() {
		It("should reconcile the resources back into the desired state", func() {
			created, _, err := manager.CreateOrUpdateAgent0ConnectorResources(ctx, logger)
			Expect(err).ToNot(HaveOccurred())
			Expect(created).To(BeTrue())

			// Change an arbitrary field, then simulate a reconcile cycle and verify the resource is back in its
			// desired state.
			deployment := &appsv1.Deployment{}
			Expect(k8sClient.Get(
				ctx,
				client.ObjectKey{Name: DeploymentName(testNamePrefix), Namespace: OperatorNamespace},
				deployment,
			)).To(Succeed())
			var changedReplicas int32 = 5
			deployment.Spec.Replicas = &changedReplicas
			Expect(k8sClient.Update(ctx, deployment)).To(Succeed())

			created, updated, err := manager.CreateOrUpdateAgent0ConnectorResources(ctx, logger)
			Expect(err).ToNot(HaveOccurred())
			Expect(created).To(BeFalse())
			Expect(updated).To(BeTrue())

			reconciled := &appsv1.Deployment{}
			Expect(k8sClient.Get(
				ctx,
				client.ObjectKey{Name: DeploymentName(testNamePrefix), Namespace: OperatorNamespace},
				reconciled,
			)).To(Succeed())
			Expect(*reconciled.Spec.Replicas).To(Equal(int32(1)))
		})
	})

	Context("when agent0-connector resources have been deleted externally", func() {
		It("should re-create the resources", func() {
			created, _, err := manager.CreateOrUpdateAgent0ConnectorResources(ctx, logger)
			Expect(err).ToNot(HaveOccurred())
			Expect(created).To(BeTrue())

			serviceAccount := &corev1.ServiceAccount{}
			Expect(k8sClient.Get(
				ctx,
				client.ObjectKey{Name: ServiceAccountName(testNamePrefix), Namespace: OperatorNamespace},
				serviceAccount,
			)).To(Succeed())
			Expect(k8sClient.Delete(ctx, serviceAccount)).To(Succeed())

			created, _, err = manager.CreateOrUpdateAgent0ConnectorResources(ctx, logger)
			Expect(err).ToNot(HaveOccurred())
			Expect(created).To(BeTrue())

			VerifyResourceExists(
				ctx,
				k8sClient,
				OperatorNamespace,
				ServiceAccountName(testNamePrefix),
				&corev1.ServiceAccount{},
			)
		})
	})

	Context("when all agent0-connector resources are up to date", func() {
		It("should report that nothing has changed", func() {
			created, updated, err := manager.CreateOrUpdateAgent0ConnectorResources(ctx, logger)
			Expect(err).ToNot(HaveOccurred())
			Expect(created).To(BeTrue())
			Expect(updated).To(BeFalse())

			created, updated, err = manager.CreateOrUpdateAgent0ConnectorResources(ctx, logger)
			Expect(err).ToNot(HaveOccurred())
			Expect(created).To(BeFalse())
			Expect(updated).To(BeFalse())

			verifyAgent0ConnectorResourcesExist(ctx)
		})
	})

	Context("when deleting all agent0-connector resources", func() {
		It("should delete the resources", func() {
			_, _, err := manager.CreateOrUpdateAgent0ConnectorResources(ctx, logger)
			Expect(err).ToNot(HaveOccurred())
			verifyAgent0ConnectorResourcesExist(ctx)

			deleted, err := manager.DeleteResources(ctx, logger)
			Expect(err).ToNot(HaveOccurred())
			Expect(deleted).To(BeTrue())

			verifyAgent0ConnectorResourcesDoNotExist(ctx, Default)

			// Deletion must be idempotent: deleting again must not error, but must report that nothing was deleted.
			deleted, err = manager.DeleteResources(ctx, logger)
			Expect(err).ToNot(HaveOccurred())
			Expect(deleted).To(BeFalse())
		})
	})
})

func newAgent0ConnectorResourceManager(authorization dash0common.Authorization) *Agent0ConnectorResourceManager {
	return NewAgent0ConnectorResourceManager(
		k8sClient,
		k8sClient.Scheme(),
		OperatorManagerDeployment,
		util.Agent0ConnectorConfig{
			Images: util.Images{
				Agent0ConnectorImage:           testImage,
				Agent0ConnectorImagePullPolicy: corev1.PullAlways,
			},
			OperatorNamespace: OperatorNamespace,
			NamePrefix:        testNamePrefix,
			PseudoClusterUid:  testPseudoClusterUid,
			ServerAddress:     Agent0ConnectorServerAddress,
			Authorization:     authorization,
			DevelopmentMode:   true,
		},
	)
}

func getDeployedAgent0ConnectorContainer(ctx context.Context) corev1.Container {
	GinkgoHelper()
	deployment := &appsv1.Deployment{}
	Expect(k8sClient.Get(
		ctx,
		client.ObjectKey{Name: DeploymentName(testNamePrefix), Namespace: OperatorNamespace},
		deployment,
	)).To(Succeed())
	Expect(deployment.Spec.Template.Spec.Containers).To(HaveLen(1))
	return deployment.Spec.Template.Spec.Containers[0]
}

func verifyAgent0ConnectorResourcesExist(ctx context.Context) {
	GinkgoHelper()
	VerifyResourceExists(ctx, k8sClient, OperatorNamespace, ServiceAccountName(testNamePrefix), &corev1.ServiceAccount{})
	VerifyResourceExists(ctx, k8sClient, "", ClusterRoleName(testNamePrefix), &rbacv1.ClusterRole{})
	VerifyResourceExists(ctx, k8sClient, "", ClusterRoleBindingName(testNamePrefix), &rbacv1.ClusterRoleBinding{})
	VerifyResourceExists(ctx, k8sClient, OperatorNamespace, DeploymentName(testNamePrefix), &appsv1.Deployment{})
}

func verifyAgent0ConnectorResourcesDoNotExist(ctx context.Context, g Gomega) {
	g.Expect(k8sClient.Get(
		ctx,
		client.ObjectKey{Name: ServiceAccountName(testNamePrefix), Namespace: OperatorNamespace},
		&corev1.ServiceAccount{},
	)).To(MatchError(ContainSubstring("not found")))
	g.Expect(k8sClient.Get(
		ctx,
		client.ObjectKey{Name: ClusterRoleName(testNamePrefix)},
		&rbacv1.ClusterRole{},
	)).To(MatchError(ContainSubstring("not found")))
	g.Expect(k8sClient.Get(
		ctx,
		client.ObjectKey{Name: ClusterRoleBindingName(testNamePrefix)},
		&rbacv1.ClusterRoleBinding{},
	)).To(MatchError(ContainSubstring("not found")))
	g.Expect(k8sClient.Get(
		ctx,
		client.ObjectKey{Name: DeploymentName(testNamePrefix), Namespace: OperatorNamespace},
		&appsv1.Deployment{},
	)).To(MatchError(ContainSubstring("not found")))
}

func verifyConfigMap(ctx context.Context, testObject *corev1.ConfigMap) {
	GinkgoHelper()
	object := &corev1.ConfigMap{}
	err := k8sClient.Get(ctx, client.ObjectKeyFromObject(testObject), object)
	Expect(err).ToNot(HaveOccurred())
	Expect(object.Name).To(Equal(testObject.Name))
	Expect(object.Namespace).To(Equal(testObject.Namespace))
	Expect(object.Labels).To(Equal(testObject.Labels))
	Expect(object.Data).To(Equal(testObject.Data))
}
