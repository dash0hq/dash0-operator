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
			verifyNoSyntheticsWorkerResourcesExist(ctx, g)
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
			_, _, err := manager.createOrUpdateResource(ctx, syntheticsWorkerTestResource.DeepCopy(), logger)
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
			_, _, err := manager.createOrUpdateResource(ctx, syntheticsWorkerTestResource.DeepCopy(), logger)
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
			results, err := manager.CreateOrUpdateSyntheticsWorkerResources(
				ctx,
				operatorConfigurationResourceWithSyntheticsWorker(syntheticsWorkerAuthToken),
				logger,
			)
			Expect(err).ToNot(HaveOccurred())
			created, updated := aggregateResults(results)
			Expect(created).To(BeTrue())
			Expect(updated).To(BeFalse())

			verifySyntheticsWorkerResourcesExist(ctx, testLocationID)
		})

		It("creates one deployment and service account per instance, and does not let one instance's "+
			"misconfiguration block the others", func() {
			resource := DefaultOperatorConfigurationResource()
			brokenToken := ""
			resource.Spec.SyntheticsWorker = dash0v1alpha1.SyntheticsWorker{
				Instances: []dash0v1alpha1.SyntheticsWorkerInstance{
					{LocationID: "location-a", Authorization: &dash0common.Authorization{Token: &syntheticsWorkerAuthToken}},
					{LocationID: "location-b", Authorization: &dash0common.Authorization{Token: &brokenToken}},
				},
			}

			results, err := manager.CreateOrUpdateSyntheticsWorkerResources(ctx, resource, logger)
			Expect(err).ToNot(HaveOccurred())
			Expect(results).To(HaveLen(2))

			var okResult, brokenResult InstanceResult
			for _, result := range results {
				switch result.LocationID {
				case "location-a":
					okResult = result
				case "location-b":
					brokenResult = result
				}
			}
			Expect(okResult.Err).ToNot(HaveOccurred())
			Expect(okResult.Created).To(BeTrue())
			Expect(errors.Is(brokenResult.Err, ErrNoAuthorizationToken)).To(BeTrue())

			VerifyResourceExists(ctx, k8sClient, OperatorNamespace, ServiceAccountName(testNamePrefix, "location-a"), &corev1.ServiceAccount{})
			VerifyResourceExists(ctx, k8sClient, OperatorNamespace, DeploymentName(testNamePrefix, "location-a"), &appsv1.Deployment{})
			Expect(k8sClient.Get(
				ctx,
				client.ObjectKey{Name: DeploymentName(testNamePrefix, "location-b"), Namespace: OperatorNamespace},
				&appsv1.Deployment{},
			)).To(MatchError(ContainSubstring("not found")))
		})

		It("deletes the resources of an instance that has been removed from the instance list", func() {
			resource := DefaultOperatorConfigurationResource()
			resource.Spec.SyntheticsWorker = dash0v1alpha1.SyntheticsWorker{
				Instances: []dash0v1alpha1.SyntheticsWorkerInstance{
					{LocationID: "location-a", Authorization: &dash0common.Authorization{Token: &syntheticsWorkerAuthToken}},
					{LocationID: "location-b", Authorization: &dash0common.Authorization{Token: &syntheticsWorkerAuthToken}},
				},
			}
			_, err := manager.CreateOrUpdateSyntheticsWorkerResources(ctx, resource, logger)
			Expect(err).ToNot(HaveOccurred())
			VerifyResourceExists(ctx, k8sClient, OperatorNamespace, DeploymentName(testNamePrefix, "location-b"), &appsv1.Deployment{})

			resource.Spec.SyntheticsWorker.Instances = resource.Spec.SyntheticsWorker.Instances[:1]
			_, err = manager.CreateOrUpdateSyntheticsWorkerResources(ctx, resource, logger)
			Expect(err).ToNot(HaveOccurred())

			VerifyResourceExists(ctx, k8sClient, OperatorNamespace, DeploymentName(testNamePrefix, "location-a"), &appsv1.Deployment{})
			Expect(k8sClient.Get(
				ctx,
				client.ObjectKey{Name: DeploymentName(testNamePrefix, "location-b"), Namespace: OperatorNamespace},
				&appsv1.Deployment{},
			)).To(MatchError(ContainSubstring("not found")))
			Expect(k8sClient.Get(
				ctx,
				client.ObjectKey{Name: ServiceAccountName(testNamePrefix, "location-b"), Namespace: OperatorNamespace},
				&corev1.ServiceAccount{},
			)).To(MatchError(ContainSubstring("not found")))
		})
	})

	Context("when the Dash0OperatorConfiguration resource is missing", func() {
		It("aborts and returns an error", func() {
			results, err := manager.CreateOrUpdateSyntheticsWorkerResources(ctx, nil, logger)

			Expect(err).To(HaveOccurred())
			Expect(errors.Is(err, ErrMisconfigured)).To(BeTrue())
			Expect(results).To(BeEmpty())
		})
	})

	Context("when resolving the authorization for the synthetics-worker workload", func() {
		It("passes a literal token via the DASH0_SYNTHETICS_WORKER_AUTH_TOKEN environment variable", func() {
			results, err := manager.CreateOrUpdateSyntheticsWorkerResources(
				ctx,
				operatorConfigurationResourceWithSyntheticsWorker(syntheticsWorkerAuthToken),
				logger,
			)
			Expect(err).ToNot(HaveOccurred())
			created, _ := aggregateResults(results)
			Expect(created).To(BeTrue())

			container := getDeployedSyntheticsWorkerContainer(ctx, testLocationID)
			Expect(container.Env).To(ContainElement(
				corev1.EnvVar{Name: authTokenEnvVarName, Value: syntheticsWorkerAuthToken}))
			Expect(container.Env).To(ContainElement(
				corev1.EnvVar{Name: headersEnvVarName, Value: "authorization=Bearer $(DASH0_SYNTHETICS_WORKER_AUTH_TOKEN)"}))
		})

		It("resolves a secret ref into the DASH0_SYNTHETICS_WORKER_AUTH_TOKEN environment variable", func() {
			resource := DefaultOperatorConfigurationResource()
			resource.Spec.SyntheticsWorker = dash0v1alpha1.SyntheticsWorker{
				Instances: []dash0v1alpha1.SyntheticsWorkerInstance{
					{
						LocationID: testLocationID,
						Authorization: &dash0common.Authorization{
							SecretRef: &dash0common.SecretRef{
								Name: "dash0-synthetics-worker-authorization-secret",
								Key:  "token",
							},
						},
					},
				},
			}
			results, err := manager.CreateOrUpdateSyntheticsWorkerResources(ctx, resource, logger)
			Expect(err).ToNot(HaveOccurred())
			created, _ := aggregateResults(results)
			Expect(created).To(BeTrue())

			container := getDeployedSyntheticsWorkerContainer(ctx, testLocationID)
			tokenEnvVar := util.GetEnvVar(&container, authTokenEnvVarName)
			Expect(tokenEnvVar).ToNot(BeNil())
			Expect(tokenEnvVar.Value).To(BeEmpty())
			Expect(tokenEnvVar.ValueFrom).ToNot(BeNil())
			Expect(tokenEnvVar.ValueFrom.SecretKeyRef).ToNot(BeNil())
			Expect(tokenEnvVar.ValueFrom.SecretKeyRef.Name).To(Equal("dash0-synthetics-worker-authorization-secret"))
			Expect(tokenEnvVar.ValueFrom.SecretKeyRef.Key).To(Equal("token"))
		})

		It("reports the instance as misconfigured, without a top-level error, when no authorization token is available", func() {
			resource := DefaultOperatorConfigurationResource()
			resource.Spec.SyntheticsWorker = dash0v1alpha1.SyntheticsWorker{
				Instances: []dash0v1alpha1.SyntheticsWorkerInstance{{LocationID: testLocationID}},
			}

			results, err := manager.CreateOrUpdateSyntheticsWorkerResources(ctx, resource, logger)

			Expect(err).ToNot(HaveOccurred())
			Expect(results).To(HaveLen(1))
			Expect(errors.Is(results[0].Err, ErrMisconfigured)).To(BeTrue())
			Expect(errors.Is(results[0].Err, ErrNoAuthorizationToken)).To(BeTrue())
			Expect(results[0].Created).To(BeFalse())
			verifySyntheticsWorkerResourcesDoNotExist(ctx, Default, testLocationID)
		})
	})

	Context("when self-monitoring is enabled in the Dash0OperatorConfiguration resource", func() {
		It("sets the environment variables of the OTel SDK on the deployed container", func() {
			results, err := manager.CreateOrUpdateSyntheticsWorkerResources(
				ctx,
				operatorConfigurationResourceWithSyntheticsWorker(syntheticsWorkerAuthToken),
				logger,
			)
			Expect(err).ToNot(HaveOccurred())
			created, _ := aggregateResults(results)
			Expect(created).To(BeTrue())

			container := getDeployedSyntheticsWorkerContainer(ctx, testLocationID)
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
			results, err := manager.CreateOrUpdateSyntheticsWorkerResources(ctx, resource, logger)
			Expect(err).ToNot(HaveOccurred())
			created, _ := aggregateResults(results)
			Expect(created).To(BeTrue())

			container := getDeployedSyntheticsWorkerContainer(ctx, testLocationID)
			for _, envVar := range container.Env {
				Expect(envVar.Name).ToNot(HavePrefix("OTEL_"))
			}
		})
	})

	Context("when synthetics-worker resources have been modified externally", func() {
		It("should reconcile the resources back into the desired state", func() {
			results, err := manager.CreateOrUpdateSyntheticsWorkerResources(
				ctx,
				operatorConfigurationResourceWithSyntheticsWorker(syntheticsWorkerAuthToken),
				logger,
			)
			Expect(err).ToNot(HaveOccurred())
			created, _ := aggregateResults(results)
			Expect(created).To(BeTrue())

			deployment := &appsv1.Deployment{}
			Expect(k8sClient.Get(
				ctx,
				client.ObjectKey{Name: DeploymentName(testNamePrefix, testLocationID), Namespace: OperatorNamespace},
				deployment,
			)).To(Succeed())
			var changedReplicas int32 = 5
			deployment.Spec.Replicas = &changedReplicas
			Expect(k8sClient.Update(ctx, deployment)).To(Succeed())

			results, err = manager.CreateOrUpdateSyntheticsWorkerResources(
				ctx,
				operatorConfigurationResourceWithSyntheticsWorker(syntheticsWorkerAuthToken),
				logger,
			)
			Expect(err).ToNot(HaveOccurred())
			created, updated := aggregateResults(results)
			Expect(created).To(BeFalse())
			Expect(updated).To(BeTrue())

			reconciled := &appsv1.Deployment{}
			Expect(k8sClient.Get(
				ctx,
				client.ObjectKey{Name: DeploymentName(testNamePrefix, testLocationID), Namespace: OperatorNamespace},
				reconciled,
			)).To(Succeed())
			Expect(*reconciled.Spec.Replicas).To(Equal(int32(1)))
		})
	})

	Context("when all synthetics-worker resources are up to date", func() {
		It("should report that nothing has changed", func() {
			results, err := manager.CreateOrUpdateSyntheticsWorkerResources(
				ctx,
				operatorConfigurationResourceWithSyntheticsWorker(syntheticsWorkerAuthToken),
				logger,
			)
			Expect(err).ToNot(HaveOccurred())
			created, updated := aggregateResults(results)
			Expect(created).To(BeTrue())
			Expect(updated).To(BeFalse())

			results, err = manager.CreateOrUpdateSyntheticsWorkerResources(
				ctx,
				operatorConfigurationResourceWithSyntheticsWorker(syntheticsWorkerAuthToken),
				logger,
			)
			Expect(err).ToNot(HaveOccurred())
			created, updated = aggregateResults(results)
			Expect(created).To(BeFalse())
			Expect(updated).To(BeFalse())

			verifySyntheticsWorkerResourcesExist(ctx, testLocationID)
		})
	})

	Context("when deleting all synthetics-worker resources", func() {
		It("should delete the resources", func() {
			_, err := manager.CreateOrUpdateSyntheticsWorkerResources(
				ctx,
				operatorConfigurationResourceWithSyntheticsWorker(syntheticsWorkerAuthToken),
				logger,
			)
			Expect(err).ToNot(HaveOccurred())
			verifySyntheticsWorkerResourcesExist(ctx, testLocationID)

			deleted, err := manager.DeleteResources(ctx, logger)
			Expect(err).ToNot(HaveOccurred())
			Expect(deleted).To(BeTrue())

			verifySyntheticsWorkerResourcesDoNotExist(ctx, Default, testLocationID)

			// Deletion must be idempotent: deleting again must not error, but must report that nothing was deleted.
			deleted, err = manager.DeleteResources(ctx, logger)
			Expect(err).ToNot(HaveOccurred())
			Expect(deleted).To(BeFalse())
		})
	})
})

