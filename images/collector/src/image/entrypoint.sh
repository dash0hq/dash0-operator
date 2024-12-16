#!/bin/sh

if [ -z "${KUBELET_STATS_TLS_INSECURE:-}" ]; then
    if [ "${K8S_NODE_NAME:-}" = "minikube" ]; then
        export "KUBELET_STATS_TLS_INSECURE=true"
        echo "This collector seems to run on a node managed by Minikube, which is known to have self-signed CA certs for the Kubelet stats endpoint: disabling TLS verification for the kubeletstat receiver"
    elif [ "${K8S_NODE_NAME:-}" = "docker-desktop" ]; then
        export "KUBELET_STATS_TLS_INSECURE=true"
        echo "This collector seems to run on a node managed by Docker Desktop's Kubernetes, which is known to have self-signed CA certs for the Kubelet stats endpoint: disabling TLS verification for the kubeletstat receiver"
    else
        export "KUBELET_STATS_TLS_INSECURE=false"
    fi
fi

./otelcol "$@" &

DASH0_COLLECTOR_PID=$!

mkdir -p "$(dirname "${DASH0_COLLECTOR_PID_FILE}")"

printf "%s" "${DASH0_COLLECTOR_PID}" > "${DASH0_COLLECTOR_PID_FILE}"

printf "Collector pid file created at \"%s\": " "${DASH0_COLLECTOR_PID_FILE}"
cat "${DASH0_COLLECTOR_PID_FILE}"
echo

wait ${DASH0_COLLECTOR_PID}
