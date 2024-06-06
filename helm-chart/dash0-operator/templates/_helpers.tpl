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
app.kubernetes.io/managed-by: {{ .Release.Service }}
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