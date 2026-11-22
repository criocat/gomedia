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

func TestADTSBufferFullnessEncoding(t *testing.T) {
	for _, tc := range []struct {
		length   uint16
		fullness uint16
	}{
		{length: 128, fullness: 0x7ff},
		{length: 131, fullness: 0x2ad},
		{length: 8191, fullness: 0x040},
	} {
		header := NewAdtsFrameHeader()
		header.Variable_Header.Frame_length = tc.length
		header.Variable_Header.Adts_buffer_fullness = tc.fullness
		header.Variable_Header.Number_of_raw_data_blocks_in_frame = 2
		var decoded ADTS_Frame_Header
		decoded.Decode(header.Encode())
		got := decoded.Variable_Header
		if got.Frame_length != tc.length || got.Adts_buffer_fullness != tc.fullness || got.Number_of_raw_data_blocks_in_frame != 2 {
			t.Errorf("decoded length=%d fullness=%#03x block field=%d, want %d/%#03x/2",
				got.Frame_length, got.Adts_buffer_fullness, got.Number_of_raw_data_blocks_in_frame, tc.length, tc.fullness)
		}
	}
}
