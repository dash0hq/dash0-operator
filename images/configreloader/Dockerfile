# Note: Expects dash0-operator/images as the build context.
FROM golang:alpine AS builder
COPY configreloader/src /usr/local/go/src/configreloader
COPY pkg /usr/local/go/pkg
WORKDIR /usr/local/go/src/configreloader
RUN CGO_ENABLED=0 go build -ldflags '-extldflags "-static"' configreloader

FROM alpine:3.20.2 AS certs
RUN apk --update add ca-certificates && apk cache clean

FROM scratch
COPY --from=builder /usr/local/go/src/configreloader/configreloader /app/configreloader
COPY --from=certs /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/ca-certificates.crt
USER 65532:65532
ENTRYPOINT [ "/app/configreloader" ]