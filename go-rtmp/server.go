package rtmp

import (
    "encoding/binary"
    "errors"

    "github.com/yapingcat/gomedia/go-codec"
    "github.com/yapingcat/gomedia/go-flv"
)

//example
//1. rtmp 推流服务端
//
//listen, _ := net.Listen("tcp4", "0.0.0.0:1935")
//conn, _ := listen.Accept()
//
// handle := NewRtmpServerHandle()
// handle.OnPublish(func(app, streamName string) StatusCode {
//     return NETSTREAM_PUBLISH_START
// })
//
// handle.SetOutput(func(b []byte) error {
//     _, err := conn.Write(b)
//     return err
// })

// handle.OnFrame(func(cid codec.CodecID, pts, dts uint32, frame []byte) {
//     if cid == codec.CODECID_VIDEO_H264 {
//        //do something
//     }
//     ........
// })

//
// 把从网络中接收到的数据，input到rtmp句柄当中
// buf := make([]byte, 60000)
// for {
//     n, err := conn.Read(buf)
//     if err != nil {
//         fmt.Println(err)
//         break
//     }
//     err = handle.Input(buf[0:n])
//     if err != nil {
//         fmt.Println(err)
//         break
//     }
// }

// rtmp播放服务端
// listen, _ := net.Listen("tcp4", "0.0.0.0:1935")
// conn, _ := listen.Accept()

// ready := make(chan struct{})
// handle := NewRtmpServerHandle()
// handle.onPlay = func(app, streamName string, start, duration float64, reset bool) StatusCode {
//        return NETSTREAM_PLAY_START
//  }
//
// handle.OnStateChange(func(newstate RtmpState) {
//    if newstate == STATE_RTMP_PLAY_START {
//        close(ready) //关闭这个通道，通知推流协程可以向客户端推流了
//    }
//  })
//
//  handle.SetOutput(func(b []byte) error {
//       _, err := conn.Write(b)
//      return err
//  })
//
//  go func() {
//
//      等待推流
//      <-ready
//
//      开始推流
//      handle.WriteVideo(cid, frame, pts, dts)
//      handle.WriteAudio(cid, frame, pts, dts)
//
//  }()
//
//  把从网络中接收到的数据，input到rtmp句柄当中
//  buf := make([]byte, 60000)
//  for {
//      n, err := conn.Read(buf)
//      if err != nil {
//          fmt.Println(err)
//          break
//      }
//      err = handle.Input(buf[0:n])
//      if err != nil {
//          fmt.Println(err)
//          break
//      }
//  }
//  conn.Close()

type RtmpServerHandle struct {
    app            string
    streamName     string
    tcUrl          string
    state          RtmpParserState
    streamState    RtmpState
    cmdChan        *chunkStreamWriter
    userCtrlChan   *chunkStreamWriter
    audioChan      *chunkStreamWriter
    videoChan      *chunkStreamWriter
    reader         *chunkStreamReader
    writeChunkSize uint32
    hs             *serverHandShake
    wndAckSize     uint32
    recvAckWindow  uint32
    bytesRead      uint32
    lastAckBytes   uint32
    videoDemuxer   flv.VideoTagDemuxer
    videoCodec     flv.FLV_VIDEO_CODEC_ID
    audioDemuxer   flv.AudioTagDemuxer
    audioCodec     flv.FLV_SOUND_FORMAT
    videoMuxer     flv.AVTagMuxer
    audioMuxer     flv.AVTagMuxer
    videoWriteCodec codec.CodecID
    audioWriteCodec codec.CodecID
    onframe        OnFrame
    output         OutputCB
    onRelease      OnReleaseStream
    onChangeState  OnStateChange
    onPlay         OnPlay
    onPublish      OnPublish
    timestamp      uint32
    streamId       uint32
}

func NewRtmpServerHandle(options ...func(*RtmpServerHandle)) *RtmpServerHandle {
    server := &RtmpServerHandle{
        hs:             newServerHandShake(),
        cmdChan:        newChunkStreamWriter(CHUNK_CHANNEL_CMD),
        userCtrlChan:   newChunkStreamWriter(CHUNK_CHANNEL_USE_CTRL),
        reader:         newChunkStreamReader(FIX_CHUNK_SIZE),
        wndAckSize:     DEFAULT_ACK_SIZE,
        writeChunkSize: DEFAULT_CHUNK_SIZE,
        streamId:       1,
    }

    for _, o := range options {
        o(server)
    }

    return server
}

func (server *RtmpServerHandle) SetOutput(output OutputCB) {
    server.output = output
    server.hs.output = output
}

func (server *RtmpServerHandle) OnFrame(onframe OnFrame) {
    server.onframe = onframe
}

