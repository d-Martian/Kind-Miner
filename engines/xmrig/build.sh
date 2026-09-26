#!/bin/bash
# Runs inside the toolchain container with no network:
#   /src      pinned source archives (read-only)
#   /patches  the patches to apply, in order (read-only)
#   /out      where the binary is written
# SOURCE_DATE_EPOCH comes from sources.lock via scripts/build-xmrig.sh.
set -euo pipefail

: "${SOURCE_DATE_EPOCH:?set by scripts/build-xmrig.sh}"
export LC_ALL=C TZ=UTC
umask 022

# Every build happens under the same path, and -ffile-prefix-map erases it
# anyway, so no path from this container ends up in the binary.
B=/build
DEPS=$B/deps
FLAGS="-O2 -ffile-prefix-map=$B=. -Wdate-time"
export CFLAGS="$FLAGS" CXXFLAGS="$FLAGS"
# No build-id: it is a hash of the link inputs and adds nothing a SHA256 of
# the file does not already give.
export LDFLAGS="-Wl,--build-id=none"
JOBS=$(nproc)

mkdir -p $B $DEPS/include $DEPS/lib
cd $B
for a in /src/*.tar.gz; do tar -xzf "$a" --no-same-owner; done

# Patches first: they take a second, and a patch that no longer applies after
# a version bump should fail the build before minutes of dependency compiling.
(
  cd xmrig-*
  for p in /patches/*.patch; do
    echo "applying $(basename "$p")"
    patch -p1 < "$p"
  done
)

# The dependency builds follow xmrig's scripts/build.{uv,hwloc,openssl3}.sh,
# minus their downloads.
(
  cd libuv-v*
  sh autogen.sh
  ./configure --disable-shared
  make -j"$JOBS"
  cp -r include $DEPS/ && cp .libs/libuv.a $DEPS/lib/
)
(
  cd hwloc-*
  ./configure --disable-shared --enable-static --disable-io --disable-libudev --disable-libxml2
  make -j"$JOBS"
  cp -r include $DEPS/ && cp hwloc/.libs/libhwloc.a $DEPS/lib/
)
(
  cd openssl-*
  ./config -no-shared -no-asm -no-zlib -no-comp -no-dgram -no-filenames -no-cms
  make -j"$JOBS"
  cp -r include $DEPS/ && cp libcrypto.a libssl.a $DEPS/lib/
)

cd $B/xmrig-*

# GPU backends off: kind-miner mines on the CPU only, and OpenCL/CUDA loaders
# are code and library lookups it has no use for. Everything else stays at
# upstream's defaults.
mkdir build && cd build
cmake .. -DXMRIG_DEPS=$DEPS -DBUILD_STATIC=ON \
  -DWITH_OPENCL=OFF -DWITH_CUDA=OFF \
  -DCMAKE_BUILD_TYPE=Release
make -j"$JOBS"
strip xmrig

install -m 0755 xmrig /out/xmrig
