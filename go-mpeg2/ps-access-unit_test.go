package mpeg2

import (
	"bytes"
	"testing"
)

func TestPSMuxerDetectsRandomAccessAfterAUD(t *testing.T) {
	for _, tc := range []struct {
		name string
		cid  PS_STREAM_TYPE
		au   []byte
	}{
		{"AVC", PS_STREAM_H264, []byte{0, 0, 1, 0x09, 0x10, 0, 0, 1, 0x65, 0x88, 0x84, 0x21, 0xa0}},
		{"HEVC", PS_STREAM_H265, []byte{0, 0, 1, 0x46, 0x01, 0x10, 0, 0, 1, 0x26, 0x01, 0xac, 0x80}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			muxer := NewPsMuxer()
			sid := muxer.AddStream(tc.cid)
			demuxer := NewPSDemuxer()
			var systemHeaders, alignedSamples int
			demuxer.OnPacket = func(packet Display, err error) {
				if err != nil {
					t.Fatal(err)
				}
				switch packet := packet.(type) {
				case *System_header:
					systemHeaders++
				case *PesPacket:
					if packet.Data_alignment_indicator == 1 && bytes.Equal(packet.Pes_payload, tc.au) {
						alignedSamples++
					}
				}
			}
			muxer.OnPacket = func(packet []byte) {
				if err := demuxer.Input(packet); err != nil {
					t.Fatal(err)
				}
			}
			for _, timestamp := range []uint64{100, 500} {
				if err := muxer.Write(sid, tc.au, timestamp, timestamp); err != nil {
					t.Fatal(err)
				}
			}
			if systemHeaders != 2 || alignedSamples != 2 {
				t.Errorf("got %d system headers and %d aligned samples, want two of each", systemHeaders, alignedSamples)
			}
		})
	}
}

func TestPSDemuxerFlushDrainsPendingAudio(t *testing.T) {
	audio := bytes.Repeat([]byte{0x55}, 160)
	muxer := NewPsMuxer()
	sid := muxer.AddStream(PS_STREAM_G711A)
	demuxer := NewPSDemuxer()
	muxer.OnPacket = func(packet []byte) {
		if err := demuxer.Input(packet); err != nil {
			t.Fatal(err)
		}
	}
	var timestamps []uint64
	demuxer.OnFrame = func(frame []byte, cid PS_STREAM_TYPE, pts, dts uint64) {
		if !bytes.Equal(frame, audio) || cid != PS_STREAM_G711A || pts != dts {
			t.Errorf("unexpected frame with length %d, codec %d, PTS/DTS %d/%d", len(frame), cid, pts, dts)
		}
		timestamps = append(timestamps, pts)
	}
	for _, timestamp := range []uint64{100, 120} {
		if err := muxer.Write(sid, audio, timestamp, timestamp); err != nil {
			t.Fatal(err)
		}
		demuxer.Flush()
		demuxer.Flush()
	}
	if len(timestamps) != 2 || timestamps[0] != 100 || timestamps[1] != 120 {
		t.Errorf("delivered timestamps = %v, want [100 120]", timestamps)
	}
}
