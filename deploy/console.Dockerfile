FROM golang:1.23-alpine AS build
WORKDIR /source
COPY console ./
RUN go test ./... && CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/console .

FROM alpine:3.20
RUN apk add --no-cache ca-certificates tzdata wget \
    && adduser -D -u 10001 app \
    && mkdir -p /run/wb2a \
    && chown -R app:app /run/wb2a
COPY --from=build /out/console /app/console
ENV WB2A_CORE_URL=http://core:7863 WB2A_LISTEN=:7863
USER app
EXPOSE 7863
HEALTHCHECK --interval=30s --timeout=5s --start-period=5s CMD wget -qO- http://127.0.0.1:7863/livez || exit 1
ENTRYPOINT ["/app/console"]
