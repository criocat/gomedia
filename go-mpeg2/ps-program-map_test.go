package mpeg2

import (
	"bytes"
	"encoding/binary"
	"testing"
)

func TestPSMuxerPublishesNewAudioTrack(t *testing.T) {
	muxer := NewPsMuxer()
	demuxer := NewPSDemuxer()

	var versions []uint8
	demuxer.OnPacket = func(packet Display, err error) {
		if err != nil {
			t.Fatal(err)
		}
		if program, ok := packet.(*Program_stream_map); ok {
			versions = append(versions, program.Program_stream_map_version)
		}
	}
	muxer.OnPacket = func(packet []byte) {
		if err := demuxer.Input(packet); err != nil {
			t.Fatal(err)
		}
	}

	for i, cid := range []PS_STREAM_TYPE{PS_STREAM_G711A, PS_STREAM_G711U} {
		sid := muxer.AddStream(cid)
		audio := bytes.Repeat([]byte{byte(i + 1)}, 160)
		var received int
		demuxer.OnFrame = func(frame []byte, gotCID PS_STREAM_TYPE, pts, dts uint64) {
			if gotCID != cid || !bytes.Equal(frame, audio) || pts != dts {
				t.Errorf("unexpected audio codec %d, size %d, PTS/DTS %d/%d", gotCID, len(frame), pts, dts)
			}
			received++
		}
		for _, timestamp := range []uint64{100 + uint64(i)*40, 120 + uint64(i)*40} {
			if err := muxer.Write(sid, audio, timestamp, timestamp); err != nil {
				t.Fatal(err)
			}
			demuxer.Flush()
		}
		if received != 2 {
			t.Errorf("codec %d: got %d frames, want 2", cid, received)
		}
	}
	if !bytes.Equal(versions, []uint8{2, 3}) {
		t.Errorf("program map versions = %v, want [2 3]", versions)
	}
}

func TestPSProgramMapCRCIncludesStartCode(t *testing.T) {
	muxer := NewPsMuxer()
	sid := muxer.AddStream(PS_STREAM_G711A)
	muxer.AddStream(PS_STREAM_G711U)

	var packet []byte
	muxer.OnPacket = func(data []byte) {
		packet = append(packet[:0], data...)
	}
	if err := muxer.Write(sid, bytes.Repeat([]byte{0x55}, 160), 100, 100); err != nil {
		t.Fatal(err)
	}
	start := bytes.Index(packet, []byte{0, 0, 1, 0xbc})
	if start < 0 || start+6 > len(packet) {
		t.Fatal("missing program stream map")
	}
	end := start + 6 + int(binary.BigEndian.Uint16(packet[start+4:]))
	if end > len(packet) {
		t.Fatal("incomplete program stream map")
	}

	// Compute the MPEG-2 remainder independently of the writer's CRC table.
	remainder := uint32(0xffffffff)
	for _, value := range packet[start:end] {
		remainder ^= uint32(value) << 24
		for i := 0; i < 8; i++ {
			if remainder&0x80000000 != 0 {
				remainder = (remainder << 1) ^ 0x04c11db7
			} else {
				remainder <<= 1
			}
		}
	}
	if remainder != 0 {
		t.Fatalf("full program stream map CRC remainder = %#08x, want zero", remainder)
	}
}
