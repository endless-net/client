package client

import (
	"encoding/binary"
	"errors"
	"net"
)

const (
	testSTUNHeaderLength         = 20
	testSTUNBindingRequestType   = 0x0001
	testSTUNBindingSuccessType   = 0x0101
	testSTUNXORMappedAddressType = 0x0020
	testSTUNMagicCookie          = 0x2112A442
)

func buildSTUNBindingResponseForTest(request []byte, remote *net.UDPAddr) ([]byte, error) {
	if len(request) < testSTUNHeaderLength {
		return nil, errors.New("STUN request is shorter than header")
	}
	if binary.BigEndian.Uint16(request[0:2]) != testSTUNBindingRequestType {
		return nil, errors.New("STUN request is not Binding")
	}
	length := int(binary.BigEndian.Uint16(request[2:4]))
	if length%4 != 0 || testSTUNHeaderLength+length != len(request) {
		return nil, errors.New("STUN request length does not match packet")
	}
	if binary.BigEndian.Uint32(request[4:8]) != testSTUNMagicCookie {
		return nil, errors.New("STUN request magic cookie mismatch")
	}
	if remote == nil {
		return nil, errors.New("remote UDP address is required")
	}
	txID := request[8:20]
	attribute, err := buildTestSTUNXORMappedAddress(remote, txID)
	if err != nil {
		return nil, err
	}
	response := make([]byte, testSTUNHeaderLength+len(attribute))
	binary.BigEndian.PutUint16(response[0:2], testSTUNBindingSuccessType)
	binary.BigEndian.PutUint16(response[2:4], uint16(len(attribute)))
	binary.BigEndian.PutUint32(response[4:8], testSTUNMagicCookie)
	copy(response[8:20], txID)
	copy(response[20:], attribute)
	return response, nil
}

func buildTestSTUNXORMappedAddress(addr *net.UDPAddr, txID []byte) ([]byte, error) {
	if ip := addr.IP.To4(); ip != nil {
		attribute := make([]byte, 12)
		binary.BigEndian.PutUint16(attribute[0:2], testSTUNXORMappedAddressType)
		binary.BigEndian.PutUint16(attribute[2:4], 8)
		attribute[5] = 0x01
		binary.BigEndian.PutUint16(attribute[6:8], uint16(addr.Port)^uint16(testSTUNMagicCookie>>16))
		cookie := make([]byte, 4)
		binary.BigEndian.PutUint32(cookie, testSTUNMagicCookie)
		for index := range ip {
			attribute[8+index] = ip[index] ^ cookie[index]
		}
		return attribute, nil
	}
	ip := addr.IP.To16()
	if ip == nil {
		return nil, errors.New("remote IP is invalid")
	}
	attribute := make([]byte, 24)
	binary.BigEndian.PutUint16(attribute[0:2], testSTUNXORMappedAddressType)
	binary.BigEndian.PutUint16(attribute[2:4], 20)
	attribute[5] = 0x02
	binary.BigEndian.PutUint16(attribute[6:8], uint16(addr.Port)^uint16(testSTUNMagicCookie>>16))
	mask := make([]byte, 16)
	binary.BigEndian.PutUint32(mask[0:4], testSTUNMagicCookie)
	copy(mask[4:], txID)
	for index := range ip {
		attribute[8+index] = ip[index] ^ mask[index]
	}
	return attribute, nil
}
