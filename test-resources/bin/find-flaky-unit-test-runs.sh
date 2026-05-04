#!/usr/bin/env bash

# SPDX-FileCopyrightText: Copyright 2026 Dash0 Inc.
# SPDX-License-Identifier: Apache-2.0

# Lists every ci.yaml workflow run (and re-run attempt) that failed in
# the "verify" job's "run operator manager and Helm chart unit tests" step.
# Includes runs that ultimately succeeded after a re-run as well as runs
# that stayed red.
#
# Requires: gh (authenticated), jq.
#
# Examples:
# - All flaky/failing unit-test runs in the last 200 ci.yaml runs (with extracted failing test names):
#   test-resources/bin/find-flaky-unit-test-runs.sh
# - Wider scan, only main branch, no log download:
#   test-resources/bin/find-flaky-unit-test-runs.sh --limit 500 --branch main --no-test-names
# - Specific time window:
#   test-resources/bin/find-flaky-unit-test-runs.sh --created '>=2026-04-01' --limit 1000
#
# Per hit it prints: run id, attempt n/N, timestamp, event, branch, run URL, failed-job URL, and (unless --no-test-names) the
# --- FAIL: TestName names parsed from the job log. GH Actions retains job logs ~90 days, so older runs may print
# (log fetch failed; logs may have expired) — the run/job URLs still work for whatever the UI retains.
#
# Cost note — for each run with attempts > 1 or a non-success conclusion, the script makes one API call per attempt plus one log
# download per matching attempt. A 1000-run window with --no-test-names is comfortable; with --with-tests and lots of hits, watch for
# secondary rate limits.

set -euo pipefail

WORKFLOW="ci.yaml"
JOB_NAME="Build & Test"
STEP_NAME="run operator manager and Helm chart unit tests"

usage() {
  cat <<EOF
Usage: $(basename "$0") [options]

Options:
  -l, --limit N          Max number of workflow runs to inspect (default: 200).
  -c, --created EXPR     Filter passed verbatim to 'gh run list --created',
                         e.g. '>=2026-01-01' or '2026-04-01..2026-05-01'.
  -b, --branch NAME      Restrict to runs on the given head branch.
      --no-test-names    Don't download logs to extract failing test names.
  -h, --help             Show this help.
EOF
}

limit=100
created=""
branch=""
extract_tests=true

while [[ $# -gt 0 ]]; do
  case "$1" in
    -l|--limit)   limit="$2"; shift 2 ;;
    -c|--created) created="$2"; shift 2 ;;
    -b|--branch)  branch="$2"; shift 2 ;;
    --no-test-names)   extract_tests=false; shift ;;
    -h|--help)    usage; exit 0 ;;
    *) echo "unknown argument: $1" >&2; usage >&2; exit 2 ;;
  esac
done

command -v gh >/dev/null || { echo "gh CLI is required" >&2; exit 1; }
command -v jq >/dev/null || { echo "jq is required" >&2; exit 1; }

list_args=(--workflow="$WORKFLOW" --limit="$limit"
  --json "databaseId,attempt,conclusion,createdAt,headBranch,displayTitle,url,event")
[[ -n "$created" ]] && list_args+=(--created "$created")
[[ -n "$branch"  ]] && list_args+=(--branch  "$branch")

echo "Fetching up to $limit ci.yaml runs..." >&2
runs_json=$(gh run list "${list_args[@]}")
total=$(jq 'length' <<<"$runs_json")
echo "Inspecting $total runs (skipping single-attempt successes)..." >&2

hits=0
inspected=0
all_failures=""

