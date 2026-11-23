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

func makeAACFragmentPacket(seq uint16, timestamp uint32, marker uint8, total int, data []byte) []byte {
	payload := make([]byte, 4+len(data))
	binary.BigEndian.PutUint16(payload, 16)
	binary.BigEndian.PutUint16(payload[2:], uint16(total)<<3)
	copy(payload[4:], data)
	packet := RtpPacket{
		Header:  RtpHdr{PayloadType: 97, SSRC: 11, SequenceNumber: seq, Timestamp: timestamp, Marker: marker},
		Payload: payload,
	}
	return packet.Encode()
}

func TestRTPAACReassemblesFragmentsBeforeAddingADTS(t *testing.T) {
	aac := []byte{0x21, 0x10, 0x04, 0x60, 0x8c, 0x1c}
	unpacker := NewAACUnPacker(13, 3, []byte{0x12, 0x10})
	frames := 0
	unpacker.OnFrame(func(data []byte, timestamp uint32, lost bool) {
		frames++
		if timestamp != 9000 || lost || len(data) != len(aac)+7 || !bytes.Equal(data[7:], aac) {
			t.Errorf("AAC frame = %x, timestamp = %d, lost = %v", data, timestamp, lost)
		}
	})
	for i, seq := range []uint16{65535, 0, 1} {
		marker := uint8(0)
		if i == 2 {
			marker = 1
		}
		packet := makeAACFragmentPacket(seq, 9000, marker, len(aac), aac[i*2:(i+1)*2])
		if err := unpacker.UnPack(packet); err != nil {
			t.Fatal(err)
		}
		// The caller can reuse its receive buffer while the next fragment is pending.
		for j := range packet {
			packet[j] = 0
		}
		if i != 2 && frames != 0 {
			t.Fatal("emitted an incomplete AAC access unit")
		}
	}
	if frames != 1 {
		t.Errorf("AAC frames = %d, want 1", frames)
	}
}

func TestRTPAACDiscardsIncompleteAccessUnitOnSequenceGap(t *testing.T) {
	unpacker := NewAACUnPacker(13, 3, nil)
	var frames [][]byte
	unpacker.OnFrame(func(data []byte, _ uint32, _ bool) { frames = append(frames, data) })
	next := []byte{0x21, 0x10, 0x04, 0x60, 0x8c, 0x1c}
	packets := [][]byte{
		makeAACFragmentPacket(1, 9000, 0, 6, []byte{0x21, 0x10}),
		makeAACFragmentPacket(3, 9000, 1, 6, []byte{0x8c, 0x1c}),
		makeAACFragmentPacket(4, 10024, 1, len(next), next),
	}
	for _, packet := range packets {
		if err := unpacker.UnPack(packet); err != nil {
			t.Fatal(err)
		}
	}
	if want := [][]byte{next}; !reflect.DeepEqual(frames, want) {
		t.Errorf("AAC frames after a lost fragment = %x, want %x", frames, want)
	}
}
