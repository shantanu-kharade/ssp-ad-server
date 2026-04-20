.PHONY: build run test test-race lint clean dev tidy help migrate-up migrate-down

# Binary output
BINARY_NAME=ssp-adserver.exe
BUILD_DIR=bin

# Go parameters
GOCMD=go
GOBUILD=$(GOCMD) build
GOTEST=$(GOCMD) test
GOVET=$(GOCMD) vet
GOMOD=$(GOCMD) mod
GOFMT=gofmt

# Build flags
LDFLAGS=-ldflags "-s -w"

# Database
DB_URL ?= postgresql://ssp:ssp@localhost:5433/ssp_db?sslmode=disable
MIGRATIONS_DIR = internal/db/migrations

## build: Compile the binary
build:
	@echo Building $(BINARY_NAME)...
	@if not exist "$(BUILD_DIR)" mkdir "$(BUILD_DIR)"
	$(GOBUILD) $(LDFLAGS) -o $(BUILD_DIR)/$(BINARY_NAME) ./cmd/server/

## run: Build and run the server
run: build
	@echo Starting $(BINARY_NAME)...
	@$(BUILD_DIR)\$(BINARY_NAME)

## dev: Run with hot reload (requires air: go install github.com/air-verse/air@latest)
dev:
	@air -c .air.toml 2>NUL || ($(GOBUILD) -o $(BUILD_DIR)/$(BINARY_NAME) ./cmd/server/ && $(BUILD_DIR)\$(BINARY_NAME))

## test: Run all tests with coverage
test:
	$(GOTEST) -cover -count=1 ./...

## test-race: Run all tests with race detector and coverage
test-race:
	$(GOTEST) -race -cover -count=1 ./...

## lint: Run golangci-lint (install: go install github.com/golangci/golangci-lint/cmd/golangci-lint@latest)
lint:
	golangci-lint run ./...

## tidy: Tidy and verify module dependencies
tidy:
	$(GOMOD) tidy
	$(GOMOD) verify

## migrate-up: Run database up migrations
migrate-up:
	@echo Running up migrations...
	migrate -path $(MIGRATIONS_DIR) -database "$(DB_URL)" up

## migrate-down: Run database down migrations
migrate-down:
	@echo Running down migrations...
	migrate -path $(MIGRATIONS_DIR) -database "$(DB_URL)" down -all

## clean: Remove build artifacts
clean:
	@echo Cleaning...
	@if exist "$(BUILD_DIR)" rmdir /s /q "$(BUILD_DIR)"

## help: Display available targets
help:
	@echo Available targets:
	@echo   build        - Compile the binary
	@echo   run          - Build and run the server
	@echo   dev          - Run with hot reload (requires air)
	@echo   test         - Run all tests with coverage
	@echo   test-race    - Run all tests with race detector
	@echo   lint         - Run golangci-lint
	@echo   tidy         - Tidy and verify module dependencies
	@echo   migrate-up   - Run database up migrations
	@echo   migrate-down - Run database down migrations
	@echo   clean        - Remove build artifacts

