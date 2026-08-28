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

{{- define "dash0-operator.preInstallHookServiceAccountName" -}}
{{- printf "%s-pre-install" (include "dash0-operator.chartName" .) }}
{{- end }}

{{- define "dash0-operator.postDeleteHookServiceAccountName" -}}
{{- printf "%s-post-delete" (include "dash0-operator.chartName" .) }}
{{- end }}

{{/* otelcol resources config map name */}}
{{- define "dash0-operator.extraConfigMapName" -}}
{{ include "dash0-operator.chartName" . }}-extra-config
{{- end }}

{{- define "dash0-operator.deploymentName" -}}
{{ include "dash0-operator.chartName" . }}-controller
{{- end }}

{{- define "dash0-operator.webhookServiceName" -}}
{{- default (printf "%s-webhook-service" (include "dash0-operator.chartName" .)) .Values.operator.webhookService.name }}
{{- end }}

{{- define "dash0-operator.webhookServicePort" -}}
{{- default .Values.operator.webhookService.port .Values.operator.webhookPort }}
{{- end }}

{{/* the controller manager container image */}}
{{- define "dash0-operator.image" -}}
{{- include "dash0-operator.imageRef" (dict "image" .Values.operator.image "context" .) -}}
{{- end }}

{{/* the instrumentation image */}}
{{- define "dash0-operator.instrumentationImage" -}}
{{- include "dash0-operator.imageRef" (dict "image" .Values.operator.instrumentationImage "context" .) -}}
{{- end }}

{{/* the collector image */}}
{{- define "dash0-operator.collectorImage" -}}
{{- include "dash0-operator.imageRef" (dict "image" .Values.operator.collectorImage "context" .) -}}
{{- end }}

{{/* the target-allocator image */}}
{{- define "dash0-operator.targetAllocatorImage" -}}
{{- include "dash0-operator.imageRef" (dict "image" .Values.operator.targetAllocatorImage "context" .) -}}
{{- end }}

{{/* the config reloader image */}}
{{- define "dash0-operator.configurationReloaderImage" -}}
{{- include "dash0-operator.imageRef" (dict "image" .Values.operator.configurationReloaderImage "context" .) -}}
{{- end }}

{{/* the filelog offset sync image */}}
{{- define "dash0-operator.filelogOffsetSyncImage" -}}
{{- include "dash0-operator.imageRef" (dict "image" .Values.operator.filelogOffsetSyncImage "context" .) -}}
{{- end }}

{{/* the filelog offset volume ownership image */}}
{{- define "dash0-operator.filelogOffsetVolumeOwnershipImage" -}}
{{- include "dash0-operator.imageRef" (dict "image" .Values.operator.filelogOffsetVolumeOwnershipImage "context" .) -}}
{{- end }}

{{/* the Signal Control collector image */}}
{{- define "dash0-operator.signalControlCollectorImage" -}}
{{- include "dash0-operator.imageRef" (dict "image" .Values.operator.signalControlCollectorImage "context" .) -}}
{{- end }}

{{/* the Edge Proxy image */}}
{{- define "dash0-operator.edgeProxyImage" -}}
{{- include "dash0-operator.imageRef" (dict "image" .Values.operator.edgeProxyImage "context" .) -}}
{{- end }}

{{/* the agent0-connector image */}}
{{- define "dash0-operator.agent0ConnectorImage" -}}
{{- include "dash0-operator.imageRef" (dict "image" .Values.operator.agent0ConnectorImage "context" .) -}}
{{- end }}

{{- define "dash0-operator.imageRef" -}}
{{- if .image.digest -}}
{{- printf "%s@%s" .image.repository .image.digest }}
{{- else -}}
{{- printf "%s:%s" .image.repository (default .context.Chart.AppVersion .image.tag) }}
{{- end }}
{{- end }}

{{/*
Renders the RBAC rules for the agent0-connector's cluster role as yaml: the custom rules from
operator.agent0Connector.clusterRole.rules when there are any, the default rules from
files/agent0-connector-default-cluster-role-rules.yaml otherwise.

The -manager-agent0-connector-ro cluster role renders the rules through this template, and so does the extra config map
from which the operator reads them, so the permissions the operator grants to the agent0-connector service account and
the read permissions the operator holds itself (which Kubernetes' privilege escalation prevention requires it to hold)
always come from the same list.

The default rules are parsed and re-rendered instead of being passed through verbatim, so that the comments documenting
the rule file stay in the file and out of the rendered manifests.
*/}}
{{- define "dash0-operator.agent0ConnectorClusterRoleRules" -}}
{{- if .Values.operator.agent0Connector.clusterRole.rules }}
{{- include "dash0-operator.agent0ConnectorCustomClusterRoleRules" . }}
{{- else }}
{{- $defaultRulesFile := "files/agent0-connector-default-cluster-role-rules.yaml" }}
{{- $defaultRulesRaw := .Files.Get $defaultRulesFile }}
{{- $defaultRules := $defaultRulesRaw | fromYamlArray }}
{{- /* fromYamlArray reports a parse error as a single-element list holding the error message, which is truthy, hence
       the additional check that the first element actually is a rule. */}}
{{- if or (not $defaultRulesRaw) (not $defaultRules) (not (kindIs "map" (first $defaultRules))) }}
{{- fail (printf "Error: the default cluster role rules for the agent0-connector could not be read from %s. This is a bug in the Dash0 operator Helm chart, please report it." $defaultRulesFile) }}
{{- end }}
{{- toYaml $defaultRules }}
{{- end }}
{{- end }}

