FROM --platform=$BUILDPLATFORM golang:1.23-alpine AS build
WORKDIR /source
COPY console ./
ARG TARGETOS
ARG TARGETARCH
RUN go test ./... && CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH go build -trimpath -ldflags="-s -w" -o /out/console .

FROM alpine:3.20
RUN test "$(apk --print-arch)" = x86_64 \
    && apk add --no-cache ca-certificates tzdata wget \
    && adduser -D -u 10001 app \
    && mkdir -p /run/wb2a \
    && chown -R app:app /run/wb2a
COPY --from=build /out/console /app/console
COPY LICENSE /app/LICENSE
ENV WB2A_CORE_URL=http://core:7863 WB2A_LISTEN=:7863
USER app
EXPOSE 7863
ENTRYPOINT ["/app/console"]
