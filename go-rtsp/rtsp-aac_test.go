package rtsp

import (
	"reflect"
	"testing"

	"github.com/yapingcat/gomedia/go-codec"
	"github.com/yapingcat/gomedia/go-rtsp/rtp"
	"github.com/yapingcat/gomedia/go-rtsp/sdp"
)

func TestRTSPAACUsesNegotiatedAccessUnitTiming(t *testing.T) {
	param := sdp.NewAACFmtpParam()
	param.Load("96 mode=AAC-hbr;config=1210;sizeLength=13;indexLength=3;indexDeltaLength=2;constantDuration=600")
	track := NewAudioTrack(NewAudioCodec("MPEG4-GENERIC", 96, 44100, 2), WithCodecParamHandler(param))
	unpacker := track.createUnpacker()
	var got []uint32
	unpacker.OnFrame(func(_ []byte, timestamp uint32, _ bool) { got = append(got, timestamp) })
	frame := []byte{0x21, 0x10, 0x04, 0x60, 0x8c, 0x1c}
	headers := codec.NewBitStreamWriter(16)
	headers.PutUint16(31, 16)
	headers.PutUint16(uint16(len(frame)), 13)
	headers.PutUint8(5, 3) // Initial AU index does not change the RTP base timestamp.
	headers.PutUint16(uint16(len(frame)), 13)
	headers.PutUint8(1, 2) // The next AU is two duration ticks later.
	payload := append(headers.Bits(), frame...)
	payload = append(payload, frame...)
	start := ^uint32(0) - 700
	packet := rtp.RtpPacket{Header: rtp.RtpHdr{Timestamp: start}, Payload: payload}
	if err := unpacker.UnPack(packet.Encode()); err != nil {
		t.Fatal(err)
	}
	if want := []uint32{start, 499}; !reflect.DeepEqual(got, want) {
		t.Errorf("AAC timestamps = %v, want %v", got, want)
	}
}