// aggregateResults folds a slice of InstanceResult into the created/updated summary the pre-multi-instance tests
// asserted on.
func aggregateResults(results []InstanceResult) (created bool, updated bool) {
	for _, result := range results {
		created = created || result.Created
		updated = updated || result.Updated
	}
	return created, updated
}

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

const testLocationID = "test-location"

// operatorConfigurationResourceWithSyntheticsWorker returns the default operator configuration resource with a single
// spec.syntheticsWorker instance populated with a location ID and the given literal authorization token.
func operatorConfigurationResourceWithSyntheticsWorker(token string) *dash0v1alpha1.Dash0OperatorConfiguration {
	resource := DefaultOperatorConfigurationResource()
	resource.Spec.SyntheticsWorker = dash0v1alpha1.SyntheticsWorker{
		Instances: []dash0v1alpha1.SyntheticsWorkerInstance{
			{LocationID: testLocationID, Authorization: &dash0common.Authorization{Token: &token}},
		},
	}
	return resource
}

func getDeployedSyntheticsWorkerContainer(ctx context.Context, locationID string) corev1.Container {
	GinkgoHelper()
	deployment := &appsv1.Deployment{}
	Expect(k8sClient.Get(
		ctx,
		client.ObjectKey{Name: DeploymentName(testNamePrefix, locationID), Namespace: OperatorNamespace},
		deployment,
	)).To(Succeed())
	Expect(deployment.Spec.Template.Spec.Containers).To(HaveLen(1))
	return deployment.Spec.Template.Spec.Containers[0]
}

