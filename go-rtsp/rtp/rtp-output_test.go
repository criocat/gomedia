package rtp

import (
	"bytes"
	"errors"
	"io"
	"reflect"
	"testing"
)

func TestHEVCPackerStopsOnOutputError(t *testing.T) {
	startCode := []byte{0, 0, 0, 1}
	// An AUD followed by a NAL large enough to fragment at this MTU.
	aud := append(append([]byte(nil), startCode...), 0x46, 0x01, 0x10)
	largeNAL := append(append([]byte(nil), startCode...), 0x26, 0x01)
	largeNAL = append(largeNAL, bytes.Repeat([]byte{0x55}, 180)...)
	for _, tc := range []struct {
		name      string
		data      []byte
		failAfter int
	}{
		{name: "standalone NAL", data: append(aud, largeNAL...), failAfter: 1},
		{name: "first fragment", data: largeNAL, failAfter: 1},
		{name: "second fragment", data: largeNAL, failAfter: 2},
	} {
		t.Run(tc.name, func(t *testing.T) {
			packer := NewH265Packer(97, 1, 41, 64)
			calls := 0
			packer.OnPacket(func([]byte) error {
				calls++
				if calls == tc.failAfter {
					return io.ErrClosedPipe
				}
				return nil
			})
			if err := packer.Pack(tc.data, 9000); !errors.Is(err, io.ErrClosedPipe) || calls != tc.failAfter {
				t.Errorf("Pack returned %v after %d writes, want closed pipe after %d", err, calls, tc.failAfter)
			}
		})
	}
}

func TestG711PackerIncrementsSequenceWithOutputCallback(t *testing.T) {
	packer := NewG711Packer(0, 1, 65534, 1400)
	var got []uint16
	packer.OnPacket(func(data []byte) error {
		var packet RtpPacket
		if err := packet.Decode(data); err != nil {
			return err
		}
		got = append(got, packet.Header.SequenceNumber)
		return nil
	})
	silence := bytes.Repeat([]byte{0xff}, 160)
	for _, timestamp := range []uint32{0, 160, 320} {
		if err := packer.Pack(silence, timestamp); err != nil {
			t.Fatal(err)
		}
	}
	want := []uint16{65534, 65535, 0}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("RTP sequence numbers = %v, want %v", got, want)
	}
}
