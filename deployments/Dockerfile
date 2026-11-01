# Shared application image for development and release verification.
# Update this digest together with go.mod; no live OS package installation.
FROM golang:1.27.3-alpine@sha256:d200d2dd76b03a99b52651fd1325aff7dcd35422c35c01bac56571b0507f3deb AS build
ENV GOTOOLCHAIN=local CGO_ENABLED=0
ARG VERSION=dev
ARG REVISION=unknown
WORKDIR /src
COPY go.mod go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod go mod download && go mod verify
COPY cmd ./cmd
COPY internal ./internal
COPY pkg ./pkg
RUN --mount=type=cache,target=/go/pkg/mod --mount=type=cache,target=/root/.cache/go-build \
    module_path=$(go list -m) && \
    go build -mod=readonly -trimpath -buildvcs=false -tags timetzdata \
    -ldflags "-X ${module_path}/internal/app.version=${VERSION} -X ${module_path}/internal/app.revision=${REVISION}" \
    -o /out/ ./cmd/app ./cmd/cli ./cmd/worker ./cmd/scheduler && \
    mkdir -p /out/storage/logs /out/storage/app

FROM scratch
WORKDIR /app
COPY --from=build /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/ca-certificates.crt
# Docker initializes new named volumes with this non-root ownership.
COPY --from=build --chown=10001:10001 /out/ /app/
COPY LICENSE /licenses/labena/LICENSE
COPY --from=build /usr/local/go/LICENSE /usr/local/go/PATENTS /licenses/go/
USER 10001:10001
EXPOSE 8080
ENTRYPOINT ["/app/app"]
