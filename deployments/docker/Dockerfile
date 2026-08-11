# PCCP Multi-stage Dockerfile
# Build stage
FROM golang:1.26-alpine AS go-builder

RUN apk add --no-cache git gcc musl-dev

WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 go build -o /pccp-server ./cmd/pccp-server/ && \
    CGO_ENABLED=0 go build -o /pccp-relay ./cmd/pccp-relay/ && \
    CGO_ENABLED=0 go build -o /pccp-pia ./cmd/pccp-pia/

# Frontend build stage
FROM node:22-alpine AS web-builder
WORKDIR /web
COPY web/package.json web/pnpm-lock.yaml web/pnpm-workspace.yaml ./
RUN corepack enable pnpm && pnpm install --frozen-lockfile
COPY web/ .
RUN pnpm build

# Final stage
FROM alpine:3.20
RUN apk add --no-cache ca-certificates tzdata

WORKDIR /app

# Copy binaries
COPY --from=go-builder /pccp-server /app/pccp-server
COPY --from=go-builder /pccp-relay /app/pccp-relay
COPY --from=go-builder /pccp-pia /app/pccp-pia

# Copy frontend
COPY --from=web-builder /web/dist /app/web/dist

# Default to Control Plane server
ENV PCCP_HTTP_ADDR=:8080
ENV PCCP_DB_DRIVER=sqlite
ENV PCCP_DB_DSN=/data/pccp.db
ENV PCCP_DEFAULT_LOCALE=ko-KR

VOLUME ["/data"]
EXPOSE 8080 8090 9090

# The entrypoint selects which binary to run based on the first arg
COPY deployments/docker/entrypoint.sh /entrypoint.sh
RUN chmod +x /entrypoint.sh
ENTRYPOINT ["/entrypoint.sh"]
CMD ["server"]
