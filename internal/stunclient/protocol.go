// Package stunclient implements the client-side STUN Binding wire contract used
// by EndlessNet netcheck and diagnostics. It intentionally contains no server,
// listener, rate limiting, deployment, or observability behavior.
package stunclient

import (
	"context"
	"crypto/rand"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"
	"time"
)

const (
	headerLength         = 20
	transactionIDLength  = 12
	maxMessageSize       = 1500
	bindingRequestType   = 0x0001
	bindingSuccessType   = 0x0101
	xorMappedAddressType = 0x0020
	magicCookie          = 0x2112A442
)

type TransactionID [transactionIDLength]byte

// TransactionIDFromMessage returns the transaction ID carried by a STUN
// message. It performs only the framing checks needed by a shared UDP socket
// dispatcher; ParseBindingResponse remains responsible for validating the
// complete Binding response.
func TransactionIDFromMessage(message []byte) (TransactionID, bool) {
	var txID TransactionID
	if len(message) < headerLength || binary.BigEndian.Uint32(message[4:8]) != magicCookie {
		return txID, false
	}
	length := int(binary.BigEndian.Uint16(message[2:4]))
	if length%4 != 0 || headerLength+length != len(message) {
		return txID, false
	}
	copy(txID[:], message[8:20])
	return txID, true
}

func BuildBindingRequest() ([]byte, TransactionID, error) {
	var txID TransactionID
	if _, err := io.ReadFull(rand.Reader, txID[:]); err != nil {
		return nil, txID, err
	}
	request := make([]byte, headerLength)
	binary.BigEndian.PutUint16(request[0:2], bindingRequestType)
	binary.BigEndian.PutUint32(request[4:8], magicCookie)
	copy(request[8:20], txID[:])
	return request, txID, nil
}

func ParseBindingResponse(response []byte, expected TransactionID) (*net.UDPAddr, error) {
	if len(response) > maxMessageSize {
		return nil, errors.New("STUN response exceeds maximum message size")
	}
	if len(response) < headerLength {
		return nil, errors.New("STUN response is shorter than header")
	}
	if got := binary.BigEndian.Uint16(response[0:2]); got != bindingSuccessType {
		return nil, fmt.Errorf("STUN message type = 0x%04x, want Binding success", got)
	}
	length := int(binary.BigEndian.Uint16(response[2:4]))
	if length%4 != 0 || headerLength+length != len(response) {
		return nil, errors.New("STUN response length does not match packet")
	}
	if binary.BigEndian.Uint32(response[4:8]) != magicCookie {
		return nil, errors.New("STUN response magic cookie mismatch")
	}
	if TransactionID(response[8:20]) != expected {
		return nil, errors.New("STUN response transaction ID mismatch")
	}
	attributes := response[headerLength:]
	var mapped *net.UDPAddr
	for len(attributes) > 0 {
		if len(attributes) < 4 {
			return nil, errors.New("STUN attribute header is truncated")
		}
		attributeType := binary.BigEndian.Uint16(attributes[0:2])
		attributeLength := int(binary.BigEndian.Uint16(attributes[2:4]))
		next := 4 + paddedLength(attributeLength)
		if next > len(attributes) || 4+attributeLength > len(attributes) {
			return nil, errors.New("STUN attribute length exceeds packet")
		}
		if attributeType == xorMappedAddressType {
			value, err := parseXORMappedAddress(attributes[4:4+attributeLength], expected)
			if err != nil {
				return nil, err
			}
			if mapped == nil {
				mapped = value
			}
		} else if attributeType < 0x8000 {
			return nil, fmt.Errorf("unsupported required STUN response attribute 0x%04x", attributeType)
		}
		attributes = attributes[next:]
	}
	if mapped != nil {
		return mapped, nil
	}
	return nil, errors.New("STUN response missing XOR-MAPPED-ADDRESS")
}

func Query(ctx context.Context, serverAddr string) (*net.UDPAddr, error) {
	dialer := net.Dialer{}
	conn, err := dialer.DialContext(ctx, "udp", serverAddr)
	if err != nil {
		return nil, err
	}
	defer func() { _ = conn.Close() }()
	deadline := time.Now().Add(2 * time.Second)
	if contextDeadline, ok := ctx.Deadline(); ok {
		deadline = contextDeadline
	}
	if err := conn.SetDeadline(deadline); err != nil {
		return nil, err
	}
	request, txID, err := BuildBindingRequest()
	if err != nil {
		return nil, err
	}
	if _, err := conn.Write(request); err != nil {
		return nil, err
	}
	response := make([]byte, maxMessageSize+1)
	n, err := conn.Read(response)
	if err != nil {
		return nil, err
	}
	if n > maxMessageSize {
		return nil, errors.New("STUN response exceeds maximum message size")
	}
	return ParseBindingResponse(response[:n], txID)
}

func parseXORMappedAddress(value []byte, txID TransactionID) (*net.UDPAddr, error) {
	if len(value) < 4 {
		return nil, errors.New("XOR-MAPPED-ADDRESS is too short")
	}
	port := int(binary.BigEndian.Uint16(value[2:4]) ^ uint16(magicCookie>>16))
	switch value[1] {
	case 0x01:
		if len(value) != 8 {
			return nil, errors.New("IPv4 XOR-MAPPED-ADDRESS has invalid length")
		}
		cookie := make([]byte, 4)
		binary.BigEndian.PutUint32(cookie, magicCookie)
		ip := make(net.IP, net.IPv4len)
		for i := range ip {
			ip[i] = value[4+i] ^ cookie[i]
		}
		return &net.UDPAddr{IP: ip, Port: port}, nil
	case 0x02:
		if len(value) != 20 {
			return nil, errors.New("IPv6 XOR-MAPPED-ADDRESS has invalid length")
		}
		mask := make([]byte, net.IPv6len)
		binary.BigEndian.PutUint32(mask[0:4], magicCookie)
		copy(mask[4:], txID[:])
		ip := make(net.IP, net.IPv6len)
		for i := range ip {
			ip[i] = value[4+i] ^ mask[i]
		}
		return &net.UDPAddr{IP: ip, Port: port}, nil
	default:
		return nil, fmt.Errorf("unsupported XOR-MAPPED-ADDRESS family 0x%02x", value[1])
	}
}

func paddedLength(length int) int {
	return (length + 3) &^ 3
}
