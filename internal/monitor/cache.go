package monitor

// L3CacheBytes reports the machine's last-level cache size in bytes, and
// whether it could be determined.
//
// This exists for one caller: choosing how many mining threads to launch.
// RandomX gives every thread a 2 MiB scratchpad it wants to keep resident in
// L3, so the last-level cache — not the core count — is what bounds how many
// threads can hash productively. Past that point threads evict each other's
// scratchpads and the machine hashes *less* in total than it would with fewer.
//
// ok is false where the size cannot be read. Callers must fall back to a core
// count in that case rather than assume a size: guessing low would silently
// cap the miner far below what the machine can do, which is indistinguishable
// from the miner simply being slow.
func L3CacheBytes() (int64, bool) { return readL3CacheBytes() }
