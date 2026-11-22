package codec

import "testing"

func TestMPEG1LayerIFrameSizeUsesWholeSlots(t *testing.T) {
	for padding, want := range []int{136, 140} {
		// MPEG-1 Layer I at 128 kbps and 44.1 kHz.
		head, err := DecodeMp3Head([]byte{0xff, 0xff, 0x40 | byte(padding)<<1, 0})
		if err != nil {
			t.Fatal(err)
		}
		if head.FrameSize != want {
			t.Errorf("frame size with padding %d = %d, want %d", padding, head.FrameSize, want)
		}
	}
}

func TestMPEG1LayerII384KbpsFrameSizes(t *testing.T) {
	wantSizes := []int{1253, 1254}
	// MPEG-1 Layer II at 384 kbps and 44.1 kHz with unpadded and padded frames.
	frames := make([]byte, wantSizes[0]+wantSizes[1])
	copy(frames, []byte{0xff, 0xfd, 0xe0, 0x04})
	copy(frames[wantSizes[0]:], []byte{0xff, 0xfd, 0xe2, 0x04})
	count := 0
	err := SplitMp3Frames(frames, func(head *MP3FrameHead, frame []byte) {
		if count >= len(wantSizes) {
			t.Fatalf("unexpected frame %d", count)
		}
		if got := head.GetBitRate(); got != 384000 || len(frame) != wantSizes[count] {
			t.Errorf("frame %d: bitrate=%d bytes=%d, want 384000/%d", count, got, len(frame), wantSizes[count])
		}
		count++
	})
	if err != nil || count != len(wantSizes) {
		t.Errorf("split %d frames, want %d: %v", count, len(wantSizes), err)
	}
}
