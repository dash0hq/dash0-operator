// SPDX-FileCopyrightText: Copyright 2024 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package e2e

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"time"

	. "github.com/onsi/ginkgo/v2" //nolint:golint,revive
	. "github.com/onsi/gomega"

	"go.opentelemetry.io/collector/pdata/ptrace"
)

const (
	certmanagerVersion             = "v1.14.5"
	certmanagerURLTmpl             = "https://github.com/jetstack/cert-manager/releases/download/%s/cert-manager.yaml"
	tracesJsonMaxLineLength        = 1_048_576
	verifyTelemetryTimeout         = 60 * time.Second
	verifyTelemetryPollingInterval = 500 * time.Millisecond
)

var (
	traceUnmarshaller = &ptrace.JSONUnmarshaler{}
)

func RunAndIgnoreOutput(cmd *exec.Cmd, logCommandArgs ...bool) error {
	_, err := Run(cmd, logCommandArgs...)
	return err
}

// Run executes the provided command within this context
func Run(cmd *exec.Cmd, logCommandArgs ...bool) ([]byte, error) {
	var logCommand bool
	if len(logCommandArgs) > 0 {
		logCommand = logCommandArgs[0]
	} else {
		logCommand = true
	}

	dir, _ := GetProjectDir()
	cmd.Dir = dir

	if err := os.Chdir(cmd.Dir); err != nil {
		fmt.Fprintf(GinkgoWriter, "chdir dir: %s\n", err)
	}

	cmd.Env = append(os.Environ(), "GO111MODULE=on")
	command := strings.Join(cmd.Args, " ")
	if logCommand {
		fmt.Fprintf(GinkgoWriter, "running: %s\n", command)
	}
	output, err := cmd.CombinedOutput()
	if err != nil {
		return output, fmt.Errorf("%s failed with error: (%v) %s", command, err, string(output))
	}

	return output, nil
}

// RunMultiple executes multiple commands
func RunMultiple(cmds []*exec.Cmd, logCommandArgs ...bool) error {
	for _, cmd := range cmds {
		if err := RunAndIgnoreOutput(cmd, logCommandArgs...); err != nil {
			return err
		}
	}
	return nil
}

func RunMultipleFromStrings(cmdsAsStrings [][]string, logCommandArgs ...bool) error {
	cmds := make([]*exec.Cmd, len(cmdsAsStrings))
	for i, cmdStrs := range cmdsAsStrings {
		cmds[i] = exec.Command(cmdStrs[0], cmdStrs[1:]...)
	}
	return RunMultiple(cmds, logCommandArgs...)
}

func EnsureNamespaceExists(namespace string) bool {
	err := RunAndIgnoreOutput(exec.Command("kubectl", "get", "ns", namespace), false)
	if err != nil {
		By(fmt.Sprintf("creating namespace %s", namespace))
		ExpectWithOffset(1,
			RunAndIgnoreOutput(exec.Command("kubectl", "create", "ns", namespace))).To(Succeed())
		return true
	}
	return false
}

// InstallCertManager installs the cert manager bundle.
func InstallCertManager() error {
	url := fmt.Sprintf(certmanagerURLTmpl, certmanagerVersion)
	if err := RunAndIgnoreOutput(exec.Command("kubectl", "apply", "-f", url)); err != nil {
		return err
	}
	// Wait for cert-manager-webhook to be ready, which can take time if cert-manager
	// was re-installed after uninstalling on a cluster.
	err := RunAndIgnoreOutput(
		exec.Command(
			"kubectl",
			"wait",
			"deployment.apps/cert-manager-webhook",
			"--for",
			"condition=Available",
			"--namespace",
			"cert-manager",
			"--timeout",
			"5m",
		))
	if err != nil {
		return err
	}
	err = RunAndIgnoreOutput(
		exec.Command(
			"kubectl",
			"wait",
			"deployment.apps/cert-manager-cainjector",
			"--for",
			"condition=Available",
			"--namespace",
			"cert-manager",
			"--timeout",
			"60s",
		))
	if err != nil {
		return err
	}
	err = RunAndIgnoreOutput(
		exec.Command(
			"kubectl",
			"wait",
			"deployment.apps/cert-manager-cainjector",
			"--for",
			"condition=Available",
			"--namespace",
			"cert-manager",
			"--timeout",
			"60s",
		))
	if err != nil {
		return err
	}

	// Not sure what is going on with that, but if there is no wait time after the cert-manager deployment (even though
	// we explicitly run kubectl wait for all three deployments), we sometimes run into
	//    tls: failed to verify certificate: x509: certificate signed by unknown authority
	// during the "make deploy" step (which deploys the operator).-
	fmt.Fprintf(GinkgoWriter, "waiting for cert-manager to _actually_ become ready (30 seconds wait time)\n")
	time.Sleep(30 * time.Second)
	return nil
}

