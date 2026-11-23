package mp4

import (
	"bytes"
	"encoding/binary"
	"testing"
)

func buildTwoTrackFragmentIndex(t *testing.T) []byte {
	t.Helper()
	output := newFmp4WriterSeeker(4096)
	muxer, err := CreateMp4Muxer(output, WithMp4Flag(MP4_FLAG_FRAGMENT))
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		id := muxer.AddAudioTrack(MP4_CODEC_AAC)
		for _, stamp := range []uint64{0, 23} {
			if err := muxer.Write(id, mp4TestAACFrame, stamp, stamp); err != nil {
				t.Fatal(err)
			}
		}
	}
	if err := muxer.WriteTrailer(); err != nil {
		t.Fatal(err)
	}
	return output.buffer
}

func fragmentIndexChild(t *testing.T, data []byte, typ string) []byte {
	t.Helper()
	for len(data) >= 8 {
		size := int(binary.BigEndian.Uint32(data))
		if size < 8 || size > len(data) {
			break
		}
		if string(data[4:8]) == typ {
			return data[:size]
		}
		data = data[size:]
	}
	t.Fatalf("missing %s box", typ)
	return nil
}

func TestMP4MFROContainsCompleteMFRASize(t *testing.T) {
	data := buildTwoTrackFragmentIndex(t)
	mfra := fragmentIndexChild(t, data, "mfra")
	mfro := fragmentIndexChild(t, mfra[8:], "mfro")
	size := int(binary.BigEndian.Uint32(mfro[12:]))
	if size != len(mfra) || !bytes.Equal(data[len(data)-len(mfra):], mfra) {
		t.Errorf("mfro size = %d, terminal mfra box size = %d", size, len(mfra))
	}
}

func TestMP4TFRAReferencesCorrectTrackFragment(t *testing.T) {
	data := buildTwoTrackFragmentIndex(t)
	mfra := fragmentIndexChild(t, data, "mfra")
	firstIndex := fragmentIndexChild(t, mfra[8:], "tfra")
	secondIndex := fragmentIndexChild(t, mfra[8+len(firstIndex):], "tfra")
	indexedTrack := binary.BigEndian.Uint32(secondIndex[12:])
	moofOffset := binary.BigEndian.Uint64(secondIndex[32:])
	trafNumber := secondIndex[40]
	if indexedTrack != 2 || trafNumber != 2 || moofOffset >= uint64(len(data)) {
		t.Fatalf("tfra track = %d, traf number = %d, moof offset = %d", indexedTrack, trafNumber, moofOffset)
	}
	moof := fragmentIndexChild(t, data[moofOffset:], "moof")
	firstTraf := fragmentIndexChild(t, moof[8:], "traf")
	secondTraf := fragmentIndexChild(t, moof[8+len(fragmentIndexChild(t, moof[8:], "mfhd"))+len(firstTraf):], "traf")
	tfhd := fragmentIndexChild(t, secondTraf[8:], "tfhd")
	if track := binary.BigEndian.Uint32(tfhd[12:]); track != indexedTrack {
		t.Errorf("tfra track = %d, referenced tfhd track = %d", indexedTrack, track)
	}
}

func TestMP4TFRAStoresWideTrackFragmentNumber(t *testing.T) {
	track := &mp4track{trackId: 512, fragments: []movFragment{{trafNumber: 256, offset: 300}}}
	wire := makeTfraBox(track)
	if wire[19] != 0x10 || !bytes.Equal(wire[40:], []byte{1, 0, 1, 1}) {
		t.Fatalf("tfra widths and entry = %02x/%x", wire[19], wire[40:])
	}
	box := NewTrackFragmentRandomAccessBox(0)
	box.Box.Box.Size = uint64(len(wire))
	if _, err := box.Decode(bytes.NewReader(wire[8:])); err != nil {
		t.Fatal(err)
	}
	entry := box.FragEntrys.frags[0]
	if entry.trafNumber != 256 || entry.trunNumber != 1 || entry.sampleNumber != 1 || entry.moofOffset != 300 {
		t.Errorf("decoded fragment entry = %+v", entry)
	}
	if _, again := box.Encode(); !bytes.Equal(again, wire) {
		t.Errorf("tfra re-encode changed box from %x to %x", wire, again)
	}
}
