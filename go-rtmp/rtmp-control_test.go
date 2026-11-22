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

func rtmpTestMetadata(writer *chunkStreamWriter) []byte {
	name := makeStringItem("onMetaData")
	note := makeStringItem(strings.Repeat("a", 512))
	return writer.writeData(append(name.encode(), note.encode()...), Metadata_AMF0, 1, 0)
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
	wire := rtmpTestMetadata(newChunkStreamWriter(CHUNK_CHANNEL_NET_STREAM))
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
