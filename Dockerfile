ARG BUILD_IMAGE=registry.fedoraproject.org/fedora@sha256:cffa25ae42f013b76653abd96137c05c776cd6e6bc1ac4a79290296ff42cbc7b
ARG RUNTIME_IMAGE=mcr.microsoft.com/azurelinux/base/core@sha256:4ecd6b297db85c54ec2df07145a28536c3655a3e98e54eb2364189bc4e6eac23

FROM ${BUILD_IMAGE} AS build-base
RUN dnf install -y --setopt=install_weak_deps=False \
        golang \
        nodejs24 \
        nodejs24-bin \
        nodejs24-full-i18n \
        nodejs24-npm \
        nodejs24-npm-bin && \
    dnf clean all && \
    rm -rf /var/cache/dnf

FROM build-base AS backend-build
ARG VERSION=0.2.0-dev
ARG COMMIT=unknown
ARG BUILD_DATE=unknown
ARG GOPROXY=https://goproxy.cn,direct
ENV GOTOOLCHAIN=auto
WORKDIR /src/beat
COPY go.mod go.sum ./
RUN GOPROXY="${GOPROXY}" go mod download
COPY cmd/ ./cmd/
COPY internal/ ./internal/
RUN GOPROXY="${GOPROXY}" CGO_ENABLED=0 go build -trimpath \
    -ldflags="-s -w -X github.com/beat/backend/internal/buildinfo.Version=${VERSION} -X github.com/beat/backend/internal/buildinfo.Commit=${COMMIT} -X github.com/beat/backend/internal/buildinfo.Date=${BUILD_DATE}" \
    -o /out/beat-server ./cmd/server && \
    GOPROXY="${GOPROXY}" CGO_ENABLED=0 go build -trimpath \
    -ldflags="-s -w -X github.com/beat/backend/internal/buildinfo.Version=${VERSION} -X github.com/beat/backend/internal/buildinfo.Commit=${COMMIT} -X github.com/beat/backend/internal/buildinfo.Date=${BUILD_DATE}" \
    -o /out/beat-agent ./cmd/agent && \
    GOPROXY="${GOPROXY}" CGO_ENABLED=0 go build -trimpath -o /out/beat-theme ./cmd/theme

FROM build-base AS frontend-build
# 主题分发资产由 scripts/build-themes.sh 生成；这里复用后端阶段已构建的 packer，
# 避免在前端阶段重复下载 Go 模块。构建产物会随 webui/public/ 进入 dist/static。
# webui 的 prebuild 以 ../scripts/build-themes.sh 定位仓库根，因此 scripts/themes/docs
# 必须与 webui/ 同级落在 /src/beat 下，不能散落在镜像根目录。
WORKDIR /src/beat
COPY --from=backend-build /out/beat-theme /usr/local/bin/beat-theme
COPY themes/ ./themes/
COPY docs/beat/theme-author-guide.md docs/beat/theme-sandbox-contract.md docs/beat/theme-market-publishing.md ./docs/beat/
COPY scripts/build-themes.sh ./scripts/build-themes.sh
ENV BEAT_THEME_BIN=/usr/local/bin/beat-theme
WORKDIR /src/beat/webui
COPY webui/package.json webui/package-lock.json ./
RUN npm ci && \
    npm audit --registry=https://registry.npmjs.org --omit=dev --audit-level=high
COPY webui/ ./
RUN npm run build

FROM ${RUNTIME_IMAGE}
RUN install -d -m 0700 -o 65532 -g 65532 /var/lib/beat
COPY --from=backend-build --chown=65532:65532 /out/beat-server /usr/local/bin/beat-server
COPY --from=backend-build --chown=65532:65532 /out/beat-agent /usr/local/bin/beat-agent
COPY --from=frontend-build --chown=65532:65532 /src/beat/webui/dist /opt/beat/static
COPY --chown=65532:65532 deploy/container-entrypoint.sh /usr/local/bin/beat-container
RUN chmod 0700 /usr/local/bin/beat-server /usr/local/bin/beat-agent && \
    chmod 0700 /usr/local/bin/beat-container && \
    chmod -R u=rwX,go= /opt/beat/static
USER 65532:65532
EXPOSE 8080
VOLUME ["/var/lib/beat"]
HEALTHCHECK --interval=30s --timeout=5s --start-period=15s --retries=3 \
    CMD curl --fail --silent --show-error --output /dev/null http://127.0.0.1:8080/readyz || exit 1
ENTRYPOINT ["/usr/local/bin/beat-container"]
CMD ["-db-path", "/var/lib/beat/data/beat.db", "-mts-path", "/var/lib/beat/data/beat_mts", "-static-dir", "/opt/beat/static", "-listen-addr", ":8080"]
