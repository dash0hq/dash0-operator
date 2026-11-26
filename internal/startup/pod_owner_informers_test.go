// SPDX-FileCopyrightText: Copyright 2026 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package startup

import (
	"context"
	"errors"

	"k8s.io/client-go/kubernetes/scheme"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"

	"github.com/dash0hq/dash0-operator/internal/util/logd"
	"github.com/dash0hq/dash0-operator/internal/workloads"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	. "github.com/dash0hq/dash0-operator/test/util"
)

type failingReader struct{}

func (failingReader) Get(context.Context, client.ObjectKey, client.Object, ...client.GetOption) error {
	return errors.New("the live reader must not be used")
}

func (failingReader) List(context.Context, client.ObjectList, ...client.ListOption) error {
	return errors.New("the live reader must not be used")
}

var _ = Describe("Resolving pod owners with the cache of the operator manager", func() {
	ctx := context.Background()

	It("should start the owner informers lazily and serve the owners of a pod from the cache", func() {
		EnsureTestNamespaceExists(ctx, k8sClient)
		mgr, err := ctrl.NewManager(cfg, ctrl.Options{
			Scheme:  scheme.Scheme,
			Metrics: metricsserver.Options{BindAddress: "0"},
			Cache:   newManagerCacheOptions(OperatorNamespace),
		})
		Expect(err).ToNot(HaveOccurred())
		managerCtx, cancelManager := context.WithCancel(ctx)
		DeferCleanup(cancelManager)
		go func() {
			defer GinkgoRecover()
			Expect(mgr.Start(managerCtx)).To(Succeed())
		}()
		Expect(mgr.GetCache().WaitForCacheSync(managerCtx)).To(BeTrue())

		deployment := BasicDeployment(TestNamespaceName, UniqueName(DeploymentNamePrefix))
		AddLabel(&deployment.ObjectMeta, "deployment-label", "value")
		CreateWorkload(ctx, k8sClient, deployment)
		replicaSet, pod := CreateReplicaSetAndPodControlledByDeployment(
			ctx,
			k8sClient,
			deployment,
			BasicPod(TestNamespaceName, UniqueName(PodNamePrefix)),
		)
		DeferCleanup(func() {
			DeleteAllCreatedObjects(ctx, k8sClient, []client.Object{pod, replicaSet, deployment})
		})

		Expect(deployment.ManagedFields).ToNot(BeEmpty())

		resolver := workloads.NewPodOwnerResolver(mgr.GetClient(), failingReader{}, mgr.GetCache())
		Eventually(func(g Gomega) {
			owners, err := resolver.Resolve(ctx, pod, logd.Discard())
			g.Expect(err).ToNot(HaveOccurred())
			g.Expect(owners).To(HaveLen(2))
			g.Expect(owners[0].Name).To(Equal(replicaSet.Name))
			g.Expect(owners[0].ManagedFields).To(BeEmpty())
			g.Expect(owners.TopLevel().UID).To(Equal(deployment.UID))
			g.Expect(owners.TopLevel().Labels).To(HaveKeyWithValue("deployment-label", "value"))
			g.Expect(owners.TopLevel().ManagedFields).To(BeEmpty())
		}).Should(Succeed())
	})
})
