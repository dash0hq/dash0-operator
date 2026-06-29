# Dash0 Operator Helm Chart

[![License](https://img.shields.io/badge/License-Apache%202.0-blue.svg)](https://opensource.org/licenses/Apache-2.0)
[![Artifact Hub](https://img.shields.io/endpoint?url=https://artifacthub.io/badge/repository/dash0-operator)](https://artifacthub.io/packages/search?repo=dash0-operator)

This repository contains the [Helm](https://helm.sh/) chart for the Dash0 operator.

There is no faster or easier way to monitor your Kubernetes cluster and workloads than using the Dash0 operator for
Kubernetes.
It is built on open standards and tailored for the optimal user experience.
Simply install the operator into your cluster to get OpenTelemetry data flowing from your Kubernetes workloads to Dash0.

## Description

The Dash0 operator for Kubernetes installs an OpenTelemetry collector into your cluster that sends data to your Dash0
ingress endpoint, with authentication already configured out of the box.
It also gathers OpenTelemetry data from applications deployed to the cluster, including traces, logs and metrics.

## Supported Runtimes

Supported runtimes for automatic workload instrumentation:

* Java 8+
* Node.js 16+
* .NET
* Python ([opt-in](https://github.com/dash0hq/dash0-operator/blob/0.100.0/helm-chart/dash0-operator/values.yaml#L408-L409))

Metrics and log collection are independent of the runtime of workloads.

## Quick Start

### Prerequisites

* [Kubernetes](https://kubernetes.io/) 1.25.16 or later.
* [Helm](https://helm.sh) >= 3.x, please refer to [Helm's documentation](https://helm.sh/docs/) for more information on
  installing Helm.

You will need:
* **`endpoint`**: The OTLP/gRPC endpoint of your Dash0 organization (from https://app.dash0.com → organization settings → "Endpoints" → "OTLP/gRPC")
* **`token`** or **`secretRef`**: Your Dash0 authorization token (from https://app.dash0.com → organization settings → "Auth Tokens")

### Installation

Add the Dash0 operator's Helm repository:

```console
helm repo add dash0-operator https://dash0hq.github.io/dash0-operator
helm repo update dash0-operator
```

Install the operator:

```console
helm install \
  --wait \
  --namespace dash0-system \
  --create-namespace \
  --set operator.dash0Export.enabled=true \
  --set operator.dash0Export.endpoint=REPLACE THIS WITH YOUR DASH0 INGRESS ENDPOINT \
  --set operator.dash0Export.apiEndpoint=REPLACE THIS WITH YOUR DASH0 API ENDPOINT \
  --set operator.dash0Export.token=REPLACE THIS WITH YOUR DASH0 AUTH TOKEN \
  dash0-operator \
  dash0-operator/dash0-operator
```

For detailed installation instructions including secret references, Prometheus CRD support, and target-allocator configuration, see **[INSTALLATION.md](docs/INSTALLATION.md)**.

### Next Steps

After installation:

1. **Configure the Dash0 backend connection** (if not done via Helm) - see [CONFIGURATION.md](docs/CONFIGURATION.md)
2. **Enable monitoring for namespaces** - see [CONFIGURATION.md#enable-dash0-monitoring-for-a-namespace](docs/CONFIGURATION.md#enable-dash0-monitoring-for-a-namespace)

The operator will automatically instrument workloads in monitored namespaces. Learn more in **[AUTO-INSTRUMENTATION.md](docs/AUTO-INSTRUMENTATION.md)**.

## Documentation

This README provides a quick overview. Detailed documentation is organized by topic:

### Core Documentation

* **[INSTALLATION.md](docs/INSTALLATION.md)** - Comprehensive installation guide
* **[CONFIGURATION.md](docs/CONFIGURATION.md)** - Backend connections, namespace monitoring, secrets, datasets, and operator configuration
* **[AUTO-INSTRUMENTATION.md](docs/AUTO-INSTRUMENTATION.md)** - Workload instrumentation details, Python support, disabling instrumentation, and custom label selectors
* **[METRICS-AND-SCRAPING.md](docs/METRICS-AND-SCRAPING.md)** - Metrics collection, Prometheus endpoint scraping, and Prometheus CRD support
* **[PROFILING.md](docs/PROFILING.md)** - Profiling support and OpenTelemetry eBPF profiler setup

### Advanced Topics

* **[MANAGING-DASH0-RESOURCES.md](docs/MANAGING-DASH0-RESOURCES.md)** - Managing dashboards, check rules, synthetic checks, views, notification channels, spam filters, and signal-to-metrics via infrastructure-as-code
* **[ADVANCED-CONFIGURATION.md](docs/ADVANCED-CONFIGURATION.md)** - cert-manager, node affinity, tolerations, sysctls, and filelog offset volumes
* **[PLATFORM-SPECIFIC.md](docs/PLATFORM-SPECIFIC.md)** - Notes for AWS EKS, GKE Autopilot, Azure AKS, Docker Desktop, Minikube, Apple Silicon, and compatibility with OPA and Kyverno

### Operations

* **[UPGRADING.md](docs/UPGRADING.md)** - Upgrade procedures, CRD version migrations, and uninstallation
* **[TROUBLESHOOTING.md](docs/TROUBLESHOOTING.md)** - Common issues and debugging techniques

## Key Features

* **Automatic Workload Instrumentation** - Automatically add OpenTelemetry tracing to Java, Node.js, .NET, and Python applications
* **Kubernetes Metrics Collection** - Collect cluster, node, pod, and container metrics
* **Log Collection** - Gather pod logs and forward them to Dash0
* **Prometheus Scraping** - Scrape Prometheus endpoints and support for Prometheus CRDs (ServiceMonitor, PodMonitor, ScrapeConfig)
* **Infrastructure as Code** - Manage Dash0 dashboards, check rules, synthetic checks, and more via Kubernetes custom resources
* **Flexible Configuration** - Per-namespace configuration with filters, transformations, and export overrides
* **Auto-Namespace Monitoring** - Optionally monitor all namespaces automatically

## Configuration Values

You can consult the chart's [values.yaml](values.yaml) file for a complete list of available configuration settings.

## Support

For issues, questions, or feature requests, please contact Dash0 support at support@dash0.com.

## License

This project is licensed under the Apache License 2.0 - see the LICENSE file for details.
