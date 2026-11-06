# SPDX-FileCopyrightText: Copyright 2026 Dash0 Inc.
# SPDX-License-Identifier: Apache-2.0

ARG DENO_VERSION=2.9.8

FROM denoland/deno:debian-${DENO_VERSION}
WORKDIR /usr/src/app
COPY app.ts .
USER deno
ENTRYPOINT [ "deno", "run", "--allow-net", "--allow-env", "app.ts" ]