// UninstallCertManager uninstalls the cert manager
func UninstallCertManager() {
	url := fmt.Sprintf(certmanagerURLTmpl, certmanagerVersion)
	if err := RunAndIgnoreOutput(exec.Command("kubectl", "delete", "-f", url)); err != nil {
		warnError(err)
	}
}

func ReinstallCollectorAndClearExportedTelemetry(namespace string) error {
	_ = UninstallCollector(namespace)
	_ = os.Remove("e2e-test-received-data/traces.jsonl")
	_ = os.Remove("e2e-test-received-data/metrics.jsonl")
	_ = os.Remove("e2e-test-received-data/logs.jsonl")
	err := RunAndIgnoreOutput(
		exec.Command(
			"helm",
			"install",
			"dash0-opentelemetry-collector-daemonset",
			"open-telemetry/opentelemetry-collector",
			"--namespace",
			namespace,
			"--values",
			"test-resources/collector/values.yaml",
			"--set",
			"image.repository=otel/opentelemetry-collector-k8s",
		))
	if err != nil {
		return err
	}
	return RunAndIgnoreOutput(
		exec.Command("kubectl",
			"rollout",
			"status",
			"daemonset",
			"dash0-opentelemetry-collector-daemonset-agent",
			"--namespace",
			namespace,
			"--timeout",
			"30s",
		))
}

func UninstallCollector(namespace string) error {
	return RunAndIgnoreOutput(
		exec.Command(
			"helm",
			"uninstall",
			"dash0-opentelemetry-collector-daemonset",
			"--namespace",
			namespace,
			"--ignore-not-found",
		))
}

func DeployOperator(namespace string, image string) {
	By("deploying the controller-manager")
	ExpectWithOffset(1, RunAndIgnoreOutput(exec.Command("make", "deploy", fmt.Sprintf("IMG=%s", image))))

	var controllerPodName string
	By("validating that the controller-manager pod is running as expected")
	verifyControllerUp := func() error {
		cmd := exec.Command("kubectl", "get",
			"pods", "-l", "control-plane=controller-manager",
			"-o", "go-template={{ range .items }}"+
				"{{ if not .metadata.deletionTimestamp }}"+
				"{{ .metadata.name }}"+
				"{{ \"\\n\" }}{{ end }}{{ end }}",
			"-n", namespace,
		)

		podOutput, err := Run(cmd, false)
		ExpectWithOffset(2, err).NotTo(HaveOccurred())
		podNames := GetNonEmptyLines(string(podOutput))
		if len(podNames) != 1 {
			return fmt.Errorf("expect 1 controller pods running, but got %d", len(podNames))
		}
		controllerPodName = podNames[0]
		ExpectWithOffset(2, controllerPodName).To(ContainSubstring("controller-manager"))

		cmd = exec.Command("kubectl", "get",
			"pods", controllerPodName, "-o", "jsonpath={.status.phase}",
			"-n", namespace,
		)
		status, err := Run(cmd)
		ExpectWithOffset(2, err).NotTo(HaveOccurred())
		if string(status) != "Running" {
			return fmt.Errorf("controller pod in %s status", status)
		}
		return nil
	}

	EventuallyWithOffset(1, verifyControllerUp, 120*time.Second, time.Second).Should(Succeed())
}

func UndeployOperator(namespace string) {
	By("undeploying the controller-manager")
	ExpectWithOffset(1, RunAndIgnoreOutput(exec.Command("make", "undeploy"))).To(Succeed())

	// We need to wait until the namespace is really gone, otherwise the next test/suite that tries to create the operator
	// will run into issues when trying to recreate the namespace which is still in the process of being deleted.
	ExpectWithOffset(1, RunAndIgnoreOutput(exec.Command(
		"kubectl",
		"wait",
		"--for=delete",
		fmt.Sprintf("namespace/%s", namespace),
		"--timeout=60s",
	))).To(Succeed())
}

func DeployDash0Resource(namespace string) {
	ExpectWithOffset(1,
		RunAndIgnoreOutput(exec.Command(
			"kubectl", "apply", "-n", namespace, "-k", "config/samples"))).To(Succeed())
}