func (server *RtmpServerHandle) OnPlay(onPlay OnPlay) {
    server.onPlay = onPlay
}

func (server *RtmpServerHandle) OnPublish(onPub OnPublish) {
    server.onPublish = onPub
}

func (server *RtmpServerHandle) OnRelease(onRelease OnReleaseStream) {
    server.onRelease = onRelease
}

//状态变更，回调函数，
//服务端在STATE_RTMP_PLAY_START状态下，开始发流
//客户端在STATE_RTMP_PUBLISH_START状态，开始推流
func (server *RtmpServerHandle) OnStateChange(stateChange OnStateChange) {
    server.onChangeState = stateChange
}

func (server *RtmpServerHandle) GetStreamName() string {
    return server.streamName
}

func (server *RtmpServerHandle) GetApp() string {
    return server.app
}

func (server *RtmpServerHandle) GetState() RtmpState {
    return server.streamState
}

func (server *RtmpServerHandle) Input(data []byte) error {
    server.bytesRead += uint32(len(data))
    for len(data) > 0 {
        switch server.state {
        case HandShake:
            server.changeState(STATE_HANDSHAKEING)
            r := server.hs.input(data)
            if server.hs.getState() == HANDSHAKE_DONE {
                server.changeState(STATE_HANDSHAKE_DONE)
                server.state = ReadChunk
            }
            data = data[r:]
        case ReadChunk:

            err := server.reader.readRtmpMessage(data, server.handleMessage)
            if err != nil {
                return err
            }
            return sendAcknowledgement(server.userCtrlChan, server.output, server.recvAckWindow, server.bytesRead, &server.lastAckBytes)
        }
    }
    return nil
}

func (server *RtmpServerHandle) WriteFrame(cid codec.CodecID, frame []byte, pts, dts uint32) error {
    if cid == codec.CODECID_AUDIO_AAC || cid == codec.CODECID_AUDIO_G711A || cid == codec.CODECID_AUDIO_G711U {
        return server.WriteAudio(cid, frame, pts, dts)
    } else if cid == codec.CODECID_VIDEO_H264 || cid == codec.CODECID_VIDEO_H265 {
        return server.WriteVideo(cid, frame, pts, dts)
    } else {
        return errors.New("unsupport codec id")
    }
}

func (server *RtmpServerHandle) WriteAudio(cid codec.CodecID, frame []byte, pts, dts uint32) error {
    if server.audioChan != nil && server.audioWriteCodec != cid {
        return errors.New("outgoing audio codec changed; reconnect required")
    }
    if server.audioMuxer == nil {
        if cid != codec.CODECID_AUDIO_AAC && cid != codec.CODECID_AUDIO_G711A && cid != codec.CODECID_AUDIO_G711U {
            return errors.New("unsupported outgoing audio codec")
        }
        server.audioMuxer = flv.CreateAudioMuxer(flv.CovertCodecId2SoundFromat(cid))
    }
    if server.audioChan == nil {
        server.audioWriteCodec = cid
        server.audioChan = newChunkStreamWriter(CHUNK_CHANNEL_AUDIO)
        server.audioChan.chunkSize = server.writeChunkSize
    }
    return writeAudioMessages(server.audioMuxer, server.audioChan, server.streamId, frame, pts, dts, server.output)
}

func (server *RtmpServerHandle) WriteVideo(cid codec.CodecID, frame []byte, pts, dts uint32) error {
    if cid != codec.CODECID_VIDEO_H264 && cid != codec.CODECID_VIDEO_H265 {
        return errors.New("unsupported outgoing video codec")
    }
    if server.videoMuxer != nil && server.videoWriteCodec != cid {
        return errors.New("outgoing video codec changed; reconnect required")
    }
    if server.videoMuxer == nil {
        server.videoWriteCodec = cid
        server.videoMuxer = flv.CreateVideoMuxer(flv.CovertCodecId2FlvVideoCodecId(cid))
    }
    if server.videoChan == nil {
        server.videoChan = newChunkStreamWriter(CHUNK_CHANNEL_VIDEO)
        server.videoChan.chunkSize = server.writeChunkSize
    }
    tags := server.videoMuxer.Write(frame, pts, dts)
    for _, tag := range tags {
        pkt := server.videoChan.writeData(tag, VIDEO, server.streamId, dts)
        if len(pkt) > 0 {
            if err := server.output(pkt); err != nil {
                return err
            }
        }
    }
    return nil
}

func (server *RtmpServerHandle) changeState(newState RtmpState) {
    if server.streamState != newState {
        server.streamState = newState
        if server.onChangeState != nil {
            server.onChangeState(newState)
        }
    }
}

