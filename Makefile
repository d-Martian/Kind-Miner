BINARY   := kind-miner
MODULE   := github.com/kind-miner/kind-miner
VERSION  := $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
LDFLAGS  := -ldflags "-X main.version=$(VERSION) -s -w"

DIST := dist

.PHONY: all deps build build-all test clean bundle-linux bundle-darwin bundle-windows appimage vendor

all: deps build

deps:
	go mod tidy

build:
	go build $(LDFLAGS) -o $(BINARY) ./cmd/kind-miner

build-all:
	GOOS=linux   GOARCH=amd64  go build $(LDFLAGS) -o $(DIST)/linux-amd64/$(BINARY)        ./cmd/kind-miner
	GOOS=linux   GOARCH=arm64  go build $(LDFLAGS) -o $(DIST)/linux-arm64/$(BINARY)        ./cmd/kind-miner
	GOOS=darwin  GOARCH=amd64  go build $(LDFLAGS) -o $(DIST)/darwin-amd64/$(BINARY)       ./cmd/kind-miner
	GOOS=darwin  GOARCH=arm64  go build $(LDFLAGS) -o $(DIST)/darwin-arm64/$(BINARY)       ./cmd/kind-miner
	GOOS=windows GOARCH=amd64  go build $(LDFLAGS) -o $(DIST)/windows-amd64/$(BINARY).exe  ./cmd/kind-miner

test:
	go test ./...

# Download XMRig binaries into dist/<platform>/bin/
download-xmrig:
	bash scripts/download-xmrig.sh

# Download p2pool binaries into dist/<platform>/bin/
download-p2pool:
	bash scripts/download-p2pool.sh

# Build release archives for all platforms (requires build-all + downloads)
bundle-linux: $(DIST)/linux-amd64/$(BINARY)
	mkdir -p $(DIST)/linux-amd64/bin
	cp -n $(DIST)/linux-amd64/bin/xmrig $(DIST)/linux-amd64/bin/ 2>/dev/null || true
	cp -n $(DIST)/linux-amd64/bin/p2pool $(DIST)/linux-amd64/bin/ 2>/dev/null || true
	cp config.example.yaml $(DIST)/linux-amd64/
	tar -czf $(DIST)/kind-miner-$(VERSION)-linux-amd64.tar.gz -C $(DIST)/linux-amd64 .

bundle-darwin:
	for arch in amd64 arm64; do \
	  mkdir -p $(DIST)/darwin-$$arch/bin; \
	  cp config.example.yaml $(DIST)/darwin-$$arch/; \
	  tar -czf $(DIST)/kind-miner-$(VERSION)-darwin-$$arch.tar.gz -C $(DIST)/darwin-$$arch .; \
	done

bundle-windows: $(DIST)/windows-amd64/$(BINARY).exe
	mkdir -p $(DIST)/windows-amd64/bin
	cp config.example.yaml $(DIST)/windows-amd64/
	cd $(DIST) && zip -r kind-miner-$(VERSION)-windows-amd64.zip windows-amd64/

appimage:
	bash scripts/build-appimage.sh $(VERSION)

vendor:
	go mod vendor

clean:
	rm -rf $(DIST) $(BINARY) build/ AppDir/
