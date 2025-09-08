# Dash0 Operator

[![License](https://img.shields.io/badge/License-Apache%202.0-blue.svg)](https://opensource.org/licenses/Apache-2.0)
[![Artifact Hub](https://img.shields.io/endpoint?url=https://artifacthub.io/badge/repository/dash0-operator)](https://artifacthub.io/packages/search?repo=dash0-operator)

The Dash0 Operator makes observability for Kubernetes _easy_.
Install the operator into your cluster and create a Dash0 monitoring resource to get OpenTelemetry data flowing from
your applications and infrastructure to Dash0.

The detailed documentation for the Dash0 operator is in the
[Helm chart documentation](helm-chart/dash0-operator/README.md), this README file only provides a brief overview of the
operator's features.

## Description

The Dash0 operator enables gathering OpenTelemetry data from your workloads for a selection of supported
runtimes, automatic log collection and metrics.

### Distributed tracing

Auto-instrumentation if supported for the following runtimes:

* Node.js 16+, using
  [Dash0 Node.js OpenTelemetry distribution](https://github.com/dash0hq/opentelemetry-js-distribution)
* Java 8+, using the [OpenTelemetry Java agent](https://github.com/open-telemetry/opentelemetry-java-instrumentation)

For more information on how the Dash0 operator automatically traces your applications, see the
[Automatic Workload Instrumentation](https://artifacthub.io/packages/helm/dash0-operator/dash0-operator#automatic-workload-instrumentation)
section of the Dash0 operator Helm chart documentation.

### Metrics

The Dash0 operator automatically collects cluster- and workload-related metrics, like node and pod cpu and memory usage.
It also supports scraping Prometheus endpoints exposed by pods according to the `prometheus.io/*` annotations defined by
the Prometheus Helm chart:

For more information on how the Dash0 operator scrapes Prometheus endpoints exposed by your applications, see the
[Scraping Prometheus endpoints](https://artifacthub.io/packages/helm/dash0-operator/dash0-operator#scraping-prometheus-endpoints)
section of the Dash0 operator Helm chart documentation.

### Logs

The Dash0 operator automatically collects pod logs from the pods that it monitors.

For more information on how to have pods monitored by the Dash0 operator, see the
[Enable Dash0 Monitoring For a Namespace](https://artifacthub.io/packages/helm/dash0-operator/dash0-operator#enable-dash0-monitoring-for-a-namespace) section of the Dash0 operator
Helm chart documentation.

### Alerting

The Dash0 operator supports the [`PrometheusRule` custom resource definition](https://github.com/prometheus-operator/prometheus-operator/blob/main/Documentation/api.md#monitoring.coreos.com/v1.PrometheusRule)
defined by the Prometheus operator.
The alert rules specified in `PrometheusRule` custom resources are used to create
[check rules](https://www.dash0.com/documentation/dash0/alerting/check-rules) in Dash0.

For more information on how the Dash0 operator synchronizes check rules based on `PrometheusRule` resources, consult the
[Managing Dash0 Check Rules](helm-chart/dash0-operator/README.md#managing-dash0-check-rules)
section of the Dash0 operator Helm chart documentation.

### Dashboards

The Dash0 operator supports the `PersesDashboard` custom resource definition defined by the
[Perses operator](https://github.com/perses/perses-operator).
The Perses dashboards specified in `PersesDashboard` custom resources are used to create
[dashboards](https://www.dash0.com/documentation/dash0/dashboards) in Dash0.

For more information on how the Dash0 operator synchronizes dashboards based on `PersesDashboard` resources, consult the
[Managing Dash0 Check Rules](helm-chart/dash0-operator/README.md#managing-dash0-dashboards)
section of the Dash0 operator Helm chart documentation.

### Synthetic Checks

The Dash0 operator supports the `Dash0SyntheticCheck` custom resource definition.
Synthetic checks stored in the cluster as Dash0SyntheticCheck custom resources are used to create
[synthetic checks](https://www.dash0.com/documentation/dash0/synthetic-monitoring) in Dash0.

For more information on how the Dash0 operator synchronizes synthetic checks based on `Dash0SyntheticCheck` resources,
consult the [Managing Dash0 Synthetic Checks](helm-chart/dash0-operator/README.md#managing-dash0-synthetic-checks)
section of the Dash0 operator Helm chart documentation.

### Views

The Dash0 operator supports the `Dash0View` custom resource definition which is used to create views on tracing,
logging or resource data within Dash0.

## Getting Started

The preferred method of installation is via the operator's
[Helm chart](https://github.com/dash0hq/dash0-operator/blob/main/helm-chart/dash0-operator/README.md).

The [Helm chart documentation](https://github.com/dash0hq/dash0-operator/blob/main/helm-chart/dash0-operator/README.md)
also contains all other relevant information for getting started with the operator, like how to enable Dash0 monitoring
for your workloads etc.
