#!/usr/bin/env bash

# SPDX-FileCopyrightText: Copyright 2024 Dash0 Inc.
# SPDX-License-Identifier: Apache-2.0

set -euo pipefail

project_root="$(dirname "${BASH_SOURCE[0]}")"/../..
scripts_lib="test-resources/bin/lib"

cd "$project_root"

# shellcheck source=./lib/util
source "$scripts_lib/util"

load_env_file

TELEMETRY_COLLECTION_ENABLED="${TELEMETRY_COLLECTION_ENABLED:-true}"
KUBERNETES_INFRASTRUCTURE_METRICS_COLLECTION_ENABLED="${KUBERNETES_INFRASTRUCTURE_METRICS_COLLECTION_ENABLED:-$TELEMETRY_COLLECTION_ENABLED}"
COLLECT_POD_LABELS_AND_ANNOTATIONS_ENABLED="${COLLECT_POD_LABELS_AND_ANNOTATIONS_ENABLED:-$TELEMETRY_COLLECTION_ENABLED}"
if [[ "$TELEMETRY_COLLECTION_ENABLED" = "false" ]]; then
  INSTRUMENT_WORKLOADS_MODE="${INSTRUMENT_WORKLOADS_MODE:-none}"
else
  INSTRUMENT_WORKLOADS_MODE="${INSTRUMENT_WORKLOADS_MODE:-all}"
fi
LOG_COLLECTION="${LOG_COLLECTION:-$TELEMETRY_COLLECTION_ENABLED}"
PROMETHEUS_SCRAPING_ENABLED="${PROMETHEUS_SCRAPING_ENABLED:-$TELEMETRY_COLLECTION_ENABLED}"

# shellcheck disable=SC2002
cat \
  test-resources/customresources/dash0operatorconfiguration/dash0operatorconfiguration.token.yaml.template | \
  DASH0_INGRESS_ENDPOINT="$DASH0_INGRESS_ENDPOINT" \
  DASH0_AUTHORIZATION_TOKEN="$DASH0_AUTHORIZATION_TOKEN" \
  DASH0_API_ENDPOINT="$DASH0_API_ENDPOINT" \
  SELF_MONITORING_ENABLED="${SELF_MONITORING_ENABLED:-true}" \
  KUBERNETES_INFRASTRUCTURE_METRICS_COLLECTION_ENABLED="$KUBERNETES_INFRASTRUCTURE_METRICS_COLLECTION_ENABLED" \
  COLLECT_POD_LABELS_AND_ANNOTATIONS_ENABLED="$COLLECT_POD_LABELS_AND_ANNOTATIONS_ENABLED" \
  TELEMETRY_COLLECTION_ENABLED="$TELEMETRY_COLLECTION_ENABLED" \
  envsubst > \
  test-resources/customresources/dash0operatorconfiguration/dash0operatorconfiguration.token.yaml

# shellcheck disable=SC2002
cat \
  test-resources/customresources/dash0operatorconfiguration/dash0operatorconfiguration.secret.yaml.template | \
  DASH0_INGRESS_ENDPOINT="$DASH0_INGRESS_ENDPOINT" \
  DASH0_AUTHORIZATION_TOKEN="$DASH0_AUTHORIZATION_TOKEN" \
  DASH0_API_ENDPOINT="$DASH0_API_ENDPOINT" \
  SELF_MONITORING_ENABLED="${SELF_MONITORING_ENABLED:-true}" \
  KUBERNETES_INFRASTRUCTURE_METRICS_COLLECTION_ENABLED="$KUBERNETES_INFRASTRUCTURE_METRICS_COLLECTION_ENABLED" \
  COLLECT_POD_LABELS_AND_ANNOTATIONS_ENABLED="$COLLECT_POD_LABELS_AND_ANNOTATIONS_ENABLED" \
  TELEMETRY_COLLECTION_ENABLED="$TELEMETRY_COLLECTION_ENABLED" \
  envsubst > \
  test-resources/customresources/dash0operatorconfiguration/dash0operatorconfiguration.secret.yaml

# shellcheck disable=SC2002
cat \
  test-resources/customresources/dash0operatorconfiguration/dash0operatorconfiguration.otlpsink.yaml.template | \
  DASH0_INGRESS_ENDPOINT="$DASH0_INGRESS_ENDPOINT" \
  DASH0_AUTHORIZATION_TOKEN="$DASH0_AUTHORIZATION_TOKEN" \
  DASH0_API_ENDPOINT="$DASH0_API_ENDPOINT" \
  SELF_MONITORING_ENABLED="${SELF_MONITORING_ENABLED:-true}" \
  KUBERNETES_INFRASTRUCTURE_METRICS_COLLECTION_ENABLED="$KUBERNETES_INFRASTRUCTURE_METRICS_COLLECTION_ENABLED" \
  COLLECT_POD_LABELS_AND_ANNOTATIONS_ENABLED="$COLLECT_POD_LABELS_AND_ANNOTATIONS_ENABLED" \
  TELEMETRY_COLLECTION_ENABLED="$TELEMETRY_COLLECTION_ENABLED" \
  envsubst > \
  test-resources/customresources/dash0operatorconfiguration/dash0operatorconfiguration.otlpsink.yaml

