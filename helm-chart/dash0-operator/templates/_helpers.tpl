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

{{/*
Renders the string "true" if the operator manager is supposed to create and update the operator configuration resource
from the values provided via Helm, and the empty string otherwise. Used by the operator manager deployment and by the
post-install hook which waits for that resource, so both cannot drift apart.
*/}}
{{- define "dash0-operator.createOperatorConfigurationResource" -}}
{{- if or .Values.operator.dash0Export.enabled .Values.operator.exports -}}
true
{{- end }}
{{- end }}

{{/*
Renders the string "true" if the operator runs in SCE-only mode (Signal Control enabled, telemetry collection disabled),
and the empty string otherwise. The operator manager derives the same mode from its command line arguments.
*/}}
{{- define "dash0-operator.signalControlSceOnlyMode" -}}
{{- if and .Values.operator.signalControl.enabled (not .Values.operator.telemetryCollectionEnabled) -}}
true
{{- end }}
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

{{/*
The Secret in which the operator manager stores the webhook certificates it manages itself, if cert-manager is not used.
*/}}
{{- define "dash0-operator.webhookCertificatesSecretName" -}}
{{ include "dash0-operator.chartName" . }}-webhook-server-cert
{{- end }}

{{/*
The Secret with the webhook certificates that chart versions before the operator managed the certificates itself have
generated on every render.
*/}}
{{- define "dash0-operator.legacyCertificatesSecretName" -}}
{{ include "dash0-operator.chartName" . }}-certificates
{{- end }}

{{/*
The webhook configurations and CRDs into which the operator manager injects the CA bundle of the webhook certificates
it manages itself, if cert-manager is not used, rendered as a comma-separated list of <kind>/<name>.
Maintenance note: A new webhook configuration (or a CRD with a conversion webhook) needs to be added here, it needs the
label rendered by dash0-operator.caBundleInjectionLabel, its caBundle needs to be rendered via
dash0-operator.liveCaBundle, and it needs to be added to the resourceNames of the patch permissions in the -manager-base
cluster role.
*/}}
{{- define "dash0-operator.caBundleTargets" -}}
{{- $chartName := include "dash0-operator.chartName" . -}}
{{- $targets := list
  (printf "MutatingWebhookConfiguration/%s-injector" $chartName)
  (printf "MutatingWebhookConfiguration/%s-operator-configuration-mutating" $chartName)
  (printf "ValidatingWebhookConfiguration/%s-operator-configuration-validator" $chartName)
  (printf "MutatingWebhookConfiguration/%s-monitoring-mutating" $chartName)
  (printf "ValidatingWebhookConfiguration/%s-monitoring-validator" $chartName)
  "CustomResourceDefinition/dash0monitorings.operator.dash0.com"
-}}
{{- if .Values.operator.signalControl.enabled -}}
{{- $targets = append $targets (printf "ValidatingWebhookConfiguration/%s-signal-control-validator" $chartName) -}}
{{- end -}}
{{- join "," $targets -}}
{{- end }}

{{/*
The label that marks a webhook configuration or CRD as a target for the injection of the CA bundle by the operator
manager. The value is the namespace of the release, so that only the operator manager of this release injects into it.
*/}}
{{- define "dash0-operator.caBundleInjectionLabel" -}}
dash0.com/inject-ca-bundle: {{ .Release.Namespace }}
{{- end }}

