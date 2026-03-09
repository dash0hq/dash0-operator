// SPDX-FileCopyrightText: Copyright 2026 Dash0 Inc.
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

var _ = Describe("The auto-namespace-monitoring controller", Ordered, func() {
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

			testAutoNamespaces = []string{
				testAutoNamespace1,
				testAutoNamespace2,
			}

			manualMonitoringResourceName = types.NamespacedName{
				Name:      "existing-manual-monitoring-resource",
				Namespace: testAutoNamespace1,
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
			for _, namespaceName := range testAutoNamespaces {
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
			DeleteMonitoringResourceByName(ctx, k8sClient, manualMonitoringResourceName, false)
		})

		It("creates a Dash0Monitoring resource when autoMonitorNamespaces is enabled and the namespace labels match the default selector", func() {
			createOperatorConfigurationResourceWithAutoMonitorNamespaces(ctx, new(true), "", nil)
			triggerNamespaceWatcherReconcile(ctx, namespaceWatcher, testAutoNamespace1)

			autoMonitoringResources := listMonitoringResources(ctx, testAutoNamespace1)
			Expect(autoMonitoringResources).To(HaveLen(1))
			autoMonitoringResource := autoMonitoringResources[0]
			Expect(autoMonitoringResource.Name).To(Equal(util.MonitoringAutoResourceDefaultName))
			Expect(autoMonitoringResource.Labels[util.AutoMonitoredNamespaceLabel]).To(Equal("true"))
		})

		It("creates a Dash0Monitoring resources for multiple namespaces", func() {
			createOperatorConfigurationResourceWithAutoMonitorNamespaces(ctx, new(true), "", nil)
			triggerNamespaceWatcherReconcile(ctx, namespaceWatcher, testAutoNamespace1)
			triggerNamespaceWatcherReconcile(ctx, namespaceWatcher, testAutoNamespace2)
			autoMonitoringResources := listMonitoringResources(ctx, testAutoNamespace1)
			Expect(autoMonitoringResources).To(HaveLen(1))
			autoMonitoringResourceNs1 := autoMonitoringResources[0]
			Expect(autoMonitoringResourceNs1.Labels[util.AutoMonitoredNamespaceLabel]).To(Equal("true"))

			autoMonitoringResources = listMonitoringResources(ctx, testAutoNamespace2)
			Expect(autoMonitoringResources).To(HaveLen(1))
			autoMonitoringResourceNs2 := autoMonitoringResources[0]
			Expect(autoMonitoringResourceNs2.Labels[util.AutoMonitoredNamespaceLabel]).To(Equal("true"))
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

			autoMonitoringResources := listMonitoringResources(ctx, testAutoNamespace1)
			Expect(autoMonitoringResources).To(HaveLen(1))
			Expect(autoMonitoringResources[0].Labels[util.AutoMonitoredNamespaceLabel]).To(Equal("true"))
		})

		It("does not create a Dash0Monitoring resource when autoMonitorNamespaces is disabled", func() {
			ns := &corev1.Namespace{}
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: testAutoNamespace1}, ns)).To(Succeed())
			Expect(k8sClient.Update(ctx, ns)).To(Succeed())

			createOperatorConfigurationResourceWithAutoMonitorNamespaces(ctx, new(false), "", nil)
			triggerNamespaceWatcherReconcile(ctx, namespaceWatcher, testAutoNamespace1)

			Expect(listMonitoringResources(ctx, testAutoNamespace1)).To(BeEmpty())
		})

		It("does not create a Dash0Monitoring resource when the autoMonitorNamespaces is not enabled", func() {
			createOperatorConfigurationResourceWithAutoMonitorNamespaces(ctx, nil, "", nil)
			triggerNamespaceWatcherReconcile(ctx, namespaceWatcher, testAutoNamespace1)

			Expect(listMonitoringResources(ctx, testAutoNamespace1)).To(BeEmpty())
		})

		It("does not create a Dash0Monitoring resource when namespace labels do not match the default selector", func() {
			ns := &corev1.Namespace{}
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: testAutoNamespace1}, ns)).To(Succeed())
			ns.Labels = map[string]string{"dash0.com/enable": "false"}
			Expect(k8sClient.Update(ctx, ns)).To(Succeed())

			createOperatorConfigurationResourceWithAutoMonitorNamespaces(ctx, new(true), "", nil)
			triggerNamespaceWatcherReconcile(ctx, namespaceWatcher, testAutoNamespace1)

			Expect(listMonitoringResources(ctx, testAutoNamespace1)).To(BeEmpty())
		})

		It("does not create a Dash0Monitoring resource when namespace labels do not match a custom selector", func() {
			createOperatorConfigurationResourceWithAutoMonitorNamespaces(ctx, new(true), "dash0.com/monitor-this-namespace=true", nil)
			triggerNamespaceWatcherReconcile(ctx, namespaceWatcher, testAutoNamespace1)
			Expect(listMonitoringResources(ctx, testAutoNamespace1)).To(BeEmpty())
		})

		It("does not create a Dash0Monitoring resource for a restricted namespace", func() {
			createOperatorConfigurationResourceWithAutoMonitorNamespaces(ctx, new(true), "", nil)
			triggerNamespaceWatcherReconcile(ctx, namespaceWatcher, "kube-system")
			Expect(listMonitoringResources(ctx, "kube-system")).To(BeEmpty())
		})

		It("does not create a Dash0Monitoring resource for the operator namespace", func() {
			createOperatorConfigurationResourceWithAutoMonitorNamespaces(ctx, new(true), "", nil)
			triggerNamespaceWatcherReconcile(ctx, namespaceWatcher, OperatorNamespace)
			Expect(listMonitoringResources(ctx, OperatorNamespace)).To(BeEmpty())
		})

		It("does not create a Dash0Monitoring resource when no OperatorConfiguration exists", func() {
			triggerNamespaceWatcherReconcile(ctx, namespaceWatcher, testAutoNamespace1)
			Expect(listMonitoringResources(ctx, testAutoNamespace1)).To(BeEmpty())
		})

		It("does not create a Dash0Monitoring resource when the OperatorConfiguration resource is not in status available", func() {
			operatorConfiguration := createOperatorConfigurationResourceWithAutoMonitorNamespaces(ctx, new(true), "", nil)
			operatorConfiguration.EnsureResourceIsMarkedAsDegraded("reason", "message")
			Expect(k8sClient.Status().Update(ctx, operatorConfiguration)).To(Succeed())

			triggerNamespaceWatcherReconcile(ctx, namespaceWatcher, testAutoNamespace1)

			autoMonitoringResources := listMonitoringResources(ctx, testAutoNamespace1)
			Expect(autoMonitoringResources).To(BeEmpty())
		})

		It("is idempotent: reconciling twice creates only one Dash0Monitoring resource", func() {
			createOperatorConfigurationResourceWithAutoMonitorNamespaces(ctx, new(true), "", nil)
			triggerNamespaceWatcherReconcile(ctx, namespaceWatcher, testAutoNamespace1)
			triggerNamespaceWatcherReconcile(ctx, namespaceWatcher, testAutoNamespace1)

			monitoringResources := listMonitoringResources(ctx, testAutoNamespace1)
			Expect(monitoringResources).To(HaveLen(1))
			monitoringResource := monitoringResources[0]
			Expect(monitoringResource.Name).To(Equal(util.MonitoringAutoResourceDefaultName))
			Expect(monitoringResource.Labels[util.AutoMonitoredNamespaceLabel]).To(Equal("true"))
		})

		It("does not create a Dash0Monitoring with dash0.com/auto-monitored-namespace when the namespace already contains non-auto monitoring resource", func() {
			createOperatorConfigurationResourceWithAutoMonitorNamespaces(ctx, new(true), "", nil)
			EnsureMonitoringResourceWithSpecExistsInNamespace(
				ctx,
				k8sClient,
				MonitoringResourceDefaultSpec,
				manualMonitoringResourceName,
			)
			triggerNamespaceWatcherReconcile(ctx, namespaceWatcher, testAutoNamespace1)

			monitoringResources := listMonitoringResources(ctx, testAutoNamespace1)
			Expect(monitoringResources).To(HaveLen(1))
			monitoringResource := monitoringResources[0]
			Expect(monitoringResource.Name).To(Equal(manualMonitoringResourceName.Name))
			_, hasAutoMonitoredLabel := monitoringResource.Labels[util.AutoMonitoredNamespaceLabel]
			Expect(hasAutoMonitoredLabel).To(BeFalse())
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

			autoMonitoringResources := listMonitoringResources(ctx, testAutoNamespace1)
			Expect(autoMonitoringResources).To(HaveLen(1))
			autoMonitoringResource := autoMonitoringResources[0]
			Expect(autoMonitoringResource.Name).To(Equal("custom-auto-monitoring-resource-name"))
			Expect(autoMonitoringResource.Spec.Export).To(BeNil())
			Expect(autoMonitoringResource.Spec.Exports).To(BeNil())
			Expect(autoMonitoringResource.Spec.InstrumentWorkloads.Mode).To(Equal(dash0common.InstrumentWorkloadsModeAll))
			Expect(autoMonitoringResource.Spec.InstrumentWorkloads.LabelSelector).To(Equal("dash0.com/monitor-namespace=true"))
			Expect(*autoMonitoringResource.Spec.InstrumentWorkloads.TraceContext.Propagators).To(Equal("tracecontext,xray"))
			Expect(*autoMonitoringResource.Spec.EventCollection.Enabled).To(BeFalse())
			Expect(*autoMonitoringResource.Spec.LogCollection.Enabled).To(BeFalse())
			Expect(*autoMonitoringResource.Spec.PrometheusScraping.Enabled).To(BeFalse())
			Expect(autoMonitoringResource.Spec.Filter.Traces.SpanFilter).To(Equal([]string{"span filter"}))
			Expect(autoMonitoringResource.Spec.Transform.Traces[0]).To(
				Equal(json.RawMessage(`"truncate_all(span.attributes, 1024)"`)))
			Expect(*autoMonitoringResource.Spec.SynchronizePersesDashboards).To(BeTrue())
			Expect(*autoMonitoringResource.Spec.SynchronizePrometheusRules).To(BeTrue())
		})

		It("updates the existing auto-monitoring resource if the monitoring template has changed", func() {
			operatorConfiguration := createOperatorConfigurationResourceWithAutoMonitorNamespaces(ctx, new(true), "", nil)
			operatorConfiguration.Spec.MonitoringTemplate.Labels = make(map[string]string)
			operatorConfiguration.Spec.MonitoringTemplate.Labels["old-label-1"] = "label-value-1"
			operatorConfiguration.Spec.MonitoringTemplate.Labels["old-label-2"] = "label-value-2"
			operatorConfiguration.Spec.MonitoringTemplate.Annotations = make(map[string]string)
			operatorConfiguration.Spec.MonitoringTemplate.Annotations["old-annotation-1"] = "annotation value 1"
			operatorConfiguration.Spec.MonitoringTemplate.Annotations["old-annotation-2"] = "annotation value 2"
			triggerNamespaceWatcherReconcile(ctx, namespaceWatcher, testAutoNamespace1)

			operatorConfiguration.Spec.MonitoringTemplate.Labels["new-label"] = "new-label-value"
			delete(operatorConfiguration.Spec.MonitoringTemplate.Labels, "old-label-2")
			operatorConfiguration.Spec.MonitoringTemplate.Annotations["new-annotation"] = "new annotation value"
			delete(operatorConfiguration.Spec.MonitoringTemplate.Annotations, "old-annotation-2")
			operatorConfiguration.Spec.MonitoringTemplate.Spec.EventCollection.Enabled = new(false)
			operatorConfiguration.Spec.MonitoringTemplate.Spec.Filter = &dash0common.Filter{
				Traces: &dash0common.TraceFilter{
					SpanFilter: []string{"span filter"},
				},
			}
			operatorConfiguration.Spec.MonitoringTemplate.Spec.InstrumentWorkloads.Mode =
				dash0common.InstrumentWorkloadsModeNone
			operatorConfiguration.Spec.MonitoringTemplate.Spec.InstrumentWorkloads.LabelSelector =
				"dash0com/instrument-workloads=true"
			operatorConfiguration.Spec.MonitoringTemplate.Spec.InstrumentWorkloads.TraceContext.Propagators =
				new("tracecontext,xray")
			operatorConfiguration.Spec.MonitoringTemplate.Spec.LogCollection.Enabled = new(false)
			operatorConfiguration.Spec.MonitoringTemplate.Spec.SynchronizePersesDashboards = new(false)
			operatorConfiguration.Spec.MonitoringTemplate.Spec.SynchronizePrometheusRules = new(false)
			operatorConfiguration.Spec.MonitoringTemplate.Spec.Transform = &dash0common.Transform{
				Traces: []json.RawMessage{
					[]byte(`"truncate_all(span.attributes, 1024)"`),
				},
			}
			operatorConfiguration.Spec.MonitoringTemplate.Spec.PrometheusScraping.Enabled = new(false)

			Expect(k8sClient.Update(ctx, operatorConfiguration)).To(Succeed())

			// reconcile again and verify the monitoring resource reflects the new monitoring template
			triggerNamespaceWatcherReconcile(ctx, namespaceWatcher, testAutoNamespace1)

			monitoringResources := listMonitoringResources(ctx, testAutoNamespace1)
			Expect(monitoringResources).To(HaveLen(1))
			monitoringResource := monitoringResources[0]
			Expect(monitoringResource.Labels).To(HaveLen(3))
			Expect(monitoringResource.Labels[util.AutoMonitoredNamespaceLabel]).To(Equal("true"))
			Expect(monitoringResource.Labels["old-label-1"]).To(Equal("label-value-1"))
			Expect(monitoringResource.Labels["new-label"]).To(Equal("new-label-value"))
			Expect(monitoringResource.Annotations).To(HaveLen(2))
			Expect(monitoringResource.Annotations["old-annotation-1"]).To(Equal("annotation value 1"))
			Expect(monitoringResource.Annotations["new-annotation"]).To(Equal("new annotation value"))
			Expect(*monitoringResource.Spec.EventCollection.Enabled).To(BeFalse())
			Expect(*monitoringResource.Spec.Filter).To(Equal(dash0common.Filter{
				ErrorMode: dash0common.FilterTransformErrorModeIgnore,
				Traces: &dash0common.TraceFilter{
					SpanFilter: []string{"span filter"},
				},
			}))
			Expect(monitoringResource.Spec.InstrumentWorkloads.Mode).To(Equal(dash0common.InstrumentWorkloadsModeNone))
			Expect(monitoringResource.Spec.InstrumentWorkloads.LabelSelector).To(Equal("dash0com/instrument-workloads=true"))
			Expect(*monitoringResource.Spec.InstrumentWorkloads.TraceContext.Propagators).To(Equal("tracecontext,xray"))
			Expect(*monitoringResource.Spec.LogCollection.Enabled).To(BeFalse())
			Expect(*monitoringResource.Spec.SynchronizePersesDashboards).To(BeFalse())
			Expect(*monitoringResource.Spec.SynchronizePrometheusRules).To(BeFalse())
			Expect(*monitoringResource.Spec.Transform).To(Equal(dash0common.Transform{
				ErrorMode: new(dash0common.FilterTransformErrorModeIgnore),
				Traces: []json.RawMessage{
					[]byte(`"truncate_all(span.attributes, 1024)"`),
				},
			}))
			Expect(*monitoringResource.Spec.PrometheusScraping.Enabled).To(BeFalse())
		})

		It("falls back to MonitoringAutoResourceName when MonitoringTemplate has no name", func() {
			createOperatorConfigurationResourceWithAutoMonitorNamespaces(ctx, new(true), "", nil)
			triggerNamespaceWatcherReconcile(ctx, namespaceWatcher, testAutoNamespace1)

			autoMonitoringResources := listMonitoringResources(ctx, testAutoNamespace1)
			Expect(autoMonitoringResources).To(HaveLen(1))
			Expect(autoMonitoringResources[0].Name).To(Equal(util.MonitoringAutoResourceDefaultName))
		})

		It("deletes the auto Dash0Monitoring resource when the namespace opt-out label is added", func() {
			// first: namespace does not have the opt-out label, hence the monitoring resource is created
			createOperatorConfigurationResourceWithAutoMonitorNamespaces(ctx, new(true), "", nil)
			triggerNamespaceWatcherReconcile(ctx, namespaceWatcher, testAutoNamespace1)
			Expect(listMonitoringResources(ctx, testAutoNamespace1)).To(HaveLen(1))

			ns := &corev1.Namespace{}
			// now set the opt-out label so namespace no longer matches the label selector
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: testAutoNamespace1}, ns)).To(Succeed())
			ns.Labels = map[string]string{"dash0.com/enable": "false"}
			Expect(k8sClient.Update(ctx, ns)).To(Succeed())

			triggerNamespaceWatcherReconcile(ctx, namespaceWatcher, testAutoNamespace1)

			Expect(listMonitoringResources(ctx, testAutoNamespace1)).To(BeEmpty())
		})

		It("deletes the auto Dash0Monitoring resource when autoMonitorNamespaces is disabled", func() {
			// First: enabled, monitoring resource is created
			createOperatorConfigurationResourceWithAutoMonitorNamespaces(ctx, new(true), "", nil)
			triggerNamespaceWatcherReconcile(ctx, namespaceWatcher, testAutoNamespace1)
			Expect(listMonitoringResources(ctx, testAutoNamespace1)).To(HaveLen(1))

			// now disable autoMonitorNamespaces
			DeleteAllOperatorConfigurationResources(ctx, k8sClient)
			createOperatorConfigurationResourceWithAutoMonitorNamespaces(ctx, new(false), "", nil)
			triggerNamespaceWatcherReconcile(ctx, namespaceWatcher, testAutoNamespace1)

			Expect(listMonitoringResources(ctx, testAutoNamespace1)).To(BeEmpty())
		})
	})

	Context("comparing monitoring resource monitoring templates", func() {
		type compareMonitoringResourceAndMonitoringTemplateTest struct {
			template dash0v1alpha1.MonitoringTemplate
			resource *dash0v1beta1.Dash0Monitoring
			verify   func(hasBeenUpdated bool, resource *dash0v1beta1.Dash0Monitoring)
		}
		autoLabel := map[string]string{util.AutoMonitoredNamespaceLabel: util.TrueString}

		DescribeTable("compareMonitoringResourceAndMonitoringTemplate",
			func(t compareMonitoringResourceAndMonitoringTemplateTest) {
				result := compareMonitoringResourceToMonitoringTemplateAndUpdate(t.template, t.resource)
				t.verify(result, t.resource)
			},
			Entry("returns false when resource already matches the template",
				compareMonitoringResourceAndMonitoringTemplateTest{
					template: dash0v1alpha1.MonitoringTemplate{
						ObjectMeta: metav1.ObjectMeta{
							Labels:      map[string]string{"label-key": "label-value"},
							Annotations: map[string]string{"annotation-key": "annotation-value"},
						},
						Spec: dash0v1beta1.Dash0MonitoringSpec{
							InstrumentWorkloads: dash0v1beta1.InstrumentWorkloads{
								Mode: dash0common.InstrumentWorkloadsModeAll,
							},
							LogCollection: dash0common.LogCollection{Enabled: ptr.To(false)},
						},
					},
					resource: &dash0v1beta1.Dash0Monitoring{
						ObjectMeta: metav1.ObjectMeta{
							Labels: map[string]string{
								"label-key":                      "label-value",
								util.AutoMonitoredNamespaceLabel: util.TrueString,
							},
							Annotations: map[string]string{"annotation-key": "annotation-value"},
						},
						Spec: dash0v1beta1.Dash0MonitoringSpec{
							InstrumentWorkloads: dash0v1beta1.InstrumentWorkloads{
								Mode: dash0common.InstrumentWorkloadsModeAll,
							},
							LogCollection: dash0common.LogCollection{Enabled: ptr.To(false)},
						},
					},
					verify: func(hasBeenUpdated bool, _ *dash0v1beta1.Dash0Monitoring) {
						Expect(hasBeenUpdated).To(BeFalse())
					},
				}),
			Entry("returns true and adds the auto-monitored-namespace label when it is missing",
				compareMonitoringResourceAndMonitoringTemplateTest{
					template: dash0v1alpha1.MonitoringTemplate{},
					resource: &dash0v1beta1.Dash0Monitoring{},
					verify: func(hasBeenUpdated bool, r *dash0v1beta1.Dash0Monitoring) {
						Expect(hasBeenUpdated).To(BeTrue())
						Expect(r.Labels[util.AutoMonitoredNamespaceLabel]).To(Equal(util.TrueString))
					},
				}),
			Entry("returns true and updates labels when they differ",
				compareMonitoringResourceAndMonitoringTemplateTest{
					template: dash0v1alpha1.MonitoringTemplate{
						ObjectMeta: metav1.ObjectMeta{
							Labels: map[string]string{"new-label": "new-value"},
						},
					},
					resource: &dash0v1beta1.Dash0Monitoring{
						ObjectMeta: metav1.ObjectMeta{
							Labels: map[string]string{
								util.AutoMonitoredNamespaceLabel: util.TrueString,
								"old-label":                      "old-value",
							},
						},
					},
					verify: func(hasBeenUpdated bool, r *dash0v1beta1.Dash0Monitoring) {
						Expect(hasBeenUpdated).To(BeTrue())
						Expect(r.Labels).To(Equal(map[string]string{
							"new-label":                      "new-value",
							util.AutoMonitoredNamespaceLabel: util.TrueString,
						}))
					},
				}),
			Entry("returns true and updates annotations when they differ",
				compareMonitoringResourceAndMonitoringTemplateTest{
					template: dash0v1alpha1.MonitoringTemplate{
						ObjectMeta: metav1.ObjectMeta{
							Annotations: map[string]string{"new-annotation": "new-value"},
						},
					},
					resource: &dash0v1beta1.Dash0Monitoring{
						ObjectMeta: metav1.ObjectMeta{
							Labels:      autoLabel,
							Annotations: map[string]string{"old-annotation": "old-value"},
						},
					},
					verify: func(hasBeenUpdated bool, r *dash0v1beta1.Dash0Monitoring) {
						Expect(hasBeenUpdated).To(BeTrue())
						Expect(r.Annotations).To(Equal(map[string]string{"new-annotation": "new-value"}))
					},
				}),
			Entry("returns true and updates InstrumentWorkloads.Mode when it differs",
				compareMonitoringResourceAndMonitoringTemplateTest{
					template: dash0v1alpha1.MonitoringTemplate{
						Spec: dash0v1beta1.Dash0MonitoringSpec{
							InstrumentWorkloads: dash0v1beta1.InstrumentWorkloads{Mode: dash0common.InstrumentWorkloadsModeNone},
						},
					},
					resource: &dash0v1beta1.Dash0Monitoring{
						ObjectMeta: metav1.ObjectMeta{Labels: autoLabel},
						Spec: dash0v1beta1.Dash0MonitoringSpec{
							InstrumentWorkloads: dash0v1beta1.InstrumentWorkloads{Mode: dash0common.InstrumentWorkloadsModeAll},
						},
					},
					verify: func(hasBeenUpdated bool, r *dash0v1beta1.Dash0Monitoring) {
						Expect(hasBeenUpdated).To(BeTrue())
						Expect(r.Spec.InstrumentWorkloads.Mode).To(Equal(dash0common.InstrumentWorkloadsModeNone))
					},
				}),
			Entry("returns true and updates InstrumentWorkloads.LabelSelector when it differs",
				compareMonitoringResourceAndMonitoringTemplateTest{
					template: dash0v1alpha1.MonitoringTemplate{
						Spec: dash0v1beta1.Dash0MonitoringSpec{
							InstrumentWorkloads: dash0v1beta1.InstrumentWorkloads{LabelSelector: "app=new-selector"},
						},
					},
					resource: &dash0v1beta1.Dash0Monitoring{
						ObjectMeta: metav1.ObjectMeta{Labels: autoLabel},
						Spec: dash0v1beta1.Dash0MonitoringSpec{
							InstrumentWorkloads: dash0v1beta1.InstrumentWorkloads{LabelSelector: "app=old-selector"},
						},
					},
					verify: func(hasBeenUpdated bool, r *dash0v1beta1.Dash0Monitoring) {
						Expect(hasBeenUpdated).To(BeTrue())
						Expect(r.Spec.InstrumentWorkloads.LabelSelector).To(Equal("app=new-selector"))
					},
				}),
			Entry("returns true and updates InstrumentWorkloads.TraceContext.Propagators when they differ",
				compareMonitoringResourceAndMonitoringTemplateTest{
					template: dash0v1alpha1.MonitoringTemplate{
						Spec: dash0v1beta1.Dash0MonitoringSpec{
							InstrumentWorkloads: dash0v1beta1.InstrumentWorkloads{
								TraceContext: dash0v1beta1.TraceContext{Propagators: ptr.To("tracecontext,xray")},
							},
						},
					},
					resource: &dash0v1beta1.Dash0Monitoring{ObjectMeta: metav1.ObjectMeta{Labels: autoLabel}},
					verify: func(hasBeenUpdated bool, r *dash0v1beta1.Dash0Monitoring) {
						Expect(hasBeenUpdated).To(BeTrue())
						Expect(*r.Spec.InstrumentWorkloads.TraceContext.Propagators).To(Equal("tracecontext,xray"))
					},
				}),
			Entry("returns true and updates LogCollection.Enabled when it differs",
				compareMonitoringResourceAndMonitoringTemplateTest{
					template: dash0v1alpha1.MonitoringTemplate{
						Spec: dash0v1beta1.Dash0MonitoringSpec{LogCollection: dash0common.LogCollection{Enabled: ptr.To(false)}},
					},
					resource: &dash0v1beta1.Dash0Monitoring{
						ObjectMeta: metav1.ObjectMeta{Labels: autoLabel},
						Spec:       dash0v1beta1.Dash0MonitoringSpec{LogCollection: dash0common.LogCollection{Enabled: ptr.To(true)}},
					},
					verify: func(hasBeenUpdated bool, r *dash0v1beta1.Dash0Monitoring) {
						Expect(hasBeenUpdated).To(BeTrue())
						Expect(*r.Spec.LogCollection.Enabled).To(BeFalse())
					},
				}),
			Entry("returns true and updates EventCollection.Enabled when it differs",
				compareMonitoringResourceAndMonitoringTemplateTest{
					template: dash0v1alpha1.MonitoringTemplate{
						Spec: dash0v1beta1.Dash0MonitoringSpec{EventCollection: dash0common.EventCollection{Enabled: ptr.To(false)}},
					},
					resource: &dash0v1beta1.Dash0Monitoring{
						ObjectMeta: metav1.ObjectMeta{Labels: autoLabel},
						Spec:       dash0v1beta1.Dash0MonitoringSpec{EventCollection: dash0common.EventCollection{Enabled: ptr.To(true)}},
					},
					verify: func(hasBeenUpdated bool, r *dash0v1beta1.Dash0Monitoring) {
						Expect(hasBeenUpdated).To(BeTrue())
						Expect(*r.Spec.EventCollection.Enabled).To(BeFalse())
					},
				}),
			Entry("returns true and updates PrometheusScraping.Enabled when it differs",
				compareMonitoringResourceAndMonitoringTemplateTest{
					template: dash0v1alpha1.MonitoringTemplate{
						Spec: dash0v1beta1.Dash0MonitoringSpec{PrometheusScraping: dash0common.PrometheusScraping{Enabled: ptr.To(false)}},
					},
					resource: &dash0v1beta1.Dash0Monitoring{
						ObjectMeta: metav1.ObjectMeta{Labels: autoLabel},
						Spec:       dash0v1beta1.Dash0MonitoringSpec{PrometheusScraping: dash0common.PrometheusScraping{Enabled: ptr.To(true)}},
					},
					verify: func(hasBeenUpdated bool, r *dash0v1beta1.Dash0Monitoring) {
						Expect(hasBeenUpdated).To(BeTrue())
						Expect(*r.Spec.PrometheusScraping.Enabled).To(BeFalse())
					},
				}),
			Entry("returns true and updates Filter when it differs",
				compareMonitoringResourceAndMonitoringTemplateTest{
					template: dash0v1alpha1.MonitoringTemplate{
						Spec: dash0v1beta1.Dash0MonitoringSpec{
							Filter: &dash0common.Filter{Traces: &dash0common.TraceFilter{SpanFilter: []string{"span filter"}}},
						},
					},
					resource: &dash0v1beta1.Dash0Monitoring{ObjectMeta: metav1.ObjectMeta{Labels: autoLabel}},
					verify: func(hasBeenUpdated bool, r *dash0v1beta1.Dash0Monitoring) {
						Expect(hasBeenUpdated).To(BeTrue())
						Expect(r.Spec.Filter.Traces.SpanFilter).To(Equal([]string{"span filter"}))
					},
				}),
			Entry("returns true and updates Transform when it differs",
				compareMonitoringResourceAndMonitoringTemplateTest{
					template: dash0v1alpha1.MonitoringTemplate{
						Spec: dash0v1beta1.Dash0MonitoringSpec{
							Transform: &dash0common.Transform{
								Traces: []json.RawMessage{[]byte(`"truncate_all(span.attributes, 1024)"`)},
							},
						},
					},
					resource: &dash0v1beta1.Dash0Monitoring{ObjectMeta: metav1.ObjectMeta{Labels: autoLabel}},
					verify: func(hasBeenUpdated bool, r *dash0v1beta1.Dash0Monitoring) {
						Expect(hasBeenUpdated).To(BeTrue())
						Expect(r.Spec.Transform.Traces[0]).To(Equal(json.RawMessage(`"truncate_all(span.attributes, 1024)"`)))
					},
				}),
			Entry("returns true and updates SynchronizePersesDashboards when it differs",
				compareMonitoringResourceAndMonitoringTemplateTest{
					template: dash0v1alpha1.MonitoringTemplate{
						Spec: dash0v1beta1.Dash0MonitoringSpec{SynchronizePersesDashboards: ptr.To(false)},
					},
					resource: &dash0v1beta1.Dash0Monitoring{
						ObjectMeta: metav1.ObjectMeta{Labels: autoLabel},
						Spec:       dash0v1beta1.Dash0MonitoringSpec{SynchronizePersesDashboards: ptr.To(true)},
					},
					verify: func(hasBeenUpdated bool, r *dash0v1beta1.Dash0Monitoring) {
						Expect(hasBeenUpdated).To(BeTrue())
						Expect(*r.Spec.SynchronizePersesDashboards).To(BeFalse())
					},
				}),
			Entry("returns true and updates SynchronizePrometheusRules when it differs",
				compareMonitoringResourceAndMonitoringTemplateTest{
					template: dash0v1alpha1.MonitoringTemplate{
						Spec: dash0v1beta1.Dash0MonitoringSpec{SynchronizePrometheusRules: ptr.To(false)},
					},
					resource: &dash0v1beta1.Dash0Monitoring{
						ObjectMeta: metav1.ObjectMeta{Labels: autoLabel},
						Spec:       dash0v1beta1.Dash0MonitoringSpec{SynchronizePrometheusRules: ptr.To(true)},
					},
					verify: func(hasBeenUpdated bool, r *dash0v1beta1.Dash0Monitoring) {
						Expect(hasBeenUpdated).To(BeTrue())
						Expect(*r.Spec.SynchronizePrometheusRules).To(BeFalse())
					},
				}),
		)
	})

	Context("comparing monitoring templates", func() {
		type compareMonitoringTemplatesTest struct {
			t1     *dash0v1alpha1.MonitoringTemplate
			t2     *dash0v1alpha1.MonitoringTemplate
			expect bool
		}

		DescribeTable("compareMonitoringTemplates",
			func(t compareMonitoringTemplatesTest) {
				Expect(compareMonitoringTemplates(t.t1, t.t2)).To(Equal(t.expect))
			},
			Entry("both nil → not changed", compareMonitoringTemplatesTest{t1: nil, t2: nil, expect: false}),
			Entry("t1 nil, t2 non-nil → changed",
				compareMonitoringTemplatesTest{t1: nil, t2: &dash0v1alpha1.MonitoringTemplate{}, expect: true}),
			Entry("t1 non-nil, t2 nil → changed",
				compareMonitoringTemplatesTest{t1: &dash0v1alpha1.MonitoringTemplate{}, t2: nil, expect: true}),
			Entry("both empty → not changed", compareMonitoringTemplatesTest{
				t1:     &dash0v1alpha1.MonitoringTemplate{},
				t2:     &dash0v1alpha1.MonitoringTemplate{},
				expect: false,
			}),
			Entry("both identical (labels + annotations + spec) → not changed",
				compareMonitoringTemplatesTest{
					t1: &dash0v1alpha1.MonitoringTemplate{
						ObjectMeta: metav1.ObjectMeta{
							Labels:      map[string]string{"label-key": "label-value"},
							Annotations: map[string]string{"annotation-key": "annotation-value"},
						},
						Spec: dash0v1beta1.Dash0MonitoringSpec{
							InstrumentWorkloads: dash0v1beta1.InstrumentWorkloads{Mode: dash0common.InstrumentWorkloadsModeAll},
							LogCollection:       dash0common.LogCollection{Enabled: ptr.To(false)},
						},
					},
					t2: &dash0v1alpha1.MonitoringTemplate{
						ObjectMeta: metav1.ObjectMeta{
							Labels:      map[string]string{"label-key": "label-value"},
							Annotations: map[string]string{"annotation-key": "annotation-value"},
						},
						Spec: dash0v1beta1.Dash0MonitoringSpec{
							InstrumentWorkloads: dash0v1beta1.InstrumentWorkloads{Mode: dash0common.InstrumentWorkloadsModeAll},
							LogCollection:       dash0common.LogCollection{Enabled: ptr.To(false)},
						},
					},
					expect: false,
				}),
			Entry("labels differ → changed",
				compareMonitoringTemplatesTest{
					t1:     &dash0v1alpha1.MonitoringTemplate{ObjectMeta: metav1.ObjectMeta{Labels: map[string]string{"key": "value-1"}}},
					t2:     &dash0v1alpha1.MonitoringTemplate{ObjectMeta: metav1.ObjectMeta{Labels: map[string]string{"key": "value-2"}}},
					expect: true,
				}),
			Entry("annotations differ → changed",
				compareMonitoringTemplatesTest{
					t1:     &dash0v1alpha1.MonitoringTemplate{ObjectMeta: metav1.ObjectMeta{Annotations: map[string]string{"key": "value-1"}}},
					t2:     &dash0v1alpha1.MonitoringTemplate{ObjectMeta: metav1.ObjectMeta{Annotations: map[string]string{"key": "value-2"}}},
					expect: true,
				}),
			Entry("InstrumentWorkloads.Mode differs → changed",
				compareMonitoringTemplatesTest{
					t1: &dash0v1alpha1.MonitoringTemplate{Spec: dash0v1beta1.Dash0MonitoringSpec{
						InstrumentWorkloads: dash0v1beta1.InstrumentWorkloads{Mode: dash0common.InstrumentWorkloadsModeAll},
					}},
					t2: &dash0v1alpha1.MonitoringTemplate{Spec: dash0v1beta1.Dash0MonitoringSpec{
						InstrumentWorkloads: dash0v1beta1.InstrumentWorkloads{Mode: dash0common.InstrumentWorkloadsModeNone},
					}},
					expect: true,
				}),
			Entry("InstrumentWorkloads.LabelSelector differs → changed",
				compareMonitoringTemplatesTest{
					t1: &dash0v1alpha1.MonitoringTemplate{Spec: dash0v1beta1.Dash0MonitoringSpec{
						InstrumentWorkloads: dash0v1beta1.InstrumentWorkloads{LabelSelector: "app=selector-1"},
					}},
					t2: &dash0v1alpha1.MonitoringTemplate{Spec: dash0v1beta1.Dash0MonitoringSpec{
						InstrumentWorkloads: dash0v1beta1.InstrumentWorkloads{LabelSelector: "app=selector-2"},
					}},
					expect: true,
				}),
			Entry("InstrumentWorkloads.TraceContext.Propagators differ → changed",
				compareMonitoringTemplatesTest{
					t1: &dash0v1alpha1.MonitoringTemplate{Spec: dash0v1beta1.Dash0MonitoringSpec{
						InstrumentWorkloads: dash0v1beta1.InstrumentWorkloads{
							TraceContext: dash0v1beta1.TraceContext{Propagators: ptr.To("tracecontext")},
						},
					}},
					t2: &dash0v1alpha1.MonitoringTemplate{Spec: dash0v1beta1.Dash0MonitoringSpec{
						InstrumentWorkloads: dash0v1beta1.InstrumentWorkloads{
							TraceContext: dash0v1beta1.TraceContext{Propagators: ptr.To("tracecontext,xray")},
						},
					}},
					expect: true,
				}),
			Entry("LogCollection.Enabled differs → changed",
				compareMonitoringTemplatesTest{
					t1: &dash0v1alpha1.MonitoringTemplate{Spec: dash0v1beta1.Dash0MonitoringSpec{
						LogCollection: dash0common.LogCollection{Enabled: ptr.To(true)},
					}},
					t2: &dash0v1alpha1.MonitoringTemplate{Spec: dash0v1beta1.Dash0MonitoringSpec{
						LogCollection: dash0common.LogCollection{Enabled: ptr.To(false)},
					}},
					expect: true,
				}),
			Entry("EventCollection.Enabled differs → changed",
				compareMonitoringTemplatesTest{
					t1: &dash0v1alpha1.MonitoringTemplate{Spec: dash0v1beta1.Dash0MonitoringSpec{
						EventCollection: dash0common.EventCollection{Enabled: ptr.To(true)},
					}},
					t2: &dash0v1alpha1.MonitoringTemplate{Spec: dash0v1beta1.Dash0MonitoringSpec{
						EventCollection: dash0common.EventCollection{Enabled: ptr.To(false)},
					}},
					expect: true,
				}),
			Entry("PrometheusScraping.Enabled differs → changed",
				compareMonitoringTemplatesTest{
					t1: &dash0v1alpha1.MonitoringTemplate{Spec: dash0v1beta1.Dash0MonitoringSpec{
						PrometheusScraping: dash0common.PrometheusScraping{Enabled: ptr.To(true)},
					}},
					t2: &dash0v1alpha1.MonitoringTemplate{Spec: dash0v1beta1.Dash0MonitoringSpec{
						PrometheusScraping: dash0common.PrometheusScraping{Enabled: ptr.To(false)},
					}},
					expect: true,
				}),
			Entry("Filter differs → changed",
				compareMonitoringTemplatesTest{
					t1: &dash0v1alpha1.MonitoringTemplate{Spec: dash0v1beta1.Dash0MonitoringSpec{
						Filter: &dash0common.Filter{Traces: &dash0common.TraceFilter{SpanFilter: []string{"filter-1"}}},
					}},
					t2: &dash0v1alpha1.MonitoringTemplate{Spec: dash0v1beta1.Dash0MonitoringSpec{
						Filter: &dash0common.Filter{Traces: &dash0common.TraceFilter{SpanFilter: []string{"filter-2"}}},
					}},
					expect: true,
				}),
			Entry("Transform differs → changed",
				compareMonitoringTemplatesTest{
					t1: &dash0v1alpha1.MonitoringTemplate{Spec: dash0v1beta1.Dash0MonitoringSpec{
						Transform: &dash0common.Transform{Traces: []json.RawMessage{[]byte(`"transform-1"`)}},
					}},
					t2: &dash0v1alpha1.MonitoringTemplate{Spec: dash0v1beta1.Dash0MonitoringSpec{
						Transform: &dash0common.Transform{Traces: []json.RawMessage{[]byte(`"transform-2"`)}},
					}},
					expect: true,
				}),
			Entry("SynchronizePersesDashboards differs → changed",
				compareMonitoringTemplatesTest{
					t1:     &dash0v1alpha1.MonitoringTemplate{Spec: dash0v1beta1.Dash0MonitoringSpec{SynchronizePersesDashboards: ptr.To(true)}},
					t2:     &dash0v1alpha1.MonitoringTemplate{Spec: dash0v1beta1.Dash0MonitoringSpec{SynchronizePersesDashboards: ptr.To(false)}},
					expect: true,
				}),
			Entry("SynchronizePrometheusRules differs → changed",
				compareMonitoringTemplatesTest{
					t1:     &dash0v1alpha1.MonitoringTemplate{Spec: dash0v1beta1.Dash0MonitoringSpec{SynchronizePrometheusRules: ptr.To(true)}},
					t2:     &dash0v1alpha1.MonitoringTemplate{Spec: dash0v1beta1.Dash0MonitoringSpec{SynchronizePrometheusRules: ptr.To(false)}},
					expect: true,
				}),
		)

		It("does not mutate the input templates", func() {
			t1 := &dash0v1alpha1.MonitoringTemplate{
				ObjectMeta: metav1.ObjectMeta{Labels: map[string]string{"key": "value-1"}},
			}
			t2 := &dash0v1alpha1.MonitoringTemplate{
				ObjectMeta: metav1.ObjectMeta{Labels: map[string]string{"key": "value-2"}},
			}
			compareMonitoringTemplates(t1, t2)
			Expect(t1.Labels).To(Equal(map[string]string{"key": "value-1"}))
			Expect(t2.Labels).To(Equal(map[string]string{"key": "value-2"}))
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

func listMonitoringResources(ctx context.Context, namespaceName string) []dash0v1beta1.Dash0Monitoring {
	list := &dash0v1beta1.Dash0MonitoringList{}
	Expect(k8sClient.List(ctx, list,
		client.InNamespace(namespaceName),
	)).To(Succeed())
	return list.Items
}
