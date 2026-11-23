package rtmp

import (
	"reflect"
	"testing"
)

func TestOptionalStreamLengthErrorKeepsPlaying(t *testing.T) {
	for _, coalesced := range []bool{false, true} {
		name := "separate_reads"
		if coalesced {
			name = "coalesced_read"
		}
		t.Run(name, func(t *testing.T) {
			client := NewRtmpClient()
			client.state = ReadChunk
			client.lastMethod, client.lastMethodTid = CREATE_STREAM, 2
			client.streamName = "live"
			client.SetOutput(func([]byte) error { return nil })
			errorsReported := 0
			client.OnError(func(string, string) { errorsReported++ })
			writer := newChunkStreamWriter(CHUNK_CHANNEL_CMD)
			if err := client.Input(writer.writeData(makeCreateStreamRes(2, 1), Command_AMF0, 0, 0)); err != nil {
				t.Fatal(err)
			}

			start := makeStatusRes(0, NETSTREAM_PLAY_START, LEVEL_STATUS, "playing")
			failedLength := makeErrorRes(3, "error", "NetConnection.Call.Failed", "length is unavailable")
			wire := [][]byte{
				writer.writeData(start, Command_AMF0, 1, 0),
				writer.writeData(failedLength, Command_AMF0, 0, 0),
			}
			if coalesced {
				wire = [][]byte{append(wire[0], wire[1]...)}
			}
			for _, data := range wire {
				if err := client.Input(data); err != nil {
					t.Fatal(err)
				}
			}
			if got := client.GetState(); got != STATE_RTMP_PLAY_START || errorsReported != 0 {
				t.Fatalf("state = %d, error callbacks = %d, want playing without errors", got, errorsReported)
			}

			playError := makeErrorRes(4, "error", "NetStream.Play.Failed", "playback failed")
			if err := client.Input(writer.writeData(playError, Command_AMF0, 1, 0)); err != nil {
				t.Fatal(err)
			}
			if got := client.GetState(); got != STATE_RTMP_PLAY_FAILED || errorsReported != 1 {
				t.Errorf("state = %d, error callbacks = %d, want playback failure", got, errorsReported)
			}
		})
	}
}

func TestPlayStopStatusChangesStateAndAllowsRestart(t *testing.T) {
	for _, stop := range []StatusCode{NETSTREAM_PLAY_STOP, NETSTREAM_PLAY_UNPUBLISH_NOTIFY} {
		t.Run(string(stop), func(t *testing.T) {
			client := NewRtmpClient()
			client.state = ReadChunk
			var states []RtmpState
			var statusCodes []string
			client.OnStateChange(func(state RtmpState) { states = append(states, state) })
			client.OnStatus(func(code, _, _ string) { statusCodes = append(statusCodes, code) })
			writer := newChunkStreamWriter(CHUNK_CHANNEL_CMD)
			for _, code := range []StatusCode{NETSTREAM_PLAY_START, stop, NETSTREAM_PLAY_START} {
				status := makeStatusRes(0, code, LEVEL_STATUS, "stream status")
				if err := client.Input(writer.writeData(status, Command_AMF0, 1, 0)); err != nil {
					t.Fatal(err)
				}
				if code == stop && client.GetState() != STATE_RTMP_PLAY_STOP {
					t.Errorf("state after %s = %d, want stopped", stop, client.GetState())
				}
			}
			if want := []RtmpState{STATE_RTMP_PLAY_START, STATE_RTMP_PLAY_STOP, STATE_RTMP_PLAY_START}; !reflect.DeepEqual(states, want) {
				t.Errorf("state callbacks = %v, want %v", states, want)
			}
			if want := []string{string(NETSTREAM_PLAY_START), string(stop), string(NETSTREAM_PLAY_START)}; !reflect.DeepEqual(statusCodes, want) {
				t.Errorf("status callbacks = %v, want %v", statusCodes, want)
			}
		})
	}
}
