package mp4

import (
    "bytes"
    "encoding/binary"
    "io"
    "math"
)

// mp4EpochOffset is the number of seconds from 1904-01-01 UTC to the Unix epoch.
const mp4EpochOffset = 2082844800

const (
    BasicBoxLen = 8
    FullBoxLen  = 12
)

type BoxEncoder interface {
    Encode(buf []byte) (int, []byte)
}

type BoxDecoder interface {
    Decode(buf []byte) (int, error)
}

type BoxSize interface {
    Size() uint64
}

// aligned(8) class Box (unsigned int(32) boxtype, optional unsigned int(8)[16] extended_type) {
//     unsigned int(32) size;
//     unsigned int(32) type = boxtype;
//     if (size==1) {
//        unsigned int(64) largesize;
//     } else if (size==0) {
//        // box extends to end of file
//     }
//     if (boxtype==‘uuid’) {
//     unsigned int(8)[16] usertype = extended_type;
//  }
// }

type BasicBox struct {
    Size     uint64
    Type     [4]byte
    UserType [16]byte
}

func NewBasicBox(boxtype [4]byte) *BasicBox {
    return &BasicBox{
        Type: boxtype,
    }
}

func (box *BasicBox) Decode(r io.Reader) (int, error) {
    buf := make([]byte, 16)
    if n, err := io.ReadFull(r, buf[:8]); err != nil {
        return n, err
    }
    boxsize := binary.BigEndian.Uint32(buf)
    copy(box.Type[:], buf[4:8])
    nn := 8
    if boxsize == 1 {
        if n, err := io.ReadFull(r, buf[8:]); err != nil {
            if err == io.EOF {
                err = io.ErrUnexpectedEOF
            }
            return nn + n, err
        }
        box.Size = binary.BigEndian.Uint64(buf[nn:])
        nn += 8
    } else {
        box.Size = uint64(boxsize)
    }
    if bytes.Equal(box.Type[:], []byte("uuid")) {
        uuid := make([]byte, 16)
        if n, err := io.ReadFull(r, uuid); err != nil {
            if err == io.EOF {
                err = io.ErrUnexpectedEOF
            }
            return n + nn, err
        }
        copy(box.UserType[:], uuid[:])
        nn += 16
    }

    return nn, nil
}

func (box *BasicBox) Encode() (int, []byte) {
    extendedSize := box.Size > math.MaxUint32
    hasUserType := bytes.Equal(box.Type[:], []byte("uuid"))
    sizeHeaderLen := BasicBoxLen
    if extendedSize {
        sizeHeaderLen += 8
    }
    headerLen := sizeHeaderLen
    if hasUserType {
        headerLen += len(box.UserType)
    }
    bufferSize := box.Size
    if extendedSize || bufferSize == 0 {
        bufferSize = uint64(headerLen)
    }
    buf := make([]byte, bufferSize)
    if extendedSize {
        binary.BigEndian.PutUint32(buf, 1)
        binary.BigEndian.PutUint64(buf[BasicBoxLen:], box.Size)
    } else {
        binary.BigEndian.PutUint32(buf, uint32(box.Size))
    }
    copy(buf[4:], box.Type[:])
    if hasUserType {
        copy(buf[sizeHeaderLen:headerLen], box.UserType[:])
    }
    return headerLen, buf
}

// aligned(8) class FullBox(unsigned int(32) boxtype, unsigned int(8) v, bit(24) f) extends Box(boxtype) {
//     unsigned int(8) version = v;
//     bit(24) flags = f;
// }

type FullBox struct {
    Box     *BasicBox
    Version uint8
    Flags   [3]byte
}

func NewFullBox(boxtype [4]byte, version uint8) *FullBox {
    return &FullBox{
        Box:     NewBasicBox(boxtype),
        Version: version,
    }
}

func (box *FullBox) Size() uint64 {
    if box.Box.Size > 0 {
        return box.Box.Size
    } else {
        return 12
    }
}

func (box *FullBox) Decode(r io.Reader) (int, error) {
    buf := make([]byte, 4)
    if n, err := io.ReadFull(r, buf); err != nil {
        return n, err
    }
    box.Version = buf[0]
    copy(box.Flags[:], buf[1:])
    return 4, nil
}

// decodeCountedPayload starts after the basic header and expects a payload with a 32-bit count.
func (box *FullBox) decodeCountedPayload(r io.Reader, size uint32) (int, []byte, error) {
    const headerSize = 12
    const countSize = 4
    if size < headerSize+countSize {
        return 0, nil, io.ErrUnexpectedEOF
    }
    offset, err := box.Decode(r)
    if err != nil {
        return offset, nil, err
    }
    payloadSize := int64(size) - headerSize
    payload, err := io.ReadAll(io.LimitReader(r, payloadSize))
    if err != nil {
        return 0, nil, err
    }
    if int64(len(payload)) != payloadSize {
        return 0, nil, io.ErrUnexpectedEOF
    }
    return offset, payload, nil
}

func (box *FullBox) Encode() (int, []byte) {
    box.Box.Size = box.Size()
    offset, buf := box.Box.Encode()
    if box.Box.Size > math.MaxUint32 {
        // BasicBox.Encode returns only the basic header for extended sizes.
        buf = append(buf, 0, 0, 0, 0)
    }
    buf[offset] = box.Version
    copy(buf[offset+1:], box.Flags[:])
    return offset + 4, buf
}
