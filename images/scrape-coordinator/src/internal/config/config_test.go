// SPDX-FileCopyrightText: Copyright 2026 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package config

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const minimalConfig = `
collectorNamespace: dash0-system
pools:
  perNode:
    selector:
      matchLabels:
        app.kubernetes.io/component: agent-collector
`

func TestParseAppliesDefaults(t *testing.T) {
	cfg, err := Parse([]byte(minimalConfig))
	require.NoError(t, err)

	assert.Equal(t, "dash0-system", cfg.CollectorNamespace)
	assert.Equal(t, DefaultCollectorNotReadyGracePeriod, cfg.CollectorNotReadyGracePeriod.Duration)
	assert.Equal(t, DefaultScrapeInterval, cfg.PrometheusCRs.ScrapeInterval.Duration)
	assert.Equal(t, DefaultListenAddr, cfg.Server.ListenAddr)
	assert.Nil(t, cfg.Server.TLS)
	assert.False(t, cfg.Server.AllowInsecureAuthSecrets)
	assert.Equal(t, []string{"dash0-system"}, cfg.SecretNamespacesOrDefault())
}

func TestParseFullConfig(t *testing.T) {
	cfg, err := Parse([]byte(`
collectorNamespace: dash0-system
pools:
  perNode:
    selector:
      matchLabels:
        app.kubernetes.io/component: agent-collector
collectorNotReadyGracePeriod: 45s
prometheusCRs:
  namespaces: [ns-a, ns-b]
  secretNamespaces: [ns-a]
  scrapeInterval: 1m
server:
  listenAddr: ":9090"
  tls:
    caFile: /etc/certs/ca.crt
    certFile: /etc/certs/tls.crt
    keyFile: /etc/certs/tls.key
  allowInsecureAuthSecrets: true
`))
	require.NoError(t, err)

	assert.Equal(t, 45*time.Second, cfg.CollectorNotReadyGracePeriod.Duration)
	assert.Equal(t, []string{"ns-a", "ns-b"}, cfg.PrometheusCRs.Namespaces)
	assert.Equal(t, []string{"ns-a"}, cfg.SecretNamespacesOrDefault())
	assert.Equal(t, time.Minute, cfg.PrometheusCRs.ScrapeInterval.Duration)
	assert.Equal(t, ":9090", cfg.Server.ListenAddr)
	require.NotNil(t, cfg.Server.TLS)
	assert.Equal(t, DefaultTLSListenAddr, cfg.Server.TLS.ListenAddr)
	assert.Equal(t, "/etc/certs/ca.crt", cfg.Server.TLS.CAFile)
	assert.Equal(t, "/etc/certs/tls.crt", cfg.Server.TLS.CertFile)
	assert.Equal(t, "/etc/certs/tls.key", cfg.Server.TLS.KeyFile)
	assert.True(t, cfg.Server.AllowInsecureAuthSecrets)
}

func TestParseRejectsInvalidConfigs(t *testing.T) {
	testCases := []struct {
		name          string
		content       string
		expectedError string
	}{
		{
			name:          "unknown field",
			content:       minimalConfig + "unknownField: true\n",
			expectedError: "unknown field",
		},
		{
			name: "missing collector namespace",
			content: `
pools:
  perNode:
    selector: {matchLabels: {app: collector}}
`,
			expectedError: "collectorNamespace must be set",
		},
		{
			name:          "no pool",
			content:       "collectorNamespace: dash0-system\n",
			expectedError: "at least one collector pool",
		},
		{
			name: "pool without selector",
			content: `
collectorNamespace: dash0-system
pools:
  perNode: {}
`,
			expectedError: "pools.perNode.selector must be set",
		},
		{
			name: "empty selector",
			content: `
collectorNamespace: dash0-system
pools:
  perNode:
    selector: {}
`,
			expectedError: "pools.perNode.selector must not be empty",
		},
		{
			name: "invalid selector",
			content: `
collectorNamespace: dash0-system
pools:
  perNode:
    selector:
      matchExpressions:
        - {key: app, operator: Invalid}
`,
			expectedError: "pools.perNode.selector is invalid",
		},
		{
			name:          "negative grace period",
			content:       minimalConfig + "collectorNotReadyGracePeriod: -1s\n",
			expectedError: "collectorNotReadyGracePeriod must be greater than zero",
		},
		{
			name:          "negative scrape interval",
			content:       minimalConfig + "prometheusCRs:\n  scrapeInterval: -1s\n",
			expectedError: "prometheusCRs.scrapeInterval must be greater than zero",
		},
		{
			name:          "tls without files",
			content:       minimalConfig + "server:\n  tls:\n    caFile: /ca.crt\n",
			expectedError: "server.tls requires caFile, certFile and keyFile",
		},
		{
			name: "tls on the plain listen address",
			content: minimalConfig + `server:
  listenAddr: ":8443"
  tls: {caFile: /ca.crt, certFile: /tls.crt, keyFile: /tls.key}
`,
			expectedError: "server.tls.listenAddr must differ from server.listenAddr",
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			_, err := Parse([]byte(testCase.content))
			require.Error(t, err)
			assert.Contains(t, err.Error(), testCase.expectedError)
		})
	}
}

func TestLoad(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	require.NoError(t, os.WriteFile(path, []byte(minimalConfig), 0o600))

	cfg, err := Load(path)
	require.NoError(t, err)
	assert.Equal(t, "dash0-system", cfg.CollectorNamespace)

	_, err = Load(filepath.Join(t.TempDir(), "does-not-exist.yaml"))
	require.Error(t, err)
}
