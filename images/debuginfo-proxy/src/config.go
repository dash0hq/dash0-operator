// SPDX-FileCopyrightText: Copyright 2026 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"context"
	"errors"
	"fmt"
	"net"
	"strings"

	"go.opentelemetry.io/collector/config/configtls"
	"go.opentelemetry.io/collector/confmap"
	"go.opentelemetry.io/collector/confmap/provider/envprovider"
	"go.opentelemetry.io/collector/confmap/provider/fileprovider"
)

type config struct {
	Receivers map[string]receiverConfig `mapstructure:"receivers"`
	Exporters map[string]exporterConfig `mapstructure:"exporters"`
	Service   struct {
		Pipelines map[string]pipelineConfig `mapstructure:"pipelines"`
	} `mapstructure:"service"`
}

type receiverConfig struct {
	GRPC struct {
		Endpoint string                  `mapstructure:"endpoint"`
		TLS      *configtls.ServerConfig `mapstructure:"tls"`
	} `mapstructure:"grpc"`
}

type exporterConfig struct {
	Endpoint string                 `mapstructure:"endpoint"`
	TLS      configtls.ClientConfig `mapstructure:"tls"`
	Headers  map[string]string      `mapstructure:"headers"`
}

type pipelineConfig struct {
	Receivers []string `mapstructure:"receivers"`
	Exporters []string `mapstructure:"exporters"`
}

func loadConfig(ctx context.Context, path string) (*config, error) {
	resolver, err := confmap.NewResolver(confmap.ResolverSettings{
		URIs: []string{path}, DefaultScheme: "env",
		ProviderFactories: []confmap.ProviderFactory{fileprovider.NewFactory(), envprovider.NewFactory()},
	})
	if err != nil {
		return nil, err
	}
	defer func() { _ = resolver.Shutdown(ctx) }()
	resolved, err := resolver.Resolve(ctx)
	if err != nil {
		return nil, errors.New("cannot resolve configuration")
	}
	c := &config{}
	if err := resolved.Unmarshal(c); err != nil {
		return nil, errors.New("invalid configuration structure")
	}
	return c, c.validate()
}

func (c *config) validate() error {
	if len(c.Receivers) != 1 || len(c.Service.Pipelines) != 1 {
		return errors.New("configure one debuginfo receiver and one debuginfo pipeline")
	}
	r, ok := c.Receivers["debuginfo"]
	if !ok || !validEndpoint(r.GRPC.Endpoint) {
		return errors.New("debuginfo receiver requires a host:port endpoint")
	}
	if err := r.validate(); err != nil {
		return err
	}
	p, ok := c.Service.Pipelines["debuginfo"]
	if !ok || len(p.Receivers) != 1 || p.Receivers[0] != "debuginfo" || len(p.Exporters) == 0 ||
		len(p.Exporters) > 64 {
		return errors.New("debuginfo pipeline must reference receiver debuginfo and 1 to 64 exporters")
	}
	seen := map[string]bool{}
	for _, id := range p.Exporters {
		e, found := c.Exporters[id]
		if !found || !strings.HasPrefix(id, "debuginfo/") || len(id) == len("debuginfo/") ||
			seen[id] || !validEndpoint(e.Endpoint) {
			return fmt.Errorf("invalid or duplicate debuginfo exporter %q", id)
		}
		seen[id] = true
		if err := e.validate(); err != nil {
			return fmt.Errorf("exporter %s: %w", id, err)
		}
	}
	if len(seen) != len(c.Exporters) {
		return errors.New("all exporters must be referenced by the debuginfo pipeline")
	}
	return nil
}

func (r receiverConfig) validate() error {
	if r.GRPC.TLS != nil {
		if err := confmap.Validate(r.GRPC.TLS); err != nil {
			return errors.New("invalid receiver TLS configuration")
		}
	}
	return nil
}

func (e exporterConfig) validate() error {
	if e.TLS.InsecureSkipVerify {
		return errors.New("TLS certificate verification must not be disabled")
	}
	if err := confmap.Validate(&e.TLS); err != nil {
		return errors.New("invalid TLS configuration")
	}
	for key, value := range e.Headers {
		if !validHeader(key, value) {
			return errors.New("headers require lowercase gRPC metadata keys and printable ASCII values")
		}
	}
	return nil
}

func validEndpoint(endpoint string) bool {
	host, port, err := net.SplitHostPort(endpoint)
	return err == nil && host != "" && port != ""
}

func validHeader(key, value string) bool {
	if key == "" || strings.HasPrefix(key, "grpc-") || strings.HasSuffix(key, "-bin") {
		return false
	}
	for _, c := range key {
		if (c < 'a' || c > 'z') && (c < '0' || c > '9') && c != '-' && c != '_' && c != '.' {
			return false
		}
	}
	for _, c := range value {
		if c < 32 || c > 126 {
			return false
		}
	}
	return true
}
