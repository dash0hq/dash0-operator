// SPDX-FileCopyrightText: Copyright 2026 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package e2e

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"os/exec"
	"slices"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

// waitForRollout waits until the new pods of a recently modified workload (e.g. after instrumenting or uninstrumenting)
// are ready, and all old pods have terminated.
func waitForRollout(namespace string, runtime runtimeType, workloadTp workloadType) {
	if slices.Contains([]workloadType{
		workloadTypeDaemonSet,
		workloadTypeDeployment,
		workloadTypeStatefulSet,
	}, workloadTp) {
		By(fmt.Sprintf(
			"%s %s: waiting for all new pods to become ready and old pods to get removed",
			runtime.runtimeTypeLabel,
			workloadTp.workloadTypeString))
		Eventually(func(g Gomega) {
			g.Expect(runAndIgnoreOutput(
				exec.Command("kubectl",
					"rollout",
					"status",
					workloadTp.workloadTypeString,
					workloadName(runtime, workloadTp),
					"--namespace",
					namespace,
					"--timeout",
					"60s",
				))).To(Succeed())
		}, 60*time.Second, 1*time.Second).Should(Succeed())
		By(fmt.Sprintf(
			"%s %s: rollout complete, new pods are ready, old pods have been removed",
			runtime.runtimeTypeLabel,
			workloadTp.workloadTypeString))
		return
	} else if workloadTp == workloadTypePod {
		waitUntilStandalonePodIsReady(namespace, runtime, workloadTp)
	} else if workloadTp == workloadTypeReplicaSet {
		waitUntilReplicaSetPodsAreReady(namespace, runtime, workloadTp)
	}
}

func waitUntilStandalonePodIsReady(namespace string, runtime runtimeType, workloadType workloadType) {
	By(fmt.Sprintf(
		"%s %s: waiting for pod to become ready",
		runtime.runtimeTypeLabel,
		workloadType.workloadTypeString))
	Eventually(func(g Gomega) {
		wlName := workloadName(runtime, workloadType)
		podJson, err := run(exec.Command(
			"kubectl",
			"get",
			"pod",
			wlName,
			"--namespace",
			namespace,
			"-o",
			"json",
		), false)
		g.Expect(err).NotTo(HaveOccurred())

		var pod corev1.Pod
		g.Expect(json.Unmarshal([]byte(podJson), &pod)).To(Succeed())
		g.Expect(pod.Status.Phase).To(
			Equal(corev1.PodRunning),
			"Pod \"%s\" is not running: phase=%s",
			wlName,
			pod.Status.Phase,
		)
		for _, condition := range pod.Status.Conditions {
			if condition.Type == corev1.PodReady {
				g.Expect(condition.Status).To(
					Equal(corev1.ConditionTrue),
					"Pod \"%s\" is not ready: Ready condition status=%s",
					wlName,
					condition.Status,
				)
				break
			}
		}
	}, 60*time.Second, 1*time.Second).Should(Succeed())
	By(fmt.Sprintf(
		"%s %s: pod is ready",
		runtime.runtimeTypeLabel,
		workloadType.workloadTypeString))
}

func waitUntilReplicaSetPodsAreReady(namespace string, runtime runtimeType, workloadType workloadType) {
	By(fmt.Sprintf(
		"%s %s: waiting for replicaset to become ready",
		runtime.runtimeTypeLabel,
		workloadType.workloadTypeString))
	Eventually(func(g Gomega) {
		wlName := workloadName(runtime, workloadType)
		replicaSetJson, err := run(exec.Command(
			"kubectl",
			"get",
			"replicaset",
			wlName,
			"--namespace",
			namespace,
			"-o",
			"json",
		), false)
		g.Expect(err).NotTo(HaveOccurred())

		var rs appsv1.ReplicaSet
		g.Expect(json.Unmarshal([]byte(replicaSetJson), &rs)).To(Succeed())
		g.Expect(rs.Spec.Replicas).ToNot(BeNil())
		g.Expect(*rs.Spec.Replicas).To(BeNumerically(">=", 1))
		g.Expect(rs.Status.ReadyReplicas).To(
			Equal(*rs.Spec.Replicas),
			"Not all pods of replicaset \"%s\" are ready: desired=%d, readyReplicas=%d",
			wlName,
			*rs.Spec.Replicas,
			rs.Status.ReadyReplicas,
		)
	}, 60*time.Second, 1*time.Second).Should(Succeed())
	By(fmt.Sprintf(
		"%s %s: all pods are ready",
		runtime.runtimeTypeLabel,
		workloadType.workloadTypeString))
}

func waitForApplicationToBecomeResponsive(
	runtime runtimeType,
	workloadType workloadType,
	route string,
	query string,
) {
	if workloadType.isBatch {
		return
	}
	Eventually(func(g Gomega) {
		sendRequest(g, runtime, workloadType, route, query)
	}, 30*time.Second, pollingInterval).Should(Succeed())
}
