// SPDX-FileCopyrightText: Copyright 2025 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package selfmonitoringapiaccess

import (
	"context"
	"fmt"
	"sync/atomic"

	"github.com/dash0hq/dash0-operator/internal/util"
	"github.com/go-logr/logr"
	"go.opentelemetry.io/otel/attribute"
	otelmetric "go.opentelemetry.io/otel/metric"
	semconv "go.opentelemetry.io/otel/semconv/v1.27.0"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/log"

	dash0v1alpha1 "github.com/dash0hq/dash0-operator/api/dash0monitoring/v1alpha1"
	"github.com/dash0hq/dash0-operator/images/pkg/common"
)

type oTelSdkState int

const (
	waitingToBeStarted = iota
	running
	hasBeenShutDown
)

type SelfMonitoringClient interface {
	InitializeSelfMonitoringMetrics(otelmetric.Meter, string, *logr.Logger)
}

type OTelSdkConfigInput struct {
	export                  dash0v1alpha1.Export
	controllerDeploymentUID types.UID
	controllerContainerName string
	operatorVersion         string
	developmentMode         bool
}

type OTelSdkStarter struct {
	currentOTelSdkState    atomic.Pointer[oTelSdkState] // TODO this should maybe be in otel.go instead?
	oTelSdkConfigInput     atomic.Pointer[OTelSdkConfigInput]
	authTokenFromSecretRef atomic.Pointer[string]
	configCompleteChannel  chan *common.OTelSdkConfig
	logger                 *logr.Logger
}

const (
	meterName = "dash0.operator.manager"
)

var (
	metricNamePrefix = fmt.Sprintf("%s.", meterName)
	meter            otelmetric.Meter
)

func NewOTelSdkStarter() *OTelSdkStarter {
	starter := &OTelSdkStarter{
		currentOTelSdkState:    atomic.Pointer[oTelSdkState]{},
		oTelSdkConfigInput:     atomic.Pointer[OTelSdkConfigInput]{},
		authTokenFromSecretRef: atomic.Pointer[string]{},
		configCompleteChannel:  make(chan *common.OTelSdkConfig),
	}
	// we start with  empty values (so we don't have to deal with nil later)
	var initialState oTelSdkState = waitingToBeStarted
	starter.currentOTelSdkState.Store(&initialState)
	starter.oTelSdkConfigInput.Store(&OTelSdkConfigInput{})
	starter.authTokenFromSecretRef.Store(ptr.To(""))
	return starter
}

func (s *OTelSdkStarter) WaitForOTelConfig(selfMonitoringClients []SelfMonitoringClient) {
	go s.waitForCompleteOTelSDKConfiguration(selfMonitoringClients)
}

func (s *OTelSdkStarter) SetInput(
	export dash0v1alpha1.Export,
	controllerDeploymentUID types.UID,
	controllerContainerName string,
	operatorVersion string,
	developmentMode bool,
	logger *logr.Logger,
) {
	s.oTelSdkConfigInput.Store(&OTelSdkConfigInput{
		export:                  export,
		controllerDeploymentUID: controllerDeploymentUID,
		controllerContainerName: controllerContainerName,
		operatorVersion:         operatorVersion,
		developmentMode:         developmentMode,
	})
	s.onInputHasChanged(logger)
}

func (s *OTelSdkStarter) RemoveInput(logger *logr.Logger) {
	s.oTelSdkConfigInput.Store(&OTelSdkConfigInput{})
	s.onInputHasChanged(logger)
}

func (s *OTelSdkStarter) SetAuthTokenFromSecretRef(authToken string, logger *logr.Logger) {
	s.authTokenFromSecretRef.Store(ptr.To(authToken))
	s.onInputHasChanged(logger)
}

func (s *OTelSdkStarter) RemoveAuthTokenFromSecretRef(logger *logr.Logger) {
	s.authTokenFromSecretRef.Store(ptr.To(""))
	s.onInputHasChanged(logger)
}

