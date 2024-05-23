// SPDX-FileCopyrightText: Copyright 2024 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package util

import (
	"context"
	"fmt"

	"github.com/dash0hq/dash0-operator/internal/util"
	. "github.com/onsi/gomega"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

func VerifySuccessEvent(
	ctx context.Context,
	clientset *kubernetes.Clientset,
	namespace string,
	resourceName string,
	eventSource string,
) {
	verifyEvent(
		ctx,
		clientset,
		namespace,
		resourceName,
		util.ReasonSuccessfulInstrumentation,
		fmt.Sprintf("Dash0 instrumentation by %s has been successful.", eventSource),
	)
}

func VerifyFailureEvent(
	ctx context.Context,
	clientset *kubernetes.Clientset,
	namespace string,
	resourceName string,
	eventSource string,
	message string,
) {
	verifyEvent(
		ctx,
		clientset,
		namespace,
		resourceName,
		util.ReasonFailedInstrumentation,
		message,
	)
}

func verifyEvent(
	ctx context.Context,
	clientset *kubernetes.Clientset,
	namespace string,
	resourceName string,
	reason util.Reason,
	message string,
) {
	allEvents, err := clientset.CoreV1().Events(namespace).List(ctx, metav1.ListOptions{})
	Expect(err).NotTo(HaveOccurred())
	Expect(allEvents.Items).To(HaveLen(1))
	Expect(allEvents.Items).To(
		ContainElement(
			MatchEvent(
				namespace,
				resourceName,
				reason,
				message,
			)))
}

func DeleteAllEvents(
	ctx context.Context,
	clientset *kubernetes.Clientset,
	namespace string,
) {
	err := clientset.CoreV1().Events(namespace).DeleteCollection(ctx, metav1.DeleteOptions{
		GracePeriodSeconds: new(int64), // delete immediately
	}, metav1.ListOptions{})
	Expect(err).NotTo(HaveOccurred())
}
