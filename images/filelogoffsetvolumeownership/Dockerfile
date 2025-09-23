# SPDX-FileCopyrightText: Copyright 2025 Dash0 Inc.
# SPDX-License-Identifier: Apache-2.0

# Note: This basically just re-tags the busybox. We do this for consistency with other images. That is, all images used
# by the collector pods are built in the same way, and stored in the same container registry. In particular, we avoid
# using images from Dockerhub directly because of its pull rate limits.

# Note: This container needs to run as root, since its purpose is to change file ownership settings in a mounted host
# path volume; the path inside the volume is owned by root by default.

FROM busybox:1.37.0-glibc
