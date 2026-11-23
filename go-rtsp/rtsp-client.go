package rtsp

import (
    "bytes"
    "encoding/binary"
    "errors"
    "fmt"
    "net/url"
    "strconv"
    "strings"
    "sync/atomic"
    "time"

    "github.com/yapingcat/gomedia/go-rtsp/sdp"
)

// The client can assume the following states:

// Init:
//        SETUP has been sent, waiting for reply.

// Ready:
//        SETUP reply received or PAUSE reply received while in Playing
//        state.

// Playing:
//        PLAY reply received

// Recording:
//        RECORD reply received

// state       message sent     next state after response
// Init        SETUP            Ready
//             TEARDOWN         Init
// Ready       PLAY             Playing
//             RECORD           Recording
//             TEARDOWN         Init
//             SETUP            Ready
// Playing     PAUSE            Ready
//             TEARDOWN         Init
//             PLAY             Playing
//             SETUP            Playing (changed transport)
// Recording   PAUSE            Ready
//             TEARDOWN         Init
//             RECORD           Recording
//             SETUP            Recording (changed transport)

const (
    STATE_Init = iota
    STATE_Ready
    STATE_Playing
    STATE_Recording
)

type OutPutCallBack func([]byte) error
type TrackCallBack func(track *RtspTrack)
type RtspClient struct {
    uri              string
    usrName          string
    passwd           string
    isRecord         bool
    cseq             int32
    auth             authenticate
    output           OutPutCallBack
    lastRequest      *RtspRequest
    tracks           map[string]*RtspTrack
    cache            []byte
    reponseHandler   func(res *RtspResponse) error
    serverCapability []string
    state            int
    sdpContext       *sdp.Sdp
    setupStep        int
    handle           ClientHandle
    sessionId        string
    timeout          int
    scale            float32
    speed            float32
    timeRange        *RangeTime
}

type ClientOption func(cli *RtspClient)

func WithEnableRecord() ClientOption {
    return func(cli *RtspClient) {
        cli.isRecord = true
    }
}

func NewRtspClient(uri string, handle ClientHandle, opt ...ClientOption) (*RtspClient, error) {
    cli := &RtspClient{
        cseq:             1,
        state:            STATE_Init,
        serverCapability: []string{OPTIONS, DESCRIBE, SETUP, PLAY, TEARDOWN, ANNOUNCE, RECORD, PAUSE, SET_PARAMETER, GET_PARAMETER, REDIRECT},
        setupStep:        0,
        handle:           handle,
        sdpContext:       &sdp.Sdp{},
        tracks:           make(map[string]*RtspTrack),
    }
    for _, o := range opt {
        o(cli)
    }
    u, err := url.Parse(uri)
    if err != nil {
        return nil, err
    }
    if u.User != nil {
        cli.usrName = u.User.Username()
        if _, ok := u.User.Password(); ok {
            cli.passwd, _ = u.User.Password()
        }
    }
    u.User = nil
    cli.uri = u.String()
    cli.sdpContext.Attrs = make(map[string]string)
    cli.sdpContext.Attrs["control"] = "*"
    return cli, nil
}

func (client *RtspClient) AddTrack(track *RtspTrack) {
    track.uri = fmt.Sprintf("track%d", len(client.tracks))
    track.OpenTrack()
    client.tracks[track.TrackName] = track
    client.sdpContext.ParserSdp(track.mediaDescripe())
}

func (client *RtspClient) GetTrack(trackName string) (track *RtspTrack, found bool) {
    track, found = client.tracks[trackName]
    return
}

func (client *RtspClient) SetOutput(output OutPutCallBack) {
    client.output = output
}

func (client *RtspClient) SessionDescribe() string {
    return client.sdpContext.Encode()
}

func (client *RtspClient) SetSessionDescribe(sdp *sdp.Sdp) {
    client.sdpContext = sdp
}

func (client *RtspClient) Start() error {
    req := makeOptions(client.uri, client.cseq)
    client.reponseHandler = client.handleOption
    return client.sendRtspRequest(&req)
}

