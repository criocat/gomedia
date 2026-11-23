package rtsp

import (
	"testing"
	"time"
)

type responseTestServerHandler struct {
	ServerHandle
	responses []RtspResponse
}

func (handler *responseTestServerHandler) HandleResponse(_ *RtspServer, response RtspResponse) {
	handler.responses = append(handler.responses, response)
}

func TestRTSPServerDeliversCompleteResponses(t *testing.T) {
	handler := &responseTestServerHandler{}
	server := NewRtspServer(handler)
	part := "RTSP/1.0 200 OK\r\nCSeq: 11\r\nContent-Length: 4\r\n\r\npo"
	if err := server.Input([]byte(part)); err != nil {
		t.Fatal(err)
	}
	if len(handler.responses) != 0 {
		t.Fatal("delivered a partial response")
	}
	rest := "ngRTSP/1.0 200 OK\r\nCSeq: 12\r\n\r\n"
	if err := server.Input([]byte(rest)); err != nil {
		t.Fatal(err)
	}
	if len(handler.responses) != 2 {
		t.Fatalf("delivered %d responses, want 2", len(handler.responses))
	}
	first, second := handler.responses[0], handler.responses[1]
	if first.StatusCode != OK || first.Fileds[CSeq] != "11" || first.Body != "pong" {
		t.Errorf("unexpected first response %+v", first)
	}
	if second.StatusCode != OK || second.Fileds[CSeq] != "12" || second.Body != "" {
		t.Errorf("unexpected second response %+v", second)
	}
}

func TestRTSPServerFormatsDateHeader(t *testing.T) {
	server := NewRtspServer(optionsTestHandler{})
	var response []byte
	server.SetOutput(func(data []byte) error {
		response = append(response[:0], data...)
		return nil
	})
	before := time.Now().UTC().Truncate(time.Second)
	if err := server.Input([]byte("OPTIONS * RTSP/1.0\r\nCSeq: 1\r\n\r\n")); err != nil {
		t.Fatal(err)
	}
	date := parsePlaybackTestResponse(t, response).Fileds[Date]
	stamp, err := time.Parse("Mon, 02 Jan 2006 15:04:05 GMT", date)
	if err != nil {
		t.Fatalf("invalid Date header %q: %v", date, err)
	}
	after := time.Now().UTC().Truncate(time.Second)
	if stamp.Before(before) || stamp.After(after) {
		t.Errorf("Date %q is outside the request interval", date)
	}
}
