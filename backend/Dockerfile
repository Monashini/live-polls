# syntax=docker/dockerfile:1

# ---------------------------------------------------------------------------
# Stage 1: build
#
# The full Go toolchain is ~800MB. None of it needs to ship, which is the whole
# reason for a multi-stage build.
# ---------------------------------------------------------------------------
FROM golang:1.26-alpine AS build

WORKDIR /src

# Dependencies are copied and downloaded before the source. Docker caches each
# layer, so editing a .go file no longer re-downloads every module -- only the
# last two layers rebuild.
COPY go.mod go.sum ./
RUN go mod download

COPY . .

# CGO_ENABLED=0 produces a statically linked binary with no libc dependency,
# which is what lets the final stage be a scratch-like image with no OS at all.
#
# -trimpath strips local filesystem paths out of the binary (they would
# otherwise appear in stack traces and leak the build machine's directory
# layout). -ldflags "-s -w" drops the symbol table and DWARF debug info,
# roughly halving the binary.
RUN CGO_ENABLED=0 GOOS=linux go build \
      -trimpath \
      -ldflags="-s -w" \
      -o /out/server \
      ./cmd/server

# ---------------------------------------------------------------------------
# Stage 2: run
#
# distroless/static has no shell, no package manager and no busybox. If the
# container is ever compromised there is nothing in it to pivot with. It does
# include ca-certificates, which this app genuinely needs: both MongoDB Atlas
# and Upstash are reached over TLS, and without root certs the connection
# fails with an opaque x509 error.
# ---------------------------------------------------------------------------
FROM gcr.io/distroless/static-debian12:nonroot

# Runs as uid 65532, not root. If the process is exploited it cannot write to
# the filesystem or bind a privileged port.
USER nonroot:nonroot

COPY --from=build /out/server /server

# Documentation only -- the platform decides the real port via $PORT, which
# config.go already reads.
EXPOSE 8080

ENTRYPOINT ["/server"]
