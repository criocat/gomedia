package rtsp

import (
	"fmt"
	"strings"
	"testing"

	"github.com/yapingcat/gomedia/go-rtsp/sdp"
)

type optionsTestHandler struct{ ServerHandle }

func (optionsTestHandler) HandleOption(*RtspServer, RtspRequest, *RtspResponse) {}

func TestOptionsEchoesCSeqRegardlessOfCase(t *testing.T) {
	for _, name := range []string{"CSeq", "cseq", "CSEQ"} {
		t.Run(name, func(t *testing.T) {
			server := NewRtspServer(optionsTestHandler{})
			var response string
			server.SetOutput(func(data []byte) error {
				response = string(data)
				return nil
			})
			request := fmt.Sprintf("OPTIONS rtsp://localhost/live RTSP/1.0\r\n%s: 17\r\n\r\n", name)
			if err := server.Input([]byte(request)); err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(response, "\r\nCSeq: 17\r\n") {
				t.Errorf("response did not echo CSeq 17: %q", response)
			}
		})
	}
}

type controlTestClient struct {
	ClientHandle
	request RtspRequest
}

func (*controlTestClient) HandleOption(*RtspClient, RtspResponse, []string) error {
	return nil
}

func (*controlTestClient) HandleDescribe(*RtspClient, RtspResponse, *sdp.Sdp, map[string]*RtspTrack) error {
	return nil
}

func (*controlTestClient) HandleSetup(*RtspClient, RtspResponse, *RtspTrack, map[string]*RtspTrack, string, int) error {
	return nil
}

func (*controlTestClient) HandlePlay(*RtspClient, RtspResponse, *RangeTime, *RtpInfo) error {
	return nil
}

func (h *controlTestClient) HandleRequest(_ *RtspClient, request RtspRequest) error {
	h.request = request
	return nil
}

func startControlTestClient(t *testing.T, handler ClientHandle, description string) (*RtspClient, *[]string) {
	t.Helper()
	client, err := NewRtspClient("rtsp://localhost/source", handler)
	if err != nil {
		t.Fatal(err)
	}
	var sent []string
	client.SetOutput(func(data []byte) error {
		sent = append(sent, string(data))
		return nil
	})
	if err := client.Start(); err != nil {
		t.Fatal(err)
	}
	options := "RTSP/1.0 200 OK\r\nCSeq: 1\r\nPublic: OPTIONS, DESCRIBE, SETUP, PLAY, TEARDOWN, GET_PARAMETER\r\n\r\n"
	feedControlMessage(t, client, options)
	describe := fmt.Sprintf("RTSP/1.0 200 OK\r\nCSeq: 2\r\nContent-Type: application/sdp\r\n"+
		"Content-Base: rtsp://localhost/source/\r\nContent-Length: %d\r\n\r\n%s", len(description), description)
	feedControlMessage(t, client, describe)
	if len(sent) != 3 || !strings.HasPrefix(sent[2], "SETUP ") {
		t.Fatalf("expected OPTIONS, DESCRIBE and SETUP, got %q", sent)
	}
	return client, &sent
}

const controlTestSDP = "v=0\r\no=- 0 0 IN IP4 127.0.0.1\r\ns=example\r\n" +
	"c=IN IP4 127.0.0.1\r\nt=0 0\r\na=control:*\r\n" +
	"m=audio 0 RTP/AVP 0\r\na=rtpmap:0 PCMU/8000\r\n"

func feedControlMessage(t *testing.T, client *RtspClient, message string) {
	t.Helper()
	if err := client.Input([]byte(message)); err != nil {
		t.Fatal(err)
	}
}

func TestClientDeliversServerKeepAlive(t *testing.T) {
	handler := &controlTestClient{}
	client, sent := startControlTestClient(t, handler, controlTestSDP+"a=control:audio\r\n")
	feedControlMessage(t, client, "RTSP/1.0 200 OK\r\nCSeq: 3\r\nSession: 12345678\r\n"+
		"Transport: RTP/AVP/TCP;unicast;interleaved=0-1\r\n\r\n")
	if len(*sent) != 4 || !strings.HasPrefix((*sent)[3], "PLAY ") {
		t.Fatalf("expected PLAY after successful SETUP, got %q", *sent)
	}
	feedControlMessage(t, client, "RTSP/1.0 200 OK\r\nCSeq: 4\r\nSession: 12345678\r\n\r\n")
	feedControlMessage(t, client, "GET_PARAMETER rtsp://localhost/source RTSP/1.0\r\ncseq: 33\r\nSession: 12345678\r\n\r\n")
	if got := handler.request; got.Method != GET_PARAMETER || got.Fileds[CSeq] != "33" || got.Fileds[Session] != "12345678" {
		t.Errorf("unexpected callback request: %+v", got)
	}
}

