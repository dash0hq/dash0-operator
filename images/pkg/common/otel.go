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

	"go.opentelemetry.io/contrib/bridges/otelzap"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/exporters/otlp/otlplog/otlploggrpc"
	"go.opentelemetry.io/otel/exporters/otlp/otlplog/otlploghttp"
	"go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetricgrpc"
	"go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetrichttp"
	otellog "go.opentelemetry.io/otel/log"
	"go.opentelemetry.io/otel/log/global"
	lognoop "go.opentelemetry.io/otel/log/noop"
	otelmetric "go.opentelemetry.io/otel/metric"
	metricnoop "go.opentelemetry.io/otel/metric/noop"
	sdklog "go.opentelemetry.io/otel/sdk/log"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/resource"
	semconv "go.opentelemetry.io/otel/semconv/v1.26.0"
)

type OTelSdkConfig struct {
	Endpoint         string
	Protocol         string
	ServiceName      string
	ServiceVersion   string
	PseudoClusterUid string
	ClusterName      string
	DeploymentUid    string
	DeploymentName   string
	ContainerName    string

	LogLevel string
	Headers  map[string]string
}

const (
	OperatorManagerServiceNamespace = "dash0-operator"

	ProtocolGrpc         = "grpc"
	ProtocolHttpProtobuf = "http/protobuf"
	ProtocolHttpJson     = "http/json"
)

var (
	meterProvider       otelmetric.MeterProvider
	loggerProvider      otellog.LoggerProvider
	shutdownFunctions   []func(ctx context.Context) error
	oTelSdkMutex        sync.Mutex
	endpointSchemeRegex = regexp.MustCompile(`^\w+://`)
)

func OTelSDKIsConfigured() bool {
	return os.Getenv("OTEL_EXPORTER_OTLP_ENDPOINT") != ""
}

func InitOTelSdkFromEnvVars(
	ctx context.Context,
	meterName string,
	serviceName string,
	containerName string,
) otelmetric.Meter {
	// InitOTelSdkFromEnvVars is used in the configuration reloader and filelog offset sync container. The OTel SDK
	// will either be started once at process startup or not. In contrast to InitOTelSdkWithConfig, it will not be
	// restarted, and the configuration is not modified from different threads. Hence, no thread safety is needed here
	// and oTelSdkMutex is not used.
	if OTelSDKIsConfigured() {

		protocol, protocolIsSet := os.LookupEnv("OTEL_EXPORTER_OTLP_PROTOCOL")
		if !protocolIsSet {
			// http/protobuf is the default transport protocol, see spec:
			// https://github.com/open-telemetry/opentelemetry-specification/blob/main/specification/protocol/exporter.md
			protocol = ProtocolHttpProtobuf
		}

		metricExporter := createMetricExporterFromProtocolEnvVar(ctx, protocol)
		logExporter := createLogExporterFromProtocolEnvVar(ctx, protocol)

		resourceAttributes := assembleResource(
			ctx,
			serviceName,
			// serviceVersion will be read from env var
			"",
			// pseudoClusterUid will be read from env var
			"",
			// clusterName will be read from env var
			"",
			// deploymentUid will be read from env var
			"",
			// deploymentName will be read from env var
			"",
			containerName,
		)

		sdkMeterProvider := sdkmetric.NewMeterProvider(
			sdkmetric.WithResource(resourceAttributes),
			sdkmetric.WithReader(
				sdkmetric.NewPeriodicReader(
					metricExporter,
					sdkmetric.WithTimeout(10*time.Second),
					sdkmetric.WithInterval(15*time.Second),
				)),
		)
		sdkLoggerProvider := sdklog.NewLoggerProvider(
			sdklog.WithResource(resourceAttributes),
			sdklog.WithProcessor(
				sdklog.NewBatchProcessor(logExporter),
			),
		)

		meterProvider = sdkMeterProvider
		loggerProvider = sdkLoggerProvider
		shutdownFunctions = []func(ctx context.Context) error{
			sdkMeterProvider.Shutdown,
			sdkLoggerProvider.Shutdown,
		}
	} else {
		meterProvider = metricnoop.MeterProvider{}
		loggerProvider = lognoop.LoggerProvider{}
	}

	otel.SetMeterProvider(meterProvider)
	global.SetLoggerProvider(loggerProvider)

	return meterProvider.Meter(meterName)
}