# shellcheck disable=SC2002
cat \
  test-resources/customresources/dash0monitoring/dash0monitoring.yaml.template | \
  DASH0_INGRESS_ENDPOINT="$DASH0_INGRESS_ENDPOINT" \
  DASH0_AUTHORIZATION_TOKEN="$DASH0_AUTHORIZATION_TOKEN" \
  DASH0_API_ENDPOINT="$DASH0_API_ENDPOINT" \
  INSTRUMENT_WORKLOADS_MODE="$INSTRUMENT_WORKLOADS_MODE" \
  LOG_COLLECTION="$LOG_COLLECTION" \
  PROMETHEUS_SCRAPING_ENABLED="$PROMETHEUS_SCRAPING_ENABLED" \
  SYNCHRONIZE_PERSES_DASHBOARDS="${SYNCHRONIZE_PERSES_DASHBOARDS:-true}" \
  SYNCHRONIZE_PROMETHEUS_RULES="${SYNCHRONIZE_PROMETHEUS_RULES:-true}" \
  envsubst > \
  test-resources/customresources/dash0monitoring/dash0monitoring.yaml
# shellcheck disable=SC2002
cat \
  test-resources/customresources/dash0monitoring/dash0monitoring-2.yaml.template | \
  DASH0_INGRESS_ENDPOINT="$DASH0_INGRESS_ENDPOINT" \
  DASH0_AUTHORIZATION_TOKEN="$DASH0_AUTHORIZATION_TOKEN" \
  DASH0_API_ENDPOINT="$DASH0_API_ENDPOINT" \
  INSTRUMENT_WORKLOADS_MODE="$INSTRUMENT_WORKLOADS_MODE" \
  LOG_COLLECTION="$LOG_COLLECTION" \
  PROMETHEUS_SCRAPING_ENABLED="$PROMETHEUS_SCRAPING_ENABLED" \
  SYNCHRONIZE_PERSES_DASHBOARDS="${SYNCHRONIZE_PERSES_DASHBOARDS:-true}" \
  SYNCHRONIZE_PROMETHEUS_RULES="${SYNCHRONIZE_PROMETHEUS_RULES:-true}" \
  envsubst > \
  test-resources/customresources/dash0monitoring/dash0monitoring-2.yaml
# shellcheck disable=SC2002
cat \
  test-resources/customresources/dash0monitoring/dash0monitoring-3.yaml.template | \
  DASH0_INGRESS_ENDPOINT="$DASH0_INGRESS_ENDPOINT" \
  DASH0_AUTHORIZATION_TOKEN="$DASH0_AUTHORIZATION_TOKEN" \
  DASH0_API_ENDPOINT="$DASH0_API_ENDPOINT" \
  INSTRUMENT_WORKLOADS_MODE="$INSTRUMENT_WORKLOADS_MODE" \
  LOG_COLLECTION="$LOG_COLLECTION" \
  PROMETHEUS_SCRAPING_ENABLED="$PROMETHEUS_SCRAPING_ENABLED" \
  SYNCHRONIZE_PERSES_DASHBOARDS="${SYNCHRONIZE_PERSES_DASHBOARDS:-true}" \
  SYNCHRONIZE_PROMETHEUS_RULES="${SYNCHRONIZE_PROMETHEUS_RULES:-true}" \
  envsubst > \
  test-resources/customresources/dash0monitoring/dash0monitoring-3.yaml
# shellcheck disable=SC2002
cat \
  test-resources/customresources/dash0monitoring/dash0monitoring-with-namespaced-exporter.yaml.template | \
  DASH0_INGRESS_ENDPOINT="$DASH0_INGRESS_ENDPOINT" \
  DASH0_AUTHORIZATION_TOKEN="$DASH0_AUTHORIZATION_TOKEN" \
  DASH0_API_ENDPOINT="$DASH0_API_ENDPOINT" \
  INSTRUMENT_WORKLOADS_MODE="$INSTRUMENT_WORKLOADS_MODE" \
  LOG_COLLECTION="$LOG_COLLECTION" \
  PROMETHEUS_SCRAPING_ENABLED="$PROMETHEUS_SCRAPING_ENABLED" \
  SYNCHRONIZE_PERSES_DASHBOARDS="${SYNCHRONIZE_PERSES_DASHBOARDS:-true}" \
  SYNCHRONIZE_PROMETHEUS_RULES="${SYNCHRONIZE_PROMETHEUS_RULES:-true}" \
  envsubst > \
  test-resources/customresources/dash0monitoring/dash0monitoring-with-namespaced-exporter.yaml

# shellcheck disable=SC2002
cat \
  test-resources/cert-manager/certificate-and-issuer.yaml.template | \
  OPERATOR_NAMESPACE="$operator_namespace" \
  envsubst > \
  test-resources/cert-manager/certificate-and-issuer.yaml
# shellcheck disable=SC2002
cat \
  test-resources/cert-manager/ta-issuers-and-cert.yaml.template | \
  OPERATOR_NAMESPACE="$operator_namespace" \
  envsubst > \
  test-resources/cert-manager/ta-issuers-and-cert.yaml
# shellcheck disable=SC2002
cat \
  test-resources/cert-manager/helm-values.yaml.template | \
  OPERATOR_NAMESPACE="$operator_namespace" \
  envsubst > \
  test-resources/cert-manager/helm-values.yaml