{{/*
Validates the custom RBAC rules for the agent0-connector's cluster role
(operator.agent0Connector.clusterRole.rules) and renders them as yaml. The custom rules replace the default rules.
They are validated to grant read-only access only: the verbs "get" and "list" for any resource, plus
"create" for the self subject review API. Creating a SelfSubjectAccessReview or a SelfSubjectRulesReview does not
persist an object, the API server evaluates the request and answers it, which is what "kubectl auth can-i" needs. Any
other verb fails the installation.

A rule also has to be a well-formed RBAC policy rule, which is checked here as well, so that a malformed rule fails the
installation instead of being rejected by the API server when the manifest is applied.

The allowed verbs, the self subject review carve-out and the shape checks are duplicated in validateClusterRoleRules
and validateRuleShape in internal/agent0connector/a0cresources/desired_state.go, which apply the same checks to the
rules the operator reads from the extra config map. Both need to be changed together.
*/}}
{{- define "dash0-operator.agent0ConnectorCustomClusterRoleRules" -}}
{{- $selfSubjectReviewApiGroup := "authorization.k8s.io" }}
{{- $selfSubjectReviewResources := list "selfsubjectaccessreviews" "selfsubjectrulesreviews" }}
{{- $rules := .Values.operator.agent0Connector.clusterRole.rules }}
{{- range $ruleIndex, $rule := $rules }}
{{- $apiGroups := $rule.apiGroups | default (list) }}
{{- $resources := $rule.resources | default (list) }}
{{- $verbs := $rule.verbs | default (list) }}
{{- $nonResourceUrls := $rule.nonResourceURLs | default (list) }}
{{- $resourceNames := $rule.resourceNames | default (list) }}
{{- /* The shape checks the API server's own ValidatePolicyRule applies. Without them a malformed rule renders fine and
       the API server rejects the whole cluster role when the manifest is applied. */}}
{{- if eq (len $verbs) 0 }}
{{- fail (printf "Error: operator.agent0Connector.clusterRole.rules[%d]: verbs must contain at least one value." $ruleIndex) }}
{{- end }}
{{- if gt (len $nonResourceUrls) 0 }}
{{- if or (gt (len $apiGroups) 0) (gt (len $resources) 0) (gt (len $resourceNames) 0) }}
{{- fail (printf "Error: operator.agent0Connector.clusterRole.rules[%d]: a rule cannot apply to both regular resources and non-resource URLs. Use one rule for nonResourceURLs and another one for apiGroups & resources." $ruleIndex) }}
{{- end }}
{{- else }}
{{- if eq (len $apiGroups) 0 }}
{{- fail (printf "Error: operator.agent0Connector.clusterRole.rules[%d]: a rule for regular resources must supply at least one apiGroup, use \"\" for the core API group." $ruleIndex) }}
{{- end }}
{{- if eq (len $resources) 0 }}
{{- fail (printf "Error: operator.agent0Connector.clusterRole.rules[%d]: a rule for regular resources must supply at least one resource." $ruleIndex) }}
{{- end }}
{{- end }}
{{- /* A rule may only grant "create" if it addresses nothing but the self subject review API; otherwise it could
       smuggle in a "create" for another resource type. */}}
{{- $selfSubjectReviewsOnly := and (gt (len $apiGroups) 0) (gt (len $resources) 0) (eq (len $nonResourceUrls) 0) }}
{{- range $apiGroup := $apiGroups }}
{{- if ne $apiGroup $selfSubjectReviewApiGroup }}
{{- $selfSubjectReviewsOnly = false }}
{{- end }}
{{- end }}
{{- range $resource := $resources }}
{{- if not (has $resource $selfSubjectReviewResources) }}
{{- $selfSubjectReviewsOnly = false }}
{{- end }}
{{- end }}
{{- $allowedVerbs := list "get" "list" }}
{{- if $selfSubjectReviewsOnly }}
{{- $allowedVerbs = append $allowedVerbs "create" }}
{{- end }}
{{- range $verb := $verbs }}
{{- if not (has $verb $allowedVerbs) }}
{{- fail (printf "Error: operator.agent0Connector.clusterRole.rules[%d]: the verb \"%s\" is not allowed. The custom cluster role for the agent0-connector must be read-only: only the verbs \"get\" and \"list\" are allowed, plus \"create\" for the resources \"selfsubjectaccessreviews\" and \"selfsubjectrulesreviews\" in the API group \"authorization.k8s.io\"." $ruleIndex $verb) }}
{{- end }}
{{- end }}
{{- end }}
{{- toYaml $rules }}
{{- end }}

{{- define "dash0-operator.restrictiveContainerSecurityContext" -}}
securityContext:
  allowPrivilegeEscalation: false
  readOnlyRootFilesystem: true
  runAsNonRoot: true
{{- if .userId }}
  runAsUser: {{ .userId }}
{{- end }}
  capabilities:
    drop:
    - ALL
  seccompProfile:
    type: RuntimeDefault
{{- end }}

{{- define "dash0-operator.restrictivePodSecurityContext" -}}
securityContext:
  runAsNonRoot: true
  seccompProfile:
    type: RuntimeDefault
{{- end }}
