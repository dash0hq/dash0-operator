// SPDX-FileCopyrightText: Copyright 2026 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package dash0telemetry

import (
	"context"
	"fmt"
	"os"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"

	"go.opentelemetry.io/collector/component"
	"go.opentelemetry.io/collector/pdata/pcommon"
	"go.opentelemetry.io/collector/service/telemetry"
)

const (
	// nodeNameEnvVarName is the environment variable holding the name of the node the collector pod runs on. The
	// operator injects it via the downward API (spec.nodeName) for both the DaemonSet and Deployment collectors.
	nodeNameEnvVarName = "K8S_NODE_NAME"

	k8sNodeUIDResourceAttrKey = "k8s.node.uid"

	// nodeUIDLookupTimeout bounds the Kubernetes API lookup so that a slow or unreachable API server cannot block
	// collector startup indefinitely.
	nodeUIDLookupTimeout = 10 * time.Second
)

// nodeUIDResolver resolves the UID of the node with the given name. It is a package-level variable so that tests can
// stub out the Kubernetes API interaction.
var nodeUIDResolver = resolveNodeUIDViaKubernetesAPI

// createResourceWithNodeUID returns a CreateResourceFunc that delegates to the wrapped factory's CreateResource and
// then attaches the k8s.node.uid attribute to the resulting resource.
func createResourceWithNodeUID(base telemetry.Factory) telemetry.CreateResourceFunc {
	return func(ctx context.Context, set telemetry.Settings, cfg component.Config) (pcommon.Resource, error) {
		res, err := base.CreateResource(ctx, set, cfg)
		if err != nil {
			return res, err
		}
		addNodeUID(ctx, res)
		return res, nil
	}
}

// addNodeUID resolves the node UID and adds it to the given resource. It is best-effort: if the node name is unknown or
// the lookup fails, the resource is left unchanged rather than failing collector startup, because self-monitoring
// telemetry is auxiliary.
func addNodeUID(ctx context.Context, res pcommon.Resource) {
	nodeName := os.Getenv(nodeNameEnvVarName)
	if nodeName == "" {
		// The node name is not known (e.g. the collector is not running inside Kubernetes); nothing to add.
		return
	}
	if _, ok := res.Attributes().Get(k8sNodeUIDResourceAttrKey); ok {
		// The attribute is already set (e.g. explicitly configured); do not override it.
		return
	}

	lookupCtx, cancel := context.WithTimeout(ctx, nodeUIDLookupTimeout)
	defer cancel()

	nodeUID, err := nodeUIDResolver(lookupCtx, nodeName)
	if err != nil {
		// The collector's zap logger is not available this early in startup, so log to stderr and continue.
		fmt.Fprintf(os.Stderr,
			"dash0telemetry: could not resolve %s for node %q, self-monitoring telemetry will not have this "+
				"attribute set: %v\n",
			k8sNodeUIDResourceAttrKey, nodeName, err)
		return
	}
	if nodeUID == "" {
		return
	}
	res.Attributes().PutStr(k8sNodeUIDResourceAttrKey, nodeUID)
}

// resolveNodeUIDViaKubernetesAPI fetches the node with the given name via the in-cluster Kubernetes API and returns its
// UID. The collector's service account already has get/list/watch permissions on nodes.
func resolveNodeUIDViaKubernetesAPI(ctx context.Context, nodeName string) (string, error) {
	restConfig, err := rest.InClusterConfig()
	if err != nil {
		return "", fmt.Errorf("cannot create in-cluster Kubernetes client config: %w", err)
	}
	clientset, err := kubernetes.NewForConfig(restConfig)
	if err != nil {
		return "", fmt.Errorf("cannot create Kubernetes client: %w", err)
	}
	node, err := clientset.CoreV1().Nodes().Get(ctx, nodeName, metav1.GetOptions{})
	if err != nil {
		return "", fmt.Errorf("cannot fetch node %q from the Kubernetes API: %w", nodeName, err)
	}
	return string(node.UID), nil
}
