#!/usr/bin/env bash

# SPDX-FileCopyrightText: Copyright 2026 Dash0 Inc.
# SPDX-License-Identifier: Apache-2.0

# Installs Kyverno and the upstream Kyverno policies into the cluster of the current kube context, for testing the
# operator against restrictive admission policies. The policies (Pod Security Standards "restricted", plus
# require-non-root-groups and require-ro-rootfs) are installed as ClusterPolicy resources in Enforce mode, with the
# upstream policy and rule names.
#
# Kyverno only receives admission requests for the namespaces in KYVERNO_ENFORCED_NAMESPACES (comma-separated, defaults
# to the operator namespace OPERATOR_NAMESPACE), so that cert-manager, ingress-nginx, test applications etc. in other
# namespaces are not rejected.
#
# Unless DEPLOY_KYVERNO_POLICY_EXCEPTION=false, a PolicyException for the collector DaemonSet of the Helm release
# dash0-operator is created, matching the example in helm-chart/dash0-operator/docs/platform-specific.md plus the
# disallow-host-ports policy. It is created in the Kyverno namespace, since the test scenario scripts delete the operator
# namespace.
#
# Running the script again updates the installation. test-resources/bin/delete-kyverno.sh removes Kyverno again.

set -euo pipefail

project_root="$(dirname "${BASH_SOURCE[0]}")"/../..
scripts_lib="test-resources/bin/lib"

cd "$project_root"

# shellcheck source=./lib/constants
source "$scripts_lib/constants"

# shellcheck source=./lib/util
source "$scripts_lib/util"

load_env_file
verify_kubectx

operator_namespace="${OPERATOR_NAMESPACE:-$default_operator_ns}"
enforced_namespaces="${KYVERNO_ENFORCED_NAMESPACES:-$operator_namespace}"
collector_daemonset_name=dash0-operator-opentelemetry-collector-agent-daemonset

kyverno_values=$(mktemp)
kyverno_policies_values=$(mktemp)
trap 'rm -f "$kyverno_values" "$kyverno_policies_values"' EXIT

{
  echo "config:"
  echo "  webhooks:"
  echo "    namespaceSelector:"
  echo "      matchExpressions:"
  echo "        - key: kubernetes.io/metadata.name"
  echo "          operator: In"
  echo "          values:"
  IFS=',' read -r -a namespaces <<< "$enforced_namespaces"
  for namespace in "${namespaces[@]}"; do
    namespace=$(trim "$namespace")
    if [[ -n "$namespace" ]]; then
      echo "            - $namespace"
    fi
  done
  echo "features:"
  echo "  policyExceptions:"
  echo "    enabled: true"
  echo "    namespace: $kyverno_namespace"
  # Keep all policies in the Kyverno webhook, which only receives requests for the namespaces listed above, instead of
  # generating ValidatingAdmissionPolicies from them.
  echo "  generateValidatingAdmissionPolicy:"
  echo "    enabled: false"
} > "$kyverno_values"

cat > "$kyverno_policies_values" <<EOF
policyType: ClusterPolicy
podSecurityStandard: restricted
validationFailureAction: Enforce
includeOtherPolicies:
  - require-non-root-groups
customPolicies:
  # A copy of best-practices/require-ro-rootfs from github.com/kyverno/policies, branch release-1.19, in Enforce mode.
  - apiVersion: kyverno.io/v1
    kind: ClusterPolicy
    metadata:
      name: require-ro-rootfs
    spec:
      validationFailureAction: Enforce
      background: true
      rules:
        - name: validate-readOnlyRootFilesystem
          match:
            any:
              - resources:
                  kinds:
                    - Pod
          validate:
            message: "Root filesystem must be read-only."
            pattern:
              spec:
                containers:
                  - securityContext:
                      readOnlyRootFilesystem: true
EOF

echo "installing Kyverno (chart version $kyverno_chart_version)," \
  "enforcing policies in the namespaces $enforced_namespaces"
helm upgrade --install \
  --repo "$kyverno_chart_repo" \
  --version "$kyverno_chart_version" \
  --namespace "$kyverno_namespace" \
  --create-namespace \
  --values "$kyverno_values" \
  --wait \
  --timeout 5m \
  kyverno \
  kyverno

echo "installing the Kyverno policies (chart version $kyverno_chart_version)"
helm upgrade --install \
  --repo "$kyverno_chart_repo" \
  --version "$kyverno_chart_version" \
  --namespace "$kyverno_namespace" \
  --values "$kyverno_policies_values" \
  --wait \
  --timeout 5m \
  kyverno-policies \
  kyverno-policies

echo "waiting for the Kyverno policies to become ready"
kubectl wait --for=condition=Ready --timeout=2m clusterpolicy --all

if [[ "${DEPLOY_KYVERNO_POLICY_EXCEPTION:-}" != "false" ]]; then
  echo "creating a PolicyException in the namespace $kyverno_namespace for the collector DaemonSet in the namespace" \
    "$operator_namespace"
  kubectl apply -f - <<EOF
apiVersion: kyverno.io/v2
kind: PolicyException
metadata:
  name: dash0-collector-agent
  namespace: $kyverno_namespace
spec:
  exceptions:
    - policyName: disallow-host-path
      ruleNames: [host-path, autogen-host-path]
    - policyName: restrict-volume-types
      ruleNames: [restricted-volumes, autogen-restricted-volumes]
    - policyName: require-non-root-groups
      ruleNames: [check-runasgroup, autogen-check-runasgroup]
    - policyName: disallow-host-ports
      ruleNames: [host-ports-none, autogen-host-ports-none]
  match:
    any:
      - resources:
          kinds: [DaemonSet]
          namespaces: [$operator_namespace]
          names: [$collector_daemonset_name]
      - resources:
          kinds: [Pod]
          namespaces: [$operator_namespace]
          names: [$collector_daemonset_name-*]
EOF
else
  kubectl delete policyexception dash0-collector-agent --namespace "$kyverno_namespace" --ignore-not-found
fi

echo "Kyverno is ready."
