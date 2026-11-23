package rtsp

import (
    "bytes"
    "encoding/binary"
    "errors"
    "fmt"
    "math/rand"
    "net/url"
    "strings"
    "time"

    "github.com/yapingcat/gomedia/go-rtsp/sdp"
)

type RtspServer struct {
    buffer      bytes.Buffer
    tracks      map[string]*RtspTrack
    userName    string
    passwd      string
    realm       string
    auth        authenticate
    output      OutPutCallBack
    handle      ServerHandle
    sessionId   string
    sdpContext  *sdp.Sdp
    interleaved int
    isRecord    bool
}

type ServerOption func(*RtspServer)

func WithUserInfo(userName, passwd string) ServerOption {
    return func(rs *RtspServer) {
        rs.userName = userName
        rs.passwd = passwd
    }
}

func WithAuthType(authType string) ServerOption {
    return func(rs *RtspServer) {
        rs.auth = createAuthByAuthenticate(authType)
    }
}

func WithRealm(realm string) ServerOption {
    return func(rs *RtspServer) {
        rs.realm = realm
    }
}

func NewRtspServer(handle ServerHandle, opt ...ServerOption) *RtspServer {
    server := &RtspServer{
        handle:     handle,
        auth:       nil,
        realm:      "gomedia server",
        tracks:     make(map[string]*RtspTrack),
        sdpContext: &sdp.Sdp{},
        isRecord:   false,
    }
    for _, o := range opt {
        o(server)
    }
    if server.auth == nil && server.userName != "" && server.passwd != "" {
        server.auth = createAuthByAuthenticate("Digest")
        server.auth.setUserInfo(server.userName, server.passwd)
        server.auth.setRealm(server.realm)
    }
    server.sdpContext.Attrs = make(map[string]string)
    server.sdpContext.Attrs["control"] = "*"
    return server
}

func (server *RtspServer) AddTrack(track *RtspTrack) {
    track.uri = fmt.Sprintf("track%d", len(server.tracks))
    server.tracks[track.TrackName] = track
    server.sdpContext.ParserSdp(track.mediaDescripe())
}

func (server *RtspServer) SetOutput(output OutPutCallBack) {
    server.output = output
}

func (server *RtspServer) Input(data []byte) (err error) {
    var buf []byte
    if server.buffer.Len() > 0 {
        server.buffer.Write(data)
        buf = server.buffer.Bytes()
    } else {
        buf = data
    }

    for len(buf) > 0 {
        ret := 0
        if buf[0] == '$' {
            ret, err = readInterleavedPacket(buf, server.tracks)
        } else {
            ret, err = server.handleRtspMessage(buf)
        }
        if err != nil {
            break
        }
        buf = buf[ret:]
    }

    if err != nil {
        if errors.Is(err, errNeedMore) {
            err = nil
        } else {
            return
        }
    }

    if len(buf) == 0 {
        server.buffer.Reset()
    } else {
        if server.buffer.Len() > 0 {
            server.buffer.Reset()
        }
        server.buffer.Write(buf)
    }
    return nil
}

func (server *RtspServer) handleRtspMessage(msg []byte) (int, error) {
    idx := bytes.IndexFunc(msg, func(r rune) bool {
        if r == ' ' {
            return false
        } else {
            return true
        }
    })

    msg = msg[idx:]
    if bytes.HasPrefix(msg, []byte{'R', 'T', 'S', 'P'}) {
        return server.handleResponse(msg)
    } else {
        return server.handleRequest(msg)
    }
}

func (server *RtspServer) handleResponse(data []byte) (int, error) {
    response := RtspResponse{Fileds: make(HeadFiled)}
    consumed, err := response.parse(string(data))
    if err != nil {
        return consumed, err
    }
    if server.handle != nil {
        server.handle.HandleResponse(server, response)
    }
    return consumed, nil
}

