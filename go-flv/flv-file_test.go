package flv

import (
	"bytes"
	"fmt"
	"io/ioutil"
	"os"
	"testing"

	"github.com/yapingcat/gomedia/go-codec"
)

func TestAVCSignedCompositionTime(t *testing.T) {
	for _, cts := range []int32{-1 << 23, -40, 0, 40, 1<<23 - 1} {
		data := WriteVideoTag(nil, false, FLV_AVC, cts, false)
		var tag VideoTag
		tag.Decode(data)
		if tag.CompositionTime != cts {
			t.Errorf("CompositionTime = %d, want %d", tag.CompositionTime, cts)
		}
	}
}

func testAVCParameterSets() (initialSPS, updatedSPS, pps, idr []byte) {
	// libx264 SPS for 16x16 and 32x16 with the same ID and PPS.
	initialSPS = []byte{0, 0, 0, 1, 0x67, 0x42, 0xc0, 0x0a, 0xda, 0x7b, 0x01, 0x10,
		0x00, 0x00, 0x03, 0x00, 0x10, 0x00, 0x00, 0x03, 0x00, 0x20, 0xf1, 0x22, 0x6a}
	updatedSPS = []byte{0, 0, 0, 1, 0x67, 0x42, 0xc0, 0x0a, 0xda, 0x2e, 0xc0, 0x44,
		0x00, 0x00, 0x03, 0x00, 0x04, 0x00, 0x00, 0x03, 0x00, 0x08, 0x3c, 0x48, 0x9a, 0x80}
	pps = []byte{0, 0, 0, 1, 0x68, 0xce, 0x0f, 0xc8}
	idr = []byte{0, 0, 0, 1, 0x65, 0x88, 0x84, 0x3a, 0x26, 0x28, 0x00, 0x09, 0x02, 0xc9, 0xe0}
	return
}

func TestAVCMuxerUpdatesSequenceHeader(t *testing.T) {
	initialSPS, updatedSPS, pps, idr := testAVCParameterSets()
	t.Run("resolution", func(t *testing.T) {
		initial := bytes.Join([][]byte{initialSPS, pps}, nil)
		updated := bytes.Join([][]byte{updatedSPS, pps}, nil)
		checkVideoSequenceUpdate(t, FLV_AVC, initial, updated, idr)
	})
	t.Run("PPS", func(t *testing.T) {
		// libx264 with chroma-qp-offset=2 produces a different PPS with the same ID.
		updatedPPS := []byte{0, 0, 0, 1, 0x68, 0xce, 0x0f, 0x24, 0x80}
		initial := bytes.Join([][]byte{updatedSPS, pps}, nil)
		updated := bytes.Join([][]byte{updatedSPS, updatedPPS}, nil)
		checkVideoSequenceUpdate(t, FLV_AVC, initial, updated, idr)
	})
}

func checkVideoSequenceUpdate(t *testing.T, cid FLV_VIDEO_CODEC_ID, initial, updated, idr []byte) {
	t.Helper()
	muxer := CreateVideoMuxer(cid)
	start := muxer.Write(append([]byte(nil), initial...), 0, 0)
	if len(start) != 1 {
		t.Fatalf("initial parameter sets produced %d tags, want one header", len(start))
	}
	frame := bytes.Join([][]byte{updated, idr}, nil)
	tags := muxer.Write(append([]byte(nil), frame...), 35, 40)
	if len(tags) != 2 {
		t.Fatalf("changed parameters produced %d tags, want header and frame", len(tags))
	}
	var header VideoTag
	header.Decode(tags[0])
	if header.AVCPacketType != PacketTypeSequenceStart || bytes.Equal(tags[0], start[0]) {
		t.Fatal("first tag must carry the updated configuration")
	}
	want := CreateVideoMuxer(cid).Write(append([]byte(nil), updated...), 0, 0)
	if len(want) != 1 || !bytes.Equal(tags[0], want[0]) {
		t.Fatal("updated header differs from a fresh header for the same parameters")
	}
	header.Decode(tags[1])
	if header.AVCPacketType != PacketTypeCodedFrames || header.CompositionTime != -5 {
		t.Fatal("second tag must carry the frame with its composition time")
	}
	var repeated []byte
	codec.SplitFrame(frame, func(nalu []byte) bool {
		repeated = append(repeated, 0, 0, 1)
		repeated = append(repeated, nalu...)
		return true
	})
	tags = muxer.Write(repeated, 75, 80)
	if len(tags) != 1 {
		t.Fatalf("repeated parameters produced %d tags, want one frame", len(tags))
	}
	header.Decode(tags[0])
	if header.AVCPacketType != PacketTypeCodedFrames || header.CompositionTime != -5 {
		t.Fatal("repeated parameters must only produce the frame")
	}
}

func TestAVCInBandParameterSets(t *testing.T) {
	initialSPS, updatedSPS, pps, idr := testAVCParameterSets()
	config, err := codec.CreateH264AVCCExtradata([][]byte{initialSPS}, [][]byte{pps})
	if err != nil {
		t.Fatal(err)
	}
	checkInBandSPSUpdate(t, FLV_AVC, config, updatedSPS, idr, bytes.Join([][]byte{updatedSPS, pps, idr}, nil))
}