while IFS= read -r run; do
  run_id=$(jq -r '.databaseId'    <<<"$run")
  attempts=$(jq -r '.attempt'     <<<"$run")
  conclusion=$(jq -r '.conclusion' <<<"$run")
  created_at=$(jq -r '.createdAt' <<<"$run")
  head_branch=$(jq -r '.headBranch' <<<"$run")
  run_url=$(jq -r '.url'          <<<"$run")
  title=$(jq -r '.displayTitle'   <<<"$run")
  event=$(jq -r '.event'          <<<"$run")

  # Cheap skip: run finished green on the first attempt -> no failure to find.
  if [[ "$attempts" -eq 1 && "$conclusion" == "success" ]]; then
    continue
  fi

  inspected=$((inspected + 1))

  for a in $(seq 1 "$attempts"); do
    attempt_json=$(gh api \
      "repos/{owner}/{repo}/actions/runs/${run_id}/attempts/${a}/jobs" \
      --paginate 2>/dev/null || true)
    [[ -z "$attempt_json" ]] && continue

    job=$(jq -c --arg job "$JOB_NAME" --arg step "$STEP_NAME" '
      .jobs[]
      | select(.name == $job)
      | select(any(.steps[]?; .name == $step and .conclusion == "failure"))
    ' <<<"$attempt_json" || true)

    [[ -z "$job" ]] && continue

    hits=$((hits + 1))
    job_id=$(jq -r  '.id'        <<<"$job")
    job_url=$(jq -r '.html_url'  <<<"$job")
    job_concl=$(jq -r '.conclusion' <<<"$job")
    started=$(jq -r '.started_at'   <<<"$job")

    printf '\n=== run %s  attempt %s/%s  (%s, event=%s, branch=%s) ===\n' \
      "$run_id" "$a" "$attempts" "$created_at" "$event" "$head_branch"
    printf '  title    : %s\n'  "$title"
    printf '  run url  : %s\n'  "$run_url"
    printf '  job url  : %s\n'  "$job_url"
    printf '  job concl: %s (started %s)\n' "$job_concl" "$started"

    if $extract_tests; then
      log=$(gh api -H 'Accept: application/vnd.github+json' \
        "repos/{owner}/{repo}/actions/jobs/${job_id}/logs" 2>/dev/null || true)
      if [[ -n "$log" ]]; then
        # Ginkgo colorizes its output, e.g. <ESC>[91m[FAIL]<ESC>[0m ... — the
        # escape between ']' and the description breaks naive grep. Strip
        # ANSI CSI sequences first, portably (works with BSD sed on macOS).
        log=$(printf '%s\n' "$log" | sed $'s/\033\\[[0-9;]*[a-zA-Z]//g')

        # Ginkgo prints a "Summarizing N Failures:" section with one
        # "[FAIL] <full spec description>" line per failed spec.
        failing=$(printf '%s\n' "$log" \
          | grep -oE '\[FAIL\] .*' \
          | sed -E 's/[[:space:]]+$//' \
          | sort -u || true)

        # Fall back to top-level Go test names for packages without Ginkgo.
        if [[ -z "$failing" ]]; then
          failing=$(printf '%s\n' "$log" \
            | grep -E '^\S*\s*--- FAIL: ' \
            | sed -E 's/.*--- FAIL: ([^ ]+).*/\1/' \
            | sort -u || true)
        fi
        if [[ -n "$failing" ]]; then
          echo "  failing tests:"
          while IFS= read -r t; do printf '    - %s\n' "$t"; done <<<"$failing"
          all_failures+="${failing}"$'\n'
        else
          echo "  failing tests: (none parsed from log; check job url)"
        fi
      else
        echo "  failing tests: (log fetch failed; logs may have expired)"
      fi
    fi
  done
done < <(jq -c '.[]' <<<"$runs_json")

if [[ -n "$all_failures" ]]; then
  echo
  echo "=== failing test frequency (most failures first) ==="
  printf '%s' "$all_failures" \
    | grep -v '^[[:space:]]*$' \
    | sort \
    | uniq -c \
    | sort -k1,1nr -k2 \
    | sed -E 's/^[[:space:]]*([0-9]+)[[:space:]]+/  \1x  /'
fi

echo >&2
echo "Done. Inspected $inspected candidate runs, found $hits failed attempts." >&2
