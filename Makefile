BINARY    := singbox-exporter
VERSION   ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
LDFLAGS   := -s -w -X main.version=$(VERSION)
PLATFORMS := linux/amd64 linux/arm64 linux/386 darwin/amd64 darwin/arm64

.PHONY: all build test vet fmt lint release docker clean

all: vet test build

build:
	go build -trimpath -ldflags "$(LDFLAGS)" -o $(BINARY) .

test:
	go test ./...

vet:
	go vet ./...

fmt:
	gofmt -l -w .

lint:
	golangci-lint run

release: clean
	@mkdir -p dist
	@for p in $(PLATFORMS); do \
		os=$${p%/*}; arch=$${p#*/}; \
		name=$(BINARY)-$${os}-$${arch}; \
		echo "building dist/$$name"; \
		CGO_ENABLED=0 GOOS=$${os} GOARCH=$${arch} \
			go build -trimpath -ldflags "$(LDFLAGS)" -o dist/$$name .; \
	done

docker:
	docker build -t $(BINARY):$(VERSION) .

clean:
	rm -rf $(BINARY) dist