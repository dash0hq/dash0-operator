// SPDX-FileCopyrightText: Copyright 2024 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package controller

import (
	"context"
	"encoding/json"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	dash0common "github.com/dash0hq/dash0-operator/api/operator/common"
	dash0v1alpha1 "github.com/dash0hq/dash0-operator/api/operator/v1alpha1"
	dash0v1beta1 "github.com/dash0hq/dash0-operator/api/operator/v1beta1"
	"github.com/dash0hq/dash0-operator/internal/util"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	. "github.com/dash0hq/dash0-operator/test/util"
)

var _ = Describe("The auto namespace monitoring controller", Ordered, func() {
	ctx := context.Background()

	Context("starting and stopping the namespace watch", func() {
		var (
			autoNamespaceMonitoringReconciler *AutoNamespaceMonitoringReconciler
		)

		BeforeEach(func() {
			// Bypass SetupWithManager to avoid registering a controller with the manager (which would fail with duplicate
			// name errors across tests). We only need the reconciler's Reconcile method and the namespaceWatcher state.
			autoNamespaceMonitoringReconciler = &AutoNamespaceMonitoringReconciler{
				Client:           k8sClient,
				manager:          mgr,
				namespaceWatcher: NewNamespaceWatcher(k8sClient, OperatorNamespace),
			}
		})

		AfterEach(func() {
			DeleteAllOperatorConfigurationResources(ctx, k8sClient)

			// Stop the namespace watch if it was started, to clean up the background goroutine.
			autoNamespaceMonitoringReconciler.namespaceWatcher.controllerStopFunctionLock.Lock()
			if autoNamespaceMonitoringReconciler.namespaceWatcher.controllerStopFunction != nil {
				(*autoNamespaceMonitoringReconciler.namespaceWatcher.controllerStopFunction)()
				autoNamespaceMonitoringReconciler.namespaceWatcher.controllerStopFunction = nil
			}
			autoNamespaceMonitoringReconciler.namespaceWatcher.controllerStopFunctionLock.Unlock()
		})

		It("starts the namespace watch when the operator configuration has auto-monitoring enabled", func() {
			createOperatorConfigurationResourceWithAutoMonitorNamespaces(ctx, new(true), "", nil)
			triggerAutoNamespaceMonitoringReconcilerForOperatorConfiguration(ctx, autoNamespaceMonitoringReconciler)
			Expect(autoNamespaceMonitoringReconciler.namespaceWatcher.isWatching()).To(BeTrue())
		})

		It("does not start the namespace watch when auto-monitoring is disabled", func() {
			createOperatorConfigurationResourceWithAutoMonitorNamespaces(ctx, new(false), "", nil)
			triggerAutoNamespaceMonitoringReconcilerForOperatorConfiguration(ctx, autoNamespaceMonitoringReconciler)
			Expect(autoNamespaceMonitoringReconciler.namespaceWatcher.isWatching()).To(BeFalse())
		})

		It("does not start the namespace watch when auto-monitoring is not explicitly set", func() {
			createOperatorConfigurationResourceWithAutoMonitorNamespaces(ctx, nil, "", nil)
			triggerAutoNamespaceMonitoringReconcilerForOperatorConfiguration(ctx, autoNamespaceMonitoringReconciler)
			Expect(autoNamespaceMonitoringReconciler.namespaceWatcher.isWatching()).To(BeFalse())
		})

		It("does not start the namespace watch when the operator configuration does not exist", func() {
			triggerAutoNamespaceMonitoringReconcilerForOperatorConfiguration(ctx, autoNamespaceMonitoringReconciler)
			Expect(autoNamespaceMonitoringReconciler.namespaceWatcher.isWatching()).To(BeFalse())
		})

		It("does not start the namespace watch when the operator configuration is not available", func() {
			operatorConfigurationResource := &dash0v1alpha1.Dash0OperatorConfiguration{
				ObjectMeta: metav1.ObjectMeta{
					Name: OperatorConfigurationResourceName,
				},
				Spec: dash0v1alpha1.Dash0OperatorConfigurationSpec{
					SelfMonitoring: dash0v1alpha1.SelfMonitoring{
						Enabled: ptr.To(false),
					},
					AutoMonitorNamespaces: dash0v1alpha1.AutoMonitorNamespaces{
						Enabled: ptr.To(true),
					},
					MonitoringTemplate: &dash0v1alpha1.MonitoringTemplate{},
				},
			}
			Expect(k8sClient.Create(ctx, operatorConfigurationResource)).To(Succeed())
			// deliberately not calling EnsureResourceIsMarkedAsAvailable
			triggerAutoNamespaceMonitoringReconcilerForOperatorConfiguration(ctx, autoNamespaceMonitoringReconciler)
			Expect(autoNamespaceMonitoringReconciler.namespaceWatcher.isWatching()).To(BeFalse())
		})

		It("is idempotent: reconciling multiple times while enabled does not start multiple namespace controllers", func() {
			createOperatorConfigurationResourceWithAutoMonitorNamespaces(ctx, new(true), "", nil)
			triggerAutoNamespaceMonitoringReconcilerForOperatorConfiguration(ctx, autoNamespaceMonitoringReconciler)
			Expect(autoNamespaceMonitoringReconciler.namespaceWatcher.isWatching()).To(BeTrue())
			firstStopFunction := autoNamespaceMonitoringReconciler.namespaceWatcher.controllerStopFunction

			triggerAutoNamespaceMonitoringReconcilerForOperatorConfiguration(ctx, autoNamespaceMonitoringReconciler)
			Expect(autoNamespaceMonitoringReconciler.namespaceWatcher.isWatching()).To(BeTrue())
			// The stop function pointer should be unchanged — no new controller was started.
			Expect(autoNamespaceMonitoringReconciler.namespaceWatcher.controllerStopFunction).To(BeIdenticalTo(firstStopFunction))
		})

		It("stops the namespace watch when auto-monitoring is disabled after being enabled", func() {
			createOperatorConfigurationResourceWithAutoMonitorNamespaces(ctx, new(true), "", nil)
			triggerAutoNamespaceMonitoringReconcilerForOperatorConfiguration(ctx, autoNamespaceMonitoringReconciler)
			Expect(autoNamespaceMonitoringReconciler.namespaceWatcher.isWatching()).To(BeTrue())

			DeleteAllOperatorConfigurationResources(ctx, k8sClient)
			createOperatorConfigurationResourceWithAutoMonitorNamespaces(ctx, new(false), "", nil)
			triggerAutoNamespaceMonitoringReconcilerForOperatorConfiguration(ctx, autoNamespaceMonitoringReconciler)
			Expect(autoNamespaceMonitoringReconciler.namespaceWatcher.isWatching()).To(BeFalse())
		})

		It("stops the namespace watch when the operator configuration is deleted", func() {
			createOperatorConfigurationResourceWithAutoMonitorNamespaces(ctx, new(true), "", nil)
			triggerAutoNamespaceMonitoringReconcilerForOperatorConfiguration(ctx, autoNamespaceMonitoringReconciler)
			Expect(autoNamespaceMonitoringReconciler.namespaceWatcher.isWatching()).To(BeTrue())

			DeleteAllOperatorConfigurationResources(ctx, k8sClient)
			triggerAutoNamespaceMonitoringReconcilerForOperatorConfiguration(ctx, autoNamespaceMonitoringReconciler)
			Expect(autoNamespaceMonitoringReconciler.namespaceWatcher.isWatching()).To(BeFalse())
		})

		It("stops the namespace watch when the operator configuration becomes unavailable", func() {
			createOperatorConfigurationResourceWithAutoMonitorNamespaces(ctx, new(true), "", nil)
			triggerAutoNamespaceMonitoringReconcilerForOperatorConfiguration(ctx, autoNamespaceMonitoringReconciler)
			Expect(autoNamespaceMonitoringReconciler.namespaceWatcher.isWatching()).To(BeTrue())

			opConfig := &dash0v1alpha1.Dash0OperatorConfiguration{}
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: OperatorConfigurationResourceName}, opConfig)).To(Succeed())
			opConfig.SetAvailableConditionToUnknown()
			Expect(k8sClient.Status().Update(ctx, opConfig)).To(Succeed())
			triggerAutoNamespaceMonitoringReconcilerForOperatorConfiguration(ctx, autoNamespaceMonitoringReconciler)
			Expect(autoNamespaceMonitoringReconciler.namespaceWatcher.isWatching()).To(BeFalse())
		})
	})

	Context("when instrumenting namespaces", func() {
		const (
			testAutoNamespace1 = "auto-monitor-test-ns-01"
			testAutoNamespace2 = "auto-monitor-test-ns-02"
		)

		var (
			namespaceWatcher *NamespaceWatcher

			testAutoNamespace1s = []string{
				testAutoNamespace1,
				testAutoNamespace2,
			}
		)

		BeforeAll(func() {
			EnsureNamespaceExists(ctx, k8sClient, testAutoNamespace1)
			EnsureNamespaceExists(ctx, k8sClient, testAutoNamespace2)
		})

		BeforeEach(func() {
			namespaceWatcher = NewNamespaceWatcher(k8sClient, OperatorNamespace)
		})

		AfterEach(func() {
			DeleteAllOperatorConfigurationResources(ctx, k8sClient)
			for _, namespaceName := range testAutoNamespace1s {
				// delete monitoring resources
				Expect(
					k8sClient.DeleteAllOf(
						ctx,
						&dash0v1beta1.Dash0Monitoring{},
						client.InNamespace(namespaceName))).
					To(Succeed())
				// reset namespace labels
				namespace := &corev1.Namespace{}
				Expect(k8sClient.Get(ctx, types.NamespacedName{Name: namespaceName}, namespace)).To(Succeed())
				namespace.Labels = nil
				Expect(k8sClient.Update(ctx, namespace)).To(Succeed())
			}
		})

		It("creates a Dash0Monitoring resource when autoMonitorNamespaces is enabled and the namespace labels match the default selector", func() {
			createOperatorConfigurationResourceWithAutoMonitorNamespaces(ctx, new(true), "", nil)
			triggerNamespaceWatcherReconcile(ctx, namespaceWatcher, testAutoNamespace1)

			autoMonitoringResources := listAutoMonitoringResources(ctx, testAutoNamespace1)
			Expect(autoMonitoringResources).To(HaveLen(1))
			autoMonitoringResoure := autoMonitoringResources[0]
			Expect(autoMonitoringResoure.Name).To(Equal(util.MonitoringAutoResourceName))
			Expect(autoMonitoringResoure.Labels[util.AutoMonitoringNamespaceLabel]).To(Equal("true"))
		})

		It("creates a Dash0Monitoring resources for multiple namespaces", func() {
			createOperatorConfigurationResourceWithAutoMonitorNamespaces(ctx, new(true), "", nil)
			triggerNamespaceWatcherReconcile(ctx, namespaceWatcher, testAutoNamespace1)
			triggerNamespaceWatcherReconcile(ctx, namespaceWatcher, testAutoNamespace2)
			autoMonitoringResources := listAutoMonitoringResources(ctx, testAutoNamespace1)
			Expect(autoMonitoringResources).To(HaveLen(1))
			autoMonitoringResoureNs1 := autoMonitoringResources[0]
			Expect(autoMonitoringResoureNs1.Labels[util.AutoMonitoringNamespaceLabel]).To(Equal("true"))

			autoMonitoringResources = listAutoMonitoringResources(ctx, testAutoNamespace2)
			Expect(autoMonitoringResources).To(HaveLen(1))
			autoMonitoringResoureNs2 := autoMonitoringResources[0]
			Expect(autoMonitoringResoureNs2.Labels[util.AutoMonitoringNamespaceLabel]).To(Equal("true"))
		})

		It("creates a Dash0Monitoring resource when autoMonitorNamespaces is enabled and the namespace labels match a custom selector", func() {
			// add a label to the namespace so it matches the selector
			ns := &corev1.Namespace{}
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: testAutoNamespace1}, ns)).To(Succeed())
			ns.Labels = map[string]string{"dash0.com/monitor-this-namespace": "true"}
			Expect(k8sClient.Update(ctx, ns)).To(Succeed())

			createOperatorConfigurationResourceWithAutoMonitorNamespaces(
				ctx,
				new(true),
				"dash0.com/monitor-this-namespace=true",
				nil,
			)
			triggerNamespaceWatcherReconcile(ctx, namespaceWatcher, testAutoNamespace1)

			autoMonitoringResources := listAutoMonitoringResources(ctx, testAutoNamespace1)
			Expect(autoMonitoringResources).To(HaveLen(1))
			Expect(autoMonitoringResources[0].Labels[util.AutoMonitoringNamespaceLabel]).To(Equal("true"))
		})

		It("does not create a Dash0Monitoring resource when autoMonitorNamespaces is disabled", func() {
			ns := &corev1.Namespace{}
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: testAutoNamespace1}, ns)).To(Succeed())
			Expect(k8sClient.Update(ctx, ns)).To(Succeed())

			createOperatorConfigurationResourceWithAutoMonitorNamespaces(ctx, new(false), "", nil)
			triggerNamespaceWatcherReconcile(ctx, namespaceWatcher, testAutoNamespace1)

			Expect(listAutoMonitoringResources(ctx, testAutoNamespace1)).To(BeEmpty())
		})

		It("does not create a Dash0Monitoring resource when the autoMonitorNamespaces is not enabled", func() {
			createOperatorConfigurationResourceWithAutoMonitorNamespaces(ctx, nil, "", nil)
			triggerNamespaceWatcherReconcile(ctx, namespaceWatcher, testAutoNamespace1)

			Expect(listAutoMonitoringResources(ctx, testAutoNamespace1)).To(BeEmpty())
		})

		It("does not create a Dash0Monitoring resource when namespace labels do not match the default selector", func() {
			ns := &corev1.Namespace{}
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: testAutoNamespace1}, ns)).To(Succeed())
			ns.Labels = map[string]string{"dash0.com/enable": "false"}
			Expect(k8sClient.Update(ctx, ns)).To(Succeed())

			createOperatorConfigurationResourceWithAutoMonitorNamespaces(ctx, new(true), "", nil)
			triggerNamespaceWatcherReconcile(ctx, namespaceWatcher, testAutoNamespace1)

			Expect(listAutoMonitoringResources(ctx, testAutoNamespace1)).To(BeEmpty())
		})

		It("does not create a Dash0Monitoring resource when namespace labels do not match a custom selector", func() {
			createOperatorConfigurationResourceWithAutoMonitorNamespaces(ctx, new(true), "dash0.com/monitor-this-namespace=true", nil)
			triggerNamespaceWatcherReconcile(ctx, namespaceWatcher, testAutoNamespace1)
			Expect(listAutoMonitoringResources(ctx, testAutoNamespace1)).To(BeEmpty())
		})

		It("does not create a Dash0Monitoring resource for a restricted namespace", func() {
			createOperatorConfigurationResourceWithAutoMonitorNamespaces(ctx, new(true), "", nil)
			triggerNamespaceWatcherReconcile(ctx, namespaceWatcher, "kube-system")
			Expect(listAutoMonitoringResources(ctx, "kube-system")).To(BeEmpty())
		})

		It("does not create a Dash0Monitoring resource for the operator namespace", func() {
			createOperatorConfigurationResourceWithAutoMonitorNamespaces(ctx, new(true), "", nil)
			triggerNamespaceWatcherReconcile(ctx, namespaceWatcher, OperatorNamespace)
			Expect(listAutoMonitoringResources(ctx, OperatorNamespace)).To(BeEmpty())
		})

		It("does not create a Dash0Monitoring resource when no OperatorConfiguration exists", func() {
			triggerNamespaceWatcherReconcile(ctx, namespaceWatcher, testAutoNamespace1)
			Expect(listAutoMonitoringResources(ctx, testAutoNamespace1)).To(BeEmpty())
		})

		It("does not create a Dash0Monitoring resource when the OperatorConfiguration resource is not in status available", func() {
			operatorConfiguration := createOperatorConfigurationResourceWithAutoMonitorNamespaces(ctx, new(true), "", nil)
			operatorConfiguration.EnsureResourceIsMarkedAsDegraded("reason", "message")
			Expect(k8sClient.Status().Update(ctx, operatorConfiguration)).To(Succeed())

			triggerNamespaceWatcherReconcile(ctx, namespaceWatcher, testAutoNamespace1)

			autoMonitoringResources := listAutoMonitoringResources(ctx, testAutoNamespace1)
			Expect(autoMonitoringResources).To(BeEmpty())
		})

		It("is idempotent: reconciling twice creates only one Dash0Monitoring resource", func() {
			createOperatorConfigurationResourceWithAutoMonitorNamespaces(ctx, new(true), "", nil)
			triggerNamespaceWatcherReconcile(ctx, namespaceWatcher, testAutoNamespace1)
			triggerNamespaceWatcherReconcile(ctx, namespaceWatcher, testAutoNamespace1)

			Expect(listAutoMonitoringResources(ctx, testAutoNamespace1)).To(HaveLen(1))
		})

		It("uses custom settings from the MonitoringTemplate when set", func() {
			createOperatorConfigurationResourceWithAutoMonitorNamespaces(
				ctx,
				new(true),
				"",
				&dash0v1alpha1.MonitoringTemplate{
					ObjectMeta: metav1.ObjectMeta{
						Name: "custom-auto-monitoring-resource-name",
					},
					Spec: dash0v1beta1.Dash0MonitoringSpec{
						InstrumentWorkloads: dash0v1beta1.InstrumentWorkloads{
							Mode:          dash0common.InstrumentWorkloadsModeAll,
							LabelSelector: "dash0.com/monitor-namespace=true",
							TraceContext: dash0v1beta1.TraceContext{
								Propagators: new("tracecontext,xray"),
							},
						},
						LogCollection: dash0common.LogCollection{
							Enabled: new(false),
						},
						EventCollection: dash0common.EventCollection{
							Enabled: new(false),
						},
						PrometheusScraping: dash0common.PrometheusScraping{
							Enabled: new(false),
						},
						Filter: &dash0common.Filter{
							Traces: &dash0common.TraceFilter{
								SpanFilter: []string{"span filter"},
							},
						},
						Transform: &dash0common.Transform{
							Traces: []json.RawMessage{
								[]byte(`"truncate_all(span.attributes, 1024)"`),
							},
						},
						SynchronizePersesDashboards: new(true),
						SynchronizePrometheusRules:  new(true),
					},
				},
			)

			triggerNamespaceWatcherReconcile(ctx, namespaceWatcher, testAutoNamespace1)

			autoMonitoringResources := listAutoMonitoringResources(ctx, testAutoNamespace1)
			Expect(autoMonitoringResources).To(HaveLen(1))
			autoMonitoringResoure := autoMonitoringResources[0]
			Expect(autoMonitoringResoure.Name).To(Equal("custom-auto-monitoring-resource-name"))
			Expect(autoMonitoringResoure.Spec.Export).To(BeNil())
			Expect(autoMonitoringResoure.Spec.Exports).To(BeNil())
			Expect(autoMonitoringResoure.Spec.InstrumentWorkloads.Mode).To(Equal(dash0common.InstrumentWorkloadsModeAll))
			Expect(autoMonitoringResoure.Spec.InstrumentWorkloads.LabelSelector).To(Equal("dash0.com/monitor-namespace=true"))
			Expect(*autoMonitoringResoure.Spec.InstrumentWorkloads.TraceContext.Propagators).To(Equal("tracecontext,xray"))
			Expect(*autoMonitoringResoure.Spec.EventCollection.Enabled).To(BeFalse())
			Expect(*autoMonitoringResoure.Spec.LogCollection.Enabled).To(BeFalse())
			Expect(*autoMonitoringResoure.Spec.PrometheusScraping.Enabled).To(BeFalse())
			Expect(autoMonitoringResoure.Spec.Filter.Traces.SpanFilter).To(Equal([]string{"span filter"}))
			Expect(autoMonitoringResoure.Spec.Transform.Traces[0]).To(
				Equal(json.RawMessage(`"truncate_all(span.attributes, 1024)"`)))
			Expect(*autoMonitoringResoure.Spec.SynchronizePersesDashboards).To(BeTrue())
			Expect(*autoMonitoringResoure.Spec.SynchronizePrometheusRules).To(BeTrue())
		})

		It("falls back to MonitoringAutoResourceName when MonitoringTemplate has no name", func() {
			createOperatorConfigurationResourceWithAutoMonitorNamespaces(ctx, new(true), "", nil)
			triggerNamespaceWatcherReconcile(ctx, namespaceWatcher, testAutoNamespace1)

			autoMonitoringResources := listAutoMonitoringResources(ctx, testAutoNamespace1)
			Expect(autoMonitoringResources).To(HaveLen(1))
			Expect(autoMonitoringResources[0].Name).To(Equal(util.MonitoringAutoResourceName))
		})

		It("deletes the auto Dash0Monitoring resource when the namespace opt-out label is added", func() {
			// first: namespace does not have the opt-out label, hence the monitoring resource is created
			createOperatorConfigurationResourceWithAutoMonitorNamespaces(ctx, new(true), "", nil)
			triggerNamespaceWatcherReconcile(ctx, namespaceWatcher, testAutoNamespace1)
			Expect(listAutoMonitoringResources(ctx, testAutoNamespace1)).To(HaveLen(1))

			ns := &corev1.Namespace{}
			// now set the opt-out label so namespace no longer matches the label selector
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: testAutoNamespace1}, ns)).To(Succeed())
			ns.Labels = map[string]string{"dash0.com/enable": "false"}
			Expect(k8sClient.Update(ctx, ns)).To(Succeed())

			triggerNamespaceWatcherReconcile(ctx, namespaceWatcher, testAutoNamespace1)

			Expect(listAutoMonitoringResources(ctx, testAutoNamespace1)).To(BeEmpty())
		})

		It("deletes the auto Dash0Monitoring resource when autoMonitorNamespaces is disabled", func() {
			// First: enabled, monitoring resource is created
			createOperatorConfigurationResourceWithAutoMonitorNamespaces(ctx, new(true), "", nil)
			triggerNamespaceWatcherReconcile(ctx, namespaceWatcher, testAutoNamespace1)
			Expect(listAutoMonitoringResources(ctx, testAutoNamespace1)).To(HaveLen(1))

			// now disable autoMonitorNamespaces
			DeleteAllOperatorConfigurationResources(ctx, k8sClient)
			createOperatorConfigurationResourceWithAutoMonitorNamespaces(ctx, new(false), "", nil)
			triggerNamespaceWatcherReconcile(ctx, namespaceWatcher, testAutoNamespace1)

			Expect(listAutoMonitoringResources(ctx, testAutoNamespace1)).To(BeEmpty())
		})
	})
})

