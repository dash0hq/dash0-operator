#!/usr/bin/env bash

# SPDX-FileCopyrightText: Copyright 2024 Dash0 Inc.
# SPDX-License-Identifier: Apache-2.0

set -euo pipefail

cd "$(dirname "${BASH_SOURCE[0]}")"/../..

source test-resources/bin/util
load_env_file

# shellcheck disable=SC2002
cat \
  test-resources/customresources/dash0operatorconfiguration/dash0operatorconfiguration.token.yaml.template | \
  DASH0_INGRESS_ENDPOINT="$DASH0_INGRESS_ENDPOINT" \
  DASH0_AUTHORIZATION_TOKEN="$DASH0_AUTHORIZATION_TOKEN" \
  DASH0_API_ENDPOINT="$DASH0_API_ENDPOINT" \
  SELF_MONITORING_ENABLED="${SELF_MONITORING_ENABLED:-true}" \
  KUBERNETES_INFRASTRUCTURE_METRICS_COLLECTION_ENABLED="${KUBERNETES_INFRASTRUCTURE_METRICS_COLLECTION_ENABLED:-true}" \
  COLLECT_POD_LABELS_AND_ANNOTATIONS_ENABLED="${COLLECT_POD_LABELS_AND_ANNOTATIONS_ENABLED:-true}" \
  envsubst > \
  test-resources/customresources/dash0operatorconfiguration/dash0operatorconfiguration.token.yaml

# shellcheck disable=SC2002
cat \
  test-resources/customresources/dash0operatorconfiguration/dash0operatorconfiguration.secret.yaml.template | \
  DASH0_INGRESS_ENDPOINT="$DASH0_INGRESS_ENDPOINT" \
  DASH0_AUTHORIZATION_TOKEN="$DASH0_AUTHORIZATION_TOKEN" \
  DASH0_API_ENDPOINT="$DASH0_API_ENDPOINT" \
  SELF_MONITORING_ENABLED="${SELF_MONITORING_ENABLED:-true}" \
  KUBERNETES_INFRASTRUCTURE_METRICS_COLLECTION_ENABLED="${KUBERNETES_INFRASTRUCTURE_METRICS_COLLECTION_ENABLED:-true}" \
  COLLECT_POD_LABELS_AND_ANNOTATIONS_ENABLED="${COLLECT_POD_LABELS_AND_ANNOTATIONS_ENABLED:-true}" \
  envsubst > \
  test-resources/customresources/dash0operatorconfiguration/dash0operatorconfiguration.secret.yaml

# shellcheck disable=SC2002
cat \
  test-resources/customresources/dash0operatorconfiguration/dash0operatorconfiguration.otlpsink.yaml.template | \
  DASH0_INGRESS_ENDPOINT="$DASH0_INGRESS_ENDPOINT" \
  DASH0_AUTHORIZATION_TOKEN="$DASH0_AUTHORIZATION_TOKEN" \
  DASH0_API_ENDPOINT="$DASH0_API_ENDPOINT" \
  SELF_MONITORING_ENABLED="${SELF_MONITORING_ENABLED:-true}" \
  KUBERNETES_INFRASTRUCTURE_METRICS_COLLECTION_ENABLED="${KUBERNETES_INFRASTRUCTURE_METRICS_COLLECTION_ENABLED:-true}" \
  COLLECT_POD_LABELS_AND_ANNOTATIONS_ENABLED="${COLLECT_POD_LABELS_AND_ANNOTATIONS_ENABLED:-true}" \
  envsubst > \
  test-resources/customresources/dash0operatorconfiguration/dash0operatorconfiguration.otlpsink.yaml

# shellcheck disable=SC2002
cat \
  test-resources/customresources/dash0monitoring/dash0monitoring.yaml.template | \
  DASH0_INGRESS_ENDPOINT="$DASH0_INGRESS_ENDPOINT" \
  DASH0_AUTHORIZATION_TOKEN="$DASH0_AUTHORIZATION_TOKEN" \
  DASH0_API_ENDPOINT="$DASH0_API_ENDPOINT" \
  INSTRUMENT_WORKLOADS="${INSTRUMENT_WORKLOADS:-all}" \
  LOG_COLLECTION="${LOG_COLLECTION:-true}" \
  PROMETHEUS_SCRAPING_ENABLED="${PROMETHEUS_SCRAPING_ENABLED:-true}" \
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
  INSTRUMENT_WORKLOADS="${INSTRUMENT_WORKLOADS:-all}" \
  LOG_COLLECTION="${LOG_COLLECTION:-true}" \
  PROMETHEUS_SCRAPING_ENABLED="${PROMETHEUS_SCRAPING_ENABLED:-true}" \
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
  INSTRUMENT_WORKLOADS="${INSTRUMENT_WORKLOADS:-all}" \
  LOG_COLLECTION="${LOG_COLLECTION:-true}" \
  PROMETHEUS_SCRAPING_ENABLED="${PROMETHEUS_SCRAPING_ENABLED:-true}" \
  SYNCHRONIZE_PERSES_DASHBOARDS="${SYNCHRONIZE_PERSES_DASHBOARDS:-true}" \
  SYNCHRONIZE_PROMETHEUS_RULES="${SYNCHRONIZE_PROMETHEUS_RULES:-true}" \
  envsubst > \
  test-resources/customresources/dash0monitoring/dash0monitoring-3.yaml
