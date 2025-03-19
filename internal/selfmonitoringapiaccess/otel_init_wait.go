// SPDX-FileCopyrightText: Copyright 2025 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package selfmonitoringapiaccess

import (
	"context"
	"fmt"
	"reflect"
	"sync/atomic"

	"github.com/go-logr/logr"
	"go.opentelemetry.io/otel/attribute"
	otelmetric "go.opentelemetry.io/otel/metric"
	semconv "go.opentelemetry.io/otel/semconv/v1.27.0"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/log"

	dash0v1alpha1 "github.com/dash0hq/dash0-operator/api/dash0monitoring/v1alpha1"
	"github.com/dash0hq/dash0-operator/images/pkg/common"
	"github.com/dash0hq/dash0-operator/internal/util"
	zaputil "github.com/dash0hq/dash0-operator/internal/util/zap"
)

type SelfMonitoringMetricsClient interface {
	InitializeSelfMonitoringMetrics(otelmetric.Meter, string, *logr.Logger)
}

type OTelSdkConfigInput struct {
	export                        dash0v1alpha1.Export
	pseudoClusterUid              string
	operatorNamespace             string
	operatorManagerDeploymentUID  types.UID
	operatorManagerDeploymentName string
	operatorManagerPodName        string
	operatorVersion               string
	developmentMode               bool
}

type OTelSdkStarter struct {
	sdkIsActive            atomic.Bool
	oTelSdkConfigInput     atomic.Pointer[OTelSdkConfigInput]
	activeOTelSdkConfig    atomic.Pointer[common.OTelSdkConfig]
	authTokenFromSecretRef atomic.Pointer[string]
	delegatingZapCore      *zaputil.DelegatingZapCore

	startOrRestartOTelSdkChannel chan *common.OTelSdkConfig
	shutDownChannel              chan bool
}

const (
	operatorManagerServiceNamespace = "dash0-operator"
	operatorManagerServiceName      = "operator-manager"
	meterName                       = "dash0.operator.manager"
)

var (
	metricNamePrefix = fmt.Sprintf("%s.", meterName)
)

func NewOTelSdkStarter(delegatingZapCore *zaputil.DelegatingZapCore) *OTelSdkStarter {
	starter := &OTelSdkStarter{
		sdkIsActive:                  atomic.Bool{},
		oTelSdkConfigInput:           atomic.Pointer[OTelSdkConfigInput]{},
		activeOTelSdkConfig:          atomic.Pointer[common.OTelSdkConfig]{},
		authTokenFromSecretRef:       atomic.Pointer[string]{},
		delegatingZapCore:            delegatingZapCore,
		startOrRestartOTelSdkChannel: make(chan *common.OTelSdkConfig),
		shutDownChannel:              make(chan bool),
	}
	// we start with empty values (so we don't have to deal with nil later)
	starter.oTelSdkConfigInput.Store(&OTelSdkConfigInput{})
	starter.authTokenFromSecretRef.Store(ptr.To(""))
	return starter
}

func (s *OTelSdkStarter) WaitForOTelConfig(
	selfMonitoringMetricsClient []SelfMonitoringMetricsClient,
) {
	go s.waitForCompleteOTelSDKConfiguration(selfMonitoringMetricsClient)
}

func (s *OTelSdkStarter) SetOTelSdkParameters(
	ctx context.Context,
	export dash0v1alpha1.Export,
	pseudoClusterUID string,
	operatorNamespace string,
	operatorManagerDeploymentUID types.UID,
	operatorManagerDeploymentName string,
	operatorManagerPodName string,
	operatorVersion string,
	developmentMode bool,
	logger *logr.Logger,
) {
	s.oTelSdkConfigInput.Store(&OTelSdkConfigInput{
		export:                        export,
		operatorManagerDeploymentUID:  operatorManagerDeploymentUID,
		pseudoClusterUid:              pseudoClusterUID,
		operatorNamespace:             operatorNamespace,
		operatorManagerDeploymentName: operatorManagerDeploymentName,
		operatorManagerPodName:        operatorManagerPodName,
		operatorVersion:               operatorVersion,
		developmentMode:               developmentMode,
	})
	s.onParametersHaveChanged(ctx, logger)
}

func (s *OTelSdkStarter) RemoveOTelSdkParameters(ctx context.Context, logger *logr.Logger) {
	s.oTelSdkConfigInput.Store(&OTelSdkConfigInput{})
	s.onParametersHaveChanged(ctx, logger)
}

func (s *OTelSdkStarter) SetAuthToken(ctx context.Context, authToken string, logger *logr.Logger) {
	s.authTokenFromSecretRef.Store(ptr.To(authToken))
	s.onParametersHaveChanged(ctx, logger)
}

