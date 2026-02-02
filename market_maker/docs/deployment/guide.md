# OpenSQT Market Maker - Deployment Guide

## 1. Prerequisites

- **Go**: 1.21+
- **Docker**: 20.10+ (optional)
- **Make**: For build automation

## 2. Configuration

Copy the example configuration:
```bash
cp configs/config.yaml.example configs/config.yaml
```

### 2.1 Essential Settings
- `app.current_exchange`: Select target exchange (binance, okx, bybit, etc.)
- `trading.symbol`: Trading pair (e.g., BTCUSDT)
- `risk_control.enabled`: Enable/Disable risk monitor

### 2.2 Concurrency Tuning (New)
Adjust worker pool sizes based on your hardware and load:
```yaml
concurrency:
  risk_pool_size: 10        # Parallel risk checks
  risk_pool_buffer: 1000    # Backlog size
  broadcast_pool_size: 10   # Notification workers
  broadcast_pool_buffer: 1000
```

## 3. Security Setup

### 3.1 TLS Certificates (Required for gRPC)
For production, generate fresh certificates:
```bash
# Generate server key and self-signed cert
mkdir -p certs
openssl req -x509 -newkey rsa:4096 -days 365 -nodes \
  -keyout certs/server-key.pem -out certs/server-cert.pem \
  -subj "/C=US/ST=State/L=City/O=OpenSQT/OU=Trading/CN=localhost"
```
Update `configs/config.yaml`:
```yaml
tls_cert_file: "certs/server-cert.pem"
tls_key_file: "certs/server-key.pem"
```

### 3.2 Credentials
Use environment variables to inject secrets (12-factor app):
```bash
export BINANCE_API_KEY="your_key"
export BINANCE_SECRET_KEY="your_secret"
export GRPC_API_KEYS="client-key-1,client-key-2"
```

## 4. Database Setup (New)

The system uses `atlas` to manage its internal SQLite schema. You must apply migrations before starting the application for the first time or after an update.

```bash
# Apply migrations to your production database
atlas migrate apply \
  --dir "file://migrations" \
  --url "sqlite://market_maker.db"
```

## 5. Running the System

### 4.1 Standalone Binary
```bash
go build -o bin/market_maker ./cmd/market_maker
./bin/market_maker --config configs/config.yaml
```

### 4.2 Docker Compose
```bash
docker-compose up -d
```

## 5. Monitoring

- **Health Check**: `GET http://localhost:8080/health`
- **Metrics**: `GET http://localhost:9090/metrics` (Prometheus)
- **Logs**: Structured JSON logs to stdout (ship to ELK/Datadog)
