// SPDX-FileCopyrightText: Copyright 2026 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package e2e

import (
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

const (
	operatorManagerDeploymentName   = "dash0-operator-controller"
	operatorManagerLeaseName        = "5ae7ac41.dash0.com"
	operatorManagerPodLabelSelector = "app.kubernetes.io/name=dash0-operator,app.kubernetes.io/component=controller"
	instrumentationWebhookName      = "dash0-operator-injector"
	instrumentationWebhookPath      = "/workloads/inject"
	operatorManagerRolloutTimeout   = 3 * time.Minute
)

func waitForOperatorManagerRollout() {
	By("waiting for the operator manager deployment to complete its rollout")
	Expect(runAndIgnoreOutput(exec.Command(
		"kubectl",
		"rollout",
		"status",
		"--namespace", operatorNamespace,
		fmt.Sprintf("deployment/%s", operatorManagerDeploymentName),
		fmt.Sprintf("--timeout=%s", operatorManagerRolloutTimeout),
	))).To(Succeed())
}

// readOperatorManagerLeader returns the name of the operator manager pod that holds the leader election lease.
func readOperatorManagerLeader(g Gomega) string {
	holderIdentity, err := run(exec.Command(
		"kubectl",
		"get",
		"lease",
		"--namespace", operatorNamespace,
		operatorManagerLeaseName,
		"-o", "jsonpath={.spec.holderIdentity}",
	), false)
	g.Expect(err).NotTo(HaveOccurred())
	// controller-runtime uses <hostname>_<uuid> as the identity, and the hostname of a pod is its name.
	podName, _, found := strings.Cut(strings.TrimSpace(holderIdentity), "_")
	g.Expect(found).To(BeTrue(), "unexpected lease holder identity %q", holderIdentity)
	return podName
}

// readOperatorManagerPods returns the names of the operator manager pods that belong to the deployment and are not
// being deleted.
func readOperatorManagerPods(g Gomega) []string {
	output, err := run(exec.Command(
		"kubectl",
		"get",
		"pods",
		"--namespace", operatorNamespace,
		"--selector", operatorManagerPodLabelSelector,
		"-o", "json",
	), false)
	g.Expect(err).NotTo(HaveOccurred())
	var podList corev1.PodList
	g.Expect(json.Unmarshal([]byte(output), &podList)).To(Succeed())
	var podNames []string
	for _, pod := range podList.Items {
		if pod.DeletionTimestamp == nil {
			podNames = append(podNames, pod.Name)
		}
	}
	return podNames
}

func verifyOperatorManagerPodDisruptionBudget() {
	By("verifying that the operator manager deployment has a pod disruption budget")
	maxUnavailable, err := run(exec.Command(
		"kubectl",
		"get",
		"poddisruptionbudget",
		"--namespace", operatorNamespace,
		operatorManagerDeploymentName,
		"-o", "jsonpath={.spec.maxUnavailable}",
	))
	Expect(err).NotTo(HaveOccurred())
	Expect(strings.TrimSpace(maxUnavailable)).To(Equal("1"))
}

/*
detachOperatorManagerPod removes the pod from the selector of the operator manager deployment and the webhook service,
by changing one of its labels. The pod keeps running, and keeps the leader election lease if it holds it, but it does
not receive webhook requests anymore, and the replica set creates another pod in its place. The detached pod is deleted
when the test is cleaned up.
*/
func detachOperatorManagerPod(podName string) {
	By(fmt.Sprintf("detaching the operator manager pod %s from the deployment and the webhook service", podName))
	Expect(runAndIgnoreOutput(exec.Command(
		"kubectl",
		"label",
		"pod",
		"--namespace", operatorNamespace,
		podName,
		"app.kubernetes.io/component=detached-by-e2e-test",
		"--overwrite",
	))).To(Succeed())
	DeferCleanup(func() {
		_ = runAndIgnoreOutput(exec.Command(
			"kubectl",
			"delete",
			"pod",
			"--namespace", operatorNamespace,
			podName,
			"--ignore-not-found",
		))
	})
}

const leaseAcquiredLogPattern = "(?i)successfully acquired lease"

func readOperatorManagerLogs(podName string) string {
	logs, err := run(exec.Command(
		"kubectl",
		"logs",
		"--namespace", operatorNamespace,
		podName,
		"--container", "manager",
	), false)
	Expect(err).NotTo(HaveOccurred())
	return logs
}

// verifyThatLogsShowLeaderElection verifies that the pod has acquired the leader election lease, which makes sure
// that verifyThatLogsShowNoLeaderElection looks for the right log line.
func verifyThatLogsShowLeaderElection(podName string) {
	Expect(readOperatorManagerLogs(podName)).To(MatchRegexp(leaseAcquiredLogPattern))
}

// verifyThatLogsShowNoLeaderElection verifies that the pod has not acquired the leader election lease.
func verifyThatLogsShowNoLeaderElection(podName string) {
	Expect(readOperatorManagerLogs(podName)).NotTo(MatchRegexp(leaseAcquiredLogPattern))
}

// setInstrumentationWebhookPath sets the path of the instrumentation webhook.
func setInstrumentationWebhookPath(path string) error {
	return runAndIgnoreOutput(exec.Command(
		"kubectl",
		"patch",
		"mutatingwebhookconfiguration",
		instrumentationWebhookName,
		"--type=json",
		"-p", fmt.Sprintf(`[{"op":"replace","path":"/webhooks/0/clientConfig/service/path","value":%q}]`, path),
	))
}

/*
makeInstrumentationWebhookUnavailable makes the instrumentation webhook unavailable until the returned function is
called, or until the test is cleaned up. The operator manager pods, including the leader, keep running.

Requests to the webhook are sent to a path that the operator manager does not serve, so they fail, which the API server
ignores due to the failure policy Ignore, and pods are created without instrumentation. Removing the endpoints of the
webhook service would not be enough for that, since the API server keeps using its open connections to the operator
manager pods.
*/
func makeInstrumentationWebhookUnavailable() func() {
	By("making the instrumentation webhook unavailable")
	Expect(setInstrumentationWebhookPath("/not-served-by-the-operator")).To(Succeed())
	restored := false
	restore := func() error {
		if restored {
			return nil
		}
		restored = true
		return setInstrumentationWebhookPath(instrumentationWebhookPath)
	}
	DeferCleanup(func() {
		// The operator might have been undeployed already.
		_ = restore()
	})
	return func() {
		By("making the instrumentation webhook available again")
		Expect(restore()).To(Succeed())
	}
}
