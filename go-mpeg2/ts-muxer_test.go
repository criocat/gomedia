package mpeg2

import (
	"reflect"
	"testing"

	"github.com/yapingcat/gomedia/go-codec"
)

func TestTSMuxerSplitsLargePMTIntoTransportPackets(t *testing.T) {
	const tracks = 36
	muxer := NewTSMuxer()
	first := muxer.AddStream(TS_STREAM_AAC)
	for i := 1; i < tracks; i++ {
		muxer.AddStream(TS_STREAM_AAC)
	}
	var section []byte
	var startFlags, counters []uint8
	muxer.OnPacket = func(data []byte) {
		if len(data) != TS_PAKCET_SIZE {
			t.Fatalf("transport packet size = %d, want %d", len(data), TS_PAKCET_SIZE)
		}
		bits := codec.NewBitStream(data)
		var packet TSPacket
		if err := packet.DecodeHeader(bits); err != nil {
			t.Fatal(err)
		}
		if packet.PID != 0x200 {
			return
		}
		if packet.Payload_unit_start_indicator != 0 && bits.Uint8(8) != 0 {
			t.Fatal("expected zero pointer at the start of the PMT")
		}
		section = append(section, bits.RemainData()...)
		startFlags = append(startFlags, packet.Payload_unit_start_indicator)
		counters = append(counters, packet.Continuity_counter)
	}
	if err := muxer.Write(first, []byte(clockTestAAC), 0, 0); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(startFlags, []uint8{1, 0}) || !reflect.DeepEqual(counters, []uint8{0, 1}) {
		t.Fatalf("PMT packet flags = %v, counters = %v", startFlags, counters)
	}
	pmt := NewPmt()
	if err := pmt.Decode(codec.NewBitStream(section)); err != nil {
		t.Fatal(err)
	}
	if len(pmt.Streams) != tracks {
		t.Fatalf("PMT contains %d tracks, want %d", len(pmt.Streams), tracks)
	}
	for i, stream := range pmt.Streams {
		if stream.Elementary_PID != first+uint16(i) || stream.StreamType != uint8(TS_STREAM_AAC) {
			t.Errorf("PMT entry %d = %+v", i, stream)
		}
	}
}

func TestTSMuxerPublishesNewPMTVersionWhenAddingTracks(t *testing.T) {
	muxer := NewTSMuxer()
	type programMap struct {
		version uint8
		tracks  int
	}
	var maps []programMap
	muxer.OnPacket = func(data []byte) {
		bits := codec.NewBitStream(data)
		var packet TSPacket
		if err := packet.DecodeHeader(bits); err != nil {
			t.Fatal(err)
		}
		if packet.PID == 0x200 {
			bits.SkipBits(8)
			pmt := NewPmt()
			if err := pmt.Decode(bits); err != nil {
				t.Fatal(err)
			}
			maps = append(maps, programMap{pmt.Version_number, len(pmt.Streams)})
		}
	}
	first := muxer.AddStream(TS_STREAM_AAC)
	if err := muxer.Write(first, []byte(clockTestAAC), 0, 0); err != nil {
		t.Fatal(err)
	}
	second := muxer.AddStream(TS_STREAM_AAC)
	muxer.AddStream(TS_STREAM_AAC)
	for _, timestamp := range []uint64{23, 500} {
		if err := muxer.Write(second, []byte(clockTestAAC), timestamp, timestamp); err != nil {
			t.Fatal(err)
		}
	}
	want := []programMap{{0, 1}, {1, 3}, {1, 3}}
	if !reflect.DeepEqual(maps, want) {
		t.Errorf("PMT versions and track counts = %v, want %v", maps, want)
	}
}
