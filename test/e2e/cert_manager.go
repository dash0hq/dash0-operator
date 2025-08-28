// SPDX-FileCopyrightText: Copyright 2025 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package e2e

import (
	_ "embed"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"text/template"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

type certificateValues struct {
	OperatorNamespace string
}

const (
	certmanagerVersion = "v1.18.2"
)

var (
	certManagerHasBeenInstalled = false

	//go:embed certificate-and-issuer.yaml.template
	certificateAndIssuerSource   string
	certificateAndIssuerTemplate *template.Template
)

func ensureCertManagerIsInstalled() {
	err := runAndIgnoreOutput(exec.Command("kubectl", "get", "ns", "cert-manager"), false)
	if err != nil {
		By("installing the cert-manager")
		e2ePrint(
			"Hint: To get a faster feedback cycle on e2e tests, deploy cert-manager once via " +
				"test-resources/cert-manager/deploy.sh. If the e2e tests find an existing cert-manager namespace, they " +
				"will not deploy cert-manager and they will also not undeploy it after running the test suite.\n",
		)
		Expect(installCertManager()).To(Succeed())
		certManagerHasBeenInstalled = true
		return
	} else {
		e2ePrint("The cert-manager namespace exists, assuming cert-manager has been deployed already.\n")
	}
}

func installCertManager() error {
	repoList, err := run(exec.Command("helm", "repo", "list"))
	if err != nil {
		return err
	}
	if !strings.Contains(repoList, "jetstack") {
		e2ePrint("The helm repo for cert-manager has not been found, adding it now.\n")
		if err := runAndIgnoreOutput(
			exec.Command(
				"helm",
				"repo",
				"add",
				"jetstack",
				"https://charts.jetstack.io",
				"--force-update",
			)); err != nil {
			return err
		}
		e2ePrint("Running helm repo update.\n")
		if err = runAndIgnoreOutput(exec.Command("helm", "repo", "update")); err != nil {
			return err
		}
	}

	if err := runAndIgnoreOutput(exec.Command(
		"helm",
		"install",
		"cert-manager",
		"jetstack/cert-manager",
		"--namespace",
		"cert-manager",
		"--create-namespace",
		"--version",
		certmanagerVersion,
		"--set",
		"installCRDs=true",
		"--timeout",
		"5m",
	)); err != nil {
		return err
	}

	// Wait for cert-manager-webhook to be ready, which can take time if cert-manager
	// was re-installed after uninstalling on a cluster.
	if err := runAndIgnoreOutput(
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
		)); err != nil {
		return err
	}
	if err := runAndIgnoreOutput(
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
		)); err != nil {
		return err
	}
	if err := runAndIgnoreOutput(
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
		)); err != nil {
		return err
	}

	return nil
}

func uninstallCertManagerIfApplicable() {
	if certManagerHasBeenInstalled {
		By("uninstalling the cert-manager bundle")
		uninstallCertManager()
	} else {
		e2ePrint(
			"Note: The e2e test suite did not install cert-manager, thus it will also not uninstall it.\n",
		)
	}
}

func uninstallCertManager() {
	if err := runAndIgnoreOutput(exec.Command(
		"helm",
		"uninstall",
		"cert-manager",
		"--namespace",
		"cert-manager",
		"--ignore-not-found",
	)); err != nil {
		e2ePrint("warning: %v\n", err)
	}

	if err := runAndIgnoreOutput(
		exec.Command(
			"kubectl", "delete", "namespace", "cert-manager", "--ignore-not-found")); err != nil {
		e2ePrint("warning: %v\n", err)
	}
}

func deployCertificateAndIssuer(operatorNamespace string) {
	values := certificateValues{
		OperatorNamespace: operatorNamespace,
	}
	renderedResourcesFileName := renderCertificateAndIssuerTemplate(values)
	defer func() {
		Expect(os.Remove(renderedResourcesFileName)).To(Succeed())
	}()

	By(fmt.Sprintf(
		"deploying certificate and issuer namespace %s", operatorNamespace))
	Expect(runAndIgnoreOutput(exec.Command(
		"kubectl",
		"apply",
		"-n",
		operatorNamespace,
		"-f",
		renderedResourcesFileName,
	))).To(Succeed())
}

func renderCertificateAndIssuerTemplate(values certificateValues) string {
	certificateAndIssuerTemplate = initTemplateOnce(
		certificateAndIssuerTemplate,
		certificateAndIssuerSource,
		"certificateandissuer",
	)
	return renderResourceTemplate(certificateAndIssuerTemplate, values, "certificateandissuer")
}

func removeCertificateAndIssuer(operatorNamespace string) {
	_ = runAndIgnoreOutput(exec.Command(
		"kubectl",
		"delete",
		"--ignore-not-found",
		"-n",
		operatorNamespace,
		"Certificate",
		"e2e-serving-certificate",
	))
	_ = runAndIgnoreOutput(exec.Command(
		"kubectl",
		"delete",
		"--ignore-not-found",
		"-n",
		operatorNamespace,
		"Issuer",
		"e2e-issuer",
	))
}
