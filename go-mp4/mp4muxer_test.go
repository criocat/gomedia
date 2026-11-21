package mp4

import (
	"encoding/binary"
	"fmt"
	"io"
	"io/ioutil"
	"os"
	"strconv"
	"testing"
	"time"

	"github.com/yapingcat/gomedia/go-codec"
	"github.com/yapingcat/gomedia/go-mpeg2"
)

func TestMP4HeaderTimesUseUTC(t *testing.T) {
	epoch := time.Date(1904, time.January, 1, 0, 0, 0, 0, time.UTC).Unix()
	for _, zone := range []*time.Location{
		time.FixedZone("UTC+3", 3*60*60),
		time.FixedZone("UTC-5", -5*60*60),
	} {
		t.Run(zone.String(), func(t *testing.T) {
			original := time.Local
			time.Local = zone
			t.Cleanup(func() { time.Local = original })

			before := time.Now().Unix() - epoch
			for _, box := range []struct {
				name   string
				encode func() (int, []byte)
			}{
				{"mvhd", NewMovieHeaderBox().Encode},
				{"mdhd", NewMediaHeaderBox().Encode},
				{"tkhd", NewTrackHeaderBox().Encode},
			} {
				_, data := box.encode()
				after := time.Now().Unix() - epoch
				created := int64(binary.BigEndian.Uint32(data[12:16]))
				modified := int64(binary.BigEndian.Uint32(data[16:20]))
				if created < before || created > after || modified != created {
					t.Errorf("%s: creation=%d, modification=%d, want both in [%d, %d]", box.name, created, modified, before, after)
				}
			}
		})
	}
}

func TestMP4TrackDurationWithBackwardDTS(t *testing.T) {
	for _, tc := range []struct {
		name string
		dts  []uint64
		want []uint32
	}{
		{"subtract backward step", []uint64{100, 110, 150, 130, 140}, []uint32{0, 0, 40, 20, 30}},
		{"saturate at zero", []uint64{100, 110, 150, 90, 100}, []uint32{0, 0, 40, 0, 10}},
		{"exactly zero", []uint64{100, 110, 150, 110}, []uint32{0, 0, 40, 0}},
		{"equal timestamps", []uint64{100, 110, 150, 150}, []uint32{0, 0, 40, 40}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var track mp4track
			for i, dts := range tc.dts {
				track.addSampleEntry(sampleEntry{dts: dts})
				if got := track.duration; got != tc.want[i] {
					t.Errorf("sample %d at DTS %d: duration = %d, want %d", i, dts, got, tc.want[i])
				}
			}
		})
	}
}

func TestMP4MuxerWritesDurationWithBackwardDTS(t *testing.T) {
	file, err := os.CreateTemp(t.TempDir(), "backward-dts-*.mp4")
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	muxer, err := CreateMp4Muxer(file)
	if err != nil {
		t.Fatal(err)
	}
	id := muxer.AddAudioTrack(MP4_CODEC_G711A, WithAudioSampleRate(8000), WithAudioChannelCount(1))
	for _, dts := range []uint64{0, 10, 50, 30, 40} {
		if err := muxer.Write(id, make([]byte, 80), dts, dts); err != nil {
			t.Fatal(err)
		}
	}
	if err := muxer.WriteTrailer(); err != nil {
		t.Fatal(err)
	}
	if _, err := file.Seek(0, io.SeekStart); err != nil {
		t.Fatal(err)
	}
	tracks, err := CreateMp4Demuxer(file).ReadHead()
	if err != nil && err != io.EOF {
		t.Fatal(err)
	}
	if len(tracks) != 1 || tracks[0].Duration != 30 {
		t.Fatalf("tracks = %+v, want one track with duration 30", tracks)
	}
}

func TestCreateMp4Reader(t *testing.T) {
	f, err := os.Open("jellyfish-3-mbps-hd.h264.mp4")
	if err != nil {
		fmt.Println(err)
		return
	}
	defer f.Close()
	for err == nil {
		nn := int64(0)
		size := make([]byte, 4)
		_, err = io.ReadFull(f, size)
		if err != nil {
			break
		}
		nn += 4
		boxtype := make([]byte, 4)
		_, err = io.ReadFull(f, boxtype)
		if err != nil {
			break
		}
		nn += 4
		var isize uint64 = uint64(binary.BigEndian.Uint32(size))
		if isize == 1 {
			size := make([]byte, 8)
			_, err = io.ReadFull(f, size)
			if err != nil {
				break
			}
			isize = binary.BigEndian.Uint64(size)
			nn += 8
		}
		fmt.Printf("Read Box(%s) size:%d\n", boxtype, isize)
		f.Seek(int64(isize)-nn, 1)
	}
}

