# Go parameters
GOCMD := go
GOBUILD := $(GOCMD) build
GOCLEAN := $(GOCMD) clean
GOTEST := $(GOCMD) test
GOGET := $(GOCMD) get
GOMOD := $(GOCMD) mod
GOFUMPT := gofumpt
GOLINT := golangci-lint
PROTOC := protoc

# Project parameters
BIN_DIR := bin
CMD_DIRS := $(wildcard cmd/*)
BINARIES := $(patsubst cmd/%,$(BIN_DIR)/%,$(CMD_DIRS))
PKG_LIST := $(shell $(GOCMD) list ./... | grep -v /vendor/)

# Proto parameters
PROTO_DIR := proto
PROTO_OUT := pb
PROTO_FILES := $(shell find $(PROTO_DIR) -name '*.proto')

# Build flags
VERSION := $(shell git describe --tags --always --dirty)
BUILD_TIME := $(shell date -u '+%Y-%m-%d %H:%M:%S')

# Debug configuration
DEBUG ?= false
ifeq ($(DEBUG),true)
    LDFLAGS := -X 'main.Version=$(VERSION)' -X 'main.BuildTime=$(BUILD_TIME)'
    GCFLAGS := -gcflags="all=-N -l"
else
    LDFLAGS := -w -s -X 'main.Version=$(VERSION)' -X 'main.BuildTime=$(BUILD_TIME)'
    GCFLAGS :=
endif

CGO_FLAGS := CGO_ENABLED=1

# Colors for pretty printing
GREEN := \033[0;32m
BLUE := \033[0;34m
YELLOW := \033[0;33m
NC := \033[0m # No Color

# Targets
.PHONY: all build compile debug test fumpt lint tidy clean setup-hooks release run help proto proto-clean

# Default target
all: build

# Full build pipeline
build: clean proto tidy fumpt lint binaries
	@printf "$(GREEN)✓ Build completed successfully!$(NC)\n"

# Quick build without linting
compile: proto tidy binaries
	@printf "$(GREEN)✓ Compile completed!$(NC)\n"

# Debug build target
debug: proto
	@printf "$(YELLOW)Building in DEBUG mode...$(NC)\n"
	@$(MAKE) DEBUG=true binaries

# Build all binaries
binaries: $(BINARIES)

# Pattern rule: build each cmd/<name> into bin/<name>
$(BIN_DIR)/%: $(shell find . -name '*.go' -not -path './vendor/*')
	@printf "$(BLUE)Building $* [DEBUG=$(DEBUG)]...$(NC)\n"
	@mkdir -p $(BIN_DIR)
	@$(CGO_FLAGS) $(GOBUILD) $(GCFLAGS) -ldflags "$(LDFLAGS)" -o $@ ./cmd/$*

# Run a specific service: make run CMD=gateway
run: $(BIN_DIR)/$(CMD)
	@printf "$(BLUE)Running $(CMD)...$(NC)\n"
	@./$(BIN_DIR)/$(CMD)

test:
	@printf "$(BLUE)Running tests ...$(NC)\n"
	@$(GOTEST) -v $(PKG_LIST)

fumpt:
	@printf "$(BLUE)Running fumpt ...$(NC)\n"
	@$(GOFUMPT) -w -l $(shell find . -name '*.go')

lint:
	@printf "$(BLUE)Running linter ...$(NC)\n"
	@$(GOLINT) run ./...

tidy:
	@printf "$(BLUE)Tidying and verifying module dependencies ...$(NC)\n"
	@$(GOMOD) tidy
	@$(GOMOD) verify

# Generate protobuf Go files
proto:
ifneq ($(PROTO_FILES),)
	@printf "$(BLUE)Generating protobuf files ...$(NC)\n"
	@mkdir -p $(PROTO_OUT)
	@$(PROTOC) --go_out=$(PROTO_OUT) --go_opt=paths=source_relative \
		--go-grpc_out=$(PROTO_OUT) --go-grpc_opt=paths=source_relative \
		-I$(PROTO_DIR) $(PROTO_FILES)
	@printf "$(GREEN)✓ Protobuf generation completed!$(NC)\n"
else
	@printf "$(YELLOW)No proto files found in $(PROTO_DIR)/, skipping protobuf generation.$(NC)\n"
endif

# Clean generated protobuf files
proto-clean:
	@printf "$(BLUE)Cleaning generated protobuf files ...$(NC)\n"
	@rm -f $(PROTO_OUT)/*.pb.go

clean: proto-clean
	@printf "$(BLUE)Cleaning up ...$(NC)\n"
	@$(GOCLEAN)
	@rm -rf $(BIN_DIR)/* *.pid *.perf

help:
	@echo "$(BLUE)Available targets:$(NC)"
	@echo "  $(GREEN)all (build)$(NC)  : Full build pipeline (clean + proto + tidy + fumpt + lint + compile)"
	@echo "  $(GREEN)compile$(NC)      : Quick build without linting (proto + tidy + compile only)"
	@echo "  $(GREEN)debug$(NC)        : Build with debug symbols (no optimizations)"
	@echo "  $(GREEN)run$(NC)          : Run a specific service (make run CMD=gateway)"
	@echo "  $(GREEN)test$(NC)         : Run all tests"
	@echo "  $(GREEN)fumpt$(NC)        : Format code with gofumpt"
	@echo "  $(GREEN)lint$(NC)         : Run golangci-lint for code quality checks"
	@echo "  $(GREEN)tidy$(NC)         : Tidy and verify go modules"
	@echo "  $(GREEN)proto$(NC)        : Generate Go code from proto files (api/*.proto → internal/pb/)"
	@echo "  $(GREEN)proto-clean$(NC)  : Remove generated protobuf files"
	@echo "  $(GREEN)clean$(NC)        : Remove binaries and clean build cache"
	@echo "  $(GREEN)help$(NC)         : Display this help message"
	@echo ""
	@echo "$(BLUE)Build modes:$(NC)"
	@echo "  Release (default): Optimized + stripped symbols → smaller binary"
	@echo "  Debug mode:        Full debug info + no optimizations → for debugging"
	@echo ""
	@echo "$(BLUE)Examples:$(NC)"
	@echo "  make              # Full build in release mode"
	@echo "  make debug        # Build in debug mode"
	@echo "  make compile      # Quick compile without linting"
	@echo "  make run CMD=gateway  # Build and run gateway"
	@echo "  make DEBUG=true   # Build with debug flag"

# Debugging
print-%:
	@echo '$*=$($*)'