func (server *RtmpServerHandle) handleMessage(msg *rtmpMessage) error {
    server.timestamp = msg.timestamp
    switch msg.msgtype {
    case SET_CHUNK_SIZE:
        if len(msg.msg) < 4 {
            return errors.New("bytes of \"set chunk size\"  < 4")
        }
        size := binary.BigEndian.Uint32(msg.msg)
        server.reader.chunkSize = size
    case ABORT_MESSAGE:
        return server.reader.abortMessage(msg.msg)
    case ACKNOWLEDGEMENT:
        if len(msg.msg) < 4 {
            return errors.New("bytes of \"acknowledgement\"  < 4")
        }
    case USER_CONTROL:
        //TODO
    case WND_ACK_SIZE:
        if len(msg.msg) < 4 {
            return errors.New("bytes of \"window acknowledgement size\"  < 4")
        }
        server.recvAckWindow = binary.BigEndian.Uint32(msg.msg)
    case SET_PEER_BW:
        //TODO
    case AUDIO:
        return server.handleAudioMessage(msg)
    case VIDEO:
        return server.handleVideoMessage(msg)
    case Command_AMF0:
        return server.handleCommand(msg.msg)
    case Command_AMF3:
    case Metadata_AMF0:
    case Metadata_AMF3:
    case SharedObject_AMF0:
    case SharedObject_AMF3:
    case Aggregate:
        return dispatchAggregate(msg, server.handleMessage)
    default:
        return errors.New("unkown message type")
    }
    return nil
}

func (server *RtmpServerHandle) handleCommand(data []byte) error {
    cmd, args, err := decodeCommand(data)
    if err != nil {
        return err
    }
    switch cmd {
    case "connect", "releaseStream", "createStream", "play", "publish":
        if len(args) == 0 || args[0].amfType != AMF0_NUMBER {
            return errors.New("invalid AMF0 transaction ID")
        }
    }
    switch cmd {
    case "connect":
        return server.handleConnect(args)
    case "releaseStream":
        return server.handleReleaseStream(args)
    case "createStream":
        return server.handleCreateStream(args)
    case "play":
        return server.handlePlay(args)
    case "publish":
        return server.handlePublish(args)
    }
    return nil
}

func (server *RtmpServerHandle) handleConnect(args []amf0Item) error {
    if len(args) < 2 {
        return errors.New("missing AMF0 connect object")
    }
    obj, ok := args[1].value.(amfObject)
    if !ok {
        return errors.New("invalid AMF0 connect object")
    }
    app, tcUrl := server.app, server.tcUrl
    for _, item := range obj.items {
        switch item.name {
        case "app", "tcUrl":
            value, ok := item.value.value.([]byte)
            if !ok {
                return errors.New("invalid AMF0 connect property: " + item.name)
            }
            if item.name == "app" {
                app = string(value)
            } else {
                tcUrl = string(value)
            }
        }
    }
    server.app, server.tcUrl = app, tcUrl
    server.changeState(STATE_RTMP_CONNECTING)

    buf := makeSetChunkSize(server.writeChunkSize)
    bufs := server.userCtrlChan.writeData(buf, SET_CHUNK_SIZE, 0, 0)
    server.userCtrlChan.chunkSize = server.writeChunkSize
    server.cmdChan.chunkSize = server.writeChunkSize
    buf = makeAcknowledgementSize(server.wndAckSize)
    bufs = append(bufs, server.userCtrlChan.writeData(buf, WND_ACK_SIZE, 0, 0)...)
    buf = makeSetPeerBandwidth(server.wndAckSize, LimitType_DYNAMIC)
    bufs = append(bufs, server.userCtrlChan.writeData(buf, SET_PEER_BW, 0, 0)...)
    bufs = append(bufs, server.cmdChan.writeData(makeConnectRes(), Command_AMF0, 0, 0)...)
    return server.output(bufs)
}

func (server *RtmpServerHandle) handleReleaseStream(args []amf0Item) error {
    if len(args) < 3 {
        return errors.New("missing AMF0 releaseStream name")
    }
    name, ok := args[2].value.([]byte)
    if !ok {
        return errors.New("invalid AMF0 releaseStream name")
    }
    if server.onRelease != nil {
        server.onRelease(server.app, string(name))
    }
    return nil
}

func (server *RtmpServerHandle) handleCreateStream(args []amf0Item) error {
    tid := uint32(args[0].value.(float64))
    bufs := server.cmdChan.writeData(makeCreateStreamRes(tid, server.streamId), Command_AMF0, 0, 0)
    return server.output(bufs)
}

