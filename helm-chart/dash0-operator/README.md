# Dash0 Kubernetes Operator Helm Chart

[![License](https://img.shields.io/badge/License-Apache%202.0-blue.svg)](https://opensource.org/licenses/Apache-2.0)
[![Artifact Hub](https://img.shields.io/endpoint?url=https://artifacthub.io/badge/repository/opentelemetry-helm)](https://artifacthub.io/packages/search?repo=opentelemetry-helm)

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
$ helm repo add dash0-operator {TODO: add repository URL}
```

Then install the operator to your cluster like this:

```console
helm install dash0-operator dash0-operator ... TODO ...
```

## Upgrading

TODO

## Configuration

TODO

```yaml
...
```
