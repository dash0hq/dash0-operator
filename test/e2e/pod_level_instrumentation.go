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
	podInstrumentationTimeout = 4 * time.Minute
)

// podLabelSelector returns a label selector matching the pods of one workload of the Node.js test application.
func podLabelSelector(runtime runtimeType, workloadType workloadType) string {
	switch workloadType {
	case workloadTypeCronjob:
		return "app.kubernetes.io/name=test-cronjob"
	case workloadTypeJob:
		return "app.kubernetes.io/name=test-job"
	default:
		return fmt.Sprintf("app=%s-app", workloadName(runtime, workloadType))
	}
}

// applyPodDisruptionBudget creates or updates a pod disruption budget for the pods of the given workload, with the
// given maxUnavailable value, and deletes it when the test is cleaned up.
func applyPodDisruptionBudget(namespace string, runtime runtimeType, workloadType workloadType, maxUnavailable int) {
	name := fmt.Sprintf("%s-pdb", workloadName(runtime, workloadType))
	matchLabels := strings.SplitN(podLabelSelector(runtime, workloadType), "=", 2)
	pdbYaml := fmt.Sprintf(`apiVersion: policy/v1
kind: PodDisruptionBudget
metadata:
  name: %s
  namespace: %s
spec:
  maxUnavailable: %d
  selector:
    matchLabels:
      %s: %s
`, name, namespace, maxUnavailable, matchLabels[0], matchLabels[1])
	applyCmd := exec.Command("kubectl", "apply", "-f", "-")
	applyCmd.Stdin = strings.NewReader(pdbYaml)
	Expect(runAndIgnoreOutput(applyCmd)).To(Succeed())
	DeferCleanup(func() {
		_ = runAndIgnoreOutput(exec.Command(
			"kubectl",
			"--namespace", namespace,
			"delete", "poddisruptionbudget", name,
			"--ignore-not-found",
		))
	})
}

// readPodsOfWorkload returns the pods of the given workload which are not being deleted.
func readPodsOfWorkload(g Gomega, namespace string, runtime runtimeType, workloadType workloadType) []corev1.Pod {
	output, err := run(exec.Command(
		"kubectl",
		"get",
		"pods",
		"--namespace",
		namespace,
		"--selector",
		podLabelSelector(runtime, workloadType),
		"-o",
		"json",
	), false)
	g.Expect(err).NotTo(HaveOccurred())
	var podList corev1.PodList
	g.Expect(json.Unmarshal([]byte(output), &podList)).To(Succeed())
	pods := make([]corev1.Pod, 0, len(podList.Items))
	for _, pod := range podList.Items {
		if pod.DeletionTimestamp == nil {
			pods = append(pods, pod)
		}
	}
	return pods
}

func readPodUids(namespace string, runtime runtimeType, workloadType workloadType) []string {
	var uids []string
	Eventually(func(g Gomega) {
		pods := readPodsOfWorkload(g, namespace, runtime, workloadType)
		g.Expect(pods).ToNot(BeEmpty())
		uids = make([]string, 0, len(pods))
		for _, pod := range pods {
			uids = append(uids, string(pod.UID))
		}
	}, labelChangeTimeout, pollingInterval).Should(Succeed())
	return uids
}

// verifyThatPodsHaveBeenInstrumentedByTheWebhook verifies that all pods of the workload have been instrumented by the
// webhook, that none of the given pods from before the workload has been instrumented is still running, that the
// workload resource itself has not been modified, and that the workload produces spans.
func verifyThatPodsHaveBeenInstrumentedByTheWebhook(
	namespace string,
	runtime runtimeType,
	workloadType workloadType,
	testId string,
	images Images,
	uninstrumentedPodUids []string,
	timestampLowerBound time.Time,
) {
	By(fmt.Sprintf("%s %s: waiting for the pods to get instrumented by the webhook",
		runtime.runtimeTypeLabel,
		workloadType.workloadTypeString))
	Eventually(func(g Gomega) {
		pods := readPodsOfWorkload(g, namespace, runtime, workloadType)
		g.Expect(pods).ToNot(BeEmpty())
		for _, pod := range pods {
			g.Expect(uninstrumentedPodUids).ToNot(ContainElement(string(pod.UID)),
				"the uninstrumented pod %s is still running", pod.Name)
			verifyPodLabels(g, pod, images)
		}
		if workloadType != workloadTypePod {
			verifyNoDash0Labels(g, namespace, runtime, workloadType)
		}
	}, podInstrumentationTimeout, pollingInterval).Should(Succeed())

	waitForRollout(namespace, runtime, workloadType)
	waitForApplicationToBecomeResponsive(runtime, workloadType, testEndpoint, "")

	By(fmt.Sprintf("%s %s: waiting for spans to be captured", runtime.runtimeTypeLabel, workloadType.workloadTypeString))
	spanTimeout := verifyTelemetryTimeout
	if workloadType == workloadTypeCronjob {
		spanTimeout = 90 * time.Second
	}
	query := fmt.Sprintf("id=%s", testId)
	Eventually(func(g Gomega) {
		verifySpans(g, runtime, workloadType, testEndpoint, query, timestampLowerBound, true)
	}, spanTimeout, pollingInterval).Should(Succeed())
}

