#!/usr/bin/env bash
# shellcheck disable=SC2059

# SPDX-FileCopyrightText: Copyright 2024 Dash0 Inc.
# SPDX-License-Identifier: Apache-2.0

set -euo pipefail
shopt -s lastpipe

start_time_build=$(date +%s)

RED='\033[0;31m'
GREEN='\033[0;32m'
NC='\033[0m'

cd "$(dirname "${BASH_SOURCE[0]}")"/..

# shellcheck source=images/instrumentation/test/build_time_profiling
source ./test/build_time_profiling

trap print_total_build_time_info EXIT

# shellcheck source=images/instrumentation/injector/test/scripts/util
source injector/test/scripts/util


echo ----------------------------------------
instrumentation_image="dash0-instrumentation:latest"
all_docker_platforms=linux/arm64,linux/amd64
script_dir="test"
test_exit_code=0
summary=""
slow_test_threshold_seconds=10
architectures=""
if [[ -n "${ARCHITECTURES:-}" ]]; then
  architectures=("${ARCHITECTURES//,/ }")
  echo Only testing a subset of architectures: "${architectures[@]}"
fi
runtimes=""
if [[ -n "${RUNTIMES:-}" ]]; then
  runtimes=("${RUNTIMES//,/ }")
  echo Only testing a subset of runtimes: "${runtimes[@]}"
fi
base_images=""
if [[ -n "${BASE_IMAGES:-}" ]]; then
  base_images=("${BASE_IMAGES//,/ }")
  echo Only testing a subset of base images: "${base_images[@]}"
fi
test_cases=""
if [[ -n "${TEST_CASES:-}" ]]; then
  test_cases=("${TEST_CASES//,/ }")
  echo Only running a subset of test cases : "${test_cases[@]}"
fi

build_or_pull_instrumentation_image() {
  # shellcheck disable=SC2155
  local start_time_step=$(date +%s)
  if [[ -n "${INSTRUMENTATION_IMAGE:-}" ]]; then
    instrumentation_image="$INSTRUMENTATION_IMAGE"

    if is_remote_image "$instrumentation_image"; then
      echo ----------------------------------------
      echo "fetching instrumentation image from remote repository: $instrumentation_image"
      echo ----------------------------------------
      echo "$instrumentation_image" >> test/.container_images_to_be_deleted_at_end
      docker pull "$instrumentation_image"
    else
      echo ----------------------------------------
      echo "using existing local instrumentation image: $instrumentation_image"
      echo ----------------------------------------
    fi
    store_build_step_duration "pull instrumentation image" "$start_time_step"
  else
    echo ----------------------------------------
    echo "building multi-arch instrumentation image for platforms ${all_docker_platforms} from local sources"
    echo ----------------------------------------

    echo "$instrumentation_image" >> test/.container_images_to_be_deleted_at_end
    if ! docker_build_output=$(
      docker build \
      --platform "$all_docker_platforms" \
      . \
      -t "${instrumentation_image}" \
      2>&1
    ); then
      echo "${docker_build_output}"
      exit 1
    fi
    if [[ "${PRINT_DOCKER_OUTPUT:-}" = "true" ]]; then
      echo "${docker_build_output}"
    fi

    store_build_step_duration "build instrumentation image" "$start_time_step"
  fi
  echo
}

