package config

import (
	"encoding/binary"
	"math/bits"
)

// keccak256 is the original Keccak-256, the hash Monero's address checksum
// uses. It is not SHA3-256 — the two differ in one padding byte — so neither
// crypto/sha3 nor anything else in the standard library computes it, and
// golang.org/x/crypto would be the project's first dependency for these few
// lines.
func keccak256(data []byte) [32]byte {
	const rate = 136 // bytes absorbed per permutation, for a 256-bit output
	var a [25]uint64
	block := func(b []byte) {
		for i := 0; i < rate/8; i++ {
			a[i] ^= binary.LittleEndian.Uint64(b[i*8:])
		}
		keccakF1600(&a)
	}
	for len(data) >= rate {
		block(data[:rate])
		data = data[rate:]
	}
	var last [rate]byte
	copy(last[:], data)
	last[len(data)] ^= 0x01 // Keccak's padding; SHA-3 would put 0x06 here
	last[rate-1] ^= 0x80
	block(last[:])

	var out [32]byte
	for i := 0; i < 4; i++ {
		binary.LittleEndian.PutUint64(out[i*8:], a[i])
	}
	return out
}

var keccakRC = [24]uint64{
	0x0000000000000001, 0x0000000000008082, 0x800000000000808A, 0x8000000080008000,
	0x000000000000808B, 0x0000000080000001, 0x8000000080008081, 0x8000000000008009,
	0x000000000000008A, 0x0000000000000088, 0x0000000080008009, 0x000000008000000A,
	0x000000008000808B, 0x800000000000008B, 0x8000000000008089, 0x8000000000008003,
	0x8000000000008002, 0x8000000000000080, 0x000000000000800A, 0x800000008000000A,
	0x8000000080008081, 0x8000000000008080, 0x0000000080000001, 0x8000000080008008,
}

// keccakRotc and keccakPiln are the rho rotations and the pi lane order, in
// the order the combined rho-pi step visits the lanes.
var (
	keccakRotc = [24]int{1, 3, 6, 10, 15, 21, 28, 36, 45, 55, 2, 14, 27, 41, 56, 8, 25, 43, 62, 18, 39, 61, 20, 44}
	keccakPiln = [24]int{10, 7, 11, 17, 18, 3, 5, 16, 8, 21, 24, 4, 15, 23, 19, 13, 12, 2, 20, 14, 22, 9, 6, 1}
)

func keccakF1600(a *[25]uint64) {
	var c [5]uint64
	for round := 0; round < 24; round++ {
		// theta
		for x := 0; x < 5; x++ {
			c[x] = a[x] ^ a[x+5] ^ a[x+10] ^ a[x+15] ^ a[x+20]
		}
		for x := 0; x < 5; x++ {
			d := c[(x+4)%5] ^ bits.RotateLeft64(c[(x+1)%5], 1)
			for y := 0; y < 25; y += 5 {
				a[y+x] ^= d
			}
		}
		// rho and pi
		t := a[1]
		for i := 0; i < 24; i++ {
			j := keccakPiln[i]
			t, a[j] = a[j], bits.RotateLeft64(t, keccakRotc[i])
		}
		// chi
		for y := 0; y < 25; y += 5 {
			copy(c[:], a[y:y+5])
			for x := 0; x < 5; x++ {
				a[y+x] = c[x] ^ (^c[(x+1)%5] & c[(x+2)%5])
			}
		}
		// iota
		a[0] ^= keccakRC[round]
	}
}
