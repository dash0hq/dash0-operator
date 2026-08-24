#!/usr/bin/env bash

# SPDX-FileCopyrightText: Copyright 2026 Dash0 Inc.
# SPDX-License-Identifier: Apache-2.0

# Updates every Go version reference in the repository to the latest stable Go release and opens a pull request for it.
#
# All Go versions in this repository must be in sync (see `make go-version-check`): the `go` directive of every go.mod
# file, and the golang base image tag of every Dockerfile. Updating them independently, as Dependabot's gomod and
# docker ecosystem updaters would, breaks that check. This script therefore updates all of them in a single commit.
#
# The update is only applied when all of the following hold:
#   * the latest stable Go release is newer than the version this repository currently uses,
#   * that release is at least COOLDOWN_DAYS days old,
#   * every golang base image tag the repository needs (for example golang:1.27.0-alpine3.23) has been published.
#
# This is invoked from the update-go-versions.yaml workflow.

set -euo pipefail

for executable in curl gh git jq make; do
  if ! command -v "$executable" &> /dev/null; then
    echo "Error: the $executable executable is not available." >&2
    exit 1
  fi
done

if [[ -z "${GITHUB_REPOSITORY:-}" ]]; then
  echo "Error: the GITHUB_REPOSITORY environment variable is not set." >&2
  exit 1
fi

cd "$(dirname "${BASH_SOURCE[0]}")/../../.."

# Do not open a pull request for a Go release that is younger than this. A fresh release occasionally needs a quick
# follow-up, and the golang base images are published with a delay of a few hours to a few days.
COOLDOWN_DAYS=4

# Matches a Go version like "1.26.6" or "1.27". Pre-releases ("1.27rc1") deliberately do not match.
go_version_regex='[0-9]+\.[0-9]+(\.[0-9]+)?'

# Reduces a Go version to its language version, that is, "1.27.0" to "1.27".
language_version() {
  echo "$1" | cut -d. -f1,2
}

# Returns 0 if the version given as the first argument is greater than or equal to the version given as the second
# argument.
version_at_least() {
  [[ "$(printf '%s\n%s\n' "$1" "$2" | sort -V | tail -n 1)" == "$1" ]]
}

# Prints the language version of the Go toolchain that a given golangci-lint release was built with, for example
# "1.27". The release binary is downloaded and asked for it, because that is the value golangci-lint compares against
# at runtime. Neither the go.mod of the golangci-lint repository nor its release notes state it reliably: the go.mod of
# v2.9.0 declares go 1.25.0, while the binary is built with go1.26.0.
golangci_lint_build_go_version() {
  local version="$1"
  local install_dir build_go_version
  install_dir=$(mktemp -d)
  # This is the same installation method that the Makefile target golangci-lint-install uses. install.sh has to be
  # taken from the tag that is being installed, not from master: the install.sh on master verifies a release tarball
  # against the checksum of the .sbom.json asset published next to it, and fails for every release that has one.
  if ! curl -sSfL "https://raw.githubusercontent.com/golangci/golangci-lint/${version}/install.sh" \
    | sh -s -- -b "$install_dir" "$version" > /dev/null 2>&1; then
    rm -rf "$install_dir"
    echo "Error: cannot install golangci-lint ${version}." >&2
    return 1
  fi
  build_go_version=$("$install_dir/golangci-lint" version --json | jq -r '.goVersion' | sed -E 's/^go//')
  rm -rf "$install_dir"
  language_version "$build_go_version"
}

# All files in this repository that reference a Go version. The `go` directive is updated in go.mod files, the golang
# base image tag in Dockerfiles and in the base-images files of the instrumentation image tests.
collect_files_with_go_versions() {
  find . \
    -name .git -prune -o \
    -name vendor -prune -o \
    -name node_modules -prune -o \
    -path ./images/collector/opentelemetry-collector -prune -o \
    -path ./images/collector/opentelemetry-collector-contrib -prune -o \
    \( -name go.mod -o -name 'Dockerfile*' -o -name base-images \) -print \
    | sort
}

