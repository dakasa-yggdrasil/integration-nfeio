# Multi-stage build: golang:1.25-bookworm -> distroless/base-debian12:nonroot.
# Matches the pattern used by integration-rabbitmq, integration-grafana,
# integration-secrets-management for consistency with the dakasa-yggdrasil
# adapter fleet.

FROM golang:1.25-bookworm AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
ARG VERSION
RUN source_version="$(./scripts/adapter-version.sh)" && \
    resolved_version="${VERSION:-${source_version}}" && \
    if [ "${resolved_version}" != "${source_version}" ]; then \
      echo "VERSION ${resolved_version} does not match AdapterVersion ${source_version}" >&2; \
      exit 1; \
    fi && \
    CGO_ENABLED=0 GOOS=linux go build \
    -trimpath \
    -ldflags="-s -w -X github.com/dakasa-yggdrasil/integration-nfeio/providers/nfeio/adapter.AdapterVersion=${resolved_version}" \
    -o /out/adapter ./cmd/adapter

FROM gcr.io/distroless/base-debian12:nonroot AS runtime
LABEL org.opencontainers.image.source="https://github.com/dakasa-yggdrasil/integration-nfeio"
LABEL org.opencontainers.image.licenses="Apache-2.0"
COPY --from=build /out/adapter /usr/local/bin/adapter
COPY --from=build /src/manifest/templates /etc/nfeio/templates
USER nonroot:nonroot
EXPOSE 8080 8081 8082
ENTRYPOINT ["/usr/local/bin/adapter"]
