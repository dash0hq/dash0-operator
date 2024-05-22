// SPDX-FileCopyrightText: Copyright 2024 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package util

import (
	"fmt"

	. "github.com/onsi/gomega"
	appsv1 "k8s.io/api/apps/v1"
)

type ContainerExpectations struct {
	VolumeMounts                             int
	Dash0VolumeMountIdx                      int
	EnvVars                                  int
	NodeOptionsEnvVarIdx                     int
	NodeOptionsValue                         string
	NodeOptionsUsesValueFrom                 bool
	Dash0CollectorBaseUrlEnvVarIdx           int
	Dash0CollectorBaseUrlEnvVarExpectedValue string
}

type DeploymentExpectations struct {
	Volumes               int
	Dash0VolumeIdx        int
	InitContainers        int
	Dash0InitContainerIdx int
	Containers            []ContainerExpectations
}

func VerifyModifiedDeployment(deployment *appsv1.Deployment, expectations DeploymentExpectations) {
	podSpec := deployment.Spec.Template.Spec

	Expect(podSpec.Volumes).To(HaveLen(expectations.Volumes))
	for i, volume := range podSpec.Volumes {
		if i == expectations.Dash0VolumeIdx {
			Expect(volume.Name).To(Equal("dash0-instrumentation"))
			Expect(volume.EmptyDir).NotTo(BeNil())
		} else {
			Expect(volume.Name).To(Equal(fmt.Sprintf("test-volume-%d", i)))
		}
	}

	Expect(podSpec.InitContainers).To(HaveLen(expectations.InitContainers))
	for i, initContainer := range podSpec.InitContainers {
		if i == expectations.Dash0InitContainerIdx {
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

	Expect(podSpec.Containers).To(HaveLen(len(expectations.Containers)))
	for i, container := range podSpec.Containers {
		Expect(container.Name).To(Equal(fmt.Sprintf("test-container-%d", i)))
		containerExpectations := expectations.Containers[i]
		Expect(container.VolumeMounts).To(HaveLen(containerExpectations.VolumeMounts))
		for i, volumeMount := range container.VolumeMounts {
			if i == containerExpectations.Dash0VolumeMountIdx {
				Expect(volumeMount.Name).To(Equal("dash0-instrumentation"))
				Expect(volumeMount.MountPath).To(Equal("/opt/dash0"))
			} else {
				Expect(volumeMount.Name).To(Equal(fmt.Sprintf("test-volume-%d", i)))
			}
		}
		Expect(container.Env).To(HaveLen(containerExpectations.EnvVars))
		for i, envVar := range container.Env {
			if i == containerExpectations.NodeOptionsEnvVarIdx {
				Expect(envVar.Name).To(Equal("NODE_OPTIONS"))
				if containerExpectations.NodeOptionsUsesValueFrom {
					Expect(envVar.Value).To(BeEmpty())
					Expect(envVar.ValueFrom).To(Not(BeNil()))
				} else if containerExpectations.NodeOptionsValue != "" {
					Expect(envVar.Value).To(Equal(containerExpectations.NodeOptionsValue))
				} else {
					Expect(envVar.Value).To(Equal(
						"--require /opt/dash0/instrumentation/node.js/node_modules/@dash0/opentelemetry/src/index.js",
					))
				}
			} else if i == containerExpectations.Dash0CollectorBaseUrlEnvVarIdx {
				Expect(envVar.Name).To(Equal("DASH0_OTEL_COLLECTOR_BASE_URL"))
				Expect(envVar.Value).To(Equal(containerExpectations.Dash0CollectorBaseUrlEnvVarExpectedValue))
				Expect(envVar.ValueFrom).To(BeNil())
			} else {
				Expect(envVar.Name).To(Equal(fmt.Sprintf("TEST%d", i)))
			}
		}
	}
}
