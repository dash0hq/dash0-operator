#!/usr/bin/env bash

# SPDX-FileCopyrightText: Copyright 2026 Dash0 Inc.
# SPDX-License-Identifier: Apache-2.0

# Manually verifies the DASH0_* environment-variable fallback for the operator manager's command-line flags.
#
# Why a dedicated script: the Helm chart passes almost every flag as a container arg, and an explicit arg always wins
# over the env fallback, so the feature is invisible in the normal test-scenario-* runs. This script exercises the
# fallback directly against an already-installed operator by mutating the manager Deployment's env and args in place and
# inspecting the manager logs. All mutations are reverted on exit (trap).
#
# The observable signal is the debug log line the operator emits for every flag it fills from the environment (the
# script forces debug logging via DASH0_DEVELOPMENT_MODE=true so this line is visible):
#   "command-line flag set from environment variable" {flag=..., envVar=..., value=...}
#
# Preconditions:
#   - a cluster is running and kubectl points at it
#   - the operator is already installed (e.g. via test-resources/bin/test-scenario-04-minimal-operator.sh) using the
#     operator image that contains the change under test
#   - kubectl and jq are on PATH
#
# Overridable via env: OPERATOR_NAMESPACE (default: operator-namespace), ROLLOUT_TIMEOUT (default: 120s).

set -euo pipefail

ns="${OPERATOR_NAMESPACE:-operator-namespace}"
rollout_timeout="${ROLLOUT_TIMEOUT:-120s}"
pod_selector="app.kubernetes.io/name=dash0-operator,app.kubernetes.io/component=controller"
applied_msg="command-line flag set from environment variable"
failures=0

info() { printf '\n\033[1m==> %s\033[0m\n' "$*"; }
pass() { printf '  \033[32mPASS\033[0m %s\n' "$*"; }
fail() { printf '  \033[31mFAIL\033[0m %s\n' "$*"; failures=$((failures + 1)); }

require() {
  if ! command -v "$1" >/dev/null 2>&1; then
    echo "error: required command '$1' not found on PATH" >&2
    exit 1
  fi
}

require kubectl
require jq

if ! kubectl cluster-info >/dev/null 2>&1; then
  echo "error: kubectl cannot reach a cluster" >&2
  exit 1
fi

deploy="$(
  kubectl -n "$ns" get deploy -o json |
    jq -r '.items[] | select(.spec.template.spec.containers[].name == "manager") | .metadata.name' |
    head -1
)"
if [[ -z "$deploy" ]]; then
  echo "error: no Deployment with a 'manager' container found in namespace '$ns'." >&2
  echo "       Install the operator first (see the preconditions in this script's header)." >&2
  exit 1
fi

cidx="$(
  kubectl -n "$ns" get deploy "$deploy" -o json |
    jq '.spec.template.spec.containers | map(.name == "manager") | index(true)'
)"

deploy_json="$(kubectl -n "$ns" get deploy "$deploy" -o json)"
orig_args="$(echo "$deploy_json" | jq -c ".spec.template.spec.containers[$cidx].args // []")"
orig_env="$(echo "$deploy_json" | jq -c ".spec.template.spec.containers[$cidx].env // []")"

# The applied-from-env log line is emitted at debug level, so the operator must run with debug logging for the checks
# below to observe it. Development mode enables debug logging without occupying the --dash0-log-level flag (which is
# itself one of the flags under test), so it is forced on in the test baseline and reverted on exit.
debug_env="$(
  echo "$orig_env" |
    jq -c '[.[] | select(.name != "DASH0_DEVELOPMENT_MODE")] + [{"name": "DASH0_DEVELOPMENT_MODE", "value": "true"}]'
)"

info "target: deployment/$deploy (container index $cidx) in namespace $ns (debug logging forced for the test)"

