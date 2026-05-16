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
	MonitoringAutoResourceDefaultName     = "dash0-monitoring-auto-resource"
	AutoMonitoredNamespaceLabel           = "dash0.com/auto-monitored-namespace"

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

	TrueString = "true" // makes goconst happy

	// PersesDashboardConversionWebhookPath is the path the operator manager exposes for the perses.dev PersesDashboard CRD
	// conversion webhook. We deliberately do NOT use the standard /convert path served by controller-runtime's typed conversion
	// mux, because that path would route through upstream Perses operator's generated conversion code — which strictly validates
	// variable.kind and rejects Dash0-specific variable types (e.g. Dash0FilterVariable) that legitimately appear in user
	// dashboards.
	//
	// Instead, this webhook performs a minimal JSON-level transformation between v1alpha1 and v1alpha2: the two versions differ only
	// in that v1alpha2 wraps the dashboard spec in a `config:` map. We move keys around without inspecting their contents, so unknown
	// variable kinds pass through untouched.
	PersesDashboardConversionWebhookPath = "/convert-persesdashboard"
)

var (
	RestrictedNamespaces = []string{
		"kube-system",
		"kube-node-lease",
		"kube-public",
	}
)

func RenderAuthorizationHeader(authToken string) string {
	return fmt.Sprintf("Bearer %s", authToken)
}
