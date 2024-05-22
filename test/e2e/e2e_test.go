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
)

const (
	operatorNamespace = "dash0-operator-system"
	operatorImage     = "dash0-operator-controller:latest"

	applicationUnderTestNamespace = "application-under-test-namespace"

	managerYaml       = "config/manager/manager.yaml"
	managerYamlBackup = managerYaml + ".backup"
)

var (
	applicationNamespaceHasBeenCreated = false

	originalKubeContext    string
	managerYamlNeedsRevert bool

	skipCertManagerInstall   = false
	skipCertManagerUninstall = false
)

var _ = Describe("Dash0 Kubernetes Operator", Ordered, func() {

	BeforeAll(func() {
		pwdOutput, err := Run(exec.Command("pwd"), false)
		ExpectWithOffset(1, err).NotTo(HaveOccurred())
		workingDir := strings.TrimSpace(string(pwdOutput))
		fmt.Fprintf(GinkgoWriter, "workingDir: %s\n", workingDir)

		if v, ok := os.LookupEnv("SKIP_CERT_MANAGER_UNINSTALL"); ok {
			skipCertManagerUninstall = strings.ToLower(v) == "true"
		}
		if v, ok := os.LookupEnv("SKIP_CERT_MANAGER"); ok {
			skipCertManagerInstall = strings.ToLower(v) == "true"
			skipCertManagerUninstall = skipCertManagerInstall
		}

		By("Reading current imagePullPolicy")
		yqOutput, err := Run(exec.Command(
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

		By("switching to kubectx docker-desktop, previous context " + originalKubeContext + " will be restored later")
		ExpectWithOffset(1, RunAndIgnoreOutput(exec.Command("kubectx", "docker-desktop"))).To(Succeed())

		if !skipCertManagerInstall {
			By("installing the cert-manager")
			ExpectWithOffset(1, InstallCertManager()).To(Succeed())
		}

		applicationNamespaceHasBeenCreated = EnsureNamespaceExists(applicationUnderTestNamespace)

		By("installing the collector")
		ExpectWithOffset(1, ReinstallCollectorAndClearExportedTelemetry(applicationUnderTestNamespace)).To(Succeed())

		By("creating manager namespace")
		ExpectWithOffset(1, RunAndIgnoreOutput(exec.Command("kubectl", "create", "ns", operatorNamespace))).To(Succeed())

		By("building the manager(Operator) image")
		ExpectWithOffset(1,
			RunAndIgnoreOutput(exec.Command("make", "docker-build", fmt.Sprintf("IMG=%s", operatorImage)))).To(Succeed())

		By("installing CRDs")
		ExpectWithOffset(1, RunAndIgnoreOutput(exec.Command("make", "install"))).To(Succeed())
	})

	AfterAll(func() {
		By("uninstalling the Node.js deployment")
		ExpectWithOffset(1, UninstallNodeJsDeployment(applicationUnderTestNamespace)).To(Succeed())

		if managerYamlNeedsRevert {
			By("reverting changes to " + managerYaml)
			err := copyFile(managerYamlBackup, managerYaml)
			ExpectWithOffset(1, err).NotTo(HaveOccurred())
			err = os.Remove(managerYamlBackup)
			ExpectWithOffset(1, err).NotTo(HaveOccurred())
		}

		if !skipCertManagerUninstall {
			By("uninstalling the cert-manager bundle")
			UninstallCertManager()
		}

		By("uninstalling the collector")
		Expect(UninstallCollector(applicationUnderTestNamespace)).To(Succeed())

		By("removing manager namespace")
		_ = RunAndIgnoreOutput(exec.Command("kubectl", "delete", "ns", operatorNamespace))

		if applicationNamespaceHasBeenCreated && applicationUnderTestNamespace != "default" {
			By("removing namespace for application under test")
			_ = RunAndIgnoreOutput(exec.Command("kubectl", "delete", "ns", applicationUnderTestNamespace))
		}

		By("switching back to original kubectx " + originalKubeContext)
		output, err := Run(exec.Command("kubectx", originalKubeContext))
		if err != nil {
			fmt.Fprint(GinkgoWriter, err.Error())
		}
		fmt.Fprint(GinkgoWriter, string(output))
	})

	Context("the Dash0 operator's webhook", func() {

		BeforeAll(func() {
			DeployOperator(operatorNamespace, operatorImage)
			fmt.Fprint(GinkgoWriter, "waiting 10 seconds\n")
			time.Sleep(10 * time.Second)
		})

		AfterAll(func() {
			UndeployOperator()
		})

		It("should modify new deployments", func() {
			By("installing the Node.js deployment")
			Expect(InstallNodeJsDeployment(applicationUnderTestNamespace)).To(Succeed())
			SendRequestsAndVerifySpansHaveBeenProduced()
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
