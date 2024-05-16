// SPDX-FileCopyrightText: Copyright 2024 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package webhook

import (
	. "github.com/dash0hq/dash0-operator/test/util"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Dash0 Webhook", func() {
	AfterEach(func() {
		_ = k8sClient.Delete(ctx, BasicDeployment(TestNamespaceName, DeploymentName))
	})

	Context("when mutating new deployments", func() {
		It("should inject Dash into a new basic deployment", func() {
			deployment := BasicDeployment(TestNamespaceName, DeploymentName)
			Expect(k8sClient.Create(ctx, deployment)).Should(Succeed())

			deployment = GetDeployment(ctx, k8sClient, TestNamespaceName, DeploymentName)
			VerifyModifiedDeployment(deployment, DeploymentExpectations{
				Volumes:               1,
				Dash0VolumeIdx:        0,
				InitContainers:        1,
				Dash0InitContainerIdx: 0,
				Containers: []ContainerExpectations{{
					VolumeMounts:         1,
					Dash0VolumeMountIdx:  0,
					EnvVars:              1,
					NodeOptionsEnvVarIdx: 0,
				}},
			})
		})

		It("should inject Dash into a new deployment that has multiple Containers, and already has Volumes and init Containers", func() {
			deployment := DeploymentWithMoreBellsAndWhistles(TestNamespaceName, DeploymentName)
			Expect(k8sClient.Create(ctx, deployment)).Should(Succeed())

			deployment = GetDeployment(ctx, k8sClient, TestNamespaceName, DeploymentName)
			VerifyModifiedDeployment(deployment, DeploymentExpectations{
				Volumes:               3,
				Dash0VolumeIdx:        2,
				InitContainers:        3,
				Dash0InitContainerIdx: 2,
				Containers: []ContainerExpectations{
					{
						VolumeMounts:         2,
						Dash0VolumeMountIdx:  1,
						EnvVars:              2,
						NodeOptionsEnvVarIdx: 1,
					},
					{
						VolumeMounts:         3,
						Dash0VolumeMountIdx:  2,
						EnvVars:              3,
						NodeOptionsEnvVarIdx: 2,
					},
				},
			})
		})

		It("should update existing Dash artifacts in a new deployment", func() {
			deployment := DeploymentWithExistingDash0Artifacts(TestNamespaceName, DeploymentName)
			Expect(k8sClient.Create(ctx, deployment)).Should(Succeed())

			deployment = GetDeployment(ctx, k8sClient, TestNamespaceName, DeploymentName)
			VerifyModifiedDeployment(deployment, DeploymentExpectations{
				Volumes:               3,
				Dash0VolumeIdx:        1,
				InitContainers:        3,
				Dash0InitContainerIdx: 1,
				Containers: []ContainerExpectations{
					{
						VolumeMounts:             2,
						Dash0VolumeMountIdx:      1,
						EnvVars:                  2,
						NodeOptionsEnvVarIdx:     1,
						NodeOptionsUsesValueFrom: true,
					},
					{
						VolumeMounts:         3,
						Dash0VolumeMountIdx:  1,
						EnvVars:              3,
						NodeOptionsEnvVarIdx: 1,
						NodeOptionsValue:     "--require /opt/dash0/instrumentation/node.js/node_modules/@dash0/opentelemetry/src/index.js --require something-else --experimental-modules",
					},
				},
			})
		})
	})
})
