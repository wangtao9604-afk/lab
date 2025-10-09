# ============================================
# Project-wide Makefile (static musl + dynamic)
# ============================================

SHELL := /bin/bash

# ---- 基本参数（可通过环境覆盖）----
PROJECT_NAME       ?= qywx
APP_DIR            ?= app
SERVICES           ?= producer consumer recorder
BIN_DIR            ?= bin
GO                 ?= go

# ---- 版本信息（可选；如需 -X 注入可在 GO_LDFLAGS 自行追加）----
VERSION            ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
COMMIT             ?= $(shell git rev-parse --short HEAD 2>/dev/null || echo "unknown")
BUILD_TIME         ?= $(shell date -u +'%Y-%m-%dT%H:%M:%SZ')

# ---- musl / 静态链接相关 ----
MUSL_CC            ?= musl-gcc
# 建议保留 -static 与 --gc-sections；如遇到重复符号冲突可在此处按需追加其它链接器选项
MUSL_EXTLDFLAGS    ?= -static -Wl,--gc-sections

# 允许通过 EXTRA_TAGS 追加构建 tags（例如：timetzdata netgo osusergo）
EXTRA_TAGS         ?=

# ---- 通用的 Go 构建参数（工程化）----
# Release 建议默认开启：去绝对路径、只读模块、不写 VCS 元数据
GO_FLAGS_BASE      := -trimpath -mod=readonly -buildvcs=false
# 静态构建使用 musl，且支持注入额外 tags
GO_TAGS_BASE       := musl $(EXTRA_TAGS)

# 统一组织 -ldflags，以便正确传入嵌套的 -extldflags
GO_LDFLAGS_COMMON  := $(GO_LDFLAGS) -linkmode external -extldflags '$(MUSL_EXTLDFLAGS)'
# Release 版默认 strip 符号
GO_LDFLAGS_RELEASE := $(GO_LDFLAGS_COMMON) -s -w
# Debug 版禁优化、禁内联
GO_GCFLAGS_DEBUG   := all=-N -l

# ---- 帮助文本渲染（make help）----
.PHONY: help
help: ## 显示可用目标
	@echo "Usage: make <target>"
	@echo
	@grep -E '^[a-zA-Z0-9_.-]+:.*?## ' $(MAKEFILE_LIST) | awk 'BEGIN{FS=":.*?## "}; {printf "  \033[36m%-32s\033[0m %s\n", $$1, $$2}'

# --------------------------------------------
# 依赖/质量相关
# --------------------------------------------
.PHONY: deps tidy verify vet fmt test clean print-vars
deps: ## 预拉依赖（只读缓存）
	$(GO) mod download -modcacherw

tidy: ## 规范 go.mod/go.sum
	$(GO) mod tidy

verify: ## 校验 go.sum
	$(GO) mod verify

vet: ## 静态检查
	$(GO) vet ./...

fmt: ## 代码格式化
	$(GO) fmt ./...

test: ## 运行单元测试
	$(GO) test ./...

clean: ## 清理构建产物
	rm -rf $(BIN_DIR)

print-vars: ## 打印关键变量（调试用）
	@echo "PROJECT_NAME        = $(PROJECT_NAME)"
	@echo "SERVICES            = $(SERVICES)"
	@echo "APP_DIR             = $(APP_DIR)"
	@echo "BIN_DIR             = $(BIN_DIR)"
	@echo "MUSL_CC             = $(MUSL_CC)"
	@echo "MUSL_EXTLDFLAGS     = $(MUSL_EXTLDFLAGS)"
	@echo "EXTRA_TAGS          = $(EXTRA_TAGS)"
	@echo "GO_FLAGS_BASE       = $(GO_FLAGS_BASE)"
	@echo "GO_TAGS_BASE        = $(GO_TAGS_BASE)"
	@echo "GO_LDFLAGS          = $(GO_LDFLAGS)"
	@echo "GO_LDFLAGS_COMMON   = $(GO_LDFLAGS_COMMON)"
	@echo "GO_LDFLAGS_RELEASE  = $(GO_LDFLAGS_RELEASE)"
	@echo "GO_GCFLAGS_DEBUG    = $(GO_GCFLAGS_DEBUG)"
	@echo "VERSION/COMMIT/TIME = $(VERSION) / $(COMMIT) / $(BUILD_TIME)"

