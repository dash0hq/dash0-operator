// SPDX-FileCopyrightText: Copyright 2024 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package e2e

import (
	"fmt"
	"os/exec"
	"strings"

	"k8s.io/utils/ptr"

	. "github.com/onsi/gomega"
)

var (
	activeKubeCtxIsKindCluster *bool
	kindClusterName            string
	kindClusterIngressIp       string
)

func isKindCluster() bool {
	if activeKubeCtxIsKindCluster != nil {
		return *activeKubeCtxIsKindCluster
	}

	activeKubeCtxIsKindCluster = ptr.To(false)

	// check if kind is installed
	err := runAndIgnoreOutput(
		exec.Command("command",
			"-v",
			"kind",
		))
	if err != nil {
		return *activeKubeCtxIsKindCluster
	}

	// kind is installed, check if the name of the current kubernetes context matches an existing kind cluster
	kubernetesContext, err := run(exec.Command("kubectl", "config", "current-context"))
	Expect(err).NotTo(HaveOccurred())
	kubernetesContext = strings.TrimSpace(kubernetesContext)
	kindClusters, err := run(exec.Command("kind", "get", "clusters"))
	Expect(err).NotTo(HaveOccurred())
	for _, clusterName := range getNonEmptyLines(kindClusters) {
		if kubernetesContext == fmt.Sprintf("kind-%s", clusterName) {
			kindClusterName = clusterName
			activeKubeCtxIsKindCluster = ptr.To(true)
			break
		}
	}

	return *activeKubeCtxIsKindCluster
}
