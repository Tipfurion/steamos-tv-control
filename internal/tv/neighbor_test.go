package tv

import "testing"

func TestNeighborMACDoesNotUseIncompleteEntries(t *testing.T) {
	table := "IP address HW type Flags HW address Mask Device\n192.0.2.1 0x1 0x0 00:00:00:00:00:00 * eth0\n192.0.2.2 0x1 0x2 00:11:22:33:44:55 * eth0\n"
	if parseNeighbor(table, "192.0.2.1") != "" || parseNeighbor(table, "192.0.2.2") != "00:11:22:33:44:55" {
		t.Fatal("invalid neighbor lookup")
	}
}
