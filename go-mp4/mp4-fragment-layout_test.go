package mp4

import (
	"bytes"
	"encoding/binary"
	"io"
	"testing"
)

func TestMP4FragmentUsesTrackExtendsDefaults(t *testing.T) {
	init := fragmentTestInit(t, MP4_CODEC_G711A)
	trex := bytes.Index(init, []byte("trex"))
	if trex < 0 {
		t.Fatal("missing trex in initialization segment")
	}
	binary.BigEndian.PutUint32(init[trex+16:], 20)
	binary.BigEndian.PutUint32(init[trex+20:], 160)

	var file []byte
	file = append(file, init...)
	for i, tc := range []struct {
		flags    uint32
		duration uint32
		size     uint32
	}{
		{0, 20, 160},
		{TF_FLAG_DEFAULT_SAMPLE_DURATION_PRESENT | TF_FLAG_DEFAULT_SAMPLE_SIZE_PRESENT, 10, 80},
	} {
		tfhd := fragmentTestHeader(1, TF_FLAG_DEAAULT_BASE_IS_MOOF|tc.flags, 0, tc.duration, tc.size)
		tfdt := fragmentTestDecodeTime(uint64(40 * i))
		moof := fragmentTestMovie(uint32(i+1), fragmentTestBox("traf", tfhd, tfdt, fragmentTestRun(true, 0, 2)))
		moof = fragmentTestMovie(uint32(i+1), fragmentTestBox("traf", tfhd, tfdt, fragmentTestRun(true, int32(len(moof)+8), 2)))
		file = append(file, moof...)
		file = append(file, fragmentTestBox("mdat", bytes.Repeat([]byte{byte(i + 1)}, int(tc.size)*2))...)
	}

	demuxer := CreateMp4Demuxer(bytes.NewReader(file))
	if _, err := demuxer.ReadHead(); err != nil {
		t.Fatal(err)
	}
	for i, want := range []struct {
		dts  uint64
		size int
		data byte
	}{
		{0, 160, 1},
		{20, 160, 1},
		{40, 80, 2},
		{50, 80, 2},
	} {
		packet, err := demuxer.ReadPacket()
		if err != nil {
			t.Fatalf("sample %d: %v", i, err)
		}
		if packet.Dts != want.dts || packet.Pts != want.dts || !bytes.Equal(packet.Data, bytes.Repeat([]byte{want.data}, want.size)) {
			t.Fatalf("sample %d: PTS/DTS/size = %d/%d/%d, want %d/%d/%d and intact audio",
				i, packet.Pts, packet.Dts, len(packet.Data), want.dts, want.dts, want.size)
		}
	}
	if _, err := demuxer.ReadPacket(); err != io.EOF {
		t.Fatalf("end of samples = %v, want EOF", err)
	}
}