{{/*
Renders the current caBundle of a webhook client configuration, read from the live object via lookup, or nothing if
the live object does not exist or cannot be read (helm template, ArgoCD). The operator manager injects the CA bundle;
rendering the live value (or omitting the field) makes sure that an install or upgrade never changes it.
Parameters: kind and name of the object, and the name of the webhook entry (omit for the conversion webhook of a CRD).
*/}}
{{- define "dash0-operator.liveCaBundle" -}}
{{- if eq .kind "CustomResourceDefinition" -}}
{{- $live := lookup "apiextensions.k8s.io/v1" .kind "" .name -}}
{{- with $live -}}
{{- with .spec -}}{{- with .conversion -}}{{- with .webhook -}}{{- with .clientConfig -}}
{{- .caBundle | default "" -}}
{{- end -}}{{- end -}}{{- end -}}{{- end -}}
{{- end -}}
{{- else -}}
{{- $live := lookup "admissionregistration.k8s.io/v1" .kind "" .name -}}
{{- $webhookName := .webhook -}}
{{- with $live -}}
{{- range .webhooks -}}
{{- if eq .name $webhookName -}}
{{- with .clientConfig -}}
{{- .caBundle | default "" -}}
{{- end -}}
{{- end -}}
{{- end -}}
{{- end -}}
{{- end -}}
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

{{/* the synthetics-worker image */}}
{{- define "dash0-operator.syntheticsWorkerImage" -}}
{{- include "dash0-operator.imageRef" (dict "image" .Values.operator.syntheticsWorkerImage "context" .) -}}
{{- end }}

{{- define "dash0-operator.imageRef" -}}
{{- if .image.digest -}}
{{- printf "%s@%s" .image.repository .image.digest }}
{{- else -}}
{{- printf "%s:%s" .image.repository (default .context.Chart.AppVersion .image.tag) }}
{{- end }}
{{- end }}

{{/*
Fails rendering the chart (install or upgrade) if a replica count (.value) is set but is not an integer of at least 1.
Null is allowed, the default for the respective setting applies then.
*/}}
{{- define "dash0-operator.validateReplicas" -}}
{{- if not (kindIs "invalid" .value) }}
{{- if not (regexMatch "^[1-9][0-9]*$" (toString .value)) }}
{{- fail (printf "Error: %s must be an integer of at least 1, but was %v." .name .value) }}
{{- end }}
{{- end }}
{{- end }}

{{/*
Renders the RBAC rules for the agent0-connector's cluster role as yaml:
* the custom rules from operator.agent0Connector.clusterRole.rules when there are any, which replace the default rules,
* the default rules from files/agent0-connector-default-cluster-role-rules.yaml plus the rules from
  operator.agent0Connector.clusterRole.additionalRules when there are any additional rules,
* the default rules otherwise.
Setting both operator.agent0Connector.clusterRole.rules and operator.agent0Connector.clusterRole.additionalRules fails
the installation.

The -manager-agent0-connector-ro cluster role renders the rules through this template, and so does the extra config map
from which the operator reads them, so the permissions the operator grants to the agent0-connector service account and
the read permissions the operator holds itself (which Kubernetes' privilege escalation prevention requires it to hold)
always come from the same list.

The default rules are parsed and re-rendered instead of being passed through verbatim, so that the comments documenting
the rule file stay in the file and out of the rendered manifests.
*/}}
{{- define "dash0-operator.agent0ConnectorClusterRoleRules" -}}
{{- $clusterRole := .Values.operator.agent0Connector.clusterRole }}
{{- if and $clusterRole.rules $clusterRole.additionalRules }}
{{- fail "Error: operator.agent0Connector.clusterRole.rules and operator.agent0Connector.clusterRole.additionalRules are mutually exclusive, only one of them can be set. Use rules to replace the default rules entirely, or additionalRules to add rules to the default rules." }}
{{- end }}
{{- if $clusterRole.rules }}
{{- include "dash0-operator.agent0ConnectorCustomClusterRoleRules" (dict "rules" $clusterRole.rules "valuePath" "operator.agent0Connector.clusterRole.rules") }}
{{- else }}
{{- $defaultRulesFile := "files/agent0-connector-default-cluster-role-rules.yaml" }}
{{- $defaultRulesRaw := .Files.Get $defaultRulesFile }}
{{- $defaultRules := $defaultRulesRaw | fromYamlArray }}
{{- /* fromYamlArray reports a parse error as a single-element list holding the error message, which is truthy, hence
       the additional check that the first element actually is a rule. */}}
{{- if or (not $defaultRulesRaw) (not $defaultRules) (not (kindIs "map" (first $defaultRules))) }}
{{- fail (printf "Error: the default cluster role rules for the agent0-connector could not be read from %s. This is a bug in the Dash0 operator Helm chart, please report it." $defaultRulesFile) }}
{{- end }}
{{- if $clusterRole.additionalRules }}
{{- $additionalRules := include "dash0-operator.agent0ConnectorCustomClusterRoleRules" (dict "rules" $clusterRole.additionalRules "valuePath" "operator.agent0Connector.clusterRole.additionalRules") | fromYamlArray }}
{{- toYaml (concat $defaultRules $additionalRules) }}
{{- else }}
{{- toYaml $defaultRules }}
{{- end }}
{{- end }}
{{- end }}