func (client *RtspClient) Input(data []byte) (err error) {
    buf := client.cache
    if len(buf) > 0 {
        buf = append(buf, data...)
    } else {
        buf = data
    }

    for len(buf) > 0 {
        ret := 0
        if buf[0] == '$' {
            ret, err = client.handlRtpOverRtsp(buf)
        } else {
            ret, err = client.handleRtspMessage(buf)
        }
        if err != nil {
            break
        }

        buf = buf[ret:]
    }

    if err != nil {
        if !errors.Is(err, errNeedMore) {
            return
        }
        err = nil
    }

    if len(buf) == 0 {
        client.cache = client.cache[:0]
        return
    }

    if cap(client.cache) >= len(buf) {
        client.cache = client.cache[:len(buf)]
    } else {
        client.cache = make([]byte, len(buf))
    }
    copy(client.cache, buf)
    return
}

func (client *RtspClient) TearDown() error {
    if client.sessionId == "" {
        return errors.New("cannot TEARDOWN without an RTSP session")
    }
    request := makeTeardown(client.sessionControlURL(), client.cseq)
    client.reponseHandler = client.handleTeardown
    return client.sendRtspRequest(&request)
}

func (client *RtspClient) Pause() error {
    if client.sessionId == "" {
        return errors.New("cannot PAUSE without an RTSP session")
    }
    request := makePause(client.sessionControlURL(), client.cseq)
    client.reponseHandler = client.handlePause
    return client.sendRtspRequest(&request)
}

func (client *RtspClient) Play() {
    if client.sessionId != "" && !client.isRecord {
        _ = client.sendPlay()
    }
}

func (client *RtspClient) sendPlay() error {
    request := makePlay(client.sessionControlURL(), client.cseq)
    if client.timeRange != nil {
        request.Fileds[Range] = client.timeRange.EncodeString()
    }
    if client.scale != 0 {
        request.Fileds[Scale] = strconv.FormatFloat(float64(client.scale), 'f', -1, 32)
    }
    if client.speed != 0 {
        request.Fileds[Speed] = strconv.FormatFloat(float64(client.speed), 'f', -1, 32)
    }
    client.reponseHandler = client.handlePlay
    return client.sendRtspRequest(&request)
}

func (client *RtspClient) SetSpeed(speed float32) {
    client.speed = speed
}

func (client *RtspClient) SetScale(scale float32) {
    client.scale = scale
}

func (client *RtspClient) SetRange(timeRange RangeTime) {
    client.timeRange = &timeRange
}

func (client *RtspClient) EnableRTCP() {

}

func (client *RtspClient) sessionControlURL() string {
    if !client.isRecord && client.sdpContext != nil {
        if client.sdpContext.ControlUrl != "" {
            return client.sdpContext.ControlUrl
        }
        if len(client.sdpContext.Medias) == 1 && client.sdpContext.Medias[0].ControlUrl != "" {
            return client.sdpContext.Medias[0].ControlUrl
        }
    }
    return client.uri
}

func (client *RtspClient) KeepAlive(method string) error {
    switch method {
    case OPTIONS:
        req := makeOptions(client.uri, client.cseq)
        req.Fileds[Session] = client.sessionId
        client.reponseHandler = client.handleOption
        return client.sendRtspRequest(&req)
    case GET_PARAMETER:
        req := makeGetParameter(client.uri, client.cseq)
        req.Fileds[Session] = client.sessionId
        client.reponseHandler = client.handleGetParameter
        return client.sendRtspRequest(&req)
    case SET_PARAMETER:
        req := makeSetParameter(client.uri, client.cseq)
        req.Fileds[Session] = client.sessionId
        client.reponseHandler = client.handleSetParameter
        return client.sendRtspRequest(&req)
    }
    return errors.New("unsupport keepalive method")
}

func (client *RtspClient) sendRtspRequest(req *RtspRequest) error {
    client.lastRequest = req
    atomic.AddInt32(&client.cseq, 1)
    if client.auth != nil {
        client.auth.setMethod(req.Method)
        client.auth.setUri(req.Uri)
        req.Fileds[Authorization] = client.auth.authenticateInfo()
    }
    if client.sessionId != "" {
        req.Fileds[Session] = client.sessionId
    }
    return client.sendToServer([]byte(req.Encode()))
}

