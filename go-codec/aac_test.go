package codec

import (
	"strconv"
	"testing"
)

func TestADTSWritesCopyrightFlagsInWireOrder(t *testing.T) {
	for _, tc := range []struct {
		bit       uint8
		start     uint8
		wireFlags byte
	}{
		{bit: 1, wireFlags: 0x08},
		{start: 1, wireFlags: 0x04},
	} {
		header := NewAdtsFrameHeader()
		header.Variable_Header.Frame_length = 13
		header.Variable_Header.Copyright_identification_bit = tc.bit
		header.Variable_Header.copyright_identification_start = tc.start
		data := header.Encode()
		if got := data[3] & 0x0c; got != tc.wireFlags {
			t.Errorf("copyright flags = %#02x, want %#02x", got, tc.wireFlags)
		}
		var decoded ADTS_Frame_Header
		decoded.Decode(data)
		if decoded.Variable_Header.Copyright_identification_bit != tc.bit || decoded.Variable_Header.copyright_identification_start != tc.start {
			t.Errorf("copyright flags after round trip = %d/%d, want %d/%d",
				decoded.Variable_Header.Copyright_identification_bit, decoded.Variable_Header.copyright_identification_start, tc.bit, tc.start)
		}
	}
}

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

func TestADTSDecodesMPEGVersionBit(t *testing.T) {
	for _, tc := range []struct {
		name    string
		byteOne byte
		id      uint8
	}{
		{"MPEG-4", 0xf1, 0},
		{"MPEG-2", 0xf9, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var header ADTS_Frame_Header
			header.Decode([]byte{0xff, tc.byteOne, 0x50, 0x80, 0x01, 0xbf, 0xfc})
			if got := header.Fix_Header.ID; got != tc.id {
				t.Errorf("MPEG ID = %d, want %d", got, tc.id)
			}
		})
	}
}

func TestASCToADTSMarksUnknownBufferFullness(t *testing.T) {
	asc := &AudioSpecificConfiguration{
		Audio_object_type:     2,
		Sample_freq_index:     uint8(AAC_SAMPLE_44100),
		Channel_configuration: 2,
	}
	header, err := ConvertASCToADTS(asc.Encode(), 13)
	if err != nil {
		t.Fatal(err)
	}
	wire := header.Encode()
	if fullness := uint16(wire[5]&0x1f)<<6 | uint16(wire[6]>>2); fullness != 0x7ff {
		t.Errorf("buffer fullness = %#03x, want 0x7ff", fullness)
	}
}
