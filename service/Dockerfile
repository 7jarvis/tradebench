# syntax=docker/dockerfile:1.7

# ---- build ----
FROM golang:1.24-alpine AS build
WORKDIR /src

# Dependencies first: this layer is cached until go.mod/go.sum change.
COPY go.mod go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod go mod download

COPY . .
# Static binary, no cgo, stripped, reproducible paths.
RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/broker ./cmd/broker

# ---- runtime ----
# distroless/static: no shell, no package manager, CA certs + tzdata only.
# The :nonroot tag runs as uid 65532.
FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /out/broker /broker
USER 65532:65532
EXPOSE 8080
# No curl in distroless: the binary checks its own readiness.
HEALTHCHECK --interval=5s --timeout=3s --start-period=10s --retries=10 CMD ["/broker", "healthcheck"]
ENTRYPOINT ["/broker"]
