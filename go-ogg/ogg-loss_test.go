package ogg

import (
	"bytes"
	"testing"

	"github.com/yapingcat/gomedia/go-codec"
)

func TestOpusRecoversWhenFirstAudioPageIsMissing(t *testing.T) {
	demuxer := newOpusLossTestDemuxer(t)
	packet := []byte{0xf8, 0xff, 0xfe} // 960 samples at 48 kHz.
	var frames int
	demuxer.OnFrame = func(_ uint32, cid codec.CodecID, frame []byte, pts, dts uint64, lost int) {
		if cid != codec.CODECID_AUDIO_OPUS || !bytes.Equal(frame, packet) || pts != 648 || dts != pts || lost != 0 {
			t.Errorf("frame codec=%d size=%d PTS/DTS=%d/%d lost=%d, want complete Opus at 648",
				cid, len(frame), pts, dts, lost)
		}
		frames++
	}
	if err := demuxer.Input(makeOggTestPage(0, 1920, 3, []byte{3}, packet)); err != nil {
		t.Fatal(err)
	}
	if frames != 1 {
		t.Errorf("received %d frames, want 1", frames)
	}
}

func TestOpusTimestampsRecoverAfterPageLoss(t *testing.T) {
	for _, tc := range []struct {
		name  string
		flags byte
	}{{"audio", 0}, {"end_of_stream", 4}} {
		t.Run(tc.name, func(t *testing.T) {
			demuxer := newOpusLossTestDemuxer(t)
			packet := []byte{0xf8, 0xff, 0xfe}
			var timestamps []uint64
			demuxer.OnFrame = func(_ uint32, cid codec.CodecID, frame []byte, pts, dts uint64, lost int) {
				if cid != codec.CODECID_AUDIO_OPUS || !bytes.Equal(frame, packet) || pts != dts || lost != 0 {
					t.Errorf("unexpected frame codec=%d size=%d PTS/DTS=%d/%d lost=%d", cid, len(frame), pts, dts, lost)
				}
				timestamps = append(timestamps, pts)
			}
			var gaps int
			demuxer.OnPacket = func(_ uint32, _ uint64, _ []byte, lost int) {
				gaps += lost
			}

			if err := demuxer.Input(makeOggTestPage(0, 960, 2, []byte{3}, packet)); err != nil {
				t.Fatal(err)
			}
			pair := append(append([]byte(nil), packet...), packet...)
			if err := demuxer.Input(makeOggTestPage(tc.flags, 3840, 4, []byte{3, 3}, pair)); err != nil {
				t.Fatal(err)
			}
			if len(timestamps) != 3 || timestamps[0] != 0 || timestamps[1] != 1608 || timestamps[2] != 2568 {
				t.Errorf("PTS = %v, want [0 1608 2568]", timestamps)
			}
			if gaps != 1 {
				t.Errorf("notified %d page gaps, want 1", gaps)
			}
		})
	}
}

func newOpusLossTestDemuxer(t *testing.T) *Demuxer {
	t.Helper()
	head := []byte{'O', 'p', 'u', 's', 'H', 'e', 'a', 'd', 1, 1, 0x38, 0x01, 0x80, 0xbb, 0, 0, 0, 0, 0}
	tags := []byte{'O', 'p', 'u', 's', 'T', 'a', 'g', 's', 0, 0, 0, 0, 0, 0, 0, 0}
	demuxer := NewDemuxer()
	if err := demuxer.Input(makeOggTestPage(2, 0, 0, []byte{byte(len(head))}, head)); err != nil {
		t.Fatal(err)
	}
	if err := demuxer.Input(makeOggTestPage(0, 0, 1, []byte{byte(len(tags))}, tags)); err != nil {
		t.Fatal(err)
	}
	return demuxer
}
