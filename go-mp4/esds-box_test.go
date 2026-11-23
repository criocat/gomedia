package mp4

import (
	"bytes"
	"strings"
	"testing"
)

func TestMP4ReadHeadReportsMissingAACConfiguration(t *testing.T) {
	init := fragmentTestInit(t, MP4_CODEC_AAC)
	demuxer := CreateMp4Demuxer(bytes.NewReader(init))
	if _, err := demuxer.ReadHead(); err == nil || !strings.Contains(err.Error(), "AAC configuration") {
		t.Fatalf("ReadHead error = %v, want missing AAC configuration", err)
	}
}

func TestMP4ReadsAACConfigurationAfterESURL(t *testing.T) {
	config := []byte{0x12, 0x10} // AAC-LC, 44.1 kHz, stereo.
	encoded := makeESDescriptor(1, MP4_CODEC_AAC, config)
	url := []byte("https://example.org/audio")
	es := makeBaseDescriptor(0x03, uint32(len(encoded)-5+1+len(url)))
	copy(es[5:8], encoded[5:8])
	es[7] = 0x40
	es[8] = byte(len(url))
	copy(es[9:], url)
	copy(es[9+len(url):], encoded[8:])
	box := fragmentTestBox("esds", make([]byte, 4), es)
	demuxer := CreateMp4Demuxer(bytes.NewReader(box[BasicBoxLen:]))
	track := &mp4track{cid: MP4_CODEC_AAC}
	demuxer.tracks = []*mp4track{track}
	if err := decodeEsdsBox(demuxer, uint32(len(box))); err != nil {
		t.Fatal(err)
	}
	if got := track.extra.export(); !bytes.Equal(got, config) {
		t.Errorf("AAC configuration = %x, want %x", got, config)
	}
}
