# syntax=docker/dockerfile:1.7

ARG BUILDPLATFORM=linux/amd64
FROM --platform=$BUILDPLATFORM golang:1.26.4-alpine3.22@sha256:727cfc3c40be55cd1bc9a4a059406b28a059857e3be752aa9d09531e12c20c56 AS build

ARG TARGETOS=linux
ARG TARGETARCH=amd64
ARG VERSION=dev
ARG COMMIT=unknown

WORKDIR /src
COPY go.mod ./
COPY cmd ./cmd
COPY internal ./internal

RUN CGO_ENABLED=0 GOOS="$TARGETOS" GOARCH="$TARGETARCH" \
    go build -trimpath \
    -ldflags="-s -w -X main.version=$VERSION -X main.commit=$COMMIT" \
    -o /out/keitaro-worker ./cmd/keitaro-worker

FROM scratch

ARG VERSION=dev
ARG COMMIT=unknown

LABEL org.opencontainers.image.title="LazyArb Keitaro Worker" \
      org.opencontainers.image.description="Durable local postback delivery for Keitaro" \
      org.opencontainers.image.source="https://github.com/lazyarb/keitaro-worker" \
      org.opencontainers.image.licenses="MIT" \
      org.opencontainers.image.version="$VERSION" \
      org.opencontainers.image.revision="$COMMIT"

COPY --from=build /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/ca-certificates.crt
COPY --from=build /out/keitaro-worker /usr/local/bin/keitaro-worker

USER 65532:65532
ENTRYPOINT ["/usr/local/bin/keitaro-worker"]
HEALTHCHECK --interval=10s --timeout=3s --start-period=10s --retries=3 CMD ["/usr/local/bin/keitaro-worker", "healthcheck"]
