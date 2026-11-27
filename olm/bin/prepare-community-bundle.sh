#!/usr/bin/env bash

# SPDX-FileCopyrightText: Copyright 2026 Dash0 Inc.
# SPDX-License-Identifier: Apache-2.0

# Prepares the submission of the release COMMUNITY_BUNDLE_VERSION to a community catalog in COMMUNITY_OPERATORS_DIR.
# COMMUNITY_CATALOG selects the catalog:
# * openshift (default): the OpenShift community catalog, COMMUNITY_OPERATORS_DIR is a clone of (a fork of)
#   redhat-openshift-ecosystem/community-operators-prod.
# * operatorhub: the catalog of operatorhub.io, COMMUNITY_OPERATORS_DIR is a clone of (a fork of)
#   k8s-operatorhub/community-operators.
# Copies the bundle from the release bundle image ghcr.io/dash0hq/dash0-operator-bundle:<version> into
# operators/dash0-operator/<version>/ and validates it like the pipeline of the catalog does. For openshift, it also
# adds the release-config.yaml of the bundle. For the first submission (no operators/dash0-operator yet), it also writes
# ci.yaml with the reviewers COMMUNITY_REVIEWERS (comma-separated GitHub user names), and for openshift the catalog
# template catalog-templates/semver.yaml. Pulls the bundle image with CONTAINER_TOOL (docker or podman). Use
# "make olm-community-bundle" instead of calling this directly.

set -euo pipefail

operators_repo_dir="${COMMUNITY_OPERATORS_DIR:?COMMUNITY_OPERATORS_DIR is required}"
version="${COMMUNITY_BUNDLE_VERSION:?COMMUNITY_BUNDLE_VERSION is required}"
operator_sdk="${OPERATOR_SDK:?OPERATOR_SDK is required}"
community_catalog="${COMMUNITY_CATALOG:-openshift}"
container_tool="${CONTAINER_TOOL:-docker}"
reviewers="${COMMUNITY_REVIEWERS:-}"

package=dash0-operator
bundle_image="ghcr.io/dash0hq/dash0-operator-bundle:$version"
# The OpenShift pipeline validates the bundle for the Kubernetes version of the newest OpenShift version. Its mapping
# table ends at OpenShift 4.17, so every newer OpenShift version maps to Kubernetes 1.30.
validation_k8s_version=1.30
# minKubeVersion 1.25.16 of the CSV is above Kubernetes 1.25.0 of OpenShift 4.12.
lowest_catalog_minor_version_of_v4=13

case "$community_catalog" in
  openshift)
    upstream_repository=redhat-openshift-ecosystem/community-operators-prod
    ;;
  operatorhub)
    upstream_repository=k8s-operatorhub/community-operators
    ;;
  *)
    echo "error: COMMUNITY_CATALOG $community_catalog is not supported, use openshift or operatorhub"
    exit 1
    ;;
esac

if [[ ! "$version" =~ ^[0-9]+\.[0-9]+\.[0-9]+$ ]]; then
  echo "error: COMMUNITY_BUNDLE_VERSION $version is not a release version (x.y.z), release candidates are not submitted"
  exit 1
fi
if [[ ! -d "$operators_repo_dir/operators" ]]; then
  echo "error: COMMUNITY_OPERATORS_DIR $operators_repo_dir has no directory operators, it is not a clone of" \
    "$upstream_repository"
  exit 1
fi
# Both repositories have a config.yaml, only the one of k8s-operatorhub/community-operators sets the cluster type k8s.
is_kubernetes_catalog_repo=false
if [[ -f "$operators_repo_dir/config.yaml" ]] && grep -q -x -F "cluster: k8s" "$operators_repo_dir/config.yaml"; then
  is_kubernetes_catalog_repo=true
fi
if [[ ("$community_catalog" = operatorhub && "$is_kubernetes_catalog_repo" = false) ||
  ("$community_catalog" = openshift && "$is_kubernetes_catalog_repo" = true) ]]; then
  echo "error: COMMUNITY_OPERATORS_DIR $operators_repo_dir is not a clone of $upstream_repository" \
    "(COMMUNITY_CATALOG=$community_catalog)"
  exit 1
fi
if [[ "$community_catalog" = operatorhub && ! -f "$operators_repo_dir/categories.json" ]]; then
  echo "error: COMMUNITY_OPERATORS_DIR $operators_repo_dir has no categories.json, which the bundle validation needs"
  exit 1
