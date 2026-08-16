# One image running both binaries. They share a module, a base image and a user, so
# splitting them would mean keeping two copies of the same hardening in step.

FROM golang:1.26.6-alpine AS build

WORKDIR /src

# Module download is its own layer: dependencies change far less often than source,
# so an edit to internal/ does not re-fetch the module cache.
COPY go.mod go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod \
    go mod download

COPY . .

# The release workflow passes the git tag. Left at dev for a local or CI build, which
# is exactly what an unversioned binary should call itself.
ARG VERSION=dev

# CGO off makes the binaries static. trimpath keeps build paths out of panics; -s -w
# drops the symbol and DWARF tables, which the service has no use for in production.
# One go build call for both: it shares the loaded package graph between them.
RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 GOOS=linux \
    go build -trimpath -ldflags="-s -w -X main.version=${VERSION}" -o /out/ ./cmd/api ./cmd/worker

# Running two processes needs something to start them, so this cannot be a distroless
# base: those carry no shell at all. The minimal tag is the smallest Rocky that still
# has one, and it already ships the CA bundle the worker needs to reach Raider.IO over
# TLS, so there is no package to install here.
#
# quay.io, not docker.io/rockylinux: the Docker Hub repo is the deprecated one and its
# newest image was built in 2023.
FROM quay.io/rockylinux/rockylinux:10-minimal

COPY --from=build /out/api /out/worker /

# A container is one lifecycle, so the failure of either process has to end it. Without
# the fail-fast, a crashed worker sits invisible behind an api that still answers, and
# the first sign of it is stale Raider.IO data nobody can explain.
#
# The trap forwards SIGTERM to both children, because the shell is PID 1 here and a
# signal to PID 1 is not delivered to the process group. Both mains already handle it
# via signal.NotifyContext, so this only has to reach them.
#
# The api applies migrations and the worker only reads the schema they produce, hence
# the ordering. Both tickers still fire once on startup, so a first run against an
# empty database logs one relation-does-not-exist per ticker before goose finishes.
# The worker retries on its next tick and recovers on its own.
COPY <<'EOF' /entrypoint.sh
#!/bin/sh
set -eu

/api &
api=$!
/worker &
worker=$!

stop() {
	kill -TERM "$api" "$worker" 2>/dev/null || true
}

# Only the handler sets the flag, because the shutdown path below calls stop too and a
# flag set there would report every crash as an orderly stop.
stopping=0
on_signal() {
	stopping=1
	stop
}
trap on_signal TERM INT

# busybox ash returns from `wait -n` only for a child that exits after the call, so a
# process dying during startup hangs it forever, which is the one case this has to
# catch. Polling liveness has no such gap.
while kill -0 "$api" 2>/dev/null && kill -0 "$worker" 2>/dev/null; do
	sleep 1
done

stop
wait || true

if [ "$stopping" -eq 1 ]; then
	exit 0
fi
echo "entrypoint: api or worker exited, stopping the container" >&2
exit 1
EOF

RUN chmod 755 /entrypoint.sh

# Numeric, not a name: a Kubernetes runAsNonRoot check reads the image's USER field and
# cannot resolve a name against the image's passwd file, so a named user fails the
# admission check that this line exists to satisfy.
USER 65532:65532

# Above 1024, so dropping every capability including NET_BIND_SERVICE still leaves the
# listener able to bind. Matches the ADDR default in cmd/api/config.go.
EXPOSE 8080

# Exec form, so the shell is PID 1 and receives SIGTERM itself. Its trap is what turns
# that into a clean shutdown of both children.
ENTRYPOINT ["/entrypoint.sh"]
