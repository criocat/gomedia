package codec

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"reflect"
	"testing"
)

func TestOpusReadsAmbisonicsAndDiscreteMapping(t *testing.T) {
	for _, tc := range []struct {
		family  byte
		streams byte
		coupled byte
		mapping []byte
		want    []ChannelMap
	}{
		{
			family: 2, streams: 5, coupled: 1, mapping: []byte{2, 3, 4, 5, 0, 1},
			want: []ChannelMap{
				{StreamIdx: 1}, {StreamIdx: 2}, {StreamIdx: 3}, {StreamIdx: 4},
				{ChannelIdx: LEFT_CHANNEL}, {ChannelIdx: RIGHT_CHANNEL},
			},
		},
		{
			family: 255, streams: 2, coupled: 1, mapping: []byte{2, 255, 1, 2},
			want: []ChannelMap{
				{StreamIdx: 1}, {Silence: true}, {ChannelIdx: RIGHT_CHANNEL},
				{StreamIdx: 1, Copy: true, CopyFrom: 0},
			},
		},
	} {
		t.Run(fmt.Sprint(tc.family), func(t *testing.T) {
			head := opusMappingTestHeader(byte(len(tc.mapping)), tc.family, tc.streams, tc.coupled, tc.mapping)
			var ctx OpusContext
			if err := ctx.ParseExtranData(head); err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(ctx.Channel, tc.mapping) || !reflect.DeepEqual(ctx.ChannelMaps, tc.want) {
				t.Errorf("mapping=%v channels=%+v, want %v and %+v", ctx.Channel, ctx.ChannelMaps, tc.mapping, tc.want)
			}
		})
	}
}

func TestOpusHeadPreservesOutputGainAndChannelMapping(t *testing.T) {
	for _, head := range [][]byte{
		opusMappingTestHeader(2, 0, 0, 0, nil),
		opusMappingTestHeader(3, 1, 2, 1, []byte{0, 2, 1}),
	} {
		t.Run(fmt.Sprint(head[18]), func(t *testing.T) {
			var ctx OpusContext
			if err := ctx.ParseExtranData(head); err != nil {
				t.Fatal(err)
			}
			if got := ctx.WriteOpusExtraData(); !bytes.Equal(got, head) {
				t.Errorf("round trip = % x, want % x", got, head)
			}
		})
	}
}

func opusMappingTestHeader(channels, family, streams, coupled byte, mapping []byte) []byte {
	head := make([]byte, 19)
	copy(head, "OpusHead")
	head[8] = 1
	head[9] = channels
	binary.LittleEndian.PutUint16(head[10:], 312)
	binary.LittleEndian.PutUint32(head[12:], 48000)
	binary.LittleEndian.PutUint16(head[16:], 0xf980)
	head[18] = family
	if family != 0 {
		head = append(head, streams, coupled)
		head = append(head, mapping...)
	}
	return head
}
