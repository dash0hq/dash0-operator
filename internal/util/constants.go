// SPDX-FileCopyrightText: Copyright 2024 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package util

import "fmt"

type WorkloadModifierActor string

const (
	AuthorizationHeaderName  = "Authorization"
	ContentTypeHeaderName    = "Content-Type"
	AcceptHeaderName         = "Accept"
	ApplicationJsonMediaType = "application/json"
	Dash0DatasetHeaderName   = "Dash0-Dataset"
	DatasetDefault           = "default"
	FieldManager             = "dash0-operator"

	OperatorConfigurationAutoResourceName = "dash0-operator-configuration-auto-resource"

	ActorController WorkloadModifierActor = "controller"
	ActorWebhook    WorkloadModifierActor = "webhook"

	AppKubernetesIoNameLabel      = "app.kubernetes.io/name"
	AppKubernetesIoPartOfLabel    = "app.kubernetes.io/part-of"
	AppKubernetesIoInstanceLabel  = "app.kubernetes.io/instance"
	AppKubernetesIoComponentLabel = "app.kubernetes.io/component"
	AppKubernetesIoManagedByLabel = "app.kubernetes.io/managed-by"
	AppKubernetesIoVersionLabel   = "app.kubernetes.io/version"
)

func RenderAuthorizationHeader(authToken string) string {
	return fmt.Sprintf("Bearer %s", authToken)
}
