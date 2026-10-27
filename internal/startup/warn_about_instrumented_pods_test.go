// SPDX-FileCopyrightText: Copyright 2026 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package startup

import (
	"context"
	"errors"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes/fake"
	k8stesting "k8s.io/client-go/testing"
	"sigs.k8s.io/controller-runtime/pkg/log"

	"github.com/dash0hq/dash0-operator/internal/util"
	"github.com/dash0hq/dash0-operator/internal/util/logd"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	. "github.com/dash0hq/dash0-operator/test/util"
)

var _ = Describe("the check for instrumented pods in SCE-only mode", func() {
	var (
		ctx              context.Context
		capturingLogSink *CapturingLogSink
	)

	BeforeEach(func() {
		var logger logd.Logger
		logger, capturingLogSink = NewCapturingLogger()
		ctx = log.IntoContext(context.Background(), logger.Logger)
	})

	pod := func(name string, instrumented string) *corev1.Pod {
		return &corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Name:      name,
				Namespace: TestNamespaceName,
				Labels:    map[string]string{util.InstrumentedLabelKey: instrumented},
			},
		}
	}

	It("does not warn when no pod is instrumented", func() {
		clientset := fake.NewClientset(pod("not-instrumented", "false"))
		Expect(NewWarnAboutInstrumentedPodsRunnable(clientset).Start(ctx)).To(Succeed())
		capturingLogSink.HasLogMessage(Default, logMsgNoInstrumentedPods)
	})

	It("warns when there is an instrumented pod", func() {
		clientset := fake.NewClientset(pod("not-instrumented", "false"), pod("instrumented", "true"))
		Expect(NewWarnAboutInstrumentedPodsRunnable(clientset).Start(ctx)).To(Succeed())
		capturingLogSink.HasLogMessage(
			Default, fmt.Sprintf(logMsgInstrumentedPodsInSceOnlyMode, TestNamespaceName, "instrumented"))
		capturingLogSink.HasNoLogMessage(Default, logMsgNoInstrumentedPods)
	})

	It("does not fail the startup when listing the pods fails", func() {
		clientset := fake.NewClientset()
		clientset.PrependReactor("list", "pods", func(k8stesting.Action) (bool, runtime.Object, error) {
			return true, nil, errors.New("list failed")
		})
		Expect(NewWarnAboutInstrumentedPodsRunnable(clientset).Start(ctx)).To(Succeed())
		capturingLogSink.HasLogMessage(Default, "Checking for pods that have been instrumented by the operator failed.")
	})
})
