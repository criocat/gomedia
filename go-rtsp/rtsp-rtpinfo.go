package rtsp

import (
    "strconv"
    "strings"
)

type RtpInfo struct {
    Url     string
    Seq     uint16
    Rtptime int64 // Negative when the optional RTP timestamp is absent.
}

func NewRtpInfo(url string, seq uint16) *RtpInfo {
    return &RtpInfo{Url: url, Seq: seq, Rtptime: -1}
}

func (info *RtpInfo) EncodeString() string {
    str := "url=" + info.Url + ";seq=" + strconv.FormatUint(uint64(info.Seq), 10)
    if info.Rtptime >= 0 {
        str += ";rtptime=" + strconv.FormatInt(info.Rtptime, 10)
    }
    return str
}

func (info *RtpInfo) Decode(str string) {
    *info = RtpInfo{Rtptime: -1}
    items := strings.Split(str, ";")
    for _, item := range items {
        kv := strings.SplitN(strings.TrimSpace(item), "=", 2)
        if len(kv) < 2 {
            continue
        }
        switch kv[0] {
        case "url":
            info.Url = kv[1]
        case "seq":
            if seq, err := strconv.ParseUint(kv[1], 10, 16); err == nil {
                info.Seq = uint16(seq)
            }
        case "rtptime":
            if timestamp, err := strconv.ParseUint(kv[1], 10, 32); err == nil {
                info.Rtptime = int64(timestamp)
            }
        }
    }
}
