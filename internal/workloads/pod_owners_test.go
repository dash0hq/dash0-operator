// SPDX-FileCopyrightText: Copyright 2026 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package workloads

import (
	"context"
	"errors"
	"sync/atomic"

	appsv1 "k8s.io/api/apps/v1"
	batchv1 "k8s.io/api/batch/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/cache"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"

	dash0common "github.com/dash0hq/dash0-operator/api/operator/common"
	dash0v1alpha1 "github.com/dash0hq/dash0-operator/api/operator/v1alpha1"
	"github.com/dash0hq/dash0-operator/internal/util"
	"github.com/dash0hq/dash0-operator/internal/util/logd"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	. "github.com/dash0hq/dash0-operator/test/util"
)

const podOwnersTestNamespace = "test-namespace"

var _ = Describe("Resolving the owners of a pod", func() {
	ctx := context.Background()

	It("should resolve the replica set and the deployment of a pod", func() {
		deployment := deploymentForOwnerTest("deployment", "deployment-uid")
		replicaSet := controlledBy(replicaSetForOwnerTest("replica-set", "replica-set-uid"), deployment)
		pod := controlledBy(BasicPod(podOwnersTestNamespace, "pod"), replicaSet)
		reader := newFakeReader(deployment, replicaSet)

		owners, err := NewPodOwnerResolver(reader, reader, nil).Resolve(ctx, pod)

		Expect(err).ToNot(HaveOccurred())
		Expect(ownerNames(owners)).To(Equal([]string{"ReplicaSet/replica-set", "Deployment/deployment"}))
		Expect(owners.TopLevel().UID).To(Equal(types.UID("deployment-uid")))
		Expect(owners.TopLevel().Labels).To(HaveKeyWithValue("deployment-label", "value"))
	})

	It("should resolve the job and the cron job of a pod", func() {
		cronJob := &batchv1.CronJob{ObjectMeta: objectMetaForOwnerTest("cron-job", "cron-job-uid")}
		job := controlledBy(&batchv1.Job{ObjectMeta: objectMetaForOwnerTest("job", "job-uid")}, cronJob)
		pod := controlledBy(BasicPod(podOwnersTestNamespace, "pod"), job)
		reader := newFakeReader(cronJob, job)

		owners, err := NewPodOwnerResolver(reader, reader, nil).Resolve(ctx, pod)

		Expect(err).ToNot(HaveOccurred())
		Expect(ownerNames(owners)).To(Equal([]string{"Job/job", "CronJob/cron-job"}))
	})

	DescribeTable("should resolve the single owner of a pod", func(owner client.Object, expectedName string) {
		pod := controlledBy(BasicPod(podOwnersTestNamespace, "pod"), owner)
		reader := newFakeReader(owner)

		owners, err := NewPodOwnerResolver(reader, reader, nil).Resolve(ctx, pod)

		Expect(err).ToNot(HaveOccurred())
		Expect(ownerNames(owners)).To(Equal([]string{expectedName}))
	},
		Entry("daemon set", &appsv1.DaemonSet{ObjectMeta: objectMetaForOwnerTest("ds", "ds-uid")}, "DaemonSet/ds"),
		Entry("stateful set", &appsv1.StatefulSet{ObjectMeta: objectMetaForOwnerTest("sts", "sts-uid")}, "StatefulSet/sts"),
		Entry("ownerless replica set", replicaSetForOwnerTest("rs", "rs-uid"), "ReplicaSet/rs"),
		Entry("ownerless job", &batchv1.Job{ObjectMeta: objectMetaForOwnerTest("job", "job-uid")}, "Job/job"),
	)

	It("should stop at an owner of an unknown type", func() {
		replicaSet := replicaSetForOwnerTest("replica-set", "replica-set-uid")
		replicaSet.OwnerReferences = []metav1.OwnerReference{{
			APIVersion: "argoproj.io/v1alpha1",
			Kind:       "Rollout",
			Name:       "rollout",
			UID:        "rollout-uid",
			Controller: new(true),
		}}
		pod := controlledBy(BasicPod(podOwnersTestNamespace, "pod"), replicaSet)
		reader := newFakeReader(replicaSet)

		owners, err := NewPodOwnerResolver(reader, reader, nil).Resolve(ctx, pod)

		Expect(err).ToNot(HaveOccurred())
		Expect(ownerNames(owners)).To(Equal([]string{"ReplicaSet/replica-set"}))
	})

	It("should not resolve anything for a pod controlled by an unknown type", func() {
		pod := PodControlledBy(podOwnersTestNamespace, "pod", metav1.TypeMeta{
			APIVersion: "core.strimzi.io/v1beta2",
			Kind:       "StrimziPodSet",
		})
		reader := newFakeReader()

		owners, err := NewPodOwnerResolver(reader, reader, nil).Resolve(ctx, pod)

		Expect(err).ToNot(HaveOccurred())
		Expect(owners).To(BeEmpty())
	})

	It("should ignore owner references that are not the controller", func() {
		replicaSet := replicaSetForOwnerTest("replica-set", "replica-set-uid")
		pod := controlledBy(BasicPod(podOwnersTestNamespace, "pod"), replicaSet)
		pod.OwnerReferences[0].Controller = nil
		reader := newFakeReader(replicaSet)

		owners, err := NewPodOwnerResolver(reader, reader, nil).Resolve(ctx, pod)

		Expect(err).ToNot(HaveOccurred())
		Expect(owners).To(BeEmpty())
	})

	It("should not resolve an owner that does not exist", func() {
		pod := PodControlledBy(podOwnersTestNamespace, "pod", util.K8sTypeMetaReplicaSet)
		reader := newFakeReader()

		owners, err := NewPodOwnerResolver(reader, reader, nil).Resolve(ctx, pod)

		Expect(err).ToNot(HaveOccurred())
		Expect(owners).To(BeEmpty())
	})

	It("should read an owner that is not in the cache yet from the live reader", func() {
		deployment := deploymentForOwnerTest("deployment", "deployment-uid")
		replicaSet := controlledBy(replicaSetForOwnerTest("replica-set", "replica-set-uid"), deployment)
		pod := controlledBy(BasicPod(podOwnersTestNamespace, "pod"), replicaSet)

		owners, err := NewPodOwnerResolver(newFakeReader(deployment), newFakeReader(deployment, replicaSet), nil).
			Resolve(ctx, pod)

		Expect(err).ToNot(HaveOccurred())
		Expect(ownerNames(owners)).To(Equal([]string{"ReplicaSet/replica-set", "Deployment/deployment"}))
	})

	It("should read an owner from the live reader if the cached read fails", func() {
		replicaSet := replicaSetForOwnerTest("replica-set", "replica-set-uid")
		pod := controlledBy(BasicPod(podOwnersTestNamespace, "pod"), replicaSet)
		failingReader := fake.NewClientBuilder().
			WithScheme(clientgoscheme.Scheme).
			WithObjects(replicaSet).
			WithInterceptorFuncs(interceptor.Funcs{
				Get: func(context.Context, client.WithWatch, client.ObjectKey, client.Object, ...client.GetOption) error {
					return errors.New("cache not synced")
				},
			}).
			Build()

		owners, err := NewPodOwnerResolver(failingReader, newFakeReader(replicaSet), nil).Resolve(ctx, pod)

		Expect(err).ToNot(HaveOccurred())
		Expect(ownerNames(owners)).To(Equal([]string{"ReplicaSet/replica-set"}))
	})

	It("should read an owner from the live reader if the cached owner has a different UID", func() {
		staleReplicaSet := replicaSetForOwnerTest("replica-set", "old-uid")
		replicaSet := replicaSetForOwnerTest("replica-set", "replica-set-uid")
		pod := controlledBy(BasicPod(podOwnersTestNamespace, "pod"), replicaSet)

		owners, err := NewPodOwnerResolver(newFakeReader(staleReplicaSet), newFakeReader(replicaSet), nil).Resolve(ctx, pod)

		Expect(err).ToNot(HaveOccurred())
		Expect(ownerNames(owners)).To(Equal([]string{"ReplicaSet/replica-set"}))
		Expect(owners.TopLevel().UID).To(Equal(types.UID("replica-set-uid")))
	})

	It("should not resolve an owner whose name has been reused by another object", func() {
		replicaSet := replicaSetForOwnerTest("replica-set", "replica-set-uid")
		pod := controlledBy(BasicPod(podOwnersTestNamespace, "pod"), replicaSet)
		reader := newFakeReader(replicaSetForOwnerTest("replica-set", "other-uid"))

		owners, err := NewPodOwnerResolver(reader, reader, nil).Resolve(ctx, pod)

		Expect(err).ToNot(HaveOccurred())
		Expect(owners).To(BeEmpty())
	})

	It("should return an error if an owner cannot be read", func() {
		replicaSet := replicaSetForOwnerTest("replica-set", "replica-set-uid")
		pod := controlledBy(BasicPod(podOwnersTestNamespace, "pod"), replicaSet)
		failingReader := fake.NewClientBuilder().
			WithScheme(clientgoscheme.Scheme).
			WithInterceptorFuncs(interceptor.Funcs{
				Get: func(context.Context, client.WithWatch, client.ObjectKey, client.Object, ...client.GetOption) error {
					return errors.New("connection refused")
				},
			}).
			Build()

		_, err := NewPodOwnerResolver(failingReader, failingReader, nil).Resolve(ctx, pod)

		Expect(err).To(MatchError(ContainSubstring("connection refused")))
	})
})

