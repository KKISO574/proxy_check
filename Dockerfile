FROM node:22-bookworm AS frontend-builder

WORKDIR /src/frontend
COPY frontend/package*.json ./
RUN npm ci
COPY frontend/ ./
RUN npm run build

FROM golang:1.23-bookworm AS backend-builder

WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY backend ./backend
RUN CGO_ENABLED=0 GOOS=linux go build -o /out/proxy-check ./backend/cmd/proxy-check

FROM debian:bookworm-slim AS runtime

ENV PROXY_CHECK_CONFIG=/app/configs/config.yaml

WORKDIR /app

RUN apt-get update \
    && apt-get install -y --no-install-recommends ca-certificates \
    && rm -rf /var/lib/apt/lists/*

COPY configs ./configs
COPY --from=frontend-builder /src/web/static ./web/static
COPY --from=backend-builder /out/proxy-check /usr/local/bin/proxy-check

RUN mkdir -p /app/data /app/runtime/bin /app/runtime/mihomo /app/runtime/miaospeed /app/runtime/downloads

EXPOSE 8000

CMD ["proxy-check"]
