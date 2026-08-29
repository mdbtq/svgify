BINARY  := trace
PKG     := ./cmd/trace
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
LDFLAGS := -s -w -X main.version=$(VERSION)

# Static, dependency-free binaries: the tracer is pure Go, so cgo is never needed.
export CGO_ENABLED = 0

PLATFORMS := darwin/arm64 darwin/amd64 linux/amd64 linux/arm64 windows/amd64

.PHONY: build test install clean fmt vet lint dist

build:
	go build -ldflags '$(LDFLAGS)' -o bin/$(BINARY) $(PKG)

test:
	go test ./...

install:
	go install -ldflags '$(LDFLAGS)' $(PKG)

fmt:
	gofmt -l -w .

vet:
	go vet ./...

lint: vet
	@test -z "$$(gofmt -l . | grep -v '^vendor/')" || { echo 'gofmt needed:'; gofmt -l . | grep -v '^vendor/'; exit 1; }

# Cross-compile release binaries for every supported target.
dist:
	@rm -rf dist && mkdir -p dist
	@for p in $(PLATFORMS); do \
		os=$${p%/*}; arch=$${p#*/}; ext=''; \
		[ "$$os" = windows ] && ext='.exe'; \
		echo "building $$os/$$arch"; \
		GOOS=$$os GOARCH=$$arch go build -ldflags '$(LDFLAGS)' \
			-o dist/$(BINARY)-$$os-$$arch$$ext $(PKG) || exit 1; \
	done
	@ls -lh dist

clean:
	rm -rf bin dist
