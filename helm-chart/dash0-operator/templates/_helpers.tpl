{{/* chart name without version */}}
{{- define "dash0-operator.chartName" -}}
{{- .Chart.Name | trunc 63 | trimSuffix "-" }}
{{- end }}

{{/* chart name with version */}}
{{- define "dash0-operator.chartNameWithVersion" -}}
{{- printf "%s-%s" (.Chart.Name | trunc 53) .Chart.Version | replace "+" "_" | trunc 63 | trimSuffix "-" }}
{{- end }}

{{/* basic labels */}}
{{- define "dash0-operator.labels" -}}
{{- if .Chart.AppVersion -}}
app.kubernetes.io/version: {{ .Chart.AppVersion | quote }}
{{- end }}
app.kubernetes.io/part-of: {{ include "dash0-operator.chartName" . }}
app.kubernetes.io/managed-by: {{ .Release.Service | quote }}
helm.sh/chart: {{ include "dash0-operator.chartNameWithVersion" . }}
{{- include "dash0-operator.additionalLabels" . }}
{{- end }}

{{- define "dash0-operator.additionalLabels" -}}
{{- if .Values.operator.additionalLabels }}
{{ tpl (.Values.operator.additionalLabels | toYaml) . }}
{{- end }}
{{- end }}

{{- define "dash0-operator.podAnnotations" -}}
{{- if .Values.operator.podAnnotations }}
{{- .Values.operator.podAnnotations | toYaml }}
{{- end }}
{{- end }}

{{- define "dash0-operator.podLabels" -}}
{{- if .Values.operator.podLabels }}
{{- .Values.operator.podLabels | toYaml }}
{{- end }}
{{- end }}

{{/* service account name */}}
{{- define "dash0-operator.serviceAccountName" -}}
{{- default (printf "%s-controller-manager" (include "dash0-operator.chartName" .)) .Values.operator.serviceAccount.name }}
{{- end }}

{{/* the controller manager container image */}}
{{- define "dash0-operator.image" -}}
{{- include "dash0-operator.imageRef" (dict "image" .Values.operator.image "context" .) -}}
{{- end }}

{{- define "dash0-operator.imageTag" -}}
{{- default .Chart.AppVersion .Values.operator.image.tag }}
{{- end }}

{{/* the init container image */}}
{{- define "dash0-operator.initContainerImage" -}}
{{- include "dash0-operator.imageRef" (dict "image" .Values.operator.initContainerImage "context" .) -}}
{{- end }}

{{- define "dash0-operator.initContainerImageTag" -}}
{{- default .Chart.AppVersion .Values.operator.initContainerImage.tag }}
{{- end }}

{{- define "dash0-operator.imageRef" -}}
{{- if .image.digest -}}
{{- printf "%s@%s" .image.repository .image.digest }}
{{- else -}}
{{- printf "%s:%s" .image.repository (default .context.Chart.AppVersion .image.tag) }}
{{- end }}
{{- end }}

{{- define "dash0-operator.restrictiveContainerSecurityContext" -}}
securityContext:
  allowPrivilegeEscalation: false
  readOnlyRootFilesystem: true
  runAsNonRoot: true
  capabilities:
    drop:
    - ALL
{{- end }}

{{- define "dash0-operator.openTelemetryCollectorBaseUrl" -}}
{{- if .Values.operator.openTelemetryCollectorBaseUrl -}}
{{- .Values.operator.openTelemetryCollectorBaseUrl -}}
{{- else if (index .Values "opentelemetry-collector").enabled -}}
{{- /*
Note: This reuses an internal template of the opentelmetry-collector Helm chart, to make sure we render the
exact same service name for the collector URL as the collector Helm chart does. This might need to be updated
if the collector Helm chart changes its service name template. Since we control the version of the collector
Helm chart we use and run rigorous tests when updating to a newer version, this approach should be safe.
*/ -}}
{{- $collectorSubChartValues :=
   dict
	 "Values" (index .Values "opentelemetry-collector")
	 "Chart" (dict "Name" "opentelemetry-collector")
	 "Release" .Release
-}}
http://{{ include "opentelemetry-collector.fullname" $collectorSubChartValues }}.{{ .Release.Namespace }}.svc.cluster.local:4318
{{- else -}}
{{- fail "Error: The value \"opentelemetry-collector.enabled\" is false and the value \"operator.openTelemetryCollectorBaseUrl\" has not been provided. Please refer to the installation instructions at https://github.com/dash0hq/dash0-operator/tree/main/helm-chart/dash0-operator#installation." -}}
{{- end -}}
{{- end -}}