mapfile -t files_with_go_versions < <(collect_files_with_go_versions)
if [[ ${#files_with_go_versions[@]} -eq 0 ]]; then
  echo "Error: found no go.mod files, Dockerfiles or base-images files." >&2
  exit 1
fi

current_version=$(grep -oE "^go ${go_version_regex}$" go.mod | head -n 1 | awk '{print $2}')
if [[ -z "$current_version" ]]; then
  echo "Error: cannot determine the current Go version from the go directive in go.mod." >&2
  exit 1
fi

# The download feed lists stable releases as well as pre-releases; only entries with stable == true and a version
# without an rc/beta suffix are considered.
latest_version=$(
  curl -sS --fail --retry 3 "https://go.dev/dl/?mode=json" \
    | jq -r '.[] | select(.stable == true) | .version' \
    | sed -E 's/^go//' \
    | grep -E "^${go_version_regex}$" \
    | sort -V \
    | tail -n 1
)
if [[ -z "$latest_version" ]]; then
  echo "Error: cannot determine the latest stable Go version from https://go.dev/dl/?mode=json." >&2
  exit 1
fi

echo "current Go version:       $current_version"
echo "latest stable Go release: $latest_version"

newer_version=$(printf '%s\n%s\n' "$current_version" "$latest_version" | sort -V | tail -n 1)
if [[ "$latest_version" == "$current_version" || "$newer_version" != "$latest_version" ]]; then
  echo "No update necessary, the Go version is up to date."
  exit 0
fi

branch_name="update-go-versions-${latest_version}"

# A branch for this Go version means that a pull request for it has already been created, and that it has neither been
# merged nor had its branch deleted. Exit with a non-zero status so that the run is not silently green while the update
# is still pending.
if gh api "repos/${GITHUB_REPOSITORY}/git/ref/heads/${branch_name}" >/dev/null 2>&1; then
  echo "Error: the branch \"${branch_name}\" already exists, there is most likely an open pull request updating Go to ${latest_version}. Merge or close it (and delete the branch) before this workflow can run again." >&2
  exit 1
fi

# Determine the age of the Go release (for the cooldown period). The golang/go repository publishes tags but no GitHub
# releases, so the commit date of the tag is used as the release date.
release_date=$(gh api "repos/golang/go/commits/go${latest_version}" --jq '.commit.committer.date' 2>/dev/null || true)
if [[ -z "$release_date" ]]; then
  echo "Cannot determine the release date of go${latest_version} from the golang/go repository, skipping update for now."
  exit 1
fi
release_age_days=$(jq -rn --arg release_date "$release_date" '((now - ($release_date | fromdateiso8601)) / 86400) | floor')
if [[ "$release_age_days" -lt "$COOLDOWN_DAYS" ]]; then
  echo "Go ${latest_version} was released on ${release_date} and is only ${release_age_days} day(s) old, waiting until it is at least ${COOLDOWN_DAYS} days old before updating."
  exit 0
fi

# The golang base image tags the repository needs, derived from the tag suffixes that are currently in use (for
# example "" for golang:1.26.6 and "-alpine3.23" for golang:1.26.6-alpine3.23) rather than from a hardcoded list. A new
# Go release is published on Docker Hub with a delay, and a given alpine variant may not be offered for it at all.
mapfile -t base_image_suffixes < <(
  grep -hoE "golang:${go_version_regex}[A-Za-z0-9._-]*" "${files_with_go_versions[@]}" \
    | sed -E "s/^golang:${go_version_regex}//" \
    | sort -u
)
if [[ ${#base_image_suffixes[@]} -eq 0 ]]; then
  echo "Error: found no golang base image references to derive the required image tags from." >&2
  exit 1
fi

for suffix in "${base_image_suffixes[@]}"; do
  image_tag="${latest_version}${suffix}"
  http_status=$(
    curl -sS --retry 3 -o /dev/null -w '%{http_code}' \
      "https://hub.docker.com/v2/repositories/library/golang/tags/${image_tag}"
  )
  case "$http_status" in
    200)
      echo "base image golang:${image_tag} has been published"
      ;;
    404)
      echo "The base image golang:${image_tag} has not been published yet, skipping update for now."
      # With the cooldown of four days, the Docker images for the Go version should be available once we attempt to
      # update versions. For this reason we treat unavailable base images as an error condition here.
      exit 1
      ;;
    *)
      echo "Error: unexpected HTTP status ${http_status} when checking whether golang:${image_tag} has been published." >&2
      exit 1
      ;;
  esac
done

# A golangci-lint binary refuses to lint a module whose `go` directive targets a language version newer than the Go
# toolchain the binary was built with ("the Go language version (go1.26) used to build golangci-lint is lower than the
# targeted Go version (1.27.0)"). A Go major or minor release therefore usually requires a newer golangci-lint as well,
# and that bump has to be part of this pull request, otherwise `make golangci-lint` fails on it. A Go patch release
# never needs it, since only the language version is compared.
target_language_version=$(language_version "$latest_version")
current_golangci_lint_version=$(grep -E '^GOLANGCI_LINT_VERSION \?= ' Makefile | head -n 1 | awk '{print $3}')
if [[ -z "$current_golangci_lint_version" ]]; then
  echo "Error: cannot determine the golangci-lint version from GOLANGCI_LINT_VERSION in the Makefile." >&2
  exit 1
fi

new_golangci_lint_version=""
current_golangci_lint_go_version=$(golangci_lint_build_go_version "$current_golangci_lint_version")
if version_at_least "$current_golangci_lint_go_version" "$target_language_version"; then
  echo "golangci-lint ${current_golangci_lint_version} is built with Go ${current_golangci_lint_go_version} and supports Go ${latest_version}, it does not need to be updated"
else
  echo "golangci-lint ${current_golangci_lint_version} is built with Go ${current_golangci_lint_go_version} and does not support Go ${latest_version}, looking for a newer release"
  latest_golangci_lint_version=$(gh api "repos/golangci/golangci-lint/releases/latest" --jq '.tag_name' 2>/dev/null || true)
  if [[ -z "$latest_golangci_lint_version" ]]; then
    echo "Error: cannot determine the latest golangci-lint release." >&2
    exit 1
  fi
  latest_golangci_lint_go_version=$(golangci_lint_build_go_version "$latest_golangci_lint_version")
  if ! version_at_least "$latest_golangci_lint_go_version" "$target_language_version"; then
    echo "Error: the latest golangci-lint release ${latest_golangci_lint_version} is built with Go ${latest_golangci_lint_go_version} and does not support Go ${latest_version} either. Updating Go would break \"make golangci-lint\", so no pull request is created. Wait for a golangci-lint release that is built with Go ${target_language_version}." >&2
    exit 1
  fi
  echo "updating golangci-lint from ${current_golangci_lint_version} to ${latest_golangci_lint_version}, which is built with Go ${latest_golangci_lint_go_version}"
  sed -i.bak -E "s/^GOLANGCI_LINT_VERSION \?= .*/GOLANGCI_LINT_VERSION ?= ${latest_golangci_lint_version}/" Makefile
  rm -f Makefile.bak
  new_golangci_lint_version="$latest_golangci_lint_version"
fi

for file in "${files_with_go_versions[@]}"; do
  case "$(basename "$file")" in
    go.mod)
      sed -i.bak -E "s/^go ${go_version_regex}$/go ${latest_version}/" "$file"
      ;;
    *)
      sed -i.bak -E "s/golang:${go_version_regex}/golang:${latest_version}/g" "$file"
      ;;
  esac
  rm -f "${file}.bak"
done

mapfile -t changed_files < <(git diff --name-only -- "${files_with_go_versions[@]}" Makefile)
if [[ ${#changed_files[@]} -eq 0 ]]; then
  echo "There are no changes, everything up to date."
  exit 0
fi

echo "There are changes, creating a pull request."
echo
echo git diff:
git --no-pager diff -- "${changed_files[@]}"

commit_message="chore(deps): update Go to ${latest_version}"
pr_body=$(cat <<EOF
This PR updates the Go version to ${latest_version}, in the \`go\` directive of every go.mod file and in every golang base image reference.
EOF
)
if [[ -n "$new_golangci_lint_version" ]]; then
  commit_message="chore(deps): update Go to ${latest_version} and golangci-lint to ${new_golangci_lint_version}"
  pr_body=$(cat <<EOF
${pr_body}

It also updates \`GOLANGCI_LINT_VERSION\` to ${new_golangci_lint_version}.
EOF
)
fi

# Base commit that the new branch will be based on.
base_sha=$(git rev-parse HEAD)

# createCommitOnBranch can only commit onto a branch that already exists. Create the pull request branch at the base
# commit. We have verified above that the branch does not exist yet.
gh api --method POST "repos/${GITHUB_REPOSITORY}/git/refs" \
  -f ref="refs/heads/${branch_name}" \
  -f sha="${base_sha}" >/dev/null

# Let "gh api graphql"/createCommitOnBranch create the commit via the GitHub API rather than "git commit"/"git push", so
# commits are automatically signed.
# Note: expectedHeadOid is an optimistic lock: the branch tip must still be at base_sha (it is, we just created it).
additions=$(
  for file in "${changed_files[@]}"; do
    # Reading from stdin and stripping the line breaks afterwards keeps this working with both GNU base64 (which wraps
    # at 76 characters unless -w0 is given) and BSD base64 (which has no -w and needs -i for a file argument).
    jq -n --arg path "$file" --arg contents "$(base64 < "$file" | tr -d '\n')" '{path: $path, contents: $contents}'
  done | jq -s '.'
)

jq -n \
  --arg repo "$GITHUB_REPOSITORY" \
  --arg branch "$branch_name" \
  --arg headline "$commit_message" \
  --arg body "$pr_body" \
  --arg oid "$base_sha" \
  --argjson additions "$additions" \
  '{
    query: "mutation($input: CreateCommitOnBranchInput!) { createCommitOnBranch(input: $input) { commit { oid } } }",
    variables: {
      input: {
        branch:          { repositoryNameWithOwner: $repo, branchName: $branch },
        message:         { headline: $headline, body: $body },
        expectedHeadOid: $oid,
        fileChanges:     { additions: $additions }
      }
    }
  }' | gh api graphql --input - >/dev/null

gh pr create \
  -B main \
  -H "$branch_name" \
  --title "$commit_message" \
  --body "$pr_body"
