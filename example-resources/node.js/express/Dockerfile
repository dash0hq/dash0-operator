ARG NODE_VERSION=20.13.1
ARG ALPINE_VERSION=3.19

FROM node:${NODE_VERSION}-alpine${ALPINE_VERSION}
ENV NODE_ENV production
WORKDIR /usr/src/app
COPY --chown=node:node package.json .
COPY --chown=node:node package-lock.json .
RUN npm ci --omit=dev
COPY --chown=node:node . .
EXPOSE 1207
CMD [ "node", "app.js" ]
