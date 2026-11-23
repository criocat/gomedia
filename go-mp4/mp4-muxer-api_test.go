package mp4

import (
	"bytes"
	"testing"
)

func TestMP4WriteRejectsUnknownTrack(t *testing.T) {
	output := newFmp4WriterSeeker(4096)
	muxer, err := CreateMp4Muxer(output)
	if err != nil {
		t.Fatal(err)
	}
	track := muxer.AddAudioTrack(MP4_CODEC_G711A, WithAudioSampleRate(8000), WithAudioChannelCount(1))
	data := bytes.Repeat([]byte{0x55}, 80)
	for _, id := range []uint32{0, track + 1} {
		if err := muxer.Write(id, data, 0, 0); err == nil {
			t.Fatalf("Write accepted unknown track %d", id)
		}
	}
	if err := muxer.Write(track, data, 0, 0); err != nil {
		t.Fatal(err)
	}
}

func TestMP4FlushFragmentRejectsRegularMuxer(t *testing.T) {
	output := newFmp4WriterSeeker(4096)
	muxer, err := CreateMp4Muxer(output)
	if err != nil {
		t.Fatal(err)
	}
	track := muxer.AddAudioTrack(MP4_CODEC_G711A, WithAudioSampleRate(8000), WithAudioChannelCount(1))
	frame := bytes.Repeat([]byte{0x55}, 80)
	if err := muxer.Write(track, frame, 0, 0); err != nil {
		t.Fatal(err)
	}
	before := append([]byte(nil), output.buffer...)
	if err := muxer.FlushFragment(); err == nil {
		t.Fatal("FlushFragment accepted a non-fragmented muxer")
	}
	if !bytes.Equal(output.buffer, before) {
		t.Fatal("rejected flush changed the file")
	}

	if err := muxer.WriteTrailer(); err != nil {
		t.Fatal(err)
	}
	demuxer := CreateMp4Demuxer(bytes.NewReader(output.buffer))
	if _, err := demuxer.ReadHead(); err != nil {
		t.Fatal(err)
	}
	packet, err := demuxer.ReadPacket()
	if err != nil || !bytes.Equal(packet.Data, frame) {
		t.Fatalf("read sample=%v, err=%v, want original frame", packet, err)
	}
}
