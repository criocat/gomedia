package codec

import (
	"encoding/hex"
	"testing"
)

func TestHEVCRecordConfigurationUsesVPSTemporalSublayers(t *testing.T) {
	// x265 with temporal-layers=2. FFmpeg trace_headers reports one
	// spatial layer and two temporal sublayers in this VPS.
	nalu, err := hex.DecodeString("0000000140010c02ffff01600000030090000003000003001e00009594aca048")
	if err != nil {
		t.Fatal(err)
	}
	config := NewHEVCRecordConfiguration()
	config.UpdateVPS(nalu)
	if config.NumTemporalLayers != 2 {
		t.Errorf("temporal layers = %d, want 2", config.NumTemporalLayers)
	}
}

func TestHEVCRecordConfigurationKeepsParameterSetCounts(t *testing.T) {
	config := NewHEVCRecordConfiguration()
	secondVPS := append([]byte(nil), vps...)
	secondVPS[6] |= 0x10 // VPS ID 1 in the first byte after the NAL header.

	for _, set := range []struct {
		nalType H265_NAL_TYPE
		update  func([]byte)
		first   []byte
		second  []byte
	}{
		{H265_NAL_VPS, config.UpdateVPS, vps, secondVPS},
		{H265_NAL_SPS, config.UpdateSPS, sps, h265sps2},
		{H265_NAL_PPS, config.UpdatePPS, pps, pps2},
	} {
		set.update(set.first)
		if got := config.NumOfArrays; int(got) != len(config.Arrays) {
			t.Errorf("array count with NAL type %d = %d, want %d", set.nalType, got, len(config.Arrays))
		}
		array := config.Arrays[len(config.Arrays)-1]
		if array.NAL_unit_type != uint8(set.nalType) || array.NumNalus != 1 {
			t.Fatalf("new array type = %d, NAL count = %d, want %d and 1",
				array.NAL_unit_type, array.NumNalus, set.nalType)
		}
		set.update(set.second)
		set.update(set.second)
		if len(array.NalUnits) != 2 || array.NumNalus != 2 {
			t.Errorf("NAL type %d: stored %d entries, count = %d, want 2",
				set.nalType, len(array.NalUnits), array.NumNalus)
		}
	}

	wire, err := config.Encode()
	if err != nil {
		t.Fatal(err)
	}
	var decoded HEVCRecordConfiguration
	decoded.Decode(wire)
	if decoded.NumOfArrays != config.NumOfArrays {
		t.Errorf("encoded array count = %d, in-memory count = %d", decoded.NumOfArrays, config.NumOfArrays)
	}
}
