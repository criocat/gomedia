package rtcp

import (
    "encoding/binary"
    "errors"
)

//           0                   1                   2                   3
//           0 1 2 3 4 5 6 7 8 9 0 1 2 3 4 5 6 7 8 9 0 1 2 3 4 5 6 7 8 9 0 1
// 			+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+
//  header  |V=2|P|    SC   |  PT=SDES=202  |             length            |
// 			+=+=+=+=+=+=+=+=+=+=+=+=+=+=+=+=+=+=+=+=+=+=+=+=+=+=+=+=+=+=+=+=+
// chunk 1	|                          SSRC/CSRC_1                          |
// 	     	+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+
// 			|                           SDES items                          |
// 			|                              ...                              |
// 			+=+=+=+=+=+=+=+=+=+=+=+=+=+=+=+=+=+=+=+=+=+=+=+=+=+=+=+=+=+=+=+=+
// chunk 2  |                          SSRC/CSRC_2                          |
//    		+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+
// 			|                           SDES items                          |
// 			|                              ...                              |
// 			+=+=+=+=+=+=+=+=+=+=+=+=+=+=+=+=+=+=+=+=+=+=+=+=+=+=+=+=+=+=+=+=+

const (
    SDES_CNAME = 1
    SDES_NAME  = 2
    SDES_EMAIL = 3
    SDES_PHONE = 4
    SDES_LOC   = 5
    SDES_TOOL  = 6
    SDES_NOTE  = 7
    SDES_PRIV  = 8
)

type ChunkItem struct {
    Type   uint8
    Length uint8
    Txt    []byte
}

func (item *ChunkItem) Encode() []byte {
    if len(item.Txt) > 255 {
        return nil
    }
    data := make([]byte, 2+len(item.Txt))
    data[0] = item.Type
    data[1] = uint8(len(item.Txt))
    copy(data[2:], item.Txt)
    return data
}

func MakeCNameItem(name []byte) *ChunkItem {
    return &ChunkItem{
        Type:   SDES_CNAME,
        Length: uint8(len(name)),
        Txt:    append([]byte(nil), name...),
    }
}

type SDESChunk struct {
    SSRC uint32
    // Item holds the first item seen for this source, or nil for an empty chunk.
    Item *ChunkItem
}

type SourceDescription struct {
    Comm
    SC     uint8
    Chunks []SDESChunk
}

func NewSourceDescription() *SourceDescription {
    return &SourceDescription{
        Comm: Comm{PT: RTCP_SDES},
    }
}

func (pkt *SourceDescription) Decode(data []byte) error {
    if err := pkt.Comm.Decode(data); err != nil {
        return err
    }
    if pkt.PT != RTCP_SDES {
        return errors.New("unexpected RTCP packet type for SDES")
    }
    sourceCount := data[0] & 0x1F
    chunks := make([]SDESChunk, 0, sourceCount)
    payload := data[4 : 4+int(pkt.PayloadLen)]
    for i := 0; i < int(sourceCount); i++ {
        chunk, length, err := decodeSDESChunk(payload)
        if err != nil {
            return err
        }
        chunks = append(chunks, chunk)
        payload = payload[length:]
    }
    if len(payload) != 0 {
        return errors.New("unexpected data after RTCP SDES chunks")
    }
    pkt.SC = sourceCount
    pkt.Chunks = chunks
    return nil
}

func decodeSDESChunk(data []byte) (SDESChunk, int, error) {
    if len(data) < 4 {
        return SDESChunk{}, 0, errors.New("incomplete RTCP SDES source")
    }
    chunk := SDESChunk{SSRC: binary.BigEndian.Uint32(data)}
    offset := 4
    for offset < len(data) && data[offset] != 0 {
        if len(data)-offset < 2 {
            return SDESChunk{}, 0, errors.New("incomplete RTCP SDES item header")
        }
        itemType, textLength := data[offset], data[offset+1]
        end := offset + 2 + int(textLength)
        if end > len(data) {
            return SDESChunk{}, 0, errors.New("incomplete RTCP SDES item text")
        }
        if chunk.Item == nil {
            chunk.Item = &ChunkItem{
                Type:   itemType,
                Length: textLength,
                Txt:    append([]byte(nil), data[offset+2:end]...),
            }
        }
        offset = end
    }
    end := (offset + 4) &^ 3 // Include the terminator and align the next chunk.
    if end > len(data) {
        return SDESChunk{}, 0, errors.New("missing RTCP SDES chunk terminator or padding")
    }
    for _, value := range data[offset:end] {
        if value != 0 {
            return SDESChunk{}, 0, errors.New("invalid RTCP SDES chunk padding")
        }
    }
    return chunk, end, nil
}

func (pkt *SourceDescription) Encode() []byte {
    if len(pkt.Chunks) > 31 {
        return nil
    }
    // The header needs the aligned size of every chunk, including its terminator.
    items := make([][]byte, len(pkt.Chunks))
    payloadBytes := 0
    for i, chunk := range pkt.Chunks {
        if chunk.Item != nil {
            items[i] = chunk.Item.Encode()
            if items[i] == nil || chunk.Item.Type == 0 {
                return nil
            }
        }
        payloadBytes += (4 + len(items[i]) + 4) &^ 3
    }
    pkt.PT = RTCP_SDES
    data := pkt.Comm.encodeAlignedPayload(payloadBytes)
    if data == nil {
        return nil
    }
    pkt.SC = uint8(len(pkt.Chunks))
    data[0] |= pkt.SC
    offset := 4
    for i, chunk := range pkt.Chunks {
        binary.BigEndian.PutUint32(data[offset:], chunk.SSRC)
        offset += 4 + copy(data[offset+4:], items[i])
        // The zero-filled buffer already contains the terminator and padding.
        offset = (offset + 4) &^ 3
    }
    return data
}
