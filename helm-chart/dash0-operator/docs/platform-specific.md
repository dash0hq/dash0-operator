# Platform-Specific Notes and Compatibility

This document provides platform-specific guidance, compatibility notes, and workarounds for running the Dash0 operator on various Kubernetes distributions and environments.

## Table of Contents

- [AWS EKS](#notes-on-aws-eks)
- [GKE Autopilot](#notes-on-gke-autopilot)
  - [Managing the AllowlistSynchronizer Manually](#managing-the-allowlistsynchronizer-manually)
- [OpenShift](#notes-on-openshift)
- [SELinux-Enforcing Clusters](#notes-on-selinux-enforcing-clusters)
- [Azure AKS](#notes-on-azure-aks)
- [Open Policy Agent (OPA Gatekeeper)](#notes-on-the-open-policy-agent)
- [Kyverno Admission Controller](#notes-on-kyverno-admission-controller)
- [GitOps Deployments](#notes-on-gitops)
- [ArgoCD](#notes-on-argocd)
- [Local Development Environments](#local-development-environments)
  - [Apple Silicon](#notes-on-running-the-operator-on-apple-silicon)
  - [Docker Desktop](#notes-on-running-the-operator-on-docker-desktop)
  - [Minikube](#notes-on-running-the-operator-on-minikube)

## Notes on AWS EKS

If your telemetry from an AWS EKS cluster is missing `cloud.provider`, `cloud.platform` and other `cloud.*` resource
attributes, refer to the [resource detection processor documentation](https://github.com/open-telemetry/opentelemetry-collector-contrib/blob/main/processor/resourcedetectionprocessor/README.md#amazon-eks).
In particular, make sure that [IMDS](https://docs.aws.amazon.com/AWSEC2/latest/UserGuide/ec2-instance-metadata.html) is
available on your EKS nodes.

## Notes on GKE Autopilot

When deploying the Dash0 operator to a GKE Autopilot cluster, provide the following additional setting when applying the
Helm chart:

```yaml
operator:
  gke:
    autopilot:
      enabled: true
```

GKE Autopilot [restricts](https://cloud.google.com/kubernetes-engine/docs/concepts/autopilot-security) what workloads
in an autopilot clusters can do.
With `operator.gke.autopilot.enabled` set to `true`, the Dash0 operator Helm chart deploys an
`auto.gke.io/AllowlistSynchronizer` resource into the target cluster, which in turn will add the required
`auto.gke.io/WorkloadAllowlist` resources for Dash0 workloads (the operator and the OpenTelemetry collectors it
manages).
This allows the Dash0 operator to work on GKE Autopilot clusters.

Not all restrictions can be lifted via workload allowlist, the following features are not available on GKE Autopilot
clusters:
- collecting utilization metrics with the `kubeletstats` receiver is disabled; collecting these requires access to the
  `/pod` endpoint of the kubelet API which is not available in GKE autopilot due to the lack of the `nodes/proxy`
  permission:
    - `k8s.pod.cpu_limit_utilization`,
    - `k8s.pod.cpu_request_utilization`,
    - `k8s.pod.memory_limit_utilization`, and
    - `k8s.pod.memory_request_utilization`
- collecting the extra metadata labels `container.id` and `k8s.volume.type` for the `kubeletstats` receiver metrics is
  disabled, collecting these requires access to the `/pod` endpoint of the kubelet API which is not available in GKE
  autopilot due to the lack of the `nodes/proxy` permission
- the [synthetics-worker](synthetics-worker.md) is not yet supported, since its container image has not been added to
  the GKE Autopilot workload allowlist

GKE Autopilot [adjusts](https://cloud.google.com/kubernetes-engine/docs/concepts/autopilot-resource-requests) the
resource requests and limits of all workloads, including the OpenTelemetry collectors and the other workloads managed by
the operator.
The adjustment is recorded in the `autopilot.gke.io/resource-adjustment` annotation of the workload.
The operator accepts the adjusted values and does not revert them.
When the resource settings in the Helm values are changed, the operator applies the new values, which GKE Autopilot then
adjusts again.

When the Dash0 operator Helm chart is uninstalled, a post-delete hook removes the `AllowlistSynchronizer` deployed by
the Helm chart, as well as the `WorkloadAllowlist` resources it has installed.
If `WorkloadAllowlist` resources are still left after a timeout of a few minutes, the hook logs a warning and
terminates without an error.
Leftover `WorkloadAllowlist` resources of the Dash0 `AllowlistSynchronizer` can be removed manually once the
`AllowlistSynchronizer` is gone:
```
kubectl delete workloadallowlists.auto.gke.io -l auto.gke.io/managed-by-synchronizer-dash0-allowlist-synchronizer=true
```

Refer to <https://cloud.google.com/kubernetes-engine/docs/how-to/run-autopilot-partner-workloads> for more information
on `AllowlistSynchronizer`, `WorkloadAllowlist`, and related concepts.

### Managing the AllowlistSynchronizer Manually

As an alternative to letting the Helm chart install the `AllowlistSynchronizer`, you can also choose to manage this
manually, if you prefer:

```yaml
operator:
  gke:
    autopilot:
      enabled: true
      deployAllowlistSynchronizer: false
```

With these settings, the Dash0 operator Helm chart will not deploy the `AllowlistSynchronizer`.
Using these settings requires that you deploy the Dash0 `AllowlistSynchronizer` before installing the Dash0 operator.
To do that, create the following file `dash0-gke-autopilot-allowlist-synchronizer.yaml`:
```yaml
apiVersion: auto.gke.io/v1
kind: AllowlistSynchronizer
metadata:
  name: dash0-allowlist-synchronizer
spec:
  allowlistPaths:
    - Dash0/operator/*
```

Then deploy it as follows:
```
kubectl apply -f dash0-gke-autopilot-allowlist-synchronizer.yaml
```

When managing the `AllowlistSynchronizer` manually, you might need to update it from time to time for future Dash0
operator releases.

## Notes on OpenShift

When deploying the Dash0 operator to a Red Hat OpenShift cluster, provide the following additional setting when applying
the Helm chart:

```yaml
operator:
  openShift:
    enabled: true
```

OpenShift enforces [Security Context Constraints](https://docs.openshift.com/container-platform/latest/authentication/managing-security-context-constraints.html)
(SCCs) on every pod. The default `restricted-v2` SCC assigns each pod a random, in-range UID from the namespace's
`openshift.io/sa.scc.uid-range` and rejects pods that pin a UID outside that range. With `operator.openShift.enabled`
set to `true`, the Dash0 operator Helm chart and operator:

- create a custom `SecurityContextConstraints` resource plus the RBAC that binds it to the OpenTelemetry collector
  DaemonSet's service account, so the DaemonSet may use the host access it requires (host log directories via `hostPath`
  and the OTLP host ports),
- drop the hard-coded pod-level `runAsUser`/`runAsGroup` from the operator-managed workloads (the agent0-connector, the
  target-allocator, and the Signal Control edge-proxy) and from the injected instrumentation init container, so that the
  namespace's SCC assigns an in-range UID instead of a pinned one that `restricted-v2` would reject, and
- set the SELinux type `spc_t` on the collector DaemonSet pods. OpenShift nodes enforce SELinux, and the default
  container type `container_t` is not allowed to read the pod log files in `/var/log/pods`. A value set via
  `operator.collectors.daemonSetSeLinuxOptions` replaces this default, `{}` removes it (see
  [Configuring SELinux Options for the Collector DaemonSet](advanced-configuration.md#configuring-selinux-options-for-the-collector-daemonset)).

## Notes on SELinux-Enforcing Clusters

For clusters that enforce SELinux, the SELinux options of the collector DaemonSet pods can be configured (see
[Configuring SELinux Options for the Collector DaemonSet](advanced-configuration.md#configuring-selinux-options-for-the-collector-daemonset)).

## Notes on Azure AKS

In [AKS](https://azure.microsoft.com/products/kubernetes-service) clusters that have the [Azure Policy add-on](https://learn.microsoft.com/azure/aks/use-azure-policy) enabled, it is highly recommended to [use a volume for filelog offsets](advanced-configuration.md#providing-a-filelog-offset-volume) instead of the default filelog offset config map.

Using the default config map filelog offset storage in AKS clusters with this add-on can lead to severe performance issues.

## Notes on the Open Policy Agent

In clusters that have the [OPA gatekeeper](https://github.com/open-policy-agent/gatekeeper) deployed, it is highly recommended to [use a volume for filelog offsets](advanced-configuration.md#providing-a-filelog-offset-volume) instead of the default filelog offset config map.

Using the default config map filelog offset storage in clusters with this component can lead to severe performance issues.

## Notes on Kyverno Admission Controller

In clusters that have the [Kyverno admission controller](https://kyverno.io/docs/introduction/how-kyverno-works/#kubernetes-admission-controls) deployed, it is highly recommended to either:

1. [Use a volume for filelog offsets](advanced-configuration.md#providing-a-filelog-offset-volume) instead of the default filelog offset config map, or
2. [Exclude](https://kyverno.io/docs/installation/customization/#resource-filters) ConfigMaps (or all resource types) in the Dash0 operator's namespace from Kyverno's processing.

Leaving Kyverno processing in place and using the config map filelog offset storage can lead to severe performance issues, since the default config map for filelog offsets is updated very frequently. This can cause Kyverno to consume a lot of CPU and memory resources, potentially even leading to OOMKills of the Kyverno admission controller.

## Notes on GitOps

When deploying workloads via GitOps tools like ArgoCD or Flux in a cluster where the Dash0 operator is installed, some care needs to be exercised to not create conflicts between the workload definition in the GitOps repository and the [workload modifications](auto-instrumentation.md#how-automatic-workload-instrumentation-works) that are applied automatically by the Dash0 operator.

Otherwise, workload settings might flip-flop between what the GitOps system wants to apply and what the Dash0 operator does, or the GitOps system might overwrite the Dash0 operator's settings, thereby breaking telemetry collection for the workload.

### Environment Variables to Avoid in GitOps

Environment variable definitions in pod spec templates are the most likely source of conflict. To avoid conflicts, it is recommended to not define the following environment variables via GitOps:

* `OTEL_EXPORTER_OTLP_ENDPOINT`
* `OTEL_EXPORTER_OTLP_PROTOCOL`
* `OTEL_PROPAGATORS`
* `LD_PRELOAD`
* `DASH0_NODE_IP`
* `DASH0_OTEL_COLLECTOR_BASE_URL`
* `OTEL_INJECTOR_K8S_NAMESPACE_NAME`
* `OTEL_INJECTOR_K8S_POD_NAME`
* `OTEL_INJECTOR_K8S_POD_UID`
* `OTEL_INJECTOR_K8S_CONTAINER_NAME`
* `OTEL_INJECTOR_SERVICE_NAME`
* `OTEL_INJECTOR_SERVICE_NAMESPACE`
* `OTEL_INJECTOR_SERVICE_VERSION`
* `OTEL_INJECTOR_RESOURCE_ATTRIBUTES`

This recommendation does not apply to workloads that are [excluded from workload instrumentation](auto-instrumentation.md#disabling-auto-instrumentation-for-specific-workloads) or workloads in namespaces without a [Dash0 monitoring resource](configuration.md#enable-dash0-monitoring-for-a-namespace) or a monitoring resource with instrumentation disabled.

## Notes on ArgoCD

Applications that have been installed with version 0.158.0 or later do not need any `ignoreDifferences` entries for the
certificates.

### Upgrading to Version 0.158.0

Up to version 0.157.0, the Helm chart generated new certificates every time it was rendered.
When upgrading from such a version, ArgoCD removes the `caBundle` values from the webhook configurations and from the
`dash0monitorings.operator.dash0.com` CRD.
Until the new operator pod has written its own CA bundle, requests to the operator's webhooks fail: changes to
`Dash0OperatorConfiguration`, `Dash0Monitoring` and `Dash0SignalControl` resources are rejected, and workloads that are
created in this time might not be instrumented.
With the instrumentation mode `all`, these workloads are instrumented once the new operator pod has started, otherwise
at their next update.
Jobs and pods without an owner that are created in this time need to be created again.

To avoid this, add the following settings to the ArgoCD application together with the upgrade or shortly before it, and
keep them afterwards.
Once they have been applied, ArgoCD stores the `caBundle` values as part of the applied manifests, and removing the
settings later makes ArgoCD remove these values once.
Do not add them months before the upgrade, since they stop ArgoCD from replacing the certificate of the old version,
which expires 365 days after it has been generated.

```yaml
---
apiVersion: argoproj.io/v1alpha1
kind: Application
metadata:
  name: dash0-operator
  namespace: argocd
spec:

  # ... your current spec for the dash0-operator ArgoCD application

  syncPolicy:
    syncOptions:
      # Without this option, ArgoCD removes the caBundle values despite ignoreDifferences.
      - RespectIgnoreDifferences=true

  ignoreDifferences:
    - kind: Secret
      name: dash0-operator-certificates
      jsonPointers:
        - /data
    - group: admissionregistration.k8s.io
      kind: MutatingWebhookConfiguration
      name: dash0-operator-injector
      jsonPointers:
        - /webhooks/0/clientConfig/caBundle
    - group: admissionregistration.k8s.io
      kind: MutatingWebhookConfiguration
      name: dash0-operator-monitoring-mutating
      jsonPointers:
        - /webhooks/0/clientConfig/caBundle
    - group: admissionregistration.k8s.io
      kind: MutatingWebhookConfiguration
      name: dash0-operator-operator-configuration-mutating
      jsonPointers:
        - /webhooks/0/clientConfig/caBundle
    - group: admissionregistration.k8s.io
      kind: ValidatingWebhookConfiguration
      name: dash0-operator-monitoring-validator
      jsonPointers:
        - /webhooks/0/clientConfig/caBundle
    - group: admissionregistration.k8s.io
      kind: ValidatingWebhookConfiguration
      name: dash0-operator-operator-configuration-validator
      jsonPointers:
        - /webhooks/0/clientConfig/caBundle
    - group: admissionregistration.k8s.io
      kind: ValidatingWebhookConfiguration
      name: dash0-operator-signal-control-validator
      jsonPointers:
        - /webhooks/0/clientConfig/caBundle
    - group: apiextensions.k8s.io
      kind: CustomResourceDefinition
      name: dash0monitorings.operator.dash0.com
      jsonPointers:
        - /spec/conversion/webhook/clientConfig/caBundle
```

If the application already contains the `ignoreDifferences` entries from earlier versions of this documentation, add
`RespectIgnoreDifferences=true` and the entries for `dash0-operator-signal-control-validator` and the
`dash0monitorings.operator.dash0.com` CRD.
If pruning is enabled, ArgoCD deletes the Secret `dash0-operator-certificates` during the upgrade.
Should the new operator version not become ready, operator pods of the old version that are re-created then cannot
start, and requests to the webhooks fail until the application is synced back.
Once the upgrade has finished, you can delete the Secret `dash0-operator-certificates`.

Before syncing back to a version before 0.158.0, remove the `ignoreDifferences` entries listed above.

### Dash0 Resources in the Same Application

If the ArgoCD application also contains Dash0 resources, like a `Dash0Monitoring` resource in a wrapper chart, ArgoCD
can apply them before the operator's webhooks are ready, for example during the first installation, and the API server
rejects them.
To avoid this, assign the Dash0 resources to later sync waves and configure retries:

```yaml
# Dash0OperatorConfiguration resources
metadata:
  annotations:
    argocd.argoproj.io/sync-wave: "1"
---
# Dash0Monitoring resources and other Dash0 resources
metadata:
  annotations:
    argocd.argoproj.io/sync-wave: "2"
```

```yaml
spec:
  syncPolicy:
    retry:
      limit: 10
      backoff:
        duration: 10s
        factor: 2
        maxDuration: 3m
```

### Unsupported Sync Options

Do not use the sync options `Replace=true` or `Force=true` for the operator's resources, see
[Webhook Certificates](advanced-configuration.md#webhook-certificates).

## Local Development Environments

### Notes on Running The Operator on Apple Silicon

When running the operator on an Apple Silicon host (M1, M3 etc.), for example via Docker Desktop, some attention needs to be paid to the CPU architecture of images. The architecture of the Kubernetes node for this scenario will be `arm64`.

When running a single-architecture `amd64` image (as opposed to a single-architecture `arm64` image or a [multi-platform build](https://docs.docker.com/build/building/multi-platform/) containing `amd64` as well as `arm64`), the operator will prevent the container from starting.

#### Why This Happens

The reason for this is the interaction between Rosetta emulation and how the operator works:

1. The Dash0 instrumentation image (which is added as an init container and contains the OpenTelemetry injector) is a multi-platform image, supporting both `amd64` and `arm64`.
2. When this image is pulled from an Apple Silicon machine, it automatically pulls the `arm64` variant.
3. That is, the injector binary that is added via the init container is compiled for `arm64`.
4. Now, when the application from your `amd64` application image is started, the injector and the application will be incompatible, as they have been built for two different CPU architectures.

Under normal circumstances, an `amd64` image would not work on an `arm64` Kubernetes node anyway, but in the case of Docker Desktop on MacOS, this combination is enabled due to Docker Desktop automatically running `amd64` images via Rosetta2 emulation.

#### Workarounds

You can work around this issue by one of the following methods:

- Using an `amd64` Kubernetes node
- By building a multi-platform image for your application
- By building the application as an `arm64` image (e.g. by using `--platform=linux/arm64` when building the image)

### Notes on Running The Operator on Docker Desktop

The `hostmetrics` receiver will be disabled when using Docker as the container runtime.

### Notes on Running The Operator on Minikube

The `hostmetrics` receiver will be disabled when using Docker as the container runtime.

## See Also

- [Advanced Configuration](advanced-configuration.md) - For filelog offset volumes, cert-manager, and other advanced topics
- [Auto-Instrumentation](auto-instrumentation.md) - For workload instrumentation details
- [Configuration](configuration.md) - For general operator configuration
- [Troubleshooting](troubleshooting.md) - For debugging issues