func createMetricExporterFromProtocolEnvVar(ctx context.Context, protocol string) sdkmetric.Exporter {
	var metricExporter sdkmetric.Exporter
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
		log.Fatalf("Cannot create the OTLP HTTP metrics exporter: the protocol 'http/json' is currently unsupported")
	default:
		log.Fatalf(
			"Unexpected OTLP protocol set as value of the 'OTEL_EXPORTER_OTLP_PROTOCOL' environment variable: %v",
			protocol,
		)
	}
	return metricExporter
}

func createLogExporterFromProtocolEnvVar(ctx context.Context, protocol string) sdklog.Exporter {
	var logExporter sdklog.Exporter
	var err error

	switch protocol {
	case ProtocolGrpc:
		if logExporter, err = otlploggrpc.New(ctx); err != nil {
			log.Fatalf("Cannot create the OTLP gRPC log exporter: %v", err)
		}
	case ProtocolHttpProtobuf:
		if logExporter, err = otlploghttp.New(ctx); err != nil {
			log.Fatalf("Cannot create the OTLP HTTP log exporter: %v", err)
		}
	case ProtocolHttpJson:
		log.Fatalf("Cannot create the OTLP HTTP log exporter: the protocol 'http/json' is currently unsupported")
	default:
		log.Fatalf(
			"Unexpected OTLP protocol set as value of the 'OTEL_EXPORTER_OTLP_PROTOCOL' environment variable: %v",
			protocol,
		)
	}
	return logExporter
}

func InitOTelSdkWithConfig(
	ctx context.Context,
	meterName string,
	oTelSdkConfig *OTelSdkConfig,
) (*otelzap.Core, otelmetric.Meter) {
	// InitOTelSdkWithConfig is used in the operator manager process. Depending on changes to the operator configuration
	// resource (in particular, spec.selfMonitoring.enabled and the export config), the OTel SDK might need to be
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

		metricExporter := createMetricExporterFromConfig(ctx, oTelSdkConfig)
		logExporter := createLogExporterFromConfig(ctx, oTelSdkConfig)

		resourceAttributes := assembleResource(
			ctx,
			oTelSdkConfig.ServiceName,
			oTelSdkConfig.ServiceVersion,
			oTelSdkConfig.PseudoClusterUid,
			oTelSdkConfig.ClusterName,
			oTelSdkConfig.DeploymentUid,
			oTelSdkConfig.DeploymentName,
			oTelSdkConfig.ContainerName,
		)

		sdkMeterProvider := sdkmetric.NewMeterProvider(
			sdkmetric.WithResource(resourceAttributes),
			sdkmetric.WithReader(
				sdkmetric.NewPeriodicReader(
					metricExporter,
					sdkmetric.WithTimeout(10*time.Second),
					sdkmetric.WithInterval(15*time.Second),
				)),
		)
		sdkLoggerProvider := sdklog.NewLoggerProvider(
			sdklog.WithResource(resourceAttributes),
			sdklog.WithProcessor(
				sdklog.NewBatchProcessor(logExporter),
			),
		)

		meterProvider = sdkMeterProvider
		loggerProvider = sdkLoggerProvider
		shutdownFunctions = []func(ctx context.Context) error{
			sdkMeterProvider.Shutdown,
			sdkLoggerProvider.Shutdown,
		}
	} else {
		meterProvider = metricnoop.MeterProvider{}
		loggerProvider = lognoop.LoggerProvider{}
	}

	otel.SetMeterProvider(meterProvider)
	global.SetLoggerProvider(loggerProvider)
	otelZapBridge := otelzap.NewCore(
		"dash0-operator",
		otelzap.WithLoggerProvider(loggerProvider),
	)

	return otelZapBridge, meterProvider.Meter(meterName)
}

