// SPDX-FileCopyrightText: Copyright 2024 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package webhook

import (
	. "github.com/dash0hq/dash0-operator/test/utils"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Dash0", func() {
	Context("with the Dash0 injection webhook", func() {
		It("should inject Dash into a deployment", func() {
			deployment := CreateDeployment(DefaultNamespace, DeploymentName)
			Expect(k8sClient.Create(ctx, deployment)).Should(Succeed())

			deployment = GetDeployment(ctx, k8sClient, DefaultNamespace, DeploymentName)

			Expect(deployment.Spec.Template.Spec.Volumes).To(HaveLen(1))
			volume := deployment.Spec.Template.Spec.Volumes[0]
			Expect(volume.Name).To(Equal("dash0-instrumentation"))
			Expect(volume.EmptyDir).NotTo(BeNil())

			Expect(deployment.Spec.Template.Spec.InitContainers).To(HaveLen(1))
			initContainer := deployment.Spec.Template.Spec.InitContainers[0]
			Expect(initContainer.Name).To(Equal("dash0-instrumentation"))
			Expect(initContainer.Image).To(MatchRegexp("^dash0-instrumentation:\\d+\\.\\d+\\.\\d+"))
			Expect(initContainer.Env).To(HaveLen(1))
			Expect(initContainer.Env).To(ContainElement(MatchEnvVar("DASH0_INSTRUMENTATION_FOLDER_DESTINATION", "/opt/dash0")))
			Expect(initContainer.SecurityContext).NotTo(BeNil())
			Expect(initContainer.VolumeMounts).To(HaveLen(1))
			Expect(initContainer.VolumeMounts).To(ContainElement(MatchVolumeMount("dash0-instrumentation", "/opt/dash0")))

			Expect(deployment.Spec.Template.Spec.Containers).To(HaveLen(1))
			container := deployment.Spec.Template.Spec.Containers[0]
			Expect(container.VolumeMounts).To(HaveLen(1))
			Expect(container.VolumeMounts).To(ContainElement(MatchVolumeMount("dash0-instrumentation", "/opt/dash0")))
			Expect(container.Env).To(HaveLen(1))
			Expect(container.Env).To(ContainElement(MatchEnvVar("NODE_OPTIONS", "--require /opt/dash0/instrumentation/node.js/node_modules/@dash0/opentelemetry/src/index.js")))
		})
	})
})
