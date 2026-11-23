package mpeg2

import (
	"bytes"
	"testing"

	"github.com/yapingcat/gomedia/go-codec"
)

func TestPSDemuxerContinuesPESWithoutBorrowingAnotherStreamsTimestamps(t *testing.T) {
	muxer := NewPsMuxer()
	primary := muxer.AddStream(PS_STREAM_G711A)
	other := muxer.AddStream(PS_STREAM_G711U)
	demuxer := NewPSDemuxer()
	muxer.OnPacket = func(packet []byte) {
		if err := demuxer.Input(packet); err != nil {
			t.Fatal(err)
		}
	}
	first := bytes.Repeat([]byte{0x55}, 80)
	second := bytes.Repeat([]byte{0x56}, 80)
	want := append(append([]byte(nil), first...), second...)
	var frames int
	demuxer.OnFrame = func(data []byte, cid PS_STREAM_TYPE, pts, dts uint64) {
		if cid == PS_STREAM_G711A {
			frames++
			if !bytes.Equal(data, want) || pts != 100 || dts != 100 {
				t.Errorf("audio=%d bytes PTS/DTS=%d/%d, want original sample at 100/100", len(data), pts, dts)
			}
		}
	}
	demuxer.OnPacket = func(packet Display, err error) {
		if pes, ok := packet.(*PesPacket); ok && err == nil && pes.PTS_DTS_flags == 0 {
			if pes.Pts != 0 || pes.Dts != 0 {
				t.Errorf("PES header without timestamps exposed PTS/DTS=%d/%d", pes.Pts, pes.Dts)
			}
		}
	}

	if err := muxer.Write(primary, first, 100, 100); err != nil {
		t.Fatal(err)
	}
	if err := muxer.Write(other, make([]byte, 160), 180, 180); err != nil {
		t.Fatal(err)
	}
	continuation := NewPesPacket()
	continuation.Stream_id = primary
	continuation.Pes_payload = second
	continuation.PES_packet_length = uint16(3 + len(second))
	writer := codec.NewBitStreamWriter(128)
	continuation.Encode(writer)
	if err := demuxer.Input(writer.Bits()); err != nil {
		t.Fatal(err)
	}

	demuxer.Flush()
	if frames != 1 {
		t.Errorf("delivered %d frames for the primary stream, want 1", frames)
	}
}

func TestMPEG1PESDecodesPTSOnlyAndOmittedTimestamps(t *testing.T) {
	pes := NewPesPacket()
	for _, tc := range []struct {
		name     string
		header   []byte
		flags    uint8
		pts, dts uint64
	}{
		{"PTS and DTS", []byte{0x31, 0, 3, 0x7b, 0xb1, 0x11, 0, 3, 0x5f, 0x91}, 3, 48600, 45000},
		{"PTS only", []byte{0x21, 0, 3, 0x7b, 0xb1}, 2, 48600, 48600},
		{"no timestamps", []byte{0x0f}, 0, 0, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			payload := []byte{0, 0, 1, 0xb3}
			wire := []byte{0, 0, 1, 0xe0, 0, byte(len(tc.header) + len(payload))}
			wire = append(wire, tc.header...)
			wire = append(wire, payload...)
			if err := pes.DecodeMpeg1(codec.NewBitStream(wire)); err != nil {
				t.Fatal(err)
			}
			if pes.PTS_DTS_flags != tc.flags || pes.Pts != tc.pts || pes.Dts != tc.dts || !bytes.Equal(pes.Pes_payload, payload) {
				t.Errorf("flags=%d PTS/DTS=%d/%d payload=%x, want flags=%d PTS/DTS=%d/%d payload=%x",
					pes.PTS_DTS_flags, pes.Pts, pes.Dts, pes.Pes_payload, tc.flags, tc.pts, tc.dts, payload)
			}
		})
	}
}
