#!/usr/bin/env bash
# Build operator-intelligent-edge-collector and barker Docker images from the dash0 monorepo.
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

PUSH=false
for arg in "$@"; do
  case "$arg" in
    --push) PUSH=true ;;
    *) echo "Unknown flag: $arg" >&2; exit 1 ;;
  esac
done

if [[ -z "${DASH0_REPO_ROOT:-}" ]]; then
  echo "Error: DASH0_REPO_ROOT must be set to the dash0 monorepo path." >&2
  exit 1
fi
COLLECTOR_DIR="$DASH0_REPO_ROOT/components/collector"
BARKER_DIR="$DASH0_REPO_ROOT/components/barker"
if [[ -z "${ARCH:-}" ]]; then
  case "$(uname -m)" in
    x86_64|amd64) ARCH=amd64 ;;
    arm64|aarch64) ARCH=arm64 ;;
    *) echo "Error: unsupported host architecture '$(uname -m)'. Set ARCH=amd64 or ARCH=arm64." >&2; exit 1 ;;
  esac
fi
REVISION="$(git -C "$DASH0_REPO_ROOT" rev-list -1 HEAD)"
VERSION="${VERSION:-0.0.1}"

LDFLAGS_IEC="-X 'github.com/dash0hq/dash0-opentelemetry-collector/internal/common/version.Version=${VERSION}' -X 'github.com/dash0hq/dash0-opentelemetry-collector/internal/common/version.GitCommit=${REVISION}'"
LDFLAGS_BARKER="-X 'github.com/dash0hq/barker/internal/utils/version.Version=${VERSION}' -X 'github.com/dash0hq/barker/internal/utils/version.GitCommit=${REVISION}'"

# Generate a builder config with path: entries resolved to the monorepo.
# The path: entries are relative to the config file, so they need rewriting.
# The replaces entries are relative to output_path and resolve correctly as-is.
BUILDER_CONFIG="$SCRIPT_DIR/operator-intelligent-edge-collector-builder-config.yaml"
RESOLVED_CONFIG="$COLLECTOR_DIR/operator-intelligent-edge-collector-builder-config.resolved.yaml"
sed -e "s|path: \./|path: ${COLLECTOR_DIR}/|g" "$BUILDER_CONFIG" > "$RESOLVED_CONFIG"

echo "==> Building operator-intelligent-edge-collector binary"
cd "$COLLECTOR_DIR"
go tool go.opentelemetry.io/collector/cmd/builder \
  --skip-compilation \
  --config "$RESOLVED_CONFIG" \
  --output-path dist_operator_intelligent_edge_collector
rm -f "$RESOLVED_CONFIG"
cd dist_operator_intelligent_edge_collector
GOWORK=off CGO_ENABLED=0 GOOS=linux GOARCH="${ARCH}" GOEXPERIMENT=newinliner go build -trimpath \
  -o "../bin/operator-intelligent-edge-collector_linux_${ARCH}" \
  -ldflags="$LDFLAGS_IEC" .

echo "==> Building barker binary"
cd "$BARKER_DIR"
CGO_ENABLED=0 GOOS=linux GOARCH="${ARCH}" go build \
  -ldflags="$LDFLAGS_BARKER" \
  -o "bin/dash0-barker_linux_${ARCH}"

echo "==> Building operator-intelligent-edge-collector Docker image"
cd "$COLLECTOR_DIR"
cp -f "bin/operator-intelligent-edge-collector_linux_${ARCH}" "operator-intelligent-edge-collector_${ARCH}"
cp -f "$SCRIPT_DIR/entrypoint-operator-intelligent-edge.sh" entrypoint-operator-intelligent-edge.sh
docker build --platform "linux/${ARCH}" --build-arg "TARGETARCH=${ARCH}" -t intelligent-edge-collector:latest \
  -f "$SCRIPT_DIR/Dockerfile.operator-intelligent-edge" .
rm -f "operator-intelligent-edge-collector_${ARCH}" entrypoint-operator-intelligent-edge.sh

echo "==> Building barker Docker image"
cd "$BARKER_DIR"
cp -f "bin/dash0-barker_linux_${ARCH}" "dash0-barker_${ARCH}"
docker build --platform "linux/${ARCH}" --build-arg "TARGETARCH=${ARCH}" -t barker:latest .
rm -f "dash0-barker_${ARCH}"

if [[ -n "${IMAGE_REPOSITORY_PREFIX:-}" ]]; then
  PREFIX="${IMAGE_REPOSITORY_PREFIX%/}"
  echo "==> Tagging images with prefix: $PREFIX"
  docker tag intelligent-edge-collector:latest "${PREFIX}/intelligent-edge-collector:latest"
  docker tag barker:latest "${PREFIX}/barker:latest"
fi

if [[ "$PUSH" == true ]]; then
  if [[ -z "${PREFIX:-}" ]]; then
    echo "Error: --push requires IMAGE_REPOSITORY_PREFIX to be set." >&2
    exit 1
  fi
  echo "==> Pushing images"
  docker push "${PREFIX}/intelligent-edge-collector:latest"
  docker push "${PREFIX}/barker:latest"
fi

echo "==> Done. Images:"
docker image inspect --format '{{.ID}}  {{.Size}}' intelligent-edge-collector:latest | xargs -I{} echo "  intelligent-edge-collector:latest  {}"
docker image inspect --format '{{.ID}}  {{.Size}}' barker:latest | xargs -I{} echo "  barker:latest  {}"
if [[ -n "${PREFIX:-}" ]]; then
  echo "  ${PREFIX}/intelligent-edge-collector:latest"
  echo "  ${PREFIX}/barker:latest"
fi
