# Automatic Workload Instrumentation

This guide covers automatic workload instrumentation, including Python support, image volumes, disabling instrumentation, and resource attributes.

## Table of Contents

- [Overview](#overview)
- [Using Image Volumes for Auto-Instrumentation Files](#using-image-volumes-for-auto-instrumentation-files)
- [Python Auto-Instrumentation](#python-auto-instrumentation)
- [Disabling Auto-Instrumentation for Specific Workloads](#disabling-auto-instrumentation-for-specific-workloads)
- [Using a Custom Label Selector to Control Auto-Instrumentation](#using-a-custom-label-selector-to-control-auto-instrumentation)
- [Specifying Additional Resource Attributes via Labels and Annotations](#specifying-additional-resource-attributes-via-labels-and-annotations)
- [Sending Data to the OpenTelemetry Collectors Managed by the Dash0 Operator](#sending-data-to-the-opentelemetry-collectors-managed-by-the-dash0-operator)
- [How Automatic Workload Instrumentation Works](#how-automatic-workload-instrumentation-works)

## Overview

In namespaces that are [enabled for Dash0 monitoring](configuration.md#enable-dash0-monitoring-for-a-namespace), all supported workload types are automatically instrumented by the Dash0 operator, to achieve two goals:

1. Enable tracing for [supported runtimes](../README.md#supported-runtimes) out of the box, and
2. Improve auto-detection of OpenTelemetry resource attributes.

This allows Dash0 users to avoid the hassle of manually adding the OpenTelemetry SDK to their applications, or to set Kubernetes-related resource attributes manually. Dash0 simply takes care of it automatically!

Automatic tracing only works for [supported runtimes](../README.md#supported-runtimes). For other runtimes, you can add an OpenTelemetry SDK to your workloads [by other means](https://opentelemetry.io/docs/languages/).

Auto-detecting OpenTelemetry resource attributes works for all runtimes, that is, for runtimes that are supported by Dash0's auto-instrumentation as well as for workloads to which an OpenTelemetry SDK has been added otherwise. There is currently one caveat: The resource attribute auto-detection relies on the process or runtime in question to use dynamic linking at startup (that is, binding to a flavor of libc), which is true for almost all runtimes. One notable exception are so called freestanding a.k.a. libc-free binaries, for example most binaries built with Go.

The Dash0 operator will instrument the following workload types:

* [CronJobs](https://kubernetes.io/docs/concepts/workloads/controllers/cron-jobs/)
* [DaemonSets](https://kubernetes.io/docs/concepts/workloads/controllers/daemonset/)
* [Deployments](https://kubernetes.io/docs/concepts/workloads/controllers/deployment/)
* [Jobs](https://kubernetes.io/docs/concepts/workloads/controllers/job/)
* [Pods](https://kubernetes.io/docs/concepts/workloads/pods/)
* [ReplicaSets](https://kubernetes.io/docs/concepts/workloads/controllers/replicaset/)
* [StatefulSets](https://kubernetes.io/docs/concepts/workloads/controllers/statefulset/)

Note that Kubernetes jobs and Kubernetes pods are only instrumented at deploy time, existing jobs and pods cannot be instrumented since there is no way to restart them. For all other workload types, the operator can instrument existing workloads as well as new workloads at deploy time (depending on the setting of `spec.instrumentWorkloads.mode` in the Dash0 monitoring resource).

The instrumentation process is performed by modifying the Pod spec template (for CronJobs, DaemonSets, Deployments, Jobs, ReplicaSets, and StatefulSets) or the Pod spec itself (for standalone Pods).

## Using Image Volumes for Auto-Instrumentation Files

When using auto-instrumentation of workloads, by default the operator adds an [`emptyDir` volume](https://kubernetes.io/docs/concepts/storage/volumes/#emptydir) and an [init container](https://kubernetes.io/docs/concepts/workloads/pods/init-containers/) to provide the instrumentation files to the workload.

Image volumes are a new Kubernetes feature that provides a better way to do this. They provide the following advantages:
* No additional ephemeral storage usage.
* Faster workload startup (because nothing needs to be copied over from the init container to the empty dir volume)

They were introduced in Kubernetes 1.31 as an alpha feature behind a feature gate. In version 1.33 they graduated to beta, but were still disabled by default. Starting with version 1.35, the image volume feature gate is enabled by default, but they are still considered beta. Image volumes finally became a stable feature in version 1.36.

Set the `instrumentationDelivery` setting in the operator configuration resource to determine under which circumstances image volumes will be used instead of the init container approach. When using the operator manager to create and manage the operator configuration resource (i.e. with `operator.dash0Export.enabled=true`), set `operator.instrumentation.delivery` in Helm to configure image volumes.

Allowed values for the instrumentation delivery setting:
- `auto`: use image volumes if the Kubernetes version is 1.36 or later, otherwise use the init container approach.
- `image-volume`: always use image volumes, also on Kubernetes versions older than 1.36. If the Kubernetes version is older than 1.31, the operator manager will log a warning and fall back to the init container approach, since image volumes are not supported in that version. Note that if you are using Kubernetes 1.34 or earlier, and you want to use this setting, you need to enable image volumes when configuring your cluster, since image volumes are disabled by default in versions older than 1.35.
- `init-container`: always use the init container approach, regardless of the Kubernetes version. This is the default.

Note: Changing the instrumentation delivery setting for an existing operator installation will not trigger a bulk re-instrumentation of all existing workloads, even for namespaces that are set to `instrumentWorkloadsMode=all`. Once a workload has been successfully instrumented, there is no benefit in re-instrumenting it with a different delivery mechanism. The new setting will be applied when instrumenting newly deployed workloads, or when a workload is updated/re-deployed.

## Python Auto-Instrumentation

To enable auto-instrumentation for Python workloads, set `operator.instrumentation.enablePythonAutoInstrumentation=true` via Helm. If this setting is enabled for an existing operator installation, Python auto-instrumentation will be enabled immediately for workloads in namespaces that have a Dash0Monitoring resource with `instrumentWorkloads.mode` set to `all`. This will cause all pods in these namespaces to be restarted. For workloads in namespaces that use `instrumentWorkloads.mode=created-and-updated`, it will become active with the next re-deployment of the workload. The setting has no effect on workloads in namespaces that use `instrumentWorkloads.mode=none` or do not have a Dash0Monitoring resource.

Python auto-instrumentation is only supported for Python 3.9 or later. If the Dash0 Python auto-instrumentation detects an incompatible Python version (i.e. version 3.8 or older), it will automatically deactivate itself safely and print a warning to `stderr`:
```
[dash0] warning: cannot auto-instrument Python process: unsupported Python version: 3.8.0
```
This warning is also visible in the Dash0 UI's log view, unless log collection has been disabled for the namespace. Update the Python version to enable automatic Python instrumentation by Dash0 for this workload.

Python auto-instrumentation only works if the configured OTLP export protocol is `http/protobuf`. If the operator is managing the container's `OTEL_EXPORTER_OTLP_ENDPOINT` and `OTEL_EXPORTER_OTLP_PROTOCOL` variables, this will be set correctly automatically. If the Dash0 Python auto-instrumentation detects an incompatible `OTEL_EXPORTER_OTLP_PROTOCOL` setting, it will automatically deactivate itself safely and print a warning to `stderr`:
```
[dash0] warning: cannot auto-instrument Python process: OTEL_EXPORTER_OTLP_PROTOCOL=grpc is not supported
```
This can only happen if the container is setting its own `OTEL_EXPORTER_OTLP_ENDPOINT` and/or `OTEL_EXPORTER_OTLP_PROTOCOL`. Remove these environment variables from the pod spec template to enable automatic Python instrumentation by Dash0 for this workload.

Dash0's Python auto-instrumentation is not compatible with workloads that are already instrumented, either [manually](https://opentelemetry.io/docs/languages/python/instrumentation/) or using the [zero-code instrumentation](https://opentelemetry.io/docs/zero-code/python/), e.g. the `opentelemetry-instrument` wrapper. If existing instrumentation is detected, the Dash0 Python auto-instrumentation will automatically deactivate itself safely and print a warning to `stderr`:
```
[dash0] warning: cannot auto-instrument Python process: The application has OpenTelemetry dependencies which indicate
that it is already instrumented. The following problematic dependencies have been found: ...
Skipping the Dash0 Python auto-instrumentation to avoid double instrumentation.
```
This warning is also visible in the Dash0 UI's log view, unless log collection has been disabled for the namespace. Remove the existing instrumentation from the workload to enable automatic Python instrumentation by Dash0 for this, or leave the existing instrumentation in place, in which case Dash0 will refrain from instrumenting it.

Last but not least, due to the nature of Python's dependency management, Python auto-instrumentation has the potential to introduce dependency conflicts. The Dash0 Python auto-instrumentation checks for potential dependency conflicts before actually instrumenting a process. If a dependency conflict is detected, the Dash0 Python auto-instrumentation will automatically deactivate itself safely and print a warning to `stderr`:
```
[dash0] warning: cannot auto-instrument Python process: dependency conflicts: {'package-name': {'version_required': '>=20.0', 'version_found': '19.0'}}
```
This warning is also visible in the Dash0 UI's log view, unless log collection has been disabled for the namespace. Resolve the version conflicts to enable automatic Python instrumentation by Dash0 for this workload, for example by updating the dependency versions used by the workload. If the conflicting dependencies cannot be resolved, you might need to instrument this workload individually, for example by using the OpenTelemetry Python [zero-code instrumentation](https://opentelemetry.io/docs/zero-code/python/).

## Disabling Auto-Instrumentation for Specific Workloads

In namespaces that are Dash0-monitoring enabled, all workloads are automatically instrumented for tracing and to improve OpenTelemetry resource attributes. This process will modify the Pod spec, e.g. by adding environment variables, Kubernetes labels and an init container. The modifications are described in detail in the section [How Automatic Workload Instrumentation Works](#how-automatic-workload-instrumentation-works).

You can disable these workload modifications for specific workloads by setting the label `dash0.com/enable: "false"` in the top level metadata section of the workload specification.

> **Note:** The actual label selector for enabling or disabling workload modification can be customized in the Dash0 monitoring resource. The label `dash0.com/enable: "false"` can be used when no custom label selector has been configured in the Dash0 monitoring resource, see [Using a Custom Label Selector to Control Auto-Instrumentation](#using-a-custom-label-selector-to-control-auto-instrumentation).

Here is an example for a deployment with this label:

```yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: my-deployment
  labels:
    app: my-deployment-app
    dash0.com/enable: "false"
spec:
  replicas: 1
  selector:
    matchLabels:
      app: my-deployment-app
  template:
    metadata:
      labels:
        app: my-deployment-app
    spec:
      containers:
        - name: my-deployment-app
          image: "some-image:latest"
```

The label can also be applied by using `kubectl`:

```console
kubectl label --namespace $YOUR_NAMESPACE --overwrite deployment $YOUR_DEPLOYMENT_NAME dash0.com/enable=false
```

Note that setting `dash0.com/enable: "false"` will not prevent log collection for pods of workloads with that label, in namespaces that have Dash0 log collection enabled. Also, log collection is enabled by default for all monitored namespaces, unless `spec.logCollection.enabled` has been set to `false` explicitly in the respective Dash0 monitoring resource. Controlling log collection for individual workloads via Kubernetes labels is not supported. To disable log collection for specific workloads in namespaces where log collection is enabled, you can add a filter rule to the monitoring resource. Here is an example:

```yaml
apiVersion: operator.dash0.com/v1beta1
kind: Dash0Monitoring
metadata:
  name: dash0-monitoring-resource
spec:
  filter:
    logs:
      log_records:
      - 'IsMatch(resource.attributes["k8s.pod.name"], "my-workload-name-*")'
```

## Using a Custom Label Selector to Control Auto-Instrumentation

By providing a custom Kubernetes [label selector](https://kubernetes.io/docs/concepts/overview/working-with-objects/labels/#label-selectors) in `spec.instrumentWorkloads.labelSelector` in a Dash0 monitoring resource, you can control which workloads in this namespace will be instrumented by the Dash0 operator.

- Workloads which match this label selector will be instrumented, subject to the value of `spec.instrumentWorkloads.mode`.
- Workloads which do not match this label selector will never be instrumented, regardless of the value of `spec.instrumentWorkloads.mode`.
- The setting `spec.instrumentWorkloads.labelSelector` setting is ignored if `spec.instrumentWorkloads.mode=none`.

If not set explicitly, this label selector assumes the value `"dash0.com/enable!=false"` by default. That is, when no explicit label selector is provided via `spec.instrumentWorkloads.labelSelector`, workloads which:

- do not have the label `dash0.com/enable` at all, or
- have the label `dash0.com/enable` with a value other than `"false"`

will be instrumented, as explained in the [previous section](#disabling-auto-instrumentation-for-specific-workloads).

It is recommended to leave this setting unset (i.e. leave the default `"dash0.com/enable!=false"` in place), unless you have a specific use case that requires a different label selector.

One such use case is implementing an opt-in model for workload instrumentation instead of the usual opt-out model. That is, instead of instrumenting all workloads in a namespace by default and only disabling instrumentation for a few specific workloads, you want to deliberately turn on auto-instrumentation for a few specific workloads and leave all others uninstrumented. Use a label selector with equals (`=`) instead of not-equals (`!=`) to achieve this, for example `auto-instrument-this-workload-with-dash0="true"`.

> **Note:** Opting out of auto-instrumentation and workload modification via a label/label selector will not prevent log collection for pods in namespaces that have Dash0 log collection enabled, see previous section for details.

## Specifying Additional Resource Attributes via Labels and Annotations

> **Note:** The labels and annotations listed in this section can be specified at the pod level, or at the workload level (i.e., the cronjob, deployment, daemonset, job, replicaset, or statefulset). Pod labels and annotations take precedence over workload labels and annotations.

The following [standard Kubernetes labels](https://kubernetes.io/docs/reference/labels-annotations-taints/#labels-annotations-and-taints-used-on-api-objects) are mapped to resource attributes as follows:

* The label `app.kubernetes.io/name` is mapped to `service.name`.
* If `app.kubernetes.io/name` is set, and the label `app.kubernetes.io/version` is also set, it is mapped to `service.version`.
* If `app.kubernetes.io/name` is set, and the label `app.kubernetes.io/part-of` is also set, it is mapped to `service.namespace`.

The operator will not combine pod labels with workload labels for this mapping. The labels `app.kubernetes.io/version` and `app.kubernetes.io/part-of` are only read from the pod labels if `app.kubernetes.io/name` is present on the pod. Similarly, the labels `app.kubernetes.io/version` and `app.kubernetes.io/part-of` are only read from the workload labels if `app.kubernetes.io/name` is present on the workload. Workload labels are not considered at all if `app.kubernetes.io/name` is present on the pod. This ensures that resource attributes are not partially based on pod and partially on workload labels, giving an inconsistent result.

> **Note:** The `OTEL_SERVICE_NAME` environment variable and `service.*` key-value pairs specified in the `OTEL_RESOURCE_ATTRIBUTES` environment variable have precedence over attributes derived from the `app.kubernetes.io/*` labels.

Any _annotation_ in the form of `resource.opentelemetry.io/<key>: <value>` is also mapped to the resource attribute `<key>=<value>`. For example, the following results in the `my.attribute=my-value` resource attribute:

```yaml
apiVersion: v1
kind: Pod
metadata:
  annotations:
    resource.opentelemetry.io/my.attribute: my-value
```

As with the `app.kubernetes.io/*` labels, the `resource.opentelemetry.io/*` annotations can be set on the pod as well as on the workload. In contrast to the `app.kubernetes.io/*` labels, mixing workload level and pod annotations is allowed, that is, you can set `resource.opentelemetry.io/attribute-one` on the workload and `resource.opentelemetry.io/attribute-two` on the pod, and both will be used. In case the same `key` is listed both on the workload and on the pod, the pod annotation takes precedence.

Key-value pairs with a specific `key` set via the `OTEL_RESOURCE_ATTRIBUTES` environment variable will override the value derived from a `resource.opentelemetry.io/<key>: <value>` annotation. Resource attributes set via the `resource.opentelemetry.io/<key>: <value>` annotations will override the resource attributes value set via `app.kubernetes.io/*` labels: for example, `resource.opentelemetry.io/service.name` has precedence over `app.kubernetes.io/name`.

## Sending Data to the OpenTelemetry Collectors Managed by the Dash0 Operator

Besides automatic workload instrumentation (which will make sure that the instrumented workloads send telemetry to the OpenTelemetry collectors managed by the operator), you can also send telemetry data from workloads that are not instrumented by the operator.

To do so, you need to [add an OpenTelemetry SDK](https://opentelemetry.io/docs/languages/) to your workload.

If the workload is in a namespace that is monitored by Dash0, the OpenTelemetry SDK will automatically be configured to send telemetry to the OpenTelemetry collectors managed by the Dash0 operator. This is because the operator automatically sets `OTEL_EXPORTER_OTLP_ENDPOINT` and `OTEL_EXPORTER_OTLP_PROTOCOL` to the correct values when applying the [automatic workload instrumentation](#how-automatic-workload-instrumentation-works).

If the workload is in a namespace that is not monitored by Dash0 (or if `spec.instrumentWorkloads.mode` is set to `none` in the respective Dash0 monitoring resource, or if the workload has opted out of auto-instrumentation via a [label](#disabling-auto-instrumentation-for-specific-workloads), you need to set the environment variable [`OTEL_EXPORTER_OTLP_ENDPOINT`](https://opentelemetry.io/docs/languages/sdk-configuration/otlp-exporter/) (and optionally also [`OTEL_EXPORTER_OTLP_PROTOCOL`](https://opentelemetry.io/docs/specs/otel/protocol/exporter/#specify-protocol)) yourself.

The DaemonSet OpenTelemetry collector managed by the Dash0 operator listens on host port 40318 for HTTP traffic and 40317 for gRPC traffic (unless the Helm chart has been deployed with `operator.collectors.disableHostPorts=true`, which disables the host ports for the collector pods). A service for the DaemonSet collector which listens on the standard ports (4318 for HTTP and 4317 for gRPC) is also available.

The preferred way of sending OTLP from your workload to the Dash0-managed collector is to use node-local traffic via the host port. To do so, add the following environment variables to your workload:

```yaml
env:
  - name: K8S_NODE_IP
    valueFrom:
      fieldRef:
        fieldPath: status.hostIP
  - name: OTEL_EXPORTER_OTLP_ENDPOINT
    value: "http://$(K8S_NODE_IP):40318"
  - name: OTEL_EXPORTER_OTLP_PROTOCOL
    value: "http/protobuf"
```

> **Notes:**
> * Listing the definition for `K8S_NODE_IP` _before_ `OTEL_EXPORTER_OTLP_ENDPOINT` is crucial.
> * Adding `OTEL_EXPORTER_OTLP_PROTOCOL=http/protobuf` is optional when the OpenTelemetry SDK in question uses that protocol as the default.
> * For gRCP, use `OTEL_EXPORTER_OTLP_ENDPOINT=http://$(K8S_NODE_IP):40317` together with `OTEL_EXPORTER_OTLP_PROTOCOL=grpc` instead.

To use the service endpoint instead of the host port, you need to know:

* the Helm release name of the Dash0 operator (for example `dash0-operator`), and
* the namespace where the Dash0 operator is installed (for example `dash0-system`).

With that information at hand, add the following to your workload:

```yaml
env:
  - name: OTEL_EXPORTER_OTLP_ENDPOINT
    value: "http://${helm-release-name}-opentelemetry-collector-service.${namespace-of-the-dash0-operator}.svc.cluster.local:4318"
  - name: OTEL_EXPORTER_OTLP_PROTOCOL
    value: "http/protobuf"
```

or, for gRPC:

```yaml
env:
  - name: OTEL_EXPORTER_OTLP_ENDPOINT
    value: "http://${helm-release-name}-opentelemetry-collector-service.${namespace-of-the-dash0-operator}.svc.cluster.local:4317"
  - name: OTEL_EXPORTER_OTLP_PROTOCOL
    value: "grpc"
```

In both cases, `${helm-release-name}` and `${namespace-of-the-dash0-operator}` needs to be replaced with the actual values.

If the workload is in a namespace that is monitored by Dash0 and workload instrumentation is enabled, Dash0 will automatically add Kubernetes-related OpenTelemetry resource attributes to your telemetry, even if the runtime in question is not yet supported by Dash0's auto-instrumentation. There is currently one caveat: The resource attribute auto-detection relies on the process or runtime in question to use dynamic linking at startup (that is, binding to a flavor of libc), which is true for almost all runtimes. One notable exception are so called freestanding a.k.a. libc-free binaries, for example most binaries built with Go.

## How Automatic Workload Instrumentation Works

The modifications that are performed for workloads are the following, depending on the chosen [instrumentation delivery mechanism](#using-image-volumes-for-auto-instrumentation-files).

With instrumentation delivery `image-volume`:
* Add an [`image` volume](https://kubernetes.io/docs/concepts/storage/volumes/#image) named `dash0-instrumentation` to the pod spec, which contains the instrumentation files

With instrumentation delivery `init-container`:
* Add an [`emptyDir` volume](https://kubernetes.io/docs/concepts/storage/volumes/#emptydir) named `dash0-instrumentation` to the pod spec
* Add an [init container](https://kubernetes.io/docs/concepts/workloads/pods/init-containers/) named `dash0-instrumentation` that will copy the OpenTelemetry SDKs and distributions for supported runtimes to the `dash0-instrumentation` volume mount, so they are available in the target container's file system
* Add the `cluster-autoscaler.kubernetes.io/safe-to-evict-local-volumes=dash0-instrumentation` annotation to the pod spec

Regardless of the instrumentation delivery:
* Add a volume mount `dash0-instrumentation` to all containers of the pod
* Add environment variables (`OTEL_EXPORTER_OTLP_ENDPOINT`, `OTEL_EXPORTER_OTLP_PROTOCOL`, `LD_PRELOAD`, and several Dash0-specific variables prefixed with `DASH0_`) to all containers of the pod
* Add the [OpenTelemetry injector](https://github.com/open-telemetry/opentelemetry-injector) (see below for details) as a startup hook (via the `LD_PRELOAD` environment variable) to all containers of the pod
* Add the following labels to the workload metadata:
    * `dash0.com/instrumented`: `true` or `false` depending on whether the workload has been successfully instrumented or not
    * `dash0.com/operator-image`: the fully qualified name of the Dash0 operator image that has instrumented this workload
    * `dash0.com/instrumentation-image`: the fully qualified name of the image that has been used to deliver instrumentation files to the workload
    * `dash0.com/instrumented-by`: either `controller` or `webhook`, depending on which component has instrumented this workload. The controller is responsible for instrumenting existing workloads while the webhook is responsible for instrumenting new workloads at deploy time.
* Add the following annotations to the workload metadata:
    * `dash0.com/instrumented-by`: either `controller` or `webhook`, depending on which component has instrumented this workload. The controller is responsible for instrumenting existing workloads while the webhook is responsible for instrumenting new workloads at deploy time.

> **Notes:**
>
> * Automatic tracing will only happen for [supported runtimes](../README.md#supported-runtimes). Nonetheless, the modifications outlined above are performed for every workload. One reason for that is that there is no way to tell which runtime a workload uses from the outside, e.g. on the Kubernetes level. The more important reason is that runtimes that are not (yet) supported for auto-instrumentation still benefit from the improved OpenTelemetry resource attribute detection.
> * The operator will add neither `OTEL_EXPORTER_OTLP_ENDPOINT` nor `OTEL_EXPORTER_OTLP_PROTOCOL` to containers that already have at least one of those environment variables set. A Kubernetes event of type `Warning` is created for workloads with affected containers.
> * The operator sets `OTEL_EXPORTER_OTLP_ENDPOINT=http://$(NODE_IP):40318`, that is, it tells the workload to send OTLP traffic to the HTTP port of the OpenTelemetry collector pod on the same host, which belongs to the OpenTelemetry collector DaemonSet managed by the operator. It also sets the protocol accordingly by setting `OTEL_EXPORTER_OTLP_PROTOCOL=http/protobuf`. The protocol `http/protobuf` is the recommended default according to the [OpenTelemetry specification](https://opentelemetry.io/docs/specs/otel/protocol/exporter/#specify-protocol), and it is widely supported. It does so under the assumption that workloads which have an OpenTelemetry SDK use an SDK that respects `OTEL_EXPORTER_OTLP_PROTOCOL` and also has support for the `http/protobuf` protocol. For workloads that have an OpenTelemetry SDK that either does not respect `OTEL_EXPORTER_OTLP_PROTOCOL` (and defaults to `grpc`) or does not have support for `http/protobuf`, this will lead to the SDK trying to establish a gRPC connection to the collector's HTTP endpoint, that is, the SDK will not be able to emit telemetry. SDKs without support for `http/protobuf` are rather rare, but one prominent example is the Kubernetes [ingress-nginx](https://kubernetes.github.io/ingress-nginx/user-guide/third-party-addons/opentelemetry/). The recommended approach is to either set `OTEL_EXPORTER_OTLP_ENDPOINT` manually to the gRPC port or to disable workload instrumentation by the Dash0 operator for these workloads. To set `OTEL_EXPORTER_OTLP_ENDPOINT` manually, you can add the following entries to the container's `env` section:
>   ```
>   - name: MY_NODE_IP
>     valueFrom:
>       fieldRef:
>         fieldPath: status.hostIP
>   - name: OTEL_EXPORTER_OTLP_ENDPOINT
>     value: http://$(MY_NODE_IP):40317
>   ```
>   To disable workload instrumentation for a workload, you can opt out of auto-instrumentation via a workload label (i.e. `dash0.com/enable: "false"`, see [Disabling Auto-Instrumentation for Specific Workloads](#disabling-auto-instrumentation-for-specific-workloads)), or by not installing a Dash0 monitoring resource in the namespace where these workloads are located. The workloads can then be monitored by following the setup described in [Sending Data to the OpenTelemetry Collectors Managed by the Dash0 Operator](#sending-data-to-the-opentelemetry-collectors-managed-by-the-dash0-operator) to have the workload send telemetry to the collectors managed by the Dash0 operator, using gRPC. Note that this is not relevant for workloads that do not have an OpenTelemetry SDK at all, since they will ignore `OTEL_EXPORTER_OTLP_ENDPOINT`. In case the Dash0 operator Helm chart has been deployed with `operator.collectors.forceUseServiceUrl=true` or `operator.collectors.disableHostPorts=true`, `OTEL_EXPORTER_OTLP_ENDPOINT` is not set to `http://$(NODE_IP):40318`, but to the HTTP port of the DaemonSet collector's service URL `http://${helm-release-name}-opentelemetry-collector-service.${namespace-of-the-dash0-operator}.svc.cluster.local:4318` instead.

### Technical Details

The remainder of this section provides a more detailed step-by-step description of how the Dash0 operator's workload instrumentation for tracing works internally, intended for the technically curious reader. You can safely skip this section if you are not interested in the technical details.

1. The Dash0 operator adds the `dash0-instrumentation` init container with the [Dash0 instrumentation image](https://github.com/dash0hq/dash0-operator/tree/main/images/instrumentation) to the pod spec template of workloads. The instrumentation image contains OpenTelemetry SDKs and distributions for all supported runtimes and the [OpenTelemetry injector](https://github.com/open-telemetry/opentelemetry-injector) binary.
2. When the init container starts, it copies the OpenTelemetry distributions and the OpenTelemetry injector binary to a dedicated shared volume mount that has been added by the operator, so they are available in the target container's file system. When it has copied all files, the init container exits.
3. The operator also adds environment variables to the target container to ensure that the OpenTelemetry SDK has the correct configuration and will get activated at startup. The activation of the OpenTelemetry SDK happens via an `LD_PRELOAD` hook. For that purpose, the Dash0 operator adds the `LD_PRELOAD` environment variable to the pod spec template of the workload. `LD_PRELOAD` is an environment variable that is evaluated by the [dynamic linker/loader](https://man7.org/linux/man-pages/man8/ld.so.8.html) when a Linux executable starts. In general, it specifies a list of additional shared objects to be loaded before the actual code of the executable. In this specific case, the OpenTelemetry injector binary is added to the `LD_PRELOAD` list.
4. At process startup, the OpenTelemetry injector adds additional environment variables to the running process by hooking into the application startup, finding the `dlsym` symbol and `setenv` symbols, and then calling `setenv` to add or modify environment variables (like `OTEL_RESOURCE_ATTRIBUTES`, `NODE_OPTIONS`, `JAVA_TOOL_OPTIONS` and others). The reason for doing that at process startup and not when modifying the pod spec (where environment variables can also be added and modified) is that the original environment variables are not necessarily fully known at that time. Workloads will sometimes set environment variables in their Dockerfile or in an entrypoint script; those environment variables are only available at process runtime. For example, the OpenTelemetry injector sets (or appends to) `NODE_OPTIONS` to activate the [Dash0 OpenTelemetry distribution for Node.js](https://github.com/dash0hq/opentelemetry-js-distribution) to collect tracing data from all Node.js workloads. For JVMs, the same is achieved by setting (or appending to) the `JAVA_TOOL_OPTIONS` environment variable, namely adding a `-javaagent`). For .NET or other CLR-based workloads, the `CORECLR_PROFILER` mechanism is used to add the OpenTelemetry .NET instrumentation. For Python auto-instrumentation, the OpenTelemetry SDK is prepended to `PYTHONPATH`. (Python auto-instrumentation needs to be [enabled](https://github.com/dash0hq/dash0-operator/blob/main/helm-chart/dash0-operator/values.yaml) explicitly via Helm.)
5. The OpenTelemetry injector also automatically improves Kubernetes-related resource attributes as follows: The operator sets the environment variables `OTEL_INJECTOR_K8S_NAMESPACE_NAME`, `OTEL_INJECTOR_K8S_POD_NAME`, `OTEL_INJECTOR_K8S_POD_UID` and `OTEL_INJECTOR_K8S_CONTAINER_NAME` on workloads. The OpenTelemetry injector binary picks these values up and uses them to populate the resource attributes `k8s.namespace.name`, `k8s.pod.name`, `k8s.pod.uid` and `k8s.container.name` via the `OTEL_RESOURCE_ATTRIBUTES` environment variable. If `OTEL_RESOURCE_ATTRIBUTES` is already set on the process, the key-value pairs for these attributes are appended to the existing value of `OTEL_RESOURCE_ATTRIBUTES`. If `OTEL_RESOURCE_ATTRIBUTES` was not set on the process, the OpenTelemetry injector will add `OTEL_RESOURCE_ATTRIBUTES` as a new environment variable.
