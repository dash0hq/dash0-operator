#!/usr/bin/env bash

# SPDX-FileCopyrightText: Copyright 2026 Dash0 Inc.
# SPDX-License-Identifier: Apache-2.0

# Deploys the Dash0 operator (from the local add-ruby branch) onto a fresh kind cluster and
# installs five Ruby demo applications that each exercise a different instrumentation pattern:
#
#   1. rails        – full Rails stack (Rack + ActionController + ActiveSupport)
#   2. sinatra      – lightweight Rack DSL framework
#   3. rack         – pure Rack, no framework
#   4. activerecord – Rails + SQLite (adds ActiveRecord / SQL-query spans)
#   5. http-client  – Rails that calls the Rails service via Net::HTTP (distributed tracing)
#
# Each application includes a traffic-generator sidecar that continuously sends requests, so
# traces appear in Dash0 immediately without manual intervention.
#
# Usage:
#   # First copy and edit the config file:
#   cp test-resources/ruby-demo.env.template test-resources/ruby-demo.env
#   # Edit test-resources/ruby-demo.env and set at minimum DASH0_AUTHORIZATION_TOKEN.
#   # Then run:
#   test-resources/bin/deploy-ruby-demo.sh [--skip-build] [--skip-cluster]
#
# Options:
#   --skip-cluster   Skip kind cluster creation (use if the cluster already exists).
#   --skip-build     Skip building Docker images (use if images are already built and loaded).

set -euo pipefail

project_root="$(cd "$(dirname "${BASH_SOURCE[0]}")"/../.. && pwd)"
cd "$project_root"

###############################################################################
# Parse flags
###############################################################################

SKIP_CLUSTER=false
SKIP_BUILD=false
for arg in "$@"; do
  case "$arg" in
    --skip-cluster) SKIP_CLUSTER=true ;;
    --skip-build)   SKIP_BUILD=true ;;
  esac
done

###############################################################################
# Load configuration
###############################################################################

env_file="test-resources/ruby-demo.env"
if [[ ! -f "$env_file" ]]; then
  echo "ERROR: $env_file not found."
  echo "Copy test-resources/ruby-demo.env.template to test-resources/ruby-demo.env and fill in your values."
  exit 1
fi
# shellcheck source=/dev/null
source "$env_file"

DASH0_AUTHORIZATION_TOKEN="${DASH0_AUTHORIZATION_TOKEN:-}"
DASH0_INGRESS_ENDPOINT="${DASH0_INGRESS_ENDPOINT:-ingress.eu-west-1.aws.dash0-dev.com:4317}"
DASH0_API_ENDPOINT="${DASH0_API_ENDPOINT:-https://api.eu-west-1.aws.dash0-dev.com}"
DASH0_DATASET="${DASH0_DATASET:-default}"
KIND_CLUSTER_NAME="${KIND_CLUSTER_NAME:-dash0-ruby-demo}"
TARGET_NAMESPACE="${TARGET_NAMESPACE:-ruby-demo}"
OPERATOR_NAMESPACE="${OPERATOR_NAMESPACE:-dash0-system}"

if [[ -z "$DASH0_AUTHORIZATION_TOKEN" ]]; then
  echo "ERROR: DASH0_AUTHORIZATION_TOKEN is not set in $env_file"
  exit 1
fi

echo "================================================================"
echo " Dash0 Ruby Auto-Instrumentation Demo"
echo "================================================================"
echo " Kind cluster:      $KIND_CLUSTER_NAME"
echo " Target namespace:  $TARGET_NAMESPACE"
echo " Operator NS:       $OPERATOR_NAMESPACE"
echo " Dash0 ingress:     $DASH0_INGRESS_ENDPOINT"
echo " Dash0 dataset:     $DASH0_DATASET"
echo "================================================================"
echo

###############################################################################
# Operator image names (built locally, no registry prefix)
###############################################################################

# target-allocator is only needed for Prometheus TargetAllocator and is omitted here.
OPERATOR_IMAGES=(
  "operator-controller:latest"
  "instrumentation:latest"
  "collector:latest"
  "configuration-reloader:latest"
  "filelog-offset-sync:latest"
  "filelog-offset-volume-ownership:latest"
  "agent0-connector:latest"
)

RUBY_APP_IMAGES=(
  "dash0-operator-ruby-rails-test-app:latest"
  "dash0-operator-ruby-sinatra-demo-app:latest"
  "dash0-operator-ruby-rack-demo-app:latest"
  "dash0-operator-ruby-activerecord-demo-app:latest"
  "dash0-operator-ruby-http-client-demo-app:latest"
)

###############################################################################
# Step 1: Create kind cluster
###############################################################################

