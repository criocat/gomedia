package codec

import (
    "reflect"
    "testing"
)

var sps1 []byte = []byte{0x64, 0x00, 0x28, 0xAC, 0x2C, 0xA4, 0x01, 0xE0, 0x08, 0x9F, 0x97, 0xFF, 0x00, 0x01, 0x00, 0x01, 0x52, 0x02, 0x02, 0x02, 0x80, 0x00,
    0x01, 0xF4, 0x80, 0x00, 0x75, 0x30, 0x70, 0x10, 0x00, 0x16, 0xE3, 0x60, 0x00, 0x08, 0x95, 0x45, 0xF8, 0xC7, 0x07, 0x68, 0x58, 0xB4, 0x48,
}

var sps2 []byte = []byte{0x00, 0x00, 0x00, 0x01, 0x67, 0x64, 0x00, 0x28, 0xAC, 0x2C, 0xA4, 0x01, 0xE0, 0x08, 0x9F, 0x97, 0xFF, 0x00, 0x01, 0x00, 0x01, 0x52, 0x02, 0x02, 0x02, 0x80, 0x00,
    0x01, 0xF4, 0x80, 0x00, 0x75, 0x30, 0x70, 0x10, 0x00, 0x16, 0xE3, 0x60, 0x00, 0x08, 0x95, 0x45, 0xF8, 0xC7, 0x07, 0x68, 0x58, 0xB4, 0x48,
}

func TestH264SPSPOCType1Offsets(t *testing.T) {
    bsw := NewBitStreamWriter(8)
    bsw.PutBytes([]byte{0x42, 0x00, 0x1e}) // Baseline profile, level 3.0.
    bsw.PutUint8(0b11, 2)                  // SPS ID and log2_max_frame_num_minus4, both ue(0).
    bsw.PutUint8(0b010, 3)                 // pic_order_cnt_type = ue(1).
    bsw.PutUint8(0b011, 3)                 // Delta flag off and two se(0) offsets.
    bsw.PutUint8(0b011, 3)                 // num_ref_frames_in_pic_order_cnt_cycle = ue(2).
    bsw.PutUint8(0b010011, 6)              // Reference offsets se(1) and se(-1).
    bsw.PutUint8(0b010, 3)                 // max_num_ref_frames = ue(1).
    bsw.PutUint8(0b01111, 5)               // No gaps, one macroblock in both dimensions, progressive, direct 8x8.
    bsw.PutUint8(0b001, 3)                 // No cropping or VUI, followed by rbsp_stop_one_bit.

    var sps SPS
    sps.Decode(NewBitStream(bsw.Bits()))
    if got := sps.Offset_for_ref_frame; !reflect.DeepEqual(got, []int64{1, -1}) {
        t.Fatalf("reference offsets = %v, want [1 -1]", got)
    }
    if sps.Max_num_ref_frames != 1 || sps.Frame_mbs_only_flag != 1 {
        t.Fatal("SPS fields following the reference offsets were misread")
    }
}

func TestH264VUITailFlags(t *testing.T) {
    for _, tc := range []struct {
        name        string
        picStruct   uint8
        restriction uint8
    }{
        {"picture-structure", 1, 0},
        {"bitstream-restriction", 0, 1},
    } {
        t.Run(tc.name, func(t *testing.T) {
            bsw := NewBitStreamWriter(3)
            bsw.PutUint8(0, 7) // No optional VUI fields through vcl_hrd_parameters_present_flag.
            bsw.PutUint8(tc.picStruct, 1)
            bsw.PutUint8(tc.restriction, 1)
            if tc.restriction == 1 {
                bsw.PutUint8(0b1111111, 7) // Motion vectors flag and six ue(0) values.
            }
            bsw.PutUint8(1, 1) // rbsp_stop_one_bit.

            var vui H264VuiParameters
            vui.Decode(NewBitStream(bsw.Bits()))
            if vui.PicStructPresentFlag != tc.picStruct || vui.BitstreamRestrictionFlag != tc.restriction {
                t.Fatalf("VUI flags = %d, %d, want %d, %d", vui.PicStructPresentFlag, vui.BitstreamRestrictionFlag, tc.picStruct, tc.restriction)
            }
        })
    }
}

