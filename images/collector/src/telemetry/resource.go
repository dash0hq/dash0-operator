// SPDX-FileCopyrightText: Copyright 2026 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package dash0telemetry

import (
	"context"
	"os"
	"time"

	"go.opentelemetry.io/collector/component"
	"go.opentelemetry.io/collector/pdata/pcommon"
	"go.opentelemetry.io/collector/service/telemetry"

	"github.com/dash0hq/dash0-operator/images/pkg/nodeuid"
)

const (
	// nodeNameEnvVarName is the environment variable holding the name of the node the collector pod runs on. The
	// operator injects it via the downward API (spec.nodeName).
	nodeNameEnvVarName = "K8S_NODE_NAME"

	k8sNodeUIDResourceAttrKey = "k8s.node.uid"
)

// nodeUIDStartupWaitTimeout bounds how long collector startup will block waiting for the node UID lookup to finish.
// The lookup is started early (see startNodeUIDPrefetch), so it usually overlaps with other startup work and is already
// finished by the time the resource is created; this deadline only applies if it has not finished yet. It is a variable
// so that tests can shorten it.
var nodeUIDStartupWaitTimeout = 5 * time.Second

// startNodeUIDPrefetch begins resolving the node UID in the background, so that the result is already available when
// the self-monitoring resource is created. It is a no-op when the node name is not known (e.g. the collector does not
// run inside Kubernetes), in which case there is no attribute to add.
func startNodeUIDPrefetch(ctx context.Context) {
	nodeuid.Prefetch(ctx, os.Getenv(nodeNameEnvVarName))
}

// createResourceWithNodeUID returns a CreateResourceFunc that delegates to the wrapped factory's CreateResource and
// then attaches the k8s.node.uid attribute to the resulting resource.
func createResourceWithNodeUID(base telemetry.Factory) telemetry.CreateResourceFunc {
	return func(ctx context.Context, set telemetry.Settings, cfg component.Config) (pcommon.Resource, error) {
		res, err := base.CreateResource(ctx, set, cfg)
		if err != nil {
			return res, err
		}
		addNodeUID(res)
		return res, nil
	}
}

// addNodeUID waits (briefly) for the prefetched node UID and adds it to the given resource. It is best-effort: if the
// lookup was not started, has not finished within nodeUIDStartupWaitTimeout, or failed, the resource is left unchanged
// rather than failing or noticeably delaying collector startup, because self-monitoring telemetry is auxiliary.
func addNodeUID(res pcommon.Resource) {
	if _, ok := res.Attributes().Get(k8sNodeUIDResourceAttrKey); ok {
		// The attribute is already set (e.g. explicitly configured); do not override it.
		return
	}

	nodeUID := nodeuid.GetNodeUid(nodeUIDStartupWaitTimeout)
	if nodeUID == "" {
		return
	}
	res.Attributes().PutStr(k8sNodeUIDResourceAttrKey, nodeUID)
}
