package rtmp

import (
	"fmt"
	"net"
	"os"
	"testing"
	"time"

	"github.com/yapingcat/gomedia/go-codec"
	"github.com/yapingcat/gomedia/go-flv"
)

func TestRtmpClient_Play(t *testing.T) {
    t.Run("play", func(t *testing.T) {
        c, err := net.Dial("tcp4", "49.235.110.177:1935")
        if err != nil {
            fmt.Println(err)
        }

        cli := NewRtmpClient(WithComplexHandshake(),
            WithComplexHandshakeSchema(HANDSHAKE_COMPLEX_SCHEMA1))

        cli.OnError(func(code, describe string) {
            fmt.Printf("rtmp error code:%s,describe:%s\n", code, describe)
        })

        cli.OnStatus(func(code, level, describe string) {
            fmt.Printf("rtmp onstatus code:%s,level:%s describe:%s\n", code, level, describe)
        })

        firstVideo := true
        firstAudio := true
        var fd *os.File = nil
        var fd2 *os.File = nil
        defer func() {
            if fd != nil {
                fd.Close()
            }
            if fd2 != nil {
                fd2.Close()
            }
        }()

        cli.OnFrame(func(cid codec.CodecID, pts, dts uint32, frame []byte) {
            if cid == codec.CODECID_VIDEO_H264 {
                if firstVideo {
                    fd, _ = os.OpenFile("v.h264", os.O_CREATE|os.O_RDWR, 0666)
                    firstVideo = false
                }
                fmt.Printf("recv frame id:%d, pts:%d , dts:%d\n", cid, pts, dts)
                fd.Write(frame)
            } else {
                if firstAudio {
                    fd2, _ = os.OpenFile("a.aac", os.O_CREATE|os.O_RDWR, 0666)
                    firstAudio = false
                }
                fd2.Write(frame)
            }
        })

        cli.SetOutput(func(data []byte) error {
            _, err := c.Write(data)
            return err
        })

        cli.Start("rtmp://49.235.110.177:1935/live/test")
        fmt.Println(*cli)
        buf := make([]byte, 4096)
        n := 0
        for err == nil {
            n, err = c.Read(buf)
            if err != nil {
                continue
            }
            cli.Input(buf[:n])
        }
        fmt.Println(err)
    })
}

func TestRtmpClient_Pub(t *testing.T) {

    t.Run("pub", func(t *testing.T) {
        c, err := net.Dial("tcp4", "49.235.110.177:1935")
        if err != nil {
            fmt.Println(err)
        }

        cli := NewRtmpClient(WithComplexHandshake(),
            WithComplexHandshakeSchema(HANDSHAKE_COMPLEX_SCHEMA1),
            WithEnablePublish())

        cli.OnError(func(code, describe string) {
            fmt.Printf("rtmp code:%s , describe:%s\n", code, describe)
        })

        isReady := make(chan struct{})
        cli.OnStatus(func(code, level, describe string) {
            fmt.Printf("rtmp onstatus code:%s,level:%s describe:%s\n", code, level, describe)
        })

        cli.OnStateChange(func(newState RtmpState) {
            if newState == STATE_RTMP_PUBLISH_START {
                fmt.Println("ready for publish")
                close(isReady)
            }
        })

        go func() {
            <-isReady
            fmt.Println("start to read flv")
            f := flv.CreateFlvReader()
            f.OnFrame = func(cid codec.CodecID, frame []byte, pts, dts uint32) {
                if cid == codec.CODECID_VIDEO_H264 {
                    fmt.Println("write video frame", pts, dts)
                    cli.WriteVideo(cid, frame, pts, dts)
                    time.Sleep(time.Millisecond * 33)
                } else if cid == codec.CODECID_AUDIO_AAC {
                    cli.WriteAudio(cid, frame, pts, dts)
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
        }()

        cli.SetOutput(func(data []byte) error {
            _, err := c.Write(data)
            return err
        })

        cli.Start("rtmp://49.235.110.177:1935/live/test")
        fmt.Println(*cli)
        buf := make([]byte, 4096)
        n := 0
        for err == nil {
            n, err = c.Read(buf)
            if err != nil {
                continue
            }
            cli.Input(buf[:n])
        }
        fmt.Println(err)
    })
}

func TestRTMPRejectInvalidAudioTags(t *testing.T) {
	for _, tc := range []struct {
		name string
		tag  []byte
	}{
		{"empty-message", nil},
		{"unknown-format", []byte{0xff, 1}},
		{"aac-without-packet-type", []byte{0xaf}},
		{"g711-without-payload", []byte{0x70}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server, client := NewRtmpServerHandle(), NewRtmpClient()
			for _, endpoint := range []struct {
				name   string
				handle func(*rtmpMessage) error
				bind   func(OnFrame)
			}{
				{"client", client.handleAudioMessage, client.OnFrame},
				{"server", server.handleAudioMessage, server.OnFrame},
			} {
				t.Run(endpoint.name, func(t *testing.T) {
					endpoint.bind(func(codec.CodecID, uint32, uint32, []byte) {
						t.Error("invalid audio tag emitted a frame")
					})
					if err := endpoint.handle(&rtmpMessage{msg: tc.tag}); err == nil {
						t.Fatal("accepted an invalid audio tag")
					}
					frames := 0
					endpoint.bind(func(cid codec.CodecID, _, _ uint32, payload []byte) {
						frames++
						if cid != codec.CODECID_AUDIO_G711A || len(payload) != 1 || payload[0] != 1 {
							t.Fatalf("unexpected valid audio frame: codec=%v, payload=%x", cid, payload)
						}
					})
					if err := endpoint.handle(&rtmpMessage{msg: []byte{0x70, 1}}); err != nil || frames != 1 {
						t.Fatalf("valid audio after rejection: err=%v, frames=%d", err, frames)
					}
				})
			}
		})
	}
}
