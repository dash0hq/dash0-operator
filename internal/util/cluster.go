// SPDX-FileCopyrightText: Copyright 2025 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package util

import (
	"context"
	"fmt"

	"github.com/go-logr/logr"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

func ReadPseudoClusterUid(ctx context.Context, k8sClient client.Client, logger *logr.Logger) types.UID {
	if uid, err := ReadPseudoClusterUidOrFail(ctx, k8sClient, logger); err != nil {
		return "unknown"
	} else {
		return uid
	}
}

func ReadPseudoClusterUidOrFail(ctx context.Context, k8sClient client.Client, logger *logr.Logger) (types.UID, error) {
	kubeSystemNamespace := &corev1.Namespace{}
	if err := k8sClient.Get(ctx, client.ObjectKey{Name: "kube-system"}, kubeSystemNamespace); err != nil {
		msg := "unable to get the kube-system namespace uid"
		logger.Error(err, msg)
		return "", fmt.Errorf("%s: %w", msg, err)
	}
	return kubeSystemNamespace.UID, nil
}
