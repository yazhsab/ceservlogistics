# Production image for the Courier OS API, worker and migrator.
#
# One image carries all three binaries: they share the same module graph, and a
# single artifact means the API and the worker in a deployment are provably the
# same build. The entrypoint selects which binary runs.

# ---------------------------------------------------------------------------
# Build
# ---------------------------------------------------------------------------
# Pinned to a patched patch-release, not the 1.25 floating tag. govulncheck
# found 22 reachable standard-library vulnerabilities on 1.25.4; every one of
# them is fixed by the toolchain rather than by any change here, so the version
# is a security control and is pinned like one.
FROM golang:1.25.12-alpine AS build

RUN apk add --no-cache git ca-certificates tzdata

WORKDIR /src

# Dependencies are cached separately from source so a code-only change does not
# re-download the module graph.
COPY go.mod go.sum ./
RUN go mod download

COPY . .

ARG VERSION=dev
ARG GIT_COMMIT=unknown
ARG BUILT_AT=unknown

# CGO is off so the result is a static binary that runs on a distroless-style
# base. Symbols and DWARF are stripped: they are of no use in production and
# add tens of megabytes.
ENV CGO_ENABLED=0 GOOS=linux
RUN go build -trimpath -ldflags="-s -w" -o /out/api     ./cmd/api  && \
    go build -trimpath -ldflags="-s -w" -o /out/worker  ./cmd/worker && \
    go build -trimpath -ldflags="-s -w" -o /out/migrate ./cmd/migrate

# ---------------------------------------------------------------------------
# Runtime
# ---------------------------------------------------------------------------
FROM alpine:3.22

# wget serves the container healthcheck; tzdata is required because operating
# hours and cut-off times are evaluated in the tenant's zone.
RUN apk add --no-cache ca-certificates tzdata wget && \
    addgroup -g 10001 -S courier && \
    adduser -u 10001 -S -G courier courier

WORKDIR /app
COPY --from=build /out/api /out/worker /out/migrate /app/

# The process runs unprivileged and the filesystem is read-only in compose;
# nothing in the application writes to disk.
USER 10001:10001

ARG VERSION=dev
ARG GIT_COMMIT=unknown
ARG BUILT_AT=unknown
ENV SERVICE_VERSION=${VERSION} GIT_COMMIT=${GIT_COMMIT} BUILT_AT=${BUILT_AT}

EXPOSE 8080

HEALTHCHECK --interval=30s --timeout=3s --start-period=10s --retries=3 \
    CMD wget -qO- http://127.0.0.1:8080/livez >/dev/null || exit 1

ENTRYPOINT ["/app/api"]
