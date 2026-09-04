// SPDX-FileCopyrightText: Copyright 2026 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package agent0connector

import (
	"context"
	"errors"
	"fmt"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/tools/events"
	"k8s.io/utils/ptr"
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

func newManager(enabledViaHelm bool) *Agent0ConnectorManager {
	return newManagerWithExtraConfig(enabledViaHelm, util.ExtraConfig{})
}

func newManagerWithExtraConfig(enabledViaHelm bool, extraConfig util.ExtraConfig) *Agent0ConnectorManager {
	eventRecorder = events.NewFakeRecorder(10)
	return NewAgent0ConnectorManager(k8sClient, enabledViaHelm, extraConfig, false, newResourceManager(), eventRecorder)
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

var _ = Describe("The agent0-connector failure reason", func() {
	forbidden := apierrors.NewForbidden(
		schema.GroupResource{Group: "rbac.authorization.k8s.io", Resource: "clusterroles"},
		"dash0-operator-agent0-connector-cr",
		errors.New("user \"system:serviceaccount:dash0-system:dash0-operator-sa\" (groups=[]) is attempting to grant "+
			"RBAC permissions not currently held"),
	)

	DescribeTable(
		"maps a reconcile error to the identifier reported in the status",
		func(err error, expectedReason string) {
			Expect(agent0ConnectorFailureReason(err)).To(Equal(expectedReason))
		},
		Entry("invalid cluster role rules", a0cresources.ErrInvalidClusterRoleRules,
			StatusReasonInvalidClusterRoleRules),
		Entry("missing authorization token", a0cresources.ErrNoAuthorizationToken,
			StatusReasonNoAuthorizationToken),
		Entry("the API server rejecting the cluster role", forbidden,
			StatusReasonOperatorMissingPermissions),
		// The error travels through the resource manager, so the check has to survive wrapping.
		Entry("a wrapped rejection", fmt.Errorf("cannot create the cluster role: %w", forbidden),
			StatusReasonOperatorMissingPermissions),
		Entry("any other error", errors.New("connection refused"), StatusReasonReconcileFailed),
	)

	It("maps forbidden", func() {
		message := agent0ConnectorFailureMessage(StatusReasonOperatorMissingPermissions, forbidden)
		Expect(message).To(ContainSubstring("privilege escalation prevention"))
		Expect(message).To(ContainSubstring(forbidden.Error()))
		Expect(message).ToNot(Equal(forbidden.Error()))
	})

	It("reports any other failure with the error itself", func() {
		err := errors.New("connection refused")
		Expect(agent0ConnectorFailureMessage(StatusReasonReconcileFailed, err)).To(Equal("connection refused"))
	})
})

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

	It("removes the agent0-connector resources when it is disabled in the operator configuration resource", func() {
		CreateDefaultOperatorConfigurationResource(ctx, k8sClient)
		manager := newManager(true)
		_, err := manager.ReconcileAgent0Connector(ctx, TriggeredByDash0OperatorConfigurationResourceReconcile)
		Expect(err).ToNot(HaveOccurred())
		expectAgent0ConnectorResourcesToExist(ctx)
		Expect(expectAgent0ConnectorStatus(ctx).Deployed).To(BeTrue())

		disableAgent0ConnectorInOperatorConfigurationResource(ctx)
		hasBeenReconciled, err := manager.ReconcileAgent0Connector(ctx, TriggeredByDash0OperatorConfigurationResourceReconcile)

		Expect(err).ToNot(HaveOccurred())
		Expect(hasBeenReconciled).To(BeTrue())
		expectAgent0ConnectorResourcesToNotExist(ctx)
		operatorConfigurationResource := LoadOperatorConfigurationResourceOrFail(ctx, k8sClient, Default)
		Expect(operatorConfigurationResource.Status.Agent0Connector).To(BeNil())
	})

	It("deploys the agent0-connector when it is explicitly enabled in the operator configuration resource", func() {
		CreateOperatorConfigurationResourceWithSpec(ctx, k8sClient, operatorConfigurationSpecWithAgent0Connector(true))

		hasBeenReconciled, err := newManager(true).ReconcileAgent0Connector(ctx, TriggeredByDash0OperatorConfigurationResourceReconcile)

		Expect(err).ToNot(HaveOccurred())
		Expect(hasBeenReconciled).To(BeTrue())
		expectAgent0ConnectorResourcesToExist(ctx)
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

	It("does not reconcile when a reconciliation is already in progress, but does not lose the trigger", func() {
		CreateDefaultOperatorConfigurationResource(ctx, k8sClient)
		manager := newManager(true)

		// Occupy the manager's reconcile guard and trigger a reconciliation from within it, the way a watch event or
		// an extra config map update would arrive while a reconciliation is running.
		executions := 0
		var skippedHasBeenReconciled bool
		var skippedErr error
		_, err := manager.reconcileGuard.Run(func() (bool, error) {
			executions++
			if executions == 1 {
				skippedHasBeenReconciled, skippedErr =
					manager.ReconcileAgent0Connector(ctx, TriggeredByWatchEvent)
			}
			return true, nil
		}, nil)

		Expect(err).ToNot(HaveOccurred())
		Expect(skippedErr).ToNot(HaveOccurred())
		Expect(skippedHasBeenReconciled).To(BeFalse())
		// The reconciliation was not executed, so no resources were created ...
		expectAgent0ConnectorResourcesToNotExist(ctx)
		// ... but the trigger was recorded and the guard repeated the reconciliation once, instead of dropping it.
		Expect(executions).To(Equal(2))
	})
})

// operatorConfigurationSpecWithAgent0Connector returns the default operator configuration spec with an explicit value
// for spec.agent0Connector.enabled.
func operatorConfigurationSpecWithAgent0Connector(enabled bool) dash0v1alpha1.Dash0OperatorConfigurationSpec {
	spec := OperatorConfigurationResourceDefaultSpec
	spec.Agent0Connector = dash0v1alpha1.Agent0Connector{Enabled: &enabled}
	return spec
}

// disableAgent0ConnectorInOperatorConfigurationResource sets spec.agent0Connector.enabled to false on the operator
// configuration resource in the cluster, which is how a user opts out of the agent0-connector.
func disableAgent0ConnectorInOperatorConfigurationResource(ctx context.Context) {
	GinkgoHelper()
	operatorConfigurationResource := LoadOperatorConfigurationResourceOrFail(ctx, k8sClient, Default)
	operatorConfigurationResource.Spec.Agent0Connector.Enabled = ptr.To(false)
	Expect(k8sClient.Update(ctx, operatorConfigurationResource)).To(Succeed())
}

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
