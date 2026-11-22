package mp4

import (
	"bytes"
	"encoding/binary"
	"reflect"
	"testing"
)

func TestMP4WritesStandardOpusSampleEntry(t *testing.T) {
	const preSkip = 3840
	const gain = -256
	head := []byte("OpusHead\x01\x01\x00\x00\x80\xbb\x00\x00\x00\x00\x00")
	binary.LittleEndian.PutUint16(head[10:], preSkip)
	binary.LittleEndian.PutUint16(head[16:], uint16(gain+65536))
	output := newFmp4WriterSeeker(4096)
	muxer, err := CreateMp4Muxer(output)
	if err != nil {
		t.Fatal(err)
	}
	id := muxer.AddAudioTrack(MP4_CODEC_OPUS, WithExtraData(head), WithAudioSampleRate(48000), WithAudioChannelCount(1), WithAudioSampleBits(16))
	if err := muxer.Write(id, []byte{0xf8, 0xff, 0xfe}, 0, 0); err != nil {
		t.Fatal(err)
	}
	if err := muxer.WriteTrailer(); err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(output.buffer, []byte("Opus")) || bytes.Contains(output.buffer, []byte("opus")) {
		t.Error("expected the case-sensitive Opus sample entry")
	}
	pos := bytes.Index(output.buffer, []byte("dOps"))
	if pos < 4 {
		t.Fatal("Opus sample entry must contain dOps")
	}
	want := []byte{0, 0, 0, 19, 'd', 'O', 'p', 's', 0, 1, 0x0f, 0x00, 0, 0, 0xbb, 0x80, 0xff, 0x00, 0}
	if got := output.buffer[pos-4 : pos-4+len(want)]; !bytes.Equal(got, want) {
		t.Errorf("dOps = %x, want %x", got, want)
	}
}

func TestOpusSpecificBoxReadsFamilyZeroAndMappedChannels(t *testing.T) {
	for _, tc := range []struct {
		body   []byte
		family uint8
		table  *ChannelMappingTable
	}{
		{body: []byte{0, 1, 0x01, 0x38, 0, 0, 0xbb, 0x80, 0, 0, 0}},
		{
			body:   []byte{0, 3, 0x01, 0x38, 0, 0, 0xbb, 0x80, 0, 0, 1, 2, 1, 0, 2, 1},
			family: 1,
			table:  &ChannelMappingTable{StreamCount: 2, CoupledCount: 1, ChannelMapping: []byte{0, 2, 1}},
		},
	} {
		box := NewdOpsBox()
		read, err := box.Decode(bytes.NewReader(tc.body), BasicBoxLen+uint32(len(tc.body)))
		if err != nil || read != len(tc.body) {
			t.Fatalf("Decode read %d bytes: %v", read, err)
		}
		if box.ChannelMappingFamily != tc.family || !reflect.DeepEqual(box.ChanMapTable, tc.table) || box.PreSkip != 312 || box.InputSampleRate != 48000 {
			t.Errorf("wrong dOps fields: %+v", box)
		}
		written, wire := box.Encode()
		if written != len(wire) || !bytes.Equal(wire[BasicBoxLen:], tc.body) {
			t.Errorf("re-encoded dOps = %x, want body %x", wire, tc.body)
		}
	}
}
