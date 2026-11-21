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

func TestRTMPExtendedTimestampBoundary(t *testing.T) {
	payload := []byte{2, 0, 2, 'o', 'k'}
	for _, tt := range []struct {
		timestamp uint32
		short     []byte
		extended  []byte
	}{
		{0x00fffffe, []byte{0xff, 0xff, 0xfe}, nil},
		{0x00ffffff, []byte{0xff, 0xff, 0xff}, []byte{0, 0xff, 0xff, 0xff}},
		{0x01000000, []byte{0xff, 0xff, 0xff}, []byte{1, 0, 0, 0}},
	} {
		want := append([]byte{5}, tt.short...)
		want = append(want, 0, 0, byte(len(payload)), byte(Metadata_AMF0), 1, 0, 0, 0)
		want = append(want, tt.extended...)
		want = append(want, payload[:3]...)
		want = append(want, 0xc5)
		want = append(want, tt.extended...)
		want = append(want, payload[3:]...)

		writer := newChunkStreamWriter(5)
		writer.chunkSize = 3
		got := writer.writeData(payload, Metadata_AMF0, 1, tt.timestamp)
		if !bytes.Equal(got, want) {
			t.Errorf("timestamp %#x encoded as % x, want % x", tt.timestamp, got, want)
		}
	}
}

func TestRTMPExtendedTimestampContinuation(t *testing.T) {
	wire := []byte{
		5, 0xff, 0xff, 0xff, // Type 0 and extended timestamp marker.
		0, 0, 5, byte(Metadata_AMF0), 1, 0, 0, 0,
		1, 0, 0, 0, // Extended timestamp, 0x01000000.
		2, 0, 2, // First three bytes of the payload.
		// A complete message with a short timestamp on another chunk stream.
		8, 0, 0, 0x20, 0, 0, 3, byte(Metadata_AMF0), 2, 0, 0, 0,
		2, 0, 0,
		0xc5, 1, 0, 0, 0, // Type 3 and its repeated extended timestamp.
		'o', 'k',
	}
	reader := newChunkStreamReader(3)
	var messages []*rtmpMessage
	for i := range wire {
		if err := reader.readRtmpMessage(wire[i:i+1], func(msg *rtmpMessage) error {
			messages = append(messages, msg)
			return nil
		}); err != nil {
			t.Fatal(err)
		}
	}
	if len(messages) != 2 {
		t.Fatalf("received %d messages, want two", len(messages))
	}
	short := messages[0]
	if short.timestamp != 0x20 || short.streamid != 2 || !bytes.Equal(short.msg, []byte{2, 0, 0}) {
		t.Fatalf("unexpected message from the other chunk stream: %+v", short)
	}
	msg := messages[1]
	if msg.timestamp != 0x01000000 || !bytes.Equal(msg.msg, []byte{2, 0, 2, 'o', 'k'}) {
		t.Fatalf("unexpected timestamp %#x or payload % x", msg.timestamp, msg.msg)
	}
}