var _ = Describe("Starting the informers for the owners of a pod", func() {
	ctx := context.Background()

	It("should start the informers on the first call and read from the live reader until they have synced", func() {
		deployment := deploymentForOwnerTest("deployment", "deployment-uid")
		replicaSet := controlledBy(replicaSetForOwnerTest("replica-set", "replica-set-uid"), deployment)
		pod := controlledBy(BasicPod(podOwnersTestNamespace, "pod"), replicaSet)
		cachedReader := &countingReader{Reader: newFakeReader(deployment, replicaSet)}
		liveReader := &countingReader{Reader: newFakeReader(deployment, replicaSet)}
		informers := &fakeInformers{}
		resolver := NewPodOwnerResolver(cachedReader, liveReader, informers)

		owners, err := resolver.Resolve(ctx, pod)
		Expect(err).ToNot(HaveOccurred())
		Expect(owners).To(HaveLen(2))
		Expect(informers.requestedKinds).To(ConsistOf("CronJob", "DaemonSet", "Deployment", "Job", "ReplicaSet",
			"StatefulSet"))
		Expect(cachedReader.gets).To(Equal(0))
		Expect(liveReader.gets).To(Equal(2))

		informers.hasSynced.Store(true)
		owners, err = resolver.Resolve(ctx, pod)
		Expect(err).ToNot(HaveOccurred())
		Expect(owners).To(HaveLen(2))
		Expect(informers.requestedKinds).To(HaveLen(6))
		Expect(cachedReader.gets).To(Equal(2))
		Expect(liveReader.gets).To(Equal(2))
	})
})

