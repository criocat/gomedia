package flv

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/yapingcat/gomedia/go-codec"
)

func testHEVCConfig(lengthSize int) []byte {
	data := make([]byte, 23)
	data[0], data[21], data[22] = 1, byte(lengthSize-1), 3
	for _, typ := range []byte{32, 33, 34} {
		data = append(data, 0x80|typ, 0, 1, 0, 2, typ<<1, 1)
	}
	return append([]byte{0x90, 'h', 'v', 'c', '1'}, data...)
}

func TestHEVCEnhancedHeaders(t *testing.T) {
	for _, cts := range []int32{-8388608, -40, 0, 40, 8388607} {
		tag := WriteVideoTag([]byte{7}, false, FLV_HEVC, cts, false)
		packetType, headerSize := byte(PacketTypeCodedFrames), 8
		if cts == 0 {
			packetType, headerSize = PacketTypeCodedFramesX, 5
		}
		if tag[0] != 0xa0|packetType || string(tag[1:5]) != "hvc1" || len(tag) != headerSize+1 {
			t.Fatalf("cts=%d: wrong header %x", cts, tag)
		}
		var parsed VideoTag
		parsed.Decode(tag)
		if parsed.CompositionTime != cts || parsed.CodecId != byte(FLV_HEVC) {
			t.Fatalf("cts=%d: decoded %+v", cts, parsed)
		}
	}
	// Preserve the H.264 wire format and signed CTS semantics.
	tag := WriteVideoTag([]byte{7}, true, FLV_AVC, -40, false)
	if !bytes.Equal(tag, []byte{0x17, 1, 0xff, 0xff, 0xd8, 7}) {
		t.Fatalf("changed AVC format: %x", tag)
	}
}

func TestHEVCDemuxNALLengthsAndOwnership(t *testing.T) {
	for _, lengthSize := range []int{1, 2, 4} {
		d := NewHevcTagDemuxer()
		if err := d.Decode(testHEVCConfig(lengthSize)); err != nil {
			t.Fatal(err)
		}
		config := append([]byte(nil), d.SpsPpsVps...)
		var frames [][]byte
		d.OnFrame(func(cid codec.CodecID, frame []byte, cts int) {
			if cid != codec.CODECID_VIDEO_H265 || cts != -40 {
				t.Fatalf("wrong codec/timestamp: %v %d", cid, cts)
			}
			frames = append(frames, frame)
		})
		tag := []byte{0x91, 'h', 'v', 'c', '1', 0xff, 0xff, 0xd8}
		tag = append(tag, make([]byte, lengthSize)...)
		tag[len(tag)-1] = 3
		tag = append(tag, 19<<1, 1, 0x80)
		input := append([]byte(nil), tag...)
		if err := d.Decode(tag); err != nil {
			t.Fatal(err)
		}
		want := append(append([]byte(nil), config...), 0, 0, 0, 1, 19<<1, 1, 0x80)
		if len(frames) != 1 || !bytes.Equal(frames[0], want) || !bytes.Equal(tag, input) {
			t.Fatal("wrong Annex B frame or input was mutated")
		}
		tag[len(tag)-1] = 0x81
		if err := d.Decode(tag); err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(frames[0], want) || !bytes.Equal(d.SpsPpsVps, config) {
			t.Fatal("next frame overwrote retained frame/configuration")
		}
		// Legacy codec ID 12 is still accepted on input.
		legacy := append([]byte{0x1c, 1, 0xff, 0xff, 0xd8}, tag[8:]...)
		if err := d.Decode(legacy); err != nil {
			t.Fatal(err)
		}
	}
}

func TestHEVCRejectMalformedTags(t *testing.T) {
	validConfig := testHEVCConfig(4)
	for n := 0; n < len(validConfig); n++ {
		if err := NewHevcTagDemuxer().Decode(validConfig[:n]); err == nil {
			t.Fatalf("accepted truncated configuration of size %d", n)
		}
	}
	for _, data := range [][]byte{
		{0x91, 'h', 'v', 'c', '1'},
		{0x91, 'h', 'v', 'c', '1', 0, 0},
		{0x93, 'h', 'v', 'c', '1'},
		{0x93, 'h', 'v', 'c', '1', 0, 0, 0},
		{0x93, 'h', 'v', 'c', '1', 0, 0, 0, 9, 0x26, 1},
		{0x93, 'h', 'v', 'c', '1', 0, 0, 0, 0},
		{0x93, 'a', 'v', '0', '1'},
	} {
		d := NewHevcTagDemuxer()
		if err := d.Decode(validConfig); err != nil {
			t.Fatal(err)
		}
		if err := d.Decode(data); err == nil {
			t.Fatalf("accepted malformed tag %x", data)
		}
	}
	for n := 0; n < 5; n++ {
		GetFLVVideoCodecId(validConfig[:n]) // must not panic
	}
}