func verifyPodLabels(g Gomega, pod corev1.Pod, images Images) {
	g.Expect(pod.Labels["dash0.com/instrumented"]).To(Equal("true"), "the pod %s has not been instrumented", pod.Name)
	g.Expect(pod.Labels["dash0.com/instrumented-by"]).To(Equal("webhook"))
	verifyImageLabel(g, pod.Labels["dash0.com/operator-image"], images.operator, "ghcr.io/dash0hq/operator-controller:")
	verifyImageLabel(
		g,
		pod.Labels["dash0.com/instrumentation-image"],
		images.instrumentation,
		"ghcr.io/dash0hq/instrumentation:",
	)
}

// verifyThatPodsHaveNeitherBeenEvictedNorInstrumented verifies that exactly the given pods of the workload are still
// running, and that they have not been instrumented.
func verifyThatPodsHaveNeitherBeenEvictedNorInstrumented(
	namespace string,
	runtime runtimeType,
	workloadType workloadType,
	podUids []string,
) {
	By(fmt.Sprintf("%s %s: verifying that the pods have neither been evicted nor instrumented",
		runtime.runtimeTypeLabel,
		workloadType.workloadTypeString))
	Eventually(func(g Gomega) {
		verifyExactlyTheseUninstrumentedPods(g, namespace, runtime, workloadType, podUids)
	}, labelChangeTimeout, pollingInterval).Should(Succeed())
}

// verifyThatPodsAreNeitherEvictedNorInstrumented verifies for a while that exactly the given pods of the workload keep
// running, and that they do not get instrumented.
func verifyThatPodsAreNeitherEvictedNorInstrumented(
	namespace string,
	runtime runtimeType,
	workloadType workloadType,
	podUids []string,
) {
	By(fmt.Sprintf("%s %s: verifying that the pods are neither evicted nor instrumented",
		runtime.runtimeTypeLabel,
		workloadType.workloadTypeString))
	Consistently(func(g Gomega) {
		verifyExactlyTheseUninstrumentedPods(g, namespace, runtime, workloadType, podUids)
	}, 15*time.Second, pollingInterval).Should(Succeed())
}

func verifyExactlyTheseUninstrumentedPods(
	g Gomega,
	namespace string,
	runtime runtimeType,
	workloadType workloadType,
	podUids []string,
) {
	pods := readPodsOfWorkload(g, namespace, runtime, workloadType)
	actualUids := make([]string, 0, len(pods))
	for _, pod := range pods {
		actualUids = append(actualUids, string(pod.UID))
		g.Expect(pod.Labels).ToNot(HaveKey("dash0.com/instrumented"))
	}
	g.Expect(actualUids).To(ConsistOf(podUids))
}

// restartAndVerifyThatNewPodsAreNotInstrumented restarts the workload and verifies that the pods replacing the given
// pods have not been instrumented by the webhook.
func restartAndVerifyThatNewPodsAreNotInstrumented(
	namespace string,
	runtime runtimeType,
	workloadType workloadType,
	oldPodUids []string,
) {
	By(fmt.Sprintf("%s %s: restarting the workload", runtime.runtimeTypeLabel, workloadType.workloadTypeString))
	Expect(runAndIgnoreOutput(exec.Command(
		"kubectl",
		"rollout",
		"restart",
		"--namespace",
		namespace,
		fmt.Sprintf("%s/%s", workloadType.workloadTypeString, workloadName(runtime, workloadType)),
	))).To(Succeed())
	waitForRollout(namespace, runtime, workloadType)

	By(fmt.Sprintf("%s %s: verifying that the new pods have not been instrumented",
		runtime.runtimeTypeLabel,
		workloadType.workloadTypeString))
	Eventually(func(g Gomega) {
		pods := readPodsOfWorkload(g, namespace, runtime, workloadType)
		g.Expect(pods).ToNot(BeEmpty())
		for _, pod := range pods {
			g.Expect(oldPodUids).ToNot(ContainElement(string(pod.UID)), "the old pod %s is still running", pod.Name)
			g.Expect(pod.Labels).ToNot(HaveKey("dash0.com/instrumented"))
		}
	}, podInstrumentationTimeout, pollingInterval).Should(Succeed())
}
