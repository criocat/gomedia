package rtmp

import (
	"encoding/binary"
	"errors"

	"github.com/yapingcat/gomedia/go-flv"
)

func dispatchAggregate(aggregate *rtmpMessage, dispatch func(*rtmpMessage) error) error {
	const headerSize, backPointerSize = 11, 4
	var initialTimestamp uint32
	for data := aggregate.msg; len(data) > 0; {
		if len(data) < headerSize+backPointerSize {
			return errors.New("incomplete RTMP aggregate sub-message")
		}
		var tag flv.FlvTag
		tag.Decode(data)
		tagEnd := headerSize + int(tag.DataSize)
		if tagEnd+backPointerSize > len(data) {
			return errors.New("invalid RTMP aggregate sub-message size")
		}
		if binary.BigEndian.Uint32(data[tagEnd:]) != uint32(tagEnd) {
			return errors.New("invalid RTMP aggregate back pointer")
		}
		timestamp := uint32(tag.TimestampExtended)<<24 | tag.Timestamp
		if len(data) == len(aggregate.msg) {
			initialTimestamp = timestamp
		}
		switch kind := MessageType(data[0]); kind {
		case AUDIO, VIDEO, Metadata_AMF0, Metadata_AMF3:
			message := rtmpMessage{
				timestamp: aggregate.timestamp + (timestamp - initialTimestamp),
				msg:       data[headerSize:tagEnd],
				msgtype:   kind,
				streamid:  aggregate.streamid,
			}
			if err := dispatch(&message); err != nil {
				return err
			}
		}
		data = data[tagEnd+backPointerSize:]
	}
	return nil
}
