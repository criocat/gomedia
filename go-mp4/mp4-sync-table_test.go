package mp4

import (
	"bytes"
	"testing"
)

func TestMP4SyncTableIncludesAllSamplesWithoutSTSS(t *testing.T) {
	output := newFmp4WriterSeeker(4096)
	muxer, err := CreateMp4Muxer(output)
	if err != nil {
		t.Fatal(err)
	}
	id := muxer.AddAudioTrack(MP4_CODEC_G711A, WithAudioSampleRate(8000), WithAudioChannelCount(1))
	frames := [][]byte{bytes.Repeat([]byte{0xd5}, 160), bytes.Repeat([]byte{0x55}, 160)}
	for i, frame := range frames {
		dts := uint64(20 * i)
		if err := muxer.Write(id, frame, dts, dts); err != nil {
			t.Fatal(err)
		}
	}
	if err := muxer.WriteTrailer(); err != nil {
		t.Fatal(err)
	}
	demuxer := CreateMp4Demuxer(bytes.NewReader(output.buffer))
	if _, err := demuxer.ReadHead(); err != nil {
		t.Fatal(err)
	}
	if demuxer.tracks[0].stbltable.stss != nil {
		t.Fatal("fixture must omit stss")
	}
	table, err := demuxer.GetSyncTable(id)
	if err != nil || len(table) != len(frames) {
		t.Fatalf("sync table = %v, %v, want both samples", table, err)
	}
	for i, sample := range table {
		start, end := int(sample.Offset), int(sample.Offset)+int(sample.Size)
		dts := uint64(20 * i)
		if end > len(output.buffer) || !bytes.Equal(output.buffer[start:end], frames[i]) ||
			sample.Pts != dts || sample.Dts != dts {
			t.Errorf("sync sample %d = %+v, want original bytes at %d ms", i, sample, dts)
		}
	}
}

func TestMP4SyncTableRejectsOffsetOutsideUint32(t *testing.T) {
	const lastOffset = uint64(1<<32 - 1)
	demuxer := &MovDemuxer{tracks: []*mp4track{{
		trackId:    1,
		timescale:  1000,
		stbltable:  &movstbl{stss: &movstss{sampleNumber: []uint32{1}}},
		samplelist: []sampleEntry{{offset: lastOffset, size: 16}},
	}}}
	table, err := demuxer.GetSyncTable(1)
	if err != nil || len(table) != 1 || table[0].Offset != uint32(lastOffset) {
		t.Fatalf("last representable offset returned %v, %v", table, err)
	}
	demuxer.tracks[0].samplelist[0].offset = lastOffset + 17
	if table, err := demuxer.GetSyncTable(1); err == nil || table != nil {
		t.Errorf("unrepresentable offset returned %v, %v, want an error", table, err)
	}
}
