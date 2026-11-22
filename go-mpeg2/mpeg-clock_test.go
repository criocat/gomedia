package mpeg2

import (
	"bytes"
	"reflect"
	"testing"

	"github.com/yapingcat/gomedia/go-codec"
)

func TestPESClockReference(t *testing.T) {
	const base = uint64(0x1a13579bd)
	const extension = uint16(0x110)
	wire := codec.NewBitStreamWriter(32)
	wire.PutBytes([]byte{0, 0, 1, 0xe0, 0, 10, 0x80, 0x20, 6})
	wire.PutUint8(3, 2) // Reserved bits.
	wire.PutUint64(base>>30, 3)
	wire.PutUint8(1, 1)
	wire.PutUint64(base>>15, 15)
	wire.PutUint8(1, 1)
	wire.PutUint64(base, 15)
	wire.PutUint8(1, 1)
	wire.PutUint16(extension, 9)
	wire.PutUint8(1, 1)
	wire.PutByte(0xff)

	decoded := NewPesPacket()
	if err := decoded.Decode(codec.NewBitStream(wire.Bits())); err != nil {
		t.Fatal(err)
	}
	if decoded.ESCR_base != base || decoded.ESCR_extension != extension || !bytes.Equal(decoded.Pes_payload, []byte{0xff}) {
		t.Errorf("clock reference = %#x/%#x, payload = %x, want %#x/%#x and ff",
			decoded.ESCR_base, decoded.ESCR_extension, decoded.Pes_payload, base, extension)
	}
	decoded.ESCR_base = base
	decoded.ESCR_extension = extension
	encoded := codec.NewBitStreamWriter(32)
	decoded.Encode(encoded)
	if !bytes.Equal(encoded.Bits(), wire.Bits()) {
		t.Errorf("encoded PES = %x, want %x", encoded.Bits(), wire.Bits())
	}
}

func TestTSMuxerPublishesPCRPIDBeforeSwitchingClock(t *testing.T) {
	muxer := NewTSMuxer()
	audio := muxer.AddStream(TS_STREAM_AAC)
	video := muxer.AddStream(TS_STREAM_H264)
	pmtPID := muxer.pat.pmts[0].pid
	var advertisedPID uint16
	var versions []uint8
	var clockPIDs []uint16
	muxer.OnPacket = func(data []byte) {
		pid := uint16(data[1]&0x1f)<<8 | uint16(data[2])
		if pid == pmtPID {
			start := 5 + int(data[4])
			versions = append(versions, (data[start+5]>>1)&31)
			advertisedPID = uint16(data[start+8]&31)<<8 | uint16(data[start+9])
		}
		if data[3]&0x20 != 0 && data[4] > 0 && data[5]&0x10 != 0 {
			clockPIDs = append(clockPIDs, pid)
			if advertisedPID != pid {
				t.Errorf("PCR on PID %d while PMT advertises %d", pid, advertisedPID)
			}
		}
	}
	adtsFrame := []byte{0xff, 0xf1, 0x4c, 0x80, 0x01, 0xbf, 0xfc, 0x21, 0x10, 0x04, 0x60, 0x8c, 0x1c}
	annexB := []byte{0, 0, 0, 1, 0x09, 0x10, 0, 0, 1, 0x65, 0x88, 0x84, 0x21, 0xa0}
	for _, sample := range []struct {
		pid  uint16
		data []byte
		dts  uint64
	}{{audio, adtsFrame, 0}, {video, annexB, 100}, {audio, adtsFrame, 120}, {video, annexB, 140}} {
		if err := muxer.Write(sample.pid, sample.data, sample.dts, sample.dts); err != nil {
			t.Fatal(err)
		}
	}
	if want := []uint16{audio, video, video}; !reflect.DeepEqual(clockPIDs, want) {
		t.Errorf("PCR PIDs = %v, want %v", clockPIDs, want)
	}
	if want := []uint8{0, 1}; !reflect.DeepEqual(versions, want) {
		t.Errorf("PMT versions = %v, want %v", versions, want)
	}
}