func createOperatorConfigurationResourceWithAutoMonitorNamespaces(
	ctx context.Context,
	autoMonitorNamespacesEnabled *bool,
	labelSelector string,
	monitoringTemplate *dash0v1alpha1.MonitoringTemplate,
) *dash0v1alpha1.Dash0OperatorConfiguration {
	operatorConfigurationResource := &dash0v1alpha1.Dash0OperatorConfiguration{
		ObjectMeta: metav1.ObjectMeta{
			Name: OperatorConfigurationResourceName,
		},
		Spec: dash0v1alpha1.Dash0OperatorConfigurationSpec{
			SelfMonitoring: dash0v1alpha1.SelfMonitoring{
				Enabled: ptr.To(false),
			},
		},
	}

	if autoMonitorNamespacesEnabled != nil {
		operatorConfigurationResource.Spec.AutoMonitorNamespaces = dash0v1alpha1.AutoMonitorNamespaces{
			Enabled: autoMonitorNamespacesEnabled,
		}
	}
	if labelSelector != "" {
		operatorConfigurationResource.Spec.AutoMonitorNamespaces.LabelSelector = labelSelector
	}
	if monitoringTemplate != nil {
		operatorConfigurationResource.Spec.MonitoringTemplate = monitoringTemplate
	} else {
		operatorConfigurationResource.Spec.MonitoringTemplate = &dash0v1alpha1.MonitoringTemplate{}
	}

	Expect(k8sClient.Create(ctx, operatorConfigurationResource)).To(Succeed())
	operatorConfigurationResource.EnsureResourceIsMarkedAsAvailable()
	Expect(k8sClient.Status().Update(ctx, operatorConfigurationResource)).To(Succeed())
	return operatorConfigurationResource
}

func triggerAutoNamespaceMonitoringReconcilerForOperatorConfiguration(
	ctx context.Context,
	autoNamespaceMonitoringReconciler *AutoNamespaceMonitoringReconciler,
) {
	_, err := autoNamespaceMonitoringReconciler.Reconcile(ctx, reconcile.Request{
		NamespacedName: types.NamespacedName{Name: OperatorConfigurationResourceName},
	})
	Expect(err).NotTo(HaveOccurred())
}

func triggerNamespaceWatcherReconcile(
	ctx context.Context,
	namespaceWatcher *NamespaceWatcher,
	namespaceName string,
) {
	_, err := namespaceWatcher.Reconcile(ctx, reconcile.Request{
		NamespacedName: types.NamespacedName{Name: namespaceName},
	})
	Expect(err).NotTo(HaveOccurred())
}

func listAutoMonitoringResources(ctx context.Context, namespaceName string) []dash0v1beta1.Dash0Monitoring {
	list := &dash0v1beta1.Dash0MonitoringList{}
	Expect(k8sClient.List(ctx, list,
		client.InNamespace(namespaceName),
		client.MatchingLabels{util.AutoMonitoringNamespaceLabel: "true"},
	)).To(Succeed())
	return list.Items
}