func (client *RtspClient) sendToServer(data []byte) error {
    if client.output != nil {
        return client.output(data)
    }
    return nil
}

func (client *RtspClient) handlRtpOverRtsp(packet []byte) (ret int, err error) {
    if len(packet) < 4 {
        return 0, errNeedMore
    }
    channel := packet[1]
    length := binary.BigEndian.Uint16(packet[2:])
    if len(packet)-4 < int(length) {
        return 0, errNeedMore
    }

    for _, track := range client.tracks {
        isRtcp := false
        if track.transport.Interleaved[1] == int(channel) {
            isRtcp = true
        }

        if track.transport.Interleaved[0] == int(channel) || isRtcp {
            return 4 + int(length), track.Input(packet[4:4+length], isRtcp)
        }
    }
    //improve compatibility
    return 4 + int(length), nil
}

func (client *RtspClient) handleRtspMessage(msg []byte) (int, error) {

    idx := bytes.IndexFunc(msg, func(r rune) bool {
        if r == ' ' {
            return false
        } else {
            return true
        }
    })

    msg = msg[idx:]
    if bytes.HasPrefix(msg, []byte{'R', 'T', 'S', 'P'}) {
        return client.handleResponse(msg)
    } else {
        return client.handleRequest(msg)
    }
}

func (client *RtspClient) handleResponse(res []byte) (ret int, err error) {
    response := RtspResponse{Fileds: make(HeadFiled)}
    ret, err = response.parse(string(res))
    if err != nil {
        return
    }
    if response.StatusCode == 401 {
        return ret, client.handleUnAuth(response)
    }
    return ret, client.reponseHandler(&response)
}

func (client *RtspClient) handleRequest(req []byte) (ret int, err error) {
    request := RtspRequest{Fileds: make(HeadFiled)}
    ret, err = request.parse(string(req))
    if err != nil {
        return
    }

    switch request.Method {
    case REDIRECT:
        return ret, client.handleRedirect(&request)
    default:
        if client.handle != nil {
            return ret, client.handle.HandleRequest(client, request)
        } else {
            return ret, nil
        }
    }
}

func (client *RtspClient) handleUnAuth(response RtspResponse) error {

    if _, found := response.Fileds[WWWAuthenticate]; !found {
        return errors.New("need WWW-Authenticate")
    }

    if client.auth == nil {
        client.auth = createAuthByAuthenticate(response.Fileds[WWWAuthenticate])
        client.auth.setUserInfo(client.usrName, client.passwd)
    }
    client.auth.setMethod(client.lastRequest.Method)
    client.auth.setUri(client.lastRequest.Uri)
    client.auth.decode(response.Fileds[WWWAuthenticate])
    client.lastRequest.Fileds.Add(CSeq, client.cseq)
    client.lastRequest.Fileds[Date] = time.Now().UTC().Format("02 Jan 06 15:04:05 GMT")
    client.lastRequest.Fileds[Authorization] = client.auth.authenticateInfo()
    atomic.AddInt32(&client.cseq, 1)
    return client.sendToServer([]byte(client.lastRequest.Encode()))
}

func (client *RtspClient) handleOption(res *RtspResponse) error {
    if client.state == STATE_Init {
        if res.Fileds.Has(Public) {
            client.serverCapability = strings.Split(res.Fileds[Public], ",")
            for i := 0; i < len(client.serverCapability); i++ {
                client.serverCapability[i] = strings.TrimSpace(client.serverCapability[i])
            }
        }
    }

    if client.handle != nil {
        if err := client.handle.HandleOption(client, *res, client.serverCapability); err != nil {
            return err
        }
    }

    if client.state != STATE_Init {
        return nil
    }
    if client.isRecord {
        if !hasRecordAbility(client.serverCapability) {
            return fmt.Errorf("server capability:%s ,unsupport Record mode ", res.Fileds[Public])
        }
        announce := makeAnnounce(client.uri, client.cseq)
        announce.Body = client.SessionDescribe()
        client.reponseHandler = client.handleAnnounce
        return client.sendRtspRequest(&announce)
    } else {
        if !hasPlayAbility(client.serverCapability) {
            return fmt.Errorf("server capability:%s ,unsupport Play mode ", res.Fileds[Public])
        }
        describe := makeDescribe(client.uri, client.cseq)
        client.reponseHandler = client.handleDescribe
        return client.sendRtspRequest(&describe)
    }
}

