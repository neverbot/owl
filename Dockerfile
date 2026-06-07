# syntax=docker/dockerfile:1.7

# Build natively on the runner architecture (BUILDPLATFORM), then
# cross-compile the Go binary to the requested TARGETPLATFORM. Buildx
# sets TARGETOS / TARGETARCH per platform when the workflow lists
# multiple `--platform` values, so a single build job produces both
# amd64 and arm64 binaries without ever invoking QEMU. owl is pure
# Go and CGO-free (modernc.org/sqlite is load-bearing for this
# property), so cross-compilation is trivial.
FROM --platform=$BUILDPLATFORM golang:1.25-alpine AS build
ARG TARGETOS TARGETARCH VERSION=dev
WORKDIR /src

# Module download is split from the source copy so a code-only change
# doesn't invalidate the dependency layer. The cache mount on
# /go/pkg/mod preserves the resolved module tree across builds even
# when go.sum changes — modules already on disk are reused, only the
# new ones are fetched.
COPY go.mod go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod \
    go mod download

COPY . .

# Cache mounts on the Go build cache and module cache let incremental
# builds reuse per-package object files across runs. With Buildx
# `cache-to: type=gha,mode=max` in the workflow, these caches persist
# between CI runs as long as the cache key is valid.
RUN --mount=type=cache,target=/root/.cache/go-build \
    --mount=type=cache,target=/go/pkg/mod \
    CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH \
    go build \
    -trimpath \
    -ldflags "-s -w -X github.com/neverbot/owl/internal/version.Version=${VERSION}" \
    -o /out/owl ./cmd/owl

# Pre-create the data directory so a `nonroot`-owned mount point exists
# in the final image. When a Docker volume is first mounted onto this
# path it inherits the directory's permissions, which lets the binary
# (running as UID 65532) open the SQLite file inside.
RUN mkdir -p /out/data

FROM gcr.io/distroless/static-debian12:nonroot
LABEL org.opencontainers.image.title="owl" \
      org.opencontainers.image.description="Tiny, lightweight self-hosted observability" \
      org.opencontainers.image.source="https://github.com/neverbot/owl" \
      org.opencontainers.image.url="https://github.com/neverbot/owl" \
      org.opencontainers.image.licenses="MIT"
COPY --from=build /out/owl /usr/local/bin/owl
COPY --from=build --chown=nonroot:nonroot /out/data /data
USER nonroot:nonroot
ENTRYPOINT ["/usr/local/bin/owl"]
