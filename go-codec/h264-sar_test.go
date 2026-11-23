package codec

import (
	"encoding/hex"
	"testing"
)

func TestH264SPSDecodesPredefinedSquareSAR(t *testing.T) {
	// SPS from the TS sample attached to upstream issue 160.
	nalu, err := hex.DecodeString("67640028acd940780227e5c05a808080a0000003002000000781e30632c0")
	if err != nil {
		t.Fatal(err)
	}
	var sps SPS
	sps.Decode(NewBitStream(CovertRbspToSodb(nalu[1:])))
	vui := sps.VuiParameters
	if vui.AspectRatioIdc != 1 || vui.SarWidth != 1 || vui.SarHeight != 1 {
		t.Errorf("aspect_ratio_idc = %d, SAR = %d:%d, want 1 and 1:1",
			vui.AspectRatioIdc, vui.SarWidth, vui.SarHeight)
	}
}

func TestH264VUIDecodesAndClearsPredefinedSAR(t *testing.T) {
	var vui H264VuiParameters
	for _, tc := range []struct {
		present uint8
		idc     uint8
		width   uint16
		height  uint16
	}{
		{1, 2, 12, 11},
		{1, 16, 2, 1},
		{1, 0, 0, 0}, // Unspecified.
		{1, 14, 4, 3},
		{1, 17, 0, 0}, // Reserved.
		{1, 1, 1, 1},
		{0, 0, 0, 0}, // Aspect ratio omitted.
	} {
		bsw := NewBitStreamWriter(3)
		bsw.PutUint8(tc.present, 1)
		if tc.present == 1 {
			bsw.PutUint8(tc.idc, 8)
		}
		bsw.PutUint8(0, 8) // Remaining VUI flags.
		vui.Decode(NewBitStream(bsw.Bits()))
		if vui.AspectRatioIdc != tc.idc || vui.SarWidth != tc.width || vui.SarHeight != tc.height {
			t.Errorf("aspect_ratio_idc = %d, SAR = %d:%d, want %d and %d:%d",
				vui.AspectRatioIdc, vui.SarWidth, vui.SarHeight, tc.idc, tc.width, tc.height)
		}
	}
}
