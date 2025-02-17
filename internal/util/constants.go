// SPDX-FileCopyrightText: Copyright 2024 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package util

type WorkloadModifierActor string

const (
	AuthorizationHeaderName      = "Authorization"
	Dash0DatasetHeaderName       = "Dash0-Dataset"
	DatasetDefault               = "default"
	FieldManager                 = "dash0-operator"
	OperatorManagerContainerName = "manager"

	ActorController WorkloadModifierActor = "controller"
	ActorWebhook    WorkloadModifierActor = "webhook"
)
