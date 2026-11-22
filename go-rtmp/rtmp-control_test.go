package rtmp

import (
	"encoding/binary"
	"reflect"
	"strings"
	"testing"
)

func captureAcknowledgements(t *testing.T, setOutput func(OutputCB)) *[]uint32 {
	t.Helper()
	var acknowledgements []uint32
	reader := newChunkStreamReader(FIX_CHUNK_SIZE)
	setOutput(func(data []byte) error {
		return reader.readRtmpMessage(data, func(message *rtmpMessage) error {
			if message.msgtype == ACKNOWLEDGEMENT {
				if len(message.msg) != 4 {
					t.Fatalf("unexpected ACK length %d", len(message.msg))
				}
				acknowledgements = append(acknowledgements, binary.BigEndian.Uint32(message.msg))
			}
			return nil
		})
	})
	return &acknowledgements
}

func rtmpTestMetadata(writer *chunkStreamWriter, timestamp uint32) []byte {
	name := makeStringItem("onMetaData")
	note := makeStringItem(strings.Repeat("a", 512))
	return writer.writeData(append(name.encode(), note.encode()...), Metadata_AMF0, 1, timestamp)
}

func TestRTMPClientPeerAcknowledgementLeavesReadCounterAlone(t *testing.T) {
	client := NewRtmpClient(WithWndAckSize(256))
	client.state = ReadChunk
	acks := captureAcknowledgements(t, client.SetOutput)
	control := newChunkStreamWriter(CHUNK_CHANNEL_USE_CTRL)
	wire := control.writeData(makeAcknowledgementSize(4096), ACKNOWLEDGEMENT, 0, 0)
	if err := client.Input(wire); err != nil {
		t.Fatal(err)
	}
	if len(*acks) != 0 {
		t.Errorf("received %d bytes but sent ACKs %v with a 256-byte window", len(wire), *acks)
	}
}

func TestRTMPClientAcknowledgesBeforeMessageCompletion(t *testing.T) {
	client := NewRtmpClient(WithWndAckSize(256))
	client.state = ReadChunk
	acks := captureAcknowledgements(t, client.SetOutput)
	wire := rtmpTestMetadata(newChunkStreamWriter(CHUNK_CHANNEL_NET_STREAM), 0)
	for _, part := range [][]byte{wire[:255], wire[255:256]} {
		if err := client.Input(part); err != nil {
			t.Fatal(err)
		}
	}
	if want := []uint32{256}; !reflect.DeepEqual(*acks, want) {
		t.Errorf("ACKs before message completion = %v, want %v", *acks, want)
	}
	if err := client.Input(wire[256:]); err != nil {
		t.Fatal(err)
	}
	if want := []uint32{256, uint32(len(wire))}; !reflect.DeepEqual(*acks, want) {
		t.Errorf("ACKs after message completion = %v, want %v", *acks, want)
	}
}

func TestRTMPServerAcknowledgesNegotiatedWindow(t *testing.T) {
	server := NewRtmpServerHandle()
	server.state = ReadChunk
	acks := captureAcknowledgements(t, server.SetOutput)
	const window = 256
	size := newChunkStreamWriter(CHUNK_CHANNEL_USE_CTRL).writeData(makeAcknowledgementSize(window), WND_ACK_SIZE, 0, 0)
	if err := server.Input(size); err != nil {
		t.Fatal(err)
	}
	message := rtmpTestMetadata(newChunkStreamWriter(CHUNK_CHANNEL_NET_STREAM), 0)
	limit := window - len(size)
	if err := server.Input(message[:limit]); err != nil {
		t.Fatal(err)
	}
	if want := []uint32{window}; !reflect.DeepEqual(*acks, want) {
		t.Errorf("server ACKs before message completion = %v, want %v", *acks, want)
	}
	if err := server.Input(message[limit:]); err != nil {
		t.Fatal(err)
	}
	if want := []uint32{window, uint32(len(size) + len(message))}; !reflect.DeepEqual(*acks, want) {
		t.Errorf("server ACKs after message completion = %v, want %v", *acks, want)
	}
}

func TestRTMPAbortDiscardsOnlySelectedChunkStream(t *testing.T) {
	client := NewRtmpClient()
	client.state = ReadChunk
	server := NewRtmpServerHandle()
	server.state = ReadChunk
	for _, tc := range []struct {
		name      string
		input     func([]byte) error
		reader    *chunkStreamReader
		timestamp *uint32
	}{
		{"client", client.Input, client.reader, &client.timestamp},
		{"server", server.Input, server.reader, &server.timestamp},
	} {
		t.Run(tc.name, func(t *testing.T) {
			input := func(data []byte) {
				t.Helper()
				if err := tc.input(data); err != nil {
					t.Fatal(err)
				}
			}
			const target = CHUNK_CHANNEL_NET_STREAM
			const other = CHUNK_CHANNEL_META
			const firstChunk = 12 + FIX_CHUNK_SIZE
			writer := newChunkStreamWriter(target)
			cancelled := rtmpTestMetadata(writer, 100)
			interleaved := rtmpTestMetadata(newChunkStreamWriter(other), 200)
			input(cancelled[:firstChunk])
			input(interleaved[:firstChunk])
			control := newChunkStreamWriter(CHUNK_CHANNEL_USE_CTRL)
			input(control.writeData(makeAcknowledgementSize(target), ABORT_MESSAGE, 0, 0))
			if got := len(tc.reader.cks[target].message); got != 0 {
				t.Fatalf("aborted stream still holds %d bytes", got)
			}
			if got := len(tc.reader.cks[other].message); got != FIX_CHUNK_SIZE {
				t.Fatalf("other stream retained %d bytes, want %d", got, FIX_CHUNK_SIZE)
			}
			replacement := writer.writeData(makeStringItem("onMetaData").encode(), Metadata_AMF0, 1, 140)
			if replacement[0]>>6 != 1 {
				t.Fatal("replacement must use a compressed RTMP header")
			}
			input(replacement)
			if *tc.timestamp != 140 {
				t.Errorf("replacement timestamp = %d, want 140", *tc.timestamp)
			}
			input(interleaved[firstChunk:])
			if *tc.timestamp != 200 {
				t.Errorf("other message timestamp = %d, want 200", *tc.timestamp)
			}
		})
	}
}
