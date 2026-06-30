// SPDX-FileCopyrightText: Copyright 2026 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package common

import (
	"context"
	"testing"

	"go.opentelemetry.io/otel/attribute"
	lognoop "go.opentelemetry.io/otel/log/noop"
	metricnoop "go.opentelemetry.io/otel/metric/noop"
)

func TestOTelSDKIsConfigured(t *testing.T) {
	t.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", "")
	if OTelSDKIsConfigured() {
		t.Errorf("OTelSDKIsConfigured() = true, want false when OTEL_EXPORTER_OTLP_ENDPOINT is unset")
	}

	t.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", "http://example.com:4318")
	if !OTelSDKIsConfigured() {
		t.Errorf("OTelSDKIsConfigured() = false, want true when OTEL_EXPORTER_OTLP_ENDPOINT is set")
	}
}

func TestEndpointHasScheme(t *testing.T) {
	tests := []struct {
		name     string
		endpoint string
		want     bool
	}{
		{
			name:     "empty string",
			endpoint: "",
			want:     false,
		},
		{
			name:     "host and port without scheme",
			endpoint: "example.com:4317",
			want:     false,
		},
		{
			name:     "protocol-relative URL",
			endpoint: "//example.com:4317",
			want:     false,
		},
		{
			name:     "http scheme",
			endpoint: "http://example.com:4318",
			want:     true,
		},
		{
			name:     "https scheme",
			endpoint: "https://example.com",
			want:     true,
		},
		{
			name:     "grpc scheme",
			endpoint: "grpc://example.com:4317",
			want:     true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := EndpointHasScheme(tt.endpoint); got != tt.want {
				t.Errorf("EndpointHasScheme(%q) = %v, want %v", tt.endpoint, got, tt.want)
			}
		})
	}
}

func TestAppendSignalSpecificPath(t *testing.T) {
	tests := []struct {
		name               string
		baseUrl            string
		signalSpecificPath string
		want               string
		wantErr            bool
	}{
		{
			name:               "base URL without trailing slash",
			baseUrl:            "https://example.com",
			signalSpecificPath: otlpLogsDefaultSignalPath,
			want:               "https://example.com/v1/logs",
		},
		{
			name:               "base URL with trailing slash",
			baseUrl:            "https://example.com/",
			signalSpecificPath: otlpMetricsDefaultSignalPath,
			want:               "https://example.com/v1/metrics",
		},
		{
			name:               "base URL with port",
			baseUrl:            "http://example.com:4318",
			signalSpecificPath: otlpLogsDefaultSignalPath,
			want:               "http://example.com:4318/v1/logs",
		},
		{
			name:               "base URL with existing path",
			baseUrl:            "https://example.com/otlp",
			signalSpecificPath: otlpLogsDefaultSignalPath,
			want:               "https://example.com/otlp/v1/logs",
		},
		{
			name:               "unparseable base URL",
			baseUrl:            ":example.com",
			signalSpecificPath: otlpLogsDefaultSignalPath,
			wantErr:            true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := appendSignalSpecificPath(tt.baseUrl, tt.signalSpecificPath)
			if (err != nil) != tt.wantErr {
				t.Fatalf("appendSignalSpecificPath(%q, %q) error = %v, wantErr %v",
					tt.baseUrl, tt.signalSpecificPath, err, tt.wantErr)
			}
			if got != tt.want {
				t.Errorf("appendSignalSpecificPath(%q, %q) = %q, want %q",
					tt.baseUrl, tt.signalSpecificPath, got, tt.want)
			}
		})
	}
}

func TestAssembleResourceWithExplicitValues(t *testing.T) {
	resourceAttributes := assembleResource(
		context.Background(),
		"service-name",
		"service-version",
		"pseudo-cluster-uid",
		"cluster-name",
		"deployment-uid",
		"deployment-name",
		"container-name",
	)

	expectedAttributes := map[string]string{
		"service.namespace":   OperatorManagerServiceNamespace,
		"service.name":        "service-name",
		"service.version":     "service-version",
		"k8s.cluster.uid":     "pseudo-cluster-uid",
		"k8s.cluster.name":    "cluster-name",
		"k8s.deployment.uid":  "deployment-uid",
		"k8s.deployment.name": "deployment-name",
		"k8s.container.name":  "container-name",
	}
	for key, want := range expectedAttributes {
		if got := attributeValue(resourceAttributes.Attributes(), key); got != want {
			t.Errorf("resource attribute %q = %q, want %q", key, got, want)
		}
	}
}

