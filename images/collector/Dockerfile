FROM golang:1.23-alpine AS builder

RUN apk add --update git make yq && apk cache clean

COPY ./src /src
WORKDIR /src

RUN export VERSION=$(yq '.dist.otelcol_version' builder/config.yaml) \
 && echo "Using otelcol v${VERSION} as baseline" \
 && go install go.opentelemetry.io/collector/cmd/builder@v${VERSION}

RUN CGO_ENABLED=0 builder --config=builder/config.yaml \
 && chmod u+x /src/dist/dash0-operator-collector

FROM alpine:3.20.2
COPY ./src/image/entrypoint.sh /entrypoint.sh
COPY --from=builder /src/dist/dash0-operator-collector ./otelcol
USER 65532:65532
ENTRYPOINT ["/entrypoint.sh"]
