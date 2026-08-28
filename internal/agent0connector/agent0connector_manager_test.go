// SPDX-FileCopyrightText: Copyright 2026 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package agent0connector

import (
	"context"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/client-go/tools/events"
	"sigs.k8s.io/controller-runtime/pkg/client"

	dash0common "github.com/dash0hq/dash0-operator/api/operator/common"
	dash0v1alpha1 "github.com/dash0hq/dash0-operator/api/operator/v1alpha1"
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

// eventRecorder is set by newManager and newManagerWithExtraConfig, so that a test can assert which events the manager
// under test has queued.
var eventRecorder *events.FakeRecorder

func newManager(enabled bool) *Agent0ConnectorManager {
	return newManagerWithExtraConfig(enabled, util.ExtraConfig{})
}

func newManagerWithExtraConfig(enabled bool, extraConfig util.ExtraConfig) *Agent0ConnectorManager {
	eventRecorder = events.NewFakeRecorder(10)
	return NewAgent0ConnectorManager(k8sClient, enabled, extraConfig, false, newResourceManager(), eventRecorder)
}

// recordedEvents drains the events the manager under test has queued so far.
func recordedEvents() []string {
	var recorded []string
	for {
		select {
		case event := <-eventRecorder.Events:
			recorded = append(recorded, event)
		default:
			return recorded
		}
	}
}

func expectAgent0ConnectorStatus(ctx context.Context) *dash0v1alpha1.Agent0ConnectorStatus {
	GinkgoHelper()
	operatorConfigurationResource := LoadOperatorConfigurationResourceOrFail(ctx, k8sClient, Default)
	Expect(operatorConfigurationResource.Status.Agent0Connector).ToNot(BeNil())
	return operatorConfigurationResource.Status.Agent0Connector
}

var _ = Describe("The agent0-connector manager", Ordered, func() {
	ctx := context.Background()

	BeforeAll(func() {
		EnsureOperatorNamespaceExists(ctx, k8sClient)
	})

	AfterEach(func() {
		_, err := newResourceManager().DeleteResources(ctx, util.ExtraConfig{}, logd.FromContext(ctx))
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

	It("applies an updated extra config map to the agent0-connector deployment", func() {
		CreateDefaultOperatorConfigurationResource(ctx, k8sClient)
		manager := newManager(true)
		_, err := manager.ReconcileAgent0Connector(ctx, TriggeredByDash0OperatorConfigurationResourceReconcile)
		Expect(err).ToNot(HaveOccurred())
		expectAgent0ConnectorResourcesToExist(ctx)

		manager.UpdateExtraConfig(ctx, util.ExtraConfig{
			Agent0ConnectorTolerations: []corev1.Toleration{
				{
					Key:      "agent0-connector-key",
					Operator: corev1.TolerationOpExists,
					Effect:   corev1.TaintEffectNoSchedule,
				},
			},
		}, logd.FromContext(ctx))

		deployment := &appsv1.Deployment{}
		Expect(k8sClient.Get(ctx,
			client.ObjectKey{Namespace: OperatorNamespace, Name: a0cresources.DeploymentName(agent0ConnectorTestNamePrefix)},
			deployment)).To(Succeed())
		Expect(deployment.Spec.Template.Spec.Tolerations).To(HaveLen(1))
		Expect(deployment.Spec.Template.Spec.Tolerations[0].Key).To(Equal("agent0-connector-key"))
	})

	It("does not report an error when the agent0-connector is misconfigured", func() {
		CreateDefaultOperatorConfigurationResource(ctx, k8sClient)
		manager := newManagerWithExtraConfig(true, extraConfigWithWriteVerb())

		hasBeenReconciled, err := manager.ReconcileAgent0Connector(
			ctx,
			TriggeredByDash0OperatorConfigurationResourceReconcile,
		)

		// Reporting an error would make the caller requeue the reconcile request, and it would abort the caller's
		// remaining reconciliation steps.
		Expect(err).ToNot(HaveOccurred())
		Expect(hasBeenReconciled).To(BeFalse())
		expectAgent0ConnectorResourcesToNotExist(ctx)
	})

	It("reports a misconfiguration in the status and queues a warning event", func() {
		CreateDefaultOperatorConfigurationResource(ctx, k8sClient)
		manager := newManagerWithExtraConfig(true, extraConfigWithWriteVerb())

		_, err := manager.ReconcileAgent0Connector(ctx, TriggeredByDash0OperatorConfigurationResourceReconcile)
		Expect(err).ToNot(HaveOccurred())

		status := expectAgent0ConnectorStatus(ctx)
		Expect(status.Deployed).To(BeFalse())
		Expect(status.Reason).To(Equal(StatusReasonInvalidClusterRoleRules))
		Expect(status.Message).To(ContainSubstring(`the verb "delete" is not allowed`))
		Expect(status.LastTransitionTime).ToNot(BeZero())

		Expect(recordedEvents()).To(ConsistOf(ContainSubstring("Agent0ConnectorNotDeployed")))
	})

	It("keeps the operator configuration resource available while the agent0-connector is misconfigured", func() {
		CreateDefaultOperatorConfigurationResource(ctx, k8sClient)
		operatorConfigurationResource := LoadOperatorConfigurationResourceOrFail(ctx, k8sClient, Default)
		operatorConfigurationResource.EnsureResourceIsMarkedAsAvailable()
		Expect(k8sClient.Status().Update(ctx, operatorConfigurationResource)).To(Succeed())

		manager := newManagerWithExtraConfig(true, extraConfigWithWriteVerb())
		_, err := manager.ReconcileAgent0Connector(ctx, TriggeredByDash0OperatorConfigurationResourceReconcile)
		Expect(err).ToNot(HaveOccurred())

		// The agent0-connector is an optional feature, an issue with it must not make the operator configuration
		// resource unavailable or degraded.
		operatorConfigurationResource = LoadOperatorConfigurationResourceOrFail(ctx, k8sClient, Default)
		Expect(operatorConfigurationResource.IsAvailable()).To(BeTrue())
		Expect(operatorConfigurationResource.IsDegraded()).To(BeFalse())
		Expect(operatorConfigurationResource.Status.Agent0Connector.Deployed).To(BeFalse())
	})

	It("queues no second event while the misconfiguration is unchanged", func() {
		CreateDefaultOperatorConfigurationResource(ctx, k8sClient)
		manager := newManagerWithExtraConfig(true, extraConfigWithWriteVerb())

		_, err := manager.ReconcileAgent0Connector(ctx, TriggeredByDash0OperatorConfigurationResourceReconcile)
		Expect(err).ToNot(HaveOccurred())
		Expect(recordedEvents()).To(HaveLen(1))

		// Reconciliation is triggered by every watch event on the agent0-connector resources, an event per attempt
		// would flood the resource.
		_, err = manager.ReconcileAgent0Connector(ctx, TriggeredByWatchEvent)
		Expect(err).ToNot(HaveOccurred())
		Expect(recordedEvents()).To(BeEmpty())
	})

	It("reports the recovery in the status and queues a normal event", func() {
		CreateDefaultOperatorConfigurationResource(ctx, k8sClient)
		manager := newManagerWithExtraConfig(true, extraConfigWithWriteVerb())
		_, err := manager.ReconcileAgent0Connector(ctx, TriggeredByDash0OperatorConfigurationResourceReconcile)
		Expect(err).ToNot(HaveOccurred())
		Expect(expectAgent0ConnectorStatus(ctx).Deployed).To(BeFalse())
		Expect(recordedEvents()).To(HaveLen(1))

		// The operator of the cluster corrects the rules.
		manager.UpdateExtraConfig(ctx, util.ExtraConfig{}, logd.FromContext(ctx))

		status := expectAgent0ConnectorStatus(ctx)
		Expect(status.Deployed).To(BeTrue())
		Expect(status.Reason).To(Equal(StatusReasonDeployed))
		Expect(recordedEvents()).To(ConsistOf(ContainSubstring("Agent0ConnectorDeployed")))
		expectAgent0ConnectorResourcesToExist(ctx)
	})

	It("removes the status when the agent0-connector is disabled", func() {
		CreateDefaultOperatorConfigurationResource(ctx, k8sClient)
		_, err := newManager(true).ReconcileAgent0Connector(ctx, TriggeredByDash0OperatorConfigurationResourceReconcile)
		Expect(err).ToNot(HaveOccurred())
		Expect(expectAgent0ConnectorStatus(ctx).Deployed).To(BeTrue())

		_, err = newManager(false).ReconcileAgent0Connector(ctx, TriggeredByDash0OperatorConfigurationResourceReconcile)
		Expect(err).ToNot(HaveOccurred())

		operatorConfigurationResource := LoadOperatorConfigurationResourceOrFail(ctx, k8sClient, Default)
		Expect(operatorConfigurationResource.Status.Agent0Connector).To(BeNil())
		// Disabling the feature is an explicit action, the operator of the cluster does not need to be told about it.
		Expect(recordedEvents()).To(BeEmpty())
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

// extraConfigWithWriteVerb returns custom cluster role rules with a write verb, e.g. an invalid configuration.
func extraConfigWithWriteVerb() util.ExtraConfig {
	return util.ExtraConfig{
		Agent0ConnectorClusterRoleRules: []rbacv1.PolicyRule{
			{
				APIGroups: []string{""},
				Resources: []string{"pods"},
				Verbs:     []string{"get", "list", "delete"},
			},
			{
				NonResourceURLs: []string{"*"},
				Verbs:           []string{"get"},
			},
		},
	}
}

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
