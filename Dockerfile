# syntax=docker/dockerfile:1

# Builder and runtime images are digest-pinned; refresh both digests
# together on any toolchain or base-image bump.
FROM --platform=$BUILDPLATFORM golang:1.27.4-alpine@sha256:70bd7ddea8b6cbe5d6943cde279d6b67feb10cc924fb9100d1d8757c1575b280 AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
ARG TARGETOS TARGETARCH VERSION=unknown
RUN CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH \
    go build -trimpath -ldflags="-s -w -X main.buildVersion=$VERSION" -o /tinfoil-proxy .

# The apk version constraint is the check: an unsatisfiable constraint fails
# the build, so a stale trust store cannot ship silently.
FROM alpine:3.24@sha256:294b683cb724975bec92580e1e685676bd4b50bda910ddb8c51d4cabeaec77e6
RUN apk add --no-cache 'ca-certificates>=20261020-r0'
COPY --from=build /tinfoil-proxy /usr/bin/tinfoil-proxy
EXPOSE 3301
ENTRYPOINT ["/usr/bin/tinfoil-proxy"]
# Bind to all interfaces by default so the proxy is reachable from a published
# port. Keep it loopback-only on the host with `-p 127.0.0.1:3301:3301`.
CMD ["--bind", "0.0.0.0", "--allowed-host", "tinfoil"]
