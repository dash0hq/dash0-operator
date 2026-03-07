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
	MonitoringAutoResourceName            = "dash0-monitoring-auto-resource"
	AutoMonitoringNamespaceLabel          = "dash0.com/auto-monitoring-namespace"

	ActorController WorkloadModifierActor = "controller"
	ActorWebhook    WorkloadModifierActor = "webhook"

	AppKubernetesIoNameLabel      = "app.kubernetes.io/name"
	AppKubernetesIoPartOfLabel    = "app.kubernetes.io/part-of"
	AppKubernetesIoInstanceLabel  = "app.kubernetes.io/instance"
	AppKubernetesIoComponentLabel = "app.kubernetes.io/component"
	AppKubernetesIoManagedByLabel = "app.kubernetes.io/managed-by"
	AppKubernetesIoVersionLabel   = "app.kubernetes.io/version"
	KubernetesIoOs                = "kubernetes.io/os"

	EnvVarDash0NodeIp = "DASH0_NODE_IP"
	EnvVarGoMemLimit  = "GOMEMLIMIT"
)

var (
	RestrictedNamespaces = []string{
		"kube-system",
		"kube-node-lease",
	}
)

func RenderAuthorizationHeader(authToken string) string {
	return fmt.Sprintf("Bearer %s", authToken)
}
