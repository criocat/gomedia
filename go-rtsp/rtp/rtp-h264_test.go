package rtp

import (
	"bytes"
	"os"
	"testing"

	"github.com/yapingcat/gomedia/go-codec"
)

func TestH264PackerPreservesAccessUnitWithAUD(t *testing.T) {
	accessUnit, err := os.ReadFile("testdata/single-au.h264")
	if err != nil {
		t.Fatal(err)
	}
	var want [][]byte
	codec.SplitFrame(accessUnit, func(nal []byte) bool {
		want = append(want, nal)
		return true
	})
	if len(want) < 2 || codec.H264NaluTypeWithoutStartCode(want[0]) != codec.H264_NAL_AUD {
		t.Fatal("test access unit must start with AUD")
	}
	packer := NewH264Packer(96, 1, 41, 8192)
	var got [][]byte
	packer.OnPacket(func(data []byte) error {
		var packet RtpPacket
		if err := packet.Decode(data); err != nil {
			return err
		}
		got = append(got, packet.Payload)
		return nil
	})
	if err := packer.Pack(accessUnit, 9000); err != nil {
		t.Fatal(err)
	}
	if len(got) != len(want) {
		t.Fatalf("wrote %d RTP packets, want %d", len(got), len(want))
	}
	for i := range got {
		if !bytes.Equal(got[i], want[i]) {
			t.Errorf("NAL %d payload length = %d, want %d matching bytes", i, len(got[i]), len(want[i]))
		}
	}
}

func TestH264PackerFragmentsOnlyOversizeNALs(t *testing.T) {
	const mtu = 64
	// The first NAL exactly fits in one RTP packet. The second needs fragments.
	small := append([]byte{0x67}, bytes.Repeat([]byte{0x55}, mtu-RTP_FIX_HEAD_LEN-1)...)
	large := append([]byte{0x65}, bytes.Repeat([]byte{0x55}, 180)...)
	accessUnit := append([]byte{0, 0, 0, 1}, small...)
	accessUnit = append(accessUnit, 0, 0, 1)
	accessUnit = append(accessUnit, large...)
	packer := NewH264Packer(96, 1, 41, mtu)
	var packets [][]byte
	packer.OnPacket(func(data []byte) error {
		if len(data) > mtu {
			t.Errorf("RTP packet size = %d, MTU = %d", len(data), mtu)
		}
		var packet RtpPacket
		if err := packet.Decode(data); err != nil {
			return err
		}
		packets = append(packets, packet.Payload)
		return nil
	})
	if err := packer.Pack(accessUnit, 9000); err != nil {
		t.Fatal(err)
	}
	if len(packets) != 5 {
		t.Fatalf("packet count = %d, want one raw NAL and four fragments", len(packets))
	}
	if !bytes.Equal(packets[0], small) {
		t.Errorf("first NAL payload type = %d, want raw SPS", packets[0][0]&31)
	}
	reassembled := []byte{large[0]}
	for i, payload := range packets[1:] {
		if len(payload) < 3 || payload[0]&31 != 28 {
			t.Fatalf("packet %d is missing its FU-A header", i+1)
		}
		want := []byte{0x80, 0, 0, 0x40}[i]
		if got := payload[1] & 0xc0; got != want {
			t.Errorf("fragment %d Start/End = %#x, want %#x", i, got, want)
		}
		reassembled = append(reassembled, payload[2:]...)
	}
	if !bytes.Equal(reassembled, large) {
		t.Error("RTP fragments did not reconstruct the original NAL")
	}
}
