<div align="center">
  <img src="https://r2.opensqt.com/opensqt_logo.png" alt="OpenSQT Logo" width="600"/>
  
  # OpenSQT Market Maker
  
  **毫秒级高频加密货币做市商系统 | High-Frequency Crypto Market Maker**

  [![Go Version](https://img.shields.io/badge/Go-1.21%2B-blue.svg)](https://golang.org/dl/)
  [![License](https://img.shields.io/badge/License-MIT-green.svg)](LICENSE)
</div>

---

## 📖 项目简介 (Introduction)

OpenSQT Market Maker 是一个高性能、低延迟的加密货币做市商系统，专注于永续合约与现货市场的网格交易策略。系统采用 Go 语言开发，基于 **Durable Workflows (DBOS)** 确保状态一致性，并通过 **gRPC** 实现交易所连接器的横向扩展。

OpenSQT is a high-performance, low-latency cryptocurrency market maker system. It features **Durable Execution** via DBOS for crash-resilient trading and **Decoupled gRPC Connectors** for multi-exchange scalability.

## ✨ 核心特性 (Key Features)

- **多交易所支持**: 适配 Binance (Spot/Futures), Bitget, Gate.io, OKX, Bybit 等。
- **持久化工作流**: 基于 DBOS 引擎，确保订单状态在系统崩溃后可完美恢复。
- **可扩展架构**: 交易引擎与交易所连接器通过 gRPC 解耦，支持多语言开发与独立缩放。
- **毫秒级响应**: 全 WebSocket 驱动（行情与订单流），配合 O(1) 槽位检索。
- **强大观测性**: 集成 OpenTelemetry Tracing、Metrics (Prometheus) 与 Zap 结构化日志。
- **高精度计算**: 全链路使用 `shopspring/decimal` 防止浮点数精度漂移。

## 🏗️ 系统架构 (Architecture)

```
market_maker/
├── cmd/
│   ├── market_maker/         # 交易引擎主程序 (Durable Engine)
│   └── exchange_connector/   # 统一交易所连接器 (gRPC Server)
├── proto/                    # Protobuf 定义 (buf managed)
├── internal/
│   ├── workflow/             # DBOS 核心工作流逻辑
│   ├── position/             # 确定性仓位决策逻辑
│   ├── exchange/             # 交易所适配器与 gRPC Proxy
│   └── infrastructure/       # OTel, HTTP/WS 基础组件
├── scripts/                  # 运维与辅助脚本
├── web/                      # 前端资产 (live monitoring)
archive/
└── legacy/                   # 归档的旧版代码与原型 (Legacy reference)
```

## 🛠️ 核心开发流程 (Development Workflow)

### 1. 标准构建与测试 (Go)
进入 `market_maker` 目录使用 Makefile:

```bash
cd market_maker
make help    # 查看可用命令
make build   # 编译所有组件
make test    # 运行含竞争检测的测试
make audit   # 运行完整质量审计 (staticcheck, vulncheck)
make proto   # 重新生成 Protobuf 代码 (Go & Python)
make proto/lint      # 检查 Protobuf 规范
make proto/breaking  # 检查 Protobuf 兼容性破坏
```

### 2. 代码质量保证 (Git Hooks)
项目使用 `pre-commit` 强制执行代码规范。首次开发前请安装：

```bash
# 安装 pre-commit 钩子
uvx pre-commit install
```

之后每次 `git commit` 时会自动运行以下检查：
- **Go**: `golangci-lint`, `go mod tidy`
- **Python**: `ruff` 检查与格式化
- **通用**: 结尾空格、文件末尾换行、YAML 语法检查

## 🚀 快速开始 (Getting Started)

### 1. 安装依赖 (Installation)

```bash
cd market_maker
go mod download
```

### 2. 编译组件 (Build)

```bash
# 编译统一连接器
go build -o exchange_connector cmd/exchange_connector/main.go

# 编译交易引擎
go build -o market_maker cmd/market_maker/main.go
```

### 3. 配置 (Configuration)

#### API 密钥配置 (API Credentials Setup)

本系统使用环境变量存储敏感的 API 密钥，确保安全性。请按以下步骤配置：

**步骤 1**: 复制环境变量模板
```bash
cd market_maker
cp .env.example .env
```

**步骤 2**: 编辑 `.env` 文件，填入真实的 API 密钥
```bash
# Binance API Credentials
BINANCE_API_KEY=your_actual_binance_api_key
BINANCE_SECRET_KEY=your_actual_binance_secret_key

# OKX API Credentials
OKX_API_KEY=your_actual_okx_api_key
OKX_SECRET_KEY=your_actual_okx_secret_key
OKX_PASSPHRASE=your_actual_okx_passphrase

# Bybit API Credentials
BYBIT_API_KEY=your_actual_bybit_api_key
BYBIT_SECRET_KEY=your_actual_bybit_secret_key
```

**步骤 3**: 在运行程序前加载环境变量
```bash
# 方法 1: 使用 source (Linux/Mac)
source .env

# 方法 2: 使用 export
export $(cat .env | xargs)

# 方法 3: 使用 direnv (推荐用于开发)
# 安装 direnv: https://direnv.net/
echo "dotenv" > .envrc
direnv allow
```

**重要安全提示**:
- `.env` 文件已在 `.gitignore` 中配置，不会被提交到版本控制
- 切勿将真实的 API 密钥提交到 Git 仓库
- 定期轮换 API 密钥，遵循最佳安全实践
- 为 API 密钥设置适当的权限（仅交易权限，禁用提现）

#### 策略参数配置 (Strategy Parameters)

编辑 `configs/config.yaml` 配置交易策略参数（注意：API 密钥通过环境变量加载，无需在此文件中配置）。

### 4. 统一保证金 (Unified Margin)

本系统支持 Bybit UTA, Binance Portfolio Margin 和 OKX 统一账户。
- **高资金效率**: 自动对冲现货与合约盈亏，减少保证金需求。
- **风险提示**: 即使开启 Unified Margin，强烈建议为做市商策略使用**独立的子账户 (Sub-account)**。
- **自动减仓**: 系统在账户 Health Score 低于 0.7 时会自动减仓 50%，低于 0.5 时会触发全仓退出。

The system supports Unified Margin (UM) for Bybit, Binance, and OKX.
- **Capital Efficiency**: Automatically offsets Spot/Perp PnL.
- **Safety**: Using **dedicated sub-accounts** is strongly recommended.
- **De-leveraging**: Auto-reduces exposure by 50% at 0.7 health score and exits at 0.5.

### 5. 运行 (Usage)

#### 启动交易所连接器 (Start Connectors)

```bash
# 启动币安连接器
./exchange_connector --exchange binance --port 50051

# 启动 OKX 连接器
./exchange_connector --exchange okx --port 50052
```

#### 启动交易引擎 (Start Engine)

```bash
./market_maker --config config.yaml
```

## ⚠️ 免责声明 (Disclaimer)

本软件仅供学习和研究使用。加密货币交易具有极高风险，可能导致资金损失。
- 使用本软件产生的任何盈亏由用户自行承担。
- 请务必在实盘前使用测试网 (Testnet) 进行充分测试。
- **SECURITY WARNING**: 默认配置中的 PostgreSQL 密码为弱密码 ("secret")。在生产环境中部署时，请务必在 `.env` 文件中修改 `POSTGRES_PASSWORD` 为强密码。
- **SECURITY WARNING**: 默认情况下 HTTP/Health 端口 (8080/8081) 对外暴露。在公网环境部署时，请使用防火墙或反向代理 (Nginx) 限制访问。

This software is for educational and research purposes only. Cryptocurrency trading involves high risk.
- Users are solely responsible for any profits or losses.
- Always test thoroughly on Testnet before using real funds.
- **SECURITY WARNING**: The default PostgreSQL password is "secret". CHANGE THIS via `.env` before production deployment.
- **SECURITY WARNING**: Default ports (8080/8081) are exposed. Restrict access using firewalls or reverse proxies in public environments.

---
Copyright © 2026 OpenSQT Team. All Rights Reserved.
