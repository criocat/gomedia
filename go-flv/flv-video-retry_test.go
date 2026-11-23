package flv

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"os"
	"testing"
)

type rejectFLVVideoTag struct {
	bytes.Buffer
	calls  int
	failAt int
}

func (w *rejectFLVVideoTag) Write(p []byte) (int, error) {
	w.calls++
	if w.calls == w.failAt {
		return 0, io.ErrClosedPipe
	}
	return w.Buffer.Write(p)
}

func TestFLVVideoWriteRetry(t *testing.T) {
	h264, err := os.ReadFile("../go-rtsp/rtp/testdata/single-au.h264")
	if err != nil {
		t.Fatal(err)
	}

	for _, tc := range []struct {
		name  string
		frame []byte
		write func(*FlvWriter, []byte, uint32, uint32) error
	}{
		{"H264", h264, (*FlvWriter).WriteH264},
		{"HEVC", testHEVCFirstFrame(t), (*FlvWriter).WriteH265},
	} {
		for _, failAt := range []int{1, 2} {
			t.Run(fmt.Sprintf("%s/tag_%d", tc.name, failAt), func(t *testing.T) {
				var dst rejectFLVVideoTag
				w := CreateFlvWriter(&dst)
				const headerSize = 13
				if err := w.WriteFlvHeader(); err != nil {
					t.Fatal(err)
				}
				dst.failAt = dst.calls + failAt

				if err := tc.write(w, tc.frame, 1040, 1000); !errors.Is(err, io.ErrClosedPipe) {
					t.Fatalf("first write error = %v, want closed pipe", err)
				}
				if err := tc.write(w, tc.frame, 1120, 1080); err != nil {
					t.Fatalf("retry: %v", err)
				}

				data := dst.Bytes()[headerSize:]
				for i := 0; i < 2; i++ {
					if len(data) < 11 {
						t.Fatalf("missing FLV tag %d", i)
					}
					var tag FlvTag
					tag.Decode(data)
					tagSize := 11 + int(tag.DataSize)
					if tagSize+4 > len(data) || tag.DataSize < 5 {
						t.Fatalf("truncated FLV tag %d", i)
					}
					if size := binary.BigEndian.Uint32(data[tagSize:]); size != uint32(tagSize) {
						t.Fatalf("previous tag size = %d, want %d", size, tagSize)
					}

					var video VideoTag
					video.Decode(data[11:tagSize])
					wantDTS := uint32(1080)
					if i == 0 && failAt == 2 {
						wantDTS = 1000
					}
					dts := uint32(tag.TimestampExtended)<<24 | tag.Timestamp
					if tag.TagType != uint8(VIDEO_TAG) || video.AVCPacketType != uint8(i) || dts != wantDTS {
						t.Fatalf("tag %d: type=%d packet=%d DTS=%d, want video packet %d DTS %d",
							i, tag.TagType, video.AVCPacketType, dts, i, wantDTS)
					}
					if i == 1 && video.CompositionTime != 40 {
						t.Fatalf("composition time = %d, want 40", video.CompositionTime)
					}
					data = data[tagSize+4:]
				}
				if len(data) != 0 {
					t.Fatalf("unexpected %d bytes after the coded frame", len(data))
				}
			})
		}
	}
}
