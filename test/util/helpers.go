// SPDX-FileCopyrightText: Copyright 2024 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package util

import (
	"context"
	"fmt"

	"github.com/dash0hq/dash0-operator/api/v1alpha1"
	"github.com/onsi/gomega"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

func VerifySuccessEvent(
	ctx context.Context,
	clientset *kubernetes.Clientset,
	namespace string,
	resourceName string,
	eventSource string,
) {
	allEvents, err := clientset.CoreV1().Events(namespace).List(ctx, v1.ListOptions{})
	gomega.Expect(err).NotTo(gomega.HaveOccurred())
	gomega.Expect(allEvents.Items).To(gomega.HaveLen(1))
	gomega.Expect(allEvents.Items).To(
		gomega.ContainElement(
			MatchEvent(
				namespace,
				resourceName,
				v1alpha1.ReasonSuccessfulInstrumentation,
				fmt.Sprintf("Dash0 instrumentation by %s has been successful.", eventSource),
			)))
}
