package rtcp

import (
    "encoding/binary"
    "errors"
)

//  	  0                   1                   2                   3
//  	  0 1 2 3 4 5 6 7 8 9 0 1 2 3 4 5 6 7 8 9 0 1 2 3 4 5 6 7 8 9 0 1
// 	     +-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+
// 	     |V=2|P|    SC   |   PT=BYE=203  |             length            |
// 	     +-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+
// 	     |                           SSRC/CSRC                           |
// 	     +-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+
// 	     :                              ...                              :
// 	     +=+=+=+=+=+=+=+=+=+=+=+=+=+=+=+=+=+=+=+=+=+=+=+=+=+=+=+=+=+=+=+=+
// (opt) |     length    |            reason for leaving     ...
// 	     +-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+

type Bye struct {
    Comm
    SC        uint8
    ReasonLen uint8
    Reason    string
    SSRCS     []uint32
}

func NewBye() *Bye {
    return &Bye{
        Comm: Comm{PT: RTCP_BYE},
    }
}

func (pkt *Bye) Decode(data []byte) error {
    if err := pkt.Comm.Decode(data); err != nil {
        return err
    }
    pkt.SC = data[0] & 0x1F
    payload := data[4 : 4+int(pkt.PayloadLen)]
    sourceBytes := 4 * int(pkt.SC)
    if len(payload) < sourceBytes {
        return errors.New("incomplete RTCP BYE sources")
    }
    pkt.SSRCS = pkt.SSRCS[:0]
    for i := 0; i < int(pkt.SC); i++ {
        pkt.SSRCS = append(pkt.SSRCS, binary.BigEndian.Uint32(payload[i*4:]))
    }
    pkt.ReasonLen, pkt.Reason = 0, ""
    if reason := payload[sourceBytes:]; len(reason) > 0 {
        if int(reason[0]) > len(reason)-1 {
            return errors.New("incomplete RTCP BYE reason")
        }
        pkt.ReasonLen = reason[0]
        pkt.Reason = string(reason[1 : 1+int(pkt.ReasonLen)])
    }
    return nil
}

func (pkt *Bye) Encode() []byte {
    if len(pkt.SSRCS) > 31 || len(pkt.Reason) > 255 {
        return nil
    }
    pkt.SC = uint8(len(pkt.SSRCS))
    pkt.ReasonLen = uint8(len(pkt.Reason))
    length := len(pkt.SSRCS) * 4
    if pkt.ReasonLen != 0 {
        length += (len(pkt.Reason) + 4) &^ 3 // Length byte, reason, and 32-bit alignment.
    }
    common := pkt.Comm
    if common.Padding {
        paddingLength := (len(common.PaddingData) + 4) &^ 3
        if paddingLength > 255 {
            return nil
        }
        padding := make([]byte, paddingLength-1)
        copy(padding, common.PaddingData)
        common.PaddingData = padding
        length += paddingLength
    }
    pkt.Comm.Length = uint16(length / 4)
    common.Length = pkt.Comm.Length
    data := common.Encode()
    data[0] |= pkt.SC
    offset := 4
    for _, ssrc := range pkt.SSRCS {
        binary.BigEndian.PutUint32(data[offset:], ssrc)
        offset += 4
    }
    if len(pkt.Reason) > 0 {
        data[offset] = pkt.ReasonLen
        copy(data[offset+1:], pkt.Reason)
    }
    return data
}
