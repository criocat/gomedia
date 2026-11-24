package ogg

import (
	"math/rand"
	"testing"
)

func referenceOggChecksum(crc uint32, data []byte) uint32 {
	for _, value := range data {
		crc ^= uint32(value) << 24
		for bit := 0; bit < 8; bit++ {
			if crc&0x80000000 != 0 {
				crc = (crc << 1) ^ 0x04c11db7
			} else {
				crc <<= 1
			}
		}
	}
	return crc
}

func TestOggChecksumMatchesReference(t *testing.T) {
	const maxPageSize = 27 + 255 + 255*255
	data := make([]byte, maxPageSize+1)
	_, _ = rand.New(rand.NewSource(1)).Read(data)
	for _, size := range []int{0, 1, 7, 8, 9, 15, 16, 17, 27, 255, 4095, 4096, 4097, maxPageSize} {
		// Use an unaligned slice to exercise the same path as an Ogg payload.
		payload := data[1 : size+1]
		for _, seed := range []uint32{0, 0xdeadbeef} {
			want := referenceOggChecksum(seed, payload)
			if got := makeChecksum(seed, payload); got != want {
				t.Fatalf("size %d, seed %08x: CRC %08x, want %08x", size, seed, got, want)
			}
			for split := 0; split < 8 && split <= size; split++ {
				got := makeChecksum(makeChecksum(seed, payload[:split]), payload[split:])
				if got != want {
					t.Fatalf("size %d, seed %08x, split %d: CRC %08x, want %08x", size, seed, split, got, want)
				}
			}
		}
	}
}
