package mp4

import (
	"bytes"
	"errors"
	"io"
	"os"
	"testing"

	"github.com/yapingcat/gomedia/go-codec"
)

func TestMP4ReadsShortVideoNALLengths(t *testing.T) {
	for _, test := range []struct {
		path string
		cid  MP4_CODEC_TYPE
	}{
		{"testdata/avc-2byte.mp4", MP4_CODEC_H264},
		{"testdata/hevc-2byte.mp4", MP4_CODEC_H265},
	} {
		t.Run(test.path, func(t *testing.T) {
			// Each file holds one FFmpeg-encoded sample with two-byte NAL lengths.
			data, err := os.ReadFile(test.path)
			if err != nil {
				t.Fatal(err)
			}
			demuxer := CreateMp4Demuxer(bytes.NewReader(data))
			if _, err := demuxer.ReadHead(); err != nil {
				t.Fatal(err)
			}
			packet, err := demuxer.ReadPacket()
			if err != nil {
				t.Fatal(err)
			}
			foundFrame := false
			codec.SplitFrameWithStartCode(packet.Data, func(nal []byte) bool {
				if packet.Cid == MP4_CODEC_H264 {
					foundFrame = codec.IsH264VCLNaluType(codec.H264NaluType(nal))
				} else {
					foundFrame = codec.IsH265VCLNaluType(codec.H265NaluType(nal))
				}
				return !foundFrame
			})
			if packet.Cid != test.cid || !foundFrame {
				t.Errorf("decoded codec %d without expected Annex B video", packet.Cid)
			}
			if _, err := demuxer.ReadPacket(); !errors.Is(err, io.EOF) {
				t.Errorf("second packet read: %v, want EOF", err)
			}
		})
	}
}

func TestMP4NormalizesImportedHEVCNALLengthsOnWrite(t *testing.T) {
	fixture, err := os.ReadFile("testdata/hevc-2byte.mp4")
	if err != nil {
		t.Fatal(err)
	}
	source := CreateMp4Demuxer(bytes.NewReader(fixture))
	if _, err := source.ReadHead(); err != nil {
		t.Fatal(err)
	}
	config := source.tracks[0].extra.export()
	if config[21]&3 != 1 {
		t.Fatal("input hvcC should declare two-byte lengths")
	}
	frame, err := source.ReadPacket()
	if err != nil {
		t.Fatal(err)
	}

	output := newFmp4WriterSeeker(4096)
	muxer, err := CreateMp4Muxer(output)
	if err != nil {
		t.Fatal(err)
	}
	track := muxer.AddVideoTrack(MP4_CODEC_H265, WithExtraData(config))
	if err := muxer.Write(track, frame.Data, 0, 0); err != nil {
		t.Fatal(err)
	}
	if err := muxer.WriteTrailer(); err != nil {
		t.Fatal(err)
	}

	demuxer := CreateMp4Demuxer(bytes.NewReader(output.buffer))
	if _, err := demuxer.ReadHead(); err != nil {
		t.Fatal(err)
	}
	if length := demuxer.tracks[0].extra.(*h265ExtraData).hvccExtra.LengthSizeMinusOne + 1; length != 4 {
		t.Errorf("output hvcC declares %d-byte lengths, want 4", length)
	}
	if config[21]&3 != 1 {
		t.Error("muxer changed the caller's hvcC")
	}
	packet, err := demuxer.ReadPacket()
	if err != nil || packet.Cid != MP4_CODEC_H265 || !codec.IsH265IDRFrame(packet.Data) {
		t.Errorf("could not read HEVC keyframe after remuxing: %v", err)
	}
}