func (server *RtspServer) handleRequest(req []byte) (ret int, err error) {
    request := RtspRequest{}
    request.Fileds = make(HeadFiled)
    ret, err = request.parse(string(req))
    if err != nil {
        return
    }
    if server.userName != "" && server.passwd != "" {
        server.auth.setMethod(request.Method)
        if !request.Fileds.Has(Authorization) || !server.auth.check(request.Fileds[Authorization]) {
            return ret, server.handleUnAuth(request)
        }
    }

    res := RtspResponse{}
    res.Fileds = make(HeadFiled)
    res.StatusCode = 200
    res.Version = RTSP_1_0
    if server.sessionId != "" && (request.Method != OPTIONS || request.Fileds.Has(Session)) {
        if !request.Fileds.Has(Session) || request.Fileds[Session] != server.sessionId {
            res.StatusCode = Session_Not_Found
            return ret, server.sendRespones(request, res)
        }
    }
    switch request.Method {
    case OPTIONS:
        methods := []string{OPTIONS, SET_PARAMETER, GET_PARAMETER, SETUP, DESCRIBE, PLAY, ANNOUNCE, RECORD, TEARDOWN, PAUSE}
        public := ""
        for _, m := range methods {
            public += m + ","
        }
        public = public[:len(public)-1]
        server.handle.HandleOption(server, request, &res)
        if res.StatusCode == 200 {
            res.Fileds[Public] = public
        }
    case DESCRIBE:
        server.handle.HandleDescribe(server, request, &res)
        if res.StatusCode == OK {
            res.Body = server.sdpContext.Encode()
            res.Fileds[ContentType] = "application/sdp"
        }
    case SETUP:
        foundTrack := false
        for _, track := range server.tracks {
            implicitControl := track.uri == "" && len(server.tracks) == 1
            if !implicitControl && !matchesTrackControl(request.Uri, track.uri) {
                continue
            }
            foundTrack = true
            transport := NewRtspTransport()
            transport.DecodeString(request.Fileds[Transport])
            server.handle.HandleSetup(server, request, &res, transport, track)
            if res.StatusCode == 200 {
                track.uri = request.Uri
                if server.sessionId == "" {
                    number := []byte("0123456789")
                    b := make([]byte, 10)
                    for i := range b {
                        b[i] = number[rand.Intn(len(number))]
                    }
                    server.sessionId = string(b)
                }
                if transport.Proto == TCP {
                    transport.Interleaved[0] = server.interleaved
                    transport.Interleaved[1] = server.interleaved + 1
                    server.interleaved = server.interleaved + 2
                    track.OnPacket(func(b []byte, isRtcp bool) error {
                        interleavedPacket := make([]byte, 4+len(b))
                        interleavedPacket[0] = '$'
                        if isRtcp {
                            interleavedPacket[1] = byte(transport.Interleaved[1])
                        } else {
                            interleavedPacket[1] = byte(transport.Interleaved[0])
                        }
                        binary.BigEndian.PutUint16(interleavedPacket[2:], uint16(len(b)))
                        copy(interleavedPacket[4:], b)
                        return server.output(interleavedPacket)
                    })
                }
                res.Fileds[Transport] = transport.EncodeString()
                res.Fileds[Session] = server.sessionId
                track.SetTransport(transport)
            }
            break
        }
        if !foundTrack {
            res.StatusCode = BAD_REQUEST
        }
    case ANNOUNCE:
        description := &sdp.Sdp{Attrs: map[string]string{"control": "*"}}
        if err = description.ParserSdp(request.Body); err != nil {
            return
        }
        tracks := make(map[string]*RtspTrack, len(description.Medias))
        for _, media := range description.Medias {
            fmtpHandle := sdp.CreateFmtpParamParser(media.EncodeName)
            if fmtpHandle != nil {
                fmtpHandle.Load(media.Attrs["fmtp"])
            }
            var track *RtspTrack = nil
            if media.MediaType == "audio" {
                track = NewAudioTrack(NewAudioCodec(media.EncodeName, uint8(media.PayloadType), uint32(media.ClockRate), media.ChannelCount), WithCodecParamHandler(fmtpHandle))
            } else if media.MediaType == "video" {
                track = NewVideoTrack(NewVideoCodec(media.EncodeName, uint8(media.PayloadType), uint32(media.ClockRate)), WithCodecParamHandler(fmtpHandle))
            } else {
                track = NewMetaTrack(NewApplicatioCodec(media.EncodeName, uint8(media.PayloadType)))
            }
            track.uri = media.ControlUrl
            tracks[media.MediaType] = track
        }
        server.isRecord = true
        server.sdpContext = description
        server.tracks = tracks
        server.handle.HandleAnnounce(server, request, tracks)
    case PLAY:
        var tr *RangeTime = nil
        var info []*RtpInfo
        if request.Fileds.Has(Range) {
            tr, _ = parseRange(request.Fileds[Range])
        }
        for _, t := range server.tracks {
            if t.transport != nil {
                info = append(info, t.playbackRtpInfo())
            }
        }
        server.handle.HandlePlay(server, request, &res, tr, info)
        if res.StatusCode == 200 {
            if tr != nil {
                res.Fileds[Range] = tr.EncodeString()
            }
            if len(info) > 0 && !res.Fileds.Has(RTPInfo) {
                values := make([]string, len(info))
                for i, track := range info {
                    values[i] = track.EncodeString()
                }
                res.Fileds[RTPInfo] = strings.Join(values, ",")
            }
        }
    case RECORD:
        var tr *RangeTime = nil
        var info []*RtpInfo
        if request.Fileds.Has(Range) {
            tr, _ = parseRange(request.Fileds[Range])
        }
        for _, t := range server.tracks {
            i := &RtpInfo{}
            i.Url = t.uri
            i.Seq = t.initSequence
        }
        server.handle.HandleRecord(server, request, &res, tr, info)
        if res.StatusCode == 200 {
            if tr != nil {
                res.Fileds[Range] = tr.EncodeString()
            }
            if len(info) > 0 {
                infostr := ""
                for _, i := range info {
                    infostr += i.EncodeString()
                    infostr += ","
                }
                res.Fileds[RTPInfo] = infostr[:len(infostr)-1]
            }
        }
    case TEARDOWN:
        server.handle.HandleTeardown(server, request, &res)
    case PAUSE:
        server.handle.HandlePause(server, request, &res)
    case SET_PARAMETER:
        server.handle.HandleSetParameter(server, request, &res)
    case GET_PARAMETER:
        server.handle.HandleGetParameter(server, request, &res)
    }
    return ret, server.sendRespones(request, res)
}

