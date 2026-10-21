// SPDX-FileCopyrightText: Copyright 2026 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package resources

import (
	"context"

	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"

	dash0v1alpha1 "github.com/dash0hq/dash0-operator/api/operator/v1alpha1"
	"github.com/dash0hq/dash0-operator/internal/util/logd"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	. "github.com/dash0hq/dash0-operator/test/util"
)

var _ = Describe("SetOwnerReference", func() {
	logger := logd.FromContext(context.Background())
	var testScheme *runtime.Scheme

	BeforeEach(func() {
		testScheme = runtime.NewScheme()
		Expect(clientgoscheme.AddToScheme(testScheme)).To(Succeed())
		Expect(dash0v1alpha1.AddToScheme(testScheme)).To(Succeed())
	})

	It("makes the operator manager deployment the owner of a namespaced object", func() {
		object := &corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Namespace: OperatorNamespace, Name: "test"}}

		Expect(SetOwnerReference(
			OperatorManagerDeployment, DefaultOperatorConfigurationResourceWithUid(), testScheme, object, logger,
		)).To(Succeed())

		ownerReferences := object.GetOwnerReferences()
		Expect(ownerReferences).To(HaveLen(1))
		Expect(ownerReferences[0].Kind).To(Equal("Deployment"))
		Expect(ownerReferences[0].Name).To(Equal(OperatorManagerDeployment.Name))
		Expect(ownerReferences[0].UID).To(Equal(OperatorManagerDeploymentUID))
		Expect(ownerReferences[0].Controller).To(BeNil())
		Expect(*ownerReferences[0].BlockOwnerDeletion).To(BeTrue())
	})

	It("makes the operator configuration resource the owner of a cluster-scoped object", func() {
		object := &rbacv1.ClusterRole{ObjectMeta: metav1.ObjectMeta{Name: "test"}}
		operatorConfigurationResource := DefaultOperatorConfigurationResourceWithUid()

		Expect(SetOwnerReference(
			OperatorManagerDeployment, operatorConfigurationResource, testScheme, object, logger,
		)).To(Succeed())

		ownerReferences := object.GetOwnerReferences()
		Expect(ownerReferences).To(HaveLen(1))
		Expect(ownerReferences[0].Kind).To(Equal("Dash0OperatorConfiguration"))
		Expect(ownerReferences[0].Name).To(Equal(operatorConfigurationResource.Name))
		Expect(ownerReferences[0].UID).To(Equal(OperatorConfigurationResourceUid))
		Expect(ownerReferences[0].Controller).To(BeNil())
		Expect(*ownerReferences[0].BlockOwnerDeletion).To(BeTrue())
	})

	It("does not set an owner on a cluster-scoped object when there is no operator configuration resource", func() {
		object := &rbacv1.ClusterRole{ObjectMeta: metav1.ObjectMeta{Name: "test"}}

		Expect(SetOwnerReference(OperatorManagerDeployment, nil, testScheme, object, logger)).To(Succeed())

		Expect(object.GetOwnerReferences()).To(BeEmpty())
	})
})
