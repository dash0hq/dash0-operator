FROM otel/opentelemetry-collector-k8s:0.105.0 AS builder

FROM alpine:3.20.2
ADD ./src/entrypoint.sh /entrypoint.sh
COPY --from=builder ./otelcol-k8s ./otelcol-k8s
ENTRYPOINT ["/entrypoint.sh"]