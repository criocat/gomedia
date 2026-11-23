package mpeg2

import (
	"errors"

	"github.com/yapingcat/gomedia/go-codec"
)

const maxPSISectionLength = 1021

type patAssembly struct {
	transportStreamID uint16
	version           uint8
	sections          [][]PmtPair
	received          int
}

func (assembly *patAssembly) add(pat *Pat) ([]PmtPair, bool) {
	if pat.Last_section_number == 0 {
		*assembly = patAssembly{}
		return pat.Pmts, true
	}

	total := int(pat.Last_section_number) + 1
	if len(assembly.sections) != total || assembly.version != pat.Version_number ||
		assembly.transportStreamID != pat.Transport_stream_id {
		*assembly = patAssembly{
			transportStreamID: pat.Transport_stream_id,
			version:           pat.Version_number,
			sections:          make([][]PmtPair, total),
		}
	}
	if assembly.sections[pat.Section_number] == nil {
		assembly.received++
	}
	pairs := make([]PmtPair, len(pat.Pmts))
	copy(pairs, pat.Pmts)
	assembly.sections[pat.Section_number] = pairs
	if assembly.received < total {
		return nil, false
	}

	var programs []PmtPair
	for _, section := range assembly.sections {
		programs = append(programs, section...)
	}
	*assembly = patAssembly{}
	return programs, true
}

type psiSections struct {
	pending     []byte
	continuity  uint8
	hasSequence bool
}

func psiSectionSize(data []byte) (int, error) {
	if len(data) < 3 {
		return 0, nil
	}
	length := int(data[1]&0x0f)<<8 | int(data[2])
	if length > maxPSISectionLength {
		return 0, errors.New("MPEG-TS PSI section exceeds maximum length")
	}
	return 3 + length, nil
}

func readCheckedPSISection(section []byte, readSection func([]byte) error) error {
	// A complete PAT or PMT has a zero CRC remainder. Retain the active
	// tables when a damaged section arrives.
	if len(section) < 4 || codec.CalcCrc32(0xffffffff, section) != 0 {
		return nil
	}
	return readSection(section)
}

func (sections *psiSections) complete(payload []byte, readSection func([]byte) error) error {
	if len(sections.pending) == 0 {
		return nil
	}
	sections.pending = append(sections.pending, payload...)
	size, err := psiSectionSize(sections.pending)
	if err != nil {
		sections.pending = nil
		return err
	}
	if size == 0 || len(sections.pending) < size {
		return nil
	}
	section := sections.pending[:size]
	sections.pending = nil
	return readCheckedPSISection(section, readSection)
}

func (sections *psiSections) input(packet *TSPacket, payload []byte, readSection func([]byte) error) error {
	if packet.Field != nil && packet.Field.Discontinuity_indicator != 0 {
		*sections = psiSections{}
	}
	if packet.Adaptation_field_control&1 == 0 || len(payload) == 0 {
		return nil
	}
	if sections.hasSequence {
		if packet.Continuity_counter == sections.continuity {
			return nil // Duplicate TS packet.
		}
		if packet.Continuity_counter != (sections.continuity+1)&0x0f {
			sections.pending = nil
		}
	}
	sections.continuity = packet.Continuity_counter
	sections.hasSequence = true

	if packet.Payload_unit_start_indicator == 0 {
		return sections.complete(payload, readSection)
	}
	pointer := int(payload[0])
	if pointer > len(payload)-1 {
		return errors.New("MPEG-TS PSI pointer exceeds payload")
	}
	if err := sections.complete(payload[1:1+pointer], readSection); err != nil {
		return err
	}
	sections.pending = nil
	payload = payload[1+pointer:]
	for len(payload) > 0 && payload[0] != 0xff {
		size, err := psiSectionSize(payload)
		if err != nil {
			return err
		}
		if size == 0 || size > len(payload) {
			sections.pending = append(sections.pending, payload...)
			break
		}
		if err := readCheckedPSISection(payload[:size], readSection); err != nil {
			return err
		}
		payload = payload[size:]
	}
	return nil
}