run_tests_for_runtime() {
  local arch="${1:-}"
  local docker_platform="${2:-}"
  local runtime="${3:-}"
  local image_name_test="${4:-}"
  local container_name_test_prefix="${5:-}"
  local base_image="${6:-}"

  if [[ -z "$docker_platform" ]]; then
    echo "missing parameter: docker_platform"
    exit 1
  fi
  if [[ -z "$runtime" ]]; then
    echo "missing parameter: runtime"
    exit 1
  fi
  if [[ -z "$image_name_test" ]]; then
    echo "missing parameter: image_name_test"
    exit 1
  fi
  if [[ -z "$container_name_test_prefix" ]]; then
    echo "missing parameter: container_name_test_prefix"
    exit 1
  fi
  if [[ -z "$base_image" ]]; then
    echo "missing parameter: base_image"
    exit 1
  fi

  for t in "${script_dir}"/"${runtime}"/test-cases/*/ ; do
    if [[ -n "${test_cases[0]}" ]]; then
      run_this_test_case="false"
      for selected_test_case in "${test_cases[@]}"; do
        if [[ "$t" =~ $selected_test_case ]]; then
          run_this_test_case="true"
        fi
      done
      if [[ "$run_this_test_case" != "true" ]]; then
        echo "- skipping test case $t"
        continue
      fi
    fi

    # shellcheck disable=SC2155
    local start_time_test_case=$(date +%s)
    test=$(basename "$(realpath "${t}")")

    case "$runtime" in

      jvm)
        # Suppressing auto instrumentations saves a few seconds per test case, which amounts to a lot for the whole
        # test suite. We do not test tracing in the instrumentation image tests, so instrumenting is not needed.
        # The Java agent still unfortunately adds a couple of seconds of startup overhead.
        # Note: Add -Dotel.javaagent.debug=true for troubleshooting details.
        test_cmd=(java -jar)
        if [[ -f "${script_dir}/${runtime}/test-cases/${test}/system.properties" ]]; then
          while IFS= read -r prop; do
            test_cmd+=("$prop")
          done < "${script_dir}/${runtime}/test-cases/${test}/system.properties"
        fi
        test_cmd+=(-Dotel.instrumentation.common.default-enabled=false "/test-cases/${test}/app.jar")
        ;;

      node)
        test_cmd=(node "/test-cases/${test}")
        ;;

      *)
        echo "Error: Test handler for runtime \"$runtime\" is not implemented. Please update $(basename "$0"), function run_tests_for_runtime."
        exit 1
        ;;
    esac

    container_name="$container_name_test_prefix-$test"
    echo "$container_name" >> test/.containers_to_be_deleted_at_end
    docker rm -f "$container_name" &> /dev/null
    if docker_run_output=$(docker run \
      --platform "$docker_platform" \
      --env-file="${script_dir}/${runtime}/test-cases/${test}/.env" \
      --name "$container_name" \
      "$image_name_test" \
      "${test_cmd[@]}" \
      2>&1
    ); then
      if [[ "${PRINT_DOCKER_OUTPUT:-}" = "true" ]]; then
        echo "$docker_run_output"
      fi
      printf "${GREEN}test case \"${test}\": OK${NC}\n"
    else
      printf "${RED}test case \"${test}\": FAIL\n"
      echo "test command was:"
      echo "${test_cmd[@]}"
      printf "test output:${NC}\n"
      echo "$docker_run_output"
      test_exit_code=1
      summary="$summary\n${runtime}/${base_image}\t- ${test}:\tfailed"
    fi

    # shellcheck disable=SC2155
    local end_time_test_case=$(date +%s)
    # shellcheck disable=SC2155
    local duration_test_case=$((end_time_test_case - start_time_test_case))
    if [[ "$duration_test_case" -gt "$slow_test_threshold_seconds" ]]; then
      echo "! slow test case: $image_name_test/$base_image/$test: took $duration_test_case seconds, logging output:"
      echo "$docker_run_output"
    fi

    store_build_step_duration "test case $test" "$start_time_test_case" "$arch" "$runtime" "$base_image"
  done
}

run_tests_for_architecture() {
  arch="${1:-}"
  if [[ -z ${arch} ]]; then
    echo "missing mandatory argument: architecture"
    exit 1
  fi
  if [ "$arch" = arm64 ]; then
    docker_platform=linux/arm64
  elif [ "$arch" = x86_64 ]; then
    docker_platform=linux/amd64
  else
    echo "The architecture $arch is not supported."
    exit 1
  fi

  echo ========================================
  echo "running tests for architecture $arch"
  echo ========================================
  for r in "${script_dir}"/*/ ; do
    runtime=$(basename "$(realpath "${r}")")

    if [[ ! -e ${script_dir}/${runtime}/base-images ]]; then
      continue
    fi
    if [[ ! -d ${script_dir}/${runtime}/test-cases ]]; then
      continue
    fi
    if [[ -n "${runtimes[0]}" ]]; then
      if [[ $(echo "${runtimes[@]}" | grep -o "$runtime" | wc -w) -eq 0 ]]; then
        echo ----------------------------------------
        echo "- skipping runtime $runtime"
        continue
      fi
    fi

    echo ----------------------------------------
    echo "- runtime: '${runtime}'"
    echo
    local base_images_for_runtime
    base_images_for_runtime=$(grep '^[^#;]' "${script_dir}/${runtime}/base-images")
    while read -r base_image ; do
      if [[ -n "${base_images[0]}" ]]; then
        if [[ $(echo "${base_images[@]}" | grep -o "$base_image" | wc -w) -eq 0 ]]; then
          echo --------------------
          echo "- skipping base image $base_image"
          continue
        fi
      fi

      echo --------------------
      echo "- base image: '${base_image}'"
      container_name_test_prefix="instrumentation-image-test-${runtime}-${arch}"
      image_name_test="instrumentation-image-test-${runtime}-${arch}:latest"
      echo "building test image \"$image_name_test\" for ${arch}/${runtime}/${base_image} with instrumentation image ${instrumentation_image}"
      # shellcheck disable=SC2155
      local start_time_docker_build=$(date +%s)
      echo "$image_name_test" >> test/.container_images_to_be_deleted_at_end
      if ! docker_build_output=$(
        docker build \
          --platform "$docker_platform" \
          --build-arg "instrumentation_image=${instrumentation_image}" \
          --build-arg "base_image=${base_image}" \
          "${script_dir}/${runtime}" \
          -t "$image_name_test" \
          2>&1
      ); then
        echo "${docker_build_output}"
        exit 1
      fi
      if [[ "${PRINT_DOCKER_OUTPUT:-}" = "true" ]]; then
        echo "${docker_build_output}"
      fi
      store_build_step_duration "docker build" "$start_time_docker_build" "$arch" "$runtime" "$base_image"
      run_tests_for_runtime "$arch" "$docker_platform" "${runtime}" "$image_name_test" "$container_name_test_prefix" "$base_image"
      echo
    done <<< "$base_images_for_runtime"
  done
  echo
  echo
}

