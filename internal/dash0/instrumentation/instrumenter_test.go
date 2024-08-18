// SPDX-FileCopyrightText: Copyright 2024 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package instrumentation

import (
	"context"
	"fmt"

	appsv1 "k8s.io/api/apps/v1"
	batchv1 "k8s.io/api/batch/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	. "github.com/dash0hq/dash0-operator/test/util"
)

const (
	olderOperatorControllerImageLabel = "some-registry_com_1234_dash0hq_operator-controller_0.9.8"
	olderInitContainerImageLabel      = "some-registry_com_1234_dash0hq_instrumentation_2.3.4"
)

var (
	namespace = TestNamespaceName
)

var _ = Describe("The instrumenter", Ordered, func() {
	ctx := context.Background()
	logger := log.FromContext(ctx)
	var createdObjects []client.Object

	var instrumenter *Instrumenter

	BeforeAll(func() {
		EnsureTestNamespaceExists(ctx, k8sClient)
		EnsureDash0OperatorNamespaceExists(ctx, k8sClient)
	})

	BeforeEach(func() {
		EnsureDash0MonitoringResourceExists(ctx, k8sClient)

		createdObjects = make([]client.Object, 0)

		instrumenter = &Instrumenter{
			Client:               k8sClient,
			Clientset:            clientset,
			Recorder:             recorder,
			Images:               TestImages,
			OTelCollectorBaseUrl: OTelCollectorBaseUrlTest,
		}
	})

	AfterEach(func() {
		createdObjects = DeleteAllCreatedObjects(ctx, k8sClient, createdObjects)
		DeleteAllEvents(ctx, clientset, namespace)

		RemoveDash0MonitoringResource(ctx, k8sClient)
	})

	DescribeTable("should instrument existing workloads at startup", func(config WorkloadTestConfig) {
		name := UniqueName(config.WorkloadNamePrefix)
		workload := config.CreateFn(ctx, k8sClient, namespace, name)
		createdObjects = append(createdObjects, workload.Get())

		instrumenter.InstrumentAtStartup(ctx, k8sClient, &logger)

		VerifySuccessfulInstrumentationEvent(ctx, clientset, namespace, name, "controller")
		config.VerifyFn(config.GetFn(ctx, k8sClient, namespace, name))
	}, Entry("should instrument a cron job at startup", WorkloadTestConfig{
		WorkloadNamePrefix: CronJobNamePrefix,
		CreateFn:           WrapCronJobFnAsTestableWorkload(CreateBasicCronJob),
		GetFn:              WrapCronJobFnAsTestableWorkload(GetCronJob),
		VerifyFn: func(workload TestableWorkload) {
			VerifyModifiedCronJob(workload.Get().(*batchv1.CronJob), BasicInstrumentedPodSpecExpectations())
		},
	}), Entry("should instrument a daemon set at startup", WorkloadTestConfig{
		WorkloadNamePrefix: DaemonSetNamePrefix,
		CreateFn:           WrapDaemonSetFnAsTestableWorkload(CreateBasicDaemonSet),
		GetFn:              WrapDaemonSetFnAsTestableWorkload(GetDaemonSet),
		VerifyFn: func(workload TestableWorkload) {
			VerifyModifiedDaemonSet(workload.Get().(*appsv1.DaemonSet), BasicInstrumentedPodSpecExpectations())
		},
	}), Entry("should instrument a deployment at startup", WorkloadTestConfig{
		WorkloadNamePrefix: DeploymentNamePrefix,
		CreateFn:           WrapDeploymentFnAsTestableWorkload(CreateBasicDeployment),
		GetFn:              WrapDeploymentFnAsTestableWorkload(GetDeployment),
		VerifyFn: func(workload TestableWorkload) {
			VerifyModifiedDeployment(workload.Get().(*appsv1.Deployment), BasicInstrumentedPodSpecExpectations())
		},
	}), Entry("should instrument a replica set at startup", WorkloadTestConfig{
		WorkloadNamePrefix: ReplicaSetNamePrefix,
		CreateFn:           WrapReplicaSetFnAsTestableWorkload(CreateBasicReplicaSet),
		GetFn:              WrapReplicaSetFnAsTestableWorkload(GetReplicaSet),
		VerifyFn: func(workload TestableWorkload) {
			VerifyModifiedReplicaSet(workload.Get().(*appsv1.ReplicaSet), BasicInstrumentedPodSpecExpectations())
		},
	}), Entry("should instrument a stateful set at startup", WorkloadTestConfig{
		WorkloadNamePrefix: StatefulSetNamePrefix,
		CreateFn:           WrapStatefulSetFnAsTestableWorkload(CreateBasicStatefulSet),
		GetFn:              WrapStatefulSetFnAsTestableWorkload(GetStatefulSet),
		VerifyFn: func(workload TestableWorkload) {
			VerifyModifiedStatefulSet(workload.Get().(*appsv1.StatefulSet), BasicInstrumentedPodSpecExpectations())
		},
	}),
	)

	Describe("should not instrument existing jobs at startup", func() {
		It("should record a failure event when attempting to instrument an existing job at startup and add labels", func() {
			name := UniqueName(JobNamePrefix)
			job := CreateBasicJob(ctx, k8sClient, namespace, name)
			createdObjects = append(createdObjects, job)

			instrumenter.InstrumentAtStartup(ctx, k8sClient, &logger)

			VerifyFailedInstrumentationEvent(
				ctx,
				clientset,
				namespace,
				name,
				fmt.Sprintf("Dash0 instrumentation of this workload by the controller has not been successful. "+
					"Error message: Dash0 cannot instrument the existing job test-namespace/%s, since this type "+
					"of workload is immutable.", name),
			)
			VerifyImmutableJobCouldNotBeModified(GetJob(ctx, k8sClient, namespace, name))
		})
	})

	DescribeTable("when updating instrumented workloads at startup", func(config WorkloadTestConfig) {
		name := UniqueName(config.WorkloadNamePrefix)
		workload := config.CreateFn(ctx, k8sClient, TestNamespaceName, name)
		createdObjects = append(createdObjects, workload.Get())
		workload.GetObjectMeta().Labels["dash0.com/operator-image"] = olderOperatorControllerImageLabel
		workload.GetObjectMeta().Labels["dash0.com/init-container-image"] = olderInitContainerImageLabel
		UpdateWorkload(ctx, k8sClient, workload.Get())
		instrumenter.InstrumentAtStartup(ctx, k8sClient, &logger)
		config.VerifyFn(config.GetFn(ctx, k8sClient, TestNamespaceName, name))
		VerifySuccessfulInstrumentationEvent(ctx, clientset, namespace, name, "controller")
	}, Entry("should override outdated instrumentation settings for a cron job at startup", WorkloadTestConfig{
		WorkloadNamePrefix: CronJobNamePrefix,
		CreateFn:           WrapCronJobFnAsTestableWorkload(CreateInstrumentedCronJob),
		GetFn:              WrapCronJobFnAsTestableWorkload(GetCronJob),
		VerifyFn: func(workload TestableWorkload) {
			VerifyModifiedCronJob(workload.Get().(*batchv1.CronJob), BasicInstrumentedPodSpecExpectations())
		},
	}), Entry("should override outdated instrumentation settings for a daemon set at startup", WorkloadTestConfig{
		WorkloadNamePrefix: DaemonSetNamePrefix,
		CreateFn:           WrapDaemonSetFnAsTestableWorkload(CreateInstrumentedDaemonSet),
		GetFn:              WrapDaemonSetFnAsTestableWorkload(GetDaemonSet),
		VerifyFn: func(workload TestableWorkload) {
			VerifyModifiedDaemonSet(workload.Get().(*appsv1.DaemonSet), BasicInstrumentedPodSpecExpectations())
		},
	}), Entry("should override outdated instrumentation settings for a deployment at startup", WorkloadTestConfig{
		WorkloadNamePrefix: DeploymentNamePrefix,
		CreateFn:           WrapDeploymentFnAsTestableWorkload(CreateInstrumentedDeployment),
		GetFn:              WrapDeploymentFnAsTestableWorkload(GetDeployment),
		VerifyFn: func(workload TestableWorkload) {
			VerifyModifiedDeployment(workload.Get().(*appsv1.Deployment), BasicInstrumentedPodSpecExpectations())
		},
	}), Entry("should override outdated instrumentation settings for a replica set at startup", WorkloadTestConfig{
		WorkloadNamePrefix: ReplicaSetNamePrefix,
		CreateFn:           WrapReplicaSetFnAsTestableWorkload(CreateInstrumentedReplicaSet),
		GetFn:              WrapReplicaSetFnAsTestableWorkload(GetReplicaSet),
		VerifyFn: func(workload TestableWorkload) {
			VerifyModifiedReplicaSet(workload.Get().(*appsv1.ReplicaSet), BasicInstrumentedPodSpecExpectations())
		},
	}), Entry("should override outdated instrumentation settings for a stateful set at startup", WorkloadTestConfig{
		WorkloadNamePrefix: StatefulSetNamePrefix,
		CreateFn:           WrapStatefulSetFnAsTestableWorkload(CreateInstrumentedStatefulSet),
		GetFn:              WrapStatefulSetFnAsTestableWorkload(GetStatefulSet),
		VerifyFn: func(workload TestableWorkload) {
			VerifyModifiedStatefulSet(workload.Get().(*appsv1.StatefulSet), BasicInstrumentedPodSpecExpectations())
		},
	}),
	)

	Describe("when attempting to update instrumented jobs at startup", func() {
		It("should not override outdated instrumentation settings for a job at startup", func() {
			name := UniqueName(JobNamePrefix)
			workload := CreateInstrumentedJob(ctx, k8sClient, TestNamespaceName, name)
			createdObjects = append(createdObjects, workload)
			workload.ObjectMeta.Labels["dash0.com/operator-image"] = "some-registry.com_1234_dash0hq_operator-controller_0.9.8"
			workload.ObjectMeta.Labels["dash0.com/init-container-image"] = "some-registry.com_1234_dash0hq_instrumentation_2.3.4"
			UpdateWorkload(ctx, k8sClient, workload)
			instrumenter.InstrumentAtStartup(ctx, k8sClient, &logger)

			// we do not attempt to update the instrumentation for jobs, since they are immutable
			workload = GetJob(ctx, k8sClient, TestNamespaceName, name)
			jobLabels := workload.ObjectMeta.Labels
			Expect(jobLabels["dash0.com/instrumented"]).To(Equal("true"))
			Expect(jobLabels["dash0.com/operator-image"]).To(Equal("some-registry.com_1234_dash0hq_operator-controller_0.9.8"))
			Expect(jobLabels["dash0.com/init-container-image"]).To(Equal("some-registry.com_1234_dash0hq_instrumentation_2.3.4"))
			VerifyNoEvents(ctx, clientset, namespace)
		})
	})
})
