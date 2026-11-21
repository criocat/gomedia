package rtp

import (
	"bytes"
	"testing"
)

func TestH265UnPackerPayloadValidation(t *testing.T) {
	unpacker := NewH265UnPacker()
	frames := 0
	nal := []byte{2, 1, 0x80}
	unpacker.OnFrame(func(data []byte, timestamp uint32, lost bool) {
		frames++
		if !bytes.Equal(data, append([]byte{0, 0, 0, 1}, nal...)) || timestamp != 42 || lost {
			t.Errorf("unexpected frame: data=%x timestamp=%d lost=%t", data, timestamp, lost)
		}
	})
	if err := unpacker.unpackPayload(&RtpPacket{}); err != nil {
		t.Fatalf("empty payload: %v", err)
	}
	if err := unpacker.unpackPayload(&RtpPacket{Payload: nal[:1]}); err == nil {
		t.Fatal("accepted an incomplete NAL header")
	}
	if err := unpacker.unpackFu(&RtpPacket{}); err == nil {
		t.Fatal("accepted an incomplete fragmentation unit header")
	}
	if frames != 0 {
		t.Fatal("invalid payload produced a frame")
	}
	packet := RtpPacket{Header: RtpHdr{Version: 2, Timestamp: 42}, Payload: nal}
	if err := unpacker.UnPack(packet.Encode()); err != nil || frames != 1 {
		t.Fatalf("valid payload after rejection: err=%v frames=%d", err, frames)
	}
}
