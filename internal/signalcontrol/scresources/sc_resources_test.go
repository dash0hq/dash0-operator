// SPDX-FileCopyrightText: Copyright 2026 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package scresources

import (
	"context"

	"github.com/cisco-open/k8s-objectmatcher/patch"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	dash0common "github.com/dash0hq/dash0-operator/api/operator/common"
	dash0v1alpha1 "github.com/dash0hq/dash0-operator/api/operator/v1alpha1"
	"github.com/dash0hq/dash0-operator/internal/util"
	"github.com/dash0hq/dash0-operator/internal/util/cluster"
	"github.com/dash0hq/dash0-operator/internal/util/logd"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	. "github.com/dash0hq/dash0-operator/test/util"
)

const (
	testNamePrefix     = "test-prefix"
	testEdgeProxyImage = "edge-proxy:latest"
)

var _ = Describe("The Signal Control resource manager", func() {
	ctx := context.Background()
	logger := logd.FromContext(ctx)

	var k8sClient client.Client

	newResourceManagerWithMode := func(k8sClient client.Client, sceOnlyMode bool) *SignalControlResourceManager {
		return NewSignalControlResourceManager(
			k8sClient,
			clientgoscheme.Scheme,
			OperatorManagerDeployment,
			OperatorNamespace,
			testNamePrefix,
			testEdgeProxyImage,
			corev1.PullIfNotPresent,
			testOperatorVersion,
			testOtlpGrpcHostPort,
			cluster.KubernetesVersionInfo{},
			false,
			false,
			sceOnlyMode,
			false,
		)
	}
	newResourceManager := func(k8sClient client.Client) *SignalControlResourceManager {
		return newResourceManagerWithMode(k8sClient, false)
	}

	BeforeEach(func() {
		k8sClient = fake.NewClientBuilder().WithScheme(clientgoscheme.Scheme).Build()
	})

	desiredObjects := func() []client.Object {
		desiredState := assembleDesiredState(
			OperatorNamespace, testNamePrefix, minimalSignalControl, DefaultOperatorConfigurationResourceWithUid(),
			testEdgeProxyImage, corev1.PullIfNotPresent, testOperatorVersion, testSelfMonitoring,
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
		resourcesHaveBeenCreated, _, err := newResourceManager(k8sClient).CreateOrUpdateResources(
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

		_, resourcesHaveBeenUpdated, err := newResourceManager(k8sClient).CreateOrUpdateResources(
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

	It("should keep the kubectl rollout restart annotation, also when reconciling other changes", func() {
		resourceManager := newResourceManager(k8sClient)
		labels := map[string]string{"app": "edge-proxy"}
		desired := &appsv1.Deployment{
			TypeMeta:   metav1.TypeMeta{Kind: "Deployment", APIVersion: "apps/v1"},
			ObjectMeta: metav1.ObjectMeta{Name: "edge-proxy", Namespace: OperatorNamespace},
			Spec: appsv1.DeploymentSpec{
				Replicas: new(int32(1)),
				Selector: &metav1.LabelSelector{MatchLabels: labels},
				Template: corev1.PodTemplateSpec{
					ObjectMeta: metav1.ObjectMeta{Labels: labels},
					Spec: corev1.PodSpec{
						Containers: []corev1.Container{{Name: "edge-proxy", Image: testEdgeProxyImage}},
					},
				},
			},
		}
		getDeployment := func() *appsv1.Deployment {
			GinkgoHelper()
			deployment := &appsv1.Deployment{}
			Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(desired), deployment)).To(Succeed())
			return deployment
		}
		Expect(resourceManager.createResource(ctx, desired.DeepCopy(), logger)).To(Succeed())

		deployment := getDeployment()
		SimulateKubectlRolloutRestart(&deployment.Spec.Template)
		Expect(k8sClient.Update(ctx, deployment)).To(Succeed())

		_, isChanged, err := resourceManager.createOrUpdateResource(ctx, desired.DeepCopy(), logger)
		Expect(err).ToNot(HaveOccurred())
		Expect(isChanged).To(BeFalse())
		Expect(getDeployment().Spec.Template.Annotations).To(
			HaveKeyWithValue(KubectlRestartedAtAnnotationTest, KubectlRestartedAtTest))

		deployment = getDeployment()
		deployment.Spec.Replicas = new(int32(5))
		Expect(k8sClient.Update(ctx, deployment)).To(Succeed())

		_, isChanged, err = resourceManager.createOrUpdateResource(ctx, desired.DeepCopy(), logger)
		Expect(err).ToNot(HaveOccurred())
		Expect(isChanged).To(BeTrue())
		reconciled := getDeployment()
		Expect(*reconciled.Spec.Replicas).To(Equal(int32(1)))
		Expect(reconciled.Spec.Template.Annotations).To(
			HaveKeyWithValue(KubectlRestartedAtAnnotationTest, KubectlRestartedAtTest))
	})

	It("should retry an update that conflicts with a concurrent modification", func() {
		configMap := &corev1.ConfigMap{
			TypeMeta:   metav1.TypeMeta{Kind: "ConfigMap", APIVersion: "v1"},
			ObjectMeta: metav1.ObjectMeta{Name: "test-config-map", Namespace: OperatorNamespace},
			Data:       map[string]string{"key": "value"},
		}
		Expect(newResourceManager(k8sClient).createResource(ctx, configMap.DeepCopy(), logger)).To(Succeed())

		conflictingClient := &ConflictOnFirstUpdateClient{Client: k8sClient}
		updated := configMap.DeepCopy()
		updated.Data["key"] = "value after a conflict"
		isNew, isChanged, err := newResourceManager(conflictingClient).createOrUpdateResource(ctx, updated, logger)

		Expect(err).ToNot(HaveOccurred())
		Expect(isNew).To(BeFalse())
		Expect(isChanged).To(BeTrue())
		Expect(conflictingClient.UpdateCalls).To(Equal(2), "expected exactly one retry after the conflict")
		reconciled := &corev1.ConfigMap{}
		Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(configMap), reconciled)).To(Succeed())
		Expect(reconciled.Data).To(Equal(updated.Data))
	})

	Describe("the Edge Proxy self-monitoring", func() {
		operatorConfig := &dash0v1alpha1.Dash0OperatorConfiguration{
			Spec: dash0v1alpha1.Dash0OperatorConfigurationSpec{
				SelfMonitoring: dash0v1alpha1.SelfMonitoring{Enabled: new(true)},
				Exports: []dash0common.Export{{
					Dash0: &dash0common.Dash0Configuration{
						Endpoint:      EndpointDash0Test,
						Authorization: dash0common.Authorization{Token: &AuthorizationTokenTest},
					},
				}},
			},
		}

		It("uses the node-local collector outside of SCE-only mode", func() {
			selfMonitoring := newResourceManager(k8sClient).edgeProxySelfMonitoring(ctx, operatorConfig, logger)
			Expect(selfMonitoring.otlpGrpcHostPort).To(Equal(testOtlpGrpcHostPort))
			Expect(selfMonitoring.directExport).To(BeNil())
		})

		It("exports directly in SCE-only mode, without resolved secret values", func() {
			selfMonitoring := newResourceManagerWithMode(k8sClient, true).
				edgeProxySelfMonitoring(ctx, operatorConfig, logger)
			Expect(selfMonitoring.directExport).ToNot(BeNil())
			Expect(selfMonitoring.directExport.SelfMonitoringEnabled).To(BeTrue())
			Expect(selfMonitoring.directExport.Export.Dash0).ToNot(BeNil())
			Expect(selfMonitoring.directExport.Export.Dash0.Endpoint).To(Equal(EndpointDash0Test))
			Expect(selfMonitoring.directExport.Token).To(BeNil())
			Expect(selfMonitoring.directExport.ResolvedSecretHeaderValues).To(BeNil())
		})

		It("disables the direct export in SCE-only mode when self-monitoring is disabled", func() {
			disabled := operatorConfig.DeepCopy()
			disabled.Spec.SelfMonitoring.Enabled = new(false)
			selfMonitoring := newResourceManagerWithMode(k8sClient, true).edgeProxySelfMonitoring(ctx, disabled, logger)
			Expect(selfMonitoring.directExport).ToNot(BeNil())
			Expect(selfMonitoring.directExport.SelfMonitoringEnabled).To(BeFalse())
		})
	})
})