func TestClientResolvesSDPControlPath(t *testing.T) {
	tests := []struct {
		name    string
		control string
		uri     string
	}{
		{"relative", "audio", "rtsp://localhost/source/audio"},
		{"absolute_path", "/alternate/audio", "rtsp://localhost/alternate/audio"},
		{"absolute_uri", "rtsp://localhost/alternate/audio", "rtsp://localhost/alternate/audio"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, sent := startControlTestClient(t, &controlTestClient{}, controlTestSDP+"a=control:"+test.control+"\r\n")
			got := strings.SplitN((*sent)[2], "\r\n", 2)[0]
			if want := "SETUP " + test.uri + " RTSP/1.0"; got != want {
				t.Errorf("got %q, want %q", got, want)
			}
		})
	}
}

type udpFallbackTestClient struct{ controlTestClient }

func (*udpFallbackTestClient) HandleSetup(_ *RtspClient, res RtspResponse, track *RtspTrack, _ map[string]*RtspTrack, _ string, _ int) error {
	if res.StatusCode == Unsupported_Transport {
		track.SetTransport(NewRtspTransport(WithEnableUdp(), WithClientUdpPort(5000, 5001)))
	}
	return nil
}

func TestClientRetriesSetupForSameTrack(t *testing.T) {
	for _, test := range []struct {
		name       string
		extraMedia string
	}{
		{"single_track", ""},
		{"two_tracks", "m=video 0 RTP/AVP 96\r\na=rtpmap:96 H264/90000\r\na=control:video\r\n"},
	} {
		t.Run(test.name, func(t *testing.T) {
			client, sent := startControlTestClient(t, &udpFallbackTestClient{}, controlTestSDP+"a=control:audio\r\n"+test.extraMedia)
			feedControlMessage(t, client, "RTSP/1.0 461 Unsupported Transport\r\nCSeq: 3\r\n\r\n")
			if len(*sent) != 4 {
				t.Fatalf("expected SETUP retry, got %q", *sent)
			}
			retry := (*sent)[3]
			if !strings.HasPrefix(retry, "SETUP rtsp://localhost/source/audio RTSP/1.0\r\n") ||
				!strings.Contains(retry, "\r\nTransport: RTP/AVP/UDP;") {
				t.Fatalf("expected UDP SETUP for audio, got %q", retry)
			}
			feedControlMessage(t, client, "RTSP/1.0 200 OK\r\nCSeq: 4\r\nSession: 12345678\r\n"+
				"Transport: RTP/AVP/UDP;unicast;client_port=5000-5001;server_port=6000-6001\r\n\r\n")
			want := "PLAY rtsp://localhost/source RTSP/1.0\r\n"
			if test.extraMedia != "" {
				want = "SETUP rtsp://localhost/source/video RTSP/1.0\r\n"
			}
			if len(*sent) != 5 || !strings.HasPrefix((*sent)[4], want) {
				t.Errorf("expected %q after successful retry, got %q", want, *sent)
			}
		})
	}
}

type rtpInfoTestClient struct {
	ClientHandle
	info *RtpInfo
}

func (handler *rtpInfoTestClient) HandlePlay(_ *RtspClient, _ RtspResponse, _ *RangeTime, info *RtpInfo) error {
	handler.info = info
	return nil
}

func TestPlayRTPInfoHeader(t *testing.T) {
	const value = "url=rtsp://localhost/live/trackID=1;seq=100;rtptime=9000"
	for _, name := range []string{"RTP-Info", "rtp-info", "RTPInfo"} {
		t.Run(name, func(t *testing.T) {
			handler := &rtpInfoTestClient{}
			client := &RtspClient{handle: handler}
			client.reponseHandler = client.handlePlay
			feedControlMessage(t, client, "RTSP/1.0 200 OK\r\nCSeq: 4\r\n"+name+": "+value+"\r\n\r\n")
			want := RtpInfo{Url: "rtsp://localhost/live/trackID=1", Seq: 100, Rtptime: 9000}
			if handler.info == nil || *handler.info != want {
				t.Errorf("PLAY callback RTP-Info = %+v, want %+v", handler.info, want)
			}
		})
	}
	response := RtspResponse{Version: RTSP_1_0, StatusCode: 200, Fileds: HeadFiled{RTPInfo: value}}
	if wire := response.Encode(); !strings.Contains(wire, "\r\nRTP-Info: "+value+"\r\n") {
		t.Errorf("encoded PLAY response = %q", wire)
	}
}

