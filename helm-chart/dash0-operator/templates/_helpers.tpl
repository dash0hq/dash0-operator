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
{{- default (printf "%s-controller" (include "dash0-operator.chartName" .)) .Values.operator.serviceAccount.name }}
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

{{/* the collector image */}}
{{- define "dash0-operator.collectorImage" -}}
{{- include "dash0-operator.imageRef" (dict "image" .Values.operator.collectorImage "context" .) -}}
{{- end }}

{{/* the config reloader image */}}
{{- define "dash0-operator.configurationReloaderImage" -}}
{{- include "dash0-operator.imageRef" (dict "image" .Values.operator.configurationReloaderImage "context" .) -}}
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