// Exercise independent FFmpeg-generated packets, then ask FFmpeg to decode our
// remuxed output when installed. The checked-in fixture keeps Go-only tests useful.
func TestHEVCFFmpegRoundTrip(t *testing.T) {
	input, err := os.ReadFile("testdata/hevc-aac.flv")
	if err != nil {
		t.Fatal(err)
	}
	var output bytes.Buffer
	w := CreateFlvWriter(&output)
	if err := w.WriteFlvHeader(); err != nil {
		t.Fatal(err)
	}
	d := NewHevcTagDemuxer()
	a := NewAACTagDemuxer()
	var dts uint32
	videoFrames, audioFrames, reordered := 0, 0, false
	var timestamps [][2]uint32
	d.OnFrame(func(cid codec.CodecID, frame []byte, cts int) {
		videoFrames++
		timestamps = append(timestamps, [2]uint32{dts + uint32(cts), dts})
		reordered = reordered || cts != 0
		// A trailing SEI must not hide the preceding VCL NAL from the muxer.
		frame = append(frame, 0, 0, 0, 1, 40<<1, 1, 0x80)
		if err := w.WriteH265(frame, dts+uint32(cts), dts); err != nil {
			t.Fatal(err)
		}
	})
	a.OnFrame(func(cid codec.CodecID, frame []byte) {
		audioFrames++
		if err := w.WriteAAC(frame, dts, dts); err != nil {
			t.Fatal(err)
		}
	})
	for rest := input[13:]; len(rest) > 0; {
		if len(rest) < 15 {
			t.Fatal("truncated fixture tag")
		}
		size := int(GetUint24(rest[1:4]))
		if len(rest) < size+15 {
			t.Fatal("truncated fixture payload")
		}
		dts = GetUint24(rest[4:7]) | uint32(rest[7])<<24
		switch rest[0] {
		case 9:
			err = d.Decode(rest[11 : 11+size])
		case 8:
			err = a.Decode(rest[11 : 11+size])
		}
		if err != nil {
			t.Fatal(err)
		}
		rest = rest[size+15:]
	}
	if videoFrames != 10 || audioFrames == 0 || !reordered {
		t.Fatalf("fixture coverage: video=%d audio=%d reordered=%v", videoFrames, audioFrames, reordered)
	}
	// Decode our output a second time, checking every video timestamp.
	d2 := NewHevcTagDemuxer()
	count := 0
	d2.OnFrame(func(_ codec.CodecID, _ []byte, cts int) {
		if count >= len(timestamps) || timestamps[count] != [2]uint32{dts + uint32(cts), dts} {
			t.Fatalf("changed PTS/DTS at frame %d", count)
		}
		count++
	})
	for rest := output.Bytes()[13:]; len(rest) > 0; {
		size := int(GetUint24(rest[1:4]))
		dts = GetUint24(rest[4:7]) | uint32(rest[7])<<24
		if rest[0] == 9 {
			if rest[11]&0x80 == 0 || string(rest[12:16]) != "hvc1" {
				t.Fatal("muxer emitted legacy HEVC")
			}
			if err := d2.Decode(rest[11 : 11+size]); err != nil {
				t.Fatal(err)
			}
		}
		rest = rest[size+15:]
	}
	if count != videoFrames {
		t.Fatalf("lost frames: %d != %d", count, videoFrames)
	}
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		t.Log("FFmpeg unavailable; external decoder check skipped")
		return
	}
	path := filepath.Join(t.TempDir(), "roundtrip.flv")
	if err := os.WriteFile(path, output.Bytes(), 0600); err != nil {
		t.Fatal(err)
	}
	if log, err := exec.Command("ffmpeg", "-v", "error", "-xerror", "-i", path, "-f", "null", "-").CombinedOutput(); err != nil {
		t.Fatalf("FFmpeg decode: %v\n%s", err, log)
	}
	// A decoder accepting the output does not prove that no frames were lost
	// or changed. Compare decoded pixel checksums with the independent fixture.
	checksums := func(path string) []string {
		log, err := exec.Command("ffmpeg", "-v", "error", "-xerror", "-i", path,
			"-map", "0:v:0", "-fps_mode", "passthrough", "-f", "framemd5", "-").CombinedOutput()
		if err != nil {
			t.Fatalf("FFmpeg frame checksums: %v\n%s", err, log)
		}
		var result []string
		for _, line := range strings.Split(string(log), "\n") {
			if line == "" || strings.HasPrefix(line, "#") {
				continue
			}
			fields := strings.Split(line, ",")
			result = append(result, strings.TrimSpace(fields[len(fields)-1]))
		}
		return result
	}
	want, got := checksums("testdata/hevc-aac.flv"), checksums(path)
	if len(got) != 10 || strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Fatalf("decoded pixels differ: got %v, want %v", got, want)
	}
}

func FuzzHEVCTagDecode(f *testing.F) {
	f.Add(testHEVCConfig(4))
	f.Add([]byte{0x93, 'h', 'v', 'c', '1', 0, 0, 0, 2, 0x26, 1})
	f.Fuzz(func(t *testing.T, data []byte) {
		d := NewHevcTagDemuxer()
		_ = d.Decode(testHEVCConfig(4))
		_ = d.Decode(data)
	})
}
