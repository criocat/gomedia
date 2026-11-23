package mp4

import (
	"bytes"
	"errors"
	"io"
	"testing"
)

func TestMP4ReadPacketDistinguishesMissingDataFromEndOfSamples(t *testing.T) {
	const sampleSize = 160
	init := fragmentTestInit(t, MP4_CODEC_G711A)
	flags := TF_FLAG_DEAAULT_BASE_IS_MOOF | TF_FLAG_DEFAULT_SAMPLE_DURATION_PRESENT | TF_FLAG_DEFAULT_SAMPLE_SIZE_PRESENT
	makeMoof := func(offset int32) []byte {
		return fragmentTestMovie(1, fragmentTestBox("traf",
			fragmentTestHeader(1, flags, 0, 20, sampleSize),
			fragmentTestDecodeTime(0), fragmentTestRun(true, offset, 1)))
	}
	moof := makeMoof(int32(len(makeMoof(0)) + BasicBoxLen))
	sample := bytes.Repeat([]byte{0xd5}, sampleSize)
	file := append(init, moof...)
	file = append(file, fragmentTestBox("mdat", sample)...)

	for _, tc := range []struct {
		name string
		cut  int
	}{
		{"complete sample", 0},
		{"partial sample", sampleSize / 2},
		{"missing sample", sampleSize},
	} {
		t.Run(tc.name, func(t *testing.T) {
			demuxer := CreateMp4Demuxer(bytes.NewReader(file[:len(file)-tc.cut]))
			if _, err := demuxer.ReadHead(); err != nil {
				t.Fatal(err)
			}
			packet, err := demuxer.ReadPacket()
			if tc.cut != 0 {
				if packet != nil || !errors.Is(err, io.ErrUnexpectedEOF) {
					t.Fatalf("ReadPacket = %v, %v, want unexpected EOF", packet, err)
				}
				return
			}
			if err != nil || !bytes.Equal(packet.Data, sample) {
				t.Fatalf("complete packet = %v, %v, want intact audio", packet, err)
			}
			if _, err := demuxer.ReadPacket(); err != io.EOF {
				t.Errorf("end of samples = %v, want EOF", err)
			}
		})
	}
}
