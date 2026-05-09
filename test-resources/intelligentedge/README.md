# Intelligent Edge Development Notes

## E2E tests

E2E tests for intelligent edge are disabled by default, so the tests can be run without requiring access to the private images.

To run the E2E tests with the tests related to intelligent edge, set `E2E_ENABLE_INTELLIGENT_EDGE_TESTS=true`.

## Temporary helper script to build and push IE images

0. clone the dash0 mono repo
0. run `DASH0_REPO_ROOT=/path/to/cloned/repo ./build-ie-and-barker.sh --push`

This will build the IE collector and barker images and push them to

```
$IMAGE_REPOSITORY_PREFIX/intelligent-edge-collector:latest
$IMAGE_REPOSITORY_PREFIX/barker:latest
```

## Using the images from the private GHCR

The images are also available in `ghcr.io/dash0hq/intelligent-edge-collector` and `ghcr.io/dash0hq/barker`. Pulling them requires authorization.

For `kind`, pick your favorite method from https://kind.sigs.k8s.io/docs/user/private-registries/ or
check the [e2e workflow](/.github/workflows/e2e.yaml).