fi

operator_dir="$operators_repo_dir/operators/$package"
bundle_dir="$operator_dir/$version"
if [[ -e "$bundle_dir" ]]; then
  echo "error: $bundle_dir exists already. A version that has been merged cannot be changed anymore, a directory that" \
    "is left over from an earlier run of this script can be deleted."
  exit 1
fi

first_submission=false
reviewer_list=()
catalogs=()
if [[ ! -e "$operator_dir" ]]; then
  first_submission=true
  IFS=',' read -r -a reviewer_entries <<< "$reviewers"
  for reviewer in ${reviewer_entries[@]+"${reviewer_entries[@]}"}; do
    reviewer="${reviewer//[[:space:]]/}"
    if [[ -z "$reviewer" ]]; then
      continue
    fi
    if [[ ! "$reviewer" =~ ^[A-Za-z0-9-]+$ ]]; then
      echo "error: COMMUNITY_REVIEWERS contains \"$reviewer\", which is not a GitHub user name"
      exit 1
    fi
    reviewer_list+=("$reviewer")
  done
  if [[ ${#reviewer_list[@]} -eq 0 ]]; then
    echo "error: $operator_dir does not exist yet, so this is the first submission, which needs COMMUNITY_REVIEWERS" \
      "(comma-separated GitHub user names) for ci.yaml"
    exit 1
  fi

  if [[ "$community_catalog" = openshift ]]; then
    if [[ ! -d "$operators_repo_dir/catalogs" ]]; then
      echo "error: COMMUNITY_OPERATORS_DIR $operators_repo_dir has no directory catalogs, which the first submission" \
        "needs for the catalog list in ci.yaml"
      exit 1
    fi
    catalog_candidates=()
    for catalog_path in "$operators_repo_dir"/catalogs/v*; do
      catalog="$(basename -- "$catalog_path")"
      if [[ -d "$catalog_path" && "$catalog" =~ ^v([0-9]+)\.([0-9]+)$ ]]; then
        major="${BASH_REMATCH[1]}"
        minor="${BASH_REMATCH[2]}"
        if ((major > 4 || (major == 4 && minor >= lowest_catalog_minor_version_of_v4))); then
          catalog_candidates+=("$catalog")
        fi
      fi
    done
    if [[ ${#catalog_candidates[@]} -eq 0 ]]; then
      echo "error: $operators_repo_dir/catalogs has no catalog from v4.$lowest_catalog_minor_version_of_v4 on"
      exit 1
    fi
    while IFS= read -r catalog; do
      catalogs+=("$catalog")
    done < <(printf '%s\n' "${catalog_candidates[@]}" | sort -V)
  fi
elif [[ ! -f "$operator_dir/ci.yaml" ]]; then
  echo "error: $operator_dir exists but has no ci.yaml"
  exit 1
fi

# The submission is staged first and only moved into the clone after all checks have passed, so that a failed run leaves
# nothing behind that blocks the next run or turns it into a later submission.
staging_dir="$(mktemp -d)"
container_id=""
cleanup() {
  if [[ -n "$container_id" ]]; then
    "$container_tool" rm "$container_id" > /dev/null || true
  fi
  rm -rf "$staging_dir"
}
trap cleanup EXIT

echo "copying the bundle from the image $bundle_image"
"$container_tool" pull "$bundle_image"
# The bundle image is built from scratch and has no command, create needs one although the container never runs.
container_id="$("$container_tool" create "$bundle_image" /bin/true)"
staged_operator_dir="$staging_dir/$package"
staged_bundle_dir="$staged_operator_dir/$version"
mkdir -p "$staged_bundle_dir"
for path in manifests metadata tests; do
  "$container_tool" cp "$container_id:/$path" "$staged_bundle_dir/$path"
done

# The pipeline requires the directory name to be the CSV version and the same operator name in the CSV, in
# metadata/annotations.yaml and in the directory name.
csv_files=("$staged_bundle_dir"/manifests/*.clusterserviceversion.yaml)
if [[ ${#csv_files[@]} -ne 1 || ! -f "${csv_files[0]}" ]]; then
  echo "error: the bundle of $bundle_image does not have exactly one ClusterServiceVersion in manifests/"
  exit 1
fi
if ! grep -q -x -F "  name: $package.v$version" "${csv_files[0]}"; then
  echo "error: the ClusterServiceVersion of $bundle_image does not have the name $package.v$version"
  exit 1
fi
if ! grep -q -x -F "  version: $version" "${csv_files[0]}"; then
  echo "error: the ClusterServiceVersion of $bundle_image does not have the version $version"
  exit 1
fi
if ! grep -q -x -F "  operators.operatorframework.io.bundle.package.v1: $package" \
  "$staged_bundle_dir/metadata/annotations.yaml"; then
  echo "error: metadata/annotations.yaml of $bundle_image does not have the package $package"
  exit 1
fi

echo "validating the bundle with the operator-sdk validators of the $upstream_repository pipeline"
if [[ "$community_catalog" = openshift ]]; then
  for selector in name=operatorhub suite=operatorframework; do
    "$operator_sdk" bundle validate "$staged_bundle_dir" --select-optional "$selector" \
      --optional-values=k8s-version="$validation_k8s_version"
  done
else
  OPERATOR_BUNDLE_CATEGORIES="$operators_repo_dir/categories.json" \
    "$operator_sdk" bundle validate "$staged_bundle_dir" --select-optional suite=operatorframework

  # Checks of the operatorhub.io pipeline that operator-sdk does not cover. With updateGraph semver-mode, the CSV must
  # not have replaces or skips, and the image of the containerImage annotation has to be used in the CSV as well.
  if grep -q -E "^  (replaces|skips):" "${csv_files[0]}"; then
    echo "error: the ClusterServiceVersion of $bundle_image has replaces or skips, which updateGraph semver-mode" \
      "does not allow"
    exit 1
  fi
  container_image="$(sed -n -E "s/^    containerImage: (.+)$/\1/p" "${csv_files[0]}")"
  if [[ -z "$container_image" ]]; then
    echo "error: the ClusterServiceVersion of $bundle_image has no containerImage annotation"
    exit 1
  fi
  if (($(grep -c -F "$container_image" "${csv_files[0]}") < 2)); then
    echo "error: the ClusterServiceVersion of $bundle_image does not use the image $container_image of its" \
      "containerImage annotation"
    exit 1
  fi
fi

if [[ "$community_catalog" = openshift ]]; then
  # The olm.semver template only allows the channels Fast, Stable and Candidate. Stable becomes the channel stable-v0.
  cat > "$staged_bundle_dir/release-config.yaml" << EOF
---
catalog_templates:
  - template_name: semver.yaml
    channels: [Stable]
EOF
fi

if [[ "$first_submission" = true ]]; then
  {
    echo "---"
    if [[ "$community_catalog" = operatorhub ]]; then
      # The upgrade edges follow the semver order of the versions in a channel, the bundles have no replaces.
      echo "updateGraph: semver-mode"
    fi
    echo "reviewers:"
    for reviewer in "${reviewer_list[@]}"; do
      echo "  - $reviewer"
    done
    if [[ "$community_catalog" = openshift ]]; then
      # With version_promotion_strategy always, the operator is added to the catalog of every new OpenShift version
      # without waiting for the approval of a reviewer.
      echo "fbc:"
      echo "  enabled: true"
      echo "  version_promotion_strategy: always"
      echo "  catalog_mapping:"
      echo "    - template_name: semver.yaml"
      echo "      type: olm.semver"
      echo "      catalog_names:"
      for catalog in "${catalogs[@]}"; do
        echo "        - $catalog"
      done
    fi
  } > "$staged_operator_dir/ci.yaml"
  written_files="ci.yaml, $version/"
  if [[ "$community_catalog" = openshift ]]; then
    mkdir -p "$staged_operator_dir/catalog-templates"
    # Without this template, the release pipeline creates one with GenerateMinorChannels: true, i.e. an additional
    # channel per minor version. It adds the bundles itself.
    cat > "$staged_operator_dir/catalog-templates/semver.yaml" << EOF
---
Schema: olm.semver
GenerateMajorChannels: true
GenerateMinorChannels: false
EOF
    written_files="ci.yaml, catalog-templates/semver.yaml, $version/"
  fi
  mv "$staged_operator_dir" "$operator_dir"
  echo "the submission has been written to $operator_dir/ ($written_files)"
else
  mv "$staged_bundle_dir" "$bundle_dir"
  echo "the submission has been written to $bundle_dir/"
fi
