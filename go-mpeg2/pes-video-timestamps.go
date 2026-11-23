package mpeg2

type pesVideoTimestamp struct {
	offset   int // Start of the PES payload in the buffered video data.
	pts, dts uint64
}

type pesVideoTimestamps []pesVideoTimestamp

func (timestamps *pesVideoTimestamps) applyThrough(start int, pts, dts *uint64) {
	consumed := 0
	for _, timestamp := range *timestamps {
		if timestamp.offset > start {
			break
		}
		*pts, *dts = timestamp.pts, timestamp.dts
		consumed++
	}
	if consumed != 0 {
		remaining := copy(*timestamps, (*timestamps)[consumed:])
		*timestamps = (*timestamps)[:remaining]
	}
}

func (timestamps pesVideoTimestamps) removePrefix(size int) {
	for i := range timestamps {
		timestamps[i].offset -= size
	}
}
