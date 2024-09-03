# Note: Expects dash0-operator/images as the build context.
FROM golang:alpine AS builder
COPY filelogoffsetsynch/src /usr/local/go/src/filelogoffsetsynch
COPY pkg /usr/local/go/pkg
WORKDIR /usr/local/go/src/filelogoffsetsynch
RUN CGO_ENABLED=0 go build -ldflags '-extldflags "-static"' filelogoffsetsynch

FROM alpine:3.20.2 AS certs
RUN apk --update add ca-certificates && apk cache clean

FROM scratch
COPY --from=builder /usr/local/go/src/filelogoffsetsynch/filelogoffsetsynch /app/filelogoffsetsynch
COPY --from=certs /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/ca-certificates.crt
USER 65532:65532
ENTRYPOINT [ "/app/filelogoffsetsynch" ]