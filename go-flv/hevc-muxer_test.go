package flv

import (
	"bytes"
	"os"
	"sync"
	"testing"

	"github.com/yapingcat/gomedia/go-codec"
)

func TestHEVCMuxerSharedInput(t *testing.T) {
	frame := testHEVCFirstFrame(t)
	snapshot := append([]byte(nil), frame...)
	const workers = 8
	outputs := make([][]byte, workers)
	var wg sync.WaitGroup
	for i := range outputs {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			m := NewHevcMuxer()
			var retained [][]byte
			for j := 0; j < 20; j++ {
				retained = append(retained, m.Write(frame, uint32(j*40), uint32(j*40))...)
			}
			outputs[i] = bytes.Join(retained, nil)
		}(i)
	}
	wg.Wait()
	if !bytes.Equal(frame, snapshot) {
		t.Fatal("concurrent muxers modified shared input")
	}
	for i := 1; i < workers; i++ {
		if len(outputs[i]) == 0 || !bytes.Equal(outputs[0], outputs[i]) {
			t.Fatalf("different output from muxer %d", i)
		}
	}
}

func TestHEVCMuxerStartCodes(t *testing.T) {
	frame := testHEVCFirstFrame(t)
	var threeByte []byte
	codec.SplitFrame(frame, func(nalu []byte) bool {
		threeByte = append(threeByte, 0, 0, 1)
		threeByte = append(threeByte, nalu...)
		return true
	})
	fourByteCopy, threeByteCopy := append([]byte(nil), frame...), append([]byte(nil), threeByte...)
	m4, m3 := NewHevcMuxer(), NewHevcMuxer()
	tags4, tags3 := m4.Write(frame, 40, 0), m3.Write(threeByte, 40, 0)
	if len(tags4) != 2 || len(tags3) != 2 {
		t.Fatalf("wrong number of tags: %d/%d", len(tags4), len(tags3))
	}
	for i := range tags4 {
		if !bytes.Equal(tags4[i], tags3[i]) {
			t.Fatalf("start code size changed output tag %d", i)
		}
	}
	if !bytes.Equal(frame, fourByteCopy) || !bytes.Equal(threeByte, threeByteCopy) {
		t.Fatal("muxer modified caller's Annex B data")
	}
}

func testHEVCFirstFrame(t *testing.T) []byte {
	t.Helper()
	data, err := os.ReadFile("testdata/hevc-aac.flv")
	if err != nil {
		t.Fatal(err)
	}
	var frame []byte
	r := CreateFlvReader()
	r.OnFrame = func(cid codec.CodecID, payload []byte, _, _ uint32) {
		if frame == nil && cid == codec.CODECID_VIDEO_H265 {
			frame = append([]byte(nil), payload...)
		}
	}
	if err := r.Input(data); err != nil {
		t.Fatal(err)
	}
	if frame == nil {
		t.Fatal("missing video frame in fixture")
	}
	return frame
}

func TestHEVCMuxerWaitsForConfiguration(t *testing.T) {
	frame := testHEVCFirstFrame(t)
	var sets [][]byte
	var video []byte
	codec.SplitFrameWithStartCode(frame, func(nalu []byte) bool {
		typ := codec.H265NaluType(nalu)
		if typ >= 32 && typ <= 34 {
			sets = append(sets, append([]byte(nil), nalu...))
		} else if codec.IsH265VCLNaluType(typ) {
			video = append(video, nalu...)
		}
		return true
	})
	if len(sets) != 3 || len(video) == 0 {
		t.Fatal("expected VPS, SPS, PPS and video in fixture")
	}
	m := NewHevcMuxer()
	if tags := m.Write(video, 0, 0); len(tags) != 0 {
		t.Fatal("muxer emitted a frame before its decoder configuration")
	}
	d := NewHevcTagDemuxer()
	for i, set := range sets {
		tags := m.Write(set, 0, 0)
		if i != 2 && len(tags) != 0 {
			t.Fatal("emitted incomplete decoder configuration")
		}
		for _, tag := range tags {
			if err := d.Decode(tag); err != nil {
				t.Fatal(err)
			}
		}
	}
	count := 0
	d.OnFrame(func(_ codec.CodecID, payload []byte, _ int) {
		codec.SplitFrameWithStartCode(payload, func(nalu []byte) bool {
			if codec.IsH265VCLNaluType(codec.H265NaluType(nalu)) {
				count++
			}
			return true
		})
	})
	tags := m.Write(video, 40, 40)
	if len(tags) != 1 {
		t.Fatalf("expected one video tag after configuration, got %d", len(tags))
	}
	if err := d.Decode(tags[0]); err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("unexpected VCL count %d; frame received before configuration was retained", count)
	}
}
