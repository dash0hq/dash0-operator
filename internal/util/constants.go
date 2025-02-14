// SPDX-FileCopyrightText: Copyright 2024 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package util

type WorkloadModifierActor string

const (
	AuthorizationHeaderName                 = "Authorization"
	Dash0DatasetHeaderName                  = "Dash0-Dataset"
	DatasetDefault                          = "default"
	SelfMonitoringAndApiAuthTokenEnvVarName = "SELF_MONITORING_AND_API_AUTH_TOKEN"
	FieldManager                            = "dash0-operator"

	ActorController WorkloadModifierActor = "controller"
	ActorWebhook    WorkloadModifierActor = "webhook"
)
