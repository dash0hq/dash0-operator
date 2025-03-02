// SPDX-FileCopyrightText: Copyright 2025 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package selfmonitoringapiaccess

import (
	"context"
	"fmt"
	"reflect"
	"sync/atomic"
	"time"

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

type SelfMonitoringClient interface {
	InitializeSelfMonitoringMetrics(otelmetric.Meter, string, *logr.Logger)
}

type OTelSdkConfigInput struct {
	export                       dash0v1alpha1.Export
	operatorManagerDeploymentUID types.UID
	operatorVersion              string
	developmentMode              bool
}

type OTelSdkStarter struct {
	sdkIsActive                  atomic.Bool // TODO this should maybe be in otel.go instead?
	oTelSdkConfigInput           atomic.Pointer[OTelSdkConfigInput]
	activeOTelSdkConfig          atomic.Pointer[common.OTelSdkConfig]
	authTokenFromSecretRef       atomic.Pointer[string]
	startOrRestartOTelSdkChannel chan *common.OTelSdkConfig
}

const (
	meterName = "dash0.operator.manager"
)

var (
	metricNamePrefix = fmt.Sprintf("%s.", meterName)
	meter            otelmetric.Meter
)

// TODO continue in applySelfMonitoringAndApiAccess, a lot of cases are currently commented out.
// TODO fix removeSelfMonitoringAndApiAccessAndUpdate
// TODO activate otel_init_wait_test.go, write more tests
//
// TODO API access! (and the token for it)
// TODO Remove TODO from opconf resource controller line 129 (about not being able to delete the auto opconf resource)
func NewOTelSdkStarter() *OTelSdkStarter {
	starter := &OTelSdkStarter{
		sdkIsActive:                  atomic.Bool{},
		oTelSdkConfigInput:           atomic.Pointer[OTelSdkConfigInput]{},
		activeOTelSdkConfig:          atomic.Pointer[common.OTelSdkConfig]{},
		authTokenFromSecretRef:       atomic.Pointer[string]{},
		startOrRestartOTelSdkChannel: make(chan *common.OTelSdkConfig),
	}
	// we start with empty values (so we don't have to deal with nil later)
	starter.oTelSdkConfigInput.Store(&OTelSdkConfigInput{})
	starter.authTokenFromSecretRef.Store(ptr.To(""))
	return starter
}

func (s *OTelSdkStarter) WaitForOTelConfig(selfMonitoringClients []SelfMonitoringClient) {
	go s.waitForCompleteOTelSDKConfiguration(selfMonitoringClients)
}

func (s *OTelSdkStarter) SetOTelSdkParameters(
	ctx context.Context,
	export dash0v1alpha1.Export,
	operatorManagerDeploymentUID types.UID,
	operatorVersion string,
	developmentMode bool,
	logger *logr.Logger,
) {
	logger.Info("XXX OTelSdkStarter#SetOTelSdkParameters")
	s.oTelSdkConfigInput.Store(&OTelSdkConfigInput{
		export:                       export,
		operatorManagerDeploymentUID: operatorManagerDeploymentUID,
		operatorVersion:              operatorVersion,
		developmentMode:              developmentMode,
	})
	s.onParametersHaveChanged(ctx, logger)
}

func (s *OTelSdkStarter) RemoveOTelSdkParameters(ctx context.Context, logger *logr.Logger) {
	logger.Info("XXX OTelSdkStarter#RemoveOTelSdkParameters")
	s.oTelSdkConfigInput.Store(&OTelSdkConfigInput{})
	s.onParametersHaveChanged(ctx, logger)
}

func (s *OTelSdkStarter) SetAuthTokenFromSecretRef(ctx context.Context, authToken string, logger *logr.Logger) {
	logger.Info("XXX OTelSdkStarter#SetAuthTokenFromSecretRef")
	s.authTokenFromSecretRef.Store(ptr.To(authToken))
	s.onParametersHaveChanged(ctx, logger)
}

func (s *OTelSdkStarter) RemoveAuthTokenFromSecretRef(ctx context.Context, logger *logr.Logger) {
	logger.Info("XXX OTelSdkStarter#RemoveAuthTokenFromSecretRef")
	s.authTokenFromSecretRef.Store(ptr.To(""))
	s.onParametersHaveChanged(ctx, logger)
}

func (s *OTelSdkStarter) waitForCompleteOTelSDKConfiguration(
	selfMonitoringClients []SelfMonitoringClient,
) {
	// TODO exit loop on shutdown
	for {
		config := <-s.startOrRestartOTelSdkChannel
		s.UpdateOTelSdkState(true)
		s.activeOTelSdkConfig.Store(config)
		startOTelSDK(selfMonitoringClients, config)
	}
}

func (s *OTelSdkStarter) onParametersHaveChanged(ctx context.Context, logger *logr.Logger) {
	sdkIsActive := s.sdkIsActive.Load()
	newOTelSDKConfig, configComplete :=
		convertExportConfigurationToOTelSDKConfig(
			s.oTelSdkConfigInput.Load(),
			s.authTokenFromSecretRef.Load(),
		)
	logger.Info(
		fmt.Sprintf(
			"XXX OTelSdkStarter#onParametersHaveChanged -> configComplete: %t, currently active: %t",
			configComplete,
			sdkIsActive,
		))
	if configComplete {
		if !sdkIsActive {
			logger.Info("XXX (RE)STARTING OTel SDK self-monitoring (was previously inactive)")
			s.startOrRestartOTelSdkChannel <- newOTelSDKConfig
		} else {
			currentlyActiveConfig := s.activeOTelSdkConfig.Load()
			if !reflect.DeepEqual(currentlyActiveConfig, newOTelSDKConfig) {
				logger.Info("XXX RESTARTING OTel SDK because config has CHANGED", "PREVIOUS", currentlyActiveConfig, "NEW", newOTelSDKConfig)
				s.ShutDownOTelSdk(ctx, logger)
				s.startOrRestartOTelSdkChannel <- newOTelSDKConfig
			} else {
				logger.Info("XXX OTel SDK is already running and the configuration has not changed.")
			}
		}
	} else {
		if sdkIsActive {
			// The config is not yet complete (or no longer complete after removing required settings from the operator
			// configuration resources). Shut down the OTel SDK if it is currently active.
			s.ShutDownOTelSdk(ctx, logger)
		} else {
			// The OTel SDK is not active, and it should not be active, nothing to do.
			logger.Info("XXX OTel SDK starter will continue to wait for complete config")
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
			// Deliberatly not prepending a protocol here, but using the endpoint as-is. When configuring this via env
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

	operatorManagerDeploymentUID := oTelSdkConfigInput.operatorManagerDeploymentUID
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
				Value: attribute.StringValue(util.OperatorManagerContainerName),
			},
			{
				Key:   semconv.ServiceVersionKey,
				Value: attribute.StringValue(operatorVersion),
			},
			{
				Key:   semconv.K8SDeploymentUIDKey,
				Value: attribute.StringValue(string(operatorManagerDeploymentUID)),
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

func (s *OTelSdkStarter) ShutDownOTelSdk(ctx context.Context, logger *logr.Logger) {
	sdkIsActive := s.sdkIsActive.Load()
	if sdkIsActive {
		logger.Info("XXX SHUTTING DOWN OTel SDK")
		common.ShutDownOTelSdkThreadSafe(ctx)
		s.UpdateOTelSdkState(false)
		s.activeOTelSdkConfig.Store(&common.OTelSdkConfig{})
	} else {
		logger.Info("OTel SDK is not running, ignoring shutdown request.")
	}
}

// TODO remove dummy metric!
func (s *OTelSdkStarter) InitializeSelfMonitoringMetrics(
	meter otelmetric.Meter,
	metricNamePrefix string,
	logger *logr.Logger,
) {
	metricName := fmt.Sprintf("%s%s", metricNamePrefix, "dummy.heartbeat")
	var dummyHeartbeat otelmetric.Int64Gauge
	var err error
	if dummyHeartbeat, err = meter.Int64Gauge(
		metricName,
		otelmetric.WithUnit("1"),
		otelmetric.WithDescription("Dummy metric"),
	); err != nil {
		logger.Error(err, fmt.Sprintf("Cannot initialize the metric %s.", metricName))
	}

	go func() {
		for {
			dummyHeartbeat.Record(context.Background(), 1)
			time.Sleep(1 * time.Second)
		}
	}()
}