func TestRTSPPlaysOneTrackWithoutAggregateControl(t *testing.T) {
	description := strings.Replace(controlTestSDP, "a=control:*\r\n", "", 1) + "a=control:audio\r\n"
	handler := &controlTestClient{}
	client, sent := startControlTestClient(t, handler, description)
	const trackURL = "rtsp://localhost/source/audio"
	if !strings.HasPrefix((*sent)[2], "SETUP "+trackURL+" RTSP/1.0\r\n") {
		t.Fatalf("SETUP did not use %q, got %q", trackURL, (*sent)[2])
	}
	feedControlMessage(t, client, "RTSP/1.0 200 OK\r\nCSeq: 3\r\nSession: 12345678\r\n"+
		"Transport: RTP/AVP/TCP;unicast;interleaved=0-1\r\n\r\n")
	if len(*sent) != 4 || !strings.HasPrefix((*sent)[3], "PLAY "+trackURL+" RTSP/1.0\r\n") {
		t.Fatalf("PLAY did not use %q, got %q", trackURL, *sent)
	}
	feedControlMessage(t, client, "RTSP/1.0 200 OK\r\nCSeq: 4\r\nSession: 12345678\r\n\r\n")
	if client.state != STATE_Playing {
		t.Errorf("RTSP client state = %d, want Playing", client.state)
	}
}

const controlTestSetupOK = "RTSP/1.0 200 OK\r\nCSeq: 3\r\nSession: 12345678;timeout=45\r\n" +
	"Transport: RTP/AVP/TCP;unicast;interleaved=4-5\r\n\r\n"

func TestRTSPAppliesSetupResponseWithoutHandler(t *testing.T) {
	client, sent := startControlTestClient(t, nil, controlTestSDP+"a=control:audio\r\n")
	feedControlMessage(t, client, controlTestSetupOK)
	if len(*sent) != 4 || !strings.Contains((*sent)[3], "\r\nSession: 12345678\r\n") || client.timeout != 45 {
		t.Fatalf("PLAY requests = %q, timeout = %d", *sent, client.timeout)
	}
	track, found := client.GetTrack("audio")
	if !found || track.GetTransport().Interleaved != [2]int{4, 5} {
		t.Fatalf("client did not negotiate the 4-5 interleaved channels")
	}
}

type controlTestVideoOnly struct{ controlTestClient }

func (*controlTestVideoOnly) HandleDescribe(_ *RtspClient, _ RtspResponse, _ *sdp.Sdp, tracks map[string]*RtspTrack) error {
	delete(tracks, "audio")
	return nil
}

func TestRTSPSkipsTracksRemovedByDescribeHandler(t *testing.T) {
	description := controlTestSDP + "a=control:audio\r\nm=video 0 RTP/AVP 96\r\n" +
		"a=rtpmap:96 H264/90000\r\na=control:video\r\n"
	client, sent := startControlTestClient(t, &controlTestVideoOnly{}, description)
	if !strings.HasPrefix((*sent)[2], "SETUP rtsp://localhost/source/video RTSP/1.0\r\n") {
		t.Fatalf("SETUP did not select the retained video track: %q", (*sent)[2])
	}
	feedControlMessage(t, client, controlTestSetupOK)
	if len(*sent) != 4 || !strings.HasPrefix((*sent)[3], "PLAY ") {
		t.Errorf("requests after successful SETUP = %q, want PLAY next", *sent)
	}
}

type controlTestLifecycle struct {
	controlTestClient
	paused, closed int
}

func (handler *controlTestLifecycle) HandlePause(_ *RtspClient, response RtspResponse) error {
	handler.paused = response.StatusCode
	return nil
}

func (handler *controlTestLifecycle) HandleTeardown(_ *RtspClient, response RtspResponse) error {
	handler.closed = response.StatusCode
	return nil
}

