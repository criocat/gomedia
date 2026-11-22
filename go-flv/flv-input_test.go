package flv

import (
	"bytes"
	"encoding/binary"
	"os"
	"reflect"
	"testing"

	"github.com/yapingcat/gomedia/go-codec"
)

func TestFlvReaderSkipsHeaderExtension(t *testing.T) {
	file, err := os.ReadFile("testdata/hevc-aac.flv")
	if err != nil {
		t.Fatal(err)
	}
	type decodedFrame struct {
		codec    codec.CodecID
		pts, dts uint32
		data     []byte
	}
	read := func(input []byte, chunkSize int) []decodedFrame {
		t.Helper()
		var frames []decodedFrame
		reader := CreateFlvReader()
		reader.OnFrame = func(cid codec.CodecID, frame []byte, pts, dts uint32) {
			frames = append(frames, decodedFrame{cid, pts, dts, append([]byte(nil), frame...)})
		}
		for len(input) > 0 {
			size := chunkSize
			if size > len(input) {
				size = len(input)
			}
			if err := reader.Input(input[:size]); err != nil {
				t.Fatalf("Input with %d-byte chunks: %v", chunkSize, err)
			}
			input = input[size:]
		}
		return frames
	}
	want := read(file, len(file))
	if len(want) == 0 {
		t.Fatal("fixture must contain frames")
	}
	const extensionSize = 29
	extended := append([]byte(nil), file[:9]...)
	binary.BigEndian.PutUint32(extended[5:], 9+extensionSize)
	extended = append(extended, bytes.Repeat([]byte{0x5a}, extensionSize)...)
	extended = append(extended, file[9:]...)
	for _, chunkSize := range []int{1, 13, len(extended)} {
		if got := read(extended, chunkSize); !reflect.DeepEqual(got, want) {
			t.Errorf("%d-byte chunks: read %d frames from extended FLV, want %d identical frames", chunkSize, len(got), len(want))
		}
	}
}

func TestAVCMuxerPreservesSharedAnnexBFrame(t *testing.T) {
	accessUnit, err := os.ReadFile("../go-rtsp/rtp/testdata/single-au.h264")
	if err != nil {
		t.Fatal(err)
	}
	original := append([]byte(nil), accessUnit...)
	first := NewAVCMuxer().Write(accessUnit, 90, 50)
	if !bytes.Equal(accessUnit, original) {
		t.Error("first AVC muxer altered the Annex B input")
	}
	second := NewAVCMuxer().Write(accessUnit, 90, 50)
	if len(first) != 2 || !reflect.DeepEqual(first, second) {
		t.Errorf("muxers produced %d and %d tags, want identical sequence headers and frames", len(first), len(second))
	}
}
