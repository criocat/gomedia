package rtsp

import (
	"fmt"
	"testing"
)

type announceTestServerHandler struct {
	ServerHandle
	announces int
}

func (handler *announceTestServerHandler) HandleAnnounce(*RtspServer, RtspRequest, map[string]*RtspTrack) {
	handler.announces++
}

func TestRTSPServerSetupMatchesCompleteTrackControl(t *testing.T) {
	server := NewRtspServer(&serverPlaybackTestHandler{})
	track := NewAudioTrack(NewAudioCodec("PCMA", 8, 8000, 1))
	server.AddTrack(track)
	var response []byte
	server.SetOutput(func(data []byte) error {
		response = append(response[:0], data...)
		return nil
	})

	for i, tc := range []struct {
		path   string
		status int
	}{
		{"/live/track01", BAD_REQUEST},
		{"/track0/live/video", BAD_REQUEST},
		{"/live/track0?token=value", OK},
	} {
		request := fmt.Sprintf("SETUP rtsp://localhost%s RTSP/1.0\r\nCSeq: %d\r\nTransport: RTP/AVP/TCP;unicast;interleaved=0-1\r\n\r\n", tc.path, i+1)
		if err := server.Input([]byte(request)); err != nil {
			t.Fatal(err)
		}
		res := parsePlaybackTestResponse(t, response)
		if res.StatusCode != tc.status {
			t.Fatalf("SETUP %s: status %d, want %d", tc.path, res.StatusCode, tc.status)
		}
		if res.StatusCode != OK && track.GetTransport() != nil {
			t.Fatalf("SETUP %s installed transport for the wrong track", tc.path)
		}
	}
	if track.GetTransport() == nil {
		t.Fatal("SETUP did not install the matching track's transport")
	}
}

func TestRTSPServerReplacesDescriptionOnNewAnnounce(t *testing.T) {
	handler := &announceTestServerHandler{}
	server := NewRtspServer(handler)
	const session = "v=0\r\no=- 1 1 IN IP4 127.0.0.1\r\ns=Test\r\nt=0 0\r\n"
	announce := func(cseq int, media string) {
		t.Helper()
		sdp := session + media
		request := fmt.Sprintf("ANNOUNCE rtsp://localhost/live RTSP/1.0\r\nCSeq: %d\r\nContent-Type: application/sdp\r\nContent-Length: %d\r\n\r\n%s", cseq, len(sdp), sdp)
		if err := server.Input([]byte(request)); err != nil {
			t.Fatal(err)
		}
	}

	announce(1, "m=audio 0 RTP/AVP 8\r\na=rtpmap:8 PCMA/8000/1\r\na=control:track0\r\n")
	if len(server.tracks) != 1 || server.tracks["audio"] == nil {
		t.Fatalf("initial announcement tracks=%v", server.tracks)
	}
	announce(2, "m=video 0 RTP/AVP 96\r\na=rtpmap:96 H265/90000\r\na=control:track1\r\n")
	video := server.tracks["video"]
	if len(server.tracks) != 1 || video == nil || video.Codec.Cid != RTSP_CODEC_H265 || len(server.sdpContext.Medias) != 1 {
		t.Errorf("new announcement tracks=%v SDP media count=%d, want one HEVC track",
			server.tracks, len(server.sdpContext.Medias))
	}
	if handler.announces != 2 {
		t.Errorf("got %d ANNOUNCE callbacks, want 2", handler.announces)
	}
}
