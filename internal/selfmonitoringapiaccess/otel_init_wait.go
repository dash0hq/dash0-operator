// SPDX-FileCopyrightText: Copyright 2025 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package selfmonitoringapiaccess

import (
	"context"
	"fmt"

	"github.com/go-logr/logr"
	otelmetric "go.opentelemetry.io/otel/metric"
	"sigs.k8s.io/controller-runtime/pkg/log"

	"github.com/dash0hq/dash0-operator/images/pkg/common"
)

type SelfMonitoringClient interface {
	InitializeSelfMonitoringMetrics(otelmetric.Meter, string, *logr.Logger)
}

type OTelSdkStarter struct {
	configChannel chan *common.OTelSdkConfig
	logger        *logr.Logger
}

const (
	meterName = "dash0.operator.manager"
)

var (
	metricNamePrefix = fmt.Sprintf("%s.", meterName)
	meter            otelmetric.Meter
)

func NewOTelSdkStarter() *OTelSdkStarter {
	return &OTelSdkStarter{
		configChannel: make(chan *common.OTelSdkConfig),
	}
}

// 4. Init OTel SDK after restart
//   - This needs to be completly different way of starting the OTel SDK, or rather, it needs a new first step before
//     handing over to common.InitOTelSdk.
//
// 1. Do it in a separate coroutine
// 3. Block until all required parameters are available
// 2. Read parameters (otel endpoint) from synced vars
//   - if started with auto operator configuration stuff, we can use that _right away_ without waiting for the
//     operator reconcile trigger
//   - if not, it just blocks until the operator configuration is reconciled
//
// 4. Start OTel SDK

func (s *OTelSdkStarter) WaitForOTelConfig(
	selfMonitoringClients []SelfMonitoringClient,
) {
	go s.waitForOTelSDKConfiguration(selfMonitoringClients)
}

func (s *OTelSdkStarter) waitForOTelSDKConfiguration(
	selfMonitoringClients []SelfMonitoringClient,
) {
	// TODO this probably needs to run in a loop and react on every change
	// blocks until the config becomes available
	config := <-s.configChannel
	onConfigAvailable(selfMonitoringClients, config)
}

// TODO we also need RemoveConfig
func (s *OTelSdkStarter) UpdateConfig(config *common.OTelSdkConfig) {
	s.configChannel <- config
}

func onConfigAvailable(selfMonitoringClients []SelfMonitoringClient, oTelSdkConfig *common.OTelSdkConfig) {
	ctx := context.Background()
	logger := log.FromContext(ctx)
	logger.Info("XXX OTel SDK configuration available", "config", oTelSdkConfig)
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
