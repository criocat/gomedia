package mp4

import "errors"

// The Annex B conversion passes expect four-byte lengths. Keep that layout
// in place and only allocate a new sample when expanding shorter prefixes.
func normalizeMP4NALLengths(sample []byte, width, headerSize int) ([]byte, error) {
	if width == 0 {
		width = 4
	}
	if width < 1 || width > 4 {
		return nil, errors.New("invalid MP4 NAL length field width")
	}
	out := sample
	if width != 4 {
		out = make([]byte, 0, len(sample))
	}
	for len(sample) != 0 {
		if len(sample) < width {
			return nil, errors.New("incomplete MP4 NAL length field")
		}
		var size uint32
		for _, octet := range sample[:width] {
			size = size<<8 | uint32(octet)
		}
		sample = sample[width:]
		if size < uint32(headerSize) || uint64(size) > uint64(len(sample)) {
			return nil, errors.New("invalid MP4 NAL unit size")
		}
		if width != 4 {
			out = appendMP4NALPayload(out, sample[:size])
		}
		sample = sample[size:]
	}
	return out, nil
}

func mp4ConfigWithFourByteNALLengths(config []byte, fieldOffset int) []byte {
	if len(config) <= fieldOffset || config[fieldOffset]&3 == 3 {
		return config
	}
	config = append([]byte(nil), config...)
	config[fieldOffset] |= 3
	return config
}
