package mp4

import (
	"bytes"
	"io"
	"os"
	"testing"
)

func TestMP4RawSampleUsesSelectedTrackEncryption(t *testing.T) {
	for _, clear := range []bool{false, true} {
		demuxer := CreateMp4Demuxer(bytes.NewReader([]byte{1, 2}))
		demuxer.mp4Info.Timescale = 1000
		demuxer.readSampleIdx = make([]uint32, 2)
		demuxer.tracks = []*mp4track{
			{
				trackId: 1, timescale: 1000, cid: MP4_CODEC_G711A,
				samplelist: []sampleEntry{{dts: 0, offset: 0, size: 1}},
				subSamples: []sencEntry{{iv: bytes.Repeat([]byte{0x11}, 8)}},
				defaultKID: [16]byte{0x31},
			},
			{
				trackId: 2, timescale: 48000, cid: MP4_CODEC_G711A,
				samplelist: []sampleEntry{{dts: 480, offset: 1, size: 1}},
				subSamples: []sencEntry{{iv: bytes.Repeat([]byte{0x22}, 16)}},
				defaultKID: [16]byte{0x32},
			},
		}
		if clear {
			demuxer.tracks[0].subSamples = nil
		}
		var metadata *SubSample
		demuxer.OnRawSample = func(_ MP4_CODEC_TYPE, _ []byte, sample *SubSample) error {
			metadata = sample
			return nil
		}
		first, err := demuxer.ReadPacket()
		if err != nil || first.TrackId != 1 {
			t.Fatalf("first sample = %+v: %v", first, err)
		}
		if clear {
			if metadata != nil {
				t.Error("clear first sample received encryption metadata")
			}
		} else if metadata == nil || metadata.IV[0] != 0x11 || metadata.IV[8] != 0 || metadata.KID[0] != 0x31 {
			t.Errorf("first sample metadata = %+v, want track 1 IV and KID", metadata)
		}
		second, err := demuxer.ReadPacket()
		if err != nil || second.TrackId != 2 {
			t.Fatalf("second sample = %+v: %v", second, err)
		}
		if metadata == nil || metadata.IV[15] != 0x22 || metadata.KID[0] != 0x32 {
			t.Errorf("second sample metadata = %+v, want track 2 IV and KID", metadata)
		}
	}
}

func TestNonFragmentedMP4ReadsCENCMetadata(t *testing.T) {
	data, err := os.ReadFile("testdata/cenc-aac.mp4")
	if err != nil {
		t.Fatal(err)
	}
	demuxer := CreateMp4Demuxer(bytes.NewReader(data))
	var received int
	var previousIV [16]byte
	demuxer.OnRawSample = func(cid MP4_CODEC_TYPE, data []byte, metadata *SubSample) error {
		if cid != MP4_CODEC_AAC || len(data) == 0 || metadata == nil {
			t.Fatalf("sample %d: codec=%d, size=%d, metadata=%+v", received, cid, len(data), metadata)
		}
		if metadata.KID != [16]byte{1} || metadata.IV == previousIV || int(metadata.Number) != received {
			t.Errorf("sample %d: invalid IV, KID, or sample index: %+v", received, metadata)
		}
		previousIV = metadata.IV
		received++
		return nil
	}
	tracks, err := demuxer.ReadHead()
	if err != nil {
		t.Fatal(err)
	}
	if len(tracks) != 1 || tracks[0].Cid != MP4_CODEC_AAC || tracks[0].SampleCount != 11 {
		t.Fatalf("tracks = %+v, want one AAC track with 11 samples", tracks)
	}
	for {
		if _, err := demuxer.ReadPacket(); err == io.EOF {
			break
		} else if err != nil {
			t.Fatal(err)
		}
	}
	if received != 11 {
		t.Errorf("received %d CENC records, want 11", received)
	}
}
