# Setup Kind Cluster for Local Integration Tests

Set up a kind cluster for local integration testing of the dash0-operator. This includes:

1. Creating a kind cluster (named `dash0-operator-test` unless a different name is specified as $ARGUMENTS)
2. Building all operator container images using `docker buildx build --load`
3. Loading all images into the kind cluster via `kind load docker-image`

## Pre-flight checks

Before starting, verify the container runtime is healthy:

```
docker info 2>&1 | grep "Server Version"
```

If `docker system prune` or `docker image ls` produces "layer not known" errors, the storage is corrupted.
Fix with:
```
# If using Podman (check: docker info shows crun runtime and /var/lib/containers/storage):
podman machine ssh -- podman system check --repair
podman machine ssh -- podman system reset --force

# If using Docker Desktop:
# Restart Docker Desktop via the UI or: osascript -e 'quit app "Docker Desktop"' && open -a "Docker Desktop"
```

Check disk space inside the container runtime VM:
```
# Podman:
podman machine ssh -- df -h /var/lib/containers
# Need at least 10GB free for all images
```

## Steps

### 1. Create kind cluster

Create a minimal kind cluster (single control-plane node, no extra mounts):

```
kind create cluster --name <cluster-name> --wait 60s
```

If the cluster already exists (`kind get clusters` shows it and `kubectl get nodes` shows Ready), skip creation.

**Important:** If you ran `docker system prune -af` or reset container storage, the kind cluster nodes will be destroyed even though `kind get clusters` might still list them. In that case, delete and recreate:
```
kind delete cluster --name <cluster-name>
kind create cluster --name <cluster-name> --wait 60s
```

### 2. Build all operator images

Build all 7 operator images using `docker buildx build --load` with the default image names (no prefix). The images and their build contexts are:

| Image Name | Dockerfile | Build Context |
|---|---|---|
| `operator-controller:latest` | `Dockerfile` | `.` (repo root) |
| `instrumentation:latest` | `images/instrumentation/Dockerfile` | `images/instrumentation` |
| `collector:latest` | `images/collector/Dockerfile` | `images/collector` |
| `configuration-reloader:latest` | `images/configreloader/Dockerfile` | `images` |
| `filelog-offset-sync:latest` | `images/filelogoffsetsync/Dockerfile` | `images` |
| `filelog-offset-volume-ownership:latest` | `images/filelogoffsetvolumeownership/Dockerfile` | `images` |
| `target-allocator:latest` | `images/target-allocator/Dockerfile` | `images/target-allocator` |

**Build order matters:** The collector image is the largest and most resource-intensive. Build it alone to avoid disk space pressure. Build the other 6 images in parallel.

Recommended strategy:
1. Build `operator-controller` and `collector` sequentially first (they are the largest).
2. Build the remaining 5 images in parallel.

If a build fails:
- **Network errors** (`unexpected EOF` from Go proxy): Retry up to 2 times.
- **"no space left on device"**: Run `docker buildx prune -af` to free buildx cache, then retry.
- **"layer not known"**: Storage is corrupted, see Pre-flight checks above.

**Do NOT use `make images`** — it uses `$(CONTAINER_TOOL) build` which may not use buildx and won't add `--load` for Podman/buildx setups.

### 3. Load images into kind

Load all built images into the kind cluster:

```
kind load docker-image <image>:latest --name <cluster-name>
```

Load all images in parallel.

### 4. Verify

Run `kubectl get nodes` to confirm the cluster is ready, and `docker image ls | grep -E "operator-controller|instrumentation|collector|configuration-reloader|filelog-offset|target-allocator"` to confirm images were built.

## Deploying the operator

After this skill completes, deploy the operator with:
```
# First, render templates (needed for Helm chart):
test-resources/bin/render-templates.sh

helm upgrade --install --namespace dash0-system --create-namespace \
  --set operator.image.repository=operator-controller --set operator.image.tag=latest --set operator.image.pullPolicy=Never \
  --set operator.initContainerImage.repository=instrumentation --set operator.initContainerImage.tag=latest --set operator.initContainerImage.pullPolicy=Never \
  --set operator.collectorImage.repository=collector --set operator.collectorImage.tag=latest --set operator.collectorImage.pullPolicy=Never \
  --set operator.configurationReloaderImage.repository=configuration-reloader --set operator.configurationReloaderImage.tag=latest --set operator.configurationReloaderImage.pullPolicy=Never \
  --set operator.filelogOffsetSyncImage.repository=filelog-offset-sync --set operator.filelogOffsetSyncImage.tag=latest --set operator.filelogOffsetSyncImage.pullPolicy=Never \
  --set operator.filelogOffsetVolumeOwnershipImage.repository=filelog-offset-volume-ownership --set operator.filelogOffsetVolumeOwnershipImage.tag=latest --set operator.filelogOffsetVolumeOwnershipImage.pullPolicy=Never \
  --set operator.targetAllocatorImage.repository=target-allocator --set operator.targetAllocatorImage.tag=latest --set operator.targetAllocatorImage.pullPolicy=Never \
  --set operator.developmentMode=true \
  dash0-operator helm-chart/dash0-operator
```

## Container runtime notes

- Always use `docker buildx build --load` (not plain `docker build`) to ensure images are loaded into the local image store.
