package rtcp

import (
    "encoding/binary"
    "errors"
)

// 		     0                   1                   2                   3
// 		 	 0 1 2 3 4 5 6 7 8 9 0 1 2 3 4 5 6 7 8 9 0 1 2 3 4 5 6 7 8 9 0 1
// 			+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+
// header 	|V=2|P|    RC   |   PT=RR=201   |             length            |
// 			+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+
// 			|                     SSRC of packet sender                     |
// 			+=+=+=+=+=+=+=+=+=+=+=+=+=+=+=+=+=+=+=+=+=+=+=+=+=+=+=+=+=+=+=+=+
// report 	|                 SSRC_1 (SSRC of first source)                 |
// block  	+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+
// 1    	| fraction lost |       cumulative number of packets lost       |
// 			+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+
// 			|           extended highest sequence number received           |
// 			+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+
// 			|                      interarrival jitter                      |
// 			+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+
// 			|                         last SR (LSR)                         |
// 			+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+
// 			|                   delay since last SR (DLSR)                  |
// 			+=+=+=+=+=+=+=+=+=+=+=+=+=+=+=+=+=+=+=+=+=+=+=+=+=+=+=+=+=+=+=+=+
// report  	|                 SSRC_2 (SSRC of second source)                |
// block   	+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+
// 2       	:                               ...                             :
// 			+=+=+=+=+=+=+=+=+=+=+=+=+=+=+=+=+=+=+=+=+=+=+=+=+=+=+=+=+=+=+=+=+
// 			|                  profile-specific extensions                  |
// 			+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+

type ReportBlock struct {
    SSRC             uint32
    Fraction         uint8
    Lost             uint32 // Raw 24-bit signed cumulative loss field.
    ExtendHighestSeq uint32
    Jitter           uint32
    Lsr              uint32
    Dlsr             uint32
}

const reportBlockSize = 24

func limitReportBlocks(blocks []ReportBlock) []ReportBlock {
    const maxReportBlocks = 31
    if len(blocks) > maxReportBlocks {
        return blocks[:maxReportBlocks]
    }
    return blocks
}

func (rb *ReportBlock) Decode(data []byte) error {
    if len(data) < reportBlockSize {
        return errors.New("report block need more data")
    }
    rb.SSRC = binary.BigEndian.Uint32(data)
    rb.Fraction = data[4]
    rb.Lost = uint32(data[5])<<16 | uint32(data[6])<<8 | uint32(data[7])
    rb.ExtendHighestSeq = binary.BigEndian.Uint32(data[8:])
    rb.Jitter = binary.BigEndian.Uint32(data[12:])
    rb.Lsr = binary.BigEndian.Uint32(data[16:])
    rb.Dlsr = binary.BigEndian.Uint32(data[20:])
    return nil
}

func (rb *ReportBlock) Encode() []byte {
    data := make([]byte, reportBlockSize)
    binary.BigEndian.PutUint32(data, rb.SSRC)
    data[4] = rb.Fraction
    data[5] = byte(rb.Lost >> 16)
    data[6] = byte(rb.Lost >> 8)
    data[7] = byte(rb.Lost)
    binary.BigEndian.PutUint32(data[8:], rb.ExtendHighestSeq)
    binary.BigEndian.PutUint32(data[12:], rb.Jitter)
    binary.BigEndian.PutUint32(data[16:], rb.Lsr)
    binary.BigEndian.PutUint32(data[20:], rb.Dlsr)
    return data
}

type ReceiverReport struct {
    Comm
    RC     uint8
    SSRC   uint32
    Blocks []ReportBlock
}

func NewReceiverReport() *ReceiverReport {
    return &ReceiverReport{
        Comm: Comm{PT: RTCP_RR},
    }
}

func (pkt *ReceiverReport) Decode(data []byte) error {
    if err := pkt.Comm.Decode(data); err != nil {
        return err
    }
    pkt.RC = data[0] & 0x1F
    pkt.SSRC = binary.BigEndian.Uint32(data[4:])
    if int(pkt.RC)*reportBlockSize > len(data)-8 {
        return errors.New("rr rtcp packet need more data")
    }

    pkt.Blocks = pkt.Blocks[:0]
    offset := 8
    for i := 0; i < int(pkt.RC); i++ {
        block := ReportBlock{}
        block.Decode(data[offset:])
        pkt.Blocks = append(pkt.Blocks, block)
        offset += reportBlockSize
    }
    return nil
}

// Encode writes a single Receiver Report with up to 31 blocks.
// Additional blocks can be sent in another Receiver Report.
func (pkt *ReceiverReport) Encode() []byte {
    const headerSize = 8
    blocks := limitReportBlocks(pkt.Blocks)
    pkt.RC = uint8(len(blocks))
    pkt.Comm.Length = uint16((headerSize+reportBlockSize*len(blocks))/4 - 1)
    data := pkt.Comm.Encode()
    data[0] |= pkt.RC
    binary.BigEndian.PutUint32(data[4:], pkt.SSRC)
    offset := headerSize
    for _, block := range blocks {
        copy(data[offset:], block.Encode())
        offset += reportBlockSize
    }
    return data
}