type countingReader struct {
	client.Reader
	gets int
}

func (r *countingReader) Get(ctx context.Context, key client.ObjectKey, obj client.Object, opts ...client.GetOption) error {
	r.gets++
	return r.Reader.Get(ctx, key, obj, opts...)
}

type fakeInformers struct {
	cache.Informers
	requestedKinds []string
	hasSynced      atomic.Bool
}

func (f *fakeInformers) GetInformer(_ context.Context, obj client.Object, _ ...cache.InformerGetOption) (cache.Informer, error) {
	f.requestedKinds = append(f.requestedKinds, obj.GetObjectKind().GroupVersionKind().Kind)
	return &fakeInformer{hasSynced: &f.hasSynced}, nil
}

type fakeInformer struct {
	cache.Informer
	hasSynced *atomic.Bool
}

func (f *fakeInformer) HasSynced() bool {
	return f.hasSynced.Load()
}

var _ = Describe("Checking whether a pod has opted out of instrumentation", func() {
	DescribeTable("should evaluate the label selector against the labels of the pod and its owners", func(
		podLabels map[string]string,
		deploymentLabels map[string]string,
		labelSelector string,
		expectedOptOut bool,
	) {
		pod := BasicPod(podOwnersTestNamespace, "pod")
		pod.Labels = podLabels
		replicaSet := NewPartialObjectMetadata(util.K8sTypeMetaReplicaSet)
		replicaSet.Labels = podLabels
		deployment := NewPartialObjectMetadata(util.K8sTypeMetaDeployment)
		deployment.Labels = deploymentLabels
		owners := PodOwners{replicaSet, deployment}

		Expect(PodHasOptedOutOfInstrumentation(pod, owners, labelSelector)).To(Equal(expectedOptOut))
	},
		Entry("no labels",
			nil, nil, dash0common.DefaultAutoInstrumentationLabelSelector, false),
		Entry("opt-out label on the deployment",
			nil, map[string]string{"dash0.com/enable": "false"}, dash0common.DefaultAutoInstrumentationLabelSelector, true),
		Entry("opt-out label on the pod",
			map[string]string{"dash0.com/enable": "false"}, nil, dash0common.DefaultAutoInstrumentationLabelSelector, true),
		Entry("conflicting labels, the pod label wins",
			map[string]string{"dash0.com/enable": "true"},
			map[string]string{"dash0.com/enable": "false"},
			dash0common.DefaultAutoInstrumentationLabelSelector,
			false,
		),
		Entry("opt-in selector, opt-in label on the deployment",
			nil, map[string]string{"dash0.com/enable": "true"}, "dash0.com/enable=true", false),
		Entry("opt-in selector, no opt-in label",
			nil, nil, "dash0.com/enable=true", true),
		Entry("selector combining labels from the pod and the deployment",
			map[string]string{"team": "a"}, map[string]string{"tier": "backend"}, "team=a,tier=backend", false),
	)

	It("should only evaluate the pod labels for an ownerless pod", func() {
		pod := PodWithOptOutLabel(podOwnersTestNamespace, "pod")
		Expect(PodHasOptedOutOfInstrumentation(pod, nil, dash0common.DefaultAutoInstrumentationLabelSelector)).
			To(BeTrue())
		Expect(PodHasOptedOutOfInstrumentation(
			BasicPod(podOwnersTestNamespace, "pod"),
			nil,
			dash0common.DefaultAutoInstrumentationLabelSelector,
		)).To(BeFalse())
	})
})

