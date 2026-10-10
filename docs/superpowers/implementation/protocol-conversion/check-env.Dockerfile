# Isolated verification tooling only; never used by production Compose.
FROM golang:1.26-alpine
RUN apk add --no-cache bash python3 git nodejs build-base docker-cli docker-cli-compose
