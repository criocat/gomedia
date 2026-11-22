package rtp

import (
	"bytes"
	"testing"
)

func TestHEVCUnpackerAcceptsTrailNAndSuffixSEI(t *testing.T) {
	for _, nal := range [][]byte{
		{0x00, 0x01, 0xd0, 0x80},             // TRAIL_N
		{0x50, 0x01, 0x84, 0x01, 0x00, 0x80}, // Suffix SEI
	} {
		unpacker := NewH265UnPacker()
		var got []byte
		unpacker.OnFrame(func(frame []byte, timestamp uint32, lost bool) {
			if lost || timestamp != 9000 {
				t.Errorf("timestamp = %d, lost = %t, want 9000/false", timestamp, lost)
			}
			got = append([]byte(nil), frame...)
		})
		packet := RtpPacket{Header: RtpHdr{PayloadType: 97, Timestamp: 9000}, Payload: nal}
		if err := unpacker.UnPack(packet.Encode()); err != nil {
			t.Errorf("NAL type %d: %v", nal[0]>>1, err)
		}
		if want := append([]byte{0, 0, 0, 1}, nal...); !bytes.Equal(got, want) {
			t.Errorf("NAL type %d: data = %x, want %x", nal[0]>>1, got, want)
		}
	}
}

func TestVideoUnpackersRecoverFromMissingFragment(t *testing.T) {
	for _, tc := range []struct {
		name      string
		unpacker  UnPacker
		fragment  []byte
		next      []byte
		intactNAL []byte
	}{
		{name: "AVC", unpacker: NewH264UnPacker(), fragment: []byte{0x7c, 0x85, 0xab}, next: []byte{0x09, 0x10}, intactNAL: []byte{0x09, 0x10}},
		{name: "AVC aggregation", unpacker: NewH264UnPacker(), fragment: []byte{0x7c, 0x85, 0xab}, next: []byte{0x18, 0, 2, 0x09, 0x10}, intactNAL: []byte{0x09, 0x10}},
		{name: "HEVC", unpacker: NewH265UnPacker(), fragment: []byte{0x62, 0x01, 0x93, 0xab}, next: []byte{0x46, 0x01, 0x10}, intactNAL: []byte{0x46, 0x01, 0x10}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var complete []byte
			var lostTimestamp uint32
			tc.unpacker.OnFrame(func(frame []byte, timestamp uint32, lost bool) {
				if lost {
					lostTimestamp = timestamp
					return
				}
				if timestamp != 18000 {
					t.Errorf("complete NAL timestamp = %d, want 18000", timestamp)
				}
				complete = append(complete, frame...)
			})
			for _, packet := range []RtpPacket{
				{Header: RtpHdr{SequenceNumber: 41, Timestamp: 9000}, Payload: tc.fragment},
				{Header: RtpHdr{SequenceNumber: 43, Timestamp: 18000}, Payload: tc.next},
			} {
				if err := tc.unpacker.UnPack(packet.Encode()); err != nil {
					t.Fatal(err)
				}
			}
			if want := append([]byte{0, 0, 0, 1}, tc.intactNAL...); !bytes.Equal(complete, want) {
				t.Errorf("intact NAL = %x, want %x", complete, want)
			}
			if lostTimestamp != 9000 {
				t.Errorf("interrupted NAL timestamp = %d, want loss reported at 9000", lostTimestamp)
			}
		})
	}
}
