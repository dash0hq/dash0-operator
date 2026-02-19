# SPDX-FileCopyrightText: Copyright 2025 Dash0 Inc.
# SPDX-License-Identifier: Apache-2.0

# Note: This Dockerfile expects dash0-operator/images as the build context.

# Note: This basically just re-tags the original target-allocator image.
# We do this for consistency with other images. That is, all images used by the collector pods are built in the same
# way, and stored in the same container registry. By retagging and unifying the versions/tags to the chart version,
# it is also clear which tags are meant to be used together.

# Retagging the image this way also enables automatic dependency updates via dependabot.

FROM ghcr.io/open-telemetry/opentelemetry-operator/target-allocator:0.145.0
