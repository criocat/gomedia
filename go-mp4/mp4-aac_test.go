package mp4

import (
	"bytes"
	"errors"
	"io"
	"testing"
)

// An AAC-LC stereo silence frame in ADTS, at 44.1 kHz.
var mp4TestAACFrame = []byte{0xff, 0xf1, 0x50, 0x80, 0x01, 0xbf, 0xfc, 0x21, 0x10, 0x04, 0x60, 0x8c, 0x1c}

type aacFailingWriter struct {
	*fmp4WriterSeeker
	armed  bool
	writes int
}

func (writer *aacFailingWriter) Write(data []byte) (int, error) {
	if writer.armed {
		writer.writes++
		if writer.writes == 1 {
			return 0, io.ErrClosedPipe
		}
	}
	return writer.fmp4WriterSeeker.Write(data)
}

func TestMP4AACStopsAtFirstWriteError(t *testing.T) {
	output := &aacFailingWriter{fmp4WriterSeeker: newFmp4WriterSeeker(4096)}
	muxer, err := CreateMp4Muxer(output)
	if err != nil {
		t.Fatal(err)
	}
	id := muxer.AddAudioTrack(MP4_CODEC_AAC)
	batch := bytes.Repeat(mp4TestAACFrame, 2)
	output.armed = true
	if err := muxer.Write(id, batch, 0, 0); !errors.Is(err, io.ErrClosedPipe) {
		t.Errorf("Write returned %v, want closed pipe", err)
	}
	if output.writes != 1 || len(muxer.tracks[id].samplelist) != 0 {
		t.Errorf("attempted %d writes and queued %d samples, want one failed write and no samples", output.writes, len(muxer.tracks[id].samplelist))
	}
	output.armed = false
	if err := muxer.Write(id, batch, 0, 0); err != nil {
		t.Fatal(err)
	}
	if got := len(muxer.tracks[id].samplelist); got != 2 {
		t.Errorf("queued %d samples after retry, want 2", got)
	}
}

func TestMP4AACBatchTimestampsUseCumulativeSamples(t *testing.T) {
	output := newFmp4WriterSeeker(4096)
	muxer, err := CreateMp4Muxer(output)
	if err != nil {
		t.Fatal(err)
	}
	id := muxer.AddAudioTrack(MP4_CODEC_AAC)
	const batchFrames = 14
	const denominator = 44100
	const samplesPerFrame = 1024
	if err := muxer.Write(id, bytes.Repeat(mp4TestAACFrame, batchFrames), 0, 0); err != nil {
		t.Fatal(err)
	}
	lastTime := uint64(batchFrames * samplesPerFrame * 1000 / denominator)
	if err := muxer.Write(id, mp4TestAACFrame, lastTime, lastTime); err != nil {
		t.Fatal(err)
	}
	if err := muxer.WriteTrailer(); err != nil {
		t.Fatal(err)
	}
	demuxer := CreateMp4Demuxer(bytes.NewReader(output.buffer))
	if _, err := demuxer.ReadHead(); err != nil {
		t.Fatal(err)
	}
	for i := uint64(0); i <= batchFrames; i++ {
		packet, err := demuxer.ReadPacket()
		if err != nil {
			t.Fatal(err)
		}
		want := i * samplesPerFrame * 1000 / denominator
		if packet.Pts != want || packet.Dts != want || !bytes.Equal(packet.Data[7:], mp4TestAACFrame[7:]) {
			t.Errorf("sample %d PTS/DTS = %d/%d, want %d/%d with matching AAC payload", i, packet.Pts, packet.Dts, want, want)
		}
	}
}
