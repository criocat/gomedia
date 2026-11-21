package rtmp

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"testing"

	"github.com/yapingcat/gomedia/go-codec"
	"github.com/yapingcat/gomedia/go-flv"
)

func TestHEVCInvalidRTMPHeaders(t *testing.T) {
	server := NewRtmpServerHandle()
	client := NewRtmpClient()
	for _, data := range [][]byte{nil, {0x90}, {0x90, 'h', 'v', 'c'}, {0x90, 'a', 'v', '0', '1'}} {
		msg := &rtmpMessage{msg: data}
		if server.handleVideoMessage(msg) == nil || client.handleVideoMessage(msg) == nil {
			t.Fatalf("accepted malformed header: %x", data)
		}
	}
}

func TestHEVCRejectCodecSwitch(t *testing.T) {
	// A new publisher may choose a different codec in a new session. A codec
	// switch inside one session must not feed HEVC data into the AVC parser.
	for _, initialHEVC := range []bool{false, true} {
		server := NewRtmpServerHandle()
		client := NewRtmpClient()
		avcEnd := &rtmpMessage{msg: []byte{0x17, 2, 0, 0, 0}}
		hevcEnd := &rtmpMessage{msg: []byte{0x92, 'h', 'v', 'c', '1'}}
		first, second := avcEnd, hevcEnd
		if initialHEVC {
			first, second = second, first
		}
		for _, handle := range []func(*rtmpMessage) error{server.handleVideoMessage, client.handleVideoMessage} {
			if err := handle(first); err != nil {
				t.Fatal(err)
			}
			if err := handle(second); err == nil {
				t.Fatalf("accepted codec switch; initialHEVC=%v", initialHEVC)
			}
		}
	}
}

func TestHEVCCompositionTimeAcrossChunks(t *testing.T) {
	// Exercise the actual client/server chunk encoders and decoders in both
	// directions, including a negative CTS.
	for _, serverToClient := range []bool{false, true} {
		server := NewRtmpServerHandle()
		client := NewRtmpClient()
		server.state, client.state = ReadChunk, ReadChunk
		server.SetOutput(func(data []byte) error { return client.Input(data) })
		client.SetOutput(func(data []byte) error { return server.Input(data) })
		var gotPTS, gotDTS uint32
		count := 0
		onFrame := func(cid codec.CodecID, pts, dts uint32, frame []byte) {
			count++
			gotPTS, gotDTS = pts, dts
			if cid != codec.CODECID_VIDEO_H265 {
				t.Fatalf("wrong codec: %v", cid)
			}
		}
		server.OnFrame(onFrame)
		client.OnFrame(onFrame)
		// Supply a valid hvcC through the low-level chunk writer, frame is an
		// ordinary two-byte HEVC NAL, enough to test transport without decoding.
		config := make([]byte, 23)
		config[0], config[21] = 1, 3
		config = append([]byte{0x90, 'h', 'v', 'c', '1'}, config...)
		frame := []byte{0xa1, 'h', 'v', 'c', '1', 0xff, 0xff, 0xd8, 0, 0, 0, 2, 2, 1}
		for i := 0; i < 100; i++ {
			frame = append(frame, 0, 0, 0, 2, 2, 1)
		}
		writer := newChunkStreamWriter(CHUNK_CHANNEL_VIDEO)
		write := func(data []byte, dts uint32) error {
			wire := writer.writeData(data, VIDEO, 1, dts)
			// A TCP read can stop at any byte of this multi-chunk frame.
			for i := range wire {
				var err error
				if serverToClient {
					err = client.Input(wire[i : i+1])
				} else {
					err = server.Input(wire[i : i+1])
				}
				if err != nil {
					return err
				}
			}
			return nil
		}
		if err := write(config, 0); err != nil {
			t.Fatal(err)
		}
		const boundaryDTS = 200 + 0xffffff
		const extendedDelta = 0x01000000
		// Reuse an extended delta in fmt 3, then cross the uint32 wrap.
		for i, dts := range []uint32{80, 120, 160, 200, boundaryDTS, boundaryDTS + extendedDelta,
			boundaryDTS + 2*extendedDelta, 0xffffffe0, 1000} {
			if err := write(frame, dts); err != nil {
				t.Fatal(err)
			}
			if count != i+1 || gotDTS != dts || gotPTS != dts-40 {
				t.Fatalf("direction=%v: count=%d PTS=%d DTS=%d, want DTS=%d", serverToClient, count, gotPTS, gotDTS, dts)
			}
		}
	}
}

