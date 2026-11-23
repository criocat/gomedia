package mpeg2

import (
	"bytes"
	"testing"
)

func TestPSDemuxerUpdatesCodecFromProgramMap(t *testing.T) {
	muxer := NewPsMuxer()
	sid := muxer.AddStream(PS_STREAM_G711A)
	demuxer := NewPSDemuxer()
	muxer.OnPacket = func(packet []byte) {
		if err := demuxer.Input(packet); err != nil {
			t.Fatal(err)
		}
	}

	type audioFrame struct {
		codec    PS_STREAM_TYPE
		pts, dts uint64
		data     []byte
	}
	var received []audioFrame
	demuxer.OnFrame = func(data []byte, codec PS_STREAM_TYPE, pts, dts uint64) {
		received = append(received, audioFrame{codec, pts, dts, append([]byte(nil), data...)})
	}

	oldAudio := bytes.Repeat([]byte{0x55}, 160)
	newAudio := bytes.Repeat([]byte{0xff}, 160)
	if err := muxer.Write(sid, oldAudio, 100, 100); err != nil {
		t.Fatal(err)
	}
	// Both encodings use the same PES stream ID across the change.
	muxer.psm.Stream_map[0].Stream_type = uint8(PS_STREAM_G711U)
	muxer.psm.Program_stream_map_version++
	muxer.tablesPending = true
	if err := muxer.Write(sid, newAudio, 120, 120); err != nil {
		t.Fatal(err)
	}
	demuxer.Flush()

	want := []audioFrame{{PS_STREAM_G711A, 100, 100, oldAudio}, {PS_STREAM_G711U, 120, 120, newAudio}}
	if len(received) != len(want) {
		t.Fatalf("received %d frames, want %d", len(received), len(want))
	}
	for i, frame := range received {
		if frame.codec != want[i].codec || frame.pts != want[i].pts || frame.dts != want[i].dts || !bytes.Equal(frame.data, want[i].data) {
			t.Errorf("frame %d: codec %d, PTS/DTS %d/%d, size %d, want codec %d, PTS/DTS %d/%d and intact audio",
				i, frame.codec, frame.pts, frame.dts, len(frame.data), want[i].codec, want[i].pts, want[i].dts)
		}
	}
}

func TestPSDemuxerWaitsForCurrentProgramMap(t *testing.T) {
	muxer := NewPsMuxer()
	demuxer := NewPSDemuxer()
	muxer.OnPacket = func(packet []byte) {
		if err := demuxer.Input(packet); err != nil {
			t.Fatal(err)
		}
	}

	audio := bytes.Repeat([]byte{0x55}, 160)
	first := muxer.AddStream(PS_STREAM_G711A)
	if err := muxer.Write(first, audio, 100, 100); err != nil {
		t.Fatal(err)
	}
	future := muxer.AddStream(PS_STREAM_G711U)
	muxer.psm.Current_next_indicator = 0
	if err := muxer.Write(first, audio, 120, 120); err != nil {
		t.Fatal(err)
	}
	if demuxer.streamMap[first] == nil || demuxer.streamMap[future] != nil {
		t.Error("a next program map changed the active streams")
	}

	muxer.psm.Current_next_indicator = 1
	muxer.tablesPending = true
	if err := muxer.Write(future, audio, 140, 140); err != nil {
		t.Fatal(err)
	}
	if track := demuxer.streamMap[future]; track == nil || track.cid != PS_STREAM_G711U {
		t.Fatal("new audio track missing from the active program map")
	}
}
