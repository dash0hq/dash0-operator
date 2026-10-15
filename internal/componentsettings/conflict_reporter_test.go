// SPDX-FileCopyrightText: Copyright 2026 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package componentsettings

import (
	"context"
	"errors"
	"strings"

	"github.com/go-logr/logr/funcr"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	dash0v1alpha1 "github.com/dash0hq/dash0-operator/api/operator/v1alpha1"
	"github.com/dash0hq/dash0-operator/internal/util"
	"github.com/dash0hq/dash0-operator/internal/util/logd"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	. "github.com/dash0hq/dash0-operator/test/util"
)

const (
	conflictMessage   = " overrides the custom Helm value"
	noConflictMessage = "no longer overrides the custom Helm value"
)

var _ = Describe("The component settings conflict reporter", func() {
	ctx := context.Background()

	var logged []string
	var logger logd.Logger

	BeforeEach(func() {
		logged = nil
		logger = logd.NewLogger(funcr.New(func(_ string, args string) {
			logged = append(logged, args)
		}, funcr.Options{}))
	})

	newClient := func(objects ...client.Object) client.Client {
		scheme := runtime.NewScheme()
		Expect(dash0v1alpha1.AddToScheme(scheme)).To(Succeed())
		return fake.NewClientBuilder().WithScheme(scheme).WithObjects(objects...).Build()
	}

	signalControlResource := func() *dash0v1alpha1.Dash0SignalControl {
		return &dash0v1alpha1.Dash0SignalControl{
			ObjectMeta: metav1.ObjectMeta{Name: "signal-control"},
			Spec: dash0v1alpha1.Dash0SignalControlSpec{
				Components: &dash0v1alpha1.SignalControlComponents{
					EdgeProxy: &dash0v1alpha1.EdgeProxySettings{Replicas: new(int32(3))},
				},
			},
		}
	}

	conflictingExtraConfig := func() util.ExtraConfig {
		extraConfig := util.ExtraConfigDefaults
		extraConfig.EdgeProxyReplicas = 4
		return extraConfig
	}

	countMessages := func(message string) int {
		count := 0
		for _, entry := range logged {
			if strings.Contains(entry, message) {
				count++
			}
		}
		return count
	}

	newReporter := func(
		k8sClient client.Client,
		extraConfig util.ExtraConfig,
		signalControlEnabled bool,
	) *ConflictReporter {
		return NewConflictReporter(
			k8sClient, extraConfig, util.ExtraConfigDefaults, signalControlEnabled, NewLeaderElectionAwareMock(true))
	}

	It("should not log anything without custom resources", func() {
		reporter := newReporter(newClient(), conflictingExtraConfig(), true)
		reporter.Report(ctx, logger)
		Expect(logged).To(BeEmpty())
	})

	It("should not log anything without conflicts", func() {
		reporter := newReporter(newClient(signalControlResource()), util.ExtraConfigDefaults, true)
		reporter.Report(ctx, logger)
		Expect(logged).To(BeEmpty())
	})

	It("should only log on the leader, and as soon as the replica has become leader", func() {
		leaderElectionAware := NewLeaderElectionAwareMock(false)
		reporter := NewConflictReporter(
			newClient(signalControlResource()),
			util.ExtraConfigDefaults,
			util.ExtraConfigDefaults,
			true,
			leaderElectionAware,
		)
		reporter.UpdateExtraConfig(ctx, conflictingExtraConfig(), logger)
		reporter.Report(ctx, logger)
		Expect(logged).To(BeEmpty())

		leaderElectionAware.SetLeader(true)
		reporter.NotifyOperatorManagerJustBecameLeader(ctx, logger)
		Expect(countMessages(conflictMessage)).To(Equal(1))
	})

	It("should log a conflict only once while it stays the same", func() {
		reporter := newReporter(newClient(signalControlResource()), conflictingExtraConfig(), true)
		reporter.Report(ctx, logger)
		Expect(countMessages(conflictMessage)).To(Equal(1))
		Expect(logged[0]).To(ContainSubstring(
			"Dash0SignalControl spec.components.edgeProxy.replicas overrides the custom Helm value " +
				"operator.signalControl.edgeProxy.replicas."))

		reporter.Report(ctx, logger)
		Expect(logged).To(HaveLen(1))
	})

	It("should log when a conflict has been resolved by a change of the extra config map", func() {
		reporter := newReporter(newClient(signalControlResource()), conflictingExtraConfig(), true)
		reporter.Report(ctx, logger)
		Expect(countMessages(conflictMessage)).To(Equal(1))

		reporter.UpdateExtraConfig(ctx, util.ExtraConfigDefaults, logger)
		Expect(logged).To(HaveLen(2))
		Expect(logged[1]).To(ContainSubstring(
			"Dash0SignalControl spec.components.edgeProxy.replicas no longer overrides the custom Helm value " +
				"operator.signalControl.edgeProxy.replicas."))

		reporter.UpdateExtraConfig(ctx, util.ExtraConfigDefaults, logger)
		Expect(logged).To(HaveLen(2))
	})

	It("should log a conflict again when its values have changed", func() {
		reporter := newReporter(newClient(signalControlResource()), conflictingExtraConfig(), true)
		reporter.Report(ctx, logger)
		Expect(countMessages(conflictMessage)).To(Equal(1))

		extraConfig := conflictingExtraConfig()
		extraConfig.EdgeProxyReplicas = 5
		reporter.UpdateExtraConfig(ctx, extraConfig, logger)
		Expect(countMessages(conflictMessage)).To(Equal(2))
		Expect(countMessages(noConflictMessage)).To(Equal(0))
		Expect(logged[1]).To(ContainSubstring(`"helmSetting"="5"`))
	})

	It("should log the conflicts of the Signal Control resource", func() {
		reporter := newReporter(newClient(signalControlResource()), conflictingExtraConfig(), true)
		reporter.Report(ctx, logger)
		Expect(countMessages(conflictMessage)).To(Equal(1))
		Expect(logged[0]).To(ContainSubstring("spec.components.edgeProxy.replicas"))
		Expect(logged[0]).To(ContainSubstring("Dash0SignalControl"))
	})

	It("should not report anything if the Signal Control resource cannot be looked up", func() {
		reporter := newReporter(newClient(signalControlResource()), conflictingExtraConfig(), true)
		reporter.Report(ctx, logger)
		Expect(countMessages(conflictMessage)).To(Equal(1))

		reporter.client = &signalControlListErrorClient{
			Client: newClient(),
			err:    errors.New("connection refused"),
		}
		reporter.Report(ctx, logger)
		Expect(countMessages(conflictMessage)).To(Equal(1))
		Expect(countMessages(noConflictMessage)).To(Equal(0))
	})

	It("should ignore the Signal Control resource if Signal Control is disabled", func() {
		reporter := newReporter(newClient(signalControlResource()), conflictingExtraConfig(), false)
		reporter.Report(ctx, logger)
		Expect(logged).To(BeEmpty())
	})

	It("should be safe to call on a nil reporter", func() {
		var reporter *ConflictReporter
		reporter.Report(ctx, logger)
		Expect(logged).To(BeEmpty())
	})
})

type signalControlListErrorClient struct {
	client.Client
	err error
}

func (c *signalControlListErrorClient) List(
	ctx context.Context,
	list client.ObjectList,
	opts ...client.ListOption,
) error {
	if _, isSignalControlList := list.(*dash0v1alpha1.Dash0SignalControlList); isSignalControlList {
		return c.err
	}
	return c.Client.List(ctx, list, opts...)
}
