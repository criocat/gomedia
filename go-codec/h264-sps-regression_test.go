package codec

import (
	"encoding/hex"
	"testing"
)

func TestH264SPSReadsFieldsAfterScalingLists(t *testing.T) {
	for _, tc := range []struct {
		name          string
		hex           string
		width, height uint32
		timeScale     uint32
	}{
		{"4:2:0", "6764000aad843fffc220843fffffffffffffffd9096c0440000003004000000c83c48992", 32, 32, 50},
		{"4:4:4", "67f4000a91b087fff0421fffffffffffffffc211b213d80880000003008000001e07891324", 32, 48, 60},
	} {
		t.Run(tc.name, func(t *testing.T) {
			nalu := decodeH264SPSFixture(t, tc.hex)
			width, height := GetH264Resolution(append([]byte{0, 0, 0, 1}, nalu...))
			if width != tc.width || height != tc.height {
				t.Errorf("resolution = %dx%d, want %dx%d", width, height, tc.width, tc.height)
			}
			var sps SPS
			sps.Decode(NewBitStream(CovertRbspToSodb(nalu[1:])))
			if sps.VuiParameters.NumUnitsInTick != 1 || sps.VuiParameters.TimeScale != tc.timeScale {
				t.Errorf("VUI clock = %d/%d, want 1/%d",
					sps.VuiParameters.NumUnitsInTick, sps.VuiParameters.TimeScale, tc.timeScale)
			}
		})
	}
}

func TestH264SPSClearsOmittedFieldsOnReuse(t *testing.T) {
	withCropAndVUI := decodeH264SPSFixture(t, "67640028acd940780227e5c05a808080a0000003002000000781e30632c0")
	withoutCropAndVUI := decodeH264SPSFixture(t, "6764000aacb212c8")
	var sps SPS
	sps.Decode(NewBitStream(CovertRbspToSodb(withCropAndVUI[1:])))
	if sps.Frame_cropping_flag != 1 || sps.Frame_crop_bottom_offset == 0 || sps.VuiParameters.TimeScale == 0 {
		t.Fatal("first fixture must signal cropping and a VUI clock")
	}
	sps.Decode(NewBitStream(CovertRbspToSodb(withoutCropAndVUI[1:])))
	if sps.Frame_cropping_flag != 0 || sps.Frame_crop_bottom_offset != 0 {
		t.Errorf("old crop survived: flag = %d, bottom offset = %d", sps.Frame_cropping_flag, sps.Frame_crop_bottom_offset)
	}
	if sps.Vui_parameters_present_flag != 0 || sps.VuiParameters.TimeScale != 0 {
		t.Errorf("old VUI survived: flag = %d, time scale = %d", sps.Vui_parameters_present_flag, sps.VuiParameters.TimeScale)
	}
}

func decodeH264SPSFixture(t *testing.T, encoded string) []byte {
	t.Helper()
	nalu, err := hex.DecodeString(encoded)
	if err != nil {
		t.Fatal(err)
	}
	return nalu
}
