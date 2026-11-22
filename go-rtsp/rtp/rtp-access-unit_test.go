package rtp

import (
	"bytes"
	"os"
	"testing"

	"github.com/yapingcat/gomedia/go-codec"
)

func TestRTPVideoMarksOnlyLastPacketOfAccessUnit(t *testing.T) {
	for _, tc := range []struct {
		fixture   string
		newPacker func(mtu int) Packer
	}{
		{fixture: "single-au.h264", newPacker: func(mtu int) Packer { return NewH264Packer(96, 1, 41, mtu) }},
		{fixture: "single-au.h265", newPacker: func(mtu int) Packer { return NewH265Packer(97, 1, 41, mtu) }},
	} {
		t.Run(tc.fixture, func(t *testing.T) {
			accessUnit, err := os.ReadFile("testdata/" + tc.fixture)
			if err != nil {
				t.Fatal(err)
			}
			for _, mtu := range []int{100, 8192} {
				var markers []uint8
				packer := tc.newPacker(mtu)
				packer.OnPacket(func(data []byte) error {
					var packet RtpPacket
					if err := packet.Decode(data); err != nil {
						return err
					}
					markers = append(markers, packet.Header.Marker)
					return nil
				})
				if err := packer.Pack(accessUnit, 9000); err != nil {
					t.Fatal(err)
				}
				if len(markers) < 2 || bytes.Count(markers, []byte{1}) != 1 || markers[len(markers)-1] != 1 {
					t.Errorf("MTU %d: RTP markers = %v, want only the final packet marked", mtu, markers)
				}
			}
		})
	}
}

func TestH264PackerAggregatesAdjacentParameterSets(t *testing.T) {
	accessUnit, err := os.ReadFile("testdata/single-au.h264")
	if err != nil {
		t.Fatal(err)
	}
	var paramSets [][]byte
	codec.SplitFrame(accessUnit, func(nal []byte) bool {
		switch codec.H264NaluTypeWithoutStartCode(nal) {
		case codec.H264_NAL_SPS, codec.H264_NAL_PPS:
			paramSets = append(paramSets, nal)
		}
		return true
	})
	if len(paramSets) != 2 {
		t.Fatalf("fixture has %d parameter sets, want SPS and PPS", len(paramSets))
	}
	var annexB []byte
	for _, nal := range paramSets {
		annexB = append(annexB, 0, 0, 0, 1)
		annexB = append(annexB, nal...)
	}
	stapSize := RTP_FIX_HEAD_LEN + 5 + len(paramSets[0]) + len(paramSets[1])
	for _, mtu := range []int{stapSize, stapSize - 1} {
		packer := NewH264Packer(96, 1, 41, mtu)
		packer.EnableStapA()
		unpacker := NewH264UnPacker()
		var restored []byte
		unpacker.OnFrame(func(frame []byte, _ uint32, lost bool) {
			if lost {
				t.Error("unexpected loss")
			}
			restored = append(restored, frame...)
		})
		var packets []RtpPacket
		packer.OnPacket(func(data []byte) error {
			if len(data) > mtu {
				t.Errorf("packet length = %d, MTU = %d", len(data), mtu)
			}
			var packet RtpPacket
			if err := packet.Decode(data); err != nil {
				return err
			}
			packets = append(packets, packet)
			return unpacker.UnPack(data)
		})
		if err := packer.Pack(annexB, 9000); err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(restored, annexB) {
			t.Errorf("MTU %d: restored %d bytes, want %d bytes with matching contents", mtu, len(restored), len(annexB))
		}
		if mtu == stapSize {
			if len(packets) != 1 || packets[0].Payload[0] != 0x78 || packets[0].Header.PayloadType != 96 || packets[0].Header.Marker != 1 {
				t.Errorf("want one STAP-A packet with NRI 3, payload type 96 and marker set, got %+v", packets)
			}
		} else if len(packets) != 2 || packets[0].Header.Marker != 0 || packets[1].Header.Marker != 1 {
			t.Errorf("want two standalone packets with only the last marked, got %+v", packets)
		}
	}
}
