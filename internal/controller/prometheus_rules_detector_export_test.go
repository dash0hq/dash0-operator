// SPDX-FileCopyrightText: Copyright 2026 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package controller

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/go-logr/logr"
	prometheusv1 "github.com/prometheus-operator/prometheus-operator/pkg/apis/monitoring/v1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"sigs.k8s.io/yaml"

	"github.com/dash0hq/dash0-operator/internal/util/logd"
)

func TestDetectorExportConversion(t *testing.T) {
	paths, err := filepath.Glob("testdata/detector-export/*.yaml")
	require.NoError(t, err)
	require.Len(t, paths, 7)
	for _, path := range paths {
		t.Run(filepath.Base(path), func(t *testing.T) {
			data, err := os.ReadFile(path)
			require.NoError(t, err)
			var manifest prometheusv1.PrometheusRule
			require.NoError(t, yaml.Unmarshal(data, &manifest))
			group := manifest.Spec.Groups[0]
			input := group.Rules[0]
			rule, issues, ok := convertAlertingRuleToCheckRule(input, upsertAction, group.Name, group.Interval, manifest.Annotations, logd.NewLogger(logr.Discard()))
			require.True(t, ok, issues)
			assert.Empty(t, issues)
			assert.Equal(t, input.Expr.StrVal, rule.Expression)
			assert.Equal(t, input.Annotations, rule.Annotations)
			assert.Equal(t, input.Labels, rule.Labels)
			assert.Equal(t, "2m", rule.For)
			assert.Equal(t, "3m", rule.KeepFiringFor)
			assert.Equal(t, "1m", rule.Interval)
			assert.NotContains(t, rule.Annotations, "dash0.com/volume-floor")
		})
	}
}
