package mp4

import (
	"bytes"
	"encoding/binary"
	"errors"
	"io"
	"testing"
)

type rejectMP4FragmentWrite struct {
	*fmp4WriterSeeker
	reject func([]byte) bool
}

func (writer *rejectMP4FragmentWrite) Write(data []byte) (int, error) {
	if writer.reject != nil && writer.reject(data) {
		writer.reject = nil
		return 0, io.ErrClosedPipe
	}
	return writer.fmp4WriterSeeker.Write(data)
}

func TestMP4FragmentRetriesAfterRejectedHeader(t *testing.T) {
	checkMP4FragmentRetry(t, func(data []byte) bool {
		return len(data) >= 8 && string(data[4:8]) == "moof"
	})
}

func TestMP4FragmentKeepsAllTracksAfterRejectedPayload(t *testing.T) {
	checkMP4FragmentRetry(t, func(data []byte) bool {
		return bytes.Equal(data, bytes.Repeat([]byte{0x51}, 80))
	})
}

func checkMP4FragmentRetry(t *testing.T, reject func([]byte) bool) {
	t.Helper()
	output := &rejectMP4FragmentWrite{fmp4WriterSeeker: newFmp4WriterSeeker(4096), reject: reject}
	muxer, err := CreateMp4Muxer(output, WithMp4Flag(MP4_FLAG_FRAGMENT))
	if err != nil {
		t.Fatal(err)
	}
	type sample struct {
		data []byte
		dts  uint64
	}
	var samples []sample
	for i, cid := range []MP4_CODEC_TYPE{MP4_CODEC_G711A, MP4_CODEC_G711U} {
		track := muxer.AddAudioTrack(cid, WithAudioSampleRate(8000), WithAudioChannelCount(1))
		data := bytes.Repeat([]byte{byte(0x50 + i)}, 80)
		dts := uint64(i * 10)
		if err := muxer.Write(track, data, dts, dts); err != nil {
			t.Fatal(err)
		}
		samples = append(samples, sample{data, dts})
	}
	if err := muxer.FlushFragment(); !errors.Is(err, io.ErrClosedPipe) {
		t.Fatalf("first flush = %v, want closed pipe", err)
	}
	if err := muxer.FlushFragment(); err != nil {
		t.Fatalf("retry: %v", err)
	}

	var fragmentCount int
	for offset := 0; offset < len(output.buffer); {
		data := output.buffer[offset:]
		if len(data) < 8 {
			t.Fatal("partial MP4 box after retry")
		}
		size := int(binary.BigEndian.Uint32(data))
		if size < 8 || size > len(data) {
			t.Fatalf("invalid MP4 box size %d at %d", size, offset)
		}
		if string(data[4:8]) == "moof" {
			fragmentCount++
			if size < 24 || string(data[12:16]) != "mfhd" || binary.BigEndian.Uint32(data[20:24]) != 1 {
				t.Error("the retried fragment should retain sequence number 1")
			}
		}
		offset += size
	}
	if fragmentCount != 1 {
		t.Errorf("wrote %d fragments, want one complete fragment", fragmentCount)
	}

	demuxer := CreateMp4Demuxer(bytes.NewReader(output.buffer))
	if _, err := demuxer.ReadHead(); err != nil {
		t.Fatal(err)
	}
	for i, want := range samples {
		packet, err := demuxer.ReadPacket()
		if err != nil {
			t.Fatalf("sample %d: %v", i, err)
		}
		if packet.TrackId != i+1 || packet.Dts != want.dts || !bytes.Equal(packet.Data, want.data) {
			t.Errorf("sample %d: track=%d DTS=%d size=%d, want track=%d DTS=%d with original bytes",
				i, packet.TrackId, packet.Dts, len(packet.Data), i+1, want.dts)
		}
	}
	if _, err := demuxer.ReadPacket(); err != io.EOF {
		t.Errorf("next packet: %v, want EOF", err)
	}
}
