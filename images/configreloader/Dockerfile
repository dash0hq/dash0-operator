FROM golang:alpine AS builder
COPY ./src /usr/local/go/src/configreloader
WORKDIR /usr/local/go/src/configreloader
RUN CGO_ENABLED=0 go build -ldflags '-extldflags "-static"' configreloader

FROM scratch
COPY --from=builder /usr/local/go/src/configreloader/configreloader /app/configreloader
USER 65532:65532
ENTRYPOINT [ "/app/configreloader" ]