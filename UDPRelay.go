package main

import (
	"flag"
	"log"
	"net"
	"runtime"

	"golang.org/x/net/ipv4"
)

const MaxPacketSize = 65535

func main() {
	runtime.GOMAXPROCS(runtime.NumCPU())

	listenPort := flag.Int("listen", 15000, "UDP listen port")
	mcastGroup := flag.String("mgroup", "239.10.0.1", "multicast group")
	mcastPort := flag.Int("mport", 5000, "multicast port")
	ttl := flag.Int("ttl", 1, "multicast TTL")
	flag.Parse()

	listenConn, err := net.ListenUDP("udp4", &net.UDPAddr{
		IP:   net.IPv4zero,
		Port: *listenPort,
	})
	if err != nil {
		log.Fatalf("listen failed: %v", err)
	}
	defer listenConn.Close()

	_ = listenConn.SetReadBuffer(16 * 1024 * 1024)

	mcastAddr := &net.UDPAddr{
		IP:   net.ParseIP(*mcastGroup),
		Port: *mcastPort,
	}

	sendConn, err := net.DialUDP("udp4", nil, mcastAddr)
	if err != nil {
		log.Fatalf("multicast dial failed: %v", err)
	}
	defer sendConn.Close()

	_ = sendConn.SetWriteBuffer(16 * 1024 * 1024)

	p := ipv4.NewPacketConn(sendConn)
	_ = p.SetMulticastTTL(*ttl)
	_ = p.SetMulticastLoopback(false)

	log.Printf("UDP listen  : 0.0.0.0:%d", *listenPort)
	log.Printf("Multicast   : %s:%d", *mcastGroup, *mcastPort)

	buffer := make([]byte, MaxPacketSize)

	for {
		n, _, err := listenConn.ReadFromUDP(buffer)
		if err != nil {
			continue
		}

		_, _ = sendConn.Write(buffer[:n])
	}
}
