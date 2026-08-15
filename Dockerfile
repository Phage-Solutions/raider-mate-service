# One Dockerfile for both binaries. They share a module, a base image and a user, so
# splitting them into two files would mean keeping two copies of the same hardening in
# step. Pick one with --build-arg CMD=api or CMD=worker.

FROM golang:1.26.6-alpine AS build

WORKDIR /src

# Module download is its own layer: dependencies change far less often than source,
# so an edit to internal/ does not re-fetch the module cache.
COPY go.mod go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod \
    go mod download

COPY . .

ARG CMD=api

# CGO off makes the binary static, which is what lets the runtime stage be a base with
# no libc at all. trimpath keeps build paths out of panics; -s -w drops the symbol and
# DWARF tables, which the service has no use for in production.
RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 GOOS=linux \
    go build -trimpath -ldflags='-s -w' -o /out/service ./cmd/${CMD}

# distroless static carries ca-certificates, which the worker needs to reach Raider.IO
# over TLS, and nothing else: no shell, no package manager, no busybox. The :nonroot
# tag ships a passwd entry for uid 65532 so the USER below resolves to a real name.
FROM gcr.io/distroless/static-debian12:nonroot

COPY --from=build /out/service /service

# Numeric, not a name: a Kubernetes runAsNonRoot check reads the image's USER field and
# cannot resolve a name against the image's passwd file, so a named user fails the
# admission check that this line exists to satisfy.
USER 65532:65532

# Above 1024, so dropping every capability including NET_BIND_SERVICE still leaves the
# listener able to bind. Matches the ADDR default in cmd/api/config.go.
EXPOSE 8080

# The binary is PID 1 and gets SIGTERM directly, which both mains already handle via
# signal.NotifyContext. No shell form, so no shell to swallow the signal.
ENTRYPOINT ["/service"]
