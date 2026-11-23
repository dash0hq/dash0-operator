// SPDX-FileCopyrightText: Copyright 2026 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package startup

import (
	"context"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/scheme"
	ctrl "sigs.k8s.io/controller-runtime"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"

	dash0v1alpha1 "github.com/dash0hq/dash0-operator/api/operator/v1alpha1"
	"github.com/dash0hq/dash0-operator/internal/util"
	"github.com/dash0hq/dash0-operator/internal/util/cluster"
	"github.com/dash0hq/dash0-operator/internal/util/logd"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	. "github.com/dash0hq/dash0-operator/test/util"
)

var _ = Describe("Watching the instrumentation settings of the operator configuration resource", func() {
	ctx := context.Background()

	withInitContainerMemoryLimit := func(memoryLimit string) dash0v1alpha1.Dash0OperatorConfigurationSpec {
		spec := OperatorConfigurationResourceDefaultSpec
		spec.Components = &dash0v1alpha1.OperatorConfigurationComponents{
			InstrumentationInitContainer: &dash0v1alpha1.InstrumentationInitContainerSettings{
				Resources: &dash0v1alpha1.ContainerResources{
					Limits: corev1.ResourceList{corev1.ResourceMemory: resource.MustParse(memoryLimit)},
				},
			},
		}
		return spec
	}

	memoryLimit := func(config *util.ClusterInstrumentationConfig) string {
		limits := config.InstrumentationInitContainerResources().Limits
		return limits.Memory().String()
	}

	BeforeEach(func() {
		previousStartupTasksK8sClient := startupTasksK8sClient
		startupTasksK8sClient = k8sClient
		DeferCleanup(func() {
			startupTasksK8sClient = previousStartupTasksK8sClient
		})
	})

	AfterEach(func() {
		DeleteAllOperatorConfigurationResources(ctx, k8sClient)
	})

	It("should apply the existing value at startup and follow the changes of the resource", func() {
		CreateOperatorConfigurationResourceWithSpec(ctx, k8sClient, withInitContainerMemoryLimit("256Mi"))
		extraConfig := util.ExtraConfigDefaults
		extraConfig.InstrumentationInitContainerResources = util.ResourceRequirementsWithGoMemLimit{
			Limits: corev1.ResourceList{corev1.ResourceMemory: resource.MustParse("128Mi")},
		}
		config := util.NewClusterInstrumentationConfig(
			util.Images{}, util.PossibleCollectorUrls{}, "", extraConfig, dash0v1alpha1.InstrumentationDeliveryAuto,
			nil, false, false, false,
			true,
			true,
		)
		mgr, err := ctrl.NewManager(cfg, ctrl.Options{
			Scheme:  scheme.Scheme,
			Metrics: metricsserver.Options{BindAddress: "0"},
		})
		Expect(err).ToNot(HaveOccurred())

		Expect(watchOperatorConfigurationInstrumentationSettings(ctx, mgr, config, util.ExtraConfigDefaultsFor(true, false))).
			To(Succeed())
		// The value is available before the manager and its cache have been started.
		Expect(memoryLimit(config)).To(Equal("256Mi"))
		// The resources the setting leaves out are taken from the defaults, not from the extra config map.
		requests := config.InstrumentationInitContainerResources().Requests
		Expect(requests.Cpu().String()).To(Equal("100m"))

		managerCtx, cancelManager := context.WithCancel(ctx)
		DeferCleanup(cancelManager)
		go func() {
			defer GinkgoRecover()
			Expect(mgr.Start(managerCtx)).To(Succeed())
		}()

		operatorConfigurationResource := LoadOperatorConfigurationResourceOrFail(ctx, k8sClient, Default)
		operatorConfigurationResource.Spec = withInitContainerMemoryLimit("512Mi")
		Expect(k8sClient.Update(ctx, operatorConfigurationResource)).To(Succeed())
		Eventually(func(g Gomega) {
			g.Expect(memoryLimit(config)).To(Equal("512Mi"))
		}).Should(Succeed())

		DeleteAllOperatorConfigurationResources(ctx, k8sClient)
		Eventually(func(g Gomega) {
			g.Expect(memoryLimit(config)).To(Equal("128Mi"))
		}).Should(Succeed())
	})

	It("should apply the instrumentation delivery of the resource once the informer has delivered it", func() {
		spec := OperatorConfigurationResourceDefaultSpec
		spec.InstrumentWorkloads.InstrumentationDelivery = dash0v1alpha1.InstrumentationDeliveryImageVolume
		CreateOperatorConfigurationResourceWithSpec(ctx, k8sClient, spec)
		config := util.NewClusterInstrumentationConfig(
			util.Images{}, util.PossibleCollectorUrls{}, "", util.ExtraConfigDefaults,
			dash0v1alpha1.InstrumentationDeliveryInitContainer, nil, false, false, false,
			true,
			true,
		)
		mgr, err := ctrl.NewManager(cfg, ctrl.Options{
			Scheme:  scheme.Scheme,
			Metrics: metricsserver.Options{BindAddress: "0"},
		})
		Expect(err).ToNot(HaveOccurred())

		Expect(watchOperatorConfigurationInstrumentationSettings(ctx, mgr, config, util.ExtraConfigDefaults)).
			To(Succeed())
		// Before the manager has started, the Kubernetes versions are not known yet, the flag value applies.
		Expect(config.ResolveInstrumentationDelivery()).To(Equal(cluster.ResolvedInstrumentationDeliveryInitContainer))

		managerCtx, cancelManager := context.WithCancel(ctx)
		DeferCleanup(cancelManager)
		go func() {
			defer GinkgoRecover()
			Expect(mgr.Start(managerCtx)).To(Succeed())
		}()

		Eventually(func(g Gomega) {
			g.Expect(config.ResolveInstrumentationDelivery()).To(Equal(cluster.ResolvedInstrumentationDeliveryImageVolume))
		}).Should(Succeed())

		operatorConfigurationResource := LoadOperatorConfigurationResourceOrFail(ctx, k8sClient, Default)
		operatorConfigurationResource.Spec.InstrumentWorkloads.InstrumentationDelivery =
			dash0v1alpha1.InstrumentationDeliveryInitContainer
		Expect(k8sClient.Update(ctx, operatorConfigurationResource)).To(Succeed())
		Eventually(func(g Gomega) {
			g.Expect(config.ResolveInstrumentationDelivery()).To(Equal(cluster.ResolvedInstrumentationDeliveryInitContainer))
		}).Should(Succeed())
	})

	Describe("waiting for the Helm managed operator configuration resource at startup", func() {
		It("should not wait if the operator configuration resource is not managed via Helm", func() {
			config := util.NewClusterInstrumentationConfig(
				util.Images{}, util.PossibleCollectorUrls{}, "", util.ExtraConfigDefaults, "", nil, false, false, false,
				true,
				true,
			)
			Expect(newWaitForHelmManagedInstrumentationSettings(nil, k8sClient, config, util.ExtraConfigDefaults)).
				To(BeNil())
		})

		It("should apply the settings that the leader has written, instead of the settings of a previous version", func() {
			// The resource as written by the previous operator version, before the Helm upgrade.
			previousSpec := withInitContainerMemoryLimit("256Mi")
			previousSpec.InstrumentWorkloads.InstrumentationDelivery = dash0v1alpha1.InstrumentationDeliveryInitContainer
			_, err := CreateOperatorConfigurationResource(ctx, k8sClient, &dash0v1alpha1.Dash0OperatorConfiguration{
				ObjectMeta: metav1.ObjectMeta{Name: util.OperatorConfigurationAutoResourceName},
				Spec:       previousSpec,
			})
			Expect(err).ToNot(HaveOccurred())

			config := util.NewClusterInstrumentationConfig(
				util.Images{}, util.PossibleCollectorUrls{}, "", util.ExtraConfigDefaults,
				dash0v1alpha1.InstrumentationDeliveryImageVolume, nil, false, false, false,
				true,
				true,
			)
			Expect(updateOperatorConfigurationInstrumentationSettings(
				ctx, k8sClient, config, util.ExtraConfigDefaults, true, logd.Discard(),
			)).To(Succeed())
			Expect(config.ResolveInstrumentationDelivery()).To(Equal(cluster.ResolvedInstrumentationDeliveryInitContainer))
			Expect(memoryLimit(config)).To(Equal("256Mi"))

			readyCheckExecuter := NewReadyCheckExecuter(k8sClient, OperatorNamespace, OperatorWebhookServiceName)
			readyCheckExecuter.bypassWebhookCheck = true
			handler := NewAutoOperatorConfigurationResourceHandler(
				k8sClient,
				readyCheckExecuter,
				OperatorConfigurationValues{
					Endpoint:                EndpointDash0Test,
					Token:                   AuthorizationTokenTest,
					InstrumentationDelivery: dash0v1alpha1.InstrumentationDeliveryImageVolume,
				},
				util.ExtraConfig{},
			)
			_, err = handler.CreateOrUpdateOperatorConfigurationResource(ctx, logd.Discard())
			Expect(err).ToNot(HaveOccurred())

			waitForOperatorConfigurationResource :=
				newWaitForHelmManagedInstrumentationSettings(handler, k8sClient, config, util.ExtraConfigDefaults)
			Expect(waitForOperatorConfigurationResource).ToNot(BeNil())
			waitHasReturned := make(chan struct{})
			go func() {
				defer GinkgoRecover()
				waitForOperatorConfigurationResource(ctx, logd.Discard())
				close(waitHasReturned)
			}()

			// The resource is only updated once this replica has become leader.
			Consistently(waitHasReturned, 500*time.Millisecond).ShouldNot(BeClosed())

			handler.NotifyOperatorManagerJustBecameLeader(ctx, logd.Discard())
			Eventually(waitHasReturned, 10*time.Second).Should(BeClosed())
			// No Eventually: the settings need to be in effect as soon as the wait returns.
			Expect(config.ResolveInstrumentationDelivery()).To(Equal(cluster.ResolvedInstrumentationDeliveryImageVolume))
			Expect(memoryLimit(config)).ToNot(Equal("256Mi"))
		})
	})
})
