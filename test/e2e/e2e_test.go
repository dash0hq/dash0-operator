// SPDX-FileCopyrightText: Copyright 2024 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package e2e

import (
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	testUtil "github.com/dash0hq/dash0-operator/test/util"
)

const (
	namespace         = "dash0-operator-system"
	managerYaml       = "config/manager/manager.yaml"
	managerYamlBackup = managerYaml + ".backup"
)

var (
	originalKubeContext    string
	managerYamlNeedsRevert bool
)

var _ = Describe("controller", Ordered, func() {

	BeforeAll(func() {
		pwdOutput, err := testUtil.Run(exec.Command("pwd"))
		ExpectWithOffset(1, err).NotTo(HaveOccurred())
		workingDir := strings.TrimSpace(string(pwdOutput))
		fmt.Fprintf(GinkgoWriter, "workingDir: %s\n", workingDir)

		By("Reading current imagePullPolicy")
		yqOutput, err := testUtil.Run(exec.Command(
			"yq",
			"e",
			"select(documentIndex == 1) | .spec.template.spec.containers[] |  select(.name == \"manager\") | .imagePullPolicy",
			managerYaml))
		ExpectWithOffset(1, err).NotTo(HaveOccurred())
		originalImagePullPolicy := strings.TrimSpace(string(yqOutput))
		fmt.Fprintf(GinkgoWriter, "original imagePullPolicy: %s\n", originalImagePullPolicy)

		if originalImagePullPolicy != "Never" {
			err = copyFile(managerYaml, managerYamlBackup)
			ExpectWithOffset(1, err).NotTo(HaveOccurred())
			managerYamlNeedsRevert = true
			By("temporarily changing imagePullPolicy to \"Never\"")
			_, err = testUtil.Run(exec.Command(
				"yq",
				"-i",
				"with(select(documentIndex == 1) | "+
					".spec.template.spec.containers[] | "+
					"select(.name == \"manager\"); "+
					".imagePullPolicy |= \"Never\")",
				managerYaml))
			ExpectWithOffset(1, err).NotTo(HaveOccurred())
		}

		By("reading current kubectx")
		kubectxOutput, err := testUtil.Run(exec.Command("kubectx", "-c"))
		ExpectWithOffset(1, err).NotTo(HaveOccurred())
		originalKubeContext = strings.TrimSpace(string(kubectxOutput))

		By("switching to kubectx kind-kind, previous context " + originalKubeContext + " will be restored later")
		_, err = testUtil.Run(exec.Command("kubectx", "kind-kind"))
		ExpectWithOffset(1, err).NotTo(HaveOccurred())

		By("installing prometheus operator")
		Expect(testUtil.InstallPrometheusOperator()).To(Succeed())

		By("installing the cert-manager")
		Expect(testUtil.InstallCertManager()).To(Succeed())

		By("creating manager namespace")
		cmd := exec.Command("kubectl", "create", "ns", namespace)
		_, _ = testUtil.Run(cmd)
	})

	AfterAll(func() {
		if managerYamlNeedsRevert {
			By("reverting changes to " + managerYaml)
			err := copyFile(managerYamlBackup, managerYaml)
			ExpectWithOffset(1, err).NotTo(HaveOccurred())
			err = os.Remove(managerYamlBackup)
			ExpectWithOffset(1, err).NotTo(HaveOccurred())
		}

		By("uninstalling the Prometheus manager bundle")
		testUtil.UninstallPrometheusOperator()

		By("uninstalling the cert-manager bundle")
		testUtil.UninstallCertManager()

		By("removing manager namespace")
		cmd := exec.Command("kubectl", "delete", "ns", namespace)
		_, _ = testUtil.Run(cmd)

		By("switching back to original kubectx " + originalKubeContext)
		output, err := testUtil.Run(exec.Command("kubectx", originalKubeContext))
		if err != nil {
			fmt.Fprint(GinkgoWriter, err.Error())
		}
		fmt.Fprint(GinkgoWriter, string(output))
	})

	Context("Operator", func() {
		It("should start the controller successfully", func() {
			var controllerPodName string
			var err error

			var projectimage = "dash0-operator-controller:latest"

			By("building the manager(Operator) image")
			cmd := exec.Command("make", "docker-build", fmt.Sprintf("IMG=%s", projectimage))
			_, err = testUtil.Run(cmd)
			ExpectWithOffset(1, err).NotTo(HaveOccurred())

			By("loading the the manager(Operator) image on Kind")
			err = testUtil.LoadImageToKindClusterWithName(projectimage)
			ExpectWithOffset(1, err).NotTo(HaveOccurred())

			By("installing CRDs")
			cmd = exec.Command("make", "install")
			_, err = testUtil.Run(cmd)
			ExpectWithOffset(1, err).NotTo(HaveOccurred())

			fmt.Fprintf(GinkgoWriter, "time.Sleep(30 * time.Second)\n")
			time.Sleep(30 * time.Second)

			By("deploying the controller-manager")
			cmd = exec.Command("make", "deploy", fmt.Sprintf("IMG=%s", projectimage))
			_, err = testUtil.Run(cmd)
			ExpectWithOffset(1, err).NotTo(HaveOccurred())

			By("validating that the controller-manager pod is running as expected")
			verifyControllerUp := func() error {
				// Get pod name

				cmd = exec.Command("kubectl", "get",
					"pods", "-l", "control-plane=controller-manager",
					"-o", "go-template={{ range .items }}"+
						"{{ if not .metadata.deletionTimestamp }}"+
						"{{ .metadata.name }}"+
						"{{ \"\\n\" }}{{ end }}{{ end }}",
					"-n", namespace,
				)

				podOutput, err := testUtil.Run(cmd)
				ExpectWithOffset(2, err).NotTo(HaveOccurred())
				podNames := testUtil.GetNonEmptyLines(string(podOutput))
				if len(podNames) != 1 {
					return fmt.Errorf("expect 1 controller pods running, but got %d", len(podNames))
				}
				controllerPodName = podNames[0]
				ExpectWithOffset(2, controllerPodName).Should(ContainSubstring("controller-manager"))

				// Validate pod status
				cmd = exec.Command("kubectl", "get",
					"pods", controllerPodName, "-o", "jsonpath={.status.phase}",
					"-n", namespace,
				)
				status, err := testUtil.Run(cmd)
				ExpectWithOffset(2, err).NotTo(HaveOccurred())
				if string(status) != "Running" {
					return fmt.Errorf("controller pod in %s status", status)
				}
				return nil
			}
			EventuallyWithOffset(1, verifyControllerUp, 120*time.Second, time.Second).Should(Succeed())
		})
	})

})

func copyFile(source string, destination string) error {
	src, err := os.Open(source)
	if err != nil {
		return err
	}
	defer func() {
		err = errors.Join(err, src.Close())
	}()

	dst, err := os.Create(destination)
	if err != nil {
		return err
	}
	defer func() {
		err = errors.Join(err, dst.Close())
	}()
	_, err = io.Copy(dst, src)
	return err
}
