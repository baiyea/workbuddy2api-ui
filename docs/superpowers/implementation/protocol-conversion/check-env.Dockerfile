# Isolated verification tooling only; never used by production Compose.
FROM golang:1.26-alpine AS go-toolchain
FROM python:3.12-alpine
COPY --from=go-toolchain /usr/local/go /usr/local/go
ENV PATH=/usr/local/go/bin:$PATH
RUN apk add --no-cache bash git nodejs build-base docker-cli docker-cli-compose docker-cli-buildx curl
RUN python -m pip install --no-cache-dir anthropic==0.67.0 httpx==0.28.1 openai==2.6.1
