package mpeg2

import (
	"bytes"
	"testing"

	"github.com/yapingcat/gomedia/go-codec"
)

func TestPESDecodesUnboundedVideoPayload(t *testing.T) {
	frame := tsVideoFixtures[0].frames(t)[0]
	wire := append([]byte{0, 0, 1, 0xe0, 0, 0, 0x80, 0, 0}, frame...)
	input := codec.NewBitStream(wire)
	pes := NewPesPacket()
	if err := pes.Decode(input); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(pes.Pes_payload, frame) || !input.EOS() {
		t.Errorf("payload = %x, final byte offset = %d, want the complete payload and offset %d",
			pes.Pes_payload, input.ByteOffset(), len(wire))
	}
}

func TestPESEncodesStuffingBeforePayload(t *testing.T) {
	payload := []byte{0x31, 0x42}
	pes := &PesPacket{
		Stream_id:              0xe0,
		PES_packet_length:      7,
		PES_header_data_length: 2,
		Pes_payload:            payload,
	}
	output := codec.NewBitStreamWriter(32)
	pes.Encode(output)
	wire := []byte{0, 0, 1, 0xe0, 0, 7, 0x80, 0, 2, 0xff, 0xff, 0x31, 0x42}
	if got := output.Bits(); !bytes.Equal(got, wire) {
		t.Errorf("encoded PES = %x, want %x", got, wire)
	}
}
