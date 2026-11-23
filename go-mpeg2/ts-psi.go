package mpeg2

import "errors"

const maxPSISectionLength = 1021

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
	return readSection(section)
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
		if err := readSection(payload[:size]); err != nil {
			return err
		}
		payload = payload[size:]
	}
	return nil
}
