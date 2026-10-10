#!/usr/bin/env bash

# SPDX-FileCopyrightText: Copyright 2026 Dash0 Inc.
# SPDX-License-Identifier: Apache-2.0

# Runs "go mod tidy" for images/agent0-connector/src and commits the result to the branch of a Dependabot pull request.
#
# Dependabot updates the directories of the gomod update blocks in .github/dependabot.yml independently of each other.
# images/agent0-connector/src replaces images/pkg/common with a local path. It becomes untidy when Dependabot raises the
# requirements of images/pkg/common but not its own, and then fails to build with "go: updates to go.mod needed".
#
# This is invoked from the dependabot-go-mod-tidy.yaml workflow.

set -euo pipefail

for executable in gh git go jq; do
  if ! command -v "$executable" &> /dev/null; then
    echo "Error: the $executable executable is not available." >&2
    exit 1
  fi
done

for env_var in GITHUB_REPOSITORY PR_NUMBER PR_HEAD_REF PR_HEAD_SHA; do
  if [[ -z "${!env_var:-}" ]]; then
    echo "Error: the $env_var environment variable is not set." >&2
    exit 1
  fi
done

cd "$(dirname "${BASH_SOURCE[0]}")/../../.."

if [[ "$(git rev-parse HEAD)" != "$PR_HEAD_SHA" ]]; then
  echo "Error: the checked out commit is not the head commit of the pull request (${PR_HEAD_SHA})." >&2
  exit 1
fi

module_dir=images/agent0-connector/src

echo "running go mod tidy in ${module_dir}"
(cd "$module_dir" && go mod tidy)

go_mod_files=("${module_dir}/go.mod" "${module_dir}/go.sum")

mapfile -t changed_files < <(
  git diff --name-only --diff-filter=d -- "${go_mod_files[@]}"
  git ls-files --others --exclude-standard -- "${go_mod_files[@]}"
)
mapfile -t deleted_files < <(git diff --name-only --diff-filter=D -- "${go_mod_files[@]}")

if [[ ${#changed_files[@]} -eq 0 && ${#deleted_files[@]} -eq 0 ]]; then
  echo "${module_dir} is tidy, nothing to commit."
  exit 0
fi

echo git diff:
git --no-pager diff -- "${go_mod_files[@]}"

commit_message="chore(deps): run go mod tidy for ${module_dir}"
commit_body=$(cat <<EOF
Dependabot has updated the requirements of images/pkg/common, which ${module_dir} replaces with a local path, without
updating ${module_dir}. This commit runs "go mod tidy" for it.
EOF
)

# The base64-encoded file contents and the additions are passed to jq via --rawfile and --slurpfile, not via --arg and
# --argjson: they can exceed the maximum length of a single command line argument (MAX_ARG_STRLEN, 128 KiB).
contents_file=$(mktemp)
additions_file=$(mktemp)
trap 'rm -f "$contents_file" "$additions_file"' EXIT
for file in "${changed_files[@]}"; do
  # Reading from stdin and stripping the line breaks afterwards keeps this working with both GNU base64 (which wraps
  # at 76 characters unless -w0 is given) and BSD base64 (which has no -w and needs -i for a file argument).
  base64 < "$file" | tr -d '\n' > "$contents_file"
  jq -n --arg path "$file" --rawfile contents "$contents_file" '{path: $path, contents: $contents}' >> "$additions_file"
done

# Let "gh api graphql"/createCommitOnBranch create the commit via the GitHub API rather than "git commit"/"git push", so
# commits are automatically signed.
# Note: expectedHeadOid is an optimistic lock: if Dependabot has pushed to the branch in the meantime, the commit is
# rejected instead of being based on an outdated state of the branch.
deletions=$(
  for file in "${deleted_files[@]}"; do
    jq -n --arg path "$file" '{path: $path}'
  done | jq -s '.'
)

commit_oid=$(
  jq -n \
    --arg repo "$GITHUB_REPOSITORY" \
    --arg branch "$PR_HEAD_REF" \
    --arg headline "$commit_message" \
    --arg body "$commit_body" \
    --arg oid "$PR_HEAD_SHA" \
    --slurpfile additions "$additions_file" \
    --argjson deletions "$deletions" \
    '{
      query: "mutation($input: CreateCommitOnBranchInput!) { createCommitOnBranch(input: $input) { commit { oid } } }",
      variables: {
        input: {
          branch:          { repositoryNameWithOwner: $repo, branchName: $branch },
          message:         { headline: $headline, body: $body },
          expectedHeadOid: $oid,
          fileChanges:     { additions: $additions, deletions: $deletions }
        }
      }
    }' | gh api graphql --input - --jq '.data.createCommitOnBranch.commit.oid'
)

echo "created commit ${commit_oid} on ${PR_HEAD_REF}"

comment_body=$(cat <<EOF
Pushed ${commit_oid}, which runs \`go mod tidy\` for \`${module_dir}\`.

\`${module_dir}\` replaces \`images/pkg/common\` with a local path. Dependabot has updated the requirements of
\`images/pkg/common\` without updating \`${module_dir}\`, which makes it fail to build with
\`go: updates to go.mod needed\`.

**CI has not run for ${commit_oid}**: the commit has been created with the workflow's \`GITHUB_TOKEN\`, and commits
created with the \`GITHUB_TOKEN\` do not trigger workflow runs. To run CI, amend the commit and force-push it, for
example with \`git commit --amend --no-edit && git push --force-with-lease\`.

Dependabot does not rebase pull requests that contain commits from someone else. \`@dependabot recreate\` discards this
commit, and this workflow then runs again for the recreated pull request.
EOF
)

gh pr comment "$PR_NUMBER" --repo "$GITHUB_REPOSITORY" --body "$comment_body"
