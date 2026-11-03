// SPDX-FileCopyrightText: Copyright 2026 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package startup

import (
	"context"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	"k8s.io/client-go/kubernetes/scheme"
	ctrl "sigs.k8s.io/controller-runtime"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"

	dash0v1alpha1 "github.com/dash0hq/dash0-operator/api/operator/v1alpha1"
	"github.com/dash0hq/dash0-operator/internal/util"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	. "github.com/dash0hq/dash0-operator/test/util"
)

var _ = Describe("Watching the init container resources of the operator configuration resource", func() {
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
		)
		mgr, err := ctrl.NewManager(cfg, ctrl.Options{
			Scheme:  scheme.Scheme,
			Metrics: metricsserver.Options{BindAddress: "0"},
		})
		Expect(err).ToNot(HaveOccurred())

		Expect(watchInstrumentationInitContainerResources(ctx, mgr, config, util.ExtraConfigDefaultsFor(true, false))).
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
})
