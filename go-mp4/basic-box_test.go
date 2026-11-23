package mp4

import (
	"bytes"
	"encoding/binary"
	"testing"
)

func TestBasicBoxEncodesMediaDataToEOF(t *testing.T) {
	box := NewBasicBox([4]byte{'m', 'd', 'a', 't'})
	headerSize, wire := box.Encode()
	want := []byte{0, 0, 0, 0, 'm', 'd', 'a', 't'}
	if headerSize != len(want) || !bytes.Equal(wire, want) {
		t.Errorf("header size = %d, bytes = %x, want %d and %x", headerSize, wire, len(want), want)
	}
}

func TestBasicBoxEncodesUserTypeWithExtendedSize(t *testing.T) {
	box := &BasicBox{
		Type:     [4]byte{'u', 'u', 'i', 'd'},
		Size:     1<<32 + 264,
		UserType: [16]byte{0x69, 0x71, 0x18, 0x5e, 0xa9, 0x94, 0x4f, 0x97, 0x82, 0x2c, 0xf1, 0xe4, 0x73, 0xc3, 0x34, 0x7a},
	}
	headerSize, wire := box.Encode()
	if headerSize != 32 || len(wire) != 32 {
		t.Fatalf("header size = %d, bytes written = %d, want 32 for both", headerSize, len(wire))
	}
	if binary.BigEndian.Uint32(wire[:4]) != 1 || binary.BigEndian.Uint64(wire[8:16]) != box.Size ||
		!bytes.Equal(wire[16:32], box.UserType[:]) {
		t.Errorf("encoded UUID header = %x, want extended size and user type", wire)
	}
	var decoded BasicBox
	if got, err := decoded.Decode(bytes.NewReader(wire)); err != nil || got != headerSize || decoded != *box {
		t.Errorf("decoded box = %+v, bytes = %d, error = %v, want %+v", decoded, got, err, *box)
	}
}
