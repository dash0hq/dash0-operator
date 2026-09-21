// SPDX-FileCopyrightText: Copyright 2026 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package swresources

import (
	"context"
	"errors"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	dash0common "github.com/dash0hq/dash0-operator/api/operator/common"
	dash0v1alpha1 "github.com/dash0hq/dash0-operator/api/operator/v1alpha1"
	"github.com/dash0hq/dash0-operator/internal/util"
	"github.com/dash0hq/dash0-operator/internal/util/logd"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	. "github.com/dash0hq/dash0-operator/test/util"
)

var (
	syntheticsWorkerAuthToken = "synthetics-worker-auth-token"

	syntheticsWorkerTestResource = &corev1.ConfigMap{
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

var _ = Describe("The synthetics-worker resource manager", Ordered, func() {
	ctx := context.Background()
	logger := logd.FromContext(ctx)

	var manager *SyntheticsWorkerResourceManager

	BeforeAll(func() {
		EnsureOperatorNamespaceExists(ctx, k8sClient)
	})

	BeforeEach(func() {
		manager = newSyntheticsWorkerResourceManager()
	})

	AfterEach(func() {
		_, err := manager.DeleteResources(ctx, logger)
		Expect(err).ToNot(HaveOccurred())
		Eventually(func(g Gomega) {
			verifySyntheticsWorkerResourcesDoNotExist(ctx, g)
		}, 500*time.Millisecond, 20*time.Millisecond).Should(Succeed())
		Expect(k8sClient.DeleteAllOf(ctx, &corev1.ConfigMap{}, client.InNamespace(OperatorNamespace))).To(Succeed())
	})

	Context("when dealing with individual resources", func() {
		It("should create a single resource", func() {
			isNew, isChanged, err := manager.createOrUpdateResource(ctx, syntheticsWorkerTestResource.DeepCopy(), logger)
			Expect(err).ToNot(HaveOccurred())
			Expect(isNew).To(BeTrue())
			Expect(isChanged).To(BeFalse())
			verifyConfigMap(ctx, syntheticsWorkerTestResource)
		})

		It("should update a single object", func() {
			err := manager.createResource(ctx, syntheticsWorkerTestResource.DeepCopy(), logger)
			Expect(err).ToNot(HaveOccurred())

			updated := syntheticsWorkerTestResource.DeepCopy()
			updated.Data["key"] = "updated value"
			isNew, isChanged, err := manager.createOrUpdateResource(ctx, updated, logger)

			Expect(err).ToNot(HaveOccurred())
			Expect(isNew).To(BeFalse())
			Expect(isChanged).To(BeTrue())
			verifyConfigMap(ctx, updated)
		})

		It("should report that nothing has changed for a single object", func() {
			err := manager.createResource(ctx, syntheticsWorkerTestResource.DeepCopy(), logger)
			Expect(err).ToNot(HaveOccurred())

			isNew, isChanged, err := manager.createOrUpdateResource(ctx, syntheticsWorkerTestResource.DeepCopy(), logger)

			Expect(err).ToNot(HaveOccurred())
			Expect(isNew).To(BeFalse())
			Expect(isChanged).To(BeFalse())
			verifyConfigMap(ctx, syntheticsWorkerTestResource)
		})
	})

	Context("when creating all synthetics-worker resources", func() {
		It("should create the service account and deployment", func() {
			created, updated, err := manager.CreateOrUpdateSyntheticsWorkerResources(
				ctx,
				operatorConfigurationResourceWithSyntheticsWorker(syntheticsWorkerAuthToken),
				logger,
			)
			Expect(err).ToNot(HaveOccurred())
			Expect(created).To(BeTrue())
			Expect(updated).To(BeFalse())

			verifySyntheticsWorkerResourcesExist(ctx)
		})
	})

	Context("when the Dash0OperatorConfiguration resource is missing", func() {
		It("aborts and returns an error", func() {
			created, updated, err := manager.CreateOrUpdateSyntheticsWorkerResources(ctx, nil, logger)

			Expect(err).To(HaveOccurred())
			Expect(errors.Is(err, ErrMisconfigured)).To(BeTrue())
			Expect(created).To(BeFalse())
			Expect(updated).To(BeFalse())
		})
	})

	Context("when no private location ID is configured", func() {
		It("aborts and returns an error", func() {
			resource := operatorConfigurationResourceWithSyntheticsWorker(syntheticsWorkerAuthToken)
			resource.Spec.SyntheticsWorker.LocationID = ""

			created, updated, err := manager.CreateOrUpdateSyntheticsWorkerResources(ctx, resource, logger)

			Expect(err).To(HaveOccurred())
			Expect(errors.Is(err, ErrNoLocationID)).To(BeTrue())
			Expect(created).To(BeFalse())
			Expect(updated).To(BeFalse())
			verifySyntheticsWorkerResourcesDoNotExist(ctx, Default)
		})
	})

	Context("when resolving the authorization for the synthetics-worker workload", func() {
		It("passes a literal token via the DASH0_SYNTHETICS_WORKER_AUTH_TOKEN environment variable", func() {
			created, _, err := manager.CreateOrUpdateSyntheticsWorkerResources(
				ctx,
				operatorConfigurationResourceWithSyntheticsWorker(syntheticsWorkerAuthToken),
				logger,
			)
			Expect(err).ToNot(HaveOccurred())
			Expect(created).To(BeTrue())

			container := getDeployedSyntheticsWorkerContainer(ctx)
			Expect(container.Env).To(ContainElement(
				corev1.EnvVar{Name: authTokenEnvVarName, Value: syntheticsWorkerAuthToken}))
			Expect(container.Env).To(ContainElement(
				corev1.EnvVar{Name: headersEnvVarName, Value: "authorization=Bearer $(DASH0_SYNTHETICS_WORKER_AUTH_TOKEN)"}))
		})

		It("resolves a secret ref into the DASH0_SYNTHETICS_WORKER_AUTH_TOKEN environment variable", func() {
			resource := DefaultOperatorConfigurationResource()
			resource.Spec.SyntheticsWorker = dash0v1alpha1.SyntheticsWorker{
				LocationID: "test-location",
				Authorization: &dash0common.Authorization{
					SecretRef: &dash0common.SecretRef{
						Name: "dash0-synthetics-worker-authorization-secret",
						Key:  "token",
					},
				},
			}
			created, _, err := manager.CreateOrUpdateSyntheticsWorkerResources(ctx, resource, logger)
			Expect(err).ToNot(HaveOccurred())
			Expect(created).To(BeTrue())

			container := getDeployedSyntheticsWorkerContainer(ctx)
			tokenEnvVar := util.GetEnvVar(&container, authTokenEnvVarName)
			Expect(tokenEnvVar).ToNot(BeNil())
			Expect(tokenEnvVar.Value).To(BeEmpty())
			Expect(tokenEnvVar.ValueFrom).ToNot(BeNil())
			Expect(tokenEnvVar.ValueFrom.SecretKeyRef).ToNot(BeNil())
			Expect(tokenEnvVar.ValueFrom.SecretKeyRef.Name).To(Equal("dash0-synthetics-worker-authorization-secret"))
			Expect(tokenEnvVar.ValueFrom.SecretKeyRef.Key).To(Equal("token"))
		})

		It("aborts and returns an error when no authorization token is available", func() {
			resource := DefaultOperatorConfigurationResource()
			resource.Spec.SyntheticsWorker = dash0v1alpha1.SyntheticsWorker{LocationID: "test-location"}

			created, updated, err := manager.CreateOrUpdateSyntheticsWorkerResources(ctx, resource, logger)

			Expect(err).To(HaveOccurred())
			Expect(errors.Is(err, ErrMisconfigured)).To(BeTrue())
			Expect(errors.Is(err, ErrNoAuthorizationToken)).To(BeTrue())
			Expect(created).To(BeFalse())
			Expect(updated).To(BeFalse())
			verifySyntheticsWorkerResourcesDoNotExist(ctx, Default)
		})
	})

	Context("when self-monitoring is enabled in the Dash0OperatorConfiguration resource", func() {
		It("sets the environment variables of the OTel SDK on the deployed container", func() {
			created, _, err := manager.CreateOrUpdateSyntheticsWorkerResources(
				ctx,
				operatorConfigurationResourceWithSyntheticsWorker(syntheticsWorkerAuthToken),
				logger,
			)
			Expect(err).ToNot(HaveOccurred())
			Expect(created).To(BeTrue())

			container := getDeployedSyntheticsWorkerContainer(ctx)
			Expect(container.Env).To(ContainElement(
				corev1.EnvVar{Name: "SELF_MONITORING_AUTH_TOKEN", Value: AuthorizationTokenTest}))
			Expect(container.Env).To(ContainElement(
				corev1.EnvVar{Name: "OTEL_EXPORTER_OTLP_ENDPOINT", Value: EndpointDash0WithProtocolTest}))
			Expect(container.Env).To(ContainElement(
				corev1.EnvVar{Name: "OTEL_EXPORTER_OTLP_PROTOCOL", Value: "grpc"}))
		})

		It("omits the environment variables of the OTel SDK when there is no operator configuration resource for "+
			"self-monitoring purposes", func() {
			resource := operatorConfigurationResourceWithSyntheticsWorker(syntheticsWorkerAuthToken)
			resource.Spec.Export = nil
			resource.Spec.Exports = nil
			created, _, err := manager.CreateOrUpdateSyntheticsWorkerResources(ctx, resource, logger)
			Expect(err).ToNot(HaveOccurred())
			Expect(created).To(BeTrue())

			container := getDeployedSyntheticsWorkerContainer(ctx)
			for _, envVar := range container.Env {
				Expect(envVar.Name).ToNot(HavePrefix("OTEL_"))
			}
		})
	})

	Context("when synthetics-worker resources have been modified externally", func() {
		It("should reconcile the resources back into the desired state", func() {
			created, _, err := manager.CreateOrUpdateSyntheticsWorkerResources(
				ctx,
				operatorConfigurationResourceWithSyntheticsWorker(syntheticsWorkerAuthToken),
				logger,
			)
			Expect(err).ToNot(HaveOccurred())
			Expect(created).To(BeTrue())

			deployment := &appsv1.Deployment{}
			Expect(k8sClient.Get(
				ctx,
				client.ObjectKey{Name: DeploymentName(testNamePrefix), Namespace: OperatorNamespace},
				deployment,
			)).To(Succeed())
			var changedReplicas int32 = 5
			deployment.Spec.Replicas = &changedReplicas
			Expect(k8sClient.Update(ctx, deployment)).To(Succeed())

			created, updated, err := manager.CreateOrUpdateSyntheticsWorkerResources(
				ctx,
				operatorConfigurationResourceWithSyntheticsWorker(syntheticsWorkerAuthToken),
				logger,
			)
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

	Context("when all synthetics-worker resources are up to date", func() {
		It("should report that nothing has changed", func() {
			created, updated, err := manager.CreateOrUpdateSyntheticsWorkerResources(
				ctx,
				operatorConfigurationResourceWithSyntheticsWorker(syntheticsWorkerAuthToken),
				logger,
			)
			Expect(err).ToNot(HaveOccurred())
			Expect(created).To(BeTrue())
			Expect(updated).To(BeFalse())

			created, updated, err = manager.CreateOrUpdateSyntheticsWorkerResources(
				ctx,
				operatorConfigurationResourceWithSyntheticsWorker(syntheticsWorkerAuthToken),
				logger,
			)
			Expect(err).ToNot(HaveOccurred())
			Expect(created).To(BeFalse())
			Expect(updated).To(BeFalse())

			verifySyntheticsWorkerResourcesExist(ctx)
		})
	})

	Context("when deleting all synthetics-worker resources", func() {
		It("should delete the resources", func() {
			_, _, err := manager.CreateOrUpdateSyntheticsWorkerResources(
				ctx,
				operatorConfigurationResourceWithSyntheticsWorker(syntheticsWorkerAuthToken),
				logger,
			)
			Expect(err).ToNot(HaveOccurred())
			verifySyntheticsWorkerResourcesExist(ctx)

			deleted, err := manager.DeleteResources(ctx, logger)
			Expect(err).ToNot(HaveOccurred())
			Expect(deleted).To(BeTrue())

			verifySyntheticsWorkerResourcesDoNotExist(ctx, Default)

			// Deletion must be idempotent: deleting again must not error, but must report that nothing was deleted.
			deleted, err = manager.DeleteResources(ctx, logger)
			Expect(err).ToNot(HaveOccurred())
			Expect(deleted).To(BeFalse())
		})
	})
})

func newSyntheticsWorkerResourceManager() *SyntheticsWorkerResourceManager {
	return NewSyntheticsWorkerResourceManager(
		k8sClient,
		k8sClient.Scheme(),
		OperatorManagerDeployment,
		util.SyntheticsWorkerConfig{
			Images: util.Images{
				SyntheticsWorkerImage:           testImage,
				SyntheticsWorkerImagePullPolicy: corev1.PullAlways,
			},
			OperatorNamespace: OperatorNamespace,
			NamePrefix:        testNamePrefix,
			ServerAddress:     SyntheticsWorkerServerAddress,
			DevelopmentMode:   true,
		},
	)
}

// operatorConfigurationResourceWithSyntheticsWorker returns the default operator configuration resource with
// spec.syntheticsWorker populated with a location ID and the given literal authorization token.
func operatorConfigurationResourceWithSyntheticsWorker(token string) *dash0v1alpha1.Dash0OperatorConfiguration {
	resource := DefaultOperatorConfigurationResource()
	resource.Spec.SyntheticsWorker = dash0v1alpha1.SyntheticsWorker{
		LocationID:    "test-location",
		Authorization: &dash0common.Authorization{Token: &token},
	}
	return resource
}

func getDeployedSyntheticsWorkerContainer(ctx context.Context) corev1.Container {
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

func verifySyntheticsWorkerResourcesExist(ctx context.Context) {
	GinkgoHelper()
	VerifyResourceExists(ctx, k8sClient, OperatorNamespace, ServiceAccountName(testNamePrefix), &corev1.ServiceAccount{})
	VerifyResourceExists(ctx, k8sClient, OperatorNamespace, DeploymentName(testNamePrefix), &appsv1.Deployment{})
}

func verifySyntheticsWorkerResourcesDoNotExist(ctx context.Context, g Gomega) {
	g.Expect(k8sClient.Get(
		ctx,
		client.ObjectKey{Name: ServiceAccountName(testNamePrefix), Namespace: OperatorNamespace},
		&corev1.ServiceAccount{},
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
