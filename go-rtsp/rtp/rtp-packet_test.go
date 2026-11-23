package rtp

import (
	"bytes"
	"reflect"
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

func TestRTPHeaderEncodesActualCSRCCount(t *testing.T) {
	for _, count := range []int{2, 15, 16} {
		sources := make([]uint32, count)
		for i := range sources {
			sources[i] = uint32(1000 + i)
		}
		payload := []byte{0x21, 0x11}
		sent := RtpPacket{Header: RtpHdr{PayloadType: 8, SSRC: 100, CSRC: sources}, Payload: payload}
		var received RtpPacket
		if err := received.Decode(sent.Encode()); err != nil {
			t.Fatal(err)
		}
		if count > 15 {
			count = 15
		}
		if int(received.Header.CC) != count || !reflect.DeepEqual(received.Header.CSRC, sources[:count]) || !bytes.Equal(received.Payload, payload) {
			t.Errorf("RTP CSRC = %v and payload = %x, want %v and %x", received.Header.CSRC, received.Payload, sources[:count], payload)
		}
	}
}
