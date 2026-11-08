# syntax=docker/dockerfile:1
#
# Three-stage build following the OCB guide:
# https://opentelemetry.io/docs/collector/extend/ocb/
#
# Build context is the repo root:
#   docker build -f otelcollector/Dockerfile -t otelcol:latest .
#
# Stage 1 — certs   : pull CA bundle from Alpine
# Stage 2 — builder : install OCB, generate sources, compile
# Stage 3 — runtime : distroless minimal image

ARG BINARY_NAME=otelcol

# ── Stage 1: CA certificates ────────────────────────────────────────────────
FROM alpine:3.24 AS certs
RUN apk --no-cache add ca-certificates

# ── Stage 2: Build ──────────────────────────────────────────────────────────
FROM golang:1.27 AS builder
ARG BINARY_NAME

WORKDIR /build/otelcollector

# Install OCB — cached across rebuilds via BuildKit mount
RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    go install go.opentelemetry.io/collector/cmd/builder@v0.164.0

# Copy the manifest — OCB generates main.go / components.go / go.mod from it
COPY otelcollector/manifest.yaml .
# Custom components (custom-components/ at the repo root), referenced from manifest.yaml `replaces`
COPY custom-components /build/custom-components

# Generate sources, then tidy and compile separately for a static binary.
RUN --mount=type=cache,target=/go/pkg/mod \
    builder --config manifest.yaml --skip-compilation

# Resolve dependency graph then compile a static Linux binary
RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    go mod tidy && \
    CGO_ENABLED=0 GOOS=linux \
    go build -trimpath -ldflags="-s -w" -o "${BINARY_NAME}" .

# ── Stage 3: Runtime ────────────────────────────────────────────────────────
FROM gcr.io/distroless/base-debian13:nonroot
ARG BINARY_NAME

COPY --from=certs /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/ca-certificates.crt
COPY --from=builder "/build/otelcollector/${BINARY_NAME}" /otelcollector
COPY otelcollector/collector-config.yaml /etc/otelcol/config.yaml

EXPOSE 4317 4318 13133

ENTRYPOINT ["/otelcollector"]
CMD ["--config", "/etc/otelcol/config.yaml"]
