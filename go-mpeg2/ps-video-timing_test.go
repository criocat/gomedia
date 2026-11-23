package mpeg2

import (
	"bytes"
	"testing"

	"github.com/yapingcat/gomedia/go-codec"
)

func TestPSDemuxerKeepsTimestampForSplitVideoNALStartCode(t *testing.T) {
	for _, fixture := range tsVideoFixtures {
		t.Run(fixture.name, func(t *testing.T) {
			frames := fixture.frames(t)
			for _, split := range []int{1, 2, 3} {
				got := demuxPSVideo(t, PS_STREAM_TYPE(fixture.cid), []tsVideoFrame{
					{frames[0], 120, 80},
					{frames[1][:split], 160, 120},
					{bytes.Join([][]byte{frames[1][split:], frames[2]}, nil), 200, 160},
				})
				if len(got) < 3 {
					t.Fatalf("split %d: received %d NAL units", split, len(got))
				}
				for i, nalu := range got[len(got)-2:] {
					pts, dts := uint64(160+40*i), uint64(120+40*i)
					if !bytes.Equal(nalu.data, frames[i+1]) || nalu.pts != pts || nalu.dts != dts {
						t.Errorf("split %d: NAL %d at %d/%d, want original bytes at %d/%d",
							split, i, nalu.pts, nalu.dts, pts, dts)
					}
				}
			}
		})
	}
}

func TestPSDemuxerSkipsTrailingVideoAUDOnFlush(t *testing.T) {
	for _, fixture := range tsVideoFixtures {
		t.Run(fixture.name, func(t *testing.T) {
			frame := fixture.frames(t)[0]
			aud := H264_AUD_NALU
			if fixture.cid == TS_STREAM_H265 {
				aud = H265_AUD_NALU
			}
			got := demuxPSVideo(t, PS_STREAM_TYPE(fixture.cid), []tsVideoFrame{
				{frame, 120, 80},
				{aud, 160, 120},
			})
			var video []byte
			for _, nalu := range got {
				video = append(video, nalu.data...)
			}
			if !bytes.Equal(video, frame) {
				t.Errorf("video = %x, want %x", video, frame)
			}
		})
	}
}

func demuxPSVideo(t *testing.T, cid PS_STREAM_TYPE, chunks []tsVideoFrame) []tsVideoFrame {
	t.Helper()
	muxer := NewPsMuxer()
	sid := muxer.AddStream(cid)
	demuxer := NewPSDemuxer()
	muxer.OnPacket = func(packet []byte) {
		if err := demuxer.Input(packet); err != nil {
			t.Fatal(err)
		}
	}
	var frames []tsVideoFrame
	demuxer.OnFrame = func(data []byte, got PS_STREAM_TYPE, pts, dts uint64) {
		if got != cid {
			t.Errorf("video codec = %d, want %d", got, cid)
		}
		frames = append(frames, tsVideoFrame{append([]byte(nil), data...), pts, dts})
	}
	first := chunks[0]
	if err := muxer.Write(sid, first.data, first.pts, first.dts); err != nil {
		t.Fatal(err)
	}
	for _, chunk := range chunks[1:] {
		pes := NewPesPacket()
		pes.Stream_id = sid
		pes.PTS_DTS_flags, pes.PES_header_data_length = 3, 10
		pes.Pts, pes.Dts = chunk.pts*90, chunk.dts*90
		pes.Pes_payload = chunk.data
		pes.PES_packet_length = uint16(13 + len(chunk.data))
		wire := codec.NewBitStreamWriter(128)
		pes.Encode(wire)
		if err := demuxer.Input(wire.Bits()); err != nil {
			t.Fatal(err)
		}
	}
	demuxer.Flush()
	return frames
}
