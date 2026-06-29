# Installation Guide

This guide covers the installation of the Dash0 operator Helm chart in detail.

## Table of Contents

- [Prerequisites](#prerequisites)
- [Basic Installation](#basic-installation)
- [Installation with Secret Reference](#installation-with-secret-reference)
- [Installation without Backend Configuration](#installation-without-backend-configuration)

## Prerequisites

* [Kubernetes](https://kubernetes.io/) 1.25.16 or later.
* [Helm](https://helm.sh) >= 3.x, please refer to [Helm's documentation](https://helm.sh/docs/) for more information on
  installing Helm.

To use the operator, you will need provide two configuration values:

* **`endpoint`**: The URL of the Dash0 ingress endpoint backend to which telemetry data will be sent.
  This property is mandatory when installing the operator.
  This is the OTLP/gRPC endpoint of your Dash0 organization.
  The correct OTLP/gRPC endpoint can be copied from https://app.dash0.com → organization settings → "Endpoints" →
  "OTLP/gRPC".
  Note that the correct endpoint value will always start with `ingress.` and end in `dash0.com:4317`.
  Including a protocol prefix (e.g. `https://`) is optional.
* Either **`token`** or **`secretRef`**: Exactly one of these two properties needs to be provided when installing the
  operator.
    * **`token`**: This is the Dash0 authorization token of your organization.
      The authorization token for your Dash0 organization can be copied from https://app.dash0.com → organization
      settings → "Auth Tokens".
      The prefix `Bearer ` must not be included in the value.
      Note that when you provide a token, it will be rendered verbatim into a Kubernetes ConfigMap object.
      Anyone with API access to the Kubernetes cluster will be able to read the value.
      Use a secret reference and a Kubernetes secret if you want to avoid that.
    * **`secretRef`**: A reference to an existing Kubernetes secret in the Dash0 operator's namespace.
      The secret needs to contain the Dash0 authorization token.
      See [Using a Kubernetes Secret](configuration.md#using-a-kubernetes-secret-for-the-dash0-authorization-token) for details on how exactly the secret should be created and configured.

## Basic Installation

Before installing the operator, add the Dash0 operator's Helm repository as follows:

```console
helm repo add dash0-operator https://dash0hq.github.io/dash0-operator
helm repo update dash0-operator
```

Now you can install the operator into your cluster via Helm with the following command:

```console
helm install \
  --wait \
  --namespace dash0-system \
  --create-namespace \
  --set operator.dash0Export.enabled=true \
  --set operator.dash0Export.endpoint=REPLACE THIS WITH YOUR DASH0 INGRESS ENDPOINT \
  --set operator.dash0Export.apiEndpoint=REPLACE THIS WITH YOUR DASH0 API ENDPOINT \
  --set operator.dash0Export.token=REPLACE THIS WITH YOUR DASH0 AUTH TOKEN \
  --set operator.clusterName=REPLACE THIS WITH YOUR THE NAME OF THE CLUSTER (OPTIONAL) \
  dash0-operator \
  dash0-operator/dash0-operator
```

You can consult the chart's
[values.yaml](https://github.com/dash0hq/dash0-operator/blob/main/helm-chart/dash0-operator/values.yaml) file for a
complete list of available configuration settings.

## Installation with Secret Reference

Instead of providing the auth token directly, you can also use a secret reference:

```console
helm install \
  --wait \
  --namespace dash0-system \
  --create-namespace \
  --set operator.dash0Export.enabled=true \
  --set operator.dash0Export.endpoint=REPLACE THIS WITH YOUR DASH0 INGRESS ENDPOINT \
  --set operator.dash0Export.apiEndpoint=REPLACE THIS WITH YOUR DASH0 API ENDPOINT \
  --set operator.dash0Export.secretRef.name=REPLACE THIS WITH THE NAME OF AN EXISTING KUBERNETES SECRET \
  --set operator.dash0Export.secretRef.key=REPLACE THIS WITH THE PROPERTY KEY IN THAT SECRET \
  --set operator.clusterName=REPLACE THIS WITH YOUR THE NAME OF THE CLUSTER (OPTIONAL) \
  dash0-operator \
  dash0-operator/dash0-operator
```

See [Using a Kubernetes Secret](configuration.md#using-a-kubernetes-secret-for-the-dash0-authorization-token)
for more information on using a Kubernetes secrets with the Dash0 operator.

## Installation without Backend Configuration

You can also install the operator without providing a Dash0 backend configuration:

```console
helm install \
  --wait \
  --namespace dash0-system \
  --create-namespace \
  dash0-operator \
  dash0-operator/dash0-operator
```

However, you will need to create a Dash0 operator configuration resource later that provides the backend connection
settings.
That is, providing `--set operator.dash0Export.enabled=true` and the other backend-related settings when running
`helm install` is simply a shortcut to deploy the Dash0 operator configuration resource automatically at startup.

On its own, the operator will only collect Kubernetes metrics.
To actually have the operator properly monitor your workloads, two more things need to be set up:

1. A [Dash0 backend connection](configuration.md#configuring-the-dash0-backend-connection) has to be configured (unless you did that
   already with the Helm values `operator.dash0Export.*`), and
2. Monitoring namespaces and their workloads to collect logs, traces and metrics has to be
   [enabled per namespace](configuration.md#enable-dash0-monitoring-for-a-namespace), or configure namespace auto-monitoring.

Both steps are described in [Configuration](configuration.md).

See [Notes on Creating the Operator Configuration Resource Via Helm](configuration.md#notes-on-creating-the-operator-configuration-resource-via-helm) for more information on providing Dash0 export settings via Helm and how it affects manual changes to the operator configuration resource.

For Prometheus CRD support (ServiceMonitor, PodMonitor, ScrapeConfig), see [Support for Prometheus CRDs](metrics-and-scraping.md#support-for-prometheus-crds).

## Next Steps

After installation, proceed to:
- [Configuration](configuration.md) - Configure the Dash0 backend connection and enable namespace monitoring
- [Auto-Instrumentation](auto-instrumentation.md) - Learn about automatic workload instrumentation
- [Metrics and Scraping](metrics-and-scraping.md) - Configure metrics collection and Prometheus scraping
