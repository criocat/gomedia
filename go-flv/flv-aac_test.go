package flv

import (
	"bytes"
	"reflect"
	"testing"

	"github.com/yapingcat/gomedia/go-codec"
)

func TestFlvWriterUpdatesAACConfigurationWhenSampleRateChanges(t *testing.T) {
	frame44100 := []byte{0xff, 0xf1, 0x50, 0x80, 0x01, 0xbf, 0xfc, 0x21, 0x10, 0x04, 0x60, 0x8c, 0x1c}
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
