package flv

import (
	"bytes"
	"os"
	"testing"

	"github.com/yapingcat/gomedia/go-codec"
)

func TestHEVCConfigurationLifecycle(t *testing.T) {
	d := NewHevcTagDemuxer()
	frame := []byte{0x93, 'h', 'v', 'c', '1', 0, 0, 0, 2, 0x26, 1}
	if err := d.Decode(frame); err == nil {
		t.Fatal("accepted frame before configuration")
	}
	if err := d.Decode(testHEVCConfig(4)); err != nil {
		t.Fatal(err)
	}
	config := append([]byte(nil), d.SpsPpsVps...)
	invalid := testHEVCConfig(2)
	if err := d.Decode(invalid[:len(invalid)-1]); err == nil {
		t.Fatal("accepted invalid replacement configuration")
	}
	if !bytes.Equal(config, d.SpsPpsVps) || d.naluLengthSize != 4 {
		t.Fatal("invalid configuration replaced active configuration")
	}
	if err := d.Decode(frame); err != nil {
		t.Fatal(err)
	}
	if err := d.Decode([]byte{0x92, 'h', 'v', 'c', '1'}); err != nil {
		t.Fatal(err)
	}
	if err := d.Decode(frame); err == nil {
		t.Fatal("accepted frame after SequenceEnd without a new configuration")
	}
	if err := d.Decode(testHEVCConfig(2)); err != nil {
		t.Fatal(err)
	}
	shortFrame := append([]byte{0x93, 'h', 'v', 'c', '1'}, frame[7:]...)
	if err := d.Decode(shortFrame); err != nil {
		t.Fatalf("could not restart sequence with 2-byte NAL lengths: %v", err)
	}
}

func TestHEVCVideoHeaderReuse(t *testing.T) {
	var header VideoTag
	header.Decode([]byte{0xa1, 'h', 'v', 'c', '1', 0xff, 0xff, 0xd8})
	if header.CompositionTime != -40 {
		t.Fatal("expected signed composition time")
	}
	header.Decode([]byte{0xa3, 'h', 'v', 'c', '1'})
	if header.CompositionTime != 0 {
		t.Fatalf("CodedFramesX retained CTS from previous packet: %d", header.CompositionTime)
	}
}

func TestHEVCFLVReaderFragmented(t *testing.T) {
	input, err := os.ReadFile("testdata/hevc-aac.flv")
	if err != nil {
		t.Fatal(err)
	}
	for _, chunkSize := range []int{len(input), 1, 2, 5, 11, 37} {
		r := CreateFlvReader()
		video, audio := 0, 0
		r.OnFrame = func(cid codec.CodecID, frame []byte, pts, dts uint32) {
			switch cid {
			case codec.CODECID_VIDEO_H265:
				video++
			case codec.CODECID_AUDIO_AAC:
				audio++
			default:
				t.Fatalf("wrong codec: %v", cid)
			}
		}
		for pos := 0; pos < len(input); pos += chunkSize {
			end := pos + chunkSize
			if end > len(input) {
				end = len(input)
			}
			if err := r.Input(input[pos:end]); err != nil {
				t.Fatalf("chunkSize=%d offset=%d: %v", chunkSize, pos, err)
			}
		}
		if video != 10 || audio == 0 {
			t.Fatalf("chunkSize=%d: video=%d audio=%d", chunkSize, video, audio)
		}
	}
}
