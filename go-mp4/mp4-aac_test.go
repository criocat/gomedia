package mp4

import (
	"bytes"
	"encoding/binary"
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

func TestMP4AACPreservesRateAboveAudioSampleEntryRange(t *testing.T) {
	const rate = 96000
	frame := append([]byte(nil), mp4TestAACFrame...)
	frame[2] &^= 0x3c // AAC sampling-frequency index 0 is 96000 Hz.
	output := newFmp4WriterSeeker(4096)
	muxer, err := CreateMp4Muxer(output)
	if err != nil {
		t.Fatal(err)
	}
	id := muxer.AddAudioTrack(MP4_CODEC_AAC)
	if err := muxer.Write(id, frame, 0, 0); err != nil {
		t.Fatal(err)
	}
	if err := muxer.WriteTrailer(); err != nil {
		t.Fatal(err)
	}

	entryType := bytes.Index(output.buffer, []byte("mp4a"))
	if entryType < 0 || len(output.buffer)-entryType < 32 {
		t.Fatal("missing mp4a sample entry")
	}
	rateField := output.buffer[entryType+28 : entryType+32]
	if encoded := binary.BigEndian.Uint32(rateField); encoded != 0 {
		t.Errorf("16.16 sample rate = %#x, want zero for 96 kHz", encoded)
	}
	check := func(t *testing.T) {
		demuxer := CreateMp4Demuxer(bytes.NewReader(output.buffer))
		tracks, err := demuxer.ReadHead()
		if err != nil || len(tracks) != 1 {
			t.Fatalf("ReadHead returned %v, %v", tracks, err)
		}
		if tracks[0].SampleRate != rate {
			t.Errorf("AAC sample rate = %d, want %d", tracks[0].SampleRate, rate)
		}
	}
	t.Run("zero_sample_entry_rate", check)
	// Older gomedia files wrapped 96000 to its low 16 bits.
	binary.BigEndian.PutUint32(rateField, (rate&0xffff)<<16)
	t.Run("legacy_wrapped_sample_entry_rate", check)
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

func TestMP4AACAcceptsID3BetweenFrames(t *testing.T) {
	// ID3v2.4 title metadata as written by FFmpeg.
	tag := []byte("ID3\x04\x00\x00\x00\x00\x00\x12TIT2\x00\x00\x00\x08\x00\x00\x03Review\x00")
	tagWithFooter := append([]byte(nil), tag...)
	tagWithFooter[5] = 0x10
	tagWithFooter = append(tagWithFooter, []byte("3DI")...)
	tagWithFooter = append(tagWithFooter, tagWithFooter[3:10]...)
	for _, tc := range []struct {
		name string
		tag  []byte
	}{
		{name: "plain", tag: tag},
		{name: "footer", tag: tagWithFooter},
	} {
		t.Run(tc.name, func(t *testing.T) {
			batch := append([]byte(nil), mp4TestAACFrame...)
			batch = append(batch, tc.tag...)
			batch = append(batch, mp4TestAACFrame...)
			output := newFmp4WriterSeeker(4096)
			muxer, err := CreateMp4Muxer(output)
			if err != nil {
				t.Fatal(err)
			}
			id := muxer.AddAudioTrack(MP4_CODEC_AAC)
			if err := muxer.Write(id, batch, 0, 0); err != nil {
				t.Fatal(err)
			}
			if err := muxer.WriteTrailer(); err != nil {
				t.Fatal(err)
			}
			demuxer := CreateMp4Demuxer(bytes.NewReader(output.buffer))
			if _, err := demuxer.ReadHead(); err != nil {
				t.Fatal(err)
			}
			for _, wantDTS := range []uint64{0, 23} {
				packet, err := demuxer.ReadPacket()
				if err != nil {
					t.Fatal(err)
				}
				if packet.Dts != wantDTS || !bytes.Equal(packet.Data[7:], mp4TestAACFrame[7:]) {
					t.Errorf("AAC payload %x at DTS %d, want original payload at %d", packet.Data[7:], packet.Dts, wantDTS)
				}
			}
			if _, err := demuxer.ReadPacket(); !errors.Is(err, io.EOF) {
				t.Errorf("ReadPacket after two packets returned %v, want EOF", err)
			}
		})
	}
}

func TestMP4AACChannelConfigurationSevenHasEightChannels(t *testing.T) {
	// One 48 kHz AAC-LC 7.1 frame produced by FFmpeg, with ADTS config 7.
	frame := []byte{
		0xff, 0xf1, 0x4d, 0xc0, 0x03, 0xff, 0xfc, 0x01, 0x18, 0x20, 0x01,
		0x08, 0x80, 0x23, 0x04, 0x60, 0x23, 0x10, 0x04, 0x60, 0x8c, 0x04,
		0xa2, 0x00, 0x8c, 0x11, 0x81, 0x84, 0x60, 0x00, 0x1c,
	}
	output := newFmp4WriterSeeker(4096)
	muxer, err := CreateMp4Muxer(output)
	if err != nil {
		t.Fatal(err)
	}
	id := muxer.AddAudioTrack(MP4_CODEC_AAC)
	if err := muxer.Write(id, bytes.Repeat(frame, 2), 0, 0); err != nil {
		t.Fatal(err)
	}
	if err := muxer.WriteTrailer(); err != nil {
		t.Fatal(err)
	}
	demuxer := CreateMp4Demuxer(bytes.NewReader(output.buffer))
	tracks, err := demuxer.ReadHead()
	if err != nil {
		t.Fatal(err)
	}
	if len(tracks) != 1 || tracks[0].ChannelCount != 8 || tracks[0].SampleRate != 48000 {
		t.Errorf("tracks = %+v, want eight channels at 48000 Hz", tracks)
	}
	packet, err := demuxer.ReadPacket()
	if err != nil {
		t.Fatal(err)
	}
	if config := (packet.Data[2]&1)<<2 | packet.Data[3]>>6; config != 7 {
		t.Errorf("AAC channel configuration = %d, want 7", config)
	}
}