func (server *RtmpServerHandle) handlePlay(args []amf0Item) error {
    if len(args) < 3 {
        return errors.New("missing AMF0 play stream name")
    }
    name, ok := args[2].value.([]byte)
    if !ok {
        return errors.New("invalid AMF0 play stream name")
    }
    tid := int(args[0].value.(float64))
    start := float64(-2)
    duration := float64(-1)
    reset := false

    if len(args) > 3 {
        start, ok = args[3].value.(float64)
        if !ok {
            return errors.New("invalid AMF0 play start")
        }
    }
    if len(args) > 4 {
        duration, ok = args[4].value.(float64)
        if !ok {
            return errors.New("invalid AMF0 play duration")
        }
    }
    if len(args) > 5 {
        reset, ok = args[5].value.(bool)
        if !ok {
            return errors.New("invalid AMF0 play reset")
        }
    }
    streamName := string(name)
    server.streamName = streamName

    code := NETSTREAM_PLAY_START
    if server.onPlay != nil {
        code = server.onPlay(server.app, streamName, start, duration, reset)
    }
    if code == NETSTREAM_PLAY_START {
        res := makeUserControlMessage(StreamBegin, int(server.streamId))
        bufs := server.userCtrlChan.writeData(res, USER_CONTROL, 0, 0)
        res = makeStatusRes(tid, NETSTREAM_PLAY_RESET, NETSTREAM_PLAY_RESET.Level(), string(NETSTREAM_PLAY_RESET.Description()))
        bufs = append(bufs, server.cmdChan.writeData(res, Command_AMF0, server.streamId, 0)...)
        res = makeStatusRes(tid, NETSTREAM_PLAY_START, NETSTREAM_PLAY_START.Level(), string(NETSTREAM_PLAY_START.Description()))
        bufs = append(bufs, server.cmdChan.writeData(res, Command_AMF0, server.streamId, 0)...)
        if err := server.output(bufs); err != nil {
            return err
        }
        server.changeState(STATE_RTMP_PLAY_START)
    } else {
        res := makeStatusRes(tid, code, code.Level(), string(code.Description()))
        if err := server.output(server.cmdChan.writeData(res, Command_AMF0, server.streamId, 0)); err != nil {
            return err
        }
        server.changeState(STATE_RTMP_PLAY_FAILED)
    }
    return nil
}

func (server *RtmpServerHandle) handlePublish(args []amf0Item) error {
    if len(args) < 3 {
        return errors.New("missing AMF0 publish stream name")
    }
    name, ok := args[2].value.([]byte)
    if !ok {
        return errors.New("invalid AMF0 publish stream name")
    }
    tid := int(args[0].value.(float64))
    streamName := string(name)
    server.streamName = streamName
    code := NETSTREAM_PUBLISH_START
    if server.onPublish != nil {
        code = server.onPublish(server.app, streamName)
    }
    res := makeStatusRes(tid, code, code.Level(), string(code.Description()))
    if err := server.output(server.cmdChan.writeData(res, Command_AMF0, server.streamId, 0)); err != nil {
        return err
    }
    if code == NETSTREAM_PUBLISH_START {
        server.changeState(STATE_RTMP_PUBLISH_START)
    } else {
        server.changeState(STATE_RTMP_PUBLISH_FAILED)
    }
    return nil
}

func (server *RtmpServerHandle) handleVideoMessage(msg *rtmpMessage) error {
    cid := flv.GetFLVVideoCodecId(msg.msg)
    if len(msg.msg) < 5 || (cid != flv.FLV_AVC && cid != flv.FLV_HEVC) {
        return errors.New("invalid or unsupported RTMP video header")
    }
    if server.videoDemuxer == nil {
        server.videoCodec = cid
        server.videoDemuxer = flv.CreateFlvVideoTagHandle(cid)
        server.videoDemuxer.OnFrame(func(codecid codec.CodecID, frame []byte, cts int) {
            dts := server.timestamp
            pts := dts + uint32(cts)
            server.onframe(codecid, pts, dts, frame)
        })
    } else if server.videoCodec != cid {
        return errors.New("video codec changed; reconnect required")
    }
    return server.videoDemuxer.Decode(msg.msg)
}

func (server *RtmpServerHandle) handleAudioMessage(msg *rtmpMessage) error {
    format, err := audioFormat(msg.msg)
    if err != nil {
        return err
    }
    if server.audioDemuxer != nil {
        if server.audioCodec != format {
            return errors.New("audio codec changed; reconnect required")
        }
        return server.audioDemuxer.Decode(msg.msg)
    }
    demuxer := flv.CreateAudioTagDemuxer(format)
    demuxer.OnFrame(func(codecid codec.CodecID, frame []byte) {
        dts := server.timestamp
        pts := dts
        server.onframe(codecid, pts, dts, frame)
    })
    if err := demuxer.Decode(msg.msg); err != nil {
        return err
    }
    server.audioDemuxer, server.audioCodec = demuxer, format
    return nil
}
