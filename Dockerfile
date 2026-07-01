# syntax=docker/dockerfile:1

FROM --platform=$BUILDPLATFORM golang:1.25-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
ARG TARGETOS TARGETARCH
RUN CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH \
    go build -trimpath -ldflags="-s -w" -o /tinfoil-proxy .

FROM alpine:latest
RUN apk add --no-cache ca-certificates
COPY --from=build /tinfoil-proxy /usr/bin/tinfoil-proxy
EXPOSE 3301
ENTRYPOINT ["/usr/bin/tinfoil-proxy"]
# Bind to all interfaces by default so the proxy is reachable from a published
# port. Keep it loopback-only on the host with `-p 127.0.0.1:3301:3301`.
CMD ["--bind", "0.0.0.0", "--allowed-host", "tinfoil"]