func (s *OTelSdkStarter) RemoveAuthToken(ctx context.Context, logger *logr.Logger) {
	s.authTokenFromSecretRef.Store(ptr.To(""))
	s.onParametersHaveChanged(ctx, logger)
}

func (s *OTelSdkStarter) waitForCompleteOTelSDKConfiguration(
	selfMonitoringMetricsClients []SelfMonitoringMetricsClient,
) {
	for {
		select {
		case config := <-s.startOrRestartOTelSdkChannel:
			s.UpdateOTelSdkState(true)
			s.activeOTelSdkConfig.Store(config)
			startOTelSDK(s.delegatingZapCore, selfMonitoringMetricsClients, config)

		case <-s.shutDownChannel:
			return
		}
	}
}

func (s *OTelSdkStarter) onParametersHaveChanged(ctx context.Context, logger *logr.Logger) {
	sdkIsActive := s.sdkIsActive.Load()
	newOTelSDKConfig, configComplete :=
		convertExportConfigurationToOTelSDKConfig(
			s.oTelSdkConfigInput.Load(),
			s.authTokenFromSecretRef.Load(),
		)
	if configComplete {
		if !sdkIsActive {
			logger.Info("starting the OpenTelemetry SDK for self-monitoring")
			s.startOrRestartOTelSdkChannel <- newOTelSDKConfig
		} else {
			currentlyActiveConfig := s.activeOTelSdkConfig.Load()
			if !reflect.DeepEqual(currentlyActiveConfig, newOTelSDKConfig) {
				logger.Info(
					"restarting the OpenTelemetry SDK for self-monitoring (config has changed)",
					"old configuration",
					currentlyActiveConfig,
					"new configuration",
					newOTelSDKConfig,
				)
				s.ShutDownOTelSdk(ctx, logger)
				s.startOrRestartOTelSdkChannel <- newOTelSDKConfig
			}
		}
	} else {
		if sdkIsActive {
			// The config is not yet complete (or no longer complete after removing required settings from the operator
			// configuration resources). Shut down the OTel SDK if it is currently active.
			s.ShutDownOTelSdk(ctx, logger)
		}
	}
}

func (s *OTelSdkStarter) UpdateOTelSdkState(newState bool) {
	s.sdkIsActive.Store(newState)
}