func matchesTrackControl(requestURI, controlURI string) bool {
    target, err := url.Parse(requestURI)
    if err != nil {
        return false
    }
    control, err := url.Parse(controlURI)
    if err != nil || (control.RawQuery != "" && control.RawQuery != target.RawQuery) {
        return false
    }
    if control.Scheme != "" && !strings.EqualFold(control.Scheme, target.Scheme) {
        return false
    }
    if control.Host != "" && !strings.EqualFold(control.Host, target.Host) {
        return false
    }
    targetPath, controlPath := target.EscapedPath(), control.EscapedPath()
    if control.IsAbs() || control.Host != "" || strings.HasPrefix(controlPath, "/") {
        return targetPath == controlPath
    }
    if controlPath == "" {
        return control.RawQuery != ""
    }
    return targetPath == controlPath || strings.HasSuffix(targetPath, "/"+controlPath)
}

func (server *RtspServer) handleUnAuth(request RtspRequest) error {
    response := RtspResponse{Fileds: make(HeadFiled)}
    response.StatusCode = 401
    response.Fileds[WWWAuthenticate] = server.auth.wwwAuthenticate()
    return server.sendRespones(request, response)
}

func (server *RtspServer) sendRespones(req RtspRequest, res RtspResponse) error {
    res.Fileds[CSeq] = req.Fileds[CSeq]
    res.Fileds[Date] = time.Now().UTC().Format("Mon, 02 Jan 2006 15:04:05 GMT")
    if server.output != nil {
        return server.output([]byte(res.Encode()))
    }
    return nil
}
