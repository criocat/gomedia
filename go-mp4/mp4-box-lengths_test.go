package mp4

import (
	"bytes"
	"encoding/binary"
	"errors"
	"io"
	"testing"
)

func TestMP4SkipsExtendedPaddingAndUnknownBoxes(t *testing.T) {
	file, sample := mp4BoxLengthFixture(t)
	for _, kind := range []string{"free", "skip", "uuid"} {
		t.Run(kind, func(t *testing.T) {
			length := 16
			if kind == "uuid" {
				length += 16
			}
			prefix := make([]byte, length)
			binary.BigEndian.PutUint32(prefix, 1)
			copy(prefix[4:], kind)
			binary.BigEndian.PutUint64(prefix[8:], uint64(length))
			checkMP4BoxLengthSample(t, append(prefix, file...), sample)
		})
	}
}

func TestMP4ReadsFinalMDATWithUnspecifiedSize(t *testing.T) {
	file, sample := mp4BoxLengthFixture(t)
	mdat := bytes.Index(file, []byte("mdat"))
	if mdat < 4 || int(binary.BigEndian.Uint32(file[mdat-4:])) != len(file)-mdat+4 {
		t.Fatal("fixture must end with mdat")
	}
	binary.BigEndian.PutUint32(file[mdat-4:], 0)
	checkMP4BoxLengthSample(t, file, sample)
}

func mp4BoxLengthFixture(t *testing.T) (file, sample []byte) {
	t.Helper()
	file = fragmentTestInit(t, MP4_CODEC_G711A)
	sample = bytes.Repeat([]byte{0xd5}, 160)
	flags := TF_FLAG_DEAAULT_BASE_IS_MOOF | TF_FLAG_DEFAULT_SAMPLE_DURATION_PRESENT | TF_FLAG_DEFAULT_SAMPLE_SIZE_PRESENT
	header := fragmentTestHeader(1, flags, 0, 20, uint32(len(sample)))
	makeMoof := func(offset int32) []byte {
		return fragmentTestMovie(1, fragmentTestBox("traf", header, fragmentTestDecodeTime(0),
			fragmentTestRun(true, offset, 1)))
	}
	file = append(file, makeMoof(int32(len(makeMoof(0))+BasicBoxLen))...)
	file = append(file, fragmentTestBox("mdat", sample)...)
	return file, sample
}

func checkMP4BoxLengthSample(t *testing.T, file, sample []byte) {
	t.Helper()
	demuxer := CreateMp4Demuxer(bytes.NewReader(file))
	tracks, err := demuxer.ReadHead()
	if err != nil || len(tracks) != 1 {
		t.Fatalf("ReadHead = %v, %v, want one track", tracks, err)
	}
	packet, err := demuxer.ReadPacket()
	if err != nil || packet == nil || !bytes.Equal(packet.Data, sample) {
		t.Fatalf("ReadPacket = %v, %v, want intact audio", packet, err)
	}
	if _, err := demuxer.ReadPacket(); !errors.Is(err, io.EOF) {
		t.Errorf("end of samples = %v, want EOF", err)
	}
}
