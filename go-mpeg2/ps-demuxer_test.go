package mpeg2

import (
	"testing"

	"github.com/yapingcat/gomedia/go-codec"
)

var ps1 []byte = []byte{0x00, 0x00, 0x01, 0xBA}
var ps2 []byte = []byte{0x00, 0x00, 0x01, 0xBA, 0x40, 0x01, 0x00, 0x01, 0x33, 0x44, 0xFF, 0xFF, 0xFF, 0xF1, 0xFF}

var ps3 []byte = []byte{0x00, 0x00, 0x01, 0xBA, 0x40, 0x01, 0x00, 0x01, 0x33, 0x44, 0xFF, 0xFF, 0xFF, 0xF0, 0x00, 0x00, 0x01, 0xBB}
var ps4 []byte = []byte{0x00, 0x00, 0x01, 0xBA, 0x40, 0x01, 0x00, 0x01, 0x33, 0x44, 0xFF, 0xFF, 0xFF, 0xF1, 0x34, 0x00, 0x00, 0x01, 0xBB, 0x00, 0x01, 0x00, 0x01, 0x33, 0x44, 0xFF, 0x34}
var ps5 []byte = []byte{0x00, 0x00, 0x01, 0xBA, 0x40, 0x01, 0x00, 0x01, 0x33, 0x44, 0xFF, 0xFF, 0xFF, 0xF1, 0x34, 0x00, 0x00, 0x01, 0xBB, 0x00, 0x09, 0x00, 0x01, 0x33, 0x44, 0xFF, 0x34, 0x81, 0x00, 0x00}
var ps7 []byte = []byte{0x00, 0x00, 0x01, 0xBA, 0x20, 0x0a, 0x00, 0x00, 0x00, 0x00, 0x00, 0x03}

func TestPSDemuxerSystemHeaderRequiresPack(t *testing.T) {
	demuxer := NewPSDemuxer()
	if err := demuxer.decodeSystemHeader(nil); err != errParser {
		t.Fatalf("system header without active pack: %v", err)
	}
	if demuxer.pkg.System != nil {
		t.Fatal("system header state changed without an active pack")
	}

	writer := codec.NewBitStreamWriter(32)
	(&PSPackHeader{}).Encode(writer)
	(&System_header{Rate_bound: 1000}).Encode(writer)
	if err := demuxer.Input(writer.Bits()); err != nil {
		t.Fatalf("ordered pack and system header: %v", err)
	}
	if demuxer.pkg.System == nil || demuxer.pkg.System.Rate_bound != 1000 {
		t.Fatal("valid system header was not decoded")
	}
}

func TestPSDemuxer_Input(t *testing.T) {
	type fields struct {
		streamMap map[uint8]*psstream
		pkg       *PSPacket
		cache     []byte
		OnPacket  func(pkg Display, decodeResult error)
		OnFrame   func(frame []byte, cid PS_STREAM_TYPE, pts uint64, dts uint64)
	}
	type args struct {
		data []byte
	}
	tests := []struct {
		name    string
		fields  fields
		args    args
		wantErr bool
	}{
		{name: "test1", fields: fields{
			streamMap: make(map[uint8]*psstream),
			pkg:       new(PSPacket),
		}, args: args{data: ps1}, wantErr: true},

		{name: "test2", fields: fields{
			streamMap: make(map[uint8]*psstream),
			pkg:       new(PSPacket),
		}, args: args{data: ps2}, wantErr: false},

		{name: "test3", fields: fields{
			streamMap: make(map[uint8]*psstream),
			pkg:       new(PSPacket),
		}, args: args{data: ps3}, wantErr: true},

		{name: "test4", fields: fields{
			streamMap: make(map[uint8]*psstream),
			pkg:       new(PSPacket),
		}, args: args{data: ps4}, wantErr: true},

		{name: "test5", fields: fields{
			streamMap: make(map[uint8]*psstream),
			pkg:       new(PSPacket),
		}, args: args{data: ps5}, wantErr: false},
		{name: "test-mpeg1", fields: fields{
			streamMap: make(map[uint8]*psstream),
			pkg:       new(PSPacket),
		}, args: args{data: ps7}, wantErr: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			psdemuxer := &PSDemuxer{
				streamMap: tt.fields.streamMap,
				pkg:       tt.fields.pkg,
				cache:     tt.fields.cache,
				OnPacket:  tt.fields.OnPacket,
				OnFrame:   tt.fields.OnFrame,
			}
			if err := psdemuxer.Input(tt.args.data); (err != nil) != tt.wantErr {
				t.Errorf("PSDemuxer.Input() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestPSDemuxerReadsSplitProgramStreamMap(t *testing.T) {
	const streamID = 0xE0
	writer := codec.NewBitStreamWriter(32)
	(&Program_stream_map{
		Current_next_indicator: 1,
		Stream_map: []*Elementary_stream_elem{{
			Stream_type:          uint8(PS_STREAM_H264),
			Elementary_stream_id: streamID,
		}},
	}).Encode(writer)
	packet := writer.Bits()
	split := len(packet) - 1

	demuxer := NewPSDemuxer()
	if err := demuxer.Input(packet[:split]); err != errNeedMore {
		t.Fatalf("incomplete program stream map: got %v, want %v", err, errNeedMore)
	}
	if len(demuxer.streamMap) != 0 {
		t.Fatal("incomplete program stream map registered a stream")
	}
	if err := demuxer.Input(packet[split:]); err != nil {
		t.Fatalf("completed program stream map: %v", err)
	}
	if stream := demuxer.streamMap[streamID]; stream == nil || stream.cid != PS_STREAM_H264 {
		t.Fatal("completed program stream map did not register the H.264 stream")
	}
}