// 1.The RTSP Content-Base field
// 2.The RTSP Content-Location field
// 3.The RTSP request URL
func (client *RtspClient) handleDescribe(res *RtspResponse) (err error) {

    if res.StatusCode != 200 {
        if client.handle != nil {
            return client.handle.HandleDescribe(client, *res, nil, nil)
        } else {
            return nil
        }
    }

    description := &sdp.Sdp{}
    if err := description.ParserSdp(res.Body); err != nil {
        return err
    }

    baseUrl := client.uri
    if res.Fileds.Has(ContentBase) {
        baseUrl = res.Fileds[ContentBase]
    } else if res.Fileds.Has(ContentLocation) {
        baseUrl = res.Fileds[ContentLocation]
    }

    if strings.HasSuffix(baseUrl, "/") {
        baseUrl = baseUrl[:len(baseUrl)-1]
    }

    getControlURL := func(control string) (string, error) {
        if control == "*" {
            return baseUrl, nil
        }
        if strings.HasPrefix(control, "rtsp://") {
            return control, nil
        }
        if !strings.HasPrefix(control, "/") {
            return baseUrl + "/" + control, nil
        }
        base, err := url.Parse(baseUrl)
        if err != nil {
            return "", err
        }
        ref, err := url.Parse(control)
        if err != nil {
            return "", err
        }
        return base.ResolveReference(ref).String(), nil
    }

    if description.ControlUrl == "" {
        if len(description.Medias) != 1 || description.Medias[0].ControlUrl == "" {
            return errors.New("missing RTSP control URL")
        }
    } else {
        description.ControlUrl, err = getControlURL(description.ControlUrl)
        if err != nil {
            return err
        }
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
        if track == nil {
            continue
        }
        track.OpenTrack()
        tracks[media.MediaType] = track
        media.ControlUrl, err = getControlURL(media.ControlUrl)
        if err != nil {
            return err
        }
    }

    client.sdpContext = description
    client.tracks = tracks
    client.setupStep = 0
    if client.handle != nil {
        if err := client.handle.HandleDescribe(client, *res, client.sdpContext, client.tracks); err != nil {
            return err
        }
    }
    interleaved := 0
    for i := client.setupStep; i < len(client.sdpContext.Medias); i++ {
        media := client.sdpContext.Medias[i]
        track, found := client.tracks[media.MediaType]
        if !found || !track.isOpen {
            continue
        }
        req := makeSetup(media.ControlUrl, client.cseq)
        if track.transport == nil {
            track.transport = NewRtspTransport(WithTcpInterleaved([2]int{interleaved, interleaved + 1}))
        }
        if track.transport.Proto == TCP && track.transport.Interleaved[0] == track.transport.Interleaved[1] {
            track.transport.Interleaved[0] = interleaved
            track.transport.Interleaved[1] = interleaved + 1
        }
        req.Fileds[Transport] = track.transport.EncodeString()
        client.setupStep = i + 1
        client.reponseHandler = client.handleSetup
        return client.sendRtspRequest(&req)
    }
    return nil
}

