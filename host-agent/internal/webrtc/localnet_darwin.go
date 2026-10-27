package webrtc

import (
	"crypto/rand"
	"net"
)

// AskLocalNetwork makes macOS ask, now, whether FreeDesk may reach devices
// on the local network, if it has never asked. From macOS 15 a program
// cannot send to an address on its own network until the operator has
// allowed it, and the first attempt is what brings the question up: for the
// agent that would be the first connection from a computer on the same
// network, behind the consent prompt, and macOS may refuse that attempt
// before the question is even answered. So the question is brought up at
// start-up, with the others, the way Apple's TN3179 shows: a UDP socket
// connected to a made-up address on each network, which sends nothing. A
// decision already made, either way, brings up nothing. Best effort: a
// network where this fails is simply asked about later.
func AskLocalNetwork() {
	ifaces, err := net.Interfaces()
	if err != nil {
		return
	}
	for _, ifc := range ifaces {
		if ifc.Flags&net.FlagUp == 0 || ifc.Flags&net.FlagBroadcast == 0 {
			continue
		}
		addrs, err := ifc.Addrs()
		if err != nil {
			continue
		}
		for _, a := range addrs {
			ipn, ok := a.(*net.IPNet)
			if !ok {
				continue
			}
			if ip4 := ipn.IP.To4(); ip4 != nil {
				connectRandomHost("udp4", ip4, ipn.Mask, "")
			} else if ipn.IP.IsLinkLocalUnicast() {
				connectRandomHost("udp6", ipn.IP.To16(), ipn.Mask, ifc.Name)
			}
		}
	}
}

// connectRandomHost connects a UDP socket to a random host on the network of
// ip and mask (port 9, the discard service, should anything ever be sent)
// and closes it again. Connecting a UDP socket sends nothing.
func connectRandomHost(network string, ip net.IP, mask net.IPMask, zone string) {
	if len(mask) != len(ip) {
		return
	}
	host := make(net.IP, len(ip))
	if _, err := rand.Read(host); err != nil {
		return
	}
	for i := range host {
		host[i] = ip[i]&mask[i] | host[i]&^mask[i]
	}
	c, err := net.DialUDP(network, nil, &net.UDPAddr{IP: host, Port: 9, Zone: zone})
	if err == nil {
		c.Close()
	}
}
