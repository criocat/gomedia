package mpeg2

import (
	"bytes"
	"reflect"
	"testing"
)

const clockTestAAC = "\xff\xf1\x4c\x80\x01\xbf\xfc\x21\x10\x04\x60\x8c\x1c"

func TestTSDemuxerUpdatesCodecOnExistingPID(t *testing.T) {
	type sample struct {
		cid  TS_STREAM_TYPE
		data string
		pts  uint64
	}
	want := []sample{
		{TS_STREAM_AAC, clockTestAAC, 100},
		{TS_STREAM_AUDIO_MPEG1, "\xff\xfd\x84\xc4\x00\x00", 124},
	}
	var data bytes.Buffer
	for version, frame := range want {
		muxer := NewTSMuxer()
		pid := muxer.AddStream(frame.cid)
		program := muxer.pat.pmts[0]
		program.version_number = uint8(version)
		program.streams[0].cc = uint8(version)
		program.cc = uint8(version)
		muxer.pat.cc = uint8(version)
		muxer.OnPacket = func(packet []byte) { data.Write(packet) }
		if err := muxer.Write(pid, []byte(frame.data), frame.pts, frame.pts); err != nil {
			t.Fatal(err)
		}
	}
	demuxer := NewTSDemuxer()
	var got []sample
	demuxer.OnFrame = func(cid TS_STREAM_TYPE, frame []byte, pts, _ uint64) {
		got = append(got, sample{cid, string(frame), pts})
	}
	if err := demuxer.Input(&data); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("frames = %+v, want %+v", got, want)
	}
}

func TestTSDemuxerUnwrapsAudioTimestamps(t *testing.T) {
	var data bytes.Buffer
	muxer := NewTSMuxer()
	pid := muxer.AddStream(TS_STREAM_AAC)
	muxer.OnPacket = func(packet []byte) { data.Write(packet) }
	want := []uint64{95443690, 95443711, 95443732, 95443753}
	for _, stamp := range want {
		if err := muxer.Write(pid, []byte(clockTestAAC), stamp, stamp); err != nil {
			t.Fatal(err)
		}
	}
	demuxer := NewTSDemuxer()
	var got, rawPTS []uint64
	demuxer.OnFrame = func(cid TS_STREAM_TYPE, _ []byte, pts, dts uint64) {
		if cid != TS_STREAM_AAC || pts != dts {
			t.Errorf("codec = %d, PTS = %d, DTS = %d", cid, pts, dts)
		}
		got = append(got, pts)
	}
	demuxer.OnTSPacket = func(packet *TSPacket) {
		if pes, ok := packet.Payload.(*PesPacket); ok {
			rawPTS = append(rawPTS, pes.Pts)
		}
	}
	if err := demuxer.Input(&data); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("frame PTS = %v, want %v", got, want)
	}
	for i := range want {
		if len(rawPTS) != len(want) || rawPTS[i] != want[i]*90&((1<<33)-1) {
			t.Fatalf("raw PES PTS = %v, expected 33-bit wire values", rawPTS)
		}
	}
}

func TestTSDemuxerUnwrapsVideoPTSRelativeToDTS(t *testing.T) {
	var data bytes.Buffer
	muxer := NewTSMuxer()
	pid := muxer.AddStream(TS_STREAM_H264)
	muxer.OnPacket = func(packet []byte) { data.Write(packet) }
	want := [][2]uint64{
		{95443740, 95443700},
		{95443720, 95443710},
		{95443780, 95443730},
	}
	for _, timestamp := range want {
		if err := muxer.Write(pid, []byte(clockTestH264), timestamp[0], timestamp[1]); err != nil {
			t.Fatal(err)
		}
	}
	demuxer := NewTSDemuxer()
	var got [][2]uint64
	demuxer.OnFrame = func(_ TS_STREAM_TYPE, _ []byte, pts, dts uint64) {
		got = append(got, [2]uint64{pts, dts})
	}
	if err := demuxer.Input(&data); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("video PTS/DTS = %v, want %v", got, want)
	}
}