func (client *RtspClient) handleSetup(res *RtspResponse) error {

    lastMedia := client.sdpContext.Medias[client.setupStep-1]
    lastTrack := client.tracks[lastMedia.MediaType]
    if res.StatusCode != 200 {
        if client.handle == nil {
            return nil
        }
        proto := lastTrack.transport.Proto
        err := client.handle.HandleSetup(client, *res, lastTrack, client.tracks, "", -1)
        if res.StatusCode == 461 {
            if lastTrack.transport.Proto != proto {
                req := makeSetup(lastMedia.ControlUrl, client.cseq)
                if lastTrack.transport.Proto == TCP && lastTrack.transport.Interleaved[0] == lastTrack.transport.Interleaved[1] {
                    lastTrack.transport.Interleaved[0] = client.setupStep * 2
                    lastTrack.transport.Interleaved[1] = client.setupStep*2 + 1
                }
                req.Fileds[Transport] = lastTrack.transport.EncodeString()
                return client.sendRtspRequest(&req)
            }
        }
        return err
    }
    session := strings.Split(res.Fileds[Session], ";")
    sessionID := strings.TrimSpace(session[0])
    if sessionID == "" {
        return errors.New("missing Session in RTSP SETUP response")
    }
    if err := lastTrack.transport.DecodeString(res.Fileds[Transport]); err != nil {
        return err
    }
    if client.sessionId == "" {
        client.sessionId = sessionID
        client.timeout = 60
        for _, parameter := range session[1:] {
            parts := strings.SplitN(parameter, "=", 2)
            if len(parts) == 2 && strings.EqualFold(strings.TrimSpace(parts[0]), "timeout") {
                if timeout, err := strconv.Atoi(strings.TrimSpace(parts[1])); err == nil {
                    client.timeout = timeout
                }
            }
        }
    }
    client.state = STATE_Ready
    if client.handle != nil {
        if err := client.handle.HandleSetup(client, *res, lastTrack, client.tracks, client.sessionId, client.timeout); err != nil {
            return err
        }
    }

    if client.output != nil && lastTrack.transport.Proto == TCP {
        lastTrack.OnPacket(func(b []byte, isRtcp bool) error {
            interleavedPacket := make([]byte, 4+len(b))
            interleavedPacket[0] = '$'
            if isRtcp {
                interleavedPacket[1] = byte(lastTrack.transport.Interleaved[1])
            } else {
                interleavedPacket[1] = byte(lastTrack.transport.Interleaved[0])
            }

            binary.BigEndian.PutUint16(interleavedPacket[2:], uint16(len(b)))
            copy(interleavedPacket[4:], b)
            return client.output(interleavedPacket)
        })
    }

    for i := client.setupStep; i < len(client.sdpContext.Medias); i++ {
        media := client.sdpContext.Medias[i]
        track, found := client.tracks[media.MediaType]
        if !found || !track.isOpen {
            continue
        }
        req := makeSetup(media.ControlUrl, client.cseq)
        if track.transport == nil {
            track.transport = NewRtspTransport(WithTcpInterleaved([2]int{lastTrack.transport.Interleaved[0] + 2, lastTrack.transport.Interleaved[0] + 3}))
        }
        if client.isRecord {
            track.transport.mode = RECORD
        } else {
            track.transport.mode = PLAY
        }
        if track.transport.Proto == TCP && lastTrack.transport.Interleaved[0] == lastTrack.transport.Interleaved[1] {
            track.transport.Interleaved[0] = i * 2
            track.transport.Interleaved[1] = i*2 + 1
        }
        client.setupStep = i + 1
        req.Fileds[Transport] = track.transport.EncodeString()
        return client.sendRtspRequest(&req)
    }

    if client.isRecord {
        recordReq := makeRecord(client.uri, client.cseq)
        client.reponseHandler = client.handleRecord
        return client.sendRtspRequest(&recordReq)
    }
    return client.sendPlay()
}

func (client *RtspClient) handlePlay(res *RtspResponse) (err error) {

    if res.StatusCode != 200 {
        if client.handle != nil {
            return client.handle.HandlePlay(client, *res, nil, nil)
        } else {
            return nil
        }
    }
    client.state = STATE_Playing
    var tr *RangeTime = nil
    var info *RtpInfo = nil
    if res.Fileds.Has(Range) {
        if tr, err = parseRange(res.Fileds[Range]); err != nil {
            return err
        }
    }

    if res.Fileds.Has(RTPInfo) {
        info = &RtpInfo{}
        info.Decode(res.Fileds[RTPInfo])
    }

    if client.handle != nil {
        return client.handle.HandlePlay(client, *res, tr, info)
    }
    return nil
}