func verifySyntheticsWorkerResourcesExist(ctx context.Context, locationID string) {
	GinkgoHelper()
	VerifyResourceExists(ctx, k8sClient, OperatorNamespace, ServiceAccountName(testNamePrefix, locationID), &corev1.ServiceAccount{})
	VerifyResourceExists(ctx, k8sClient, OperatorNamespace, DeploymentName(testNamePrefix, locationID), &appsv1.Deployment{})
}

func verifySyntheticsWorkerResourcesDoNotExist(ctx context.Context, g Gomega, locationID string) {
	g.Expect(k8sClient.Get(
		ctx,
		client.ObjectKey{Name: ServiceAccountName(testNamePrefix, locationID), Namespace: OperatorNamespace},
		&corev1.ServiceAccount{},
	)).To(MatchError(ContainSubstring("not found")))
	g.Expect(k8sClient.Get(
		ctx,
		client.ObjectKey{Name: DeploymentName(testNamePrefix, locationID), Namespace: OperatorNamespace},
		&appsv1.Deployment{},
	)).To(MatchError(ContainSubstring("not found")))
}

// verifyNoSyntheticsWorkerResourcesExist checks, by label rather than by name, that no Deployment or ServiceAccount of
// any synthetics-worker instance remains - used in AfterEach, where the set of instance names a given test used is
// not known generically.
func verifyNoSyntheticsWorkerResourcesExist(ctx context.Context, g Gomega) {
	var deployments appsv1.DeploymentList
	g.Expect(k8sClient.List(ctx, &deployments, client.InNamespace(OperatorNamespace), client.MatchingLabels(FeatureLabelSelector()))).To(Succeed())
	g.Expect(deployments.Items).To(BeEmpty())

	var serviceAccounts corev1.ServiceAccountList
	g.Expect(k8sClient.List(ctx, &serviceAccounts, client.InNamespace(OperatorNamespace), client.MatchingLabels(FeatureLabelSelector()))).To(Succeed())
	g.Expect(serviceAccounts.Items).To(BeEmpty())
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