# --------------------------------------------
# 微服务：Linux amd64 静态构建（musl）
# --------------------------------------------
.PHONY: build_amd64_static build_amd64_static_dbg

build_amd64_static: ## Linux amd64 静态链接构建所有微服务（Release, musl）
	@mkdir -p $(BIN_DIR)
	@set -e; for svc in $(SERVICES); do \
		echo "==> [release] linux/amd64 static ($$svc)"; \
		CGO_ENABLED=1 GOOS=linux GOARCH=amd64 CC=$(MUSL_CC) \
		CGO_LDFLAGS_ALLOW='-Wl,--allow-multiple-definition' \
		$(GO) build $(GO_FLAGS_BASE) -tags '$(GO_TAGS_BASE)' \
			-ldflags "$(GO_LDFLAGS_RELEASE)" \
			-o $(BIN_DIR)/$$svc-linux-amd64 ./$(APP_DIR)/$$svc; \
		file $(BIN_DIR)/$$svc-linux-amd64 || true; \
		if command -v ldd >/dev/null 2>&1; then \
			if ! ldd $(BIN_DIR)/$$svc-linux-amd64 2>&1 | grep -q 'not a dynamic executable'; then \
				echo "✗ not static: $(BIN_DIR)/$$svc-linux-amd64"; exit 1; \
			fi; \
		fi; \
		ls -lh $(BIN_DIR)/$$svc-linux-amd64; \
	done

build_amd64_static_dbg: ## Debug: Linux amd64 静态链接构建所有微服务（musl, -N -l）
	@mkdir -p $(BIN_DIR)
	@set -e; for svc in $(SERVICES); do \
		echo "==> [debug] linux/amd64 static ($$svc)"; \
		CGO_ENABLED=1 GOOS=linux GOARCH=amd64 CC=$(MUSL_CC) \
		CGO_LDFLAGS_ALLOW='-Wl,--allow-multiple-definition' \
		$(GO) build $(GO_FLAGS_BASE) -tags '$(GO_TAGS_BASE)' \
			-gcflags '$(GO_GCFLAGS_DEBUG)' \
			-ldflags "$(GO_LDFLAGS_COMMON)" \
			-o $(BIN_DIR)/$$svc-linux-amd64.dbg ./$(APP_DIR)/$$svc; \
		file $(BIN_DIR)/$$svc-linux-amd64.dbg || true; \
		ldd  $(BIN_DIR)/$$svc-linux-amd64.dbg 2>/dev/null || true; \
		ls -lh $(BIN_DIR)/$$svc-linux-amd64.dbg; \
	done

# --------------------------------------------
# 单体：Linux amd64 静态构建（musl）
# --------------------------------------------
.PHONY: build_amd64_static_monolith build_amd64_static_monolith_dbg

build_amd64_static_monolith: ## Linux amd64 构建 legacy 单体（Release, musl）
	@echo "==> 构建 Linux x86_64 静态链接版本 ($(PROJECT_NAME)) [release]"
	@mkdir -p $(BIN_DIR)
	@CGO_ENABLED=1 GOOS=linux GOARCH=amd64 CC=$(MUSL_CC) \
	CGO_LDFLAGS_ALLOW='-Wl,--allow-multiple-definition' \
	$(GO) build $(GO_FLAGS_BASE) -tags '$(GO_TAGS_BASE)' \
		-ldflags "$(GO_LDFLAGS_RELEASE)" \
		-o $(BIN_DIR)/$(PROJECT_NAME)-linux-amd64 .
	@file $(BIN_DIR)/$(PROJECT_NAME)-linux-amd64 || true
	@if command -v ldd >/dev/null 2>&1; then \
		if ! ldd $(BIN_DIR)/$(PROJECT_NAME)-linux-amd64 2>&1 | grep -q 'not a dynamic executable'; then \
			echo "✗ not static: $(BIN_DIR)/$(PROJECT_NAME)-linux-amd64"; exit 1; \
		fi; \
	fi
	@ls -lh $(BIN_DIR)/$(PROJECT_NAME)-linux-amd64

