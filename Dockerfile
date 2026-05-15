# syntax=docker/dockerfile:1.7

FROM golang:1.22-alpine AS build
WORKDIR /src
COPY go.mod ./
RUN go mod download
COPY . .
ARG VERSION=dev
RUN CGO_ENABLED=0 go build \
    -trimpath \
    -ldflags "-s -w -X github.com/neverbot/owl/internal/version.Version=${VERSION}" \
    -o /out/owl ./cmd/owl

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /out/owl /usr/local/bin/owl
USER nonroot:nonroot
ENTRYPOINT ["/usr/local/bin/owl"]