var _ = Describe("Modifying a pod with owners", func() {
	It("should derive service attributes from the top-level owner", func() {
		config := util.NewClusterInstrumentationConfig(
			TestImages,
			PossibleCollectorUrlsTest,
			OTelCollectorNodeLocalBaseUrlTest,
			util.ExtraConfigDefaults,
			dash0v1alpha1.InstrumentationDeliveryInitContainer,
			nil,
			false,
			false,
			false,
		)
		namespaceConfig := DefaultNamespaceInstrumentationConfig
		namespaceConfig.InstrumentPods = true
		modifier := NewResourceModifier(config, namespaceConfig, testActor, logd.Discard())
		pod := PodControlledBy(podOwnersTestNamespace, "pod", util.K8sTypeMetaReplicaSet)
		replicaSet := NewPartialObjectMetadata(util.K8sTypeMetaReplicaSet)
		replicaSet.Labels = map[string]string{util.AppKubernetesIoNameLabel: "replica-set-name"}
		deployment := NewPartialObjectMetadata(util.K8sTypeMetaDeployment)
		deployment.Labels = map[string]string{
			util.AppKubernetesIoNameLabel:   "deployment-name",
			util.AppKubernetesIoPartOfLabel: "deployment-part-of",
		}
		deployment.Annotations = map[string]string{"resource.opentelemetry.io/deployment.attribute": "value"}

		modificationResult := modifier.ModifyPod(pod, PodOwners{replicaSet, deployment})

		Expect(modificationResult.HasBeenModified).To(BeTrue())
		envVars := pod.Spec.Containers[0].Env
		VerifyEnvVarsFromMap(map[string]*EnvVarExpectation{
			envVarOTelInjectorServiceName:            {Value: "deployment-name"},
			envVarOTelInjectorServiceNamespace:       {Value: "deployment-part-of"},
			envVarOTelInjectorResourceAttributesName: {Value: "deployment.attribute=value"},
		}, envVars)
	})
})

func newFakeReader(objects ...client.Object) client.Reader {
	return fake.NewClientBuilder().WithScheme(clientgoscheme.Scheme).WithObjects(objects...).Build()
}

func objectMetaForOwnerTest(name string, uid types.UID) metav1.ObjectMeta {
	return metav1.ObjectMeta{Namespace: podOwnersTestNamespace, Name: name, UID: uid}
}

func deploymentForOwnerTest(name string, uid types.UID) *appsv1.Deployment {
	deployment := &appsv1.Deployment{ObjectMeta: objectMetaForOwnerTest(name, uid)}
	deployment.Labels = map[string]string{"deployment-label": "value"}
	return deployment
}

func replicaSetForOwnerTest(name string, uid types.UID) *appsv1.ReplicaSet {
	return &appsv1.ReplicaSet{ObjectMeta: objectMetaForOwnerTest(name, uid)}
}

func controlledBy[T client.Object](owned T, owner client.Object) T {
	gvk, _, err := clientgoscheme.Scheme.ObjectKinds(owner)
	Expect(err).ToNot(HaveOccurred())
	owned.SetOwnerReferences([]metav1.OwnerReference{
		*metav1.NewControllerRef(owner, gvk[0]),
	})
	return owned
}

func ownerNames(owners PodOwners) []string {
	names := make([]string, 0, len(owners))
	for _, owner := range owners {
		names = append(names, owner.Kind+"/"+owner.Name)
	}
	return names
}
