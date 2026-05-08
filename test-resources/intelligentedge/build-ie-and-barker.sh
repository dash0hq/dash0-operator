#!/usr/bin/env bash
# Build operator-intelligent-edge-collector and barker Docker images from the dash0 monorepo.
set -euo pipefail

PUSH=false
PUSH_ONLY=false
for arg in "$@"; do
  case "$arg" in
    --push) PUSH=true ;;
    --push-only) PUSH_ONLY=true ;;
    *) echo "Unknown flag: $arg" >&2; exit 1 ;;
  esac
done

if [[ "$PUSH" == true && "$PUSH_ONLY" == true ]]; then
  echo "Error: --push and --push-only are mutually exclusive." >&2
  exit 1
fi

for var in INTELLIGENT_EDGE_COLLECTOR_IMAGE_REPOSITORY INTELLIGENT_EDGE_COLLECTOR_IMAGE_TAG BARKER_IMAGE_REPOSITORY BARKER_IMAGE_TAG; do
  if [[ -z "${!var:-}" ]]; then
    echo "Error: $var must be set." >&2
    exit 1
  fi
done

INTELLIGENT_EDGE_COLLECTOR_IMAGE="${INTELLIGENT_EDGE_COLLECTOR_IMAGE_REPOSITORY}:${INTELLIGENT_EDGE_COLLECTOR_IMAGE_TAG}"
BARKER_IMAGE="${BARKER_IMAGE_REPOSITORY}:${BARKER_IMAGE_TAG}"

if [[ "$PUSH_ONLY" == true ]]; then
  echo "==> Pushing images"
  echo "docker push $INTELLIGENT_EDGE_COLLECTOR_IMAGE"
  docker push "$INTELLIGENT_EDGE_COLLECTOR_IMAGE"
  echo "docker push $BARKER_IMAGE"
  docker push "$BARKER_IMAGE"
  exit 0
fi

if [[ -z "${DASH0_REPO_ROOT:-}" ]]; then
  echo "Error: DASH0_REPO_ROOT must be set to the dash0 monorepo path." >&2
  exit 1
fi
COLLECTOR_DIR="$DASH0_REPO_ROOT/components/collector"
COLLECTOR_OPERATOR_DIR="$COLLECTOR_DIR/operator"
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

echo "==> Building operator-intelligent-edge-collector binary"
cd "$COLLECTOR_DIR"
go tool go.opentelemetry.io/collector/cmd/builder \
  --skip-compilation \
  --config operator/operator-intelligent-edge-collector-builder-config.yaml \
  --output-path dist_operator_intelligent_edge_collector
cd dist_operator_intelligent_edge_collector
GOWORK=off CGO_ENABLED=0 GOOS=linux GOARCH="${ARCH}" GOEXPERIMENT=newinliner go build -trimpath \
  -o "../bin/operator-intelligent-edge-collector_linux_${ARCH}" \
  -ldflags="$LDFLAGS_IEC" \
  -tags "dash0_external_collector_exclude" .

echo "==> Building barker binary"
cd "$BARKER_DIR"
CGO_ENABLED=0 GOOS=linux GOARCH="${ARCH}" go build \
  -ldflags="$LDFLAGS_BARKER" \
  -o "bin/dash0-barker_linux_${ARCH}"

echo "==> Building operator-intelligent-edge-collector Docker image: $INTELLIGENT_EDGE_COLLECTOR_IMAGE"
cd "$COLLECTOR_DIR"
cp -f "bin/operator-intelligent-edge-collector_linux_${ARCH}" "operator/operator-intelligent-edge-collector_${ARCH}"
docker build --platform "linux/${ARCH}" --build-arg "TARGETARCH=${ARCH}" -t "$INTELLIGENT_EDGE_COLLECTOR_IMAGE" \
  -f "$COLLECTOR_OPERATOR_DIR/Dockerfile.operator-intelligent-edge" "$COLLECTOR_OPERATOR_DIR"
rm -f "operator/operator-intelligent-edge-collector_${ARCH}"

echo "==> Building barker Docker image: $BARKER_IMAGE"
cd "$BARKER_DIR"
cp -f "bin/dash0-barker_linux_${ARCH}" "dash0-barker_${ARCH}"
docker build --platform "linux/${ARCH}" --build-arg "TARGETARCH=${ARCH}" -t "$BARKER_IMAGE" .
rm -f "dash0-barker_${ARCH}"

if [[ "$PUSH" == true ]]; then
  echo "==> Pushing images"
  echo "docker push $INTELLIGENT_EDGE_COLLECTOR_IMAGE"
  docker push "$INTELLIGENT_EDGE_COLLECTOR_IMAGE"
  echo "docker push $BARKER_IMAGE"
  docker push "$BARKER_IMAGE"
fi

echo "==> Done. Images:"
docker image inspect --format '{{.ID}}  {{.Size}}' "$INTELLIGENT_EDGE_COLLECTOR_IMAGE" | xargs -I{} echo "  $INTELLIGENT_EDGE_COLLECTOR_IMAGE  {}"
docker image inspect --format '{{.ID}}  {{.Size}}' "$BARKER_IMAGE" | xargs -I{} echo "  $BARKER_IMAGE  {}"