// convertExportConfigurationToOTelSDKConfig is used when enabling self-monitoring from within an already running
// process. We use this approach for the operator manager.
func convertExportConfigurationToOTelSDKConfig(
	oTelSdkConfigInput *OTelSdkConfigInput,
	authTokenFromSecretRef *string,
) (*common.OTelSdkConfig, bool) {
	if oTelSdkConfigInput == nil {
		return nil, false
	}
	selfMonitoringExport := oTelSdkConfigInput.export
	var endpointAndHeaders *EndpointAndHeaders
	if selfMonitoringExport.Dash0 != nil {
		dash0Export := selfMonitoringExport.Dash0
		token := selfMonitoringExport.Dash0.Authorization.Token
		if token == nil || *token == "" {
			token = authTokenFromSecretRef
		}
		if token == nil || *token == "" {
			return nil, false
		}

		headers := []dash0v1alpha1.Header{{
			Name:  util.AuthorizationHeaderName,
			Value: fmt.Sprintf("Bearer %s", *token),
		}}
		if dash0Export.Dataset != "" && dash0Export.Dataset != util.DatasetDefault {
			headers = append(headers, dash0v1alpha1.Header{
				Name:  util.Dash0DatasetHeaderName,
				Value: dash0Export.Dataset,
			})
		}
		endpointAndHeaders = &EndpointAndHeaders{
			// Deliberately not prepending a protocol here, but using the endpoint as-is. When configuring this via env
			// var OTEL_EXPORTER_OTLP_ENDPOINT, the Go SDK will expect the endpoint to be a valid URL including a
			// protocol. When setting the endpoint via in-code configuration, no protocol is expected.
			Endpoint: dash0Export.Endpoint,
			Protocol: common.ProtocolGrpc,
			Headers:  headers,
		}
	} else if selfMonitoringExport.Grpc != nil {
		endpointAndHeaders = &EndpointAndHeaders{
			Endpoint: selfMonitoringExport.Grpc.Endpoint,
			Protocol: common.ProtocolGrpc,
			Headers:  selfMonitoringExport.Grpc.Headers,
		}
	} else if selfMonitoringExport.Http != nil {
		protocol := common.ProtocolHttpProtobuf
		// The Go SDK does not support http/json, so we ignore this setting for now.
		// if selfMonitoringExport.Http.Encoding == dash0v1alpha1.Json {
		// 	 protocol = common.ProtocolHttpJson
		// }
		endpointAndHeaders = &EndpointAndHeaders{
			Endpoint: selfMonitoringExport.Http.Endpoint,
			Protocol: protocol,
			Headers:  selfMonitoringExport.Http.Headers,
		}
	}

	if endpointAndHeaders == nil {
		return nil, false
	}

	resourceAttributes := []attribute.KeyValue{
		{
			Key:   semconv.ServiceNamespaceKey,
			Value: attribute.StringValue(operatorManagerServiceNamespace),
		},
		{
			Key:   semconv.ServiceNameKey,
			Value: attribute.StringValue(operatorManagerServiceName),
		},
		{
			Key:   semconv.ServiceVersionKey,
			Value: attribute.StringValue(oTelSdkConfigInput.operatorVersion),
		},
		{
			Key:   semconv.K8SClusterUIDKey,
			Value: attribute.StringValue(string(oTelSdkConfigInput.pseudoClusterUid)),
		},
		{
			Key:   semconv.K8SNamespaceNameKey,
			Value: attribute.StringValue(oTelSdkConfigInput.operatorNamespace),
		},
		{
			Key:   semconv.K8SDeploymentNameKey,
			Value: attribute.StringValue(oTelSdkConfigInput.operatorManagerDeploymentName),
		},
		{
			Key:   semconv.K8SDeploymentUIDKey,
			Value: attribute.StringValue(string(oTelSdkConfigInput.operatorManagerDeploymentUID)),
		},
		{
			Key:   semconv.K8SPodNameKey,
			Value: attribute.StringValue(oTelSdkConfigInput.operatorManagerPodName),
		},
	}

	oTelSdkConfig := &common.OTelSdkConfig{
		Endpoint:           endpointAndHeaders.Endpoint,
		Protocol:           endpointAndHeaders.Protocol,
		ResourceAttributes: resourceAttributes,
	}
	if len(endpointAndHeaders.Headers) > 0 {
		headers := make(map[string]string)
		for _, header := range endpointAndHeaders.Headers {
			headers[header.Name] = header.Value
		}
		oTelSdkConfig.Headers = headers
	}
	if oTelSdkConfigInput.developmentMode {
		oTelSdkConfig.LogLevel = "debug"
	}

	return oTelSdkConfig, true
}

func startOTelSDK(
	delegatingZapCore *zaputil.DelegatingZapCore,
	selfMonitoringMetricsClients []SelfMonitoringMetricsClient,
	oTelSdkConfig *common.OTelSdkConfig,
) {
	ctx := context.Background()
	logger := log.FromContext(ctx)
	logger.Info("starting (or restarting) the OpenTelemetry SDK for self-monitoring")
	zapOTelBridge, meter :=
		common.InitOTelSdkWithConfig(
			ctx,
			meterName,
			oTelSdkConfig,
		)

	// Setting the zap OTel bridge as the delegate logger core will spool all messages that have been only logged to
	// stdout so far (and buffered in the delegating zap core) to the newly created zap OTel bridge, so that we also see
	// messages from the operator startup in self-monitoring, even if the OTel SDK is delayed until we have found and
	// reconciled an operator configuration resource.
	delegatingZapCore.SetDelegate(zapOTelBridge)

	for _, client := range selfMonitoringMetricsClients {
		client.InitializeSelfMonitoringMetrics(
			meter,
			metricNamePrefix,
			&logger,
		)
	}
}

func (s *OTelSdkStarter) ShutDownOTelSdk(ctx context.Context, logger *logr.Logger) {
	sdkIsActive := s.sdkIsActive.Load()
	if sdkIsActive {
		s.delegatingZapCore.UnsetDelegate()
		logger.Info("shuttingdown the OpenTelemetry SDK, stopping self-monitoring")
		common.ShutDownOTelSdkThreadSafe(ctx)
		s.UpdateOTelSdkState(false)
		s.activeOTelSdkConfig.Store(&common.OTelSdkConfig{})
	}
}

func (s *OTelSdkStarter) ForTestOnlyGetState() (bool, *common.OTelSdkConfig, *string) {
	return s.sdkIsActive.Load(),
		s.activeOTelSdkConfig.Load(),
		s.authTokenFromSecretRef.Load()
}

func (s *OTelSdkStarter) ShutDown(ctx context.Context, logger *logr.Logger) {
	s.ShutDownOTelSdk(ctx, logger)
	s.shutDownChannel <- true
}
