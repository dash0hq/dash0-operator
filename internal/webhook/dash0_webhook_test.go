// SPDX-FileCopyrightText: Copyright 2024 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package webhook

import (
	"fmt"

	. "github.com/dash0hq/dash0-operator/test/utils"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	appsv1 "k8s.io/api/apps/v1"
)

type containerExpectations struct {
	volumeMounts             int
	dash0VolumeMountIdx      int
	envVars                  int
	nodeOptionsEnvVarIdx     int
	nodeOptionsValue         string
	nodeOptionsUsesValueFrom bool
}
type deploymentExpectations struct {
	volumes               int
	dash0VolumeIdx        int
	initContainers        int
	dash0InitContainerIdx int
	containers            []containerExpectations
}

var _ = Describe("Dash0 Webhook", func() {
	AfterEach(func() {
		_ = k8sClient.Delete(ctx, CreateBasicDeployment(DefaultNamespace, DeploymentName))
	})

	Context("when mutating new deployments", func() {
		It("should inject Dash into a new basic deployment", func() {
			deployment := CreateBasicDeployment(DefaultNamespace, DeploymentName)
			Expect(k8sClient.Create(ctx, deployment)).Should(Succeed())

			deployment = GetDeployment(ctx, k8sClient, DefaultNamespace, DeploymentName)
			verifyDeployment(deployment, deploymentExpectations{
				volumes:               1,
				dash0VolumeIdx:        0,
				initContainers:        1,
				dash0InitContainerIdx: 0,
				containers: []containerExpectations{{
					volumeMounts:         1,
					dash0VolumeMountIdx:  0,
					envVars:              1,
					nodeOptionsEnvVarIdx: 0,
				}},
			})
		})

		It("should inject Dash into a new deployment that has multiple containers, and already has volumes and init containers", func() {
			deployment := CreateDeploymentWithMoreBellsAndWhistles(DefaultNamespace, DeploymentName)
			Expect(k8sClient.Create(ctx, deployment)).Should(Succeed())

			deployment = GetDeployment(ctx, k8sClient, DefaultNamespace, DeploymentName)
			verifyDeployment(deployment, deploymentExpectations{
				volumes:               3,
				dash0VolumeIdx:        2,
				initContainers:        3,
				dash0InitContainerIdx: 2,
				containers: []containerExpectations{
					{
						volumeMounts:         2,
						dash0VolumeMountIdx:  1,
						envVars:              2,
						nodeOptionsEnvVarIdx: 1,
					},
					{
						volumeMounts:         3,
						dash0VolumeMountIdx:  2,
						envVars:              3,
						nodeOptionsEnvVarIdx: 2,
					},
				},
			})
		})

		It("should update existing Dash artifacts in a new deployment", func() {
			deployment := CreateDeploymentWithExistingDash0Artifacts(DefaultNamespace, DeploymentName)
			Expect(k8sClient.Create(ctx, deployment)).Should(Succeed())

			deployment = GetDeployment(ctx, k8sClient, DefaultNamespace, DeploymentName)
			verifyDeployment(deployment, deploymentExpectations{
				volumes:               3,
				dash0VolumeIdx:        1,
				initContainers:        3,
				dash0InitContainerIdx: 1,
				containers: []containerExpectations{
					{
						volumeMounts:             2,
						dash0VolumeMountIdx:      1,
						envVars:                  2,
						nodeOptionsEnvVarIdx:     1,
						nodeOptionsUsesValueFrom: true,
					},
					{
						volumeMounts:         3,
						dash0VolumeMountIdx:  1,
						envVars:              3,
						nodeOptionsEnvVarIdx: 1,
						nodeOptionsValue:     "--require /opt/dash0/instrumentation/node.js/node_modules/@dash0/opentelemetry/src/index.js --require something-else --experimental-modules",
					},
				},
			})
		})
	})
})

func verifyDeployment(deployment *appsv1.Deployment, expectations deploymentExpectations) {
	podSpec := deployment.Spec.Template.Spec

	Expect(podSpec.Volumes).To(HaveLen(expectations.volumes))
	for i, volume := range podSpec.Volumes {
		if i == expectations.dash0VolumeIdx {
			Expect(volume.Name).To(Equal("dash0-instrumentation"))
			Expect(volume.EmptyDir).NotTo(BeNil())
		} else {
			Expect(volume.Name).To(Equal(fmt.Sprintf("test-volume-%d", i)))
		}
	}

	Expect(podSpec.InitContainers).To(HaveLen(expectations.initContainers))
	for i, initContainer := range podSpec.InitContainers {
		if i == expectations.dash0InitContainerIdx {
			Expect(initContainer.Name).To(Equal("dash0-instrumentation"))
			Expect(initContainer.Image).To(MatchRegexp("^dash0-instrumentation:\\d+\\.\\d+\\.\\d+"))
			Expect(initContainer.Env).To(HaveLen(1))
			Expect(initContainer.Env).To(ContainElement(MatchEnvVar("DASH0_INSTRUMENTATION_FOLDER_DESTINATION", "/opt/dash0")))
			Expect(initContainer.SecurityContext).NotTo(BeNil())
			Expect(initContainer.VolumeMounts).To(HaveLen(1))
			Expect(initContainer.VolumeMounts).To(ContainElement(MatchVolumeMount("dash0-instrumentation", "/opt/dash0")))
		} else {
			Expect(initContainer.Name).To(Equal(fmt.Sprintf("test-init-container-%d", i)))
			Expect(initContainer.Env).To(HaveLen(i + 1))
		}
	}

	Expect(podSpec.Containers).To(HaveLen(len(expectations.containers)))
	for i, container := range podSpec.Containers {
		Expect(container.Name).To(Equal(fmt.Sprintf("test-container-%d", i)))
		containerExpectations := expectations.containers[i]
		Expect(container.VolumeMounts).To(HaveLen(containerExpectations.volumeMounts))
		for i, volumeMount := range container.VolumeMounts {
			if i == containerExpectations.dash0VolumeMountIdx {
				Expect(volumeMount.Name).To(Equal("dash0-instrumentation"))
				Expect(volumeMount.MountPath).To(Equal("/opt/dash0"))
			} else {
				Expect(volumeMount.Name).To(Equal(fmt.Sprintf("test-volume-%d", i)))
			}
		}
		Expect(container.Env).To(HaveLen(containerExpectations.envVars))
		for i, envVar := range container.Env {
			if i == containerExpectations.nodeOptionsEnvVarIdx {
				Expect(envVar.Name).To(Equal("NODE_OPTIONS"))
				if containerExpectations.nodeOptionsUsesValueFrom {
					Expect(envVar.Value).To(BeEmpty())
					Expect(envVar.ValueFrom).To(Not(BeNil()))
				} else if containerExpectations.nodeOptionsValue != "" {
					Expect(envVar.Value).To(Equal(containerExpectations.nodeOptionsValue))
				} else {
					Expect(envVar.Value).To(Equal("--require /opt/dash0/instrumentation/node.js/node_modules/@dash0/opentelemetry/src/index.js"))
				}
			} else {
				Expect(envVar.Name).To(Equal(fmt.Sprintf("TEST%d", i)))
			}
		}
	}
}
