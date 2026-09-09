BINARY  := trace
PKG     := ./cmd/trace
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
LDFLAGS := -s -w -X main.version=$(VERSION)

# Static, dependency-free binaries: the tracer is pure Go, so cgo is never needed.
export CGO_ENABLED = 0

PLATFORMS := darwin/arm64 darwin/amd64 linux/amd64 linux/arm64 windows/amd64

.DEFAULT_GOAL := help

.PHONY: help
help: ## Show this help
	@awk 'BEGIN {FS = ":.*?## "} \
		/^##@ / {printf "  \033[1m%s\033[0m\n", substr($$0, 5); next} \
		/^[a-zA-Z0-9_\/.-]+:.*?## / {printf "  \033[36m%-16s\033[0m %s\n", $$1, $$2}' \
		$(MAKEFILE_LIST)

##@ Build

.PHONY: build/binary
build/binary: ## Build the binary into bin/
	go build -ldflags '$(LDFLAGS)' -o bin/$(BINARY) $(PKG)

.PHONY: build/install
build/install: ## Install the binary with go install
	go install -ldflags '$(LDFLAGS)' $(PKG)

.PHONY: build/dist
build/dist: ## Cross-compile release binaries for every supported target
	@rm -rf dist && mkdir -p dist
	@for p in $(PLATFORMS); do \
		os=$${p%/*}; arch=$${p#*/}; ext=''; \
		[ "$$os" = windows ] && ext='.exe'; \
		echo "building $$os/$$arch"; \
		GOOS=$$os GOARCH=$$arch go build -ldflags '$(LDFLAGS)' \
			-o dist/$(BINARY)-$$os-$$arch$$ext $(PKG) || exit 1; \
	done
	@ls -lh dist

.PHONY: build/clean
build/clean: ## Remove build artefacts
	rm -rf bin dist

##@ Test

.PHONY: test/all
test/all: ## Run the test suite
	go test ./...

##@ Lint

.PHONY: lint/fmt
lint/fmt: ## Format the source
	gofmt -l -w .

.PHONY: lint/vet
lint/vet: ## Run go vet
	go vet ./...

.PHONY: lint/check
lint/check: lint/vet ## Run vet and check formatting without changing files
	@test -z "$$(gofmt -l . | grep -v '^vendor/')" || { echo 'gofmt needed:'; gofmt -l . | grep -v '^vendor/'; exit 1; }

# Deprecated aliases, kept so existing habits and scripts keep working.
# They carry no ## comment, so `make help` lists only the names above.
.PHONY: build install dist clean fmt vet lint
build: build/binary
install: build/install
dist: build/dist
clean: build/clean
fmt: lint/fmt
vet: lint/vet
lint: lint/check
