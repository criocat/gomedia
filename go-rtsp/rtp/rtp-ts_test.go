package rtp

import (
	"bytes"
	"errors"
	"io"
	"reflect"
	"testing"
)

func TestTSPackerKeepsPacketsAlignedAcrossRTPWrites(t *testing.T) {
	// Four MPEG-TS null packets with distinct continuity counters.
	stream := bytes.Repeat([]byte{0xff}, 4*188)
	for i := 0; i < 4; i++ {
		copy(stream[i*188:], []byte{0x47, 0x1f, 0xff, byte(0x10 + i)})
	}
	for _, mtu := range []int{388, 400} {
		packer := NewTsPacker(33, 1, 41, mtu)
		var payload []byte
		var sizes []int
		var seq []uint16
		packer.OnPacket(func(wire []byte) error {
			if len(wire) > mtu {
				t.Errorf("RTP packet length = %d, MTU = %d", len(wire), mtu)
			}
			var packet RtpPacket
			if err := packet.Decode(wire); err != nil {
				return err
			}
			payload = append(payload, packet.Payload...)
			sizes = append(sizes, len(packet.Payload))
			seq = append(seq, packet.Header.SequenceNumber)
			return nil
		})
		for _, ts := range []uint32{9000, 18000} {
			if err := packer.Pack(stream, ts); err != nil {
				t.Fatal(err)
			}
		}
		if !bytes.Equal(payload, append(append([]byte(nil), stream...), stream...)) {
			t.Errorf("MTU %d: received %d TS bytes, want %d with matching contents", mtu, len(payload), 2*len(stream))
		}
		if want := []int{376, 376, 376, 376}; !reflect.DeepEqual(sizes, want) {
			t.Errorf("MTU %d: payload lengths = %v, want %v", mtu, sizes, want)
		}
		if want := []uint16{41, 42, 43, 44}; !reflect.DeepEqual(seq, want) {
			t.Errorf("MTU %d: sequence numbers = %v, want %v", mtu, seq, want)
		}
	}

	packer := NewTsPacker(33, 1, 41, 400)
	calls := 0
	packer.OnPacket(func([]byte) error {
		calls++
		return io.ErrClosedPipe
	})
	if err := packer.Pack(stream, 9000); !errors.Is(err, io.ErrClosedPipe) || calls != 1 {
		t.Errorf("Pack returned %v after %d writes, want closed pipe after one", err, calls)
	}
}
