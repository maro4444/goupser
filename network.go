package main

import (
	"bytes"
	"crypto/rand"
	"encoding/binary"
	"errors"
	"net"
	"time"
)

const (
	stunTimeout     = 2 * time.Second
	stunMagicCookie = 0x2112A442

	stunBindingRequest  = 0x0001
	stunBindingResponse = 0x0101
	stunAttrMapped      = 0x0001
	stunAttrXorMapped   = 0x0020
)

// Publiczne serwery STUN, odpytywane po kolei do pierwszej poprawnej odpowiedzi.
var stunServers = []string{
	"stun.l.google.com:19302",
	"stun.cloudflare.com:3478",
}

// lanIPv4Addrs zwraca prywatne adresy IPv4 aktywnych interfejsów sieciowych,
// czyli adresy, pod którymi serwer jest osiągalny z sieci lokalnej.
func lanIPv4Addrs() []string {
	ifaces, err := net.Interfaces()
	if err != nil {
		return nil
	}
	var addrs []string
	for _, iface := range ifaces {
		if iface.Flags&net.FlagUp == 0 || iface.Flags&net.FlagLoopback != 0 {
			continue
		}
		ifAddrs, err := iface.Addrs()
		if err != nil {
			continue
		}
		for _, a := range ifAddrs {
			ipNet, ok := a.(*net.IPNet)
			if !ok {
				continue
			}
			ip := ipNet.IP.To4()
			if ip == nil || !ip.IsPrivate() {
				continue
			}
			addrs = append(addrs, ip.String())
		}
	}
	return addrs
}

// primaryLANAddr zwraca adres interfejsu, przez który idzie trasa domyślna
// (zwykle fizyczna karta Wi-Fi albo Ethernet, a nie karty wirtualne Hyper-V/WSL).
// "Połączenie" UDP niczego nie wysyła, system tylko wybiera trasę.
func primaryLANAddr() (string, bool) {
	conn, err := net.Dial("udp4", "8.8.8.8:80")
	if err != nil {
		return "", false
	}
	defer conn.Close()
	ip := conn.LocalAddr().(*net.UDPAddr).IP.To4()
	if ip == nil || ip.IsLoopback() || ip.IsLinkLocalUnicast() {
		return "", false
	}
	return ip.String(), true
}

// publicAddrSTUN ustala zewnętrzny (publiczny) adres IPv4 komputera,
// pytając serwer STUN, z jakiego adresu widzi nasze zapytanie.
func publicAddrSTUN() (string, error) {
	err := errors.New("brak serwerów STUN")
	for _, server := range stunServers {
		var ip net.IP
		if ip, err = stunQuery(server); err == nil {
			return ip.String(), nil
		}
	}
	return "", err
}

// stunQuery wysyła pojedyncze zapytanie STUN Binding (RFC 5389) i odczytuje
// z odpowiedzi adres, spod którego serwer widzi klienta.
func stunQuery(server string) (net.IP, error) {
	conn, err := net.DialTimeout("udp4", server, stunTimeout)
	if err != nil {
		return nil, err
	}
	defer conn.Close()
	conn.SetDeadline(time.Now().Add(stunTimeout))

	req := make([]byte, 20)
	binary.BigEndian.PutUint16(req[0:], stunBindingRequest)
	binary.BigEndian.PutUint32(req[4:], stunMagicCookie)
	txID := req[8:20]
	if _, err := rand.Read(txID); err != nil {
		return nil, err
	}
	if _, err := conn.Write(req); err != nil {
		return nil, err
	}

	resp := make([]byte, 1500)
	n, err := conn.Read(resp)
	if err != nil {
		return nil, err
	}
	resp = resp[:n]
	if n < 20 || binary.BigEndian.Uint16(resp[0:]) != stunBindingResponse ||
		binary.BigEndian.Uint32(resp[4:]) != stunMagicCookie || !bytes.Equal(resp[8:20], txID) {
		return nil, errors.New("niepoprawna odpowiedź STUN")
	}

	var mapped net.IP
	attrs := resp[20:]
	for len(attrs) >= 4 {
		typ := binary.BigEndian.Uint16(attrs[0:])
		length := int(binary.BigEndian.Uint16(attrs[2:]))
		if 4+length > len(attrs) {
			break
		}
		val := attrs[4 : 4+length]
		// Interesuje nas tylko IPv4: bajt 1 = rodzina (0x01), bajty 4-7 = adres.
		if length >= 8 && val[1] == 0x01 {
			ip := net.IP(append([]byte(nil), val[4:8]...))
			switch typ {
			case stunAttrXorMapped:
				var cookie [4]byte
				binary.BigEndian.PutUint32(cookie[:], stunMagicCookie)
				for i := range ip {
					ip[i] ^= cookie[i]
				}
				return ip, nil
			case stunAttrMapped:
				mapped = ip
			}
		}
		// Atrybuty są wyrównane do 4 bajtów.
		next := 4 + (length+3)&^3
		if next > len(attrs) {
			break
		}
		attrs = attrs[next:]
	}
	if mapped != nil {
		return mapped, nil
	}
	return nil, errors.New("brak adresu w odpowiedzi STUN")
}
