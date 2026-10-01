#!/usr/bin/env bash

# SPDX-FileCopyrightText: Copyright 2026 Dash0 Inc.
# SPDX-License-Identifier: Apache-2.0

# Updates the kubectl image in images/agent0-connector/Dockerfile and the kubectl Go modules in
# images/agent0-connector/src/go.mod and opens a pull request for it.
#
# The agent0-connector parses kubectl command lines with the kubectl Go modules and executes them with the kubectl
# binary from the image. Both need to have the same version, so they are updated in a single commit (kubectl v1.x.y
# belongs to the Go modules v0.x.y).
#
# The update is only applied when all of the following hold:
#   * the latest stable kubectl release is newer than the version the agent0-connector currently uses,
#   * that release is at least COOLDOWN_DAYS days old,
#   * the kubectl image and the Go modules for that release have been published.
#
# Note: The pull request is created even if the new Go modules require changes to the agent0-connector code or a newer
# `go` directive than the rest of the repository uses. Failing checks on the pull request signal that it needs manual
# work.
#
# This is invoked from the update-agent0-connector-kubectl.yaml workflow.

set -euo pipefail

for executable in curl gh git go jq; do
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

# Do not open a pull request for a kubectl release that is younger than this. A fresh release occasionally needs a
# quick follow-up.
COOLDOWN_DAYS=4

dockerfile=images/agent0-connector/Dockerfile
module_dir=images/agent0-connector/src

# The Go modules the agent0-connector requires directly that are released in lockstep with kubectl. Their indirect
# lockstep dependencies (k8s.io/api, k8s.io/client-go, ...) are updated along with them by "go get".
kubectl_go_modules=(
  k8s.io/kubectl
  k8s.io/cli-runtime
  k8s.io/component-base
)

# Matches a stable kubectl version like "v1.37.1". Pre-releases ("v1.38.0-rc.0") deliberately do not match.
kubectl_version_regex='v1\.[0-9]+\.[0-9]+'

current_version=$(grep -oE "^FROM registry\.k8s\.io/kubectl:${kubectl_version_regex} " "$dockerfile" | head -n 1 | sed -E 's/^FROM registry\.k8s\.io\/kubectl:([^ ]+) $/\1/')
if [[ -z "$current_version" ]]; then
  echo "Error: cannot determine the current kubectl version from ${dockerfile}." >&2
  exit 1
fi

latest_version=$(curl -sS --fail --retry 3 "https://dl.k8s.io/release/stable.txt")
if [[ ! "$latest_version" =~ ^${kubectl_version_regex}$ ]]; then
  echo "Error: cannot determine the latest stable kubectl version from https://dl.k8s.io/release/stable.txt, got \"${latest_version}\"." >&2
  exit 1
fi

echo "current kubectl version:       $current_version"
echo "latest stable kubectl release: $latest_version"

newer_version=$(printf '%s\n%s\n' "$current_version" "$latest_version" | sort -V | tail -n 1)
if [[ "$latest_version" == "$current_version" || "$newer_version" != "$latest_version" ]]; then
  echo "No update necessary, kubectl is up to date."
  exit 0
fi

# kubectl v1.x.y belongs to the Go modules v0.x.y.
go_module_version="v0.${latest_version#v1.}"

branch_name="update-agent0-connector-kubectl-${latest_version}"

# A branch for this kubectl version means that a pull request for it has already been created, and that it has neither
# been merged nor had its branch deleted. Exit with a non-zero status so that the run is not silently green while the
# update is still pending.
if gh api "repos/${GITHUB_REPOSITORY}/git/ref/heads/${branch_name}" >/dev/null 2>&1; then
  echo "Error: the branch \"${branch_name}\" already exists, there is most likely an open pull request updating kubectl to ${latest_version}. Merge or close it (and delete the branch) before this workflow can run again." >&2
  exit 1
fi

release_date=$(gh api "repos/kubernetes/kubernetes/releases/tags/${latest_version}" --jq '.published_at' 2>/dev/null || true)
if [[ -z "$release_date" ]]; then
  echo "Error: cannot determine the release date of kubectl ${latest_version} from the kubernetes/kubernetes repository." >&2
  exit 1
fi
release_age_days=$(jq -rn --arg release_date "$release_date" '((now - ($release_date | fromdateiso8601)) / 86400) | floor')
if [[ "$release_age_days" -lt "$COOLDOWN_DAYS" ]]; then
  echo "kubectl ${latest_version} was released on ${release_date} and is only ${release_age_days} day(s) old, waiting until it is at least ${COOLDOWN_DAYS} days old before updating."
  exit 0
fi

# With the cooldown period, the image and the Go modules should be available once we attempt to update, so their
# absence is treated as an error.
if ! curl -sS --fail --retry 3 -L "https://registry.k8s.io/v2/kubectl/tags/list" \
  | jq -e --arg tag "$latest_version" '.tags | index($tag) != null' > /dev/null; then
  echo "Error: the image registry.k8s.io/kubectl:${latest_version} has not been published." >&2
  exit 1
fi
echo "image registry.k8s.io/kubectl:${latest_version} has been published"

for module in "${kubectl_go_modules[@]}"; do
  http_status=$(
    curl -sS --retry 3 -o /dev/null -w '%{http_code}' \
      "https://proxy.golang.org/${module}/@v/${go_module_version}.info"
  )
  if [[ "$http_status" != 200 ]]; then
    echo "Error: the Go module ${module}@${go_module_version} is not available on proxy.golang.org (HTTP status ${http_status})." >&2
    exit 1
  fi
  echo "Go module ${module}@${go_module_version} has been published"
done

sed -i.bak -E "s/^FROM registry\.k8s\.io\/kubectl:${kubectl_version_regex} /FROM registry.k8s.io\/kubectl:${latest_version} /" "$dockerfile"
rm -f "${dockerfile}.bak"

go_directive_before=$(grep -E '^go ' "${module_dir}/go.mod")
go_get_args=()
for module in "${kubectl_go_modules[@]}"; do
  go_get_args+=("${module}@${go_module_version}")
done
# GOTOOLCHAIN=auto lets "go get" raise the go directive if the new modules require a newer Go version, instead of
# failing.
(
  cd "$module_dir"
  GOTOOLCHAIN=auto go get "${go_get_args[@]}"
  GOTOOLCHAIN=auto go mod tidy
)
go_directive_after=$(grep -E '^go ' "${module_dir}/go.mod")

files_to_check=("$dockerfile" "${module_dir}/go.mod" "${module_dir}/go.sum")
mapfile -t changed_files < <(git diff --name-only -- "${files_to_check[@]}")
if [[ ${#changed_files[@]} -eq 0 ]]; then
  echo "There are no changes, everything up to date."
  exit 0
fi

echo "There are changes, creating a pull request."
echo
echo git diff:
git --no-pager diff -- "${changed_files[@]}"

commit_message="chore(deps): update kubectl in the agent0-connector to ${latest_version}"
pr_body=$(cat <<EOF
This PR updates the kubectl image in \`${dockerfile}\` to ${latest_version} and the kubectl Go modules in \`${module_dir}/go.mod\` to ${go_module_version}.
EOF
)
if [[ "$go_directive_before" != "$go_directive_after" ]]; then
  pr_body=$(cat <<EOF
${pr_body}

The new Go modules require a newer Go version, \`go mod tidy\` has changed the go directive of \`${module_dir}/go.mod\` from \`${go_directive_before}\` to \`${go_directive_after}\`. This breaks \`make go-version-check\` until the Go version of the whole repository has been updated (see .github/workflows/update-go-versions.yaml).
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
