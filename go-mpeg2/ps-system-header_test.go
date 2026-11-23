package mpeg2

import (
	"encoding/hex"
	"testing"
)

func TestPSDemuxerReadsSystemHeaderWithTrailingPadding(t *testing.T) {
	// Pack header, system header and program stream map from upstream issue 166.
	wire, err := hex.DecodeString("000001ba44000400040100601bfaffff" +
		"000001bb000e801efffeff3fe0e0d8c0c020ffff" +
		"000001bc0012efff000000081be0000090c000009adde1b5")
	if err != nil {
		t.Fatal(err)
	}
	demuxer := NewPSDemuxer()
	var headerRead, mapRead bool
	demuxer.OnPacket = func(packet Display, err error) {
		if err != nil {
			t.Fatal(err)
		}
		switch packet := packet.(type) {
		case *System_header:
			headerRead = true
			if len(packet.Streams) != 2 || packet.Streams[0].Stream_id != 0xe0 || packet.Streams[1].Stream_id != 0xc0 {
				t.Errorf("system header streams = %+v, want E0 and C0", packet.Streams)
			}
		case *Program_stream_map:
			mapRead = true
		}
	}
	if err := demuxer.Input(wire); err != nil {
		t.Fatal(err)
	}
	if !headerRead || !mapRead {
		t.Errorf("system header read = %t, following stream map read = %t", headerRead, mapRead)
	}
}
