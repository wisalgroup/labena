# syntax=docker/dockerfile:1
# Build pinned upstream sources: the community registry images are unavailable.
# MinIO and mc are AGPL-3.0; source revisions and licenses ship with this image.
FROM golang:1.27.3-alpine AS build
ARG MINIO_VERSION=v0.0.0-20260212201848-7aac2a2c5b7c
ARG MC_VERSION=v0.0.0-20251106162529-77f82e18b540
RUN --mount=type=cache,target=/go/pkg/mod --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 GOBIN=/out go install -trimpath github.com/minio/minio@${MINIO_VERSION} && \
    CGO_ENABLED=0 GOBIN=/out go install -trimpath github.com/minio/mc@${MC_VERSION} && \
    mkdir -p /licenses && \
    cp /go/pkg/mod/github.com/minio/minio@${MINIO_VERSION}/LICENSE /licenses/minio.LICENSE && \
    cp /go/pkg/mod/github.com/minio/mc@${MC_VERSION}/LICENSE /licenses/mc.LICENSE && \
    printf 'minio %s\nmc %s\n' "$MINIO_VERSION" "$MC_VERSION" > /licenses/SOURCES

FROM alpine:3.22
RUN apk add --no-cache ca-certificates curl && \
    addgroup -g 10001 minio && adduser -D -u 10001 -G minio minio && \
    mkdir -p /data && chown minio:minio /data
COPY --from=build /out/minio /out/mc /usr/local/bin/
COPY --from=build /licenses/ /usr/share/licenses/minio/
USER 10001:10001
EXPOSE 9000 9001
ENTRYPOINT ["minio"]
CMD ["server", "/data", "--console-address", ":9001"]
