// SPDX-FileCopyrightText: Copyright 2024 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package common // import "github.com/dash0hq/dash0-operator/images/pkg/common"

import (
	"context"
	"log"
	"os"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetricgrpc"
	"go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetrichttp"
	otelmetric "go.opentelemetry.io/otel/metric"
	metricnoop "go.opentelemetry.io/otel/metric/noop"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/resource"
	semconv "go.opentelemetry.io/otel/semconv/v1.26.0"
)

var (
	meterProvider otelmetric.MeterProvider
)

func InitOTelSdk(
	ctx context.Context,
	meterName string,
) (otelmetric.Meter, []func(ctx context.Context) error) {
	podUid, isSet := os.LookupEnv("K8S_POD_UID")
	if !isSet {
		log.Println("Env var 'K8S_POD_UID' is not set")
	}

	nodeName, isSet := os.LookupEnv("K8S_NODE_NAME")
	if !isSet {
		log.Println("Env var 'K8S_NODE_NAME' is not set")
	}

	var doMeterShutdown func(ctx context.Context) error

	if _, isSet = os.LookupEnv("OTEL_EXPORTER_OTLP_ENDPOINT"); isSet {
		var metricExporter sdkmetric.Exporter

		protocol, isProtocolSet := os.LookupEnv("OTEL_EXPORTER_OTLP_PROTOCOL")
		if !isProtocolSet {
			// http/protobuf is the default transport protocol, see spec:
			// https://github.com/open-telemetry/opentelemetry-specification/blob/main/specification/protocol/exporter.md
			protocol = "http/protobuf"
		}

		var err error
		switch protocol {
		case "grpc":
			if metricExporter, err = otlpmetricgrpc.New(ctx); err != nil {
				log.Fatalf("Cannot create the OTLP gRPC metrics exporter: %v", err)
			}
		case "http/protobuf":
			if metricExporter, err = otlpmetrichttp.New(ctx); err != nil {
				log.Fatalf("Cannot create the OTLP HTTP metrics exporter: %v", err)
			}
		case "http/json":
			log.Fatalf("Cannot create the OTLP HTTP exporter: the protocol 'http/json' is currently unsupported")
		default:
			log.Fatalf("Unexpected OTLP protocol set as value of the 'OTEL_EXPORTER_OTLP_PROTOCOL' environment variable: %v", protocol)
		}

		resourceWithPodAndNode, err := resource.New(ctx,
			resource.WithAttributes(
				semconv.K8SPodUID(podUid),
				semconv.K8SNodeName(nodeName),
			),
		)
		if err != nil {
			log.Fatalf("Cannot initialize the OpenTelemetry resource: %v", err)
		}
		// Note: Merging with resource.Environment() should not be necessary, the SDK is supposed to do that on its own,
		// that is, merge the resource attributes provided here with the key-value pairs from OTEL_RESOURCE_ATTRIBUTES.
		//
		// Actually, the trace SDK does that correctly, but the metric SDK does not.
		// - https://github.com/open-telemetry/opentelemetry-go/blob/932a4d8a5f2536645618d7aee8e5da6b8e3b6751/sdk/trace/provider.go#L353
		// - https://github.com/open-telemetry/opentelemetry-go/blob/932a4d8a5f2536645618d7aee8e5da6b8e3b6751/sdk/metric/config.go#L106
		//
		// Reported here: https://github.com/open-telemetry/opentelemetry-go/issues/5764
		finalResource, err := resource.Merge(resource.Environment(), resourceWithPodAndNode)
		if err != nil {
			log.Fatalf("Cannot merge the OpenTelemetry resource: %v", err)
		}

		sdkMeterProvider := sdkmetric.NewMeterProvider(
			sdkmetric.WithResource(finalResource),
			sdkmetric.WithReader(
				sdkmetric.NewPeriodicReader(
					metricExporter,
					sdkmetric.WithTimeout(10*time.Second),
					sdkmetric.WithInterval(15*time.Second),
				)),
		)

		meterProvider = sdkMeterProvider
		doMeterShutdown = sdkMeterProvider.Shutdown
	} else {
		meterProvider = metricnoop.MeterProvider{}
		doMeterShutdown = func(ctx context.Context) error { return nil }
	}

	otel.SetMeterProvider(meterProvider)

	return meterProvider.Meter(meterName), []func(ctx context.Context) error{
		doMeterShutdown,
	}
}

func ShutDownOTelSdk(ctx context.Context, shutdownFunctions []func(ctx context.Context) error) {
	var err error
	for _, shutdownFunction := range shutdownFunctions {
		timeoutCtx, cancelFun := context.WithTimeout(ctx, time.Second)
		if err = shutdownFunction(timeoutCtx); err != nil {
			log.Printf("Failed to shutdown self monitoring, telemetry may have been lost:%v\n", err)
		}
		cancelFun()
	}
}