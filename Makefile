# OpenSQT Root Orchestrator

.PHONY: help build test lint proto

help:
	@echo "OpenSQT Root Makefile"
	@echo "Available commands:"
	@echo "  build   - Build both Go and Python components"
	@echo "  test    - Run tests for both Go and Python"
	@echo "  lint    - Run lints for both Go and Python"
	@echo "  proto   - Regenerate Protobuf code for both languages"

build:
	cd market_maker && $(MAKE) build
	@echo "Python components are ready (uv managed)"

test:
	cd market_maker && $(MAKE) test
	cd python-connector && uv run pytest -m "not integration" tests

test-all:
	cd market_maker && $(MAKE) test
	cd python-connector && uv run pytest tests

lint:
	cd market_maker && $(MAKE) audit
	cd python-connector && uv run ruff check .

proto:
	cd market_maker && $(MAKE) proto
