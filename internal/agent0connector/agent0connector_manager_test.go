// SPDX-FileCopyrightText: Copyright 2026 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package agent0connector

import (
	"context"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"sigs.k8s.io/controller-runtime/pkg/client"

	dash0common "github.com/dash0hq/dash0-operator/api/operator/common"
	"github.com/dash0hq/dash0-operator/internal/agent0connector/a0cresources"
	"github.com/dash0hq/dash0-operator/internal/util"
	"github.com/dash0hq/dash0-operator/internal/util/logd"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	. "github.com/dash0hq/dash0-operator/test/util"
)

const agent0ConnectorTestNamePrefix = "dash0-operator-test"

func newResourceManager() *a0cresources.Agent0ConnectorResourceManager {
	token := AuthorizationTokenTest
	return a0cresources.NewAgent0ConnectorResourceManager(
		k8sClient,
		k8sClient.Scheme(),
		OperatorManagerDeployment,
		util.Agent0ConnectorConfig{
			Images:            util.Images{Agent0ConnectorImage: "ghcr.io/dash0hq/agent0-connector:test"},
			OperatorNamespace: OperatorNamespace,
			NamePrefix:        agent0ConnectorTestNamePrefix,
			ServerAddress:     "https://example.com:4317",
			Authorization:     dash0common.Authorization{Token: &token},
		},
	)
}

func newManager(enabled bool) *Agent0ConnectorManager {
	return NewAgent0ConnectorManager(k8sClient, enabled, false, newResourceManager())
}

var _ = Describe("The agent0-connector manager", Ordered, func() {
	ctx := context.Background()

	BeforeAll(func() {
		EnsureOperatorNamespaceExists(ctx, k8sClient)
	})

	AfterEach(func() {
		_, err := newResourceManager().DeleteResources(ctx, logd.FromContext(ctx))
		Expect(err).ToNot(HaveOccurred())
		DeleteAllOperatorConfigurationResources(ctx, k8sClient)
	})

	It("creates the agent0-connector resources when enabled and an operator configuration resource exists", func() {
		CreateDefaultOperatorConfigurationResource(ctx, k8sClient)

		hasBeenReconciled, err := newManager(true).ReconcileAgent0Connector(ctx, TriggeredByDash0OperatorConfigurationResourceReconcile)

		Expect(err).ToNot(HaveOccurred())
		Expect(hasBeenReconciled).To(BeTrue())
		expectAgent0ConnectorResourcesToExist(ctx)
	})

	It("removes the agent0-connector resources when the feature is disabled", func() {
		CreateDefaultOperatorConfigurationResource(ctx, k8sClient)
		// First create the resources with an enabled manager, ...
		_, err := newManager(true).ReconcileAgent0Connector(ctx, TriggeredByDash0OperatorConfigurationResourceReconcile)
		Expect(err).ToNot(HaveOccurred())
		expectAgent0ConnectorResourcesToExist(ctx)

		// ... then reconcile with a disabled manager and expect them to be removed again.
		hasBeenReconciled, err := newManager(false).ReconcileAgent0Connector(ctx, TriggeredByDash0OperatorConfigurationResourceReconcile)

		Expect(err).ToNot(HaveOccurred())
		Expect(hasBeenReconciled).To(BeTrue())
		expectAgent0ConnectorResourcesToNotExist(ctx)
	})

	It("removes the agent0-connector resources when there is no operator configuration resource", func() {
		// Create the resources first (with an operator configuration resource present), ...
		CreateDefaultOperatorConfigurationResource(ctx, k8sClient)
		_, err := newManager(true).ReconcileAgent0Connector(ctx, TriggeredByDash0OperatorConfigurationResourceReconcile)
		Expect(err).ToNot(HaveOccurred())
		expectAgent0ConnectorResourcesToExist(ctx)

		// ... then delete the operator configuration resource and reconcile again.
		DeleteAllOperatorConfigurationResources(ctx, k8sClient)
		hasBeenReconciled, err := newManager(true).ReconcileAgent0Connector(ctx, TriggeredByWatchEvent)

		Expect(err).ToNot(HaveOccurred())
		Expect(hasBeenReconciled).To(BeTrue())
		expectAgent0ConnectorResourcesToNotExist(ctx)
	})

	It("does not reconcile when an update is already in progress", func() {
		CreateDefaultOperatorConfigurationResource(ctx, k8sClient)
		manager := newManager(true)
		manager.updateInProgress.Store(true)

		hasBeenReconciled, err := manager.ReconcileAgent0Connector(ctx, TriggeredByWatchEvent)

		Expect(err).ToNot(HaveOccurred())
		Expect(hasBeenReconciled).To(BeFalse())
		// The reconcile was skipped, so no resources were created.
		expectAgent0ConnectorResourcesToNotExist(ctx)
	})
})

func expectAgent0ConnectorResourcesToExist(ctx context.Context) {
	GinkgoHelper()
	Expect(k8sClient.Get(ctx,
		client.ObjectKey{Namespace: OperatorNamespace, Name: a0cresources.ServiceAccountName(agent0ConnectorTestNamePrefix)},
		&corev1.ServiceAccount{})).To(Succeed())
	Expect(k8sClient.Get(ctx,
		client.ObjectKey{Name: a0cresources.ClusterRoleName(agent0ConnectorTestNamePrefix)},
		&rbacv1.ClusterRole{})).To(Succeed())
	Expect(k8sClient.Get(ctx,
		client.ObjectKey{Name: a0cresources.ClusterRoleBindingName(agent0ConnectorTestNamePrefix)},
		&rbacv1.ClusterRoleBinding{})).To(Succeed())
	Expect(k8sClient.Get(ctx,
		client.ObjectKey{Namespace: OperatorNamespace, Name: a0cresources.DeploymentName(agent0ConnectorTestNamePrefix)},
		&appsv1.Deployment{})).To(Succeed())
}

func expectAgent0ConnectorResourcesToNotExist(ctx context.Context) {
	GinkgoHelper()
	Expect(apierrors.IsNotFound(k8sClient.Get(ctx,
		client.ObjectKey{Namespace: OperatorNamespace, Name: a0cresources.ServiceAccountName(agent0ConnectorTestNamePrefix)},
		&corev1.ServiceAccount{}))).To(BeTrue())
	Expect(apierrors.IsNotFound(k8sClient.Get(ctx,
		client.ObjectKey{Name: a0cresources.ClusterRoleName(agent0ConnectorTestNamePrefix)},
		&rbacv1.ClusterRole{}))).To(BeTrue())
	Expect(apierrors.IsNotFound(k8sClient.Get(ctx,
		client.ObjectKey{Name: a0cresources.ClusterRoleBindingName(agent0ConnectorTestNamePrefix)},
		&rbacv1.ClusterRoleBinding{}))).To(BeTrue())
	Expect(apierrors.IsNotFound(k8sClient.Get(ctx,
		client.ObjectKey{Namespace: OperatorNamespace, Name: a0cresources.DeploymentName(agent0ConnectorTestNamePrefix)},
		&appsv1.Deployment{}))).To(BeTrue())
}
