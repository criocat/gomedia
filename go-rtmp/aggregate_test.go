package rtmp

import (
	"encoding/binary"
	"reflect"
	"strings"
	"testing"

	"github.com/yapingcat/gomedia/go-codec"
	"github.com/yapingcat/gomedia/go-flv"
)

func TestRTMPAggregateDeliversAudioAndVideo(t *testing.T) {
	const childBase = 0x01fffff0
	const aggregateDTS = 1000
	var payload []byte
	for _, child := range []struct {
		kind   MessageType
		offset uint32
		data   []byte
	}{
		{Metadata_AMF0, 0, append(makeStringItem("onMetaData").encode(), makeStringItem(strings.Repeat("a", 128)).encode()...)},
		{AUDIO, 0, flv.WriteAudioTag([]byte{0x12, 0x10}, flv.FLV_AAC, 0, 0, true)},
		{AUDIO, 0, flv.WriteAudioTag(rtmpTestAACFrame[7:], flv.FLV_AAC, 0, 0, false)},
		{VIDEO, 12, flv.WriteVideoTag([]byte{0, 0, 0, 2, 0x65, 0x80}, true, flv.FLV_AVC, -5, false)},
		{AUDIO, 23, flv.WriteAudioTag(rtmpTestAACFrame[7:], flv.FLV_AAC, 0, 0, false)},
	} {
		timestamp := childBase + child.offset
		header := flv.FlvTag{TagType: uint8(child.kind), DataSize: uint32(len(child.data)),
			Timestamp: timestamp & 0x00ffffff, TimestampExtended: uint8(timestamp >> 24)}
		payload = append(payload, header.Encode()...)
		payload = append(payload, child.data...)
		var backPointer [4]byte
		binary.BigEndian.PutUint32(backPointer[:], uint32(11+len(child.data)))
		payload = append(payload, backPointer[:]...)
	}
	type timing struct {
		codec    codec.CodecID
		pts, dts uint32
	}
	want := []timing{
		{codec.CODECID_AUDIO_AAC, 1000, 1000},
		{codec.CODECID_VIDEO_H264, 1007, 1012},
		{codec.CODECID_AUDIO_AAC, 1023, 1023},
	}
	for _, name := range []string{"client", "server"} {
		t.Run(name, func(t *testing.T) {
			_, receiver := audioTestPair(name == "client")
			var got []timing
			receiver.OnFrame(func(cid codec.CodecID, pts, dts uint32, _ []byte) {
				got = append(got, timing{cid, pts, dts})
			})
			writer := newChunkStreamWriter(CHUNK_CHANNEL_NET_STREAM)
			packet := writer.writeData(payload, Aggregate, 1, aggregateDTS)
			for i := range packet {
				if err := receiver.Input(packet[i : i+1]); err != nil {
					t.Fatal(err)
				}
			}
			if !reflect.DeepEqual(got, want) {
				t.Errorf("received frames %+v, want %+v", got, want)
			}
		})
	}
}