{{/*
Validates custom RBAC rules for the agent0-connector's cluster role and renders them as yaml. The template expects a
dict with the keys "rules" (the rules to validate, either operator.agent0Connector.clusterRole.rules or
operator.agent0Connector.clusterRole.additionalRules) and "valuePath" (the name of the Helm value the rules come from,
used in error messages). The rules are validated to grant read-only access only: the verbs "get" and "list" for any
resource, plus "create" for the self subject review API. Creating a SelfSubjectAccessReview or a SelfSubjectRulesReview
does not persist an object, the API server evaluates the request and answers it, which is what "kubectl auth can-i"
needs. Any other verb fails the installation.

A rule also has to be a well-formed RBAC policy rule, which is checked here as well, so that a malformed rule fails the
installation instead of being rejected by the API server when the manifest is applied.

The allowed verbs, the self subject review carve-out and the shape checks are duplicated in validateClusterRoleRules
and validateRuleShape in internal/agent0connector/a0cresources/desired_state.go, which apply the same checks to the
rules the operator reads from the extra config map. Both need to be changed together.
*/}}
{{- define "dash0-operator.agent0ConnectorCustomClusterRoleRules" -}}
{{- $selfSubjectReviewApiGroup := "authorization.k8s.io" }}
{{- $selfSubjectReviewResources := list "selfsubjectaccessreviews" "selfsubjectrulesreviews" }}
{{- $rules := .rules }}
{{- $valuePath := .valuePath }}
{{- /* Without this check, a map would pass the validation below (range iterates over its values), and the caller's
       fromYamlArray would turn the rendered map into an error string that ends up in the cluster role as a rule. */}}
{{- if not (kindIs "slice" $rules) }}
{{- fail (printf "Error: %s must be a list of RBAC policy rules." $valuePath) }}
{{- end }}
{{- range $ruleIndex, $rule := $rules }}
{{- $apiGroups := $rule.apiGroups | default (list) }}
{{- $resources := $rule.resources | default (list) }}
{{- $verbs := $rule.verbs | default (list) }}
{{- $nonResourceUrls := $rule.nonResourceURLs | default (list) }}
{{- $resourceNames := $rule.resourceNames | default (list) }}
{{- /* The shape checks the API server's own ValidatePolicyRule applies. Without them a malformed rule renders fine and
       the API server rejects the whole cluster role when the manifest is applied. */}}
{{- if eq (len $verbs) 0 }}
{{- fail (printf "Error: %s[%d]: verbs must contain at least one value." $valuePath $ruleIndex) }}
{{- end }}
{{- if gt (len $nonResourceUrls) 0 }}
{{- if or (gt (len $apiGroups) 0) (gt (len $resources) 0) (gt (len $resourceNames) 0) }}
{{- fail (printf "Error: %s[%d]: a rule cannot apply to both regular resources and non-resource URLs. Use one rule for nonResourceURLs and another one for apiGroups & resources." $valuePath $ruleIndex) }}
{{- end }}
{{- else }}
{{- if eq (len $apiGroups) 0 }}
{{- fail (printf "Error: %s[%d]: a rule for regular resources must supply at least one apiGroup, use \"\" for the core API group." $valuePath $ruleIndex) }}
{{- end }}
{{- if eq (len $resources) 0 }}
{{- fail (printf "Error: %s[%d]: a rule for regular resources must supply at least one resource." $valuePath $ruleIndex) }}
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
{{- fail (printf "Error: %s[%d]: the verb \"%s\" is not allowed. The custom cluster role for the agent0-connector must be read-only: only the verbs \"get\" and \"list\" are allowed, plus \"create\" for the resources \"selfsubjectaccessreviews\" and \"selfsubjectrulesreviews\" in the API group \"authorization.k8s.io\"." $valuePath $ruleIndex $verb) }}
{{- end }}
{{- end }}
{{- end }}
{{- toYaml $rules }}
{{- end }}

