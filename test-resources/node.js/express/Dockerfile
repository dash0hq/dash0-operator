# SPDX-FileCopyrightText: Copyright 2024 Dash0 Inc.
# SPDX-License-Identifier: Apache-2.0

ARG NODE_VERSION=20.19.0
ARG ALPINE_VERSION=3.21

FROM node:${NODE_VERSION}-alpine${ALPINE_VERSION}
ENV NODE_ENV=production
WORKDIR /usr/src/app
COPY --chown=node:node package.json .
COPY --chown=node:node package-lock.json .
RUN npm ci --omit=dev
COPY --chown=node:node . .
ENTRYPOINT [ "node", "app.js" ]
