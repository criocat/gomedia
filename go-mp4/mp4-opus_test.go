package mp4

import (
	"bytes"
	"encoding/binary"
	"os"
	"testing"
)

func TestMP4ReadsOpusTrack(t *testing.T) {
	file, err := os.ReadFile("testdata/opus-mono.mp4")
	if err != nil {
		t.Fatal(err)
	}
	check := func(t *testing.T, data []byte) {
		demuxer := CreateMp4Demuxer(bytes.NewReader(data))
		tracks, err := demuxer.ReadHead()
		if err != nil {
			t.Fatal(err)
		}
		if len(tracks) != 1 || tracks[0].Cid != MP4_CODEC_OPUS || tracks[0].SampleRate != 48000 || tracks[0].ChannelCount != 1 {
			t.Fatalf("tracks = %+v, want a mono Opus track at 48000 Hz", tracks)
		}
		packet, err := demuxer.ReadPacket()
		if err != nil {
			t.Fatal(err)
		}
		if packet.Cid != MP4_CODEC_OPUS || len(packet.Data) == 0 {
			t.Errorf("first packet has codec %d and size %d", packet.Cid, len(packet.Data))
		}
	}
	t.Run("standard FFmpeg sample entry", func(t *testing.T) { check(t, file) })
	t.Run("lowercase legacy sample entry", func(t *testing.T) {
		legacy := append([]byte(nil), file...)
		pos := bytes.Index(legacy, []byte("Opus"))
		if pos < 4 {
			t.Fatal("fixture lacks Opus sample entry")
		}
		legacy[pos] = 'o'
		binary.BigEndian.PutUint16(legacy[pos+20:], 2) // Legacy entry disagrees with dOps.
		check(t, legacy)
	})
}

func TestMP4InfersOpusChannelCountFromPackets(t *testing.T) {
	for _, tc := range []struct {
		name     string
		packet   []byte
		channels uint8
	}{
		{"mono", []byte{0xf8, 0xff, 0xfe}, 1},
		{"stereo", []byte{0xfc, 0xff, 0xfe}, 2},
	} {
		t.Run(tc.name, func(t *testing.T) {
			output := newFmp4WriterSeeker(4096)
			muxer, err := CreateMp4Muxer(output)
			if err != nil {
				t.Fatal(err)
			}
			id := muxer.AddAudioTrack(MP4_CODEC_OPUS)
			for _, ts := range []uint64{0, 20} {
				if err := muxer.Write(id, tc.packet, ts, ts); err != nil {
					t.Fatal(err)
				}
			}
			if got := muxer.tracks[id].chanelCount; got != tc.channels {
				t.Errorf("muxer channel count = %d, want %d", got, tc.channels)
			}
			if err := muxer.WriteTrailer(); err != nil {
				t.Fatal(err)
			}
			if !bytes.Contains(output.buffer, []byte("dOps")) {
				t.Error("Opus track lacks dOps")
			}
			demuxer := CreateMp4Demuxer(bytes.NewReader(output.buffer))
			tracks, err := demuxer.ReadHead()
			if err != nil {
				t.Fatal(err)
			}
			if len(tracks) != 1 || tracks[0].ChannelCount != tc.channels || tracks[0].SampleRate != 48000 {
				t.Errorf("tracks = %+v, want %d Opus channels at 48000 Hz", tracks, tc.channels)
			}
		})
	}
}
