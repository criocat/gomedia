package ogg

import (
	"fmt"
	"os"
	"testing"

	"github.com/yapingcat/gomedia/go-codec"
)

func TestDemuxer_Input(t *testing.T) {

    t.Run("ogg demux", func(t *testing.T) {
        demuxer := NewDemuxer()
        demuxer.OnPacket = func(streamId uint32, granule uint64, packet []byte, lost int) {
            //fmt.Printf("onpacket sid:%d granule:%d package len:%d lost:%d\n", streamId, granule, len(packet), lost)
        }
        getAudioParam := false
        getVideoParam := false
        demuxer.OnFrame = func(streamId uint32, cid codec.CodecID, frame []byte, pts, dts uint64, lost int) {
            if cid == codec.CODECID_AUDIO_OPUS {
                param := demuxer.GetAudioParam()
                if param != nil && !getAudioParam {
                    fmt.Println(param)
                    getAudioParam = true
                }
                fmt.Printf("opus frame:sid[%d] frame len:[%d] pts:[%d] dts:[%d] lost:%d\n", streamId, len(frame), pts, dts, lost)
            } else if cid == codec.CODECID_VIDEO_VP8 {
                param := demuxer.GetVideoParam()
                if param != nil && !getVideoParam {
                    fmt.Println(param)
                    getVideoParam = true
                }
                fmt.Printf("vp8 frame:sid[%d] frame len:[%d] pts:[%d] dts:[%d] lost:%d\n", streamId, len(frame), pts, dts, lost)
            }
        }

        demuxer.OnPage = func(page *oggPage) {
            //	PrintPage(page)
        }
        oggfile, _ := os.Open("test.ogg")
        buf := make([]byte, 4096)
        for {
            n, err := oggfile.Read(buf)
            if err != nil {
                fmt.Println(err)
                break
            }
            //fmt.Printf("read buf %d\n", n)
            err = demuxer.Input(buf[0:n])
            if err != nil {
                fmt.Println(err)
            }
        }
    })
}

func TestVP8PacketTimingSkipsEmptyPackets(t *testing.T) {
    page := &oggPage{
        granulePos: 10<<32 | 1<<30,
        packets:    [][]byte{{0x10}, nil, {0x10}, {0x00}},
    }
    stream := &oggStream{currentPage: page}
    var vp8 vp8Demuxer
    // Visible frames count towards the remaining duration. Empty packets and
    // invisible frames occupy an index but contribute zero.
    want := []uint64{8, 9, 9, 10}
    for i, packet := range page.packets {
        _, pts, dts := vp8.packet(stream, packet)
        if pts != want[i] || dts != want[i] {
            t.Errorf("packet %d: PTS=%d DTS=%d, want %d", i, pts, dts, want[i])
        }
    }
}
