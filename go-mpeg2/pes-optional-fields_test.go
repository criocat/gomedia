package mpeg2

import (
	"bytes"
	"testing"

	"github.com/yapingcat/gomedia/go-codec"
)

// MPEG-2 PES with ES rate, DSM slow motion, copy info and previous CRC.
var pesOptionalFieldsWire = []byte{
	0, 0, 1, 0xe0, 0, 12, 0x80, 0x1e, 7,
	0x81, 0x90, 0x45, 0x24, 0xac, 0xbe, 0xef,
	0x31, 0x42,
}

func TestPESDecodesCopyInfoAndPreviousCRC(t *testing.T) {
	pes := NewPesPacket()
	if err := pes.Decode(codec.NewBitStream(pesOptionalFieldsWire)); err != nil {
		t.Fatal(err)
	}
	if pes.Additional_copy_info != 0x2c || pes.Previous_PES_packet_CRC != 0xbeef {
		t.Errorf("copy info = %#x, CRC = %#x, want 0x2c and 0xbeef",
			pes.Additional_copy_info, pes.Previous_PES_packet_CRC)
	}
	if pes.ES_rate != 51234 || pes.Trick_mode_control != 1 || pes.Trick_value != 4 {
		t.Errorf("ES rate = %d, trick mode = %d/%d, want 51234 and 1/4",
			pes.ES_rate, pes.Trick_mode_control, pes.Trick_value)
	}
	if got, want := pes.Pes_payload, []byte{0x31, 0x42}; !bytes.Equal(got, want) {
		t.Errorf("payload = %x, want %x", got, want)
	}
}

func TestPESEncodesDeclaredOptionalFields(t *testing.T) {
	pes := &PesPacket{
		Stream_id:                 0xe0,
		PES_packet_length:         12,
		PES_header_data_length:    7,
		ES_rate_flag:              1,
		ES_rate:                   51234,
		DSM_trick_mode_flag:       1,
		Trick_mode_control:        1,
		Trick_value:               4,
		Additional_copy_info_flag: 1,
		Additional_copy_info:      0x2c,
		PES_CRC_flag:              1,
		Previous_PES_packet_CRC:   0xbeef,
		Pes_payload:               []byte{0x31, 0x42},
	}
	output := codec.NewBitStreamWriter(32)
	pes.Encode(output)
	if got := output.Bits(); !bytes.Equal(got, pesOptionalFieldsWire) {
		t.Errorf("encoded PES = %x, want %x", got, pesOptionalFieldsWire)
	}
}
