package rtcp

import (
    "encoding/binary"
    "errors"
)

//  0                   1                   2                   3
//  0 1 2 3 4 5 6 7 8 9 0 1 2 3 4 5 6 7 8 9 0 1 2 3 4 5 6 7 8 9 0 1
// +-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+
// |V=2|P| subtype |   PT=APP=204  |             length            |
// +-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+
// |                           SSRC/CSRC                           |
// +-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+
// |                          name (ASCII)                         |
// +-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+
// |                   application-dependent data                ...
// +-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+

type App struct {
    Comm
    SubType uint8
    SSRC    uint32
    Name    []byte
    AppData []byte
}

func NewApp() *App {
    return &App{
        Comm: Comm{PT: RTCP_APP},
    }
}

func (pkt *App) Decode(data []byte) error {
    if err := pkt.Comm.Decode(data); err != nil {
        return err
    }
    if pkt.PT != RTCP_APP {
        return errors.New("unexpected RTCP packet type for APP")
    }
    if pkt.PayloadLen < 8 {
        return errors.New("incomplete RTCP APP source or name")
    }
    pkt.SubType = data[0] & 0x1F
    pkt.SSRC = binary.BigEndian.Uint32(data[4:])
    pkt.Name = data[8:12]
    pkt.AppData = data[12 : 4+int(pkt.PayloadLen)]
    return nil
}

func (pkt *App) Encode() []byte {
    pkt.PT = RTCP_APP
    payloadBytes := (8 + len(pkt.AppData) + 3) &^ 3
    data := pkt.Comm.encodeAlignedPayload(payloadBytes)
    if data == nil {
        return nil
    }
    data[0] |= pkt.SubType & 0x1F
    binary.BigEndian.PutUint32(data[4:8], pkt.SSRC)
    copy(data[8:12], pkt.Name)
    copy(data[12:4+payloadBytes], pkt.AppData)
    return data
}