func TestCreateMp4Muxer(t *testing.T) {

	f, err := os.Open("jellyfish-3-mbps-hd.h265")
	if err != nil {
		fmt.Println(err)
		return
	}
	defer f.Close()

	mp4filename := "jellyfish-3-mbps-hd.h265.mp4"
	mp4file, err := os.OpenFile(mp4filename, os.O_CREATE|os.O_RDWR, 0666)
	if err != nil {
		fmt.Println(err)
		return
	}
	defer mp4file.Close()

	buf, _ := ioutil.ReadAll(f)
	pts := uint64(0)
	dts := uint64(0)
	ii := [3]uint64{33, 33, 34}
	idx := 0

	type args struct {
		wh io.WriteSeeker
	}
	tests := []struct {
		name string
		args args
		want *Movmuxer
	}{
		{name: "muxer h264", args: args{wh: mp4file}, want: nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			muxer, err := CreateMp4Muxer(tt.args.wh)
			if err != nil {
				fmt.Println(err)
				return
			}
			tid := muxer.AddVideoTrack(MP4_CODEC_H265)
			cache := make([]byte, 0)
			codec.SplitFrameWithStartCode(buf, func(nalu []byte) bool {
				ntype := codec.H265NaluType(nalu)
				if !codec.IsH265VCLNaluType(ntype) {
					cache = append(cache, nalu...)
					return true
				}
				if len(cache) > 0 {
					cache = append(cache, nalu...)
					muxer.Write(tid, cache, pts, dts)
					cache = cache[:0]
				} else {
					muxer.Write(tid, nalu, pts, dts)
				}
				pts += ii[idx]
				dts += ii[idx]
				idx++
				idx = idx % 3
				return true
			})
			fmt.Printf("last dts %d\n", dts)
			muxer.WriteTrailer()
		})
	}
}

func TestMuxAAC(t *testing.T) {
	f, err := os.Open("test.aac")
	if err != nil {
		fmt.Println(err)
		return
	}
	defer f.Close()

	mp4filename := "aac.mp4"
	mp4file, err := os.OpenFile(mp4filename, os.O_CREATE|os.O_RDWR, 0666)
	if err != nil {
		fmt.Println(err)
		return
	}
	defer mp4file.Close()

	aac, _ := ioutil.ReadAll(f)
	var pts uint64 = 0
	//var dts uint64 = 0
	//var i int = 0
	samples := uint64(0)
	muxer, err := CreateMp4Muxer(mp4file)
	if err != nil {
		fmt.Println(err)
		return
	}

	tid := muxer.AddAudioTrack(MP4_CODEC_AAC)
	codec.SplitAACFrame(aac, func(aac []byte) {
		samples += 1024
		pts = samples * 1000 / 44100
		// if i < 3 {
		// 	pts += 23
		// 	dts += 23
		// 	i++
		// } else {
		// 	pts += 24
		// 	dts += 24
		// 	i = 0
		// }
		muxer.Write(tid, aac, pts, pts)
		//fmt.Println(pts)
	})
	muxer.WriteTrailer()
}

func TestMuxMp4(t *testing.T) {
	tsfilename := `demo.ts` // input
	tsfile, err := os.Open(tsfilename)
	if err != nil {
		fmt.Println(err)
		return
	}
	defer tsfile.Close()

	mp4filename := "test14.mp4" // output
	mp4file, err := os.OpenFile(mp4filename, os.O_CREATE|os.O_RDWR, 0666)
	if err != nil {
		fmt.Println(err)
		return
	}
	defer mp4file.Close()

	muxer, err := CreateMp4Muxer(mp4file)
	if err != nil {
		fmt.Println(err)
		return
	}
	vtid := muxer.AddVideoTrack(MP4_CODEC_H264)
	atid := muxer.AddAudioTrack(MP4_CODEC_AAC)

	afile, err := os.OpenFile("r.aac", os.O_CREATE|os.O_RDWR, 0666)
	if err != nil {
		fmt.Println(err)
		return
	}
	defer afile.Close()
	demuxer := mpeg2.NewTSDemuxer()
	demuxer.OnFrame = func(cid mpeg2.TS_STREAM_TYPE, frame []byte, pts uint64, dts uint64) {

		if cid == mpeg2.TS_STREAM_AAC {
			err = muxer.Write(atid, frame, uint64(pts), uint64(dts))
			if err != nil {
				panic(err)
			}
		} else if cid == mpeg2.TS_STREAM_H264 {
			fmt.Println("pts,dts,len", pts, dts, len(frame))
			err = muxer.Write(vtid, frame, uint64(pts), uint64(dts))
			if err != nil {
				panic(err)
			}
		} else {
			panic("unkwon cid " + strconv.Itoa(int(cid)))
		}
	}

	err = demuxer.Input(tsfile)
	if err != nil {
		panic(err)
	}

	err = muxer.WriteTrailer()
	if err != nil {
		panic(err)
	}
}