{{/*
Validates operator.agent0Connector.allowedKubectlCommands and renders it as yaml. The setting can only restrict the
kubectl commands the agent0-connector supports, hence every key has to be one of these commands, and every value has to
be a boolean. At least one kubectl command has to be enabled, the agent0-connector would terminate on startup otherwise.
The list of supported commands mirrors supportedKubectlCommands in images/agent0-connector/src/kubectl/validation.go,
minus the commands that are rejected unconditionally (like "describe"). Both need to be changed together, a command
that the Helm chart accepts but the agent0-connector does not support makes the agent0-connector terminate on startup.
TestHelmChartListsEverySupportedKubectlCommand in images/agent0-connector/src/kubectl/allowed_commands_test.go checks
that they are in sync.
*/}}
{{- define "dash0-operator.agent0ConnectorAllowedKubectlCommands" -}}
{{- $supportedCommands := list "api-resources" "api-versions" "auth" "cluster-info" "events" "explain" "get" "logs" "top" "version" }}
{{- $allowedKubectlCommands := .Values.operator.agent0Connector.allowedKubectlCommands }}
{{- if not $allowedKubectlCommands }}
{{- fail "Error: operator.agent0Connector.allowedKubectlCommands must not be empty, at least one kubectl command needs to be enabled. If you do not want Agent0 to execute any kubectl command, set operator.agent0Connector.enabled=false instead." }}
{{- end }}
{{- $atLeastOneEnabled := false }}
{{- range $kubectlCommand, $allowed := $allowedKubectlCommands }}
{{- if eq $kubectlCommand "describe" }}
{{- fail "Error: operator.agent0Connector.allowedKubectlCommands.describe: \"kubectl describe\" cannot be enabled, because the agent0-connector cannot redact credentials from its output. Agent0 reads resources via \"kubectl get ... -o yaml\" or \"-o json\" instead." }}
{{- end }}
{{- if not (has $kubectlCommand $supportedCommands) }}
{{- fail (printf "Error: operator.agent0Connector.allowedKubectlCommands.%s: \"%s\" is not a kubectl command supported by the agent0-connector. Supported kubectl commands are: %s." $kubectlCommand $kubectlCommand (join ", " $supportedCommands)) }}
{{- end }}
{{- if not (kindIs "bool" $allowed) }}
{{- fail (printf "Error: operator.agent0Connector.allowedKubectlCommands.%s: the value must be true or false, but it is \"%v\"." $kubectlCommand $allowed) }}
{{- end }}
{{- if $allowed }}
{{- $atLeastOneEnabled = true }}
{{- end }}
{{- end }}
{{- if not $atLeastOneEnabled }}
{{- fail "Error: operator.agent0Connector.allowedKubectlCommands does not enable any kubectl command, at least one kubectl command needs to be set to true. If you do not want Agent0 to execute any kubectl command, set operator.agent0Connector.enabled=false instead." }}
{{- end }}
{{- toYaml $allowedKubectlCommands }}
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
