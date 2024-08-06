FROM golang:alpine AS builder
ADD ./src /usr/local/go/src/configreloader
RUN CGO_ENABLED=0 go build -ldflags '-extldflags "-static"' configreloader

FROM scratch
COPY --from=builder /go/configreloader /app/configreloader
ENTRYPOINT [ "/app/configreloader" ]