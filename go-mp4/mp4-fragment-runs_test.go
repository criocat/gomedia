package mp4

import (
	"bytes"
	"encoding/binary"
	"testing"
)

func TestMP4FragmentCarriesDTSAcrossRuns(t *testing.T) {
	track := newmp4track(MP4_CODEC_AAC, nil)
	track.defaultDuration = 40
	demuxer := CreateMp4Demuxer(nil)
	demuxer.currentTrack = track

	readDecodeTime := func(timestamp uint64) {
		t.Helper()
		_, wire := NewTrackFragmentBaseMediaDecodeTimeBox(timestamp).Encode()
		demuxer.reader = bytes.NewReader(wire[8:])
		if err := decodeTfdtBox(demuxer, uint32(len(wire))); err != nil {
			t.Fatal(err)
		}
	}
	readRun := func(offset int32, duration uint32) {
		t.Helper()
		run := NewTrackRunBox()
		run.Box.Flags = [3]uint8{0, 3, 1} // Duration, size, and data offset.
		run.SampleCount = 1
		run.Dataoffset = offset
		run.EntryList = &movtrun{entrys: []trunEntry{{sampleDuration: duration, sampleSize: 50}}}
		_, wire := run.Encode()
		demuxer.reader = bytes.NewReader(wire[8:])
		if err := decodeTrunBox(demuxer, uint32(len(wire))); err != nil {
			t.Fatal(err)
		}
	}
	readDecodeTime(100)
	readRun(1000, 25)
	readRun(1100, 0) // Use the fragment default duration of 40.
	readRun(1200, 40)
	readDecodeTime(250)
	readRun(1300, 25)
	for i, want := range []uint64{100, 125, 165, 250} {
		got := track.samplelist[i]
		if got.dts != want || got.pts != want || got.offset != uint64(1000+100*i) {
			t.Errorf("sample %d: PTS/DTS/offset = %d/%d/%d, want %d/%d/%d", i, got.pts, got.dts, got.offset, want, want, 1000+100*i)
		}
	}
}

func TestMP4FirstSingleSampleFragmentIsNotEmpty(t *testing.T) {
	output := newFmp4WriterSeeker(4096)
	muxer, err := CreateMp4Muxer(output, WithMp4Flag(MP4_FLAG_FRAGMENT))
	if err != nil {
		t.Fatal(err)
	}
	id := muxer.AddAudioTrack(MP4_CODEC_AAC)
	if err := muxer.Write(id, mp4TestAACFrame, 0, 0); err != nil {
		t.Fatal(err)
	}
	if err := muxer.WriteTrailer(); err != nil {
		t.Fatal(err)
	}
	tfhd := bytes.Index(output.buffer, []byte("tfhd"))
	trun := bytes.Index(output.buffer, []byte("trun"))
	if tfhd < 0 || trun < 0 {
		t.Fatal("expected fragment containing tfhd and trun")
	}
	flags := binary.BigEndian.Uint32(output.buffer[tfhd+4:]) & 0xffffff
	samples := binary.BigEndian.Uint32(output.buffer[trun+8:])
	if samples != 1 || flags&TF_FLAG_DURATION_IS_EMPTY != 0 {
		t.Errorf("tfhd flags %#06x for trun containing %d samples, want one sample and a clear DurationIsEmpty flag", flags, samples)
	}
}
