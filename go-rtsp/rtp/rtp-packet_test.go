package rtp

import (
	"bytes"
	"testing"
)

func TestRTPDecodeClearsOptionsWhenReused(t *testing.T) {
	header := RtpHdr{SSRC: 1, PayloadType: 97}
	withOptions := (&RtpPacket{
		Header:     header,
		Extensions: []byte{0xbe, 0xde, 0, 1, 0x10, 0x42, 0, 0},
		Padding:    []byte{0, 0, 0, 4},
		Payload:    []byte{1, 2},
	}).Encode()
	header.SequenceNumber++
	plain := (&RtpPacket{Header: header, Payload: []byte{3, 4}}).Encode()

	var packet RtpPacket
	if err := packet.Decode(withOptions); err != nil {
		t.Fatal(err)
	}
	if len(packet.Extensions) != 8 || len(packet.Padding) != 4 {
		t.Fatalf("first packet extensions = %x, padding = %x", packet.Extensions, packet.Padding)
	}
	if err := packet.Decode(plain); err != nil {
		t.Fatal(err)
	}
	if len(packet.Extensions) != 0 || len(packet.Padding) != 0 || !bytes.Equal(packet.Encode(), plain) {
		t.Errorf("reused packet extensions = %x, padding = %x, wire = %x", packet.Extensions, packet.Padding, packet.Encode())
	}
}
