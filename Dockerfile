# syntax=docker/dockerfile:1.7

FROM golang:1.25-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
ARG VERSION=dev
RUN CGO_ENABLED=0 go build \
    -trimpath \
    -ldflags "-s -w -X github.com/neverbot/owl/internal/version.Version=${VERSION}" \
    -o /out/owl ./cmd/owl
# Pre-create the data directory so a `nonroot`-owned mount point exists
# in the final image. When a Docker volume is first mounted onto this
# path it inherits the directory's permissions, which lets the binary
# (running as UID 65532) open the SQLite file inside.
RUN mkdir -p /out/data

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /out/owl /usr/local/bin/owl
COPY --from=build --chown=nonroot:nonroot /out/data /data
USER nonroot:nonroot
ENTRYPOINT ["/usr/local/bin/owl"]
