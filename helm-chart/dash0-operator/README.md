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

Before installing the operator, add the Helm repository as follows:

```console
helm repo add dash0-operator https://dash0hq.github.io/dash0-operator
helm repo update
```

Create a file named `dash0-operator-values.yaml` with the following content:

```yaml
#
# Note: The required values for the authorization token and OTLP/gRPC endpoint
# can be copied from https://app.dash0.com/settings (you need to be logged in).
#

opentelemetry-collector:
  config:
    extensions:
      bearertokenauth/dash0:
        token: "auth_..." # <- put your actual Dash0 Authorization Token here

    exporters:
      otlp:
        auth:
          authenticator: bearertokenauth/dash0
        endpoint: "ingress.XX-XXXX-X.aws.dash0.com:4317" # <- pur your actual OTLP/gRPC endpoint here

    service:
      extensions:
        - health_check
        - memory_ballast
        - bearertokenauth/dash0
```

Edit the file to replace the placeholders for the token and the endpoint.
The required values can be found in the Dash0 web application at https://app.dash0.com/settings.

Then install the operator to your cluster via Helm with the following command:

```console
helm install \
  --namespace dash0-operator-system \
  --create-namespace \
  --values dash0-operator-values.yaml \
  dash0-operator \
  dash0-operator/dash0-operator  
```

Note: When installing a chart, you might see a warning like the following printed to the console:
```
coalesce.go:286: warning: cannot overwrite table with non table for dash0-operator.opentelemetry-collector.config.receivers.prometheus (map[config:map[scrape_configs:[map[job_name:opentelemetry-collector scrape_interval:10s static_configs:[map[targets:[${env:MY_POD_IP}:8888]]]]]]])
coalesce.go:286: warning: cannot overwrite table with non table for dash0-operator.opentelemetry-collector.config.receivers.zipkin (map[endpoint:${env:MY_POD_IP}:9411])
coalesce.go:286: warning: cannot overwrite table with non table for dash0-operator.opentelemetry-collector.config.receivers.jaeger (map[protocols:map[grpc:map[endpoint:${env:MY_POD_IP}:14250] thrift_compact:map[endpoint:${env:MY_POD_IP}:6831] thrift_http:map[endpoint:${env:MY_POD_IP}:14268]]])
```

This can be safely ignored.

## Uninstallation

To remove the Dash0 Kubernetes Operator from your cluster, run the following command:

```
helm uninstall dash0-operator --namespace dash0-operator-system
```

Depending on the command you used to install the operator, you may need to use a different Helm release name or
namespace.

Optionally, remove the namespace that has been created for the operator:

```
kubectl delete namespace dash0-operator-system
```
