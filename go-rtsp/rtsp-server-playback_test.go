package rtsp

import (
	"encoding/binary"
	"fmt"
	"strings"
	"testing"
)

type serverPlaybackTestHandler struct {
	ServerHandle
	info []*RtpInfo
}

func (*serverPlaybackTestHandler) HandleSetup(*RtspServer, RtspRequest, *RtspResponse, *RtspTransport, *RtspTrack) {
}
func (*serverPlaybackTestHandler) HandleOption(*RtspServer, RtspRequest, *RtspResponse) {}
func (*serverPlaybackTestHandler) HandlePause(*RtspServer, RtspRequest, *RtspResponse)  {}

func (handler *serverPlaybackTestHandler) HandlePlay(_ *RtspServer, _ RtspRequest, _ *RtspResponse, _ *RangeTime, info []*RtpInfo) {
	handler.info = info
}

func TestRTSPServerAdvertisesNextPlaybackPacket(t *testing.T) {
	const root = "rtsp://localhost/live"
	const audioURL = root + "/track0"
	handler := &serverPlaybackTestHandler{}
	server := NewRtspServer(handler)
	track := NewAudioTrack(NewAudioCodec("PCMA", 8, 8000, 1))
	server.AddTrack(track)
	server.AddTrack(NewVideoTrack(NewVideoCodec("H264", 96, 90000)))

	var response []byte
	var rtpSequence uint16
	server.SetOutput(func(wire []byte) error {
		if wire[0] == '$' {
			if len(wire) < 8 {
				t.Fatalf("invalid interleaved RTP: %x", wire)
			}
			rtpSequence = binary.BigEndian.Uint16(wire[6:8])
		} else {
			response = append(response[:0], wire...)
		}
		return nil
	})
	send := func(method, url, headers string, cseq int) RtspResponse {
		t.Helper()
		request := fmt.Sprintf("%s %s RTSP/1.0\r\nCSeq: %d\r\n%s\r\n", method, url, cseq, headers)
		if err := server.Input([]byte(request)); err != nil {
			t.Fatal(err)
		}
		return parsePlaybackTestResponse(t, response)
	}

	setup := send(SETUP, audioURL, "Transport: RTP/AVP/TCP;unicast;interleaved=0-1\r\n", 1)
	session := "Session: " + setup.Fileds[Session] + "\r\n"
	checkPlay := func(cseq int, wantSeq uint16) {
		t.Helper()
		res := send(PLAY, root, session, cseq)
		if len(handler.info) != 1 || handler.info[0].Url != audioURL || handler.info[0].Seq != wantSeq {
			t.Errorf("PLAY callback got %+v, want the transport for %s at sequence %d", handler.info, audioURL, wantSeq)
		}
		value := res.Fileds[RTPInfo]
		var info RtpInfo
		info.Decode(value)
		if res.StatusCode != OK || info.Url != audioURL || info.Seq != wantSeq || info.Rtptime != -1 || strings.Contains(value, ",") {
			t.Errorf("PLAY response status=%d RTP-Info=%q, want %s at sequence %d", res.StatusCode, value, audioURL, wantSeq)
		}
	}
	checkPlay(2, track.initSequence)
	if err := track.WriteSample(RtspSample{Sample: []byte{0x55}, Timestamp: 9000}); err != nil {
		t.Fatal(err)
	}
	if rtpSequence != track.initSequence {
		t.Fatalf("first RTP sequence = %d, want %d", rtpSequence, track.initSequence)
	}
	send(PAUSE, root, session, 3)
	checkPlay(4, rtpSequence+1)
}

func TestRTSPServerOptionsWithoutSessionDuringPlayback(t *testing.T) {
	server := NewRtspServer(&serverPlaybackTestHandler{})
	server.AddTrack(NewAudioTrack(NewAudioCodec("PCMA", 8, 8000, 1)))
	var response []byte
	server.SetOutput(func(wire []byte) error {
		response = append(response[:0], wire...)
		return nil
	})
	setup := "SETUP rtsp://localhost/live/track0 RTSP/1.0\r\nCSeq: 1\r\nTransport: RTP/AVP/TCP;unicast;interleaved=0-1\r\n\r\n"
	if err := server.Input([]byte(setup)); err != nil {
		t.Fatal(err)
	}
	if res := parsePlaybackTestResponse(t, response); res.StatusCode != OK || res.Fileds[Session] == "" {
		t.Fatalf("SETUP response status=%d Session=%q", res.StatusCode, res.Fileds[Session])
	}
	if err := server.Input([]byte("OPTIONS * RTSP/1.0\r\nCSeq: 2\r\n\r\n")); err != nil {
		t.Fatal(err)
	}
	if res := parsePlaybackTestResponse(t, response); res.StatusCode != OK || res.Fileds[CSeq] != "2" || res.Fileds[Public] == "" {
		t.Errorf("OPTIONS response status=%d CSeq=%q Public=%q, want OK with allowed methods",
			res.StatusCode, res.Fileds[CSeq], res.Fileds[Public])
	}
}

func parsePlaybackTestResponse(t *testing.T, response []byte) RtspResponse {
	t.Helper()
	parsed := RtspResponse{Fileds: make(HeadFiled)}
	if _, err := parsed.parse(string(response)); err != nil {
		t.Fatal(err)
	}
	return parsed
}