func TestH264VUIShortTail(t *testing.T) {
    // Seven VUI presence flags followed by pic_struct_present_flag.
    var vui H264VuiParameters
    vui.Decode(NewBitStream([]byte{0b00000001}))
    if vui.PicStructPresentFlag != 1 || vui.BitstreamRestrictionFlag != 0 {
        t.Fatalf("VUI flags = %d, %d, want 1, 0", vui.PicStructPresentFlag, vui.BitstreamRestrictionFlag)
    }
}

func TestSPS_Decode(t *testing.T) {
    type args struct {
        bs *BitStream
    }
    tests := []struct {
        name string
        sps  *SPS
        args args
    }{
        {
            name: "sps1",
            sps:  new(SPS),
            args: args{
                bs: NewBitStream(sps1),
            }},
    }
    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            tt.sps.Decode(tt.args.bs)
            t.Log(tt.sps)
        })
    }
}

func TestGetH264Resolution(t *testing.T) {
    type args struct {
        sps []byte
    }
    tests := []struct {
        name       string
        args       args
        wantWidth  uint32
        wantHeight uint32
    }{
        {
            name: "Resolution1",
            args: args{
                sps: sps2,
            }, wantWidth: 1920, wantHeight: 1080,
        },
    }
    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            gotWidth, gotHeight := GetH264Resolution(tt.args.sps)
            t.Logf("%d,%d", gotWidth, gotHeight)
            if gotWidth != tt.wantWidth {
                t.Errorf("GetH264Resolution() gotWidth = %v, want %v", gotWidth, tt.wantWidth)
            }
            if gotHeight != tt.wantHeight {
                t.Errorf("GetH264Resolution() gotHeight = %v, want %v", gotHeight, tt.wantHeight)
            }
        })
    }
}

var spss1 [][]byte = [][]byte{{0x00, 0x00, 0x00, 0x01, 0x67, 0x64, 0x00, 0x0A, 0xAC, 0x72, 0x84, 0x44,
    0x26, 0x84, 0x00, 0x00, 0x03, 0x00, 0x04, 0x00, 0x00, 0x03, 0x00, 0xCA, 0x3C, 0x48, 0x96, 0x11, 0x80}}
var ppss1 [][]byte = [][]byte{{0x00, 0x00, 0x00, 0x01, 0x68, 0xE8, 0x43, 0x8F, 0x13, 0x21, 0x30}}
var want1 []byte = []byte{0x01, 0x64, 0x00, 0x0A, 0xFF, 0xE1, 0x00, 0x19, 0x67, 0x64, 0x00, 0x0A, 0xAC, 0x72, 0x84, 0x44,
    0x26, 0x84, 0x00, 0x00, 0x03, 0x00, 0x04, 0x00, 0x00, 0x03, 0x00, 0xCA, 0x3C, 0x48, 0x96, 0x11,
    0x80, 0x01, 0x00, 0x07, 0x68, 0xE8, 0x43, 0x8F, 0x13, 0x21, 0x30}

func TestCreateH264AVCCExtradata(t *testing.T) {
    type args struct {
        spss [][]byte
        ppss [][]byte
    }
    tests := []struct {
        name string
        args args
        want []byte
    }{
        {name: "extrandata1", args: args{
            spss: spss1,
            ppss: ppss1,
        }, want: want1},
    }
    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            if got, _ := CreateH264AVCCExtradata(tt.args.spss, tt.args.ppss); !reflect.DeepEqual(got, tt.want) {
                t.Errorf("CreateH264AVCCExtradata() = %v, want %v", got, tt.want)
            }
        })
    }
}
