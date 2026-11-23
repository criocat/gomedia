package mpeg2

import (
	"bytes"
	"encoding/hex"
	"testing"
)

func TestTSDemuxerSplitsConsecutiveVideoAccessUnits(t *testing.T) {
	for _, fixture := range tsVideoFixtures {
		t.Run(fixture.name, func(t *testing.T) {
			frames := fixture.frames(t)
			got := demuxTSVideo(t, fixture.cid, []tsVideoFrame{
				{data: bytes.Join(frames, nil), pts: 120, dts: 80},
			})
			if len(got) != len(frames) {
				t.Fatalf("received %d frames, want %d", len(got), len(frames))
			}
			for i, frame := range frames {
				if !bytes.Equal(got[i].data, frame) {
					t.Errorf("frame %d = %x, want %x", i, got[i].data, frame)
				}
			}
		})
	}
}

func TestTSDemuxerUsesNewPESTimestampsForFollowingAccessUnits(t *testing.T) {
	for _, fixture := range tsVideoFixtures {
		t.Run(fixture.name, func(t *testing.T) {
			frames := fixture.frames(t)
			got := demuxTSVideo(t, fixture.cid, []tsVideoFrame{
				{data: frames[0], pts: 120, dts: 80},
				{data: bytes.Join(frames[1:3], nil), pts: 240, dts: 200},
			})
			if len(got) != 3 {
				t.Fatalf("received %d frames, want 3", len(got))
			}
			for i, want := range [][2]uint64{{120, 80}, {240, 200}, {240, 200}} {
				if pts, dts := got[i].pts, got[i].dts; pts != want[0] || dts != want[1] {
					t.Errorf("frame %d PTS/DTS = %d/%d, want %d/%d", i, pts, dts, want[0], want[1])
				}
			}
		})
	}
}

type tsVideoFixture struct {
	name string
	cid  TS_STREAM_TYPE
	hex  [4]string
}

// Four access units from libx264 and libx265 without AUD. The first is
// an IDR with parameter sets and the remaining three are predicted frames.
var tsVideoFixtures = []tsVideoFixture{
	{"H264", TS_STREAM_H264, [4]string{
		"000000016764000aacb212d808800000030080000019078913240000000168ebc3cb22c00000016588840cfffef6ecbe053614c8509679c717c1",
		"00000001419a3b10bffe97",
		"00000001419a4f0864ca610bfffe97",
		"00000001419a727843c994c0857ffe56",
	}},
	{"H265", TS_STREAM_H265, [4]string{
		"0000000140010c01ffff01600000030090000003000003001e9280900000000142010101600000030090000003000003001ea0208105964a924caf016808000003000800000300c840000000014401c172b422400000012801af1d80f701b2fffd232fb33c",
		"000000010201d0097e10c63bfa84",
		"000000010201d011ffd4430639fa84",
		"000000010201d0189ffdc43018e4fa84",
	}},
}

func (fixture tsVideoFixture) frames(t *testing.T) [][]byte {
	t.Helper()
	var frames [][]byte
	for _, encoded := range fixture.hex {
		frame, err := hex.DecodeString(encoded)
		if err != nil {
			t.Fatal(err)
		}
		frames = append(frames, frame)
	}
	return frames
}

type tsVideoFrame struct {
	data     []byte
	pts, dts uint64
}

func demuxTSVideo(t *testing.T, cid TS_STREAM_TYPE, pes []tsVideoFrame) []tsVideoFrame {
	t.Helper()
	var wire bytes.Buffer
	muxer := NewTSMuxer()
	pid := muxer.AddStream(cid)
	muxer.OnPacket = func(packet []byte) { wire.Write(packet) }
	for _, packet := range pes {
		if err := muxer.Write(pid, packet.data, packet.pts, packet.dts); err != nil {
			t.Fatal(err)
		}
	}
	demuxer := NewTSDemuxer()
	var frames []tsVideoFrame
	demuxer.OnFrame = func(got TS_STREAM_TYPE, data []byte, pts, dts uint64) {
		if got != cid {
			t.Errorf("frame codec = %d, want %d", got, cid)
		}
		frames = append(frames, tsVideoFrame{append([]byte(nil), data...), pts, dts})
	}
	if err := demuxer.Input(&wire); err != nil {
		t.Fatal(err)
	}
	return frames
}
