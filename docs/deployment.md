# Production Deployment Guide

This guide covers how to deploy the OpenSQT Market Maker in a production environment using Docker and Docker Compose.

## 1. Prerequisites
- Docker and Docker Compose installed.
- API keys for your selected exchange(s).

## 2. Configuration

### 2.1 Credentials
Copy the `.env.example` file to `.env` and fill in your exchange API keys.
```bash
cp .env.example .env
# Edit .env with your keys
```

### 2.2 Trading Parameters
The trading parameters are located in `market_maker/configs/config.yaml`.
Key parameters to check:
- `symbol`: The trading pair (e.g., BTCUSDT).
- `price_interval`: Distance between grid levels.
- `order_quantity`: Size of each order.
- `grid_mode`: `long` or `neutral`.

## 3. Deployment Architecture

The OpenSQT Market Maker MUST be deployed using the **gRPC Architecture** (formerly Phase 16) for all production environments. This ensures proper fault isolation, centralized rate limiting, and language independence.

```
┌─────────────────────┐
│ exchange_connector  │ ← Single process managing exchange connection
│   :50051 (gRPC)     │
└──────────┬──────────┘
           │ gRPC
   ┌───────┴────────┐
   │                │
┌──▼──────┐    ┌────▼───────┐
│ market   │    │ live_server │
│ _maker   │    │            │
└──────────┘    └────────────┘
```

## 4. Deployment Steps

### 4.1 gRPC Mode (REQUIRED)
1. Ensure `.env` contains your API credentials.
2. Verify `docker-compose.grpc.yml` is used.
3. Start the stack:
```bash
docker-compose -f docker-compose.grpc.yml up -d
```

### 4.2 Legacy Standalone Mode (DEPRECATED)
Direct native connection mode is deprecated and should only be used for local development or debugging specific connection issues.

## 5. Monitoring

### 4.1 Health Check
The engine exposes a health check endpoint at `http://localhost:8080/health`. It returns the current status of exchange connectivity and trading symbol.

### 4.2 Logs
View real-time logs for the engine:
```bash
docker-compose logs -f market-maker
```

## 5. Persistence & Recovery
The system stores the current inventory and order state in a SQLite database located at `/app/data/market_maker.db`.
- This database is mapped to a Docker volume (`market-data`).
- If the container restarts, it will automatically reload the state.
- On cold start (if DB is lost), the bot fetches current positions from the exchange and reconstructs the grid logic.

## 6. Upgrade Strategy
To upgrade the bot to a new version:
1. Pull latest code.
2. Rebuild and restart:
```bash
docker-compose up -d --build market-maker
```
The persistent volume ensures that the trading state is preserved during the upgrade.
