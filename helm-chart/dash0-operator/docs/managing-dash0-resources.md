# Managing Dash0 Resources

This guide covers managing Dash0 dashboards, check rules, synthetic checks, views, notification channels, spam filters, and signal-to-metrics via infrastructure-as-code.

## Managing Dash0 Configurations

The Dash0 operator offers capabilities to manage certain Dash0 configurations as infrastructure as code, by deploying
them as Kubernetes resources to a cluster and letting the operator synchronize them to the Dash0 API.

### Managing Dash0 Dashboards

You can manage your Dash0 dashboards via the Dash0 operator.

**Pre-requisites for this feature:**

* A Dash0 operator configuration resource has to be installed in the cluster.
* The operator configuration resource must have the `apiEndpoint` property.
* The operator configuration resource must have at least one Dash0 export configured with authorization (either `token`
  or `secret-ref`).
* The operator will only pick up Perses dashboard resources in namespaces that have a Dash0 monitoring resource
  deployed.
* The operator will not synchronize Perses dashboard resources in namespaces where the Dash0 monitoring resource has the
  setting [`synchronizePersesDashboards`](configuration.md#monitoringresource.spec.synchronizePersesDashboards) set to `false`.
  (This setting is optional and defaults to `true` when omitted.)
* Optional: In addition to the global/default API endpoint and authorization described above, it is possible to define
  namespace-specific overrides by providing one or more Dash0 export(s) with an API endpoint and token in the Dash0
  monitoring resource.

The custom resource definition for Perses dashboards needs to be installed in the cluster.
There are two ways to achieve this:

1. Install the Perses dashboard custom resource definition with the following command:
   ```console
   kubectl apply --server-side -f https://raw.githubusercontent.com/perses/perses-operator/refs/tags/v0.4.0/config/crd/bases/perses.dev_persesdashboards.yaml
   ```
2. Alternatively, install the full Perses operator: Go to <https://github.com/perses/perses-operator> and follow the
   installation instructions there.

With the prerequisites in place, you can manage Dash0 dashboards via the operator.
The Dash0 operator will watch for Perses dashboard resources in all namespaces that have a Dash0 monitoring resource
deployed, and synchronize the Perses dashboard resources with the Dash0 backend:

* When a new Perses dashboard resource is created, the operator will create a corresponding dashboard via Dash0's API.
* When a Perses dashboard resource is changed, the operator will update the corresponding dashboard via Dash0's API.
* When a Perses dashboard resource is deleted, the operator will delete the corresponding dashboard via Dash0's API.

The dashboards created by the operator will be in read-only mode in Dash0.

If the Dash0 operator configuration resource has the `dataset` property set, the operator will create the dashboards in
that dataset, otherwise they will be created in the `default` dataset.

You can opt out of synchronization for individual Perses dashboard resources by adding the Kubernetes label
`dash0.com/enable: false` to the Perses dashboard resource.
If this label is added to a dashboard which has previously been synchronized to Dash0, the operator will delete the
corresponding dashboard in Dash0.
Note that the `spec.instrumentWorkloads.labelSelector` in the monitoring resource does not affect the synchronization of
Perses dashboards, the label to opt out of synchronization is always `dash0.com/enable: false`, even if a non-default
label selector has been set in `spec.instrumentWorkloads.labelSelector`.

When a Perses dashboard resource has been synchronized to Dash0, the operator will write a summary of that
synchronization operation to the status of the Dash0 monitoring resource in the same namespace.
This summary will also show whether the dashboard had any validation issues or an error occurred during synchronization:

```yaml
Kind: Dash0Monitoring
...
Status:
  Perses Dashboard Synchronization Results:
    my-namespace/perses-dashboard-test:
      Synchronization Status:     successful
      Synchronized At:            2024-10-25T12:02:12Z
```

> **Note:** If you only want to manage dashboards, check rules, synthetic checks, views, notification channels, spam
> filters and signal-to-metrics rules via the Dash0 operator, and you do not want it to collect telemetry, you can set
> `telemetryCollection.enabled` to `false` in the Dash0 operator configuration resource.
> This will disable the telemetry collection by the operator, and it will also instruct the operator to not deploy the
> OpenTelemetry collector in your cluster.

#### Conversion Webhook for the PersesDashboard CRD

Starting with version v0.3.0, the `persesdashboards.perses.dev` CRD manifest provides two CRD versions, `v1alpha1` and `v1alpha2`,
with `v1alpha2` as the storage version.
Converting between them requires a conversion webhook.
If you install only the standalone CRD without the Perses operator (the first option above), no conversion webhook is present in
the cluster.
The Kubernetes API server would silently prune fields when a `v1alpha1` resource is deployed, resulting in an empty dashboard.

To prevent this, the Dash0 operator can host the conversion webhook itself.
When the Dash0 operator discovers the PersesDashboard CRD, it will check if a conversion webhook is already configured.
If it is, the Dash0 operator will assume that the full Perses operator including its conversion webhook has been deployed,
and that no additional conversion webhook is required.
If there is no conversion webhook configured, the Dash0 operator patches the CRD's `spec.conversion` stanza to point at its own
webhook endpoint.
This can be disabled by setting the Helm value `operator.iac.persesDashboard.autoPatchConversionWebhook` to `false` (the default
is `true`).
If you manage the PersesDashboard CRD via GitOps (Argo CD, Flux, etc.), configure it to ignore `spec.conversion` in the
`persesdashboards.perses.dev` CRD.
Alternatively, set `operator.iac.persesDashboard.autoPatchConversionWebhook` to `false` and make sure to only deploy resources in
version `v1alpha2`, or add the conversion webhook to the CRD in your GitOps sources:

```
apiVersion: apiextensions.k8s.io/v1
kind: CustomResourceDefinition
metadata:
  name: persesdashboards.perses.dev
  ...
spec:
  conversion:
    strategy: Webhook
    webhook:
      clientConfig:
        service:
          name: dash0-operator-webhook-service-name
          namespace: operator-namespace
          path: /convert-persesdashboard
          port: 443
      conversionReviewVersions:
      - v1
...
```

If you leave `operator.iac.persesDashboard.autoPatchConversionWebhook` enabled and your GitOps tooling removes
`spec.conversion` on every sync, the operator will re-apply the patch on each CRD update event and log a warning, so the
back-and-forth patching between the Dash0 operator and the GitOps system is observable.

### Managing Dash0 Check Rules

You can manage your Dash0 check rules via the Dash0 operator.

**Pre-requisites for this feature:**

* A Dash0 operator configuration resource has to be installed in the cluster.
* The operator configuration resource must have the `apiEndpoint` property.
* The operator configuration resource must have at least one Dash0 export configured with authorization (either `token`
  or `secret-ref`).
* The operator will only pick up Prometheus rule resources in namespaces that have a Dash0 monitoring resource deployed.
* The operator will not synchronize Prometheus rule resources in namespaces where the Dash0 monitoring resource has the
  setting [`synchronizePrometheusRules`](configuration.md#monitoringresource.spec.synchronizePrometheusRules) set to `false`.
  (This setting is optional and defaults to `true` when omitted.)
* Optional: In addition to the global/default API endpoint and authorization described above, it is possible to define
  namespace-specific overrides by providing one or more Dash0 export(s) with an API endpoint and token in the Dash0
  monitoring resource.

The custom resource definition for Prometheus rules also needs to be installed in the cluster.
There are two ways to achieve this:

1. Install the Prometheus rules custom resource definition with the following command:
   ```console
   kubectl apply --server-side -f https://raw.githubusercontent.com/prometheus-operator/prometheus-operator/v0.92.1/example/prometheus-operator-crd/monitoring.coreos.com_prometheusrules.yaml
   ```
2. Alternatively, install the full kube-prometheus stack Helm chart: Go to
   <https://github.com/prometheus-community/helm-charts/tree/main/charts/kube-prometheus-stack> and follow the
   installation instructions there.

With the prerequisites in place, you can manage Dash0 check rules via the operator.
The Dash0 operator will watch for Prometheus rule resources in all namespaces that have a Dash0 monitoring resource
deployed, and synchronize the Prometheus rule resources with the Dash0 backend:

* When a new Prometheus rule resource is created, the operator will create corresponding check rules via Dash0's API.
* When a Prometheus rule resource is changed, the operator will update the corresponding check rules via Dash0's API.
* When a Prometheus rule resource is deleted, the operator will delete the corresponding check rules via Dash0's API.

Note that a Prometheus rule resource can contain multiple groups, and each of those groups can have multiple rules.
The Dash0 operator will create individual check rules for each rule in each group.

If the Dash0 operator configuration resource has the `dataset` property set, the operator will create the rules in that
dataset, otherwise they will be created in the `default` dataset.

#### Prometheus Rule to Dash0 Check Rule Mapping

Prometheus rules will be mapped to Dash0 check rules as follows:

* Each `rules` list item with an `alert` attribute in all `groups` will be converted to an individual check rule in
  Dash0.
* Rules that have the `record` attribute set instead of the `alert` attribute will be ignored.
* The name of the Dash0 check rule will be `"${group_name} - ${alert}"`, where `${group_name}` is the `name` attribute
  of the group in the Prometheus rule resource, and `${alert}` is value of the `alert` attribute.
* The `interval` attribute of the group will be used for the setting "Evaluate every" for each Dash0 check rule in that
  group.
* Other attributes in the Prometheus rule are converted to Dash0 check rule attributes as described in the table below.
* Top level Kubernetes annotations from the PrometheusRule metadata are added to each check rule's annotation map.
  If the same annotation appears in both the PrometheusRule metadata and an individual rule, the annotation value in
  the individual check rule takes priority and overrides the metadata annotation.
  This allows defining common annotations at the PrometheusRule level and selectively override them for specific rules
  as needed.
* Some rule annotations and labels are interpreted by Dash0, these are described in the conversion table below.
  For example, to set the summary of the Dash0 check rule, add an annotation `summary` to the `rules` item in the
  Prometheus rule resource.
* If `expr` contains the token `$__threshold`, and neither annotation `dash0-threshold-degraded` nor
  `dash0-threshold-critical` is present, the rule will be considered invalid and will not be synchronized to Dash0.
* If the rule has the annotation `dash0-enabled=false`, the check rule will be synchronized but disabled in Dash0.
  This Prometheus annotation is not to be confused with the Kubernetes label `dash0.com/enable: false`, which disables
  synchronization of the entire Prometheus rules resource (and all its check rules) to Dash0 (see below).
* The group attribute `limit` is not supported by Dash0 and will be ignored.
* The group attribute `partial_response_strategy` is not supported by Dash0 and will be ignored.
* All labels (except for the ones explicitly mentioned in the conversion table below) will be listed under "Additional
  labels".
* All annotations (except for the ones explicitly mentioned in the conversion table below) will be listed under
  "Annotations".

| Prometheus alerting rule attribute     | Dash0 Check rule field                                                                                            | Notes                                                                                                                                                      |
|----------------------------------------|-------------------------------------------------------------------------------------------------------------------|------------------------------------------------------------------------------------------------------------------------------------------------------------|
| `alert`                                | Name of check rule, prefixed by the group name                                                                    | must be a non-empty string                                                                                                                                 |
| `expr`                                 | "Expression"                                                                                                      | must be a non-empty string                                                                                                                                 |
| `interval` (from group)                | "Evaluate every"                                                                                                  |                                                                                                                                                            |
| `for`                                  | "Grace periods"/"For"                                                                                             | default: "0s"                                                                                                                                              |
| `keep_firing_for`                      | "Grace periods"/"Keep firing for"                                                                                 | default: "0s"                                                                                                                                              |
| `annotations/summary`                  | "Summary"                                                                                                         |                                                                                                                                                            |
| `annotations/description`              | "Description"                                                                                                     |                                                                                                                                                            |
| `annotations/dash0-enabled`            | Denotes whether the check rule is enabled and should be evaluated.                                                | default: "true"; must be either "true" or "false"                                                                                                          |
| `annotations/dash0-threshold-degraded` | Will be used in place of the token `$__threshold` in the expression, to determine whether the check is degraded   | If present, it needs to be a string that can be parsed to a float value according to the syntax described in https://go.dev/ref/spec#Floating-point_literals.   |
| `annotations/dash0-threshold-critical` | Will be used in place of the token `$__threshold` in the expression, to determine whether the check is critical   | If present, it needs to be a string that can be parsed to a float value according to the syntax described in https://go.dev/ref/spec#Floating-point_literals.   |
| `annotations/*`                        | "Annotations"                                                                                                     | |
| `labels/*`                             | "Additional labels"                                                                                               | |

You can opt out of synchronization for individual Prometheus rules resources by adding the Kubernetes label
`dash0.com/enable: false` to it.
If this label is added to a Prometheus rules resource which has previously been synchronized to Dash0, the operator will
delete all corresponding check rules in Dash0.
Note that this refers to a _Kubernetes_ label on the Kubernetes resource, and it will affect all check rules contained
in this Prometheus rules resource.
This mechanism is not to be confused with the Prometheus annotation `dash0-enabled`, which can be applied to
individual rules in a Prometheus rules resource, and controls whether the check rule is enabled or disabled in Dash0.
Please also note that the `spec.instrumentWorkloads.labelSelector` in the monitoring resource does not affect the
synchronization of Prometheus rule resources, the label to opt out of synchronization is always
`dash0.com/enable: false`, even if a non-default label selector has been set in
`spec.instrumentWorkloads.labelSelector`.

When a Prometheus rules resource has been synchronized to Dash0, the operator will write a summary of that
synchronization operation to the status of the Dash0 monitoring resource in the same namespace.
This summary will also show whether any of the rules had validation issues or errors occurred during synchronization:

```yaml
Kind: Dash0Monitoring
...
Status:
  Prometheus Rule Synchronization Results:
    my-namespace/prometheus-example-rules:
      Synchronization Status:        successful
      Synchronized At:               2026-04-30T07:12:41Z
      Alerting Rules Total:          3
      Recording Rules Total:         1
      Invalid Rules Total:           0
      Synchronization Results:
        dash0ApiEndpoint:              https://api.$region.dash0.com/
        dash0Dataset:                  default
        Synchronization Errors Total:  0
        Synchronized Rules Attributes:
          dash0/collector - exporter send failed spans:
            dash0Origin:  dash0-operator_e70c4b03-7e8d-432d-b2cd-addff593076e_default_namespace_prometheus-example-rules_dash0|collector_exporter send failed spans
          dash0/k8s - K8s Deployment replicas mismatch:
            dash0Origin:  dash0-operator_e70c4b03-7e8d-432d-b2cd-addff593076e_default_namespace_prometheus-example-rules_dash0|k8s_K8s Deployment replicas mismatch
          dash0/k8s - K8s pod crash looping:
            dash0Origin:  dash0-operator_e70c4b03-7e8d-432d-b2cd-addff593076e_default_namespace_prometheus-example-rules_dash0|k8s_K8s pod crash looping
          dash0/k8s - job:http_requests:rate5m:
            dash0Origin:           dash0-operator_e70c4b03-7e8d-432d-b2cd-addff593076e_default_namespace_prometheus-example-rules_dash0|k8s_job|http_requests|rate5m
        Synchronized Rules Total:  4
```

> **Note:** If you only want to manage dashboards, check rules, synthetic checks, views, notification channels, spam
> filters and signal-to-metrics rules via the Dash0 operator, and you do not want it to collect telemetry, you can set
> `telemetryCollection.enabled` to `false` in the Dash0 operator configuration resource.
> This will disable the telemetry collection by the operator, and it will also instruct the operator to not deploy the
> OpenTelemetry collector in your cluster.

### Managing Dash0 Synthetic Checks

You can manage your Dash0 synthetic checks via the Dash0 operator.

**Pre-requisites for this feature:**

* A Dash0 operator configuration resource has to be installed in the cluster.
* The operator configuration resource must have the `apiEndpoint` property.
* The operator configuration resource must have at least one Dash0 export configured with authorization (either `token`
  or `secret-ref`).
* The operator will only pick up synthetic check resources in namespaces that have a Dash0 monitoring resource
  deployed.
* Optional: In addition to the global/default API endpoint and authorization described above, it is possible to define
  namespace-specific overrides by providing one or more Dash0 export(s) with an API endpoint and token in the Dash0
  monitoring resource.

With the prerequisites in place, you can manage Dash0 synthetic checks via the operator.
The Dash0 operator will watch for synthetic check resources in all namespaces that have a Dash0 monitoring resource
deployed, and synchronize the synthetic check resources with the Dash0 backend:

* When a new synthetic check resource is created, the operator will create a corresponding synthetic check via Dash0's
  API.
* When a synthetic check resource is changed, the operator will update the corresponding synthetic check via Dash0's
  API.
* When a synthetic check resource is deleted, the operator will delete the corresponding synthetic check via Dash0's
  API.

The synthetic checks created by the operator will be in read-only mode in Dash0.

The custom resource definition for Dash0 synthetic checks can be found
[here](https://github.com/dash0hq/dash0-operator/blob/main/helm-chart/dash0-operator/templates/operator/custom-resource-definition-synthetic-checks.yaml).
An easy way to get started is to create a synthetic check in the Dash0 UI and then download the YAML representation of
that check with the button in the upper right corner.
The downloaded YAML can then be deployed as the manifest of a synthethic check in your Kubernetes cluster.
Once the check is managed via the operator, you might want to delete the synthetic check that has been created in the
Dash0 UI directly in the first step -- otherwise it would show up as a duplicate in the Dash0 UI, i.e. two synthetic
checks with the same name but different internal IDs.

If the Dash0 operator configuration resource has the `dataset` property set, the operator will create the synthetic
checks in that dataset, otherwise they will be created in the `default` dataset.

You can opt out of synchronization for individual synthetic check resources by adding the Kubernetes label
`dash0.com/enable: false` to the synthetic check resource.
If this label is added to a synthetic check which has previously been synchronized to Dash0, the operator will delete
the corresponding synthetic check in Dash0.
Note that the `spec.instrumentWorkloads.labelSelector` in the monitoring resource does not affect the synchronization of
synthetic checks, the label to opt out of synchronization is always `dash0.com/enable: false`, even if a non-default
label selector has been set in `spec.instrumentWorkloads.labelSelector`.

When a synthetic check resource has been synchronized to Dash0, the operator will write a summary of that
synchronization operation to its status.
Note that in contrast to synchronizing Prometheus rules or Perses dashboards (which are third-party custom resources
from the perspective of the operator, i.e. they are potentially owned and managed by another Kubernetes operator), the
result of the synchronization operation will not be written to the status of the Dash0 monitoring resource in the same
namespace, but to the synthetic check resource status directly.
The status will also show whether the synthetic check had any validation issues or an error occurred during
synchronization.

```yaml
Kind: Dash0SyntheticCheck
...
Status:
  Synchronization Status: successful
  Synchronized At:        2025-09-05T11:47:56Z
```

> **Note:** If you only want to manage dashboards, check rules, synthetic checks, views, notification channels, spam
> filters and signal-to-metrics rules via the Dash0 operator, and you do not want it to collect telemetry, you can set
> `telemetryCollection.enabled` to `false` in the Dash0 operator configuration resource.
> This will disable the telemetry collection by the operator, and it will also instruct the operator to not deploy the
> OpenTelemetry collector in your cluster.

### Managing Dash0 Views

You can manage your Dash0 views via the Dash0 operator.

**Pre-requisites for this feature:**

* A Dash0 operator configuration resource has to be installed in the cluster.
* The operator configuration resource must have the `apiEndpoint` property.
* The operator configuration resource must have at least one Dash0 export configured with authorization (either `token`
  or `secret-ref`).
* The operator will only pick up Dash0 view resources in namespaces that have a Dash0 monitoring resource
  deployed.
* Optional: In addition to the global/default API endpoint and authorization described above, it is possible to define
  namespace-specific overrides by providing one or more Dash0 export(s) with an API endpoint and token in the Dash0
  monitoring resource.

With the prerequisites in place, you can manage Dash0 views via the operator.
The Dash0 operator will watch for view resources in all namespaces that have a Dash0 monitoring resource deployed, and
synchronize the view resources with the Dash0 backend:

* When a new view resource is created, the operator will create a corresponding view via Dash0's API.
* When a view resource is changed, the operator will update the corresponding view via Dash0's API.
* When a view resource is deleted, the operator will delete the corresponding view via Dash0's API.

The views created by the operator will be in read-only mode in Dash0.

The custom resource definition for Dash0 views can be found
[here](https://github.com/dash0hq/dash0-operator/blob/main/helm-chart/dash0-operator/templates/operator/custom-resource-definition-views.yaml).
An easy way to get started is to create a view in the Dash0 UI and then download the YAML representation of that view by
using the "Download → YAML" action from the context menu of the view.
The downloaded YAML can then be deployed as the manifest of a view in your Kubernetes cluster.
Once the view is managed via the operator, you might want to delete the view that has been created in the Dash0 UI
directly in the first step -- otherwise it would show up as a duplicate in the Dash0 UI, i.e. two views with the same
name but different internal IDs.

If the Dash0 operator configuration resource has the `dataset` property set, the operator will create the view in that
dataset, otherwise they will be created in the `default` dataset.

You can opt out of synchronization for individual view resources by adding the Kubernetes label
`dash0.com/enable: false` to the view resource.
If this label is added to a view which has previously been synchronized to Dash0, the operator will delete the
corresponding view in Dash0.
Note that the `spec.instrumentWorkloads.labelSelector` in the monitoring resource does not affect the synchronization of
views, the label to opt out of synchronization is always `dash0.com/enable: false`, even if a non-default label selector
has been set in `spec.instrumentWorkloads.labelSelector`.

When a view resource has been synchronized to Dash0, the operator will write a summary of that synchronization operation
to its status.
Note that in contrast to synchronizing Prometheus rules or Perses dashboards (which are third-party custom resources
from the perspective of the operator, i.e. they are potentially owned and managed by another Kubernetes operator), the
result of the synchronization operation will not be written to the status of the Dash0 monitoring resource in the same
namespace, but to the view resource status directly.
The status will also show whether the view had any validation issues or an error occurred during synchronization.

```yaml
Kind: Dash0View
...
Status:
  Synchronization Status: successful
  Synchronized At:        2025-09-05T11:47:56Z
```

> **Note:** If you only want to manage dashboards, check rules, synthetic checks, views, notification channels, spam
> filters and signal-to-metrics rules via the Dash0 operator, and you do not want it to collect telemetry, you can set
> `telemetryCollection.enabled` to `false` in the Dash0 operator configuration resource.
> This will disable the telemetry collection by the operator, and it will also instruct the operator to not deploy the
> OpenTelemetry collector in your cluster.

### Managing Dash0 Notification Channels

You can manage your Dash0 notification channels via the Dash0 operator.

Pre-requisites for this feature:
* A Dash0 operator configuration resource has to be installed in the cluster.
* The operator configuration resource must have the `apiEndpoint` property.
* The operator configuration resource must have at least one Dash0 export configured with authorization
  (either `token` or `secret-ref`).
* The operator will only pick up notification channel resources in namespaces that have a Dash0 monitoring resource
  deployed.
* Optional: In addition to the global/default API endpoint and authorization described above, it is possible to define
  namespace-specific overrides by providing one or more Dash0 export(s) with an API endpoint and token in the Dash0
  monitoring resource.

With the prerequisites in place, you can manage Dash0 notification channels via the operator.
The Dash0 operator will watch for notification channel resources in all namespaces that have a Dash0 monitoring resource
deployed, and synchronize the notification channel resources with the Dash0 backend:
* When a new notification channel resource is created, the operator will create a corresponding notification channel via
  Dash0's API.
* When a notification channel resource is changed, the operator will update the corresponding notification channel via
  Dash0's API.
* When a notification channel resource is deleted, the operator will delete the corresponding notification channel via
  Dash0's API.

Notification channels are organization-level resources. Unlike synthetic checks or views, they are not scoped to a
dataset.

The custom resource definition for Dash0 notification channels can be found
[here](https://github.com/dash0hq/dash0-operator/blob/main/helm-chart/dash0-operator/templates/operator/custom-resource-definition-notification-channels.yaml).

You can opt out of synchronization for individual notification channel resources by adding the Kubernetes label
`dash0.com/enable: false` to the notification channel resource.
If this label is added to a notification channel which has previously been synchronized to Dash0, the operator will
delete the corresponding notification channel in Dash0.

When a notification channel resource has been synchronized to Dash0, the operator will write a summary of that
synchronization operation to its status.
The result of the synchronization operation will be written directly to the notification channel resource status (not to
the Dash0 monitoring resource).
The status will also show whether any error occurred during synchronization.

```yaml
Kind: Dash0NotificationChannel
...
Status:
  Synchronization Status: successful
  Synchronized At:        2025-09-05T11:47:56Z
```

#### Supported Notification Channel Types

The `type` field in the spec determines which type-specific config field must be set.
Exactly one config field must be provided, matching the `type`.

##### Slack Webhook

Sends notifications to a Slack channel via an [incoming webhook](https://api.slack.com/messaging/webhooks).
This is the simplest Slack integration and requires no OAuth flow.

```yaml
apiVersion: operator.dash0.com/v1beta1
kind: Dash0NotificationChannel
metadata:
  name: slack-alerts
spec:
  display:
    name: Slack Alerts
  type: slack
  slackConfig:
    webhookURL: "https://hooks.slack.com/services/T00000000/B00000000/XXXXXXXXXXXXXXXXXXXXXXXX"
    channel: "#alerts"
```

##### Slack Bot

Sends notifications using the Dash0 Slack Bot, which is installed into a Slack workspace via OAuth.
The Slack Bot integration supports richer formatting and centralized channel management compared to the simpler
Slack Webhook integration.

**Prerequisites:**
* You must have **admin permissions** on the target Slack workspace.
* You need access to the **Dash0 UI** to initiate the OAuth authorization flow.

**Step 1: Install the Dash0 Slack App.**
In the Dash0 UI, navigate to **Settings > Notification Channels**, click **Add Notification Channel** and select
**Slack Bot**. Click **Authorize** to start the OAuth flow. You will be redirected to Slack to authorize the Dash0 bot
for your workspace. After authorization, Slack grants Dash0 a bot token scoped to your workspace. This is a
**one-time operation per workspace** -- once authorized, you can create multiple notification channels against different
Slack channels in that workspace without repeating the OAuth flow. Note the **Team ID** displayed after authorization
(e.g. `T012345`). You will need it for the `teamId` field.

**Step 2: Invite the bot to target channels.**
The Dash0 bot must be explicitly added to each Slack channel it will post to.
In Slack, open the target channel and run `/invite @Dash0`. Repeat this for every channel you want to receive
notifications in.

**Step 3: Create the notification channel.**
Once the bot is installed and invited to the target channel, create a `Dash0NotificationChannel` resource:

```yaml
apiVersion: operator.dash0.com/v1beta1
kind: Dash0NotificationChannel
metadata:
  name: slack-bot-alerts
spec:
  display:
    name: Slack Bot Alerts
  type: slack_bot
  slackBotConfig:
    teamId: "T012345"
    channel: "#alerts"
```

You can also create notification channels programmatically using the
[Dash0 Go client library](https://github.com/dash0hq/dash0-api-client-go):

```go
var config dash0.NotificationChannelSpec_Config
config.FromSlackBotConfig(dash0.SlackBotConfig{
    Channel: "#alerts",
    TeamId:  "T012345",
})
```

**Multiple channels:** A single OAuth installation supports creating multiple notification channels across different
Slack channels in the same workspace. Create additional `Dash0NotificationChannel` resources with the same `teamId` but
different `channel` values:

```yaml
# Channel 1: production alerts
apiVersion: operator.dash0.com/v1beta1
kind: Dash0NotificationChannel
metadata:
  name: slack-bot-prod
spec:
  display:
    name: Production Alerts
  type: slack_bot
  slackBotConfig:
    teamId: "T012345"
    channel: "#prod-alerts"
---
# Channel 2: staging alerts
apiVersion: operator.dash0.com/v1beta1
kind: Dash0NotificationChannel
metadata:
  name: slack-bot-staging
spec:
  display:
    name: Staging Alerts
  type: slack_bot
  slackBotConfig:
    teamId: "T012345"
    channel: "#staging-alerts"
```

##### Email

Sends notifications to a list of email recipients.

```yaml
apiVersion: operator.dash0.com/v1beta1
kind: Dash0NotificationChannel
metadata:
  name: email-alerts
spec:
  display:
    name: Email Alerts
  type: email_v2
  emailV2Config:
    recipients:
      - oncall@example.com
      - teamlead@example.com
    plaintext: false
```

##### Generic Webhook

Sends notifications to an arbitrary HTTP endpoint.

```yaml
apiVersion: operator.dash0.com/v1beta1
kind: Dash0NotificationChannel
metadata:
  name: webhook-alerts
spec:
  display:
    name: Webhook Alerts
  type: webhook
  webhookConfig:
    url: "https://example.com/webhook"
    headers:
      X-Custom-Header: "my-value"
    followRedirects: false
    allowInsecure: false
```

##### Incident.io

```yaml
apiVersion: operator.dash0.com/v1beta1
kind: Dash0NotificationChannel
metadata:
  name: incidentio-alerts
spec:
  display:
    name: Incident.io Alerts
  type: incidentio
  incidentioConfig:
    url: "https://api.incident.io/v2/alert_events/http/my-source"
    headers: "Bearer my-api-token"
```

##### OpsGenie

```yaml
apiVersion: operator.dash0.com/v1beta1
kind: Dash0NotificationChannel
metadata:
  name: opsgenie-alerts
spec:
  display:
    name: OpsGenie Alerts
  type: opsgenie
  opsgenieConfig:
    instance: eu
    apiKey: "my-opsgenie-api-key"
```

##### PagerDuty

```yaml
apiVersion: operator.dash0.com/v1beta1
kind: Dash0NotificationChannel
metadata:
  name: pagerduty-alerts
spec:
  display:
    name: PagerDuty Alerts
  type: pagerduty
  pagerdutyConfig:
    key: "my-integration-key"
    url: "https://events.pagerduty.com/v2/enqueue"
```

##### Microsoft Teams Webhook

```yaml
apiVersion: operator.dash0.com/v1beta1
kind: Dash0NotificationChannel
metadata:
  name: teams-alerts
spec:
  display:
    name: Teams Alerts
  type: teams_webhook
  teamsWebhookConfig:
    url: "https://outlook.office.com/webhook/..."
```

##### Discord Webhook

```yaml
apiVersion: operator.dash0.com/v1beta1
kind: Dash0NotificationChannel
metadata:
  name: discord-alerts
spec:
  display:
    name: Discord Alerts
  type: discord_webhook
  discordWebhookConfig:
    url: "https://discord.com/api/webhooks/..."
```

##### Google Chat Webhook

```yaml
apiVersion: operator.dash0.com/v1beta1
kind: Dash0NotificationChannel
metadata:
  name: google-chat-alerts
spec:
  display:
    name: Google Chat Alerts
  type: google_chat_webhook
  googleChatWebhookConfig:
    url: "https://chat.googleapis.com/v1/spaces/.../messages?key=..."
```

##### iLert

```yaml
apiVersion: operator.dash0.com/v1beta1
kind: Dash0NotificationChannel
metadata:
  name: ilert-alerts
spec:
  display:
    name: iLert Alerts
  type: ilert
  ilertConfig:
    url: "https://api.ilert.com/api/events/my-integration-key"
```

##### All Quiet

```yaml
apiVersion: operator.dash0.com/v1beta1
kind: Dash0NotificationChannel
metadata:
  name: allquiet-alerts
spec:
  display:
    name: All Quiet Alerts
  type: all_quiet
  allQuietConfig:
    url: "https://allquiet.app/api/webhook/my-inbound-integration-id"
```

#### Optional Fields

**`frequency`**: Controls the notification frequency (e.g. `10m`, `5m`, `1h`). Defaults to `10m` if omitted.

**`routing`**: Defines which assets and filters determine when this channel is notified.

`filters` is a list of filter groups. The conditions within a single group (the inner list) are combined with
**AND**, and the groups (the outer list) are combined with **OR**. This two-level structure is what allows
expressing rules like "(A and B) or (C and D)", which a flat list could not.

```yaml
spec:
  # ...type and config fields...
  frequency: 5m
  routing:
    assets:
      - kind: check_rule
        id: "rule-id"
        name: "My Check Rule"
        dataset: "default"
    filters:
      # Group 1: service.name is "checkout" AND severity is "error" or "critical"
      - - key: service.name
          operator: is
          value: checkout
        - key: severity
          operator: is_one_of
          values:
            - error
            - critical
      # Group 2: service.name is "payments" AND environment is "production"
      - - key: service.name
          operator: is
          value: payments
        - key: environment
          operator: is
          value: production
```

With the example above, the channel is notified when
`(service.name = checkout AND severity in [error, critical]) OR (service.name = payments AND environment = production)`.

### Managing Spam Filters

You can manage your [spam filters](https://www.dash0.com/docs/dash0/cost-control/spam-filters) via the Dash0 operator.
Spam filters allow you to drop unwanted telemetry data (logs, spans, or metrics) **in the Dash0 ingestion pipeline** based on attribute conditions before it
is stored in Dash0.

**Note:** For filtering telemetry before it leaves your cluster, use
[filter](configuration.md#monitoringresource.spec.filter) rules in your the monitoring resources.

Pre-requisites for this feature:
* A Dash0 operator configuration resource has to be installed in the cluster.
* The operator configuration resource must have the `apiEndpoint` property.
* The operator configuration resource must have at least one Dash0 export configured with authorization
  (either `token` or `secret-ref`).
* The operator will only pick up spam filter resources in namespaces that have a Dash0 monitoring resource
  deployed.
* Optional: In addition to the global/default API endpoint and authorization described above, it is possible to define
  namespace-specific overrides by providing one or more Dash0 export(s) with an API endpoint and token in the Dash0
  monitoring resource.

With the prerequisites in place, you can manage Dash0 spam filters via the operator.
The Dash0 operator will watch for spam filter resources in all namespaces that have a Dash0 monitoring resource
deployed, and synchronize the spam filter resources with the Dash0 backend:
* When a new spam filter resource is created, the operator will create a corresponding spam filter via Dash0's API.
* When a spam filter resource is changed, the operator will update the corresponding spam filter via Dash0's API.
* When a spam filter resource is deleted, the operator will delete the corresponding spam filter via Dash0's API.

The custom resource definition for Dash0 spam filters can be found
[here](https://github.com/dash0hq/dash0-operator/blob/main/helm-chart/dash0-operator/templates/operator/custom-resource-definition-spam-filters.yaml).

Here is an example of a spam filter that drops all log records from the `kube-system` namespace:

```yaml
apiVersion: operator.dash0.com/v1alpha1
kind: Dash0SpamFilter
metadata:
  name: drop-health-checks
spec:
  contexts:
    - log
  filter:
    - key: "k8s.namespace.name"
      operator: "is"
      value: "kube-system"
```

The `contexts` field specifies which signal types the filter applies to (e.g. `log`, `span`, `metric` or `datapoint`).
The `filter` field contains one or more conditions. Each condition matches against an attribute `key` with an `operator`
(e.g. `is`, `is_not`, `contains`, `starts_with`) and a `value` to compare against.

If the Dash0 operator configuration resource has the `dataset` property set, the operator will create the spam filter in
that dataset, otherwise they will be created in the `default` dataset.

You can opt out of synchronization for individual spam filter resources by adding the Kubernetes label
`dash0.com/enable: false` to the spam filter resource.
If this label is added to a spam filter which has previously been synchronized to Dash0, the operator will delete the
corresponding spam filter in Dash0.
Note that the `spec.instrumentWorkloads.labelSelector` in the monitoring resource does not affect the synchronization of
spam filters, the label to opt out of synchronization is always `dash0.com/enable: false`, even if a non-default label
selector has been set in `spec.instrumentWorkloads.labelSelector`.

When a spam filter resource has been synchronized to Dash0, the operator will write a summary of that synchronization
operation to its status.
The result of the synchronization operation will be written directly to the spam filter resource status (not to the
Dash0 monitoring resource).
The status will also show whether the spam filter had any validation issues or an error occurred during synchronization.

```yaml
Kind: Dash0SpamFilter
...
Status:
  Synchronization Status: successful
  Synchronized At:        2026-05-01T12:00:00Z
```

### Managing Dash0 Signal-to-Metrics

You can manage your Dash0 signal-to-metrics rules via the Dash0 operator.
Signal-to-metrics rules derive custom metrics from spans or log records based on user-defined filter criteria. Span
matches produce exponential histograms (latency distributions); log record matches produce monotonic counters.

Pre-requisites for this feature:
* A Dash0 operator configuration resource has to be installed in the cluster.
* The operator configuration resource must have the `apiEndpoint` property.
* The operator configuration resource must have at least one Dash0 export configured with authorization
  (either `token` or `secret-ref`).
* The operator will only pick up signal-to-metrics resources in namespaces that have a Dash0 monitoring resource
  deployed.
* Optional: In addition to the global/default API endpoint and authorization described above, it is possible to define
  namespace-specific overrides by providing one or more Dash0 export(s) with an API endpoint and token in the Dash0
  monitoring resource.

With the prerequisites in place, you can manage Dash0 signal-to-metrics rules via the operator.
The Dash0 operator will watch for signal-to-metrics resources in all namespaces that have a Dash0 monitoring resource
deployed, and synchronize the signal-to-metrics resources with the Dash0 backend:
* When a new signal-to-metrics resource is created, the operator will create a corresponding rule via Dash0's API.
* When a signal-to-metrics resource is changed, the operator will update the corresponding rule via Dash0's API.
* When a signal-to-metrics resource is deleted, the operator will delete the corresponding rule via Dash0's API.

The custom resource definition for Dash0 signal-to-metrics rules can be found
[here](https://github.com/dash0hq/dash0-operator/blob/main/helm-chart/dash0-operator/templates/operator/custom-resource-definition-signal-to-metrics.yaml).

Here is an example of a signal-to-metrics rule that derives a latency histogram for requests to a specific HTTP route:

```yaml
apiVersion: operator.dash0.com/v1alpha1
kind: Dash0SignalToMetrics
metadata:
  name: checkout-latency
spec:
  enabled: true
  display:
    name: Checkout Service Latency
  match:
    signal: spans
    filters:
      - key: http.route
        operator: is
        value: /api/v1/checkout
  output:
    name: checkout.request.duration
    description: How long calls to our checkout API take
    interval: 60s
```

You can opt out of synchronization for individual signal-to-metrics resources by adding the Kubernetes label
`dash0.com/enable: false` to the resource.
If this label is added to a signal-to-metrics resource which has previously been synchronized to Dash0, the operator
will delete the corresponding rule in Dash0.

When a signal-to-metrics resource has been synchronized to Dash0, the operator will write a summary of that
synchronization operation to its status.
The result of the synchronization operation will be written directly to the signal-to-metrics resource status (not to
the Dash0 monitoring resource).
The status will also show whether any error occurred during synchronization.

```yaml
Kind: Dash0SignalToMetrics
...
Status:
  Synchronization Status: successful
  Synchronized At:        2026-05-16T12:00:00Z
```

### Infrastructure-as-Code Only Mode (Disable Telemetry Collection)

If you use the Dash0 operator only for infrastructure-as-code purposes, and not to collect telemetry in the cluster,
you can set the Helm flag `operator.telemetryCollectionEnabled=false`.
In this mode, the operator will not deploy any OpenTelemetry collectors to the cluster, and no telemetry will be
collected.

By default, the operator Helm chart will set up all necessary Kubernetes RBAC permissions to manage OpenTelemetry
collectors and the target allocator.
If set to false, these RBAC permissions will not be granted, and the operator will run with a reduced set of RBAC
permissions.

If `operator.telemetryCollectionEnabled=false` is set, it will not be possible to set
[`spec.telemetryCollection.enabled`](configuration.md#operatorconfigurationresource.spec.telemetryCollection.enabled)
to `true` in the [Dash0OperatorConfiguration](configuration.md#configuring-the-dash0-backend-connection), since the required RBAC
permissions have not been granted.
To enable telemetry collection later on, it is required to change this Helm value to `true` and perform a
`helm upgrade --install` with the updated setting.

If the operator is installed with `telemetryCollectionEnabled=true`, and the flag is later flipped to false via
`helm upgrade`, the operator will not remove its managed OpenTelemetry collectors, nor the associated resources
(config maps, cluster roles, roles & bindings, services etc.) from the cluster.
It cannot, because `telemetryCollectionEnabled=false` implies that the necessary RBAC permissions for managing these
resources (including the permission to delete these resources) are not granted to the operator manger.
The recommended resolution is to uninstall the operator from the cluster entirely, then re-install with
`telemetryCollectionEnabled=false`.
