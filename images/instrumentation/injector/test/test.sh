#/bin/env bash

readonly script_dir=$( cd -- "$( dirname -- "${BASH_SOURCE[0]}" )" &> /dev/null && pwd )

function run_tests () {
    local runtime="${1}"
    local docker_image="${2}"

    for t in ${script_dir}/${runtime}/test-cases/*/ ; do
        test=$(basename $(realpath "${t}"))
        printf '%s' "Test '${test}' " | sed 's/^/  /' # `echo -n` does not work on Mac OS
        tempfile=$(mktemp)
        docker run ${docker_image} "npm" "start" "--prefix" "/test-cases/${test}" > ${tempfile} 2>&1
        if [ $? -eq 0 ]; then
            echo OK
        else
            echo FAIL
            echo 'Output (stdout + stderr):'
            cat ${tempfile} | sed 's/^/    /'
        fi
        rm ${tempfile}
    done
}

# Build instr image
instr_image="${1}"

if [ -z "${instr_image}" ]; then    
    instr_image="dash0-instrumentation:test-latest"
    echo "Building instrumentation image '${instr_image}' (no instrumentation image tag provided in input)"

    tempfile=$(mktemp)
    if ! docker build ../.. -t "${instr_image}" > ${tempfile} 2>&1; then
        echo "Failed to build the instrumentation image:"
        cat ${tempfile} | sed 's/^/  /'
    fi
    rm ${tempfile}
fi

# Run tests
for r in ${script_dir}/*/ ; do
    runtime=$(basename $(realpath "${r}"))
    echo "Runtime '${runtime}'"
    grep '^[^#;]' "${script_dir}/${runtime}/base-images" | while read -r base_image ; do
        echo "Base image '${base_image}'" | sed 's/^/  /'
        build_output=$(docker build "${script_dir}/${runtime}" --build-arg "instr_image=${instr_image}" --build-arg "base_image=${base_image}" -t "test-${runtime}:latest" 2>&1 | sed 's/^/    /')
        if [ $? -ne 0 ]; then
            echo "${build_output}"
            exit 1
        fi
        run_tests "${runtime}" "test-${runtime}:latest" | sed 's/^/  /'
    done
done