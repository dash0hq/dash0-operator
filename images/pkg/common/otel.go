// SPDX-FileCopyrightText: Copyright 2024 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package common // import "github.com/dash0hq/dash0-operator/images/pkg/common"

import (
	"context"
	"log"
	"os"
	"regexp"
	"sync"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetricgrpc"
	"go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetrichttp"
	otelmetric "go.opentelemetry.io/otel/metric"
	metricnoop "go.opentelemetry.io/otel/metric/noop"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/resource"
	semconv "go.opentelemetry.io/otel/semconv/v1.26.0"
)

type OTelSdkConfig struct {
	Endpoint           string
	Protocol           string
	ResourceAttributes []attribute.KeyValue
	LogLevel           string
	Headers            map[string]string
}

const (
	ProtocolGrpc         = "grpc"
	ProtocolHttpProtobuf = "http/protobuf"
	ProtocolHttpJson     = "http/json"
)

var (
	meterProvider       otelmetric.MeterProvider
	shutdownFunctions   []func(ctx context.Context) error
	oTelSdkMutex        sync.Mutex
	endpointSchemeRegex = regexp.MustCompile(`^\w+://`)
)

func InitOTelSdkFromEnvVars(
	ctx context.Context,
	meterName string,
	extraResourceAttributes []attribute.KeyValue,
) otelmetric.Meter {
	// InitOTelSdkFromEnvVars is used in the configuration reloader and filelog offset sync container. The OTel SDK
	// will either be started once at process startup or not. In contrast to InitOTelSdkWithConfig, it will not be
	// restarted, and the configuration is not modified from different threads. Hence, no thread safety is needed here
	// and oTelSdkMutex is not used.
	podUid, nodeName, daemonSetUid, deploymentUid := getKubernetesResourceAttributes()
	if _, otelExporterEndpointIsSet := os.LookupEnv("OTEL_EXPORTER_OTLP_ENDPOINT"); otelExporterEndpointIsSet {
		var metricExporter sdkmetric.Exporter

		protocol, protocolIsSet := os.LookupEnv("OTEL_EXPORTER_OTLP_PROTOCOL")
		if !protocolIsSet {
			// http/protobuf is the default transport protocol, see spec:
			// https://github.com/open-telemetry/opentelemetry-specification/blob/main/specification/protocol/exporter.md
			protocol = ProtocolHttpProtobuf
		}

		var err error
		switch protocol {
		case ProtocolGrpc:
			if metricExporter, err = otlpmetricgrpc.New(ctx); err != nil {
				log.Fatalf("Cannot create the OTLP gRPC metrics exporter: %v", err)
			}
		case ProtocolHttpProtobuf:
			if metricExporter, err = otlpmetrichttp.New(ctx); err != nil {
				log.Fatalf("Cannot create the OTLP HTTP metrics exporter: %v", err)
			}
		case ProtocolHttpJson:
			log.Fatalf("Cannot create the OTLP HTTP exporter: the protocol 'http/json' is currently unsupported")
		default:
			log.Fatalf("Unexpected OTLP protocol set as value of the 'OTEL_EXPORTER_OTLP_PROTOCOL' environment variable: %v", protocol)
		}

		resourceAttributes := assembleResource(ctx, podUid, nodeName, daemonSetUid, deploymentUid, extraResourceAttributes)
		sdkMeterProvider := sdkmetric.NewMeterProvider(
			sdkmetric.WithResource(resourceAttributes),
			sdkmetric.WithReader(
				sdkmetric.NewPeriodicReader(
					metricExporter,
					sdkmetric.WithTimeout(10*time.Second),
					sdkmetric.WithInterval(15*time.Second),
				)),
		)

		meterProvider = sdkMeterProvider
		shutdownFunctions = []func(ctx context.Context) error{
			sdkMeterProvider.Shutdown,
		}
	} else {
		meterProvider = metricnoop.MeterProvider{}
	}

	otel.SetMeterProvider(meterProvider)

	return meterProvider.Meter(meterName)
}

