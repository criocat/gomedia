package rtmp

import (
	"errors"

	"github.com/yapingcat/gomedia/go-flv"
)

// Protocol control messages MUST have message stream ID 0 (called as control stream) and chunk stream ID 2, and are sent with highest
// priority.

type MessageType int

const (
    //Protocol control messages
    SET_CHUNK_SIZE  MessageType = 1
    ABORT_MESSAGE   MessageType = 2
    ACKNOWLEDGEMENT MessageType = 3
    USER_CONTROL    MessageType = 4
    WND_ACK_SIZE    MessageType = 5
    SET_PEER_BW     MessageType = 6

    AUDIO             MessageType = 8
    VIDEO             MessageType = 9
    Command_AMF0      MessageType = 20
    Command_AMF3      MessageType = 17
    Metadata_AMF0     MessageType = 18
    Metadata_AMF3     MessageType = 15
    SharedObject_AMF0 MessageType = 19
    SharedObject_AMF3 MessageType = 16
    Aggregate         MessageType = 22
)

type rtmpMessage struct {
    timestamp uint32
    msg       []byte
    msgtype   MessageType
    streamid  uint32
}

func audioFormat(data []byte) (flv.FLV_SOUND_FORMAT, error) {
	if len(data) < 2 {
		return 0, errors.New("empty or truncated RTMP audio packet")
	}
	format := flv.FLV_SOUND_FORMAT(data[0] >> 4)
	switch format {
	case flv.FLV_AAC, flv.FLV_G711A, flv.FLV_G711U, flv.FLV_MP3:
		return format, nil
	default:
		return 0, errors.New("unsupported RTMP audio codec")
	}
}
