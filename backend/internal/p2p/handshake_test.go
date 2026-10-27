package p2p

import (
	"bufio"
	"bytes"
	"encoding/binary"
	"testing"
)

// buildHandshakeFrame будує сирий Minecraft handshake-фрейм так, як це
// робить справжній клієнт гри.
func buildHandshakeFrame(protocolVersion int32, addr string, port uint16, nextState int32) []byte {
	appendVarint := func(b []byte, v int32) []byte {
		u := uint32(v)
		for u >= 0x80 {
			b = append(b, byte(u)|0x80)
			u >>= 7
		}
		return append(b, byte(u))
	}

	payload := appendVarint(nil, 0x00) // packet id
	payload = appendVarint(payload, protocolVersion)
	payload = appendVarint(payload, int32(len(addr)))
	payload = append(payload, addr...)
	payload = binary.BigEndian.AppendUint16(payload, port)
	payload = appendVarint(payload, nextState)

	frame := appendVarint(nil, int32(len(payload)))
	frame = append(frame, payload...)
	return frame
}

func TestReadHandshake_AddressRoundTrip(t *testing.T) {
	frame := buildHandshakeFrame(770, "prlool-482917.play.shaurma-launcher.org", 45678, 2)
	br := bufio.NewReader(bytes.NewReader(frame))

	hs, err := ReadHandshake(br)
	if err != nil {
		t.Fatalf("ReadHandshake: %v", err)
	}
	if hs.ServerAddress != "prlool-482917.play.shaurma-launcher.org" {
		t.Fatalf("ServerAddress = %q", hs.ServerAddress)
	}
	if hs.NextState != 2 {
		t.Fatalf("NextState = %d, want 2", hs.NextState)
	}
	if !bytes.Equal(hs.Raw, frame) {
		t.Fatalf("Raw не збігається з вихідним фреймом:\n got=%x\nwant=%x", hs.Raw, frame)
	}

	addr, err := hs.Address("play.shaurma-launcher.org")
	if err != nil {
		t.Fatalf("Address: %v", err)
	}
	if addr != "prlool-482917" {
		t.Fatalf("Address = %q, want prlool-482917", addr)
	}
}

func TestReadHandshake_WrongSuffix(t *testing.T) {
	frame := buildHandshakeFrame(770, "hypixel.net", 25565, 2)
	br := bufio.NewReader(bytes.NewReader(frame))

	hs, err := ReadHandshake(br)
	if err != nil {
		t.Fatalf("ReadHandshake: %v", err)
	}
	if _, err := hs.Address("play.shaurma-launcher.org"); err == nil {
		t.Fatalf("очікувалась помилка для домену, що не належить play-суфіксу")
	}
}

func TestReadHandshake_NotAHandshake(t *testing.T) {
	junk := []byte{0x05, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF}
	br := bufio.NewReader(bytes.NewReader(junk))
	if _, err := ReadHandshake(br); err == nil {
		t.Fatalf("очікувалась помилка на сміттєвих даних")
	}
}

func TestAddress_CaseInsensitiveLowercased(t *testing.T) {
	hs := Handshake{ServerAddress: "PrLoOl-482917.PLAY.shaurma-launcher.org"}
	addr, err := hs.Address("play.shaurma-launcher.org")
	if err != nil {
		t.Fatalf("Address: %v", err)
	}
	if addr != "prlool-482917" {
		t.Fatalf("addr = %q, want prlool-482917", addr)
	}
}

func TestAddress_StripsTrailingPort(t *testing.T) {
	hs := Handshake{ServerAddress: "prlool-482917.play.shaurma-launcher.org:25565"}
	addr, err := hs.Address("play.shaurma-launcher.org")
	if err != nil {
		t.Fatalf("Address: %v", err)
	}
	if addr != "prlool-482917" {
		t.Fatalf("addr = %q, want prlool-482917", addr)
	}
}
