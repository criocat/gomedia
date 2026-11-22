package codec

import (
	"bytes"
	"reflect"
	"testing"
)

func TestOpusPacketDurationFiveMilliseconds(t *testing.T) {
	// A 5 ms mono packet encoded by libopus.
	packet := []byte{
		0xe8, 0x9f, 0x12, 0x74, 0x91, 0xb7, 0xcd, 0x6d, 0xd0, 0xae, 0x3f, 0xa9, 0x2d, 0xd6, 0xfc, 0xc2,
		0xc8, 0x91, 0xea, 0xb6, 0xcd, 0x9f, 0xef, 0x93, 0x5d, 0x09, 0x58, 0x65, 0x74, 0xcd, 0x11, 0x49,
		0x54, 0xf6, 0x1a, 0xcb, 0x95, 0x8f, 0xc0, 0x1b, 0x2b, 0x67, 0x50, 0x57, 0x2f, 0xfb, 0x5f, 0xea,
		0x80, 0x51, 0x9e, 0x1e, 0x8c, 0x1a, 0xeb, 0x86, 0x3d, 0x0c, 0xc3, 0x54, 0xa6, 0x0f, 0x34, 0x91,
	}
	if got := OpusPacketDuration(packet); got != 240 {
		t.Errorf("5 ms packet duration = %d samples, want 240", got)
	}
}

func TestDecodeOpusPacketSetsDuration(t *testing.T) {
	// A 20 ms mono silence packet encoded by libopus.
	packet := []byte{0xf8, 0xff, 0xfe}
	if got := DecodeOpusPacket(packet).Duration; got != 960 {
		t.Errorf("decoded packet duration = %d samples, want 960", got)
	}
}

func TestDecodeOpusPacketTwoFrameVBR(t *testing.T) {
	for _, tc := range []struct {
		name        string
		frameSize   []byte
		firstLength int
	}{
		{name: "DTX", frameSize: []byte{0}},
		{name: "two-byte length", frameSize: []byte{252, 64}, firstLength: 508},
	} {
		t.Run(tc.name, func(t *testing.T) {
			packet := append([]byte{0xfa}, tc.frameSize...)
			payloadOffset := len(packet)
			packet = append(packet, make([]byte, tc.firstLength)...)
			packet = append(packet, 0xff, 0xfe)
			got := DecodeOpusPacket(packet)
			wantLengths := []uint16{uint16(tc.firstLength), 2}
			if got.FrameCount != 2 || got.Vbr != 1 || !reflect.DeepEqual(got.FrameLen, wantLengths) {
				t.Errorf("frame count = %d, VBR = %d, lengths = %v, want 2 VBR frames with lengths %v",
					got.FrameCount, got.Vbr, got.FrameLen, wantLengths)
			}
			if want := packet[payloadOffset:]; !bytes.Equal(got.Frame, want) {
				t.Errorf("payload length = %d, want %d with matching contents", len(got.Frame), len(want))
			}
		})
	}
}

func TestOpusCode3CountsFortyFrames(t *testing.T) {
	// Forty 2.5 ms CELT DTX frames, as reported by libopus.
	packet := []byte{0x83, 40}
	if got := OpusPacketDuration(packet); got != 4800 {
		t.Errorf("duration = %d samples, want 4800", got)
	}
	decoded := DecodeOpusPacket(packet)
	if decoded.Duration != 4800 || decoded.FrameCount != 40 || len(decoded.Frame) != 0 {
		t.Errorf("unexpected decoded DTX packet: %+v", decoded)
	}
}

func TestOpusCode3ExcludesPadding(t *testing.T) {
	for _, tc := range []struct {
		name    string
		header  []byte
		lengths []uint16
		padding int
	}{
		{name: "CBR", header: []byte{0x41, 10}, lengths: []uint16{3}, padding: 10},
		{name: "VBR", header: []byte{0xc2, 255, 5, 252, 64}, lengths: []uint16{508, 3}, padding: 259},
	} {
		t.Run(tc.name, func(t *testing.T) {
			payloadSize := int(tc.lengths[0])
			if len(tc.lengths) > 1 {
				payloadSize += int(tc.lengths[1])
			}
			payload := bytes.Repeat([]byte{0x3a}, payloadSize)
			packet := append([]byte{0x83}, tc.header...)
			packet = append(packet, payload...)
			packet = append(packet, make([]byte, tc.padding)...)
			got := DecodeOpusPacket(packet)
			if got.FrameCount != len(tc.lengths) || !reflect.DeepEqual(got.FrameLen, tc.lengths) || !bytes.Equal(got.Frame, payload) {
				t.Errorf("frame count = %d, lengths = %v, payload size = %d, want lengths %v and %d matching bytes",
					got.FrameCount, got.FrameLen, len(got.Frame), tc.lengths, len(payload))
			}
		})
	}
}

func TestOpusChannelMapping(t *testing.T) {
	stereo := OpusContext{ChannelCount: 2, SampleRate: 48000}
	var parsedStereo OpusContext
	if err := parsedStereo.ParseExtranData(stereo.WriteOpusExtraData()); err != nil {
		t.Fatal(err)
	}
	wantStereo := []ChannelMap{{ChannelIdx: LEFT_CHANNEL}, {ChannelIdx: RIGHT_CHANNEL}}
	if !reflect.DeepEqual(parsedStereo.ChannelMaps, wantStereo) {
		t.Errorf("stereo channels = %+v, want %+v", parsedStereo.ChannelMaps, wantStereo)
	}

	multichannel := OpusContext{ChannelCount: 3, SampleRate: 48000}
	extra := multichannel.WriteOpusExtraData()
	extra[18] = 1
	// One coupled stream, one uncoupled stream, and a reused uncoupled channel.
	extra = append(extra, 2, 1, 0, 2, 2)
	var parsedMultichannel OpusContext
	if err := parsedMultichannel.ParseExtranData(extra); err != nil {
		t.Fatal(err)
	}
	wantMultichannel := []ChannelMap{{ChannelIdx: LEFT_CHANNEL}, {StreamIdx: 1}, {StreamIdx: 1, Copy: true, CopyFrom: 1}}
	if !reflect.DeepEqual(parsedMultichannel.ChannelMaps, wantMultichannel) {
		t.Errorf("multichannel mapping = %+v, want %+v", parsedMultichannel.ChannelMaps, wantMultichannel)
	}
}

func TestOpusChannelMappingKeepsSilence(t *testing.T) {
	context := OpusContext{ChannelCount: 3, SampleRate: 48000}
	extra := context.WriteOpusExtraData()
	extra[18] = 1
	// One silent output followed by two outputs copying the same mono stream.
	extra = append(extra, 2, 1, 255, 2, 2)
	var parsed OpusContext
	want := []ChannelMap{{Silence: true}, {StreamIdx: 1}, {StreamIdx: 1, Copy: true, CopyFrom: 1}}
	for i := 0; i < 2; i++ {
		if err := parsed.ParseExtranData(extra); err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(parsed.ChannelMaps, want) {
			t.Fatalf("channel mapping = %+v, want %+v", parsed.ChannelMaps, want)
		}
	}
}
