# syntax=docker/dockerfile:1
# Multi-stage build: compiles the exporter inside the image, so `docker build`
# works on a fresh clone without a pre-built binary on the host.

FROM golang:1.27-alpine AS build
ARG VERSION=dev
WORKDIR /src
COPY go.mod ./
COPY *.go ./
RUN CGO_ENABLED=0 go build -trimpath \
    -ldflags="-s -w -X main.version=${VERSION}" \
    -o /out/singbox-exporter .

FROM alpine:3.22
COPY --from=build /out/singbox-exporter /usr/local/bin/singbox-exporter
USER nobody:nogroup
EXPOSE 9098
HEALTHCHECK --interval=15s --timeout=5s --start-period=5s --retries=3 \
  CMD wget -q -O /dev/null http://127.0.0.1:9098/healthz || exit 1
ENTRYPOINT ["/usr/local/bin/singbox-exporter"]