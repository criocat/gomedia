package flv

import (
	"bytes"
	"errors"
	"io"
	"reflect"
	"testing"

	"github.com/yapingcat/gomedia/go-codec"
)

// One stereo AAC-LC silence frame in ADTS at 44.1 kHz.
var flvTestAACFrame = []byte{0xff, 0xf1, 0x50, 0x80, 0x01, 0xbf, 0xfc, 0x21, 0x10, 0x04, 0x60, 0x8c, 0x1c}

func TestFlvWriterUpdatesAACConfigurationWhenSampleRateChanges(t *testing.T) {
	frame44100 := flvTestAACFrame
	frame48000 := append([]byte(nil), frame44100...)
	frame48000[2] = 0x4c // AAC-LC at 48 kHz.
	var file bytes.Buffer
	writer := CreateFlvWriter(&file)
	if err := writer.WriteFlvHeader(); err != nil {
		t.Fatal(err)
	}
	for i, frame := range [][]byte{frame44100, frame48000, frame48000, frame44100} {
		ts := uint32(25 * i)
		if err := writer.WriteAAC(frame, ts, ts); err != nil {
			t.Fatalf("WriteAAC %d: %v", i, err)
		}
	}
	reader := CreateFlvReader()
	var rates []int
	reader.OnFrame = func(cid codec.CodecID, data []byte, pts, dts uint32) {
		if cid != codec.CODECID_AUDIO_AAC || pts != dts || !bytes.Equal(data[7:], frame44100[7:]) {
			t.Fatalf("unexpected AAC payload at %d/%d", pts, dts)
		}
		var header codec.ADTS_Frame_Header
		header.Decode(data)
		rates = append(rates, codec.AACSampleIdxToSample(int(header.Fix_Header.Sampling_frequency_index)))
	}
	if err := reader.Input(file.Bytes()); err != nil {
		t.Fatal(err)
	}
	if want := []int{44100, 48000, 48000, 44100}; !reflect.DeepEqual(rates, want) {
		t.Errorf("decoded AAC rates = %v, want %v", rates, want)
	}
}

func TestFlvAACBatchUsesCumulativeTimestamps(t *testing.T) {
	const initialDTS = 0x00ffffe0 // The batch crosses the extended timestamp boundary.
	const frames44100 = 17
	frame48000 := append([]byte(nil), flvTestAACFrame...)
	frame48000[2] = 0x4c
	batch := bytes.Repeat(flvTestAACFrame, frames44100)
	batch = append(batch, frame48000...)
	batch = append(batch, frame48000...)
	var file bytes.Buffer
	writer := CreateFlvWriter(&file)
	if err := writer.WriteFlvHeader(); err != nil {
		t.Fatal(err)
	}
	if err := writer.WriteAAC(batch, initialDTS, initialDTS); err != nil {
		t.Fatal(err)
	}
	var got, want []uint32
	for i := uint32(0); i <= frames44100; i++ {
		want = append(want, initialDTS+i*1024000/44100)
	}
	// Preserve the fraction left over from the 44.1 kHz frames at the rate change.
	want = append(want, initialDTS+1024000*(frames44100*48000+44100)/(44100*48000))
	reader := CreateFlvReader()
	reader.OnFrame = func(_ codec.CodecID, frame []byte, pts, dts uint32) {
		if pts != dts || !bytes.Equal(frame[7:], flvTestAACFrame[7:]) {
			t.Errorf("unexpected AAC payload at %d/%d", pts, dts)
		}
		got = append(got, dts)
	}
	if err := reader.Input(file.Bytes()); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("AAC DTS = %v, want %v", got, want)
	}
}

type flvFailingWriter struct {
	bytes.Buffer
	writesUntilFailure int
}

func (writer *flvFailingWriter) Write(data []byte) (int, error) {
	if writer.writesUntilFailure > 0 {
		writer.writesUntilFailure--
		if writer.writesUntilFailure == 0 {
			return 0, io.ErrClosedPipe
		}
	}
	return writer.Buffer.Write(data)
}

func TestFlvAACRetryAfterFailedTag(t *testing.T) {
	for _, tc := range []struct {
		name      string
		failWrite int
	}{
		{name: "configuration", failWrite: 1},
		{name: "audio frame", failWrite: 2},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var output flvFailingWriter
			writer := CreateFlvWriter(&output)
			if err := writer.WriteFlvHeader(); err != nil {
				t.Fatal(err)
			}
			output.writesUntilFailure = tc.failWrite
			if err := writer.WriteAAC(flvTestAACFrame, 0, 0); !errors.Is(err, io.ErrClosedPipe) {
				t.Fatalf("first WriteAAC returned %v, want closed pipe", err)
			}
			if err := writer.WriteAAC(flvTestAACFrame, 0, 0); err != nil {
				t.Fatal(err)
			}

			var clean bytes.Buffer
			reference := CreateFlvWriter(&clean)
			if err := reference.WriteFlvHeader(); err != nil {
				t.Fatal(err)
			}
			if err := reference.WriteAAC(flvTestAACFrame, 0, 0); err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(output.Bytes(), clean.Bytes()) {
				t.Error("retry did not produce exactly one configuration tag and one audio tag")
			}
		})
	}
}