func InitOTelSdkWithConfig(
	ctx context.Context,
	meterName string,
	oTelSdkConfig *OTelSdkConfig,
) otelmetric.Meter {
	// InitOTelSdkWithConfig is used in the operator manager process. Depending on changes to the operator configuration
	// resouce (in particular, spec.selfMonitoring.enabled and the export config), the OTel SDK might need to be
	// started, shut down, and restarted multiple times during the lifetime of the operator manager process. This can
	// potentially be triggered by different threads, thus we need thread safety here.
	oTelSdkMutex.Lock()
	defer func() {
		oTelSdkMutex.Unlock()
	}()

	if oTelSdkConfig.Endpoint != "" {
		// We currently ignore the log level from the config, setting a log level is cumbersome with OTel Go SDK.
		// Would need to be a new logger with the correct level.
		// otel.SetLogger(logger)

		var metricExporter sdkmetric.Exporter

		protocol := oTelSdkConfig.Protocol
		if protocol == "" {
			// http/protobuf is the default transport protocol, see spec:
			// https://github.com/open-telemetry/opentelemetry-specification/blob/main/specification/protocol/exporter.md
			protocol = ProtocolHttpProtobuf
		}

		var err error
		switch protocol {
		case ProtocolGrpc:
			var options []otlpmetricgrpc.Option
			if EndpointHasScheme(oTelSdkConfig.Endpoint) {
				log.Printf("Using a gRPC export for self-monitoring (via WithEndpointURL): %s \n", oTelSdkConfig.Endpoint)
				options = []otlpmetricgrpc.Option{otlpmetricgrpc.WithEndpointURL(oTelSdkConfig.Endpoint)}
			} else {
				log.Printf("Using a gRPC export for self-monitoring (via WithEndpoint): %s\n", oTelSdkConfig.Endpoint)
				options = []otlpmetricgrpc.Option{otlpmetricgrpc.WithEndpoint(oTelSdkConfig.Endpoint)}
			}
			if len(oTelSdkConfig.Headers) > 0 {
				options = append(options, otlpmetricgrpc.WithHeaders(oTelSdkConfig.Headers))
			}
			if metricExporter, err = otlpmetricgrpc.New(ctx, options...); err != nil {
				log.Printf("Cannot create the OTLP gRPC metrics exporter: %v\n", err)
			}
		case ProtocolHttpProtobuf:
			var options []otlpmetrichttp.Option
			if EndpointHasScheme(oTelSdkConfig.Endpoint) {
				log.Printf("Using an HTTP export for self-monitoring (via WithEndpointURL): %s \n", oTelSdkConfig.Endpoint)
				options = []otlpmetrichttp.Option{otlpmetrichttp.WithEndpointURL(oTelSdkConfig.Endpoint)}
			} else {
				log.Printf("Using an HTTP export for self-monitoring (via WithEndpoint): %s\n", oTelSdkConfig.Endpoint)
				options = []otlpmetrichttp.Option{otlpmetrichttp.WithEndpoint(oTelSdkConfig.Endpoint)}
			}
			if len(oTelSdkConfig.Headers) > 0 {
				options = append(options, otlpmetrichttp.WithHeaders(oTelSdkConfig.Headers))
			}
			if metricExporter, err = otlpmetrichttp.New(ctx, options...); err != nil {
				log.Printf("Cannot create the OTLP HTTP metrics exporter: %v\n", err)
			}
		case ProtocolHttpJson:
			log.Printf("Cannot create the OTLP HTTP exporter: the protocol 'http/json' is currently unsupported\n")
		default:
			log.Printf("Unexpected OTLP protocol set as value of the 'OTEL_EXPORTER_OTLP_PROTOCOL' environment variable: %v\n", protocol)
		}

		// This method is only used for the operator manager deployment, which has no daemonset UID (since it is a
		// deployment), and the deployment UID is already contained in oTelSdkConfig.ResourceAttributes. Hence, we
		// ignore the daemonset/deployment UID return values from getKubernetesResourceAttributes here deliberately.
		podUid, nodeName, _, _ := getKubernetesResourceAttributes()
		resourceAttributes := assembleResource(ctx, podUid, nodeName, "", "", oTelSdkConfig.ResourceAttributes)
		sdkMeterProvider := sdkmetric.NewMeterProvider(
			sdkmetric.WithResource(resourceAttributes),
			sdkmetric.WithReader(
				sdkmetric.NewPeriodicReader(
					metricExporter,
					sdkmetric.WithTimeout(10*time.Second),
					sdkmetric.WithInterval(15*time.Second),
				)),
		)

		meterProvider = sdkMeterProvider
		shutdownFunctions = []func(ctx context.Context) error{
			sdkMeterProvider.Shutdown,
		}
	} else {
		meterProvider = metricnoop.MeterProvider{}
	}

	otel.SetMeterProvider(meterProvider)
	return meterProvider.Meter(meterName)
}

