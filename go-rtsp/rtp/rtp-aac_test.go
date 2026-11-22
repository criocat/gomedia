package rtp

import (
	"bytes"
	"encoding/binary"
	"reflect"
	"testing"
)

func TestRTPAACBatchedAccessUnitsAdvanceTimestamps(t *testing.T) {
	frame := []byte{0x21, 0x10, 0x04, 0x60, 0x8c, 0x1c}
	wire := make([]byte, 6)
	binary.BigEndian.PutUint16(wire, 32)
	binary.BigEndian.PutUint16(wire[2:], uint16(len(frame))<<3)
	binary.BigEndian.PutUint16(wire[4:], uint16(len(frame))<<3)
	wire = append(wire, frame...)
	wire = append(wire, frame...)
	packet := RtpPacket{Header: RtpHdr{Timestamp: 9000}, Payload: wire}
	unpacker := NewAACUnPacker(13, 3, []byte{0x12, 0x10})
	var timestamps []uint32
	unpacker.OnFrame(func(data []byte, ts uint32, lost bool) {
		if lost || len(data) < 7 || !bytes.Equal(data[7:], frame) {
			t.Fatalf("invalid AAC frame %x or loss flag %t", data, lost)
		}
		timestamps = append(timestamps, ts)
	})
	if err := unpacker.UnPack(packet.Encode()); err != nil {
		t.Fatal(err)
	}
	if want := []uint32{9000, 10024}; !reflect.DeepEqual(timestamps, want) {
		t.Errorf("AAC timestamps = %v, want %v", timestamps, want)
	}
}
