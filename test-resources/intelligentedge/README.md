# Temporary helper script to build and push IE images

0. clone the dash0 mono repo
0. run `DASH0_REPO_ROOT=/path/to/cloned/repo ./build-ie-and-barker.sh --push`

This will build the IE collector and barker images and push them to

```
$IMAGE_REPOSITORY_PREFIX/intelligent-edge-collector:latest
$IMAGE_REPOSITORY_PREFIX/barker:latest
```