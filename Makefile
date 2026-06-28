BINARY   := kind-miner
MODULE   := github.com/kind-miner/kind-miner
VERSION  ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)

# Commit time — a deterministic clock for any timestamped packaging step.
SOURCE_DATE_EPOCH ?= $(shell git log -1 --format=%ct 2>/dev/null || echo 0)
export SOURCE_DATE_EPOCH

# Reproducible build flags — see REPRODUCIBLE.md.
#   -trimpath        strip local filesystem paths from the binary
#   -buildvcs=false  don't embed git state (the dirty flag is nondeterministic)
#   -buildid=        drop the nondeterministic build id
GO_LDFLAGS     := -s -w -buildid= -X main.version=$(VERSION)
GO_BUILD_FLAGS := -trimpath -buildvcs=false -ldflags "$(GO_LDFLAGS)"

DIST := dist

# Deterministic archive flags (GNU tar): sorted member names, fixed mtime, uid/
# gid 0, and no volatile pax atime/ctime headers. Always pipe the result through
# `gzip -n` so the gzip header carries no filename/mtime. Without these, the tar
# stream embeds the build-time mtimes and the archive is not reproducible even
# when the files inside it are. See REPRODUCIBLE.md / reproducible-builds.org.
TAR_REPRO := --sort=name --mtime=@$(SOURCE_DATE_EPOCH) --owner=0 --group=0 --numeric-owner \
             --pax-option=exthdr.name=%d/PaxHeaders/%f,delete=atime,delete=ctime

.PHONY: all deps build build-all reproduce verify-repro test clean \
        download-xmrig download-p2pool \
        bundle-linux bundle-darwin bundle-windows appimage flatpak vendor \
        vendor-tarball

all: deps build

deps:
	go mod tidy

build:
	go build $(GO_BUILD_FLAGS) -o $(BINARY) ./cmd/kind-miner

# Canonical reproducible build: pinned stock toolchain + hermetic environment.
# Produces a binary anyone can rebuild bit-for-bit from source.
reproduce:
	bash scripts/reproduce.sh $(VERSION) $(BINARY)

# Prove determinism on this host: build twice with the local toolchain (offline)
# and compare SHA256. A necessary condition for cross-host reproducibility.
verify-repro:
	@GOTOOLCHAIN=local bash scripts/reproduce.sh $(VERSION) build/repro-a/$(BINARY) >/dev/null
	@GOTOOLCHAIN=local bash scripts/reproduce.sh $(VERSION) build/repro-b/$(BINARY) >/dev/null
	@a=$$(sha256sum build/repro-a/$(BINARY) | cut -d' ' -f1); \
	 b=$$(sha256sum build/repro-b/$(BINARY) | cut -d' ' -f1); \
	 echo "build A: $$a"; \
	 echo "build B: $$b"; \
	 if [ "$$a" = "$$b" ]; then echo "✓ reproducible — identical bytes"; \
	 else echo "✗ NOT reproducible — builds differ"; exit 1; fi

# NOTE: the GUI uses cgo (Fyne/OpenGL), so build-all cannot cross-compile the
# graphical binary from a single host. Run it on a native runner per OS, or use
# fyne-cross. Headless server builds can still cross-compile with CGO_ENABLED=0.
build-all:
	GOOS=linux   GOARCH=amd64  go build $(GO_BUILD_FLAGS) -o $(DIST)/linux-amd64/$(BINARY)        ./cmd/kind-miner
	GOOS=linux   GOARCH=arm64  go build $(GO_BUILD_FLAGS) -o $(DIST)/linux-arm64/$(BINARY)        ./cmd/kind-miner
	GOOS=darwin  GOARCH=amd64  go build $(GO_BUILD_FLAGS) -o $(DIST)/darwin-amd64/$(BINARY)       ./cmd/kind-miner
	GOOS=darwin  GOARCH=arm64  go build $(GO_BUILD_FLAGS) -o $(DIST)/darwin-arm64/$(BINARY)       ./cmd/kind-miner
	GOOS=windows GOARCH=amd64  go build $(GO_BUILD_FLAGS) -o $(DIST)/windows-amd64/$(BINARY).exe  ./cmd/kind-miner

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
	tar $(TAR_REPRO) -cf - -C $(DIST)/linux-amd64 . | gzip -9 -n > $(DIST)/kind-miner-$(VERSION)-linux-amd64.tar.gz

bundle-darwin:
	for arch in amd64 arm64; do \
	  mkdir -p $(DIST)/darwin-$$arch/bin; \
	  cp config.example.yaml $(DIST)/darwin-$$arch/; \
	  tar $(TAR_REPRO) -cf - -C $(DIST)/darwin-$$arch . | gzip -9 -n > $(DIST)/kind-miner-$(VERSION)-darwin-$$arch.tar.gz; \
	done

bundle-windows: $(DIST)/windows-amd64/$(BINARY).exe
	mkdir -p $(DIST)/windows-amd64/bin
	cp config.example.yaml $(DIST)/windows-amd64/
	# Deterministic zip: pin every member mtime, feed a sorted file list, and
	# drop extra attributes (-X) and directory entries (-D) so the archive is
	# byte-identical across builds.
	cd $(DIST) && find windows-amd64 -type f -exec touch -d @$(SOURCE_DATE_EPOCH) {} + && \
	  find windows-amd64 -type f | LC_ALL=C sort | zip -X -D -q kind-miner-$(VERSION)-windows-amd64.zip -@

appimage:
	bash scripts/build-appimage.sh $(VERSION)

# Single-file Flatpak bundle from the working tree (dist/*.flatpak). Shareable;
# install with `flatpak install --user dist/kind-miner-*.flatpak`.
flatpak:
	bash scripts/build-flatpak.sh $(VERSION)

vendor:
	go mod vendor

# Reproducible vendored source tarball for the Flathub manifest
# (flatpak/flathub/). Upload the output as a release asset and pin its sha256.
vendor-tarball:
	bash scripts/vendor-tarball.sh $(VERSION)

clean:
	rm -rf $(DIST) $(BINARY) build/ AppDir/
