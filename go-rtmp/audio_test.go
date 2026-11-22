package rtmp

import (
	"bytes"
	"errors"
	"io"
	"reflect"
	"testing"

	"github.com/yapingcat/gomedia/go-codec"
)

// Stereo AAC-LC silence in ADTS, 44.1 kHz and 1024 samples per frame.
var rtmpTestAACFrame = []byte{0xff, 0xf1, 0x50, 0x80, 0x01, 0xbf, 0xfc, 0x21, 0x10, 0x04, 0x60, 0x8c, 0x1c}

type audioTestEndpoint interface {
	Input([]byte) error
	OnFrame(OnFrame)
	SetOutput(OutputCB)
	WriteAudio(codec.CodecID, []byte, uint32, uint32) error
}

func audioTestPair(fromServer bool) (sender, receiver audioTestEndpoint) {
	client, server := NewRtmpClient(), NewRtmpServerHandle()
	client.state, server.state = ReadChunk, ReadChunk
	client.streamId = 1
	if fromServer {
		return server, client
	}
	return client, server
}

func TestRTMPAACBatchPreservesTimestamps(t *testing.T) {
	for _, name := range []string{"client", "server"} {
		t.Run(name, func(t *testing.T) {
			sender, receiver := audioTestPair(name == "server")
			sender.SetOutput(receiver.Input)
			var timestamps []uint32
			receiver.OnFrame(func(cid codec.CodecID, pts, dts uint32, frame []byte) {
				if cid != codec.CODECID_AUDIO_AAC || pts != dts || len(frame) < 7 || !bytes.Equal(frame[7:], rtmpTestAACFrame[7:]) {
					t.Errorf("unexpected received frame %x, codec %d, PTS/DTS %d/%d", frame, cid, pts, dts)
				}
				timestamps = append(timestamps, dts)
			})
			if err := sender.WriteAudio(codec.CODECID_AUDIO_AAC, bytes.Repeat(rtmpTestAACFrame, 3), 100, 100); err != nil {
				t.Fatal(err)
			}
			if want := []uint32{100, 123, 146}; !reflect.DeepEqual(timestamps, want) {
				t.Errorf("receiver DTS = %v ms, want %v ms", timestamps, want)
			}
		})
	}
}

func TestRTMPAACRetriesOutputWithoutLosingConfiguration(t *testing.T) {
	for _, name := range []string{"client", "server"} {
		for _, tc := range []struct {
			tag    string
			failAt int
		}{
			{tag: "configuration", failAt: 1},
			{tag: "raw sample", failAt: 2},
		} {
			t.Run(name+"/"+tc.tag, func(t *testing.T) {
				sender, receiver := audioTestPair(name == "server")
				var timestamps []uint32
				receiver.OnFrame(func(_ codec.CodecID, _, dts uint32, frame []byte) {
					if len(frame) < 7 || !bytes.Equal(frame[7:], rtmpTestAACFrame[7:]) {
						t.Errorf("unexpected AAC payload %x", frame)
					}
					timestamps = append(timestamps, dts)
				})
				calls := 0
				sender.SetOutput(func(data []byte) error {
					calls++
					if calls == tc.failAt {
						return io.ErrClosedPipe
					}
					return receiver.Input(data)
				})
				if err := sender.WriteAudio(codec.CODECID_AUDIO_AAC, rtmpTestAACFrame, 100, 100); !errors.Is(err, io.ErrClosedPipe) {
					t.Fatalf("first WriteAudio = %v, want closed pipe on output %d", err, tc.failAt)
				}
				if err := sender.WriteAudio(codec.CODECID_AUDIO_AAC, rtmpTestAACFrame, 123, 123); err != nil {
					t.Fatalf("retrying WriteAudio: %v", err)
				}
				if !reflect.DeepEqual(timestamps, []uint32{123}) || calls != 3 {
					t.Errorf("receiver DTS = %v, output calls = %d, want one AAC frame at 123 ms with three output calls", timestamps, calls)
				}
			})
		}
	}
}
