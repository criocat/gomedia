package mp4

import (
    "bytes"
    "encoding/binary"
    "fmt"
    "io"
    "os"
    "testing"
)

func TestCountedFullBoxPayloads(t *testing.T) {
    trun := NewTrackRunBox()
    trun.Box.Flags = [3]byte{0, 0x0f, 0x05}
    trun.SampleCount = 1
    trun.Dataoffset = 64
    trun.FirstSampleFlags = 1
    trun.EntryList = &movtrun{entrys: []trunEntry{{sampleDuration: 3, sampleSize: 7, sampleFlags: 1, sampleCompositionTimeOffset: 2}}}
    _, encodedTrun := trun.Encode()

    // A single auxiliary offset with the optional type and parameter fields.
    saio := make([]byte, 4+8+4+8)
    saio[0], saio[3] = 1, 1
    copy(saio[4:], "cenc")
    binary.BigEndian.PutUint32(saio[12:], 1)
    binary.BigEndian.PutUint64(saio[16:], 32)

    // One sample with an eight-byte IV and one subsample.
    senc := make([]byte, 4+4+8+2+6)
    senc[3] = byte(UseSubsampleEncryption)
    binary.BigEndian.PutUint32(senc[4:], 1)
    binary.BigEndian.PutUint16(senc[16:], 1)
    binary.BigEndian.PutUint32(senc[20:], 32)

    for _, tc := range []struct {
        name   string
        data   []byte
        decode func([]byte) error
    }{
        {"saio", saio, func(data []byte) error {
            box := SaioBox{Box: new(FullBox)}
            return box.Decode(bytes.NewReader(data), uint32(8+len(data)))
        }},
        {"trun", encodedTrun[8:], func(data []byte) error {
            box := TrackRunBox{Box: new(FullBox)}
            _, err := box.Decode(bytes.NewReader(data), uint32(8+len(data)), 0)
            return err
        }},
        {"senc", senc, func(data []byte) error {
            box := SencBox{Box: new(FullBox)}
            _, err := box.Decode(bytes.NewReader(data), uint32(8+len(data)), 8)
            return err
        }},
    } {
        t.Run(tc.name, func(t *testing.T) {
            if err := tc.decode(tc.data); err != nil {
                t.Fatalf("complete box: %v", err)
            }
            for n := range tc.data {
                if err := tc.decode(tc.data[:n]); err != io.ErrUnexpectedEOF {
                    t.Errorf("short box of %d bytes: got %v, want %v", n, err, io.ErrUnexpectedEOF)
                }
            }
        })
    }
}

func TestCreateMovDemuxer(t *testing.T) {
    f, err := os.Open("source.200kbps.768x320.flv.mp4")
    if err != nil {
        fmt.Println(err)
        return
    }
    defer f.Close()
    vfile, _ := os.OpenFile("v.h264", os.O_CREATE|os.O_RDWR, 0666)
    defer vfile.Close()
    afile, _ := os.OpenFile("a.aac", os.O_CREATE|os.O_RDWR, 0666)
    defer afile.Close()
	demuxer := CreateMp4Demuxer(f)
    if infos, err := demuxer.ReadHead(); err != nil && err != io.EOF {
        fmt.Println(err)
    } else {
        fmt.Printf("%+v\n", infos)
    }
    mp4info := demuxer.GetMp4Info()
    fmt.Printf("%+v\n", mp4info)
    for {
        pkg, err := demuxer.ReadPacket()
        if err != nil {
            fmt.Println(err)
            break
        }
        fmt.Printf("track:%d,cid:%+v,pts:%d dts:%d\n", pkg.TrackId, pkg.Cid, pkg.Pts, pkg.Dts)
        if pkg.Cid == MP4_CODEC_H264 {
            vfile.Write(pkg.Data)
        } else if pkg.Cid == MP4_CODEC_AAC {
            afile.Write(pkg.Data)
        }
    }
}
