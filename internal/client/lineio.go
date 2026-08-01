package client

import (
	"bufio"
	"errors"
	"fmt"
	"strings"

	relay "github.com/endless-net/relay/protocol/v1"
)

const (
	MaxControlMapStreamEventBytes = 16 * 1024 * 1024
	MaxRelayControlLineBytes      = relay.MaxFramePayloadBytes*2 + 16*1024
)

var ErrLineTooLarge = errors.New("line exceeds configured limit")

func readBoundedLine(reader *bufio.Reader, maxBytes int, label string) ([]byte, error) {
	if maxBytes <= 0 {
		return nil, fmt.Errorf("%s limit must be positive", boundedLineLabel(label))
	}
	label = boundedLineLabel(label)
	var out []byte
	for {
		part, err := reader.ReadSlice('\n')
		if len(part) > 0 {
			if len(out)+len(part) > maxBytes {
				return nil, fmt.Errorf("%s exceeds %d bytes: %w", label, maxBytes, ErrLineTooLarge)
			}
			out = append(out, part...)
		}
		if err == nil {
			return out, nil
		}
		if errors.Is(err, bufio.ErrBufferFull) {
			continue
		}
		return out, err
	}
}

func boundedLineLabel(label string) string {
	label = strings.TrimSpace(label)
	if label == "" {
		return "line"
	}
	return label
}