func getKubernetesResourceAttributes() (string, string, string, string) {
	podUid, isSet := os.LookupEnv("K8S_POD_UID")
	if !isSet {
		log.Println("Env var 'K8S_POD_UID' is not set")
	}

	nodeName, isSet := os.LookupEnv("K8S_NODE_NAME")
	if !isSet {
		log.Println("Env var 'K8S_NODE_NAME' is not set")
	}

	daemonSetUid := os.Getenv("K8S_DAEMONSET_UID")
	deploymentUid := os.Getenv("K8S_DEPLOYMENT_UID")

	return podUid, nodeName, daemonSetUid, deploymentUid
}

func assembleResource(
	ctx context.Context,
	podUid string,
	nodeName string,
	daemonSetUid string,
	deploymentUid string,
	extraResourceAttributes []attribute.KeyValue,
) *resource.Resource {
	attributes := make([]attribute.KeyValue, 0, len(extraResourceAttributes)+2)
	attributes = append(attributes, semconv.K8SPodUID(podUid))
	attributes = append(attributes, semconv.K8SNodeName(nodeName))
	if daemonSetUid != "" {
		attributes = append(attributes, semconv.K8SDaemonSetUID(daemonSetUid))
	}
	if deploymentUid != "" {
		attributes = append(attributes, semconv.K8SDeploymentUID(deploymentUid))
	}
	for _, keyValue := range extraResourceAttributes {
		attributes = append(attributes, keyValue)
	}
	resourceAttributes, err := resource.New(ctx,
		resource.WithAttributes(attributes...),
	)
	if err != nil {
		log.Printf("Cannot initialize the OpenTelemetry resource: %v\n", err)
	}
	return resourceAttributes
}

// ShutDownOTelSdk calls the Shutdown function on the sdkMeterProvider, and removes the references to the
// sdkMeterProvider and the shutdown functions.
func ShutDownOTelSdk(ctx context.Context) {
	if len(shutdownFunctions) == 0 {
		return
	}

	timeoutCtx, cancelFun := context.WithTimeout(ctx, time.Second)
	defer cancelFun()
	for _, shutdownFunction := range shutdownFunctions {
		if err := shutdownFunction(timeoutCtx); err != nil {
			log.Printf("Failed to shutdown self monitoring, telemetry may have been lost:%v\n", err)
		}
	}
	shutdownFunctions = nil
	meterProvider = nil
}

// ShutDownOTelSdkThreadSafe calls the Shutdown function on the sdkMeterProvider, and removes the references to the
// sdkMeterProvider and the shutdown functions. This variant of ShutDownOTelSdk is to be used in the operator manager.
// Auxiliary collector processes like configreloader and filelogoffsetsync can use ShutDownOTelSdk directly.
func ShutDownOTelSdkThreadSafe(ctx context.Context) {
	oTelSdkMutex.Lock()
	defer func() {
		oTelSdkMutex.Unlock()
	}()

	ShutDownOTelSdk(ctx)
}

func EndpointHasScheme(endpoint string) bool {
	return endpointSchemeRegex.MatchString(endpoint)
}
