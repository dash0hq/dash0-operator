// SPDX-FileCopyrightText: Copyright 2024 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package k8sresources

import (
	"context"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"sigs.k8s.io/controller-runtime/pkg/log"

	. "github.com/dash0hq/dash0-operator/test/util"
)

// Maintenance note: There is some overlap of test cases between this file and dash0_webhook_test.go. This is
// intentional. However, this test should be used for more fine-grained test cases, while dash0_webhook_test.go should
// be used to verify external effects (recording events etc.) that cannot be covered in this test.

var _ = Describe("Dash0 Resource Modification", func() {

	ctx := context.Background()

	Context("when mutating new deployments", func() {
		It("should inject Dash into a new basic deployment", func() {
			deployment := BasicDeployment(TestNamespaceName, DeploymentName)
			result := ModifyPodSpec(&deployment.Spec.Template.Spec, TestNamespaceName, log.FromContext(ctx))

			Expect(result).To(BeTrue())
			VerifyModifiedDeployment(deployment, DeploymentExpectations{
				Volumes:               1,
				Dash0VolumeIdx:        0,
				InitContainers:        1,
				Dash0InitContainerIdx: 0,
				Containers: []ContainerExpectations{{
					VolumeMounts:                             1,
					Dash0VolumeMountIdx:                      0,
					EnvVars:                                  2,
					NodeOptionsEnvVarIdx:                     0,
					Dash0CollectorBaseUrlEnvVarIdx:           1,
					Dash0CollectorBaseUrlEnvVarExpectedValue: "http://dash0-opentelemetry-collector-daemonset.test-namespace.svc.cluster.local:4318",
				}},
			})
		})

		It("should inject Dash into a new deployment that has multiple Containers, and already has Volumes and init Containers", func() {
			deployment := DeploymentWithMoreBellsAndWhistles(TestNamespaceName, DeploymentName)
			result := ModifyPodSpec(&deployment.Spec.Template.Spec, TestNamespaceName, log.FromContext(ctx))

			Expect(result).To(BeTrue())
			VerifyModifiedDeployment(deployment, DeploymentExpectations{
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

		It("should update existing Dash artifacts in a new deployment", func() {
			deployment := DeploymentWithExistingDash0Artifacts(TestNamespaceName, DeploymentName)
			result := ModifyPodSpec(&deployment.Spec.Template.Spec, TestNamespaceName, log.FromContext(ctx))

			Expect(result).To(BeTrue())
			VerifyModifiedDeployment(deployment, DeploymentExpectations{
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
	})
})