func TestHEVCWriteFrameRoundTrip(t *testing.T) {
	type mediaFrame struct {
		cid      codec.CodecID
		pts, dts uint32
		data     []byte
	}
	input, err := os.ReadFile("../go-flv/testdata/hevc-aac.flv")
	if err != nil {
		t.Fatal(err)
	}
	var source []mediaFrame
	reader := flv.CreateFlvReader()
	reader.OnFrame = func(cid codec.CodecID, data []byte, pts, dts uint32) {
		source = append(source, mediaFrame{cid, pts, dts, append([]byte(nil), data...)})
	}
	if err := reader.Input(input); err != nil {
		t.Fatal(err)
	}
	if len(source) < 10 {
		t.Fatal("empty test fixture")
	}
	for _, serverToClient := range []bool{false, true} {
		for _, chunkSize := range []uint32{128, 257, 60000} {
			for _, fragmentSize := range []int{1, 13, 4096} {
				name := fmt.Sprintf("serverToClient=%v/chunk=%d/fragment=%d", serverToClient, chunkSize, fragmentSize)
				t.Run(name, func(t *testing.T) {
					server := NewRtmpServerHandle()
					client := NewRtmpClient(WithChunkSize(chunkSize))
					// Bypass negotiation here, the external smoke test covers the
					// handshake. Exercise public WriteFrame/Input and both codecs.
					server.state, client.state = ReadChunk, ReadChunk
					server.writeChunkSize = chunkSize
					server.reader.chunkSize, client.reader.chunkSize = chunkSize, chunkSize
					var received []mediaFrame
					onFrame := func(cid codec.CodecID, pts, dts uint32, data []byte) {
						// Deliberately retain the callback slice without copying it.
						received = append(received, mediaFrame{cid, pts, dts, data})
					}
					server.OnFrame(onFrame)
					client.OnFrame(onFrame)
					feed := server.Input
					write := client.WriteFrame
					if serverToClient {
						feed, write = client.Input, server.WriteFrame
					}
					output := func(data []byte) error {
						for len(data) > 0 {
							n := fragmentSize
							if n > len(data) {
								n = len(data)
							}
							if err := feed(data[:n]); err != nil {
								return err
							}
							data = data[n:]
						}
						return nil
					}
					if serverToClient {
						server.SetOutput(output)
						client.SetOutput(func([]byte) error { return nil })
					} else {
						client.SetOutput(output)
						server.SetOutput(func([]byte) error { return nil })
					}
					for _, frame := range source {
						payload := append([]byte(nil), frame.data...)
						if err := write(frame.cid, payload, frame.pts, frame.dts); err != nil {
							t.Fatal(err)
						}
						if !bytes.Equal(payload, frame.data) {
							t.Fatal("WriteFrame mutated its input")
						}
					}
					if len(received) != len(source) {
						t.Fatalf("lost frames: %d != %d", len(received), len(source))
					}
					for i, want := range source {
						got := received[i]
						if got.cid != want.cid || got.pts != want.pts || got.dts != want.dts || !bytes.Equal(got.data, want.data) {
							t.Fatalf("frame %d differs: codec %v/%v, PTS %d/%d, DTS %d/%d, bytes %d/%d", i,
								got.cid, want.cid, got.pts, want.pts, got.dts, want.dts, len(got.data), len(want.data))
						}
					}
				})
			}
		}
	}
}