build_amd64_static_monolith_dbg: ## Debug: Linux amd64 构建 legacy 单体（musl, -N -l）
	@echo "==> 构建 Linux x86_64 静态链接版本 ($(PROJECT_NAME)) [debug]"
	@mkdir -p $(BIN_DIR)
	@CGO_ENABLED=1 GOOS=linux GOARCH=amd64 CC=$(MUSL_CC) \
	CGO_LDFLAGS_ALLOW='-Wl,--allow-multiple-definition' \
	$(GO) build $(GO_FLAGS_BASE) -tags '$(GO_TAGS_BASE)' \
		-gcflags '$(GO_GCFLAGS_DEBUG)' \
		-ldflags "$(GO_LDFLAGS_COMMON)" \
		-o $(BIN_DIR)/$(PROJECT_NAME)-linux-amd64.dbg .
	@file $(BIN_DIR)/$(PROJECT_NAME)-linux-amd64.dbg || true
	@ldd  $(BIN_DIR)/$(PROJECT_NAME)-linux-amd64.dbg 2>/dev/null || true
	@ls -lh $(BIN_DIR)/$(PROJECT_NAME)-linux-amd64.dbg

# --------------------------------------------
# 本机动态构建（便于日常开发；非静态）
# --------------------------------------------
.PHONY: build_dynamic build_dynamic_monolith

build_dynamic: ## 本机 OS/ARCH 动态构建所有微服务（Debug, -N -l）
	@mkdir -p $(BIN_DIR)
	@set -e; \
	HOST_GOOS=$$($(GO) env GOOS); HOST_GOARCH=$$($(GO) env GOARCH); \
	for svc in $(SERVICES); do \
		echo "==> [debug] $$HOST_GOOS/$$HOST_GOARCH dynamic ($$svc)"; \
		CGO_ENABLED=1 $(GO) build -mod=readonly -buildvcs=false \
			-gcflags '$(GO_GCFLAGS_DEBUG)' \
			-tags '$(EXTRA_TAGS)' \
			-o $(BIN_DIR)/$$svc-$$HOST_GOOS-$$HOST_GOARCH ./$(APP_DIR)/$$svc; \
		ls -lh $(BIN_DIR)/$$svc-$$HOST_GOOS-$$HOST_GOARCH; \
	done

build_dynamic_monolith: ## 本机 OS/ARCH 动态构建单体（Debug, -N -l）
	@echo "==> 本机动态构建 ($(PROJECT_NAME)) [debug]"
	@mkdir -p $(BIN_DIR)
	@HOST_GOOS=$$($(GO) env GOOS); HOST_GOARCH=$$($(GO) env GOARCH); \
	CGO_ENABLED=1 $(GO) build -mod=readonly -buildvcs=false \
		-gcflags '$(GO_GCFLAGS_DEBUG)' \
		-tags '$(EXTRA_TAGS)' \
		-o $(BIN_DIR)/$(PROJECT_NAME)-$$HOST_GOOS-$$HOST_GOARCH .
	@ls -lh $(BIN_DIR)/$(PROJECT_NAME)-$$HOST_GOOS-$$HOST_GOARCH

# --------------------------------------------
# 便捷聚合目标
# --------------------------------------------
.PHONY: all release debug
all: build_amd64_static ## 等价于 release

release: build_amd64_static ## Release（静态 musl）

debug: build_amd64_static_dbg ## Debug（静态 musl）