//nolint:dupl
func createMetricExporterFromConfig(ctx context.Context, oTelSdkConfig *OTelSdkConfig) sdkmetric.Exporter {
	var metricExporter sdkmetric.Exporter
	var err error

	protocol := oTelSdkConfig.Protocol
	if protocol == "" {
		// http/protobuf is the default transport protocol, see spec:
		// https://github.com/open-telemetry/opentelemetry-specification/blob/main/specification/protocol/exporter.md
		protocol = ProtocolHttpProtobuf
	}

	switch protocol {
	case ProtocolGrpc:
		var options []otlpmetricgrpc.Option
		if EndpointHasScheme(oTelSdkConfig.Endpoint) {
			log.Printf("Using a gRPC export for self-monitoring metrics (via WithEndpointURL): %s \n", oTelSdkConfig.Endpoint)
			options = []otlpmetricgrpc.Option{otlpmetricgrpc.WithEndpointURL(oTelSdkConfig.Endpoint)}
		} else {
			log.Printf("Using a gRPC export for self-monitoring metrics (via WithEndpoint): %s\n", oTelSdkConfig.Endpoint)
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
			log.Printf("Using an HTTP export for self-monitoring metrics (via WithEndpointURL): %s \n", oTelSdkConfig.Endpoint)
			options = []otlpmetrichttp.Option{otlpmetrichttp.WithEndpointURL(oTelSdkConfig.Endpoint)}
		} else {
			log.Printf("Using an HTTP export for self-monitoring metrics (via WithEndpoint): %s\n", oTelSdkConfig.Endpoint)
			options = []otlpmetrichttp.Option{otlpmetrichttp.WithEndpoint(oTelSdkConfig.Endpoint)}
		}
		if len(oTelSdkConfig.Headers) > 0 {
			options = append(options, otlpmetrichttp.WithHeaders(oTelSdkConfig.Headers))
		}
		if metricExporter, err = otlpmetrichttp.New(ctx, options...); err != nil {
			log.Printf("Cannot create the OTLP HTTP metrics exporter: %v\n", err)
		}
	case ProtocolHttpJson:
		log.Printf("Cannot create the OTLP HTTP metrics exporter: the protocol 'http/json' is currently unsupported\n")
	default:
		log.Printf("Unexpected OTLP protocol for metrics exporter: %v\n", protocol)
	}
	return metricExporter
}

//nolint:dupl
func createLogExporterFromConfig(ctx context.Context, oTelSdkConfig *OTelSdkConfig) sdklog.Exporter {
	var logExporter sdklog.Exporter
	var err error

	protocol := oTelSdkConfig.Protocol
	if protocol == "" {
		// http/protobuf is the default transport protocol, see spec:
		// https://github.com/open-telemetry/opentelemetry-specification/blob/main/specification/protocol/exporter.md
		protocol = ProtocolHttpProtobuf
	}

	switch protocol {
	case ProtocolGrpc:
		var options []otlploggrpc.Option
		if EndpointHasScheme(oTelSdkConfig.Endpoint) {
			log.Printf("Using a gRPC export for logs self-monitoring (via WithEndpointURL): %s \n", oTelSdkConfig.Endpoint)
			options = []otlploggrpc.Option{otlploggrpc.WithEndpointURL(oTelSdkConfig.Endpoint)}
		} else {
			log.Printf("Using a gRPC export for logs self-monitoring (via WithEndpoint): %s\n", oTelSdkConfig.Endpoint)
			options = []otlploggrpc.Option{otlploggrpc.WithEndpoint(oTelSdkConfig.Endpoint)}
		}
		if len(oTelSdkConfig.Headers) > 0 {
			options = append(options, otlploggrpc.WithHeaders(oTelSdkConfig.Headers))
		}
		if logExporter, err = otlploggrpc.New(ctx, options...); err != nil {
			log.Printf("Cannot create the OTLP gRPC logs exporter: %v\n", err)
		}
	case ProtocolHttpProtobuf:
		var options []otlploghttp.Option
		if EndpointHasScheme(oTelSdkConfig.Endpoint) {
			log.Printf("Using an HTTP export for logs self-monitoring (via WithEndpointURL): %s \n", oTelSdkConfig.Endpoint)
			options = []otlploghttp.Option{otlploghttp.WithEndpointURL(oTelSdkConfig.Endpoint)}
		} else {
			log.Printf("Using an HTTP export for logs self-monitoring (via WithEndpoint): %s\n", oTelSdkConfig.Endpoint)
			options = []otlploghttp.Option{otlploghttp.WithEndpoint(oTelSdkConfig.Endpoint)}
		}
		if len(oTelSdkConfig.Headers) > 0 {
			options = append(options, otlploghttp.WithHeaders(oTelSdkConfig.Headers))
		}
		if logExporter, err = otlploghttp.New(ctx, options...); err != nil {
			log.Printf("Cannot create the OTLP HTTP logs exporter: %v\n", err)
		}
	case ProtocolHttpJson:
		log.Printf("Cannot create the OTLP HTTP logs exporter: the protocol 'http/json' is currently unsupported\n")
	default:
		log.Printf("Unexpected OTLP protocol for logs exporter: %v\n", protocol)
	}
	return logExporter
}

