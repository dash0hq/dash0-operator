// SPDX-FileCopyrightText: Copyright 2024 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package k8sresources

import (
	"context"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"sigs.k8s.io/controller-runtime/pkg/log"

	"github.com/dash0hq/dash0-operator/internal/util"

	. "github.com/dash0hq/dash0-operator/test/util"
)

// Maintenance note: There is some overlap of test cases between this file and dash0_webhook_test.go. This is
// intentional. However, this test should be used for more fine-grained test cases, while dash0_webhook_test.go should
// be used to verify external effects (recording events etc.) that cannot be covered in this test.

var (
	instrumentationMetadata = util.InstrumentationMetadata{
		Versions: util.Versions{OperatorVersion: "1.2.3",
			InitContainerImageVersion: "4.5.6",
		},
		InstrumentedBy: "modify_test",
	}
)

var _ = Describe("Dash0 Resource Modification", func() {

	ctx := context.Background()
	logger := log.FromContext(ctx)
	resourceModifier := NewResourceModifier(instrumentationMetadata, &logger)

	Context("when mutating new resources", func() {
		It("should inject Dash0 into a new basic deployment", func() {
			deployment := BasicDeployment(TestNamespaceName, DeploymentName)
			result := resourceModifier.ModifyDeployment(deployment, TestNamespaceName)

			Expect(result).To(BeTrue())
			VerifyModifiedDeployment(deployment, BasicPodSpecExpectations)
		})

		It("should inject Dash0 into a new deployment that has multiple Containers, and already has Volumes and init Containers", func() {
			deployment := DeploymentWithMoreBellsAndWhistles(TestNamespaceName, DeploymentName)
			result := resourceModifier.ModifyDeployment(deployment, TestNamespaceName)

			Expect(result).To(BeTrue())
			VerifyModifiedDeployment(deployment, PodSpecExpectations{
				Volumes:               3,
				Dash0VolumeIdx:        2,
				InitContainers:        3,
				Dash0InitContainerIdx: 2,
				Containers: []ContainerExpectations{
					{
						VolumeMounts:                             2,
						Dash0VolumeMountIdx:                      1,
						EnvVars:                                  3,
						NodeOptionsEnvVarIdx:                     1,
						Dash0CollectorBaseUrlEnvVarIdx:           2,
						Dash0CollectorBaseUrlEnvVarExpectedValue: "http://dash0-opentelemetry-collector-daemonset.test-namespace.svc.cluster.local:4318",
					},
					{
						VolumeMounts:                             3,
						Dash0VolumeMountIdx:                      2,
						EnvVars:                                  4,
						NodeOptionsEnvVarIdx:                     2,
						Dash0CollectorBaseUrlEnvVarIdx:           3,
						Dash0CollectorBaseUrlEnvVarExpectedValue: "http://dash0-opentelemetry-collector-daemonset.test-namespace.svc.cluster.local:4318",
					},
				},
			})
		})

		It("should update existing Dash0 artifacts in a new deployment", func() {
			deployment := DeploymentWithExistingDash0Artifacts(TestNamespaceName, DeploymentName)
			result := resourceModifier.ModifyDeployment(deployment, TestNamespaceName)

			Expect(result).To(BeTrue())
			VerifyModifiedDeployment(deployment, PodSpecExpectations{
				Volumes:               3,
				Dash0VolumeIdx:        1,
				InitContainers:        3,
				Dash0InitContainerIdx: 1,
				Containers: []ContainerExpectations{
					{
						VolumeMounts:                             2,
						Dash0VolumeMountIdx:                      1,
						EnvVars:                                  3,
						NodeOptionsEnvVarIdx:                     1,
						NodeOptionsUsesValueFrom:                 true,
						Dash0CollectorBaseUrlEnvVarIdx:           2,
						Dash0CollectorBaseUrlEnvVarExpectedValue: "http://dash0-opentelemetry-collector-daemonset.test-namespace.svc.cluster.local:4318",
					},
					{
						VolumeMounts:                             3,
						Dash0VolumeMountIdx:                      1,
						EnvVars:                                  3,
						NodeOptionsEnvVarIdx:                     1,
						NodeOptionsValue:                         "--require /opt/dash0/instrumentation/node.js/node_modules/@dash0/opentelemetry/src/index.js --require something-else --experimental-modules",
						Dash0CollectorBaseUrlEnvVarIdx:           0,
						Dash0CollectorBaseUrlEnvVarExpectedValue: "http://dash0-opentelemetry-collector-daemonset.test-namespace.svc.cluster.local:4318",
					},
				},
			})
		})

		It("should inject Dash0 into a new basic cron job", func() {
			resource := BasicCronJob(TestNamespaceName, CronJobName)
			result := resourceModifier.ModifyCronJob(resource, TestNamespaceName)

			Expect(result).To(BeTrue())
			VerifyModifiedCronJob(resource, BasicPodSpecExpectations)
		})

		It("should inject Dash0 into a new basic daemon set", func() {
			resource := BasicDaemonSet(TestNamespaceName, DaemonSetName)
			result := resourceModifier.ModifyDaemonSet(resource, TestNamespaceName)

			Expect(result).To(BeTrue())
			VerifyModifiedDaemonSet(resource, BasicPodSpecExpectations)
		})

		It("should inject Dash0 into a new basic job", func() {
			resource := BasicJob(TestNamespaceName, JobName)
			result := resourceModifier.ModifyJob(resource, TestNamespaceName)

			Expect(result).To(BeTrue())
			VerifyModifiedJob(resource, BasicPodSpecExpectations)
		})

		It("should inject Dash0 into a new basic replica set", func() {
			resource := BasicReplicaSet(TestNamespaceName, ReplicaSetName)
			result := resourceModifier.ModifyReplicaSet(resource, TestNamespaceName)

			Expect(result).To(BeTrue())
			VerifyModifiedReplicaSet(resource, BasicPodSpecExpectations)
		})

		It("should not inject Dash0 into a new basic replica set that is owned by a deployment", func() {
			resource := ReplicaSetOwnedByDeployment(TestNamespaceName, ReplicaSetName)
			result := resourceModifier.ModifyReplicaSet(resource, TestNamespaceName)

			Expect(result).To(BeFalse())
			VerifyUnmodifiedReplicaSet(resource)
		})

		It("should inject Dash0 into a new basic stateful set", func() {
			resource := BasicStatefulSet(TestNamespaceName, StatefulSetName)
			result := resourceModifier.ModifyStatefulSet(resource, TestNamespaceName)

			Expect(result).To(BeTrue())
			VerifyModifiedStatefulSet(resource, BasicPodSpecExpectations)
		})
	})
})
