package flv

import (
	"bytes"
	"testing"

	"github.com/yapingcat/gomedia/go-codec"
)

func TestAACRejectInvalidTags(t *testing.T) {
	for _, tc := range []struct {
		name string
		tag  []byte
	}{
		{"truncated-tag", []byte{0xaf}},
		{"empty-config", []byte{0xaf, 0}},
		{"truncated-config", []byte{0xaf, 0, 0x12}},
		{"unknown-packet-type", []byte{0xaf, 2, 0x12, 0x10}},
		{"wrong-sound-format", []byte{0x70, 0, 0x11, 0x90}},
		{"empty-payload", []byte{0xaf, 1}},
		{"oversized-payload", append([]byte{0xaf, 1}, make([]byte, 8185)...)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			demuxer := NewAACTagDemuxer()
			if err := demuxer.Decode([]byte{0xaf, 0, 0x12, 0x10}); err != nil {
				t.Fatal(err)
			}
			var frames [][]byte
			demuxer.OnFrame(func(_ codec.CodecID, frame []byte) {
				frames = append(frames, frame)
			})

			if err := demuxer.Decode(tc.tag); err == nil {
				t.Error("accepted invalid AAC tag")
			}
			if len(frames) != 0 {
				t.Fatal("invalid AAC tag produced a frame")
			}

			payload := []byte{1, 2, 3}
			if err := demuxer.Decode(append([]byte{0xaf, 1}, payload...)); err != nil {
				t.Fatalf("valid tag after rejection: %v", err)
			}
			if len(frames) != 1 || len(frames[0]) != 7+len(payload) || !bytes.Equal(frames[0][7:], payload) {
				t.Fatal("valid AAC payload was not emitted")
			}
			header := codec.NewAdtsFrameHeader()
			header.Decode(frames[0])
			if header.Fix_Header.Profile != 1 || header.Fix_Header.Sampling_frequency_index != 4 || header.Fix_Header.Channel_configuration != 2 {
				t.Fatal("invalid tag changed active AAC-LC configuration")
			}
		})
	}
}

func TestAACMaxADTSPayload(t *testing.T) {
	demuxer := NewAACTagDemuxer()
	if err := demuxer.Decode([]byte{0xaf, 0, 0x12, 0x10}); err != nil {
		t.Fatal(err)
	}
	var frame []byte
	demuxer.OnFrame(func(_ codec.CodecID, data []byte) {
		frame = data
	})
	if err := demuxer.Decode(append([]byte{0xaf, 1}, make([]byte, 8184)...)); err != nil {
		t.Fatalf("maximum AAC payload rejected: %v", err)
	}
	if len(frame) != 8191 {
		t.Fatalf("ADTS frame length = %d, want 8191", len(frame))
	}
	header := codec.NewAdtsFrameHeader()
	header.Decode(frame)
	if got := header.Variable_Header.Frame_length; got != 8191 {
		t.Fatalf("ADTS header length field = %d, want 8191", got)
	}
}