func assembleResource(
	ctx context.Context,
	serviceName,
	serviceVersion,
	pseudoClusterUid string,
	clusterName string,
	deploymentUid string,
	deploymentName string,
	containerName string,
) *resource.Resource {
	var attributes []attribute.KeyValue

	attributes = append(attributes, semconv.ServiceNamespace(OperatorManagerServiceNamespace))
	if serviceName != "" {
		attributes = append(attributes, semconv.ServiceName(serviceName))
	}
	if serviceVersion != "" {
		attributes = append(attributes, semconv.ServiceVersion(serviceVersion))
	} else if serviceVersionFromEnvVar := os.Getenv("SERVICE_VERSION"); serviceVersionFromEnvVar != "" {
		attributes = append(attributes, semconv.ServiceVersion(serviceVersionFromEnvVar))
	}

	if pseudoClusterUid != "" {
		attributes = append(attributes, semconv.K8SClusterUID(pseudoClusterUid))
	} else if pseudoClusterUidFromEnvVar := os.Getenv("K8S_CLUSTER_UID"); pseudoClusterUidFromEnvVar != "" {
		attributes = append(attributes, semconv.K8SClusterUID(pseudoClusterUidFromEnvVar))
	}
	if clusterName != "" {
		attributes = append(attributes, semconv.K8SClusterName(clusterName))
	} else if clusterNameFromEnvVar := os.Getenv("K8S_CLUSTER_NAME"); clusterNameFromEnvVar != "" {
		attributes = append(attributes, semconv.K8SClusterName(clusterNameFromEnvVar))
	}

	if nodeName := os.Getenv("K8S_NODE_NAME"); nodeName != "" {
		attributes = append(attributes, semconv.K8SNodeName(nodeName))
	}

	if namespace := os.Getenv("DASH0_OPERATOR_NAMESPACE"); namespace != "" {
		attributes = append(attributes, semconv.K8SNamespaceName(namespace))
	}

	if daemonSetUid := os.Getenv("K8S_DAEMONSET_UID"); daemonSetUid != "" {
		attributes = append(attributes, semconv.K8SDaemonSetUID(daemonSetUid))
	}
	if daemonSetName := os.Getenv("K8S_DAEMONSET_NAME"); daemonSetName != "" {
		attributes = append(attributes, semconv.K8SDaemonSetName(daemonSetName))
	}
	if deploymentUid != "" {
		attributes = append(attributes, semconv.K8SDeploymentUID(deploymentUid))
	} else if deploymentUidFromEnvVar := os.Getenv("K8S_DEPLOYMENT_UID"); deploymentUidFromEnvVar != "" {
		attributes = append(attributes, semconv.K8SDeploymentUID(deploymentUidFromEnvVar))
	}
	if deploymentName != "" {
		attributes = append(attributes, semconv.K8SDeploymentName(deploymentName))
	} else if deploymentNameFromEnvVar := os.Getenv("K8S_DEPLOYMENT_NAME"); deploymentNameFromEnvVar != "" {
		attributes = append(attributes, semconv.K8SDeploymentName(deploymentNameFromEnvVar))
	}

	if podUid := os.Getenv("K8S_POD_UID"); podUid != "" {
		attributes = append(attributes, semconv.K8SPodUID(podUid))
	}
	if podName := os.Getenv("K8S_POD_NAME"); podName != "" {
		attributes = append(attributes, semconv.K8SPodName(podName))
	}
	if containerName != "" {
		attributes = append(attributes, semconv.K8SContainerName(containerName))
	}

	resource, err := resource.New(ctx,
		resource.WithAttributes(attributes...),
	)
	if err != nil {
		log.Printf("Cannot initialize the OpenTelemetry resource: %v\n", err)
	}
	return resource
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
