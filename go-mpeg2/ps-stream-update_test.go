package mpeg2

import (
	"os"
	"testing"

	"github.com/yapingcat/gomedia/go-codec"
)

func TestPSDemuxerRetiresStreamMissingFromCurrentMap(t *testing.T) {
	muxer := NewPsMuxer()
	removed := muxer.AddStream(PS_STREAM_G711A)
	remaining := muxer.AddStream(PS_STREAM_G711U)
	demuxer := NewPSDemuxer()
	muxer.OnPacket = func(packet []byte) {
		if err := demuxer.Input(packet); err != nil {
			t.Fatal(err)
		}
	}

	var retiredFrames int
	demuxer.OnFrame = func(data []byte, cid PS_STREAM_TYPE, pts, dts uint64) {
		if cid == PS_STREAM_G711A {
			retiredFrames++
			if len(data) != 160 || data[0] != 0x55 || pts != 100 || dts != 100 {
				t.Errorf("retired frame size=%d PTS/DTS=%d/%d, want original A-law sample", len(data), pts, dts)
			}
		}
	}

	law := make([]byte, 160)
	law[0] = 0x55
	if err := muxer.Write(removed, law, 100, 100); err != nil {
		t.Fatal(err)
	}
	muxer.psm.Stream_map = muxer.psm.Stream_map[1:]
	muxer.system.Streams = muxer.system.Streams[1:]
	muxer.system.Audio_bound = 1
	muxer.psm.Program_stream_map_version++
	muxer.tablesPending = true
	if err := muxer.Write(remaining, make([]byte, 160), 120, 120); err != nil {
		t.Fatal(err)
	}
	if retiredFrames != 1 || len(demuxer.streamMap) != 1 || demuxer.streamMap[remaining] == nil {
		t.Errorf("after map update: old frames=%d active tracks=%d, want one old frame and only the remaining track",
			retiredFrames, len(demuxer.streamMap))
	}
	demuxer.Flush()
	if retiredFrames != 1 {
		t.Errorf("Flush yielded %d old frames total, want 1", retiredFrames)
	}
}

func TestPSDemuxerKeepsVideoNALTimesAcrossPES(t *testing.T) {
	accessUnit, err := os.ReadFile("../go-rtsp/rtp/testdata/single-au.h265")
	if err != nil {
		t.Fatal(err)
	}
	type nalu struct {
		kind     codec.H265_NAL_TYPE
		pts, dts uint64
	}
	var types []codec.H265_NAL_TYPE
	codec.SplitFrame(accessUnit, func(data []byte) bool {
		if len(data) != 0 {
			if kind := codec.H265NaluTypeWithoutStartCode(data); kind != codec.H265_NAL_AUD {
				types = append(types, kind)
			}
		}
		return true
	})

	muxer := NewPsMuxer()
	sid := muxer.AddStream(PS_STREAM_H265)
	demuxer := NewPSDemuxer()
	muxer.OnPacket = func(packet []byte) {
		if err := demuxer.Input(packet); err != nil {
			t.Fatal(err)
		}
	}
	var got []nalu
	demuxer.OnFrame = func(data []byte, cid PS_STREAM_TYPE, pts, dts uint64) {
		if cid != PS_STREAM_H265 {
			t.Errorf("video codec=%d, want HEVC", cid)
		}
		got = append(got, nalu{codec.H265NaluType(data), pts, dts})
	}

	var want []nalu
	for _, dts := range []uint64{100, 140} {
		for _, kind := range types {
			want = append(want, nalu{kind, dts + 20, dts})
		}
		if err := muxer.Write(sid, accessUnit, dts+20, dts); err != nil {
			t.Fatal(err)
		}
	}
	demuxer.Flush()
	if len(got) != len(want) {
		t.Fatalf("received %d NAL units, want %d", len(got), len(want))
	}
	for index, received := range got {
		if received != want[index] {
			t.Errorf("NAL %d = %+v, want %+v", index, received, want[index])
		}
	}
}
