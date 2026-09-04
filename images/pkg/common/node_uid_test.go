// SPDX-FileCopyrightText: Copyright 2026 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package common

import (
	"context"
	"testing"
)

func TestAssembleResourceOmitsTheNodeUidWhenItCannotBeResolved(t *testing.T) {
	t.Setenv("K8S_NODE_NAME", "node-1")
	t.Setenv("KUBERNETES_SERVICE_HOST", "")
	t.Setenv("KUBERNETES_SERVICE_PORT", "")

	resourceAttributes := assembleResource(context.Background(), "service-name", "", "", "", "", "", "")

	if got := attributeValue(resourceAttributes.Attributes(), "k8s.node.name"); got != "node-1" {
		t.Errorf("resource attribute \"k8s.node.name\" = %q, want %q", got, "node-1")
	}
	if got := attributeValue(resourceAttributes.Attributes(), "k8s.node.uid"); got != "" {
		t.Errorf("resource attribute \"k8s.node.uid\" = %q, want it to be absent", got)
	}
}