func (client *RtspClient) handleTeardown(res *RtspResponse) error {
    if res.StatusCode == OK {
        client.state = STATE_Init
        client.sessionId = ""
        client.timeout = 0
        client.setupStep = 0
    }
    if client.handle != nil {
        return client.handle.HandleTeardown(client, *res)
    }
    return nil
}

func (client *RtspClient) handlePause(res *RtspResponse) error {
    if res.StatusCode == OK {
        client.state = STATE_Ready
    }
    if client.handle != nil {
        return client.handle.HandlePause(client, *res)
    }
    return nil
}

func (client *RtspClient) handleAnnounce(res *RtspResponse) error {
    if res.StatusCode != 200 {
        if client.handle != nil {
            return client.handle.HandleAnnounce(client, *res)
        } else {
            return nil
        }
    }

    if client.handle != nil {
        client.handle.HandleAnnounce(client, *res)
    }

    for _, media := range client.sdpContext.Medias {
        if client.uri[len(client.uri)-1] == '/' {
            media.ControlUrl = client.uri + client.tracks[media.MediaType].uri
        } else {
            media.ControlUrl = client.uri + "/" + client.tracks[media.MediaType].uri
        }
    }
    if client.setupStep >= len(client.sdpContext.Medias) {
        return errors.New("need track")
    }
    track := client.tracks[client.sdpContext.Medias[client.setupStep].MediaType]
    req := makeSetup(client.sdpContext.Medias[client.setupStep].ControlUrl, client.cseq)
    if track.transport == nil {
        track.transport = NewRtspTransport(WithTcpInterleaved([2]int{client.setupStep * 2, client.setupStep*2 + 1}), WithMode(RECORD))
    }
    track.transport.mode = RECORD
    if track.transport.Proto == TCP && track.transport.Interleaved[0] == track.transport.Interleaved[1] {
        track.transport.Interleaved[0] = client.setupStep * 2
        track.transport.Interleaved[1] = client.setupStep*2 + 1
    }
    client.setupStep++
    req.Fileds[Transport] = track.transport.EncodeString()
    client.reponseHandler = client.handleSetup
    return client.sendRtspRequest(&req)
}

func (client *RtspClient) handleRecord(res *RtspResponse) error {
    if res.StatusCode != 200 {
        if client.handle != nil {
            return client.handle.HandleRecord(client, *res, nil, nil)
        } else {
            return nil
        }
    }
    client.state = STATE_Recording
    var tr *RangeTime = nil
    var info *RtpInfo = nil
    var err error
    if res.Fileds.Has(Range) {
        if tr, err = parseRange(res.Fileds[Range]); err != nil {
            return err
        }
    }

    if res.Fileds.Has(RTPInfo) {
        info = &RtpInfo{}
        info.Decode(res.Fileds[RTPInfo])
    }

    if client.handle != nil {
        return client.handle.HandleRecord(client, *res, tr, info)
    }
    return nil
}

func (client *RtspClient) handleGetParameter(res *RtspResponse) error {
    if res.StatusCode != 200 {
        if client.handle != nil {
            return client.handle.HandleGetParameter(client, *res)
        } else {
            return nil
        }
    }
    if client.handle != nil {
        return client.handle.HandleGetParameter(client, *res)
    }
    return nil
}

func (client *RtspClient) handleSetParameter(res *RtspResponse) error {
    if res.StatusCode != 200 {
        if client.handle != nil {
            return client.handle.HandleSetParameter(client, *res)
        } else {
            return nil
        }
    }
    if client.handle != nil {
        return client.handle.HandleSetParameter(client, *res)
    }
    return nil
}

func (client *RtspClient) handleRedirect(req *RtspRequest) error {
    if !req.Fileds.Has(Location) {
        return errors.New("redirect request has Location Filed")
    }

    location := req.Fileds[Location]
    var tr *RangeTime = nil
    if req.Fileds.Has(Range) {
        tr, _ = parseRange(req.Fileds[Range])
    }
    if client.handle != nil {
        return client.handle.HandleRedirect(client, *req, location, tr)
    }
    return nil
}
