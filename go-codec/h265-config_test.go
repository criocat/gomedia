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

func TestH265VPSInfersLowerSublayerOrdering(t *testing.T) {
	// A two-sublayer VPS with ordering fields signaled only for layer 1.
	nalu, err := hex.DecodeString("40010c02ffff01600000030090000003000003001e0000159409")
	if err != nil {
		t.Fatal(err)
	}
	var decoded VPS
	decoded.Decode(nalu)
	if decoded.Vps_max_sub_layers_minus1 != 1 || decoded.Vps_sub_layer_ordering_info_present_flag != 0 {
		t.Fatalf("expected two sublayers with inferred ordering fields")
	}
	for layer := 0; layer < 2; layer++ {
		got := [3]uint64{
			decoded.Vps_max_dec_pic_buffering_minus1[layer],
			decoded.Vps_max_num_reorder_pics[layer],
			decoded.Vps_max_latency_increase_plus1[layer],
		}
		if want := [3]uint64{4, 2, 4}; got != want {
			t.Errorf("sublayer %d ordering = %v, want %v", layer, got, want)
		}
	}
}

func TestH265SPSReadsVUIAfterPredictedReferencePictureSets(t *testing.T) {
	for _, tt := range []struct {
		name string
		hex  string
	}{
		// The first set explicitly signals two entries. The next set
		// predicts from those entries, with one unused delta.
		{"explicit reference", "42010201600000030090000003000003001e0000a0102061659594acb249934be8de02d010000003001000000300c080"},
		// A third set predicts from the entries derived in the second.
		{"predicted reference", "42010201600000030090000003000003001e0000a0102061659594acb2499112fa3e6f0168080000030008000003006040"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			nalu, err := hex.DecodeString(tt.hex)
			if err != nil {
				t.Fatal(err)
			}
			var decoded H265RawSPS
			decoded.Decode(nalu)
			if decoded.Vui_parameters_present_flag != 1 || decoded.Vui.Vui_timing_info_present_flag != 1 {
				t.Fatal("VUI timing information was lost")
			}
			if got := decoded.Vui.Vui_num_units_in_tick; got != 1 {
				t.Errorf("VUI units per tick = %d, want 1", got)
			}
			if got := decoded.Vui.Vui_time_scale; got != 12 {
				t.Errorf("VUI time scale = %d, want 12", got)
			}
		})
	}
}
