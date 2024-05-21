#!/usr/bin/env bash
set -euo pipefail

cd "$(dirname ${BASH_SOURCE})"

helm uninstall dash0-opentelemetry-collector-daemonset --ignore-not-found
