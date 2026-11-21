package rtmp

import (
	"bytes"
	"testing"
)

func TestRTMPMessageStreamIDByteOrder(t *testing.T) {
	const streamID = 1
	name := makeStringItem("onMetaData")
	payload := name.encode()
	wire := []byte{
		5,       // Chunk stream ID in a type 0 header.
		0, 0, 0, // Timestamp.
		0, 0, byte(len(payload)), byte(Metadata_AMF0),
		1, 0, 0, 0, // Message stream ID.
	}
	wire = append(wire, payload...)

	var received uint32
	reader := newChunkStreamReader(FIX_CHUNK_SIZE)
	if err := reader.readRtmpMessage(wire, func(message *rtmpMessage) error {
		received = message.streamid
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if received != streamID {
		t.Errorf("message stream ID = %d, want %d", received, streamID)
	}
	var packet chunkPacket
	packet.decodeHead(wire)
	if packet.msgHdr.msgStreamId != streamID {
		t.Errorf("packet header stream ID = %d, want %d", packet.msgHdr.msgStreamId, streamID)
	}
}

func TestRTMPChunkStreamIDByteOrder(t *testing.T) {
	for _, tt := range []struct {
		id   uint32
		wire []byte
	}{
		{63, []byte{63}},
		{64, []byte{0, 0}},
		{319, []byte{0, 255}},
		{320, []byte{1, 0, 1}},
		{365, []byte{1, 45, 1}},
		{65599, []byte{1, 255, 255}},
	} {
		header := basicHead{fmt: 0, csid: tt.id}
		if got := header.encode(); !bytes.Equal(got, tt.wire) {
			t.Errorf("chunk stream ID %d encoded as % x, want % x", tt.id, got, tt.wire)
		}
	}
}
