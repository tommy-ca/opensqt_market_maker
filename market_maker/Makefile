# ==================================================================================== #
# VARIABLES
# ==================================================================================== #

BIN_DIR = ./bin

# ==================================================================================== #
# HELPERS
# ==================================================================================== #

## help: print this help message
.PHONY: help
help:
	@echo 'Usage:'
	@sed -n 's/^##//p' ${MAKEFILE_LIST} | column -t -s ':' |  sed -e 's/^/ /'

.PHONY: confirm
confirm:
	@echo -n 'Are you sure? [y/N] ' && read ans && [ $${ans:-N} = y ]

# ==================================================================================== #
# QUALITY CONTROL
# ==================================================================================== #

## audit: run quality control checks
.PHONY: audit
audit:
	@echo 'Tidying and verifying module dependencies...'
	go mod tidy
	go mod verify
	@echo 'Formatting code...'
	go fmt ./...
	@echo 'Vetting code...'
	go vet ./...
	@echo 'Running staticcheck...'
	@go run honnef.co/go/tools/cmd/staticcheck@latest -checks=all,-ST1000,-U1000 ./...
	@echo 'Running govulncheck...'
	@out=$$(go run golang.org/x/vuln/cmd/govulncheck@latest ./... 2>&1); status=$$?; echo "$$out"; \
	if [ $$status -ne 0 ]; then \
		case "$$out" in \
			*"403 Forbidden"*) echo "Warning: govulncheck could not reach vuln.go.dev (403), skipping." ;; \
			*) exit $$status ;; \
		esac; \
	fi

## test: run all tests
.PHONY: test
test:
	go test -v -race -buildvcs ./...

## test/cover: run all tests and display coverage
.PHONY: test/cover
test/cover:
	go test -v -race -buildvcs -coverprofile=/tmp/coverage.out ./...
	go tool cover -html=/tmp/coverage.out

# ==================================================================================== #
# DEVELOPMENT
# ==================================================================================== #

## tidy: tidy modfiles and format .go files
.PHONY: tidy
tidy:
	go mod tidy -v
	go fmt ./...

## build: build all applications
.PHONY: build
build: build/market_maker build/exchange_connector build/live_server

## build/market_maker: build the market_maker application
.PHONY: build/market_maker
build/market_maker:
	@mkdir -p ${BIN_DIR}
	go build -o=${BIN_DIR}/market_maker ./cmd/market_maker

## build/exchange_connector: build the exchange_connector application
.PHONY: build/exchange_connector
build/exchange_connector:
	@mkdir -p ${BIN_DIR}
	go build -o=${BIN_DIR}/exchange_connector ./cmd/exchange_connector

## build/live_server: build the live_server application
.PHONY: build/live_server
build/live_server:
	@mkdir -p ${BIN_DIR}
	go build -o=${BIN_DIR}/live_server ./cmd/live_server

## proto: generate protobuf files
.PHONY: proto
proto:
	buf generate

## proto/lint: lint protobuf files
.PHONY: proto/lint
proto/lint:
	buf lint

## proto/breaking: check for breaking changes against main branch
.PHONY: proto/breaking
proto/breaking:
	# Ignore if we can't find the branch, common in some CI or detached HEAD states
	-buf breaking --against ".git#branch=main" || echo "Warning: Could not run breaking change check against main"

## clean: remove binary artifacts
.PHONY: clean
clean:
	rm -rf ${BIN_DIR}

# ==================================================================================== #
# OPERATIONS
# ==================================================================================== #

## push: push changes to the remote Git repository
.PHONY: push
push: audit
	git push

# Default target
.DEFAULT_GOAL := help
