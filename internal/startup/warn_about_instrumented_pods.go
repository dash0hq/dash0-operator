// SPDX-FileCopyrightText: Copyright 2026 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package startup

import (
	"context"
	"fmt"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"

	"github.com/dash0hq/dash0-operator/internal/util"
	"github.com/dash0hq/dash0-operator/internal/util/logd"
)

const (
	logMsgNoInstrumentedPods            = "No pods instrumented by the operator have been found."
	logMsgInstrumentedPodsInSceOnlyMode = "SCE-only mode is active, but there are pods that have been instrumented by " +
		"the operator (for example %s/%s). Their telemetry is lost, since the collector daemonset is not deployed in " +
		"SCE-only mode. To remove the instrumentation, set operator.signalControl.sceOnlyMode=false via helm upgrade, " +
		"wait for the operator to be rolled out, uninstall the operator, then re-install it with " +
		"operator.signalControl.sceOnlyMode=true."
)

// WarnAboutInstrumentedPodsRunnable checks once at startup whether pods instrumented by the operator are still running
// while SCE-only mode is active. These pods keep sending telemetry to the collector daemonset, which SCE-only mode does
// not deploy.
type WarnAboutInstrumentedPodsRunnable struct {
	clientset kubernetes.Interface
}

// NewWarnAboutInstrumentedPodsRunnable creates a WarnAboutInstrumentedPodsRunnable.
func NewWarnAboutInstrumentedPodsRunnable(clientset kubernetes.Interface) *WarnAboutInstrumentedPodsRunnable {
	return &WarnAboutInstrumentedPodsRunnable{
		clientset: clientset,
	}
}

// NeedLeaderElection implements the LeaderElectionRunnable interface, which indicates that the
// WarnAboutInstrumentedPodsRunnable requires leader election.
func (r *WarnAboutInstrumentedPodsRunnable) NeedLeaderElection() bool {
	return true
}

// Start runs the check for instrumented pods.
func (r *WarnAboutInstrumentedPodsRunnable) Start(ctx context.Context) error {
	logger := logd.FromContext(ctx)
	pods, err := r.clientset.CoreV1().Pods(metav1.NamespaceAll).List(ctx, metav1.ListOptions{
		LabelSelector: fmt.Sprintf("%s=true", util.InstrumentedLabelKey),
		Limit:         1,
	})
	if err != nil {
		logger.ErrorAsWarn(err, "Checking for pods that have been instrumented by the operator failed.")
		return nil
	}
	if len(pods.Items) == 0 {
		logger.Debug(logMsgNoInstrumentedPods)
		return nil
	}
	pod := pods.Items[0]
	logger.WarnTelemetryCollectionIssue(fmt.Sprintf(logMsgInstrumentedPodsInSceOnlyMode, pod.Namespace, pod.Name))
	return nil
}