func TestAssembleResourceFallsBackToEnvVars(t *testing.T) {
	t.Setenv("SERVICE_VERSION", "version-from-env")
	t.Setenv("K8S_CLUSTER_UID", "cluster-uid-from-env")
	t.Setenv("K8S_CLUSTER_NAME", "cluster-name-from-env")
	t.Setenv("K8S_DEPLOYMENT_UID", "deployment-uid-from-env")
	t.Setenv("K8S_DEPLOYMENT_NAME", "deployment-name-from-env")
	t.Setenv("K8S_POD_NAME", "pod-name-from-env")

	resourceAttributes := assembleResource(
		context.Background(),
		"service-name",
		"",
		"",
		"",
		"",
		"",
		"",
	)

	expectedAttributes := map[string]string{
		"service.version":     "version-from-env",
		"k8s.cluster.uid":     "cluster-uid-from-env",
		"k8s.cluster.name":    "cluster-name-from-env",
		"k8s.deployment.uid":  "deployment-uid-from-env",
		"k8s.deployment.name": "deployment-name-from-env",
		"k8s.pod.name":        "pod-name-from-env",
	}
	for key, want := range expectedAttributes {
		if got := attributeValue(resourceAttributes.Attributes(), key); got != want {
			t.Errorf("resource attribute %q = %q, want %q", key, got, want)
		}
	}
	if got := attributeValue(resourceAttributes.Attributes(), "k8s.container.name"); got != "" {
		t.Errorf("resource attribute \"k8s.container.name\" = %q, want it to be absent", got)
	}
}

func TestAssembleResourceExplicitValuesTakePrecedenceOverEnvVars(t *testing.T) {
	t.Setenv("SERVICE_VERSION", "version-from-env")
	t.Setenv("K8S_CLUSTER_NAME", "cluster-name-from-env")

	resourceAttributes := assembleResource(
		context.Background(),
		"service-name",
		"explicit-version",
		"",
		"explicit-cluster-name",
		"",
		"",
		"",
	)

	if got := attributeValue(resourceAttributes.Attributes(), "service.version"); got != "explicit-version" {
		t.Errorf("resource attribute \"service.version\" = %q, want %q", got, "explicit-version")
	}
	if got := attributeValue(resourceAttributes.Attributes(), "k8s.cluster.name"); got != "explicit-cluster-name" {
		t.Errorf("resource attribute \"k8s.cluster.name\" = %q, want %q", got, "explicit-cluster-name")
	}
}

func TestInitOTelSdkWithConfigWithoutEndpointUsesNoopProviders(t *testing.T) {
	otelZapBridge, meter := InitOTelSdkWithConfig(
		context.Background(),
		"test-meter",
		&OTelSdkConfig{},
		nil,
	)

	if otelZapBridge == nil {
		t.Errorf("InitOTelSdkWithConfig() returned a nil otelzap core")
	}
	if meter == nil {
		t.Errorf("InitOTelSdkWithConfig() returned a nil meter")
	}
	if _, isNoop := meterProvider.(metricnoop.MeterProvider); !isNoop {
		t.Errorf("meterProvider = %T, want a no-op meter provider when no endpoint is configured", meterProvider)
	}
	if _, isNoop := loggerProvider.(lognoop.LoggerProvider); !isNoop {
		t.Errorf("loggerProvider = %T, want a no-op logger provider when no endpoint is configured", loggerProvider)
	}
	if len(shutdownFunctions) != 0 {
		t.Errorf("len(shutdownFunctions) = %d, want 0 when no endpoint is configured", len(shutdownFunctions))
	}

	// must not panic or block when there is nothing to shut down
	ShutDownOTelSdkThreadSafe(context.Background())
}

func attributeValue(attributes []attribute.KeyValue, key string) string {
	for _, keyValue := range attributes {
		if string(keyValue.Key) == key {
			return keyValue.Value.Emit()
		}
	}
	return ""
}