if [[ "$SKIP_CLUSTER" = "false" ]]; then
  echo "STEP: Creating kind cluster '$KIND_CLUSTER_NAME'..."

  if kind get clusters 2>/dev/null | grep -qx "$KIND_CLUSTER_NAME"; then
    echo "  Cluster '$KIND_CLUSTER_NAME' already exists – skipping creation."
  else
    kind create cluster \
      --name "$KIND_CLUSTER_NAME" \
      --config test-resources/kind-config-ruby-demo.yaml \
      --wait 90s
    echo "  Cluster created."
  fi
  echo
fi

kubectl config use-context "kind-$KIND_CLUSTER_NAME"

###############################################################################
# Step 2: Build Docker images
###############################################################################

if [[ "$SKIP_BUILD" = "false" ]]; then
  echo "STEP: Building operator images..."
  # Build only the images this repo owns (operator-controller and instrumentation injector).
  # The remaining images (collector, configuration-reloader, filelog-offset-sync,
  # filelog-offset-volume-ownership, target-allocator, agent0-connector) are typically already
  # present locally from a previous build. If they exist with a registry prefix
  # (e.g. localhost:5001/), retag them so kind can load them without a prefix.
  make image-controller image-instrumentation

  # Verify the instrumentation image contains the Ruby distribution before proceeding.
  # The build may silently succeed but use a stale cached image without Ruby support.
  if ! docker run --rm --entrypoint ls instrumentation:latest \
    /__otel_auto_instrumentation/agents/ 2>/dev/null | grep -q ruby; then
    echo "ERROR: instrumentation:latest is missing the Ruby distribution."
    echo "  This usually means the image build used a stale Docker cache."
    echo "  Re-run: docker build --no-cache -t instrumentation:latest images/instrumentation"
    exit 1
  fi

  # The remaining images (collector, configuration-reloader, etc.) may exist only with a
  # registry prefix from a previous build. Retag them to be loadable into kind without a prefix.
  # Non-fatal: if an image is missing entirely, the Helm chart will pull it from the registry.
  for img in collector configuration-reloader filelog-offset-sync filelog-offset-volume-ownership agent0-connector; do
    if ! docker inspect "${img}:latest" > /dev/null 2>&1; then
      for prefix in "localhost:5001/" "ghcr.io/dash0hq/"; do
        if docker inspect "${prefix}${img}:latest" > /dev/null 2>&1; then
          echo "  Retagging ${prefix}${img}:latest -> ${img}:latest"
          docker tag "${prefix}${img}:latest" "${img}:latest" 2>/dev/null || \
            echo "  Warning: retag failed for ${prefix}${img}:latest (image may be corrupted locally)"
          break
        fi
      done
    fi
  done
  echo

  echo "STEP: Building Ruby demo application images..."
  docker build -t "dash0-operator-ruby-rails-test-app:latest"        test-resources/ruby/rails
  docker build -t "dash0-operator-ruby-sinatra-demo-app:latest"      test-resources/ruby/sinatra
  docker build -t "dash0-operator-ruby-rack-demo-app:latest"         test-resources/ruby/rack
  docker build -t "dash0-operator-ruby-activerecord-demo-app:latest" test-resources/ruby/activerecord
  docker build -t "dash0-operator-ruby-http-client-demo-app:latest"  test-resources/ruby/http-client
  echo
fi

###############################################################################
# Step 3: Load images into kind
###############################################################################

if [[ "$SKIP_BUILD" = "false" ]]; then
  echo "STEP: Loading operator images into kind..."
  for img in "${OPERATOR_IMAGES[@]}"; do
    echo "  Loading $img..."
    kind load docker-image "$img" --name "$KIND_CLUSTER_NAME" 2>/dev/null || \
      echo "  Warning: could not load $img into kind (image may be missing locally; will use registry pull)"
  done
  echo

  echo "STEP: Loading Ruby demo app images into kind..."
  for img in "${RUBY_APP_IMAGES[@]}"; do
    echo "  Loading $img..."
    kind load docker-image "$img" --name "$KIND_CLUSTER_NAME"
  done
  echo
fi

###############################################################################
# Step 4: Install Dash0 operator with Ruby auto-instrumentation enabled
###############################################################################

echo "STEP: Installing Dash0 operator..."

kubectl create namespace "$OPERATOR_NAMESPACE" --dry-run=client -o yaml | kubectl apply -f -

