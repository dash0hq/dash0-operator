// SPDX-FileCopyrightText: Copyright 2024 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package e2e

import (
	"bytes"
	_ "embed"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"text/template"

	. "github.com/onsi/ginkgo/v2" //nolint:golint,revive
	. "github.com/onsi/gomega"
)

var (
	requiredPorts = []int{1207, 4317, 4318}

	//go:embed dash0monitoring.e2e.yaml.template
	dash0MonitoringResourceSource  string
	tmpDash0MonitoringResourceFile *os.File
)

func checkIfRequiredPortsAreBlocked() {
	portsCurrentlyInUseByKubernetesServices, err := run(
		exec.Command(
			"kubectl",
			"get",
			"svc",
			"--all-namespaces",
			"-o",
			"go-template='{{range .items}}{{range.spec.ports}}{{if .port}}{{.port}}{{\"\\n\"}}{{end}}{{end}}{{end}}'",
		))
	Expect(err).NotTo(HaveOccurred())
	portsCurrentlyInUseArray := getNonEmptyLines(portsCurrentlyInUseByKubernetesServices)
	messages := make([]string, 0)
	foundBlockedPort := false
	for _, usedPortStr := range portsCurrentlyInUseArray {
		usedPort, err := strconv.Atoi(usedPortStr)
		if err != nil {
			continue
		}
		for _, requiredPort := range requiredPorts {
			if usedPort == requiredPort {
				messages = append(messages,
					fmt.Sprintf(
						"Port %d is required by the test suite, but it is already in use by a Kubernetes "+
							"service. Please check for conflicting deployed serivces.",
						requiredPort,
					))
				foundBlockedPort = true
			}
		}
	}
	if foundBlockedPort {
		messages = append(messages,
			"Note: If you have used the scripts for manual testing in test-resources, running "+
				"test-resources/bin/test-cleanup.sh might help removing all left-over Kubernetes objects.")
		Fail(strings.Join(messages, "\n"))
	}
}

func renderTemplates() {
	By("render yaml templates via render-templates.sh")
	Expect(runAndIgnoreOutput(exec.Command("test-resources/bin/render-templates.sh"))).To(Succeed())

	By("render resource template via text/template package")
	dash0MonitoringResourceTemplate :=
		template.Must(template.New("dash0monitoring").Parse(dash0MonitoringResourceSource))
	var dash0MonitoringResource bytes.Buffer
	Expect(dash0MonitoringResourceTemplate.Execute(&dash0MonitoringResource, dash0MonitoringTemplateValues{
		IngressEndpoint: "http://otlp-sink.otlp-sink.svc.cluster.local",
	})).To(Succeed())
	var err error
	tmpDash0MonitoringResourceFile, err = os.CreateTemp("/tmp", "dash0monitoring-*.yaml")
	Expect(err).NotTo(HaveOccurred())

	Expect(os.WriteFile(tmpDash0MonitoringResourceFile.Name(), dash0MonitoringResource.Bytes(), 0644)).To(Succeed())
}

func removeRenderedResourceTemplate() {
	if tmpDash0MonitoringResourceFile == nil {
		Fail("tmpDash0MonitoringResourceFile is nil, this should not happen")
	}
	Expect(os.Remove(tmpDash0MonitoringResourceFile.Name())).To(Succeed())
}

func setKubeContext(kubeContextForTest string) (bool, string) {
	By("reading current kubectx")
	kubectxOutput, err := run(exec.Command("kubectx", "-c"))
	Expect(err).NotTo(HaveOccurred())
	originalKubeContext := strings.TrimSpace(kubectxOutput)

	if originalKubeContext != kubeContextForTest {
		By("switching to kubectx docker-desktop, previous context " + originalKubeContext + " will be restored later")
		Expect(runAndIgnoreOutput(exec.Command("kubectx", "docker-desktop"))).To(Succeed())
		return true, originalKubeContext
	} else {
		return false, originalKubeContext
	}
}

func revertKubeCtx(originalKubeContext string) {
	By("switching back to original kubectx " + originalKubeContext)
	output, err := run(exec.Command("kubectx", originalKubeContext))
	if err != nil {
		fmt.Fprint(GinkgoWriter, err.Error())
	}
	fmt.Fprint(GinkgoWriter, output)
}

func recreateNamespace(namespace string) {
	By(fmt.Sprintf("(re)creating namespace %s", namespace))
	output, err := run(exec.Command("kubectl", "get", "ns", namespace))
	if err != nil {
		if strings.Contains(output, "(NotFound)") {
			// The namespace does not exist, that's fine, we will create it further down.
		} else {
			Fail(fmt.Sprintf("kubectl get ns %s failed with unexpected error: %v", namespace, err))
		}
	} else {
		Expect(
			runAndIgnoreOutput(exec.Command("kubectl", "delete", "ns", namespace))).To(Succeed())
		Expect(
			runAndIgnoreOutput(
				exec.Command("kubectl", "wait", "--for=delete", "ns", namespace, "--timeout=60s"))).To(Succeed())
	}

	Expect(
		runAndIgnoreOutput(exec.Command("kubectl", "create", "ns", namespace))).To(Succeed())
}