func (s *OTelSdkStarter) onInputHasChanged(logger *logr.Logger) {
	currentOTelSdkState := *s.currentOTelSdkState.Load()
	oTelSDKConfig, configComplete :=
		convertExportConfigurationToOTelSDKConfig(
			s.oTelSdkConfigInput.Load(),
			s.authTokenFromSecretRef.Load(),
		)
	if configComplete {
		if currentOTelSdkState == waitingToBeStarted {
			s.configCompleteChannel <- oTelSDKConfig
		} else if currentOTelSdkState == running {
			// TODO check if config has changed, restart OTel SDK with new config if necessary
		} else if currentOTelSdkState == hasBeenShutDown {
			// TODO restart OTel SDK -- after having called sdkMeterProvider.Shutdown previously, we probably need to
			// re-create the meter provider and all meters. Maybe otel.go should own `currentOTelSdkState` instead of
			// this module?
			logger.Error(fmt.Errorf("OTel SDK restart not supported"), "OTel SDK restart not supported")
		} else {
			logger.Error(
				fmt.Errorf("unknown OTel SDK state %d", currentOTelSdkState),
				"unknown OTel SDK state",
			)
		}
	} else {
		// The config is not yet complete (or no longer complete after removing required settings from the operator
		// configuration resources). Shut down the OTel SDK if it is still active.
		if currentOTelSdkState == waitingToBeStarted {
			// nothing to do
		} else if currentOTelSdkState == running {
			// TODO shutdown
			logger.Error(
				fmt.Errorf("OTel SDK shutdown not implemented"),
				"OTel SDK shutdown not implemented",
			)
		} else if currentOTelSdkState == hasBeenShutDown {
			// nothing to do
		} else {
			logger.Error(
				fmt.Errorf("unknown OTel SDK state %d", currentOTelSdkState),
				"unknown OTel SDK state",
			)
		}
	}
}

func (s *OTelSdkStarter) waitForCompleteOTelSDKConfiguration(
	selfMonitoringClients []SelfMonitoringClient,
) {
	// blocks until the config becomes available
	config := <-s.configCompleteChannel
	startOTelSDK(selfMonitoringClients, config)
}

func startOTelSDK(selfMonitoringClients []SelfMonitoringClient, oTelSdkConfig *common.OTelSdkConfig) {
	ctx := context.Background()
	logger := log.FromContext(ctx)
	logger.Info("XXX starting OTel SDK", "config", oTelSdkConfig)
	meter =
		common.InitOTelSdkWithConfig(
			ctx,
			meterName,
			oTelSdkConfig,
			logger,
		)
	for _, client := range selfMonitoringClients {
		client.InitializeSelfMonitoringMetrics(
			meter,
			metricNamePrefix,
			&logger,
		)
	}
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
			// Deliberatly not prepending a protocol here, but using the endpoint as-is. When configuring this via env
			// var OTEL_EXPORTER_OTLP_ENDPOINT, the Go SDK will expect the endpoint to be a valid URL including a
			// protocol. When setting the endpoint via in-code configuration, no protocol is expected.
			Endpoint: dash0Export.Endpoint,
			Protocol: "grpc",
			Headers:  headers,
		}
	} else if selfMonitoringExport.Grpc != nil {
		endpointAndHeaders = &EndpointAndHeaders{
			Endpoint: selfMonitoringExport.Grpc.Endpoint,
			Protocol: "grpc",
			Headers:  selfMonitoringExport.Grpc.Headers,
		}
	} else if selfMonitoringExport.Http != nil {
		protocol := "http/protobuf"
		// The Go SDK does not support http/json, so we ignore this setting for now.
		// if selfMonitoringExport.Http.Encoding == dash0v1alpha1.Json {
		// 	 protocol = "http/json"
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

	controllerDeploymentUID := oTelSdkConfigInput.controllerDeploymentUID
	controllerContainerName := oTelSdkConfigInput.controllerContainerName
	operatorVersion := oTelSdkConfigInput.operatorVersion
	developmentMode := oTelSdkConfigInput.developmentMode
	oTelSdkConfig := &common.OTelSdkConfig{
		Endpoint: endpointAndHeaders.Endpoint,
		Protocol: endpointAndHeaders.Protocol,
		ResourceAttributes: []attribute.KeyValue{
			{
				Key:   semconv.ServiceNamespaceKey,
				Value: attribute.StringValue("dash0.operator"),
			},
			{
				Key:   semconv.ServiceNameKey,
				Value: attribute.StringValue(controllerContainerName),
			},
			{
				Key:   semconv.ServiceVersionKey,
				Value: attribute.StringValue(operatorVersion),
			},
			{
				Key:   semconv.K8SDeploymentUIDKey,
				Value: attribute.StringValue(string(controllerDeploymentUID)),
			},
		},
	}
	if len(endpointAndHeaders.Headers) > 0 {
		headers := make(map[string]string)
		for _, header := range endpointAndHeaders.Headers {
			headers[header.Name] = header.Value
		}
		oTelSdkConfig.Headers = headers
	}
	if developmentMode {
		oTelSdkConfig.LogLevel = "debug"
	}

	return oTelSdkConfig, true
}
