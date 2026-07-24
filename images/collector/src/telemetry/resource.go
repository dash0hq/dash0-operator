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

	// nodeUIDLookupTimeout bounds the background Kubernetes API lookup so that a slow or unreachable API server cannot
	// keep the lookup goroutine (and its client connection) alive indefinitely.
	nodeUIDLookupTimeout = 5 * time.Second
)

// nodeUIDStartupWaitTimeout bounds how long collector startup will block waiting for the node UID lookup to finish.
// The lookup is started early (see startNodeUIDPrefetch), so it usually overlaps with other startup work and is already
// finished by the time the resource is created; this deadline only applies if it has not finished yet. It is a variable
// so that tests can shorten it.
var nodeUIDStartupWaitTimeout = 1 * time.Second

// nodeUIDResolver resolves the UID of the node with the given name. It is a package-level variable so that tests can
// stub out the Kubernetes API interaction.
var nodeUIDResolver = resolveNodeUIDViaKubernetesAPI

// nodeUIDResult carries the outcome of the asynchronous node UID lookup.
type nodeUIDResult struct {
	uid string
	err error
}

// startNodeUIDPrefetch begins resolving the node UID in the background and returns a channel that will receive the
// result exactly once. It returns nil if the node name is not known (e.g. the collector is not running inside
// Kubernetes), in which case there is nothing to wait for and no attribute to add.
func startNodeUIDPrefetch(ctx context.Context) <-chan nodeUIDResult {
	nodeName := os.Getenv(nodeNameEnvVarName)
	if nodeName == "" {
		return nil
	}
	// Buffered so the goroutine never blocks on send, even if startup gives up waiting before the lookup finishes.
	resultChan := make(chan nodeUIDResult, 1)
	go func() {
		lookupCtx, cancel := context.WithTimeout(ctx, nodeUIDLookupTimeout)
		defer cancel()
		uid, err := nodeUIDResolver(lookupCtx, nodeName)
		resultChan <- nodeUIDResult{uid: uid, err: err}
	}()
	return resultChan
}

// createResourceWithNodeUID returns a CreateResourceFunc that delegates to the wrapped factory's CreateResource and
// then attaches the k8s.node.uid attribute (resolved via the given prefetch channel) to the resulting resource.
func createResourceWithNodeUID(
	base telemetry.Factory,
	nodeUIDFuture <-chan nodeUIDResult,
) telemetry.CreateResourceFunc {
	return func(ctx context.Context, set telemetry.Settings, cfg component.Config) (pcommon.Resource, error) {
		res, err := base.CreateResource(ctx, set, cfg)
		if err != nil {
			return res, err
		}
		addNodeUID(ctx, res, nodeUIDFuture)
		return res, nil
	}
}

// addNodeUID waits (briefly) for the prefetched node UID and adds it to the given resource. It is best-effort: if the
// lookup was not started, has not finished within nodeUIDStartupWaitTimeout, or failed, the resource is left unchanged
// rather than failing or noticeably delaying collector startup, because self-monitoring telemetry is auxiliary.
func addNodeUID(ctx context.Context, res pcommon.Resource, nodeUIDFuture <-chan nodeUIDResult) {
	if nodeUIDFuture == nil {
		// The node name was not known when the lookup would have started; nothing to add.
		return
	}
	if _, ok := res.Attributes().Get(k8sNodeUIDResourceAttrKey); ok {
		// The attribute is already set (e.g. explicitly configured); do not override it.
		return
	}

	nodeUID, ok := awaitNodeUID(ctx, nodeUIDFuture)
	if !ok {
		return
	}
	res.Attributes().PutStr(k8sNodeUIDResourceAttrKey, nodeUID)
}

// awaitNodeUID waits for the prefetched node UID, bounded by nodeUIDStartupWaitTimeout and the given context. It
// returns the resolved UID and true only if a non-empty UID was resolved before the deadline; otherwise it returns
// false (and logs the reason, since the collector's zap logger is not yet available this early in startup).
func awaitNodeUID(ctx context.Context, nodeUIDFuture <-chan nodeUIDResult) (string, bool) {
	select {
	case result := <-nodeUIDFuture:
		if result.err != nil {
			fmt.Fprintf(os.Stderr,
				"dash0telemetry: could not resolve %s, self-monitoring telemetry will not have this attribute "+
					"set: %v\n",
				k8sNodeUIDResourceAttrKey, result.err)
			return "", false
		}
		if result.uid == "" {
			return "", false
		}
		return result.uid, true
	case <-time.After(nodeUIDStartupWaitTimeout):
		fmt.Fprintf(os.Stderr,
			"dash0telemetry: resolving %s did not finish within %s, self-monitoring telemetry will not have this "+
				"attribute set\n",
			k8sNodeUIDResourceAttrKey, nodeUIDStartupWaitTimeout)
		return "", false
	case <-ctx.Done():
		return "", false
	}
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