if [[ "${CI:-false}" != "true" ]]; then
  dockerDriver="$(docker info -f '{{ .DriverStatus }}')"
  if [[ "$dockerDriver" != *"io.containerd."* ]]; then
    echo "Error: This script requires that the containerd image store is enabled for Docker, since the script needs to build and use multi-arch images locally. You driver is $dockerDriver. Please see https://docs.docker.com/desktop/containerd/#enable-the-containerd-image-store for instructions on enabling the containerd image store."
    exit 1
  fi
fi

# Remember which containers have been created and which container images have been built or pulled for the test run and
# delete them at the end via a trap. Otherwise, on systems where the Docker VM has limited disk space (like Docker
# Desktop on MacOS), the test run will leave behind some fairly large images and hog disk space.
rm -f test/.containers_to_be_deleted_at_end
rm -f test/.container_images_to_be_deleted_at_end
touch test/.containers_to_be_deleted_at_end
touch test/.container_images_to_be_deleted_at_end
trap cleanup_docker_containers_and_images_instrumentation_image_tests EXIT

build_or_pull_instrumentation_image

declare -a all_architectures=(
  "arm64"
  "x86_64"
)

for arch in "${all_architectures[@]}"; do
  if [[ -n "${architectures[0]}" ]]; then
    if [[ $(echo "${architectures[@]}" | grep -o "$arch" | wc -w) -eq 0 ]]; then
      echo ========================================
      echo "- skipping CPU architecture $arch"
      echo ========================================
      continue
    fi
  fi
  run_tests_for_architecture "$arch"
done

if [[ $test_exit_code -ne 0 ]]; then
  printf "\n${RED}There have been failing test cases:"
  printf "$summary\n"
  printf "\nSee above for details.${NC}\n"
else
  printf "\n${GREEN}All test cases have passed.${NC}\n"
fi
exit $test_exit_code

