// SPDX-FileCopyrightText: Copyright 2026 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

// Package dash0telemetry provides a drop-in replacement for the collector's default internal telemetry factory
// (go.opentelemetry.io/collector/service/telemetry/otelconftelemetry). It behaves identically to that factory, except
// that it enriches the resource used for the collector's own self-monitoring telemetry with the k8s.node.uid attribute.
//
// The node UID is not available via the Kubernetes downward API, so it cannot be injected as an environment variable
// (unlike k8s.node.name, which comes from spec.nodeName). It also cannot be added by the k8sattributes or
// resourcedetection processors, because the collector's self-monitoring telemetry is emitted directly by the
// OpenTelemetry SDK and never passes through the collector's processing pipelines. This factory therefore resolves the
// node UID at collector startup by looking up the node (identified by the K8S_NODE_NAME environment variable) via the
// Kubernetes API and adds it to the telemetry resource.
//
// This factory is wired into the collector build via the "telemetry" section of the OpenTelemetry Collector Builder
// config (images/collector/src/builder/config.yaml).
package dash0telemetry

import (
	"go.opentelemetry.io/collector/service/telemetry"
	"go.opentelemetry.io/collector/service/telemetry/otelconftelemetry"
)

// NewFactory returns a telemetry.Factory that wraps the default otelconftelemetry factory and additionally attaches the
// k8s.node.uid resource attribute to the collector's self-monitoring telemetry. All other behavior is delegated
// unchanged to the wrapped factory.
func NewFactory() telemetry.Factory {
	base := otelconftelemetry.NewFactory()
	return telemetry.NewFactory(
		base.CreateDefaultConfig,
		telemetry.WithCreateResource(createResourceWithNodeUID(base)),
		telemetry.WithCreateLogger(base.CreateLogger),
		telemetry.WithCreateMeterProvider(base.CreateMeterProvider),
		telemetry.WithCreateTracerProvider(base.CreateTracerProvider),
	)
}
