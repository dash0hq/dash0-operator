// SPDX-FileCopyrightText: Copyright 2025 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package e2e

import (
	"os"
	"os/exec"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func ensureNginxIngressControllerIsInstalled(cleanupSteps *neccessaryCleanupSteps) {
	if os.Getenv("DEPLOY_NGINX_INGRESS") == "false" {
		return
	}
	By("checking if ingress-nginx is already running")
	if err := runAndIgnoreOutput(
		exec.Command(
			"kubectl",
			"wait",
			"--namespace",
			"ingress-nginx",
			"--for=condition=ready",
			"pod",
			"--selector=app.kubernetes.io/component=controller",
			"--timeout=5s",
		)); err == nil {
		e2ePrint("ingress-nginx is already running\n")
		return
	}

	e2ePrint(
		"Hint: To get a faster feedback cycle on e2e tests, deploy the ingress-nginx once via the following commands:\n" +
			"  kubectl apply -k test-resources/nginx\n" +
			"If the e2e tests find an existing ingress-nginx installation, they will not deploy the ingress-nginx and " +
			"they will also not undeploy it after running the test suite.\n",
	)
	installNginxIngressController(cleanupSteps)
}

func installNginxIngressController(cleanupSteps *neccessaryCleanupSteps) {
	By("deploying ingress-nginx")
	cleanupSteps.removeIngressNginx = true
	Expect(runAndIgnoreOutput(exec.Command(
		"kubectl", "apply", "-k", "test-resources/nginx",
	))).To(Succeed())

	By("waiting for ingress-nginx")
	Expect(runAndIgnoreOutput(exec.Command(
		"kubectl",
		"wait",
		"--namespace",
		"ingress-nginx",
		"--for=condition=ready",
		"pod",
		"--selector=app.kubernetes.io/component=controller",
		"--timeout=90s",
	))).To(Succeed())

	// The ingress-nginx pod becoming ready (checked above) is not sufficient for being able to continue. We also need
	// to wait for the ingress-nginx-controller-admission webhook service to be ready and reachable. Otherwise, when
	// deploying right after installing the ingress-nginx, we might run into
	//   Internal error occurred: failed calling webhook "validate.nginx.ingress.kubernetes.io": failed to call webhook:
	//   Post "https://ingress-nginx-controller-admission.ingress-nginx.svc:443/networking/v1/ingresses?timeout=10s":
	//   dial tcp ...:443: connect: connection refused
	// Hence, we also wait until the endpointslice for this webhook service becomes ready and has a port assigned.
	Eventually(func(g Gomega) {
		g.Expect(runAndIgnoreOutput(exec.Command(
			"kubectl",
			"get",
			"--namespace",
			"ingress-nginx",
			"service",
			"ingress-nginx-controller-admission",
		))).To(Succeed())
		endpointSliceReady, err := run(exec.Command(
			"kubectl",
			"get",
			"--namespace",
			"ingress-nginx",
			"endpointslice",
			"-l",
			"kubernetes.io/service-name=ingress-nginx-controller-admission",
			"-o=jsonpath='{.items[0].endpoints[0].conditions.ready}'",
		))
		g.Expect(err).ToNot(HaveOccurred())
		g.Expect(endpointSliceReady).To(Equal("'true'"))
		endpointSlicePort, err := run(exec.Command(
			"kubectl",
			"get",
			"--namespace",
			"ingress-nginx",
			"endpointslice",
			"-l",
			"kubernetes.io/service-name=ingress-nginx-controller-admission",
			"-o=jsonpath='{.items[0].ports[0].port}'",
		))
		g.Expect(err).ToNot(HaveOccurred())
		g.Expect(endpointSlicePort).To(Equal("'8443'"))
	}, 60*time.Second, 1*time.Second).Should(Succeed())
}

func undeployNginxIngressController(cleanupSteps *neccessaryCleanupSteps) {
	if !cleanupSteps.removeIngressNginx {
		return
	}
	By("removing nginx ingress controller")
	Expect(runAndIgnoreOutput(exec.Command(
		"kubectl", "delete", "-k", "test-resources/nginx", "--ignore-not-found",
	))).To(Succeed())
}