helm upgrade --install \
  --namespace "$OPERATOR_NAMESPACE" \
  --wait \
  --set operator.image.repository=operator-controller \
  --set operator.image.tag=latest \
  --set operator.image.pullPolicy=IfNotPresent \
  --set operator.instrumentationImage.repository=instrumentation \
  --set operator.instrumentationImage.tag=latest \
  --set operator.instrumentationImage.pullPolicy=IfNotPresent \
  --set operator.collectorImage.repository=collector \
  --set operator.collectorImage.tag=latest \
  --set operator.collectorImage.pullPolicy=IfNotPresent \
  --set operator.configurationReloaderImage.repository=configuration-reloader \
  --set operator.configurationReloaderImage.tag=latest \
  --set operator.configurationReloaderImage.pullPolicy=IfNotPresent \
  --set operator.filelogOffsetSyncImage.repository=filelog-offset-sync \
  --set operator.filelogOffsetSyncImage.tag=latest \
  --set operator.filelogOffsetSyncImage.pullPolicy=IfNotPresent \
  --set operator.filelogOffsetVolumeOwnershipImage.repository=filelog-offset-volume-ownership \
  --set operator.filelogOffsetVolumeOwnershipImage.tag=latest \
  --set operator.filelogOffsetVolumeOwnershipImage.pullPolicy=IfNotPresent \
  --set operator.targetAllocatorImage.repository=target-allocator \
  --set operator.targetAllocatorImage.tag=latest \
  --set operator.targetAllocatorImage.pullPolicy=IfNotPresent \
  --set operator.agent0ConnectorImage.repository=agent0-connector \
  --set operator.agent0ConnectorImage.tag=latest \
  --set operator.agent0ConnectorImage.pullPolicy=IfNotPresent \
  --set operator.developmentMode=true \
  --set operator.instrumentation.enableRubyAutoInstrumentation=true \
  --set operator.dash0Export.enabled=true \
  --set "operator.dash0Export.endpoint=${DASH0_INGRESS_ENDPOINT}" \
  --set "operator.dash0Export.token=${DASH0_AUTHORIZATION_TOKEN}" \
  --set "operator.dash0Export.apiEndpoint=${DASH0_API_ENDPOINT}" \
  --set "operator.dash0Export.dataset=${DASH0_DATASET}" \
  --set "operator.clusterName=${KIND_CLUSTER_NAME}" \
  dash0-operator \
  helm-chart/dash0-operator
echo

###############################################################################
# Step 5: Create target namespace and deploy Dash0Monitoring resource
###############################################################################

echo "STEP: Creating target namespace '$TARGET_NAMESPACE'..."
kubectl create namespace "$TARGET_NAMESPACE" --dry-run=client -o yaml | kubectl apply -f -
echo

echo "STEP: Deploying Dash0Monitoring resource..."
kubectl apply -n "$TARGET_NAMESPACE" -f - <<EOF
apiVersion: operator.dash0.com/v1beta1
kind: Dash0Monitoring
metadata:
  name: dash0-monitoring-resource
spec:
  instrumentWorkloads:
    mode: all
  logCollection:
    enabled: true
EOF

echo "  Waiting for Dash0Monitoring to become available..."
kubectl wait \
  --namespace "$TARGET_NAMESPACE" \
  dash0monitorings.operator.dash0.com/dash0-monitoring-resource \
  --for condition=Available \
  --timeout=120s
echo

###############################################################################
# Step 6: Deploy the 5 Ruby demo applications
###############################################################################

echo "STEP: Deploying Ruby demo applications..."

echo "  1/5 Rails (Rack + ActionController + ActiveSupport)..."
helm upgrade --install \
  --namespace "$TARGET_NAMESPACE" \
  --wait \
  ruby-rails-demo \
  test-resources/ruby/rails/helm-chart-demo

echo "  2/5 Sinatra (Rack DSL framework)..."
helm upgrade --install \
  --namespace "$TARGET_NAMESPACE" \
  --wait \
  ruby-sinatra-demo \
  test-resources/ruby/sinatra/helm-chart

echo "  3/5 Rack (pure Rack, no framework)..."
helm upgrade --install \
  --namespace "$TARGET_NAMESPACE" \
  --wait \
  ruby-rack-demo \
  test-resources/ruby/rack/helm-chart

echo "  4/5 Rails + ActiveRecord + SQLite (database query spans)..."
helm upgrade --install \
  --namespace "$TARGET_NAMESPACE" \
  --wait \
  ruby-activerecord-demo \
  test-resources/ruby/activerecord/helm-chart

echo "  5/5 Rails HTTP client → Rails (distributed tracing)..."
helm upgrade --install \
  --namespace "$TARGET_NAMESPACE" \
  --wait \
  ruby-http-client-demo \
  test-resources/ruby/http-client/helm-chart

echo

###############################################################################
# Done
###############################################################################

echo "================================================================"
echo " Deployment complete!"
echo "================================================================"
echo
echo " All 5 Ruby demo apps are running in namespace: $TARGET_NAMESPACE"
echo " Each app has a traffic-generator sidecar sending requests every 3s."
echo " Traces and spans are being reported to Dash0 (dataset: $DASH0_DATASET)."
echo
echo " Deployments:"
kubectl get deployments -n "$TARGET_NAMESPACE"
echo
echo " To tear down everything:"
echo "   helm uninstall -n $TARGET_NAMESPACE ruby-rails-demo ruby-sinatra-demo ruby-rack-demo ruby-activerecord-demo ruby-http-client-demo"
echo "   helm uninstall -n $OPERATOR_NAMESPACE dash0-operator"
echo "   kubectl delete ns $TARGET_NAMESPACE $OPERATOR_NAMESPACE"
echo "   kind delete cluster --name $KIND_CLUSTER_NAME"
echo
