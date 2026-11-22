package rtmp

import "testing"

func TestRTMPClientReadsFirstChunkAfterHandshake(t *testing.T) {
	for _, tc := range []struct {
		name       string
		prefeed    int
		chunkBytes int
	}{
		{name: "same read", chunkBytes: 16},
		{name: "partial S2 and first chunk header", prefeed: HANDSHAKE_SIZE - 1, chunkBytes: 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			client := NewRtmpClient()
			var output [][]byte
			client.SetOutput(func(data []byte) error {
				output = append(output, append([]byte(nil), data...))
				return nil
			})
			client.Start("rtmp://localhost/live/regression")
			input := func(data []byte) {
				t.Helper()
				if err := client.Input(data); err != nil {
					t.Fatal(err)
				}
			}
			c1 := output[0][1:]
			s0s1 := append(makeS0(), makeS1()...)
			input(s0s1[:28])
			input(s0s1[28:])
			s2 := makeS2(c1)
			control := newChunkStreamWriter(CHUNK_CHANNEL_USE_CTRL)
			setChunkSize := control.writeData(makeSetChunkSize(4096), SET_CHUNK_SIZE, 0, 0)
			input(s2[:tc.prefeed])
			input(append(s2[tc.prefeed:], setChunkSize[:tc.chunkBytes]...))
			input(setChunkSize[tc.chunkBytes:])
			if client.GetState() != STATE_RTMP_CONNECTING || client.reader.chunkSize != 4096 || client.lastMethod != CONNECT {
				t.Errorf("state=%d chunk size=%d last method=%d, want connecting, 4096, connect",
					client.GetState(), client.reader.chunkSize, client.lastMethod)
			}
			if len(output) != 3 {
				t.Fatalf("wrote %d buffers, want C0/C1, C2 and connect", len(output))
			}
			if want := uint32(len(s0s1) + len(s2) + len(setChunkSize)); client.bytesRead != want {
				t.Errorf("received-byte counter = %d, want %d", client.bytesRead, want)
			}
		})
	}
}