func UndeployDash0Resource(namespace string) {
	ExpectWithOffset(1,
		RunAndIgnoreOutput(exec.Command(
			"kubectl", "delete", "-n", namespace, "-k", "config/samples"))).To(Succeed())
}

func InstallNodeJsDeployment(namespace string) error {
	imageName := "dash0-operator-nodejs-20-express-test-app"
	err := RunMultipleFromStrings([][]string{
		{"docker", "build", "test-resources/node.js/express", "-t", imageName},
		{"kubectl", "apply", "-f", "test-resources/node.js/express/deploy.yaml", "--namespace", namespace},
	})
	if err != nil {
		return err
	}

	return RunAndIgnoreOutput(
		exec.Command(
			"kubectl",
			"wait",
			"deployment.apps/dash0-operator-nodejs-20-express-test-deployment",
			"--for",
			"condition=Available",
			"--namespace",
			namespace,
			"--timeout",
			"30s",
		))
}

func UninstallNodeJsDeployment(namespace string) error {
	return RunAndIgnoreOutput(
		exec.Command(
			"kubectl",
			"delete",
			"--namespace",
			namespace,
			"-f",
			"test-resources/node.js/express/deploy.yaml",
		))
}

func SendRequestsAndVerifySpansHaveBeenProduced() {
	timestampLowerBound := time.Now()

	By("verify that the resource has been instrumented and is sending telemetry", func() {
		Eventually(func(g Gomega) {
			output, err := Run(exec.Command("curl", "http://localhost:1207/ohai"), false)
			g.ExpectWithOffset(1, err).NotTo(HaveOccurred())
			g.ExpectWithOffset(1, string(output)).To(ContainSubstring(
				"We make Observability easy for every developer."))
			fileHandle, err := os.Open("e2e-test-received-data/traces.jsonl")
			g.ExpectWithOffset(1, err).NotTo(HaveOccurred())
			defer func() {
				_ = fileHandle.Close()
			}()
			scanner := bufio.NewScanner(fileHandle)
			scanner.Buffer(make([]byte, tracesJsonMaxLineLength), tracesJsonMaxLineLength)

			// read file line by line
			spansFound := false
			for scanner.Scan() {
				resourceSpanBytes := scanner.Bytes()
				traces, err := traceUnmarshaller.UnmarshalTraces(resourceSpanBytes)
				if err != nil {
					// ignore lines that cannot be parsed
					continue
				}
				if spansFound = hasMatchingSpans(traces, timestampLowerBound, isHttpServerSpanWithRoute("/ohai")); spansFound {
					break
				}
			}
			g.Expect(scanner.Err()).NotTo(HaveOccurred())
			g.Expect(spansFound).To(BeTrue(), "expected to find an HTTP server span")
		}, verifyTelemetryTimeout, verifyTelemetryPollingInterval).Should(Succeed())
	})
}

func hasMatchingSpans(traces ptrace.Traces, timestampLowerBound time.Time, matchFn func(span ptrace.Span) bool) bool {
	for i := 0; i < traces.ResourceSpans().Len(); i++ {
		resourceSpan := traces.ResourceSpans().At(i)
		for j := 0; j < resourceSpan.ScopeSpans().Len(); j++ {
			scopeSpan := resourceSpan.ScopeSpans().At(j)
			for k := 0; k < scopeSpan.Spans().Len(); k++ {
				span := scopeSpan.Spans().At(k)
				if span.StartTimestamp().AsTime().After(timestampLowerBound) && matchFn(span) {
					return true
				}
			}
		}
	}
	return false
}

func isHttpServerSpanWithRoute(expectedRoute string) func(span ptrace.Span) bool {
	return func(span ptrace.Span) bool {
		if span.Kind() == ptrace.SpanKindServer {
			route, hasRoute := span.Attributes().Get("http.route")
			if hasRoute && route.Str() == expectedRoute {
				return true
			}
		}
		return false
	}
}

func warnError(err error) {
	fmt.Fprintf(GinkgoWriter, "warning: %v\n", err)
}

// GetNonEmptyLines converts given command output string into individual objects
// according to line breakers, and ignores the empty elements in it.
func GetNonEmptyLines(output string) []string {
	var res []string
	elements := strings.Split(output, "\n")
	for _, element := range elements {
		if element != "" {
			res = append(res, element)
		}
	}

	return res
}

// GetProjectDir will return the directory where the project is
func GetProjectDir() (string, error) {
	wd, err := os.Getwd()
	if err != nil {
		return wd, err
	}
	wd = strings.Replace(wd, "/test/e2e", "", -1)
	return wd, nil
}

func CopyFile(source string, destination string) error {
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
