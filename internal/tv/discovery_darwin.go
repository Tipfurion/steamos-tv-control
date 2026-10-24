//go:build darwin

package tv

import (
	"context"
	"net"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"time"

	atv "github.com/jkiddo/atvremote/pkg/v2/remote"
)

// Bonjour knows the actual multicast interface, including Macs with VPNs.
func discoverNative(ctx context.Context) ([]atv.DiscoveredDevice, error) {
	browse, cancel := context.WithTimeout(ctx, 3*time.Second)
	b, _ := exec.CommandContext(browse, "dns-sd", "-B", atv.MDNSServiceType, "local.").Output()
	cancel()
	var devices []atv.DiscoveredDevice
	seen := map[string]bool{}
	pattern := regexp.MustCompile(`can be reached at (.+):([0-9]+) \(interface`)
	for _, line := range strings.Split(string(b), "\n") {
		fields := strings.Fields(line)
		index := -1
		for i, f := range fields {
			if f == atv.MDNSServiceType+"." {
				index = i
				break
			}
		}
		if index < 0 || index+1 >= len(fields) {
			continue
		}
		name := strings.Join(fields[index+1:], " ")
		if seen[name] {
			continue
		}
		seen[name] = true
		lookup, stop := context.WithTimeout(ctx, 2*time.Second)
		out, _ := exec.CommandContext(lookup, "dns-sd", "-L", name, atv.MDNSServiceType, "local.").Output()
		stop()
		match := pattern.FindStringSubmatch(string(out))
		if len(match) != 3 {
			continue
		}
		port, _ := strconv.Atoi(match[2])
		addresses, err := net.DefaultResolver.LookupIPAddr(ctx, match[1])
		if err != nil {
			continue
		}
		for _, ip := range addresses {
			if ip.IP.To4() != nil {
				devices = append(devices, atv.DiscoveredDevice{Name: name, Host: match[1], IP: ip.IP.String(), Port: port})
				break
			}
		}
	}
	return devices, nil
}
