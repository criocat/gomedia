# HEVC/AAC interoperability fixture

`hevc-aac.flv` is generated locally from FFmpeg synthetic video and audio
sources. It contains 10 64×64 HEVC frames (including B-frames), AAC audio,
an Enhanced FLV `hvc1` sequence header and colorInfo metadata.

Generated with FFmpeg 8.0.1:

```sh
ffmpeg -hide_banner -loglevel error \
  -f lavfi -i testsrc2=size=64x64:rate=10 \
  -f lavfi -i sine=frequency=440:sample_rate=44100 -t 1 \
  -c:v libx265 -preset ultrafast \
  -x265-params pools=1:frame-threads=1:log-level=error:keyint=5:min-keyint=5:scenecut=0:bframes=2 \
  -c:a aac -f flv hevc-aac.flv
```
