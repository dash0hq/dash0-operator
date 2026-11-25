# Installation via the Operator Lifecycle Manager (OLM)

This guide covers the installation of the Dash0 operator via the
[Operator Lifecycle Manager](https://olm.operatorframework.io/) (OLM) from the catalog that Dash0 publishes.
Installing via OLM is an alternative to the [Helm chart](../../helm-chart/dash0-operator/docs/installation.md).
The two installation methods cannot be combined: a cluster has either a Helm installation or an OLM installation of the
operator, see [Switching from Helm to OLM](#switching-from-helm-to-olm).

## Table of Contents

- [Prerequisites](#prerequisites)
- [Installation](#installation)
  - [Creating the Operator Namespace](#creating-the-operator-namespace)
  - [Adding the Dash0 Catalog](#adding-the-dash0-catalog)
  - [Creating the OperatorGroup and the Subscription](#creating-the-operatorgroup-and-the-subscription)
- [Initial Configuration](#initial-configuration)
  - [Configuring the Dash0 Backend Connection](#configuring-the-dash0-backend-connection)
  - [Enabling Dash0 Monitoring for Namespaces](#enabling-dash0-monitoring-for-namespaces)
- [Configuration](#configuration)
  - [Operator Settings via the Subscription](#operator-settings-via-the-subscription)
  - [Resources of the Operator Manager](#resources-of-the-operator-manager)
  - [Settings of the Managed Workloads](#settings-of-the-managed-workloads)
  - [Optional Features](#optional-features)
  - [Settings That Are Only Available with Helm](#settings-that-are-only-available-with-helm)
- [Notes on OpenShift](#notes-on-openshift)
- [Upgrading](#upgrading)
- [Switching from Helm to OLM](#switching-from-helm-to-olm)
- [Uninstallation](#uninstallation)

## Prerequisites

* [Kubernetes](https://kubernetes.io/) 1.25.16 or later.
* OLM v0, also known as OLM Classic, installed in the cluster.
  Red Hat OpenShift ships with OLM.
  For other clusters, refer to the [OLM documentation](https://olm.operatorframework.io/docs/getting-started/) for
  installing it.
  OLM v1 (`ClusterExtension` resources) is not supported at the moment.
* Permissions to create namespaces and cluster-scoped resources.
* No Helm installation of the Dash0 operator in the cluster.

## Installation

The installation consists of the following steps:

1. Create a namespace for the operator.
2. Add the Dash0 catalog to that namespace.
3. Subscribe to the operator package in that namespace. OLM then installs the operator.

Once the operator is installed, configure the Dash0 backend connection and enable Dash0 monitoring, see
[Initial Configuration](#initial-configuration).

The examples in this guide use `kubectl`.
On OpenShift, `oc` can be used in the same way.

### Creating the Operator Namespace

OLM does not create the namespace for the operator.
Create it before subscribing to the operator. This guide uses `dash0-system`:

```console
kubectl create namespace dash0-system
```

The operator runs its OpenTelemetry collectors in this namespace.
The collector DaemonSet reads the log files of the pods from the nodes via `hostPath` volumes and listens on host
ports, which the Pod Security Standards `baseline` and `restricted` do not allow.
The eBPF profiler (see [Optional Features](#optional-features)) even requires a privileged container.
Therefore, label the namespace so that Pod Security Admission allows privileged pods:

```console
kubectl label namespace dash0-system pod-security.kubernetes.io/enforce=privileged --overwrite
```

Do not install the operator into the namespace `operators` that OLM creates.
OLM labels this namespace with the Pod Security Standard `baseline`, which rejects the collector DaemonSet.

### Adding the Dash0 Catalog

Create a file `dash0-operator-catalog.yaml` with the following content:

```yaml
apiVersion: operators.coreos.com/v1alpha1
kind: CatalogSource
metadata:
  name: dash0-operator-catalog
  namespace: dash0-system
spec:
  sourceType: grpc
  image: ghcr.io/dash0hq/dash0-operator-catalog:latest
  displayName: Dash0 Operator
  publisher: Dash0
  grpcPodConfig:
    securityContextConfig: restricted
  updateStrategy:
    registryPoll:
      interval: 60m
```

Apply it:

```console
kubectl apply -f dash0-operator-catalog.yaml
```

Notes on the settings:

* `metadata.namespace`: A catalog in the operator namespace is only available to subscriptions in that namespace.
  Other namespaces do not see the Dash0 catalog.
* `spec.image`: The tag `latest` always points to the catalog with the newest operator release.
  OLM reads the catalog image again every `registryPoll.interval` and finds new releases this way, see
  [Upgrading](#upgrading).
  A catalog with a full version tag (e.g. `ghcr.io/dash0hq/dash0-operator-catalog:0.162.0`) contains the releases up to
  that version and never changes.
* `spec.grpcPodConfig.securityContextConfig`: OLM runs the catalog image as a pod in the namespace of the catalog
  source.
  The value `restricted` makes this pod comply with the Pod Security Standard `restricted`.

Wait until the catalog source is ready, that is, until the following command prints `READY`:

```console
kubectl get catalogsource dash0-operator-catalog \
  --namespace dash0-system \
  -o jsonpath='{.status.connectionState.lastObservedState}'
```

### Creating the OperatorGroup and the Subscription

The operator watches all namespaces of the cluster.
OLM therefore needs an `OperatorGroup` without target namespaces (install mode `AllNamespaces`) in the operator
namespace.
A namespace must not have more than one `OperatorGroup`.

Create a file `dash0-operator-subscription.yaml` with the following content:

```yaml
apiVersion: operators.coreos.com/v1
kind: OperatorGroup
metadata:
  name: dash0-operator
  namespace: dash0-system
---
apiVersion: operators.coreos.com/v1alpha1
kind: Subscription
metadata:
  name: dash0-operator
  namespace: dash0-system
spec:
  name: dash0-operator
  channel: stable-v0
  source: dash0-operator-catalog
  sourceNamespace: dash0-system
  installPlanApproval: Automatic
```

Apply it:

```console
kubectl apply -f dash0-operator-subscription.yaml
```

OLM now installs the newest operator version of the channel `stable-v0`.
The installation is complete when the `ClusterServiceVersion` of the operator has the phase `Succeeded`:

```console
kubectl get clusterserviceversions --namespace dash0-system
```

## Initial Configuration

The Dash0 resources can only be created once the installation is complete, that is, once the `ClusterServiceVersion`
of the operator has the phase `Succeeded`.
OLM creates the custom resource definitions during the installation, and the `Dash0OperatorConfiguration` and
`Dash0Monitoring` resources are checked by webhooks of the operator, so the API server rejects them as long as the
operator is not running.

### Configuring the Dash0 Backend Connection

With Helm, the backend connection can be provided as Helm values when installing the operator.
With OLM, configure it by creating a `Dash0OperatorConfiguration` resource, as described in
[Configuring the Dash0 Backend Connection](../../helm-chart/dash0-operator/docs/configuration.md#configuring-the-dash0-backend-connection).
If you reference the Dash0 authorization token via a Kubernetes secret, create the secret in the operator namespace
`dash0-system`, see
[Using a Kubernetes Secret for the Dash0 Authorization Token](../../helm-chart/dash0-operator/docs/configuration.md#using-a-kubernetes-secret-for-the-dash0-authorization-token).

### Enabling Dash0 Monitoring for Namespaces

Enable Dash0 monitoring per namespace with a `Dash0Monitoring` resource, see
[Enable Dash0 Monitoring For a Namespace](../../helm-chart/dash0-operator/docs/configuration.md#enable-dash0-monitoring-for-a-namespace),
or let the operator monitor namespaces automatically, see
[Automatic Namespace Monitoring](../../helm-chart/dash0-operator/docs/configuration.md#automatic-namespace-monitoring).

Under OLM, the `Dash0Monitoring` resource is only available in the version `operator.dash0.com/v1beta1`.

All other configuration guides (for example [Configuration](../../helm-chart/dash0-operator/docs/configuration.md),
[Auto-Instrumentation](../../helm-chart/dash0-operator/docs/auto-instrumentation.md) and
[Managing Dash0 Resources](../../helm-chart/dash0-operator/docs/managing-dash0-resources.md)) apply to an OLM
installation as well.
Where they refer to Helm values, use the equivalents described in the next section.

## Configuration

The Helm values of an installation via Helm have the following equivalents under OLM:

| Helm values                                                                                                                                                                           | OLM installation                                                                                              |
|---------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------|---------------------------------------------------------------------------------------------------------------|
| Settings of the operator configuration resource (`operator.dash0Export.*`, `operator.exports`, `operator.clusterName`, `operator.selfMonitoringEnabled`, `operator.autoMonitorNamespaces.*`, ...) | Fields of the `Dash0OperatorConfiguration` resource, see [Configuration](../../helm-chart/dash0-operator/docs/configuration.md) |
| Settings of the operator manager process (`operator.logLevel`, `operator.collectors.disableHostPorts`, ...)                                                                          | Environment variables in the `Subscription`, see [Operator Settings via the Subscription](#operator-settings-via-the-subscription) |
| Resources of the operator manager (`operator.managerContainerResources`)                                                                                                             | `spec.config.resources` of the `Subscription`, see [Resources of the Operator Manager](#resources-of-the-operator-manager) |
| Resources, scheduling and metadata of the workloads the operator manages (`operator.collectors.daemonSetCollectorContainerResources`, `operator.targetAllocator.tolerations`, ...)  | `spec.components` of the `Dash0OperatorConfiguration` and `Dash0SignalControl` resources, see [Settings of the Managed Workloads](#settings-of-the-managed-workloads) |

### Operator Settings via the Subscription

Settings of the operator manager process are environment variables of the operator manager container.
Set them in `spec.config.env` of the `Subscription`, for example:

```yaml
apiVersion: operators.coreos.com/v1alpha1
kind: Subscription
metadata:
  name: dash0-operator
  namespace: dash0-system
spec:
  name: dash0-operator
  channel: stable-v0
  source: dash0-operator-catalog
  sourceNamespace: dash0-system
  installPlanApproval: Automatic
  config:
    env:
      - name: DASH0_LOG_LEVEL
        value: debug
```

A variable in the `Subscription` replaces the variable of the same name in the operator's deployment.
OLM restarts the operator manager when the `Subscription` changes, and keeps the settings across upgrades.

| Helm value                                                                                                                                    | Environment variable                                                                                                                                                       |
|-----------------------------------------------------------------------------------------------------------------------------------------------|----------------------------------------------------------------------------------------------------------------------------------------------------------------------------|
| `operator.logLevel`                                                                                                                           | `DASH0_LOG_LEVEL`                                                                                                                                                          |
| `operator.openShift.enabled`                                                                                                                  | `DASH0_OPENSHIFT` (not needed in most cases, see [Notes on OpenShift](#notes-on-openshift))                                                                               |
| `operator.telemetryCollectionEnabled`                                                                                                         | `DASH0_TELEMETRY_COLLECTION_ENABLED`                                                                                                                                       |
| `operator.collectors.forceUseServiceUrl`                                                                                                      | `DASH0_FORCE_USE_OTEL_COLLECTOR_SERVICE_URL`                                                                                                                               |
| `operator.collectors.disableHostPorts`                                                                                                        | `DASH0_DISABLE_OTEL_COLLECTOR_HOST_PORTS`                                                                                                                                  |
| `operator.collectors.otlpGrpcHostPort`                                                                                                        | `DASH0_OTEL_COLLECTOR_OTLP_GRPC_HOST_PORT`                                                                                                                                 |
| `operator.collectors.otlpHttpHostPort`                                                                                                        | `DASH0_OTEL_COLLECTOR_OTLP_HTTP_HOST_PORT`                                                                                                                                 |
| `operator.instrumentation.delayAfterEachWorkloadMillis`                                                                                       | `DASH0_INSTRUMENTATION_DELAY_AFTER_EACH_WORKLOAD_MILLIS`                                                                                                                   |
| `operator.instrumentation.delayAfterEachNamespaceMillis`                                                                                      | `DASH0_INSTRUMENTATION_DELAY_AFTER_EACH_NAMESPACE_MILLIS`                                                                                                                  |
| `operator.instrumentation.debug`                                                                                                              | `DASH0_INSTRUMENTATION_DEBUG`                                                                                                                                              |
| `operator.instrumentation.enablePythonAutoInstrumentation`                                                                                    | `DASH0_ENABLE_PYTHON_AUTO_INSTRUMENTATION`                                                                                                                                 |
| `operator.instrumentation.enableRubyAutoInstrumentation`                                                                                      | `DASH0_ENABLE_RUBY_AUTO_INSTRUMENTATION`                                                                                                                                   |
| `operator.ebpfProfilerEnabled`                                                                                                                | `DASH0_EBPF_PROFILER_ENABLED`, see [Optional Features](#optional-features)                                                                                                 |
| `operator.agent0Connector.enabled`, `.serverAddress`, `.token`, `.secretRef.name`, `.secretRef.key`, `.insecure`                              | `DASH0_AGENT0_CONNECTOR_ENABLED`, `DASH0_AGENT0_CONNECTOR_SERVER_ADDRESS`, `DASH0_AGENT0_CONNECTOR_TOKEN`, `DASH0_AGENT0_CONNECTOR_SECRET_REF_NAME`, `DASH0_AGENT0_CONNECTOR_SECRET_REF_KEY`, `DASH0_AGENT0_CONNECTOR_INSECURE` |
| `operator.syntheticsWorker.enabled`, `.serverAddress`, `.insecure`, `.priorityClassName`                                                      | `DASH0_SYNTHETICS_WORKER_ENABLED`, `DASH0_SYNTHETICS_WORKER_SERVER_ADDRESS`, `DASH0_SYNTHETICS_WORKER_INSECURE`, `DASH0_SYNTHETICS_WORKER_PRIORITY_CLASS_NAME`             |
| `operator.iac.periodicRetry.enabled`, `.interval`                                                                                             | `DASH0_IAC_PERIODIC_RETRY_ENABLED`, `DASH0_IAC_PERIODIC_RETRY_INTERVAL`                                                                                                    |
| `operator.disableCollectorResourceWatches`                                                                                                    | `DASH0_DISABLE_COLLECTOR_RESOURCE_WATCHES`                                                                                                                                 |
| `operator.collectors.debugVerbosityDetailed`                                                                                                  | `OTEL_COLLECTOR_DEBUG_VERBOSITY_DETAILED`                                                                                                                                  |
| `operator.collectors.sendBatchSize`, `.sendBatchMaxSize`                                                                                      | `OTEL_COLLECTOR_SEND_BATCH_SIZE`, `OTEL_COLLECTOR_SEND_BATCH_MAX_SIZE`                                                                                                     |
| `operator.collectors.k8s_attributes.disableReplicasetInformer`, `.disableJobInformer`                                                         | `OTEL_COLLECTOR_K8SATTRIBUTES_DISABLE_REPLICASET_INFORMER`, `OTEL_COLLECTOR_K8SATTRIBUTES_DISABLE_JOB_INFORMER`                                                            |
| `operator.collectors.k8s_attributes.wait_for_metadata`, `.wait_for_metadata_timeout`                                                          | `OTEL_COLLECTOR_K8SATTRIBUTES_WAIT_FOR_METADATA`, `OTEL_COLLECTOR_K8SATTRIBUTES_WAIT_FOR_METADATA_TIMEOUT`                                                                 |
| `operator.collectors.k8s_attributes.shareProcessorBetweenPipelines`                                                                           | `OTEL_COLLECTOR_K8SATTRIBUTES_SHARE_PROCESSOR_BETWEEN_PIPELINES`                                                                                                           |
| `operator.collectors.enablePprof`                                                                                                             | `OTEL_COLLECTOR_ENABLE_PPROF_EXTENSION`                                                                                                                                    |
| `operator.collectors.compressConfigMaps`                                                                                                      | `OTEL_COLLECTOR_COMPRESS_CONFIG_MAPS`                                                                                                                                      |
| `operator.collectors.kubeletstats.autoDetectEndpoint`, `.endpoint`, `.authType`, `.insecureSkipVerify`                                         | `OTEL_COLLECTOR_KUBELETSTATS_AUTO_DETECT_ENDPOINT`, `OTEL_COLLECTOR_KUBELETSTATS_ENDPOINT`, `OTEL_COLLECTOR_KUBELETSTATS_AUTH_TYPE`, `OTEL_COLLECTOR_KUBELETSTATS_INSECURE_SKIP_VERIFY` (set all four together) |
| `pullPolicy` of the images except `operator.image.pullPolicy`, e.g. `operator.collectorImage.pullPolicy`                                       | `DASH0_<IMAGE>_IMAGE_PULL_POLICY`, e.g. `DASH0_COLLECTOR_IMAGE_PULL_POLICY`                                                                                                |
| `operator.managerContainerResources.gomemlimit`                                                                                               | `GOMEMLIMIT`, see [Resources of the Operator Manager](#resources-of-the-operator-manager)                                                                                   |

Boolean variables take the values `true` and `false`.
The descriptions of the settings are in the Helm chart's
[values.yaml](https://github.com/dash0hq/dash0-operator/blob/main/helm-chart/dash0-operator/values.yaml).

Do not set any other environment variable of the operator's deployment in the `Subscription`.
In particular:

* The image variables (`DASH0_<IMAGE>_IMAGE`, `RELATED_IMAGE_<IMAGE>`) are set by each operator version.
  A value in the `Subscription` is kept across upgrades and would pin the image to the old version.
* The variables `DASH0_OPERATOR_CONFIGURATION_*` are not supported under OLM.
  Create the `Dash0OperatorConfiguration` resource instead.

### Resources of the Operator Manager

Set the resource requests and limits of the operator manager container in `spec.config.resources` of the `Subscription`.
They replace the resources of the operator's deployment as a whole, so specify all requests and limits you want to keep.
Set the environment variable `GOMEMLIMIT` as well. The default value `205MiB` matches the default memory limit of
`256Mi`.
A value of about 80% of the memory limit is recommended:

```yaml
spec:
  config:
    resources:
      limits:
        cpu: 500m
        ephemeral-storage: 500Mi
        memory: 512Mi
      requests:
        cpu: 50m
        ephemeral-storage: 500Mi
        memory: 256Mi
    env:
      - name: GOMEMLIMIT
        value: 410MiB
```

`spec.config` also supports `tolerations`, `nodeSelector` and `affinity` for the operator manager pod, the equivalents
of the Helm values `operator.tolerations` and `operator.nodeAffinity`.

### Settings of the Managed Workloads

Resources, scheduling settings, labels and annotations of the workloads the operator manages are fields under
`spec.components` of the `Dash0OperatorConfiguration` resource, and of the `Dash0SignalControl` resource for the
Signal Control workloads.
Fields that are not set keep their defaults.

| Workload                       | Resource and field                                                          | Fields                                                                                                                                                                                                                                                                                           | Helm values (for comparison)                                                           |
|--------------------------------|-----------------------------------------------------------------------------|--------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------|----------------------------------------------------------------------------------------|
| Instrumentation init container | `Dash0OperatorConfiguration` `spec.components.instrumentationInitContainer` | `resources`                                                                                                                                                                                                                                                                                      | `operator.initContainerResources`                                                      |
| Collector DaemonSet            | `Dash0OperatorConfiguration` `spec.components.daemonSetCollector`           | `collectorContainerResources`, `configurationReloaderContainerResources`, `filelogOffsetSyncContainerResources`, `filelogOffsetStorageVolume`, `seLinuxOptions`, `sysctls`, `probes`, `priorityClassName`, `labels`, `annotations`, `podLabels`, `podAnnotations`, `tolerations`, `nodeAffinity` | `operator.collectors.daemonSet*`, `operator.collectors.filelogOffsetSyncStorageVolume` |
| Collector Deployment           | `Dash0OperatorConfiguration` `spec.components.deploymentCollector`          | `collectorContainerResources`, `configurationReloaderContainerResources`, `sysctls`, `probes`, `priorityClassName`, `labels`, `annotations`, `podLabels`, `podAnnotations`, `tolerations`, `nodeAffinity`                                                                                        | `operator.collectors.deployment*`                                                      |
| Target-allocator               | `Dash0OperatorConfiguration` `spec.components.targetAllocator`              | `containerResources`, `mTls`, `allowInsecureAuthSecrets`, `labels`, `annotations`, `podLabels`, `podAnnotations`, `tolerations`, `nodeAffinity`                                                                                                                                                  | `operator.targetAllocator.*`                                                           |
| Agent0-connector               | `Dash0OperatorConfiguration` `spec.components.agent0Connector`              | `containerResources`, `maxConcurrentCommands`, `allowedKubectlCommands`, `enablePprof`, `labels`, `annotations`, `podLabels`, `podAnnotations`, `tolerations`, `nodeAffinity`                                                                                                                    | `operator.agent0Connector.*`                                                           |
| eBPF profiler                  | `Dash0OperatorConfiguration` `spec.components.ebpfProfiler`                 | `containerResources`, `priorityClassName`, `labels`, `annotations`, `podLabels`, `podAnnotations`, `tolerations`, `nodeAffinity`                                                                                                                                                                 | `operator.ebpfProfiler.*`                                                              |
| Debuginfo proxy                | `Dash0OperatorConfiguration` `spec.components.debuginfoProxy`               | `containerResources`, `priorityClassName`, `labels`, `annotations`, `podLabels`, `podAnnotations`, `tolerations`, `nodeAffinity`                                                                                                                                                                 | `operator.debuginfoProxy.*`                                                            |
| Signal Control collector       | `Dash0SignalControl` `spec.components.collector`                            | `replicas`, `collectorContainerResources`, `configurationReloaderContainerResources`, `sysctls`, `probes`, `priorityClassName`, `labels`, `annotations`, `podLabels`, `podAnnotations`, `tolerations`, `nodeAffinity`                                                                            | `operator.collectors.signalControlCollector*`                                          |
| Edge Proxy                     | `Dash0SignalControl` `spec.components.edgeProxy`                            | `replicas`, `containerResources`, `enablePprof`, `labels`, `annotations`, `podLabels`, `podAnnotations`, `tolerations`, `nodeAffinity`                                                                                                                                                           | `operator.signalControl.edgeProxy.*`                                                   |

The structure of each field is shown by `kubectl explain`, for example:

```console
kubectl explain dash0operatorconfigurations.spec.components.daemonSetCollector
```

The collector DaemonSet stores its filelog offsets in a `hostPath` volume on each node, or in a config map if the
cluster does not admit that volume or its nodes likely enforce SELinux, see
[Filelog Offset Storage](../../helm-chart/dash0-operator/docs/advanced-configuration.md#filelog-offset-storage).
If it uses the config map in one of the clusters listed in
[When to Avoid the Config Map for Filelog Offsets](../../helm-chart/dash0-operator/docs/advanced-configuration.md#when-to-avoid-the-config-map-for-filelog-offsets),
provide a volume for the filelog offsets (see
[Providing a Filelog Offset Volume](../../helm-chart/dash0-operator/docs/advanced-configuration.md#providing-a-filelog-offset-volume)).
Review the resources of the collectors as well (see
[Tuning Resource Requests and Limits](../../helm-chart/dash0-operator/docs/advanced-configuration.md#tuning-resource-requests-and-limits)).
For example:

```yaml
apiVersion: operator.dash0.com/v1alpha1
kind: Dash0OperatorConfiguration
metadata:
  name: dash0-operator-configuration
spec:
  exports:
    - dash0:
        endpoint: ingress... # TODO REPLACE THIS WITH YOUR DASH0 INGRESS ENDPOINT
        authorization:
          secretRef:
            name: dash0-authorization-secret
            key: token
  dash0Api:
    - endpoint: https://api... # TODO REPLACE THIS WITH YOUR DASH0 API ENDPOINT
      authorization:
        secretRef:
          name: dash0-authorization-secret
          key: token
  components:
    daemonSetCollector:
      collectorContainerResources:
        limits:
          memory: 1Gi
        requests:
          memory: 512Mi
      filelogOffsetStorageVolume:
        persistentVolumeClaim:
          claimName: dash0-filelog-offsets
```

The persistent volume claim has to exist in the operator namespace.

### Optional Features

The following features are switched off by default and are switched on via the `Subscription` (see
[Operator Settings via the Subscription](#operator-settings-via-the-subscription)):

* **Agent0 Kubernetes integration:** Set `DASH0_AGENT0_CONNECTOR_ENABLED` to `true`, together with
  `DASH0_AGENT0_CONNECTOR_SERVER_ADDRESS` and either `DASH0_AGENT0_CONNECTOR_SECRET_REF_NAME` and
  `DASH0_AGENT0_CONNECTOR_SECRET_REF_KEY` or `DASH0_AGENT0_CONNECTOR_TOKEN`, see
  [Agent0 Kubernetes Integration](../../helm-chart/dash0-operator/docs/agent0-integration.md#enabling-the-integration).
  The agent0-connector uses its default RBAC rules, which cannot be changed under OLM.
  The kubectl commands `logs` and `events` can be allowed via `spec.components.agent0Connector.allowedKubectlCommands`,
  but they fail, since the default RBAC rules grant no access to pod logs and events.
* **Synthetics worker (private locations):** Set `DASH0_SYNTHETICS_WORKER_ENABLED` to `true`, together with
  `DASH0_SYNTHETICS_WORKER_SERVER_ADDRESS`, see
  [Dash0 Synthetics Worker](../../helm-chart/dash0-operator/docs/synthetics-worker.md).
* **eBPF profiler:** Set `DASH0_EBPF_PROFILER_ENABLED` to `true`, and set `spec.profiling.ebpfProfiler.enabled: true`
  in the `Dash0OperatorConfiguration` resource, see
  [Operator-managed eBPF profiler](../../helm-chart/dash0-operator/docs/profiling.md#operator-managed-ebpf-profiler).
  This also enables the profiling pipelines, unless `spec.profiling.enabled` is set; setting it to `false` together
  with the profiler is rejected.
  Where error messages of the operator refer to the Helm value `operator.ebpfProfilerEnabled`, set
  `DASH0_EBPF_PROFILER_ENABLED` instead.
  The profiler runs as a privileged container, which the namespace label from
  [Creating the Operator Namespace](#creating-the-operator-namespace) allows.

Python and Ruby auto-instrumentation are enabled by default.
To disable them, set `DASH0_ENABLE_PYTHON_AUTO_INSTRUMENTATION` or `DASH0_ENABLE_RUBY_AUTO_INSTRUMENTATION` to `false`
in the `Subscription`, see
[Python Auto-Instrumentation](../../helm-chart/dash0-operator/docs/configuration.md#python-auto-instrumentation) and
[Ruby Auto-Instrumentation](../../helm-chart/dash0-operator/docs/configuration.md#ruby-auto-instrumentation).

Signal Control needs no setting in the `Subscription`. It is switched on by creating a `Dash0SignalControl` resource,
see [Signal Control](../../helm-chart/dash0-operator/docs/signal-control-edge.md).

### Settings That Are Only Available with Helm

The following Helm values have no equivalent under OLM:

* `operator.gke.autopilot.enabled`: GKE Autopilot is not supported under OLM.
* `operator.certManager.*`: OLM provides the certificates of the operator's webhooks.
* `operator.iac.persesDashboard.autoPatchConversionWebhook`: The operator does not patch the conversion webhook of a
  `PersesDashboard` CRD under OLM.
* `operator.targetAllocator.allowSecretAccessInNamespaces`: The operator and the target-allocator can read secrets in
  all namespaces.
* `operator.agent0Connector.clusterRole.rules` and `operator.agent0Connector.clusterRole.additionalRules`: The
  agent0-connector always uses its default RBAC rules.
* `operator.managedResourceNamePrefix`: The names of the workloads the operator manages (collectors,
  target-allocator, Signal Control, eBPF profiler, agent0-connector and synthetics workers) always start with
  `dash0-operator`, for example `dash0-operator-opentelemetry-collector-agent`.
* `operator.collectors.filelogOffsetStorageMode` and `operator.collectors.filelogOffsetHostPath`: The operator always
  uses the storage mode `auto` and the directory `/var/lib/dash0-operator/filelog-offsets` on the nodes.
  Log messages of the operator that suggest setting `operator.collectors.filelogOffsetStorageMode=hostPath` do not
  apply under OLM.
  On nodes that the operator treats as SELinux-enforcing, it only uses the `hostPath` volume if the collector DaemonSet
  runs with the SELinux type `spc_t` (`spec.components.daemonSetCollector.seLinuxOptions`).
  To store the filelog offsets elsewhere, provide a volume via
  `spec.components.daemonSetCollector.filelogOffsetStorageVolume`, see
  [Settings of the Managed Workloads](#settings-of-the-managed-workloads).
* `operator.replicaCount`: The operator manager always runs with one replica.
* `operator.managerPriorityClassName`: The `Subscription` cannot set a priority class for the operator manager.
* `operator.imagePullSecrets`: The `Subscription` cannot set image pull secrets for the operator manager.

## Notes on OpenShift

The installation on Red Hat OpenShift uses the same steps and resources as on other clusters.

* The operator detects OpenShift automatically.
  The Helm value `operator.openShift.enabled` and its environment variable `DASH0_OPENSHIFT` are not needed.
* The operator package includes service accounts for the collector DaemonSet and the eBPF profiler that may use the
  `privileged` SecurityContextConstraints (SCC). OLM creates them together with the operator.
  Unlike the Helm chart, no custom SCC is created.
  All other workloads that the operator creates in its namespace run with the default SCC `restricted-v2`.
* When uninstalling the operator in the web console, follow the steps in [Uninstallation](#uninstallation) first.

## Upgrading

With `installPlanApproval: Automatic`, OLM upgrades the operator as soon as the catalog has a new version, that is,
within the `registryPoll.interval` of the `CatalogSource` after the release.
OLM upgrades the operator's custom resource definitions together with the operator.

To approve every upgrade manually, set `installPlanApproval: Manual` in the `Subscription`.
OLM then creates an `InstallPlan` for each new version and waits for its approval:

```console
kubectl get installplans --namespace dash0-system
kubectl patch installplan <name of the install plan> \
  --namespace dash0-system \
  --type merge \
  --patch '{"spec":{"approved":true}}'
```

With manual approval, the first installation needs an approval as well.

## Switching from Helm to OLM

An existing Helm installation cannot be taken over by OLM.
Uninstall it first, as described in
[Uninstallation](../../helm-chart/dash0-operator/docs/upgrading.md#uninstallation).
`helm uninstall` also removes the custom resource definitions of the operator (all `*.dash0.com` definitions and
`slos.openslo.com`), and with them all resources of these kinds in the cluster (operator configuration, monitoring
resources, views, synthetic checks, notification channels, SLOs etc.), including `SLO` resources of other tools.
`PersesDashboard` and `PrometheusRule` resources are not affected.
Keep the manifests of these resources, and apply them again after the operator has been installed via OLM.
Under OLM, `Dash0Monitoring` manifests have to use the version `operator.dash0.com/v1beta1`, see
[Operator Version 0.71.0: v1alpha1 to v1beta1 Migration](../../helm-chart/dash0-operator/docs/upgrading.md#operator-version-0710-v1alpha1-to-v1beta1-migration).
If the operator configuration resource has been created from Helm values, create it as described in
[Configuring the Dash0 Backend Connection](#configuring-the-dash0-backend-connection).

## Uninstallation

Deleting the `Subscription` and the `ClusterServiceVersion` removes the operator, but OLM does not revert the changes
the operator has made to your workloads.
Follow these steps in this order, so that the operator reverts them while it still runs:

1. Delete the Dash0 operator configuration resource.
   The operator stops monitoring namespaces automatically and removes its collectors:
   ```console
   kubectl delete dash0operatorconfigurations --all
   ```
2. Delete all Dash0 monitoring resources.
   The operator reverts the instrumentation of the workloads in each namespace before the resource is gone, which
   restarts the pods of the instrumented workloads.
   The command returns once all monitoring resources have been deleted:
   ```console
   kubectl delete dash0monitorings --all --all-namespaces
   ```
3. Delete the `Subscription` and the `ClusterServiceVersion`.
   During an upgrade, the `Subscription` references two `ClusterServiceVersion`s. The commands delete both:
   ```console
   current_csv=$(kubectl get subscription dash0-operator --namespace dash0-system -o jsonpath='{.status.currentCSV}')
   installed_csv=$(kubectl get subscription dash0-operator --namespace dash0-system -o jsonpath='{.status.installedCSV}')
   kubectl delete subscription dash0-operator --namespace dash0-system
   kubectl delete clusterserviceversion "$current_csv" "$installed_csv" --namespace dash0-system --ignore-not-found
   ```

In namespaces with
[`instrumentWorkloads.mode: pods`](../../helm-chart/dash0-operator/docs/auto-instrumentation.md#instrumenting-pods-instead-of-workloads),
pods that have been instrumented when they were created keep their instrumentation until they are recreated.

Optionally, delete the operator namespace afterwards, which also removes the `OperatorGroup`, the `CatalogSource` and
the secret with the Dash0 authorization token:

```console
kubectl delete namespace dash0-system
```

If the collectors have stored their filelog offsets in the operator's `hostPath` volume, the offsets remain on the
nodes.
To remove them, delete the directory `/var/lib/dash0-operator/filelog-offsets/dash0-system` on each node.

OLM does not delete the custom resource definitions of the operator, and keeps the remaining Dash0 resources (for
example views, synthetic checks and notification channels).
Deleting a custom resource definition deletes all resources of its kind in the cluster.
The objects in Dash0 that the operator has created from these resources are not deleted, since the operator no longer
runs.
To remove the definitions, list them with

```console
kubectl get customresourcedefinitions -o name | grep -e '\.dash0\.com$' -e '^customresourcedefinition.apiextensions.k8s.io/slos\.openslo\.com$'
```

and delete them with `kubectl delete`.
Keep `slos.openslo.com` if another tool in the cluster uses OpenSLO resources.

> **Warning:** In the uninstall dialog of the OpenShift web console, leave the option "Delete all operand instances"
> unchecked, and follow the steps above before uninstalling.
> The option deletes all resources of the operator's custom resource definitions in the cluster at the same time,
> including views, synthetic checks, notification channels and teams.
> Depending on the order in which they are deleted, the operator may also delete the corresponding objects in Dash0.
> It also deletes all `SLO` resources of the group `openslo.com`, including those of other tools.

If the operator has been removed before the monitoring resources were deleted, the workloads stay instrumented, and
namespaces with a Dash0 monitoring resource cannot be deleted, since the finalizer of the monitoring resource is no
longer handled.
To recover, install the operator again via OLM and follow the steps above.

## Related Documentation

- [Configuration](../../helm-chart/dash0-operator/docs/configuration.md) - Configuration options
- [Auto-Instrumentation](../../helm-chart/dash0-operator/docs/auto-instrumentation.md) - Workload instrumentation
  details
- [Managing Dash0 Resources](../../helm-chart/dash0-operator/docs/managing-dash0-resources.md) - Dash0 resources via
  Kubernetes custom resources
