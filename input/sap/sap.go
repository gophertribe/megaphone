package sap

import (
	"bufio"
	"encoding/hex"
	"fmt"
	"io"
	"net"
)

type Header struct {
	Version              int    `json:"version"`
	AddressType          byte   `json:"address_type"`
	MessageType          byte   `json:"message_type"`
	Encrypted            bool   `json:"encrypted"`
	Compressed           bool   `json:"compressed"`
	AuthenticationLength int    `json:"authentication_length"`
	MessageIdHash        string `yaml:"message_id_hash"`
	OriginatingSource    net.IP `json:"originating_source"`
	PayloadType          string `json:"payload_type"`
}

// Parse parses header from provided reader. It will wrap the reader in bufio.Reader if not already done.
// TODO: Address auth and compression.
func (header *Header) Parse(r io.Reader) error {
	var br *bufio.Reader
	var ok bool
	if br, ok = r.(*bufio.Reader); !ok {
		br = bufio.NewReader(br)
	}
	buf := make([]byte, 8)
	n, err := br.Read(buf)
	if err != nil {
		return fmt.Errorf("could not read from udp listener: %w", err)
	}
	if n < 8 {
		return fmt.Errorf("sap header too short")
	}
	header.Version = int(buf[0] >> 5)
	header.AddressType = buf[0] >> 4 & 0x01
	// 3rd bit is reserved
	header.MessageType = buf[0] >> 2 & 0x01
	header.Encrypted = buf[0]>>1&0x01 == 1
	header.Compressed = buf[0]&0x01 == 1
	header.AuthenticationLength = int(buf[1])
	header.MessageIdHash = hex.EncodeToString(buf[2:4])
	header.OriginatingSource = buf[4:8]
	auth := make([]byte, header.AuthenticationLength)
	_, err = br.Read(auth)
	if err != nil {
		return fmt.Errorf("could not read authentication: %w", err)
	}
	pt, err := br.ReadSlice(0x00)
	if err != nil {
		return fmt.Errorf("could not read payload type: %w", err)
	}
	header.PayloadType = string(pt[:len(pt)-1])
	return nil
}
