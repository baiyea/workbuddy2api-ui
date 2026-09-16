FROM --platform=$BUILDPLATFORM golang:1.23-alpine AS build
RUN apk add --no-cache git python3
WORKDIR /source
COPY upstream.lock ./
COPY upstream ./upstream
COPY extensions ./extensions
COPY patches ./patches
COPY scripts/overlay.py ./scripts/overlay.py
RUN python3 scripts/overlay.py prepare --output /build/core
WORKDIR /build/core
RUN go mod download
ARG TARGETOS
ARG TARGETARCH
RUN set -eu; \
    commit="$(python3 -c 'import json; print(json.load(open("/source/upstream.lock"))["commit"])')"; \
    identity="$(python3 /source/scripts/overlay.py identity)"; \
    go test ./...; \
    CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH go build -trimpath -ldflags="-s -w -X main.upstreamCommit=$commit -X main.patchIdentity=$identity" -o /out/wb2api ./cmd/server; \
    for command in signin login credit trial activity; do CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH go build -trimpath -ldflags="-s -w" -o "/out/$command" "./cmd/$command"; done

FROM alpine:3.20
RUN test "$(apk --print-arch)" = x86_64 \
    && apk add --no-cache bash ca-certificates python3 su-exec tzdata wget \
    && adduser -D -u 10001 app \
    && mkdir -p /app/auths /app/data /app/scripts /run/wb2a \
    && chown -R app:app /app /run/wb2a
WORKDIR /app
ENV WB2A_CORE=true WB2A_LISTEN=:7863 WB2A_AUTH_DIR=/app/auths WB2A_STATE_FILE=/app/data/state.json
COPY deploy/default-config.json /app/config.json
COPY --chmod=755 deploy/core-entrypoint.sh /usr/local/bin/wb2api-entrypoint.sh
COPY LICENSE /app/LICENSE
COPY --from=build /out/wb2api /app/wb2api
COPY --from=build /out/signin /app/signin_bin
COPY --from=build /out/login /app/login
COPY --from=build /out/credit /app/credit
COPY --from=build /out/trial /app/trial_bin
COPY --from=build /out/activity /app/activity_bin
COPY --from=build /build/core/checkin.sh /build/core/login.sh /build/core/signin.sh /build/core/credit.sh /build/core/trial.sh /app/
COPY --from=build /build/core/scripts/ /app/scripts/
RUN sed -i 's/\r$//' /app/*.sh /app/scripts/*.py && chmod 755 /app/*.sh /app/scripts/*.py
USER root
EXPOSE 7863
ENTRYPOINT ["/usr/local/bin/wb2api-entrypoint.sh"]
CMD ["/app/wb2api", "-config", "/app/config.json"]
