package rtmp

import "github.com/yapingcat/gomedia/go-flv"

func writeAudioMessages(muxer flv.AVTagMuxer, channel *chunkStreamWriter, streamID uint32, frame []byte, pts, dts uint32, output OutputCB) error {
	writeTag := func(tag []byte, timestamp uint32) error {
		return channel.sendMessage(tag, AUDIO, streamID, timestamp, output)
	}
	if aac, ok := muxer.(*flv.AACMuxer); ok {
		return aac.WriteTags(frame, dts, writeTag)
	}
	for _, tag := range muxer.Write(frame, pts, dts) {
		if err := writeTag(tag, dts); err != nil {
			return err
		}
	}
	return nil
}
