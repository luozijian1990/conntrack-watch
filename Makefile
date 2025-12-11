# Conntrack Watch Makefile
# -----------------------------------------------------------------------------

BINARY_NAME := conntrack-watch
BUILD_DIR := build
CMD_PATH := ./cmd/conntrack-watch

# 版本信息（可通过环境变量覆盖）
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
COMMIT ?= $(shell git rev-parse --short HEAD 2>/dev/null || echo "unknown")
BUILD_TIME ?= $(shell date '+%Y-%m-%d %H:%M:%S')

# Go 构建参数
GO := go
GOFLAGS := -trimpath
LDFLAGS := -s -w \
	-X 'main.Version=$(VERSION)' \
	-X 'main.Commit=$(COMMIT)' \
	-X 'main.BuildTime=$(BUILD_TIME)'

# 目标平台
GOOS ?= linux
GOARCH ?= amd64

.PHONY: all build build-linux clean run test lint fmt help

# 默认目标
all: build

## build: 构建当前平台二进制
build:
	@echo "==> Building $(BINARY_NAME)..."
	$(GO) build $(GOFLAGS) -ldflags "$(LDFLAGS)" -o $(BUILD_DIR)/$(BINARY_NAME) $(CMD_PATH)
	@echo "==> Built: $(BUILD_DIR)/$(BINARY_NAME)"

## build-linux: 交叉编译 Linux amd64 二进制
build-linux:
	@echo "==> Cross-compiling for Linux amd64..."
	GOOS=linux GOARCH=amd64 $(GO) build $(GOFLAGS) -ldflags "$(LDFLAGS)" \
		-o $(BUILD_DIR)/$(BINARY_NAME)-linux-amd64 $(CMD_PATH)
	@echo "==> Built: $(BUILD_DIR)/$(BINARY_NAME)-linux-amd64"

## build-all: 构建所有平台
build-all: build-linux
	@echo "==> All builds completed"

## clean: 清理构建产物
clean:
	@echo "==> Cleaning..."
	rm -rf $(BUILD_DIR)
	@echo "==> Clean completed"

## run: 本地运行（需要 root 权限）
run: build
	sudo $(BUILD_DIR)/$(BINARY_NAME) -config config.yaml

## test: 运行测试
test:
	@echo "==> Running tests..."
	$(GO) test -v -race ./...

## lint: 代码检查
lint:
	@echo "==> Running linter..."
	@which golangci-lint > /dev/null || (echo "Please install golangci-lint" && exit 1)
	golangci-lint run ./...

## fmt: 格式化代码
fmt:
	@echo "==> Formatting code..."
	$(GO) fmt ./...
	@echo "==> Format completed"

## mod: 整理依赖
mod:
	@echo "==> Tidying modules..."
	$(GO) mod tidy
	$(GO) mod verify
	@echo "==> Modules tidied"

## version: 显示版本信息
version:
	@echo "Version:    $(VERSION)"
	@echo "Commit:     $(COMMIT)"
	@echo "Build Time: $(BUILD_TIME)"

## help: 显示帮助信息
help:
	@echo "Usage: make [target]"
	@echo ""
	@echo "Targets:"
	@grep -E '^## ' Makefile | sed 's/## /  /' | column -t -s ':'
