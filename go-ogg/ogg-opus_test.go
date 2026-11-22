package ogg

import (
	"encoding/binary"
	"os"
	"reflect"
	"testing"

	"github.com/yapingcat/gomedia/go-codec"
)

func TestOpusAudioParametersUseGranuleClock(t *testing.T) {
	head := []byte{'O', 'p', 'u', 's', 'H', 'e', 'a', 'd', 1, 1, 0x38, 0x01, 0xc0, 0x5d, 0, 0, 0, 0, 0}
	page := makeOggTestPage(2, 0, 0, []byte{byte(len(head))}, head)
	demuxer := NewDemuxer()
	if err := demuxer.Input(page); err != nil {
		t.Fatal(err)
	}
	parameters := demuxer.GetAudioParam()
	if parameters == nil || parameters.SampleRate != 48000 {
		t.Fatalf("Opus parameters = %+v, want 48 kHz", parameters)
	}
	if got := binary.LittleEndian.Uint32(parameters.ExtraData[12:]); got != 24000 {
		t.Errorf("original sample rate in OpusHead = %d, want 24000", got)
	}
}

func TestDemuxerRejectsBadPageChecksumBeforeCallbacks(t *testing.T) {
	head := []byte{'O', 'p', 'u', 's', 'H', 'e', 'a', 'd', 1, 1, 0x38, 0x01, 0x80, 0xbb, 0, 0, 0, 0, 0}
	page := makeOggTestPage(2, 0, 0, []byte{byte(len(head))}, head)
	damaged := append([]byte(nil), page...)
	damaged[22] ^= 1

	demuxer := NewDemuxer()
	var pages, packets int
	demuxer.OnPage = func(*oggPage) { pages++ }
	demuxer.OnPacket = func(uint32, uint64, []byte, int) { packets++ }
	if err := demuxer.Input(damaged[:30]); err != nil {
		t.Fatal(err)
	}
	if err := demuxer.Input(damaged[30:]); err == nil {
		t.Fatal("accepted a page with a bad checksum")
	}
	if pages != 0 || packets != 0 || demuxer.GetAudioParam() != nil {
		t.Fatalf("damaged page reached callbacks: %d pages, %d packets, parameters %+v", pages, packets, demuxer.GetAudioParam())
	}
	if err := demuxer.Input(page); err != nil {
		t.Fatalf("retry with a complete valid page: %v", err)
	}
	if pages != 1 || packets != 1 {
		t.Errorf("valid page reached callbacks %d and %d times, want one each", pages, packets)
	}
}

func TestDemuxerReadsOpusStream(t *testing.T) {
	data, err := os.ReadFile("testdata/opus-short.ogg")
	if err != nil {
		t.Fatal(err)
	}
	demuxer := NewDemuxer()
	var timestamps []uint64
	demuxer.OnFrame = func(_ uint32, cid codec.CodecID, frame []byte, pts, dts uint64, lost int) {
		if cid != codec.CODECID_AUDIO_OPUS || len(frame) == 0 || pts != dts || lost != 0 {
			t.Fatalf("unexpected frame: codec=%d length=%d PTS=%d DTS=%d lost=%d", cid, len(frame), pts, dts, lost)
		}
		timestamps = append(timestamps, pts)
	}
	for i := range data {
		if err := demuxer.Input(data[i : i+1]); err != nil {
			t.Fatalf("Input at byte %d: %v", i, err)
		}
	}
	info := demuxer.GetAudioParam()
	if info == nil || info.CodecId != codec.CODECID_AUDIO_OPUS || info.SampleRate != 48000 || info.ChannelCount != 1 || info.InitialPadding != 312 {
		t.Fatalf("unexpected Opus parameters: %+v", info)
	}
	// FFprobe starts at -312. The unsigned callback clamps this to zero.
	want := []uint64{0, 648, 1608, 2568, 3528, 4488, 5448, 6408, 7368, 8328, 9288}
	if !reflect.DeepEqual(timestamps, want) {
		t.Errorf("Opus PTS = %v, want %v", timestamps, want)
	}
}

func TestOpusFinalPageTimestamps(t *testing.T) {
	packet := []byte{0xf8, 0xff, 0xfe} // 960 samples at 48 kHz.
	for _, tc := range []struct {
		name  string
		pages []oggPage
		want  []uint64
	}{
		{
			name:  "single trimmed page",
			pages: []oggPage{{pageSeq: 2, granulePos: 1800, eos: true, packets: [][]byte{packet, packet}}},
			want:  []uint64{0, 648},
		},
		{
			name:  "single page with an offset",
			pages: []oggPage{{pageSeq: 2, granulePos: 3000, eos: true, packets: [][]byte{packet, packet}}},
			want:  []uint64{768, 1728},
		},
		{
			name: "trimmed page after audio",
			pages: []oggPage{
				{pageSeq: 2, granulePos: 960, packets: [][]byte{packet}},
				{pageSeq: 3, granulePos: 2500, eos: true, packets: [][]byte{packet, packet}},
			},
			want: []uint64{0, 648, 1608},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			opus := createParser(codec.CODECID_AUDIO_OPUS).(*opusDemuxer)
			opus.ctx.Preskip = 312
			var got []uint64
			for i := range tc.pages {
				stream := &oggStream{currentPage: &tc.pages[i]}
				for _, packet := range stream.currentPage.packets {
					_, pts, _ := opus.packet(stream, packet)
					got = append(got, pts)
				}
			}
			if !reflect.DeepEqual(got, tc.want) {
				t.Errorf("Opus PTS = %v, want %v", got, tc.want)
			}
		})
	}
}
