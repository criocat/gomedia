package rtmp

import (
	"errors"
	"fmt"
	"io"
	"os"
	"testing"

	"github.com/yapingcat/gomedia/go-codec"
	"github.com/yapingcat/gomedia/go-flv"
)

func TestRTMPVideoWriteRetry(t *testing.T) {
	h264, err := os.ReadFile("../go-rtsp/rtp/testdata/single-au.h264")
	if err != nil {
		t.Fatal(err)
	}
	flvData, err := os.ReadFile("../go-flv/testdata/hevc-aac.flv")
	if err != nil {
		t.Fatal(err)
	}
	var hevc []byte
	flvReader := flv.CreateFlvReader()
	flvReader.OnFrame = func(id codec.CodecID, frame []byte, _, _ uint32) {
		if id == codec.CODECID_VIDEO_H265 && hevc == nil {
			hevc = append([]byte(nil), frame...)
		}
	}
	if err := flvReader.Input(flvData); err != nil {
		t.Fatal(err)
	}
	if hevc == nil {
		t.Fatal("missing HEVC frame in fixture")
	}

	type endpoint interface {
		SetOutput(OutputCB)
		WriteVideo(codec.CodecID, []byte, uint32, uint32) error
	}
	for _, tc := range []struct {
		name  string
		id    codec.CodecID
		frame []byte
	}{
		{"H264", codec.CODECID_VIDEO_H264, h264},
		{"HEVC", codec.CODECID_VIDEO_H265, hevc},
	} {
		for _, sender := range []struct {
			name string
			new  func() endpoint
		}{
			{"client", func() endpoint { return NewRtmpClient(WithChunkSize(FIX_CHUNK_SIZE)) }},
			{"server", func() endpoint {
				return NewRtmpServerHandle(func(server *RtmpServerHandle) {
					server.writeChunkSize = FIX_CHUNK_SIZE
				})
			}},
		} {
			for _, failAt := range []int{1, 2} {
				t.Run(fmt.Sprintf("%s/%s/message_%d", sender.name, tc.name, failAt), func(t *testing.T) {
					ep := sender.new()
					var wire []byte
					calls := 0
					ep.SetOutput(func(p []byte) error {
						calls++
						if calls == failAt {
							return io.ErrClosedPipe
						}
						wire = append(wire, p...)
						return nil
					})
					if err := ep.WriteVideo(tc.id, tc.frame, 1040, 1000); !errors.Is(err, io.ErrClosedPipe) {
						t.Fatalf("first write error = %v, want closed pipe", err)
					}
					if err := ep.WriteVideo(tc.id, tc.frame, 1120, 1080); err != nil {
						t.Fatalf("retry: %v", err)
					}

					got := 0
					err := newChunkStreamReader(FIX_CHUNK_SIZE).readRtmpMessage(wire, func(msg *rtmpMessage) error {
						if msg.msgtype != VIDEO || len(msg.msg) < 5 {
							t.Fatalf("unexpected message type %d (%d bytes)", msg.msgtype, len(msg.msg))
						}
						var video flv.VideoTag
						video.Decode(msg.msg)
						wantDTS := uint32(1080)
						if got == 0 && failAt == 2 {
							wantDTS = 1000
						}
						if video.AVCPacketType != uint8(got) || msg.timestamp != wantDTS {
							t.Fatalf("message %d: packet type=%d DTS=%d, want type %d DTS %d",
								got, video.AVCPacketType, msg.timestamp, got, wantDTS)
						}
						if got == 1 && video.CompositionTime != 40 {
							t.Fatalf("composition time = %d, want 40", video.CompositionTime)
						}
						got++
						return nil
					})
					if err != nil || got != 2 {
						t.Fatalf("decoded %d RTMP video messages, want 2: %v", got, err)
					}
				})
			}
		}
	}
}
