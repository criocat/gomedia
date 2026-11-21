package flv

import (
	"bytes"
	"os"
	"testing"

	"github.com/yapingcat/gomedia/go-codec"
)

func testHEVCParameterSets() (vps, initialSPS, updatedSPS, pps, idr []byte) {
	// libx265 SPS for 64x64 and 96x64 with the same ID, VPS and PPS.
	vps = []byte{0, 0, 0, 1, 0x40, 0x01, 0x0c, 0x01, 0xff, 0xff, 0x01, 0x60,
		0x00, 0x00, 0x03, 0x00, 0x90, 0x00, 0x00, 0x03, 0x00, 0x00, 0x03, 0x00, 0x1e, 0x95, 0x94, 0x09}
	initialSPS = []byte{0, 0, 0, 1, 0x42, 0x01, 0x01, 0x01, 0x60, 0x00, 0x00, 0x03, 0x00, 0x90,
		0x00, 0x00, 0x03, 0x00, 0x00, 0x03, 0x00, 0x1e, 0xa0, 0x20, 0x81, 0x05, 0x96, 0x56,
		0x54, 0xa4, 0xc2, 0xf0, 0x16, 0x80, 0x80, 0x00, 0x00, 0x03, 0x00, 0x80, 0x00, 0x00, 0x03, 0x00, 0x84}
	updatedSPS = []byte{0, 0, 0, 1, 0x42, 0x01, 0x01, 0x01, 0x60, 0x00, 0x00, 0x03, 0x00, 0x90,
		0x00, 0x00, 0x03, 0x00, 0x00, 0x03, 0x00, 0x1e, 0xa0, 0x30, 0x81, 0x05, 0x96, 0x56,
		0x54, 0xa4, 0xc2, 0xf0, 0x16, 0x80, 0x80, 0x00, 0x00, 0x03, 0x00, 0x80, 0x00, 0x00, 0x03, 0x00, 0x84}
	pps = []byte{0, 0, 0, 1, 0x44, 0x01, 0xc0, 0x73, 0xc0, 0x89}
	idr = []byte{0, 0, 0, 1, 0x28, 0x01, 0xac, 0x4e, 0xd7, 0x1f, 0xff, 0xf5, 0xde, 0x9c, 0xaf, 0xdf, 0x86, 0x25, 0xc0}
	return
}

func TestHEVCInBandParameterSets(t *testing.T) {
	vps, initialSPS, updatedSPS, pps, idr := testHEVCParameterSets()
	config := codec.NewHEVCRecordConfiguration()
	config.UpdateVPS(vps)
	config.UpdateSPS(initialSPS)
	config.UpdatePPS(pps)
	sequence, err := config.Encode()
	if err != nil {
		t.Fatal(err)
	}
	checkInBandSPSUpdate(t, FLV_HEVC, sequence, updatedSPS, idr, bytes.Join([][]byte{vps, updatedSPS, pps, idr}, nil))
}

func TestHEVCLegacyStartup(t *testing.T) {
	firstFrame := testHEVCFirstFrame(t)
	start := []byte{byte(KEY_FRAME)<<4 | byte(FLV_HEVC), AVC_NALU, 0, 0, 0}
	next := append([]byte(nil), start...)
	var parameterSets, frameWithoutSets []byte
	codec.SplitFrameWithStartCode(firstFrame, func(nalu []byte) bool {
		start = appendHEVCNAL(start, nalu)
		switch codec.H265NaluType(nalu) {
		case codec.H265_NAL_VPS, codec.H265_NAL_SPS, codec.H265_NAL_PPS:
			parameterSets = append(parameterSets, nalu...)
		default:
			next = appendHEVCNAL(next, nalu)
			frameWithoutSets = append(frameWithoutSets, nalu...)
		}
		return true
	})

	d := NewHevcTagDemuxer()
	var frames [][]byte
	d.OnFrame(func(cid codec.CodecID, frame []byte, cts int) {
		if cid != codec.CODECID_VIDEO_H265 || cts != 0 {
			t.Fatalf("wrong codec or composition time: %v, %d", cid, cts)
		}
		frames = append(frames, frame)
	})
	enhanced := append([]byte{0x93, 'h', 'v', 'c', '1'}, start[5:]...)
	if err := d.Decode(enhanced); err == nil {
		t.Fatal("accepted enhanced HEVC before SequenceStart")
	}
	if err := d.Decode(next); err == nil || len(frames) != 0 {
		t.Fatal("accepted legacy startup without parameter sets")
	}
	if err := d.Decode(start); err != nil {
		t.Fatalf("legacy startup failed: %v", err)
	}
	if len(frames) != 1 || !bytes.Equal(frames[0], firstFrame) {
		t.Fatal("wrong legacy startup frame")
	}
	if err := d.Decode(next); err != nil {
		t.Fatal(err)
	}
	if len(frames) != 2 || !bytes.Equal(frames[1], append(parameterSets, frameWithoutSets...)) {
		t.Fatal("following keyframe did not get the initial parameter sets")
	}
}

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
