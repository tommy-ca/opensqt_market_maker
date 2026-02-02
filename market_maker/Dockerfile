# Stage 1: Build
FROM golang:1.25-bookworm AS builder

WORKDIR /app

# Copy workspce and modules
COPY go.work ./
COPY market_maker/go.mod market_maker/go.sum ./market_maker/
RUN cd market_maker && go mod download

# Copy source
COPY market_maker/ ./market_maker/

# Build
RUN cd market_maker && \
    CGO_ENABLED=1 GOOS=linux go build -o /app/market_maker cmd/market_maker/main.go && \
    CGO_ENABLED=1 GOOS=linux go build -o /app/exchange_connector cmd/exchange_connector/main.go

# Stage 2: Runtime
FROM debian:bookworm-slim

# Install sqlite and ca-certificates
RUN apt-get update && apt-get install -y --no-install-recommends \
    sqlite3 \
    ca-certificates \
    && rm -rf /var/lib/apt/lists/*

WORKDIR /app

# Copy binaries from builder
COPY --from=builder /app/market_maker .
COPY --from=builder /app/exchange_connector .

# Default config and data directories
RUN mkdir -p /app/configs /app/data

# Default environment variables
ENV LOG_LEVEL=INFO
ENV CONFIG_FILE=/app/configs/config.yaml
ENV DB_FILE=/app/data/market_maker.db

# Expose health and gRPC ports
EXPOSE 8080 50051

ENTRYPOINT ["./market_maker"]