func TestRTSPPauseAndTeardownLifecycle(t *testing.T) {
	for _, test := range []struct {
		name, sdp, url string
	}{
		{"aggregate", controlTestSDP, "rtsp://localhost/source"},
		{"single_track", strings.Replace(controlTestSDP, "a=control:*\r\n", "", 1), "rtsp://localhost/source/audio"},
	} {
		t.Run(test.name, func(t *testing.T) {
			handler := &controlTestLifecycle{}
			client, sent := startControlTestClient(t, handler, test.sdp+"a=control:audio\r\n")
			feedControlMessage(t, client, controlTestSetupOK)
			feedControlMessage(t, client, "RTSP/1.0 200 OK\r\nCSeq: 4\r\nSession: 12345678\r\n\r\n")
			checkRequest := func(method string, count int) {
				t.Helper()
				if len(*sent) != count {
					t.Fatalf("got %d RTSP requests, want %d after %s", len(*sent), count, method)
				}
				wire := (*sent)[count-1]
				if !strings.HasPrefix(wire, method+" "+test.url+" RTSP/1.0\r\n") ||
					!strings.Contains(wire, "\r\nSession: 12345678\r\n") {
					t.Errorf("unexpected %s request: %q", method, wire)
				}
			}
			if err := client.Pause(); err != nil {
				t.Fatal(err)
			}
			checkRequest(PAUSE, 5)
			feedControlMessage(t, client, "RTSP/1.0 200 OK\r\nCSeq: 5\r\nSession: 12345678\r\n\r\n")
			if client.state != STATE_Ready || handler.paused != 200 {
				t.Errorf("state = %d, pause status = %d, want Ready after OK", client.state, handler.paused)
			}
			if err := client.TearDown(); err != nil {
				t.Fatal(err)
			}
			checkRequest(TEARDOWN, 6)
			feedControlMessage(t, client, "RTSP/1.0 200 OK\r\nCSeq: 6\r\nSession: 12345678\r\n\r\n")
			if client.state != STATE_Init || client.sessionId != "" || handler.closed != 200 {
				t.Errorf("state = %d, session = %q, teardown status = %d", client.state, client.sessionId, handler.closed)
			}
		})
	}
}

func TestRTSPPlaySendsConfiguredHeadersAndResumes(t *testing.T) {
	client, sent := startControlTestClient(t, &controlTestLifecycle{}, controlTestSDP+"a=control:audio\r\n")
	client.SetRange(RangeTime{rangeType: RANGE_NPT, begin: 1250, end: 3500})
	client.SetScale(2)
	client.SetSpeed(1.5)
	checkPlay := func(count int) {
		t.Helper()
		if len(*sent) != count || !strings.HasPrefix((*sent)[count-1], "PLAY rtsp://localhost/source RTSP/1.0\r\n") {
			t.Fatalf("requests = %q, want PLAY at position %d", *sent, count)
		}
		for _, header := range []string{"Session: 12345678", "Range: npt=1.250-3.500", "Scale: 2", "Speed: 1.5"} {
			if !strings.Contains((*sent)[count-1], "\r\n"+header+"\r\n") {
				t.Errorf("PLAY is missing %q", header)
			}
		}
	}

	feedControlMessage(t, client, controlTestSetupOK)
	checkPlay(4)
	feedControlMessage(t, client, "RTSP/1.0 200 OK\r\nCSeq: 4\r\nSession: 12345678\r\n\r\n")
	if err := client.Pause(); err != nil {
		t.Fatal(err)
	}
	feedControlMessage(t, client, "RTSP/1.0 200 OK\r\nCSeq: 5\r\nSession: 12345678\r\n\r\n")
	client.Play()
	checkPlay(6)
	feedControlMessage(t, client, "RTSP/1.0 200 OK\r\nCSeq: 6\r\nSession: 12345678\r\n\r\n")
	if client.state != STATE_Playing {
		t.Errorf("resumed client state = %d, want Playing", client.state)
	}
}

func TestRTSPReplacesSDPAfterTeardown(t *testing.T) {
	handler := &controlTestLifecycle{}
	client, sent := startControlTestClient(t, handler, controlTestSDP+"a=control:old-audio\r\n")
	feedControlMessage(t, client, controlTestSetupOK)
	feedControlMessage(t, client, "RTSP/1.0 200 OK\r\nCSeq: 4\r\nSession: 12345678\r\n\r\n")
	if err := client.TearDown(); err != nil {
		t.Fatal(err)
	}
	feedControlMessage(t, client, "RTSP/1.0 200 OK\r\nCSeq: 5\r\nSession: 12345678\r\n\r\n")

	if err := client.Start(); err != nil {
		t.Fatal(err)
	}
	feedControlMessage(t, client, "RTSP/1.0 200 OK\r\nCSeq: 6\r\nPublic: OPTIONS, DESCRIBE, SETUP, PLAY, TEARDOWN\r\n\r\n")
	description := controlTestSDP + "a=control:new-audio\r\n"
	feedControlMessage(t, client, fmt.Sprintf("RTSP/1.0 200 OK\r\nCSeq: 7\r\nContent-Base: rtsp://localhost/source/\r\nContent-Length: %d\r\n\r\n%s", len(description), description))
	if len(*sent) != 8 || !strings.HasPrefix((*sent)[7], "SETUP rtsp://localhost/source/new-audio RTSP/1.0\r\n") {
		t.Errorf("expected SETUP for new-audio, got %q", *sent)
	}
	if len(client.sdpContext.Medias) != 1 || len(client.tracks) != 1 {
		t.Errorf("new description contains %d media sections and %d tracks, want 1 each", len(client.sdpContext.Medias), len(client.tracks))
	}
}
