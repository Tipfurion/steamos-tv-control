package tv

import (
	"net"
	"os"
	"strings"
)

// A neighbor entry belongs to the IP interface. The mDNS "bt" attribute is a
// Bluetooth address and must never be used for Wake-on-LAN.
func NeighborMAC(host string) string {
	b, err := os.ReadFile("/proc/net/arp")
	if err != nil {
		return ""
	}
	return parseNeighbor(string(b), host)
}
func parseNeighbor(table, host string) string {
	for _, line := range strings.Split(table, "\n") {
		v := strings.Fields(line)
		if len(v) < 6 || v[0] != host || v[2] != "0x2" {
			continue
		}
		m, err := net.ParseMAC(v[3])
		if err == nil && len(m) == 6 && m.String() != "00:00:00:00:00:00" {
			return m.String()
		}
	}
	return ""
}
