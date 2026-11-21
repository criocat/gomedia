package codec

import (
	"strconv"
	"testing"
)

func TestADTSFrameLengthEncoding(t *testing.T) {
	for _, length := range []uint16{7, 2047, 2048, 4095, 4096, 6143, 6144, 8191} {
		t.Run(strconv.Itoa(int(length)), func(t *testing.T) {
			header := NewAdtsFrameHeader()
			header.Fix_Header.Channel_configuration = 2
			header.Variable_Header.Frame_length = length

			wire := header.Encode()
			if len(wire) != 7 {
				t.Fatalf("header size = %d, want 7", len(wire))
			}
			if wire[3]&0xfc != 0x80 {
				t.Fatalf("adjacent fields changed: header byte 3 = %#02x", wire[3])
			}
			decoded := NewAdtsFrameHeader()
			decoded.Decode(wire)
			if got := decoded.Variable_Header.Frame_length; got != length {
				t.Fatalf("decoded frame length = %d, want %d", got, length)
			}
		})
	}
}
