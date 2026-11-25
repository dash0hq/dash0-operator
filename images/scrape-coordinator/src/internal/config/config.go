// SPDX-FileCopyrightText: Copyright 2026 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

// Package config loads, defaults and validates the configuration file of the scrape coordinator.
package config

import (
	"errors"
	"fmt"
	"os"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/yaml"
)

const (
	DefaultConfigFilePath               = "/etc/scrape-coordinator/config.yaml"
	DefaultListenAddr                   = ":8080"
	DefaultTLSListenAddr                = ":8443"
	DefaultCollectorNotReadyGracePeriod = 30 * time.Second
	DefaultScrapeInterval               = 30 * time.Second
)

// Config is the content of the scrape coordinator's configuration file.
type Config struct {
	// CollectorNamespace is the namespace of the collector pods, and the default namespace for watching secrets.
	CollectorNamespace string `json:"collectorNamespace"`

	// Pools defines which collector pods targets are assigned to.
	Pools Pools `json:"pools"`

	// CollectorNotReadyGracePeriod is the time after which a collector pod that is not running or not ready stops
	// receiving targets.
	CollectorNotReadyGracePeriod metav1.Duration `json:"collectorNotReadyGracePeriod,omitzero"`

	PrometheusCRs PrometheusCRs `json:"prometheusCRs"`

	Server Server `json:"server"`
}

// Pools defines the collector pools. At least one pool must be configured.
type Pools struct {
	// PerNode assigns each target with a node name to the collector pod of the pool that runs on the same node.
	PerNode *Pool `json:"perNode,omitempty"`
}

// Pool selects the collector pods that belong to it.
type Pool struct {
	Selector *metav1.LabelSelector `json:"selector"`
}

// PrometheusCRs configures how Prometheus custom resources (ServiceMonitor, PodMonitor, ScrapeConfig, Probe) are
// discovered.
type PrometheusCRs struct {
	// Namespaces restricts the namespaces in which Prometheus custom resources are watched. Empty means all namespaces.
	Namespaces []string `json:"namespaces,omitempty"`

	// SecretNamespaces are the namespaces in which secrets are watched for changes. Empty means the collector
	// namespace.
	SecretNamespaces []string `json:"secretNamespaces,omitempty"`

	// ScrapeInterval is the default scrape interval for monitors that do not set one.
	ScrapeInterval metav1.Duration `json:"scrapeInterval,omitzero"`
}

// Server configures the HTTP endpoints the collectors query.
type Server struct {
	ListenAddr string `json:"listenAddr,omitempty"`

	// TLS enables an additional listener with mutual TLS. When it is set, the plain listener only serves the health
	// endpoints.
	TLS *TLS `json:"tls,omitempty"`

	// AllowInsecureAuthSecrets serves the scrape configs with unmasked credentials on the plain listener.
	AllowInsecureAuthSecrets bool `json:"allowInsecureAuthSecrets,omitempty"`
}

// TLS configures the mutual TLS listener.
type TLS struct {
	ListenAddr string `json:"listenAddr,omitempty"`
	CAFile     string `json:"caFile"`
	CertFile   string `json:"certFile"`
	KeyFile    string `json:"keyFile"`
}

// Load reads the configuration file at path, applies the defaults and validates the result.
func Load(path string) (*Config, error) {
	content, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("cannot read the configuration file %s: %w", path, err)
	}
	return Parse(content)
}

// Parse parses the configuration file content, applies the defaults and validates the result.
func Parse(content []byte) (*Config, error) {
	cfg := &Config{}
	if err := yaml.UnmarshalStrict(content, cfg); err != nil {
		return nil, fmt.Errorf("cannot parse the configuration file: %w", err)
	}
	cfg.applyDefaults()
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	return cfg, nil
}

func (c *Config) applyDefaults() {
	if c.CollectorNotReadyGracePeriod.Duration == 0 {
		c.CollectorNotReadyGracePeriod.Duration = DefaultCollectorNotReadyGracePeriod
	}
	if c.PrometheusCRs.ScrapeInterval.Duration == 0 {
		c.PrometheusCRs.ScrapeInterval.Duration = DefaultScrapeInterval
	}
	if c.Server.ListenAddr == "" {
		c.Server.ListenAddr = DefaultListenAddr
	}
	if c.Server.TLS != nil && c.Server.TLS.ListenAddr == "" {
		c.Server.TLS.ListenAddr = DefaultTLSListenAddr
	}
}

// Validate checks the configuration for consistency.
func (c *Config) Validate() error {
	if c.CollectorNamespace == "" {
		return errors.New("collectorNamespace must be set")
	}
	if c.Pools.PerNode == nil {
		return errors.New("at least one collector pool must be configured in pools")
	}
	if err := validateSelector("pools.perNode.selector", c.Pools.PerNode.Selector); err != nil {
		return err
	}
	if c.CollectorNotReadyGracePeriod.Duration < 0 {
		return errors.New("collectorNotReadyGracePeriod must be greater than zero")
	}
	if c.PrometheusCRs.ScrapeInterval.Duration < 0 {
		return errors.New("prometheusCRs.scrapeInterval must be greater than zero")
	}
	if c.Server.TLS != nil {
		if c.Server.TLS.CAFile == "" || c.Server.TLS.CertFile == "" || c.Server.TLS.KeyFile == "" {
			return errors.New("server.tls requires caFile, certFile and keyFile")
		}
		if c.Server.TLS.ListenAddr == c.Server.ListenAddr {
			return errors.New("server.tls.listenAddr must differ from server.listenAddr")
		}
	}
	return nil
}

func validateSelector(path string, selector *metav1.LabelSelector) error {
	if selector == nil {
		return fmt.Errorf("%s must be set", path)
	}
	parsed, err := metav1.LabelSelectorAsSelector(selector)
	if err != nil {
		return fmt.Errorf("%s is invalid: %w", path, err)
	}
	if parsed.Empty() {
		return fmt.Errorf("%s must not be empty", path)
	}
	return nil
}

// SecretNamespacesOrDefault returns the namespaces in which secrets are watched for changes.
func (c *Config) SecretNamespacesOrDefault() []string {
	if len(c.PrometheusCRs.SecretNamespaces) > 0 {
		return c.PrometheusCRs.SecretNamespaces
	}
	return []string{c.CollectorNamespace}
}
