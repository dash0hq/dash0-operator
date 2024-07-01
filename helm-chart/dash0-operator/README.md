# Dash0 Kubernetes Operator Helm Chart

[![License](https://img.shields.io/badge/License-Apache%202.0-blue.svg)](https://opensource.org/licenses/Apache-2.0)
[![Artifact Hub](https://img.shields.io/endpoint?url=https://artifacthub.io/badge/repository/dash0-operator)](https://artifacthub.io/packages/search?repo=dash0-operator)

This repository contains the [Helm](https://helm.sh/) chart for the Dash0 Kubernetes operator.

The Dash0 Kubernetes Operator makes observability for Kubernetes _easy_.
Simply install the operator into your cluster to get OpenTelemetry data flowing from your Kubernetes workloads to Dash0.

## Description

The Dash0 Kubernetes operator installs an OpenTelemetry collector into your cluster that sends data to your Dash0
ingress endpoint, with authentication already configured out of the box. Additionally, it will enable gathering
OpenTelemetry data from applications deployed to the cluster for a selection of supported runtimes.

More information on the Dash0 Kubernetes Operator can be found at 
https://github.com/dash0hq/dash0-operator/blob/main/README.md.

## Prerequisites

- [Kubernetes](https://kubernetes.io/) >= 1.xx
- [Helm](https://helm.sh) >= 3.xx, please refer to Helm's [documentation](https://helm.sh/docs/) for more information 
  on installing Helm.

## Installation

Before installing the operator, add the required Helm repositories as follows:

```console
helm repo add dash0-operator https://dash0hq.github.io/dash0-operator
helm repo add open-telemetry https://open-telemetry.github.io/opentelemetry-helm-charts
helm repo update
```

Create the namespace for the operator and a secret with your Dash0 authorization token.
The authorization token for your Dash0 organization can be copied from https://app.dash0.com/settings.
Make sure to use `dash0-authorization-secret` for the name of the secret and `dash0-authorization-token` as the token
key.

```console
kubectl create namespace dash0-operator-system
kubectl create secret generic \
  dash0-authorization-secret \
  --namespace dash0-operator-system \
  --from-literal=dash0-authorization-token=auth_...your-token-here...
```

Now you can install the operator into your cluster via Helm with the following command.
Use the same namespace that you have created in the previous step and which contains the `dash0-authorization-secret`. 
You will need to replace the value given to `--set opentelemetry-collector.config.exporters.otlp.endpoint` with the
actual OTLP/gRPC endpoint of your Dash0 organization.
Again, the correct OTLP/gRPC endpoint can be copied fom https://app.dash0.com/settings.
The correct endpoint value will always start with `ingress.` and end in `dash0.com:4317`.

```console
helm install \
  --namespace dash0-operator-system \
  --set opentelemetry-collector.config.exporters.otlp.endpoint=your-dash0-ingress-endpoint-here.dash0.com:4317 \
  dash0-operator \
  dash0-operator/dash0-operator  
```

Note: When installing the chart, you might see a warning like the following printed to the console:
```
coalesce.go:286: warning: cannot overwrite table with non table for dash0-operator.opentelemetry-collector.config.receivers.prometheus (map[config:map[scrape_configs:[map[job_name:opentelemetry-collector scrape_interval:10s static_configs:[map[targets:[${env:MY_POD_IP}:8888]]]]]]])
coalesce.go:286: warning: cannot overwrite table with non table for dash0-operator.opentelemetry-collector.config.receivers.zipkin (map[endpoint:${env:MY_POD_IP}:9411])
coalesce.go:286: warning: cannot overwrite table with non table for dash0-operator.opentelemetry-collector.config.receivers.jaeger (map[protocols:map[grpc:map[endpoint:${env:MY_POD_IP}:14250] thrift_compact:map[endpoint:${env:MY_POD_IP}:6831] thrift_http:map[endpoint:${env:MY_POD_IP}:14268]]])
```

These warnings can be safely ignored.

Note that Dash0 has to be enabled per namespace that you want to monitor, which is described in the next section.

## Enable Dash0 For a Namespace

For each namespace that you want to monitor with Dash0, enable monitoring by installing a custom Dash0 resource:

Create a file `dash0.yaml` with the following content:
```yaml
apiVersion: operator.dash0.com/v1alpha1
kind: Dash0
metadata:
  name: dash0-resource
```

Then apply the resource to the namespace you want to monitor:
```console
kubectl apply --namespace my-namespace -f dash0.yaml
```

## Disable Dash0 For a Namespace

If you want to stop monitoring a namespace with Dash0, remove the Dash0 resource from that namespace.

```console
kubectl delete --namespace my-namespace Dash0 dash0-resource
```

or, alternatively, by using the `dash0.yaml` file created earlier:

```console
kubectl delete --namespace test-namespace -f dash0.yaml
```

## Uninstallation

To remove the Dash0 Kubernetes Operator from your cluster, run the following command:

```
helm uninstall dash0-operator --namespace dash0-operator-system
```

Depending on the command you used to install the operator, you may need to use a different Helm release name or
namespace.

This will also automatically disable Dash0 monitoring for all namespaces.

Optionally, remove the namespace that has been created for the operator:

```
kubectl delete namespace dash0-operator-system
```

If you choose to not remove the namespace, you might consider removing the secret with the Dash0 authorization token:

```console
kubectl delete secret --namespace dash0-operator-system dash0-authorization-secret
```