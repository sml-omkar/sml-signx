APP        := sml-signx
VERSION    ?= 0.1.0
COMMIT     := $(shell git rev-parse --short HEAD 2>/dev/null || echo unknown)
BUILD_DATE := $(shell date -u +%Y-%m-%dT%H:%M:%SZ)
DIST       := dist

LDFLAGS := -s -w \
	-X github.com/sml/sml-signx/internal/version.Version=$(VERSION) \
	-X github.com/sml/sml-signx/internal/version.Commit=$(COMMIT) \
	-X github.com/sml/sml-signx/internal/version.BuildDate=$(BUILD_DATE)

.PHONY: all build linux windows darwin test vet fmt clean

all: build

build:
	mkdir -p bin
	go build -trimpath -ldflags "$(LDFLAGS)" -o bin/$(APP) ./cmd/sml-signx

linux:
	mkdir -p $(DIST)
	GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build -trimpath -ldflags "$(LDFLAGS)" -o $(DIST)/$(APP)-linux-amd64 ./cmd/sml-signx

windows:
	mkdir -p $(DIST)
	GOOS=windows GOARCH=amd64 CGO_ENABLED=0 go build -trimpath -ldflags "$(LDFLAGS) -H=windowsgui" -o $(DIST)/$(APP)-windows-amd64.exe ./cmd/sml-signx

darwin:
	mkdir -p $(DIST)
	GOOS=darwin GOARCH=arm64 CGO_ENABLED=0 go build -trimpath -ldflags "$(LDFLAGS)" -o $(DIST)/$(APP)-darwin-arm64 ./cmd/sml-signx

test:
	go test ./...

smoke:
	./scripts/smoke-test.sh

real-test:
	./scripts/test-real-pfx.sh

vet:
	go vet ./...

fmt:
	gofmt -w .

clean:
	rm -rf bin $(DIST)
