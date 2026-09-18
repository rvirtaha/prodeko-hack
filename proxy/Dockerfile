# Deliberately free of BuildKit-only syntax: no `# syntax=` directive, no
# heredocs, no `RUN --mount=type=cache`. Rootless Docker on fuse-overlayfs
# cannot do cross-stage COPY under BuildKit, so this image has to remain
# buildable with DOCKER_BUILDKIT=0. See DEV.md.

FROM golang:1.27-bookworm AS build

WORKDIR /src

# Dependencies first, so a source-only change does not re-download them.
# The bracket in go.su[m] makes the file optional: the module has no
# dependencies yet, so go.sum does not exist.
COPY go.mod go.su[m] ./
RUN go mod download

COPY . .

# Static, stripped, reproducible. CGO off because the final image has no libc.
# The binary's package is ./cmd/proxy by convention; the module root is
# accepted as a fallback so the image builds whichever layout main.go lands in.
RUN set -eu; \
    if [ -d ./cmd/proxy ]; then pkg=./cmd/proxy; else pkg=.; fi; \
    CGO_ENABLED=0 GOOS=linux go build \
        -trimpath \
        -ldflags="-s -w" \
        -o /out/cms-auth-proxy \
        "$pkg"


FROM gcr.io/distroless/static:nonroot

# distroless/static carries ca-certificates and tzdata, which is everything a
# Go binary needs to make outbound TLS calls to Keycloak and GitHub. It has no
# shell, no package manager and no coreutils, so there is nothing to exec into
# and no HEALTHCHECK is possible here.
COPY --from=build /out/cms-auth-proxy /usr/local/bin/cms-auth-proxy

USER nonroot:nonroot
EXPOSE 8080

ENTRYPOINT ["/usr/local/bin/cms-auth-proxy"]
