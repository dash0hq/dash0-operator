// SPDX-FileCopyrightText: Copyright 2026 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package startup

import (
	"context"
	"time"

	"k8s.io/apimachinery/pkg/util/wait"
	"sigs.k8s.io/controller-runtime/pkg/client"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	. "github.com/dash0hq/dash0-operator/test/util"
)

var _ = Describe("instrument at startup", Ordered, func() {
	ctx := context.Background()

	var (
		runnable       *InstrumentAtStartupRunnable
		logger, _      = NewCapturingLogger()
		createdObjects []client.Object
	)

	BeforeAll(func() {
		EnsureOperatorNamespaceExists(ctx, k8sClient)
	})

	BeforeEach(func() {
		createdObjects = make([]client.Object, 0)
		readyCheckExecuter := NewReadyCheckExecuter(k8sClient, OperatorNamespace, OperatorWebhookServiceName)
		readyCheckExecuter.retryBackoff = wait.Backoff{Duration: 10 * time.Millisecond, Factor: 1, Steps: 2}
		runnable = &InstrumentAtStartupRunnable{readyCheckExecuter: readyCheckExecuter}
	})

	AfterEach(func() {
		createdObjects = DeleteAllCreatedObjects(ctx, k8sClient, createdObjects)
	})

	It("should not proceed if the webhook service endpoint does not become ready", func() {
		webhookService := WebhookService()
		Expect(k8sClient.Create(ctx, &webhookService)).To(Succeed())
		createdObjects = append(createdObjects, &webhookService)
		endpointSlice := WebhookServiceEndpointSlice(true, false)
		Expect(k8sClient.Create(ctx, &endpointSlice)).To(Succeed())
		createdObjects = append(createdObjects, &endpointSlice)

		Expect(runnable.waitForWebhookService(ctx, logger)).To(BeFalse())
	})

	It("should proceed once the webhook service endpoint is ready", func() {
		webhookService := WebhookService()
		Expect(k8sClient.Create(ctx, &webhookService)).To(Succeed())
		createdObjects = append(createdObjects, &webhookService)
		endpointSlice := WebhookServiceEndpointSlice(true, true)
		Expect(k8sClient.Create(ctx, &endpointSlice)).To(Succeed())
		createdObjects = append(createdObjects, &endpointSlice)

		Expect(runnable.waitForWebhookService(ctx, logger)).To(BeTrue())
	})
})