func TestMP4FragmentResolvesSampleDataOffsets(t *testing.T) {
	init := fragmentTestInit(t, MP4_CODEC_G711A, MP4_CODEC_G711U)
	const defaults = TF_FLAG_DEFAULT_SAMPLE_DURATION_PRESENT | TF_FLAG_DEFAULT_SAMPLE_SIZE_PRESENT
	first := fragmentTestHeader(1, TF_FLAG_DEAAULT_BASE_IS_MOOF|defaults, 0, 20, 160)
	second := fragmentTestHeader(2, defaults, 0, 20, 160)

	makeFirst := func(offset int32) []byte {
		return fragmentTestMovie(1,
			fragmentTestBox("traf", first, fragmentTestDecodeTime(0), fragmentTestRun(true, offset, 1)),
			fragmentTestBox("traf", second, fragmentTestDecodeTime(20), fragmentTestRun(true, 1, 1), fragmentTestRun(false, 0, 1)),
		)
	}
	moof := makeFirst(int32(len(makeFirst(0)) + 8))
	file := append(init, moof...)
	file = append(file, fragmentTestBox("mdat", bytes.Repeat([]byte{1}, 160), []byte{0}, bytes.Repeat([]byte{2}, 160), bytes.Repeat([]byte{3}, 160))...)

	makeSecond := func(base uint64) []byte {
		header := fragmentTestHeader(1, TF_FLAG_BASE_DATA_OFFSET|defaults, base, 20, 160)
		return fragmentTestMovie(2, fragmentTestBox("traf", header, fragmentTestDecodeTime(60), fragmentTestRun(false, 0, 1)))
	}
	// This fragment supplies the absolute payload position in tfhd, with
	// no trun data offset. The first sample begins exactly at that position.
	base := uint64(len(file) + len(makeSecond(0)) + 8)
	file = append(file, makeSecond(base)...)
	file = append(file, fragmentTestBox("mdat", bytes.Repeat([]byte{4}, 160))...)

	demuxer := CreateMp4Demuxer(bytes.NewReader(file))
	if _, err := demuxer.ReadHead(); err != nil {
		t.Fatal(err)
	}
	for i, track := range []int{1, 2, 2, 1} {
		packet, err := demuxer.ReadPacket()
		if err != nil {
			t.Fatalf("sample %d: %v", i, err)
		}
		dts := uint64(i * 20)
		if packet.TrackId != track || packet.Dts != dts || !bytes.Equal(packet.Data, bytes.Repeat([]byte{byte(i + 1)}, 160)) {
			t.Errorf("sample %d: track=%d DTS=%d size=%d, want track %d DTS %d and intact audio",
				i, packet.TrackId, packet.Dts, len(packet.Data), track, dts)
		}
	}
}

func fragmentTestInit(t *testing.T, codecs ...MP4_CODEC_TYPE) []byte {
	t.Helper()
	muxer, err := CreateMp4Muxer(newFmp4WriterSeeker(4096), WithMp4Flag(MP4_FLAG_FRAGMENT))
	if err != nil {
		t.Fatal(err)
	}
	for _, cid := range codecs {
		muxer.AddAudioTrack(cid, WithAudioSampleRate(8000), WithAudioChannelCount(1))
	}
	var init bytes.Buffer
	if err := muxer.WriteInitSegment(&init); err != nil {
		t.Fatal(err)
	}
	return init.Bytes()
}

func fragmentTestBox(typ string, payloads ...[]byte) []byte {
	box := make([]byte, 8)
	copy(box[4:], typ)
	for _, payload := range payloads {
		box = append(box, payload...)
	}
	binary.BigEndian.PutUint32(box, uint32(len(box)))
	return box
}

func fragmentTestMovie(sequence uint32, tracks ...[]byte) []byte {
	var header [8]byte
	binary.BigEndian.PutUint32(header[4:], sequence)
	return fragmentTestBox("moof", append([][]byte{fragmentTestBox("mfhd", header[:])}, tracks...)...)
}

func fragmentTestHeader(id, flags uint32, base uint64, duration, size uint32) []byte {
	tfhd := NewTrackFragmentHeaderBox(id)
	tfhd.Box.Flags = [3]byte{byte(flags >> 16), byte(flags >> 8), byte(flags)}
	tfhd.BaseDataOffset = base
	tfhd.DefaultSampleDuration = duration
	tfhd.DefaultSampleSize = size
	_, wire := tfhd.Encode()
	return wire
}

func fragmentTestRun(hasOffset bool, offset int32, count uint32) []byte {
	run := NewTrackRunBox()
	if hasOffset {
		run.Box.Flags[2] = byte(TR_FLAG_DATA_OFFSET)
		run.Dataoffset = offset
	}
	run.SampleCount = count
	_, wire := run.Encode()
	return wire
}

func fragmentTestDecodeTime(dts uint64) []byte {
	_, wire := NewTrackFragmentBaseMediaDecodeTimeBox(dts).Encode()
	return wire
}
