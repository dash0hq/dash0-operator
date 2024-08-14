FROM golang:alpine AS builder
COPY ./src /usr/local/go/src/filelogoffsetsynch
RUN CGO_ENABLED=0 go build -ldflags '-extldflags "-static"' filelogoffsetsynch

FROM scratch
COPY --from=builder /go/filelogoffsetsynch /app/filelogoffsetsynch
USER 65532:65532
ENTRYPOINT [ "/app/filelogoffsetsynch" ]