// SPDX-FileCopyrightText: Copyright 2024 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package e2e

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

const (
	operatorNamespace = "dash0-operator-system"
	operatorImage     = "dash0-operator-controller:latest"

	kubeContextForTest            = "docker-desktop"
	applicationUnderTestNamespace = "e2e-application-under-test-namespace"

	managerYaml       = "config/manager/manager.yaml"
	managerYamlBackup = managerYaml + ".backup"
)

var (
	applicationNamespaceHasBeenCreated = false
	certManagerHasBeenInstalled        = false

	originalKubeContext            string
	managerYamlNeedsRevert         bool
	collectorHasBeenInstalled      bool
	managerNamespaceHasBeenCreated bool
	setupFinishedSuccessfully      bool
)

var _ = Describe("Dash0 Kubernetes Operator", Ordered, func() {

	BeforeAll(func() {
		pwdOutput, err := Run(exec.Command("pwd"), false)
		ExpectWithOffset(1, err).NotTo(HaveOccurred())
		workingDir := strings.TrimSpace(string(pwdOutput))
		fmt.Fprintf(GinkgoWriter, "workingDir: %s\n", workingDir)

		CheckIfRequiredPortsAreBlocked()

		RenderTemplates()

		By("reading current imagePullPolicy")
		yqOutput, err := Run(exec.Command(
			"yq",
			"e",
			"select(documentIndex == 1) | .spec.template.spec.containers[] |  select(.name == \"manager\") | .imagePullPolicy",
			managerYaml))
		ExpectWithOffset(1, err).NotTo(HaveOccurred())
		originalImagePullPolicy := strings.TrimSpace(string(yqOutput))
		fmt.Fprintf(GinkgoWriter, "original imagePullPolicy: %s\n", originalImagePullPolicy)

		if originalImagePullPolicy != "Never" {
			err = CopyFile(managerYaml, managerYamlBackup)
			ExpectWithOffset(1, err).NotTo(HaveOccurred())
			managerYamlNeedsRevert = true
			By("temporarily changing imagePullPolicy to \"Never\"")
			ExpectWithOffset(1, RunAndIgnoreOutput(exec.Command(
				"yq",
				"-i",
				"with(select(documentIndex == 1) | "+
					".spec.template.spec.containers[] | "+
					"select(.name == \"manager\"); "+
					".imagePullPolicy |= \"Never\")",
				managerYaml))).To(Succeed())
		}

		By("reading current kubectx")
		kubectxOutput, err := Run(exec.Command("kubectx", "-c"))
		ExpectWithOffset(1, err).NotTo(HaveOccurred())
		originalKubeContext = strings.TrimSpace(string(kubectxOutput))

		if originalKubeContext != kubeContextForTest {
			By("switching to kubectx docker-desktop, previous context " + originalKubeContext + " will be restored later")
			ExpectWithOffset(1, RunAndIgnoreOutput(exec.Command("kubectx", "docker-desktop"))).To(Succeed())
		}

		certManagerHasBeenInstalled = EnsureCertManagerIsInstalled()

		applicationNamespaceHasBeenCreated = EnsureNamespaceExists(applicationUnderTestNamespace)

		By("(re)installing the collector")
		ExpectWithOffset(1, ReinstallCollectorAndClearExportedTelemetry(applicationUnderTestNamespace)).To(Succeed())
		collectorHasBeenInstalled = true

		By("creating manager namespace")
		ExpectWithOffset(1, RunAndIgnoreOutput(exec.Command("kubectl", "create", "ns", operatorNamespace))).To(Succeed())
		managerNamespaceHasBeenCreated = true

		By("building the manager(Operator) image")
		ExpectWithOffset(1,
			RunAndIgnoreOutput(exec.Command("make", "docker-build", fmt.Sprintf("IMG=%s", operatorImage)))).To(Succeed())

		By("installing CRDs")
		ExpectWithOffset(1, RunAndIgnoreOutput(exec.Command("make", "install"))).To(Succeed())

		setupFinishedSuccessfully = true
	})

	AfterAll(func() {
		if managerYamlNeedsRevert {
			By("reverting changes to " + managerYaml)
			err := CopyFile(managerYamlBackup, managerYaml)
			ExpectWithOffset(1, err).NotTo(HaveOccurred())
			err = os.Remove(managerYamlBackup)
			ExpectWithOffset(1, err).NotTo(HaveOccurred())
		}

		UninstallCertManagerIfApplicable(certManagerHasBeenInstalled)

		if collectorHasBeenInstalled {
			By("uninstalling the collector")
			Expect(UninstallCollector(applicationUnderTestNamespace)).To(Succeed())
		}

		if managerNamespaceHasBeenCreated {
			By("removing manager namespace")
			_ = RunAndIgnoreOutput(exec.Command("kubectl", "delete", "ns", operatorNamespace))
		}

		if applicationNamespaceHasBeenCreated && applicationUnderTestNamespace != "default" {
			By("removing namespace for application under test")
			_ = RunAndIgnoreOutput(exec.Command("kubectl", "delete", "ns", applicationUnderTestNamespace))
		}

		if originalKubeContext != "" && originalKubeContext != kubeContextForTest {
			By("switching back to original kubectx " + originalKubeContext)
			output, err := Run(exec.Command("kubectx", originalKubeContext))
			if err != nil {
				fmt.Fprint(GinkgoWriter, err.Error())
			}
			fmt.Fprint(GinkgoWriter, string(output))
		}
	})

	BeforeEach(func() {
		DeleteTestIdFiles()
	})

	AfterEach(func() {
		if setupFinishedSuccessfully {
			// As an alternative to undeploying all applications under test (deployment, daemonset, cronjob, ...) we
			// could also delete the whole namespace for the application under test to after each test case get rid of
			// all applications (and then recreate the namespace before each test). However, this would mean we also
			// need to deploy the OpenTelemetry collector to the target namespace again for each test case, which would
			// slow down tests a bit more.
			RemoveAllTestApplications(applicationUnderTestNamespace)
			DeleteTestIdFiles()
		}
	})

	Describe("controller", func() {
		AfterEach(func() {
			UndeployDash0Resource(applicationUnderTestNamespace)
			UndeployOperator(operatorNamespace)
		})

		DescribeTable(
			"when instrumenting existing resources",
			func(
				resourceType string,
				installResource func(string) error,
				sendRequests bool,
				restartPodsManually bool,
			) {
				By(fmt.Sprintf("installing the Node.js %s", resourceType))
				Expect(installResource(applicationUnderTestNamespace)).To(Succeed())
				By("deploy the operator and the Dash0 custom resource")
				DeployOperator(operatorNamespace, operatorImage)
				DeployDash0Resource(applicationUnderTestNamespace)
				By(fmt.Sprintf("verifying that the Node.js %s has been instrumented by the controller", resourceType))
				VerifyThatSpansAreCaptured(
					applicationUnderTestNamespace,
					resourceType,
					sendRequests,
					restartPodsManually,
					"controller",
				)
			},
			Entry("should modify existing cron jobs", "cronjob", InstallNodeJsCronJob, false, false),
			Entry("should modify existing daemon sets", "daemonset", InstallNodeJsDaemonSet, true, false),
			Entry("should modify existing deployments", "deployment", InstallNodeJsDeployment, true, false),
			Entry("should modify existing replica set", "replicaset", InstallNodeJsReplicaSet, true, true),
			Entry("should modify existing stateful set", "statefulset", InstallNodeJsStatefulSet, true, false),
		)

		Describe("when it detects existing immutable jobs", func() {
			It("should label them accordingly", func() {
				By("installing the Node.js job")
				Expect(InstallNodeJsJob(applicationUnderTestNamespace)).To(Succeed())
				By("deploy the operator and the Dash0 custom resource")
				DeployOperator(operatorNamespace, operatorImage)
				DeployDash0Resource(applicationUnderTestNamespace)
				By("verifying that the Node.js job has been labelled by the controller")
				Eventually(func(g Gomega) {
					verifyLabels(g, applicationUnderTestNamespace, "job", false, "controller")
				}, verifyTelemetryTimeout, verifyTelemetryPollingInterval).Should(Succeed())
			})
		})
	})

	Describe("webhook", func() {

		BeforeAll(func() {
			DeployOperator(operatorNamespace, operatorImage)

			// Deliberately not deploying the Dash0 custom resource here: as of now, the webhook does not rely on the
			// presence of the Dash0 resource. This is subject to change though. (If changed, it also needs to be
			// undeployed in the AfterAll hook.)
			//
			// DeployDash0Resource(applicationUnderTestNamespace)

			fmt.Fprint(GinkgoWriter, "waiting 10 seconds to give the webhook some time to get ready\n")
			time.Sleep(10 * time.Second)
		})

		AfterAll(func() {
			// See comment in BeforeAll hook.
			// UndeployDash0Resource(applicationUnderTestNamespace)
			UndeployOperator(operatorNamespace)
		})

		DescribeTable(
			"when instrumenting new resources",
			func(
				resourceType string,
				installResource func(string) error,
				sendRequests bool,
			) {
				By(fmt.Sprintf("installing the Node.js %s", resourceType))
				Expect(installResource(applicationUnderTestNamespace)).To(Succeed())
				By(fmt.Sprintf("verifying that the Node.js %s has been instrumented by the webhook", resourceType))
				VerifyThatSpansAreCaptured(applicationUnderTestNamespace, resourceType, sendRequests, false, "webhook")
			},
			Entry("should modify new cron jobs", "cronjob", InstallNodeJsCronJob, false),
			Entry("should modify new daemon sets", "daemonset", InstallNodeJsDaemonSet, true),
			Entry("should modify new deployments", "deployment", InstallNodeJsDeployment, true),
			Entry("should modify new jobs", "job", InstallNodeJsJob, false),
			Entry("should modify new replica sets", "replicaset", InstallNodeJsReplicaSet, true),
			Entry("should modify new stateful sets", "statefulset", InstallNodeJsStatefulSet, true),
		)
	})
})