func TestHEVCWriteFrameRejectCodecSwitch(t *testing.T) {
	for _, serverSide := range []bool{false, true} {
		for _, startHEVC := range []bool{false, true} {
			server, client := NewRtmpServerHandle(), NewRtmpClient()
			server.SetOutput(func([]byte) error { return nil })
			client.SetOutput(func([]byte) error { return nil })
			write := client.WriteFrame
			if serverSide {
				write = server.WriteFrame
			}
			first, second := codec.CODECID_VIDEO_H264, codec.CODECID_VIDEO_H265
			avc, hevc := []byte{0, 0, 0, 1, 0x65, 0x80}, []byte{0, 0, 0, 1, 2, 1, 0x80}
			before, after := avc, hevc
			if startHEVC {
				first, second, before, after = second, first, hevc, avc
			}
			if err := write(first, before, 0, 0); err != nil {
				t.Fatal(err)
			}
			snapshot := append([]byte(nil), after...)
			if err := write(second, after, 40, 40); err == nil {
				t.Fatalf("server=%v startHEVC=%v: outgoing codec switch accepted", serverSide, startHEVC)
			}
			if !bytes.Equal(after, snapshot) {
				t.Fatal("rejected frame was mutated")
			}
		}
	}
}

func TestRTMPWriteVideoRejectUnsupportedCodec(t *testing.T) {
	server, client := NewRtmpServerHandle(), NewRtmpClient()
	for _, endpoint := range []struct {
		name      string
		write     func(codec.CodecID, []byte, uint32, uint32) error
		setOutput func(OutputCB)
	}{
		{"client", client.WriteVideo, client.SetOutput},
		{"server", server.WriteVideo, server.SetOutput},
	} {
		t.Run(endpoint.name, func(t *testing.T) {
			writes := 0
			endpoint.setOutput(func([]byte) error {
				writes++
				return nil
			})
			reject := func() {
				t.Helper()
				before := writes
				if err := endpoint.write(codec.CODECID_UNRECOGNIZED, nil, 0, 0); err == nil || err.Error() != "unsupported outgoing video codec" {
					t.Fatalf("unsupported video codec: got %v", err)
				}
				if writes != before {
					t.Fatal("unsupported video codec produced output")
				}
			}
			reject()
			frame := []byte{0, 0, 0, 1, 0x65, 0x80}
			if err := endpoint.write(codec.CODECID_VIDEO_H264, frame, 0, 0); err != nil {
				t.Fatalf("valid video codec after rejection: %v", err)
			}
			reject()
		})
	}
}

func TestHEVCWriteFrameOutputError(t *testing.T) {
	data, err := os.ReadFile("../go-flv/testdata/hevc-aac.flv")
	if err != nil {
		t.Fatal(err)
	}
	var video []byte
	r := flv.CreateFlvReader()
	r.OnFrame = func(cid codec.CodecID, frame []byte, _, _ uint32) {
		if cid == codec.CODECID_VIDEO_H265 && video == nil {
			video = append([]byte(nil), frame...)
		}
	}
	if err := r.Input(data); err != nil {
		t.Fatal(err)
	}
	if video == nil {
		t.Fatal("missing HEVC frame")
	}
	for _, serverSide := range []bool{false, true} {
		for _, failAt := range []int{1, 2} {
			server, client := NewRtmpServerHandle(), NewRtmpClient()
			want := errors.New("test output failure")
			called := 0
			output := func([]byte) error {
				called++
				if called == failAt {
					return want
				}
				return nil
			}
			server.SetOutput(output)
			client.SetOutput(output)
			write := client.WriteFrame
			if serverSide {
				write = server.WriteFrame
			}
			if err := write(codec.CODECID_VIDEO_H265, video, 200, 0); !errors.Is(err, want) || called != failAt {
				t.Fatalf("HEVC output failure was lost: err=%v calls=%d failAt=%d", err, called, failAt)
			}
		}
	}
}
