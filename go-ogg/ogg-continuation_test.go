package ogg

import (
	"bytes"
	"encoding/binary"
	"encoding/hex"
	"reflect"
	"testing"

	"github.com/yapingcat/gomedia/go-codec"
)

func TestDemuxerReassemblesVP8AcrossPages(t *testing.T) {
	header, err := hex.DecodeString("4f56503830010100078004380000010000010000000300000001")
	if err != nil {
		t.Fatal(err)
	}
	var frames [][]byte
	demuxer := NewDemuxer()
	demuxer.OnFrame = func(_ uint32, cid codec.CodecID, frame []byte, _, _ uint64, lost int) {
		if cid != codec.CODECID_VIDEO_VP8 || lost != 0 {
			t.Fatalf("frame codec = %d, lost = %d", cid, lost)
		}
		frames = append(frames, append([]byte(nil), frame...))
	}
	first := bytes.Repeat([]byte{0x11}, 255)
	middle := bytes.Repeat([]byte{0x22}, 255)
	last := bytes.Repeat([]byte{0x33}, 255)
	next := []byte{0x10, 0x55}
	pages := []struct {
		flags   byte
		granule uint64
		lacing  []byte
		payload []byte
	}{
		{flags: 2, lacing: []byte{byte(len(header))}, payload: header},
		{granule: ^uint64(0), lacing: []byte{255}, payload: first},
		{flags: 1, granule: ^uint64(0), lacing: []byte{255}, payload: middle},
		{flags: 1, granule: ^uint64(0), lacing: []byte{255}, payload: last},
		{flags: 5, granule: 2 << 32, lacing: []byte{0, byte(len(next))}, payload: next},
	}
	for i, page := range pages {
		wire := makeOggTestPage(page.flags, page.granule, uint32(i), page.lacing, page.payload)
		if err := demuxer.Input(wire); err != nil {
			t.Fatalf("page %d: %v", i, err)
		}
		if i < len(pages)-1 && len(frames) != 0 {
			t.Fatalf("reported %d frames before a complete packet", len(frames))
		}
	}
	want := [][]byte{append(append(first, middle...), last...), next}
	if !reflect.DeepEqual(frames, want) {
		var sizes []int
		for _, frame := range frames {
			sizes = append(sizes, len(frame))
		}
		t.Errorf("frame sizes = %v, want [765 2] with matching bytes", sizes)
	}
}

func makeOggTestPage(flags byte, granule uint64, sequence uint32, lacing, payload []byte) []byte {
	page := make([]byte, 27, 27+len(lacing)+len(payload))
	copy(page, "OggS")
	page[5] = flags
	binary.LittleEndian.PutUint64(page[6:], granule)
	binary.LittleEndian.PutUint32(page[14:], 1) // Stream serial.
	binary.LittleEndian.PutUint32(page[18:], sequence)
	page[26] = byte(len(lacing))
	page = append(page, lacing...)
	page = append(page, payload...)
	binary.LittleEndian.PutUint32(page[22:], makeChecksum(0, page))
	return page
}
