package mp4

import (
	"bytes"
	"encoding/binary"
	"errors"
	"io"
	"os"
	"testing"
)

func TestMP4EmptyEditUsesMovieTimescale(t *testing.T) {
	file, err := os.ReadFile("testdata/delayed-aac.mp4")
	if err != nil {
		t.Fatal(err)
	}
	demuxer := CreateMp4Demuxer(bytes.NewReader(file))
	tracks, err := demuxer.ReadHead()
	if err != nil {
		t.Fatal(err)
	}
	if len(tracks) != 2 || demuxer.GetMp4Info().Timescale != 1000 || tracks[1].Timescale != 44100 {
		t.Fatalf("expected two tracks and movie/second-track timescales of 1000/44100, got %+v", tracks)
	}
	if got := tracks[1].StartDts; got != 376 || demuxer.tracks[1].samplelist[0].dts != 16582 {
		t.Errorf("delayed track starts at %d ms, want 376 ms (16582 track ticks as reported by FFprobe)", got)
	}
}

func TestMP4ReadsExtendedMdatWithoutSkippingNextBox(t *testing.T) {
	file, err := os.ReadFile("testdata/delayed-aac.mp4")
	if err != nil {
		t.Fatal(err)
	}
	free := int(binary.BigEndian.Uint32(file))
	if string(file[free+4:free+8]) != "free" || binary.BigEndian.Uint32(file[free:]) != 8 || string(file[free+12:free+16]) != "mdat" {
		t.Fatal("fixture must have an 8-byte free box directly before mdat")
	}
	mdatSize := binary.BigEndian.Uint32(file[free+8:])
	extended := append([]byte(nil), file...)
	// Reuse the free box space for the longer header. Payload and stco stay put.
	binary.BigEndian.PutUint32(extended[free:], 1)
	copy(extended[free+4:], "mdat")
	binary.BigEndian.PutUint64(extended[free+8:], uint64(mdatSize)+8)
	// A leaf box makes skipping its header observable even when the parser
	// would otherwise discover the next container's children independently.
	end := free + 8 + int(mdatSize)
	extended = append(extended[:end:end], 0, 0, 0, 12, 'f', 'r', 'e', 'e', 0, 0, 0, 0)
	extended = append(extended, file[end:]...)
	standard := CreateMp4Demuxer(bytes.NewReader(file))
	if _, err := standard.ReadHead(); err != nil {
		t.Fatal(err)
	}
	demuxer := CreateMp4Demuxer(bytes.NewReader(extended))
	tracks, err := demuxer.ReadHead()
	if err != nil || len(tracks) != 2 {
		t.Fatalf("ReadHead with extended mdat returned %d tracks and %v, want two tracks", len(tracks), err)
	}
	var count uint32
	for {
		want, referenceErr := standard.ReadPacket()
		got, gotErr := demuxer.ReadPacket()
		if errors.Is(referenceErr, io.EOF) && errors.Is(gotErr, io.EOF) {
			break
		}
		if referenceErr != nil || gotErr != nil {
			t.Fatalf("sample %d: reference: %v, extended: %v", count, referenceErr, gotErr)
		}
		if got.TrackId != want.TrackId || got.Pts != want.Pts || got.Dts != want.Dts || !bytes.Equal(got.Data, want.Data) {
			t.Fatalf("sample %d differs between normal and extended mdat", count)
		}
		count++
	}
	if count == 0 || count != tracks[0].SampleCount+tracks[1].SampleCount {
		t.Errorf("read %d packets, expected %d", count, tracks[0].SampleCount+tracks[1].SampleCount)
	}
}
