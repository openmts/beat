ARG BUILD_IMAGE=registry.fedoraproject.org/fedora@sha256:cffa25ae42f013b76653abd96137c05c776cd6e6bc1ac4a79290296ff42cbc7b
ARG RUNTIME_IMAGE=mcr.microsoft.com/azurelinux/base/core@sha256:4ecd6b297db85c54ec2df07145a28536c3655a3e98e54eb2364189bc4e6eac23

FROM ${BUILD_IMAGE} AS build-base
RUN dnf install -y --setopt=install_weak_deps=False \
        golang \
        nodejs22 \
        nodejs22-bin \
        nodejs22-npm \
        nodejs22-npm-bin && \
    dnf clean all && \
    rm -rf /var/cache/dnf

FROM build-base AS frontend-build
WORKDIR /src/frontend
COPY frontend/package.json frontend/package-lock.json ./
RUN npm ci && \
    npm audit --registry=https://registry.npmjs.org --omit=dev --audit-level=high
COPY frontend/ ./
RUN npm run build

FROM build-base AS backend-build
ARG VERSION=dev
ARG COMMIT=unknown
ARG BUILD_DATE=unknown
ARG GOPROXY=https://goproxy.cn,direct
WORKDIR /src/backend
COPY backend/go.mod backend/go.sum ./
RUN GOPROXY="${GOPROXY}" go mod download
COPY backend/ ./
RUN GOPROXY="${GOPROXY}" CGO_ENABLED=0 go build -trimpath \
    -ldflags="-s -w -X github.com/beat/backend/internal/buildinfo.Version=${VERSION} -X github.com/beat/backend/internal/buildinfo.Commit=${COMMIT} -X github.com/beat/backend/internal/buildinfo.Date=${BUILD_DATE}" \
    -o /out/beat-server ./cmd/server && \
    GOPROXY="${GOPROXY}" CGO_ENABLED=0 go build -trimpath \
    -ldflags="-s -w -X github.com/beat/backend/internal/buildinfo.Version=${VERSION} -X github.com/beat/backend/internal/buildinfo.Commit=${COMMIT} -X github.com/beat/backend/internal/buildinfo.Date=${BUILD_DATE}" \
    -o /out/beat-agent ./cmd/agent

FROM ${RUNTIME_IMAGE}
RUN install -d -m 0700 -o 65532 -g 65532 /var/lib/beat
COPY --from=backend-build --chown=65532:65532 /out/beat-server /usr/local/bin/beat-server
COPY --from=backend-build --chown=65532:65532 /out/beat-agent /usr/local/bin/beat-agent
COPY --from=frontend-build --chown=65532:65532 /src/frontend/dist /opt/beat/static
RUN chmod 0700 /usr/local/bin/beat-server /usr/local/bin/beat-agent && \
    chmod -R u=rwX,go= /opt/beat/static
USER 65532:65532
EXPOSE 8080
VOLUME ["/var/lib/beat"]
HEALTHCHECK --interval=30s --timeout=5s --start-period=15s --retries=3 \
    CMD curl --fail --silent --show-error --output /dev/null http://127.0.0.1:8080/readyz || exit 1
ENTRYPOINT ["/usr/local/bin/beat-server"]
CMD ["-db-path", "/var/lib/beat/beat.db", "-mts-path", "/var/lib/beat/beat_mts", "-static-dir", "/opt/beat/static", "-listen-addr", ":8080"]
