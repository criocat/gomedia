package rtcp

import (
	"bytes"
	"testing"
)

func TestSDESPacketWithTwoSources(t *testing.T) {
	want := []byte{
		0x82, 202, 0, 5,
		0, 0, 0, 1, 1, 1, 'a', 0,
		0, 0, 0, 2, 1, 2, 'b', 'c', 0, 0, 0, 0,
	}
	packet := NewSourceDescription()
	packet.Chunks = []SDESChunk{
		{SSRC: 1, Item: MakeCNameItem([]byte("a"))},
		{SSRC: 2, Item: MakeCNameItem([]byte("bc"))},
	}
	if got := packet.Encode(); !bytes.Equal(got, want) {
		t.Errorf("encoded SDES = %x, want %x", got, want)
	}

	decoded := NewSourceDescription()
	if err := decoded.Decode(want); err != nil {
		t.Fatal(err)
	}
	if decoded.SC != 2 || len(decoded.Chunks) != 2 {
		t.Fatalf("decoded sources = %d, chunks = %+v", decoded.SC, decoded.Chunks)
	}
	for i, name := range []string{"a", "bc"} {
		chunk := decoded.Chunks[i]
		if chunk.SSRC != uint32(i+1) || chunk.Item == nil || chunk.Item.Type != SDES_CNAME ||
			chunk.Item.Length != uint8(len(name)) || string(chunk.Item.Txt) != name {
			t.Errorf("unexpected SDES chunk %d: %+v", i, chunk)
		}
	}
}

func TestSDESDecodesAdditionalItemsAndEmptyChunk(t *testing.T) {
	wire := []byte{
		0xa2, 202, 0, 6, // Two sources, followed by RTCP padding.
		0, 0, 0, 1, 1, 1, 'a', 2, 2, 'b', 'c', 0,
		0, 0, 0, 2, 0, 0, 0, 0,
		0, 0, 0, 4,
	}
	packet := NewSourceDescription()
	if err := packet.Decode(wire); err != nil {
		t.Fatal(err)
	}
	if len(packet.Chunks) != 2 {
		t.Fatalf("decoded %d chunks, want 2", len(packet.Chunks))
	}
	if got := packet.Chunks[0]; got.SSRC != 1 || got.Item == nil || string(got.Item.Txt) != "a" {
		t.Errorf("first source = %+v", got)
	}
	if got := packet.Chunks[1]; got.SSRC != 2 || got.Item != nil {
		t.Errorf("second source = %+v, want an empty source with SSRC 2", got)
	}
	want := []byte{
		0xa2, 202, 0, 5,
		0, 0, 0, 1, 1, 1, 'a', 0,
		0, 0, 0, 2, 0, 0, 0, 0,
		0, 0, 0, 4,
	}
	if got := packet.Encode(); !bytes.Equal(got, want) {
		t.Errorf("encoded SDES = %x, want %x", got, want)
	}
	if err := packet.Decode([]byte{0x80, 202, 0, 0}); err != nil || len(packet.Chunks) != 0 {
		t.Errorf("decoding an empty SDES after reuse: chunks = %d, err = %v", len(packet.Chunks), err)
	}
}

func TestGenerateSDESFillsSourceAndItem(t *testing.T) {
	const source = 0x01020304
	ctx := NewRtcpContext(source, 1, 90000)
	packet := ctx.GenerateSDES(SDES_CNAME, "hi")
	want := []byte{0x81, 202, 0, 3, 1, 2, 3, 4, 1, 2, 'h', 'i', 0, 0, 0, 0}
	if got := packet.Encode(); !bytes.Equal(got, want) {
		t.Errorf("generated SDES = %x, want %x", got, want)
	}
}