func checkInBandSPSUpdate(t *testing.T, cid FLV_VIDEO_CODEC_ID, config, sps, idr, want []byte) {
	t.Helper()
	demuxer := CreateFlvVideoTagHandle(cid)
	var frame []byte
	demuxer.OnFrame(func(_ codec.CodecID, payload []byte, _ int) { frame = payload })
	if err := demuxer.Decode(WriteVideoTag(config, true, cid, 0, true)); err != nil {
		t.Fatal(err)
	}
	updateNAL := codec.ConvertAnnexBToAVCC(append([]byte(nil), sps...))
	update := WriteVideoTag(updateNAL, false, cid, 0, false)
	if err := demuxer.Decode(update); err != nil {
		t.Fatal(err)
	}
	for i := range update {
		update[i] = 0
	}
	picture := codec.ConvertAnnexBToAVCC(append([]byte(nil), idr...))
	if err := demuxer.Decode(WriteVideoTag(picture, true, cid, 0, false)); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(frame, want) {
		t.Fatalf("keyframe = %x, want %x", frame, want)
	}
}

func TestFlvReader_Input(t *testing.T) {
	t.Run("test_1", func(t *testing.T) {
		videoFile, _ := os.OpenFile("v.h264", os.O_CREATE|os.O_RDWR, 0666)
		defer videoFile.Close()
		audioFile, _ := os.OpenFile("a.aac", os.O_CREATE|os.O_RDWR, 0666)
		defer audioFile.Close()
		f := CreateFlvReader()
		f.OnFrame = func(cid codec.CodecID, frame []byte, pts, dts uint32) {
			if cid == codec.CODECID_VIDEO_H264 {
				videoFile.Write(frame)
			} else if cid == codec.CODECID_AUDIO_AAC {
				audioFile.Write(frame)
			}
		}
		fd, _ := os.Open("source.200kbps.768x320.flv")
		defer fd.Close()
		cache := make([]byte, 4096)
		for {
			n, err := fd.Read(cache)
			if err != nil {
				fmt.Println(err)
				break
			}
			f.Input(cache[0:n])
		}

		// content, _ := ioutil.ReadAll(fd)
		// if err := f.Input(content); err != nil {
		// 	t.Errorf("FlvReader.Input() error = %v", err)
		// }
	})
}

func TestFlvWriter_Write(t *testing.T) {

	t.Run("test_2", func(t *testing.T) {
		newflv, _ := os.OpenFile("new.flv", os.O_CREATE|os.O_RDWR, 0666)
		defer newflv.Close()
		wf := CreateFlvWriter(newflv)
		wf.WriteFlvHeader()
		rf := CreateFlvReader()
		rf.OnFrame = func(cid codec.CodecID, frame []byte, pts, dts uint32) {
			if cid == codec.CODECID_VIDEO_H264 {
				if err := wf.WriteH264(frame, pts, dts); err != nil {
					fmt.Println(err)
				}
			} else if cid == codec.CODECID_AUDIO_AAC {
				if err := wf.WriteAAC(frame, pts, dts); err != nil {
					fmt.Println(err)
				}
			}
		}
		fd, _ := os.Open("source.200kbps.768x320.flv")
		defer fd.Close()
		content, _ := ioutil.ReadAll(fd)
		if err := rf.Input(content); err != nil {
			t.Errorf("FlvReader.Input() error = %v", err)
		}
	})
}

func TestFlvWriter_WriteHevc(t *testing.T) {

	t.Run("test_3", func(t *testing.T) {
		newflv, _ := os.OpenFile("h265.flv", os.O_CREATE|os.O_RDWR, 0666)
		defer newflv.Close()
		wf := CreateFlvWriter(newflv)
		wf.WriteFlvHeader()
		var pts uint32 = 0
		var dts uint32 = 0
		rawh265, err := os.Open("1.h265")
		if err != nil {
			fmt.Println(err)
			return
		}
		defer rawh265.Close()
		buf, _ := ioutil.ReadAll(rawh265)
		codec.SplitFrameWithStartCode(buf, func(nalu []byte) bool {
			fmt.Printf("%x %x %x %x %x\n", nalu[0], nalu[1], nalu[2], nalu[3], nalu[4])
			fmt.Printf("nalu size %d\n", len(nalu))
			if err := wf.WriteH265(nalu, pts, dts); err != nil {
				fmt.Println(err)
			}
			pts += 40
			dts += 40
			return true
		})
	})
}

func TestFlvReadH265(t *testing.T) {

	t.Run("test_4", func(t *testing.T) {
		videoFile, _ := os.OpenFile("v2.h265", os.O_CREATE|os.O_RDWR, 0666)
		defer videoFile.Close()
		f := CreateFlvReader()
		f.OnFrame = func(cid codec.CodecID, frame []byte, pts, dts uint32) {
			if cid == codec.CODECID_VIDEO_H265 {
				videoFile.Write(frame)
			}
		}
		fd, _ := os.Open("l.flv")
		defer fd.Close()
		content, _ := ioutil.ReadAll(fd)
		if err := f.Input(content); err != nil {
			t.Errorf("FlvReader.Input() error = %v", err)
		}
	})
}
