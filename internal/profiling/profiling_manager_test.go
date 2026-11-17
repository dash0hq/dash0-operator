// SPDX-FileCopyrightText: Copyright 2026 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package profiling

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/dash0hq/dash0-operator/internal/util"
	"github.com/dash0hq/dash0-operator/internal/util/logd"
)

func TestUpdateExtraConfigRequestsOneReconciliationPerChange(t *testing.T) {
	m := NewManager(nil, util.ExtraConfigDefaults, util.ExtraConfigDefaults)
	ctx := context.Background()

	m.UpdateExtraConfig(ctx, util.ExtraConfigDefaults, logd.Discard())
	require.Empty(t, m.extraConfigUpdates, "an unchanged extra config must not request a reconciliation")

	changed := util.ExtraConfigDefaults
	changed.EbpfProfilerPodLabels = map[string]string{"source": "helm"}
	m.UpdateExtraConfig(ctx, changed, logd.Discard())
	require.Len(t, m.extraConfigUpdates, 1)

	changedAgain := util.ExtraConfigDefaults
	changedAgain.DebuginfoProxyPodLabels = map[string]string{"source": "helm"}
	m.UpdateExtraConfig(ctx, changedAgain, logd.Discard())
	require.Len(t, m.extraConfigUpdates, 1, "pending requests must be coalesced")
	require.Equal(t, changedAgain, *m.extraConfig.Load())
}