apply_args_env() {
  # apply_args_env <args-json> <env-json>
  kubectl -n "$ns" patch deploy "$deploy" --type=json -p "[
    {\"op\": \"add\", \"path\": \"/spec/template/spec/containers/$cidx/args\", \"value\": $1},
    {\"op\": \"add\", \"path\": \"/spec/template/spec/containers/$cidx/env\", \"value\": $2}
  ]" >/dev/null
}

# shellcheck disable=SC2317,SC2329  # invoked via 'trap restore EXIT'
restore() {
  info "restoring the manager Deployment to its original args and env"
  apply_args_env "$orig_args" "$orig_env"
  kubectl -n "$ns" rollout status "deploy/$deploy" --timeout="$rollout_timeout" >/dev/null 2>&1 || true
}
trap restore EXIT

reset_to_baseline() {
  apply_args_env "$orig_args" "$debug_env"
  kubectl -n "$ns" rollout status "deploy/$deploy" --timeout="$rollout_timeout" >/dev/null
}

set_env() { kubectl -n "$ns" set env "deploy/$deploy" -c manager "$@" >/dev/null; }

remove_arg() {
  # remove_arg <arg-name-prefix>
  local new_args
  new_args="$(
    kubectl -n "$ns" get deploy "$deploy" -o json |
      jq -c "[.spec.template.spec.containers[$cidx].args[] | select(startswith(\"$1\") | not)]"
  )"
  kubectl -n "$ns" patch deploy "$deploy" --type=json \
    -p "[{\"op\": \"add\", \"path\": \"/spec/template/spec/containers/$cidx/args\", \"value\": $new_args}]" >/dev/null
}

wait_rollout() { kubectl -n "$ns" rollout status "deploy/$deploy" --timeout="$rollout_timeout" >/dev/null; }

manager_logs() {
  local pod
  pod="$(
    kubectl -n "$ns" get pods -l "$pod_selector" \
      --sort-by=.metadata.creationTimestamp -o jsonpath='{.items[-1:].metadata.name}' 2>/dev/null || true
  )"
  [[ -z "$pod" ]] && return 0
  kubectl -n "$ns" logs "$pod" -c manager 2>/dev/null || true
  kubectl -n "$ns" logs "$pod" -c manager --previous 2>/dev/null || true
}

applied_has_flag() { manager_logs | grep -F "$applied_msg" | grep -qF "$1"; }
# shellcheck disable=SC2317,SC2329  # invoked indirectly via 'retry'
logs_have() { manager_logs | grep -qF "$1"; }

retry() {
  # retry <timeout-seconds> <cmd...>
  local timeout="$1" elapsed=0
  shift
  while ((elapsed < timeout)); do
    if "$@"; then return 0; fi
    sleep 3
    elapsed=$((elapsed + 3))
  done
  return 1
}

# --------------------------------------------------------------------------------------------------------------------
info "Scenario 1: env var applies to a flag the chart does NOT pass as an arg (--enable-http2)"
reset_to_baseline
set_env DASH0_ENABLE_HTTP2=true
wait_rollout
if retry 30 applied_has_flag "enable-http2"; then
  pass "DASH0_ENABLE_HTTP2 was applied to --enable-http2"
else
  fail "no applied-from-env log line for enable-http2"
fi

# --------------------------------------------------------------------------------------------------------------------
info "Scenario 2: explicit arg wins over env (--dash0-log-level arg present, DASH0_LOG_LEVEL set)"
reset_to_baseline
set_env DASH0_LOG_LEVEL=debug
wait_rollout
sleep 3
if applied_has_flag "dash0-log-level"; then
  fail "env overrode the explicit --dash0-log-level arg (precedence broken)"
else
  pass "explicit --dash0-log-level arg won; DASH0_LOG_LEVEL ignored"
fi

# --------------------------------------------------------------------------------------------------------------------
info "Scenario 3: with the --dash0-log-level arg removed, DASH0_LOG_LEVEL now applies (OLM simulation)"
reset_to_baseline
set_env DASH0_LOG_LEVEL=debug
remove_arg "--dash0-log-level"
wait_rollout
if retry 30 applied_has_flag "dash0-log-level"; then
  pass "DASH0_LOG_LEVEL applied to --dash0-log-level once the arg was removed"
else
  fail "DASH0_LOG_LEVEL was not applied after removing the arg"
fi

# --------------------------------------------------------------------------------------------------------------------
info "Scenario 4: an invalid env value makes the operator fail fast"
reset_to_baseline
set_env DASH0_ENABLE_HTTP2=notabool
if retry 60 logs_have "cannot apply environment variable DASH0_ENABLE_HTTP2"; then
  pass "operator refused to start and logged the parse error"
else
  fail "expected fail-fast error not found in manager logs"
fi

# --------------------------------------------------------------------------------------------------------------------
info "Summary"
if ((failures == 0)); then
  printf '\033[32mAll checks passed.\033[0m\n'
else
  printf '\033[31m%d check(s) failed.\033[0m\n' "$failures"
fi
exit "$failures"
