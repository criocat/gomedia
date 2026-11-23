package mp4

import (
	"bytes"
	"encoding/binary"
	"errors"
	"io"
	"reflect"
	"testing"
)

func TestMP4MuxerFinalizesMDATAboveFourGiB(t *testing.T) {
	// Model the output window after a large top-level free box.
	output := &offsetMP4Writer{
		base:             1<<32 + 64,
		fmp4WriterSeeker: newFmp4WriterSeeker(4096),
	}
	muxer, err := CreateMp4Muxer(output)
	if err != nil {
		t.Fatal(err)
	}
	id := muxer.AddAudioTrack(MP4_CODEC_G711A, WithAudioSampleRate(8000), WithAudioChannelCount(1))
	frame := bytes.Repeat([]byte{0xd5}, 160)
	if err := muxer.Write(id, frame, 0, 0); err != nil {
		t.Fatal(err)
	}
	if err := muxer.WriteTrailer(); err != nil {
		t.Fatal(err)
	}
	mdat := bytes.Index(output.buffer, []byte("mdat"))
	if mdat < 4 || binary.BigEndian.Uint32(output.buffer[mdat-4:mdat]) != uint32(len(frame)+8) {
		t.Fatalf("missing mdat header with the final size")
	}
	co64 := bytes.Index(output.buffer, []byte("co64"))
	if co64 < 4 {
		t.Fatal("missing co64 box")
	}
	got := binary.BigEndian.Uint64(output.buffer[co64+12:])
	if want := uint64(output.base) + uint64(mdat+4); got != want {
		t.Errorf("chunk offset = %d, want %d", got, want)
	}
}

func TestMP4ChunkOffsetEncodingChecksAllChunks(t *testing.T) {
	for _, tc := range []struct {
		name    string
		offsets []uint64
		box     string
	}{
		{"32-bit offsets", []uint64{1<<32 - 1, 512}, "stco"},
		{"first offset above 4 GiB", []uint64{1<<32 + 512, 1024}, "co64"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			wire := makeStco(&movstco{entryCount: uint32(len(tc.offsets)), chunkOffsetlist: tc.offsets})
			if got := string(wire[4:8]); got != tc.box {
				t.Fatalf("box = %s, want %s", got, tc.box)
			}
			demuxer := CreateMp4Demuxer(bytes.NewReader(wire[8:]))
			track := &mp4track{stbltable: new(movstbl)}
			demuxer.tracks = []*mp4track{track}
			decode := decodeStcoBox
			if tc.box == "co64" {
				decode = decodeCo64Box
			}
			if err := decode(demuxer); err != nil {
				t.Fatal(err)
			}
			if got := track.stbltable.stco.chunkOffsetlist; !reflect.DeepEqual(got, tc.offsets) {
				t.Errorf("offsets = %v, want %v", got, tc.offsets)
			}
		})
	}
}

type offsetMP4Writer struct {
	*fmp4WriterSeeker
	base int64
}

func (writer *offsetMP4Writer) Seek(offset int64, whence int) (int64, error) {
	if whence == io.SeekStart {
		offset -= writer.base
		if offset < 0 {
			return 0, errors.New("seek before the MP4 output window")
		}
	}
	position, err := writer.fmp4WriterSeeker.Seek(offset, whence)
	if err != nil {
		return 0, err
	}
	return writer.base + position, nil
}
