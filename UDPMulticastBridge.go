package main

import (
	"encoding/binary"
	"flag"
	"fmt"
	"hash/fnv"
	"log"
	"net"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"golang.org/x/net/ipv4"
)

const (
	MaxPacketSize = 65535
	SendQueueSize = 65536
)

type Packet struct {
	Data []byte
	Len  int
	From string
}

type RecentCache struct {
	mu    sync.Mutex
	ttl   time.Duration
	items map[uint64]time.Time
}

func NewRecentCache(ttl time.Duration) *RecentCache {
	return &RecentCache{
		ttl:   ttl,
		items: make(map[uint64]time.Time),
	}
}

func hashPayload(data []byte) uint64 {
	h := fnv.New64a()
	_, _ = h.Write(data)
	return h.Sum64()
}

func (c *RecentCache) SeenOrAdd(data []byte) bool {
	if c == nil {
		return false
	}

	now := time.Now()
	key := hashPayload(data)

	c.mu.Lock()
	defer c.mu.Unlock()

	if t, ok := c.items[key]; ok {
		if now.Sub(t) < c.ttl {
			return true
		}
	}

	c.items[key] = now

	if len(c.items) > 200000 {
		for k, t := range c.items {
			if now.Sub(t) > c.ttl {
				delete(c.items, k)
			}
		}
	}

	return false
}

type Bridge struct {
	pool      sync.Pool
	toMcast   chan Packet
	toRemotes chan Packet
	cache     *RecentCache

	mcastGroup string
	mcastPort  int
	listenPort int
	remoteList []string
	ifaceName  string
}

func NewBridge(listenPort int, mcastGroup string, mcastPort int, remotes []string, ifaceName string, loopGuardTTL time.Duration) *Bridge {
	b := &Bridge{
		toMcast:    make(chan Packet, SendQueueSize),
		toRemotes:  make(chan Packet, SendQueueSize),
		mcastGroup: mcastGroup,
		mcastPort:  mcastPort,
		listenPort: listenPort,
		remoteList: remotes,
		ifaceName:  ifaceName,
	}

	b.pool.New = func() any {
		buf := make([]byte, MaxPacketSize)
		return &buf
	}

	if loopGuardTTL > 0 {
		b.cache = NewRecentCache(loopGuardTTL)
	}

	return b
}

func (b *Bridge) getBuffer() []byte {
	p := b.pool.Get().(*[]byte)
	return *p
}

func (b *Bridge) putBuffer(buf []byte) {
	if cap(buf) < MaxPacketSize {
		return
	}
	buf = buf[:MaxPacketSize]
	b.pool.Put(&buf)
}

func (b *Bridge) Start() error {
	go b.listenUnicast()
	go b.listenMulticast()
	go b.sendToMulticastWorker()
	go b.sendToRemoteWorker()

	return nil
}

func (b *Bridge) listenUnicast() {
	addr := &net.UDPAddr{
		IP:   net.IPv4zero,
		Port: b.listenPort,
	}

	conn, err := net.ListenUDP("udp4", addr)
	if err != nil {
		log.Fatalf("unicast listen failed: %v", err)
	}
	defer conn.Close()

	log.Printf("UDP listener started: 0.0.0.0:%d", b.listenPort)

	for {
		buf := b.getBuffer()

		n, from, err := conn.ReadFromUDP(buf)
		if err != nil {
			b.putBuffer(buf)
			continue
		}

		data := buf[:n]

		if b.cache != nil {
			_ = b.cache.SeenOrAdd(data)
		}

		select {
		case b.toMcast <- Packet{Data: data, Len: n, From: from.String()}:
		default:
			b.putBuffer(buf)
		}
	}
}

func (b *Bridge) listenMulticast() {
	group := net.ParseIP(b.mcastGroup)
	if group == nil {
		log.Fatalf("invalid multicast group: %s", b.mcastGroup)
	}

	addr := &net.UDPAddr{
		IP:   net.IPv4zero,
		Port: b.mcastPort,
	}

	conn, err := net.ListenUDP("udp4", addr)
	if err != nil {
		log.Fatalf("multicast listen failed: %v", err)
	}
	defer conn.Close()

	p := ipv4.NewPacketConn(conn)

	var iface *net.Interface
	if b.ifaceName != "" {
		iface, err = net.InterfaceByName(b.ifaceName)
		if err != nil {
			log.Fatalf("interface not found: %v", err)
		}
	}

	if err := p.JoinGroup(iface, &net.UDPAddr{IP: group}); err != nil {
		log.Fatalf("join multicast failed: %v", err)
	}

	_ = p.SetMulticastLoopback(false)

	log.Printf("multicast listener started: %s:%d", b.mcastGroup, b.mcastPort)

	for {
		buf := b.getBuffer()

		n, from, err := conn.ReadFromUDP(buf)
		if err != nil {
			b.putBuffer(buf)
			continue
		}

		data := buf[:n]

		if b.cache != nil && b.cache.SeenOrAdd(data) {
			b.putBuffer(buf)
			continue
		}

		select {
		case b.toRemotes <- Packet{Data: data, Len: n, From: from.String()}:
		default:
			b.putBuffer(buf)
		}
	}
}

func (b *Bridge) sendToMulticastWorker() {
	group := net.ParseIP(b.mcastGroup)
	addr := &net.UDPAddr{
		IP:   group,
		Port: b.mcastPort,
	}

	conn, err := net.DialUDP("udp4", nil, addr)
	if err != nil {
		log.Fatalf("multicast sender failed: %v", err)
	}
	defer conn.Close()

	p := ipv4.NewPacketConn(conn)
	_ = p.SetMulticastTTL(1)
	_ = p.SetMulticastLoopback(false)

	for pkt := range b.toMcast {
		_, _ = conn.Write(pkt.Data[:pkt.Len])
		b.putBuffer(pkt.Data)
	}
}

func (b *Bridge) sendToRemoteWorker() {
	remoteAddrs := make([]*net.UDPAddr, 0, len(b.remoteList))

	for _, r := range b.remoteList {
		addr, err := net.ResolveUDPAddr("udp4", r)
		if err != nil {
			log.Printf("invalid remote address %s: %v", r, err)
			continue
		}
		remoteAddrs = append(remoteAddrs, addr)
	}

	conn, err := net.ListenUDP("udp4", nil)
	if err != nil {
		log.Fatalf("remote sender failed: %v", err)
	}
	defer conn.Close()

	for pkt := range b.toRemotes {
		for _, addr := range remoteAddrs {
			_, _ = conn.WriteToUDP(pkt.Data[:pkt.Len], addr)
		}
		b.putBuffer(pkt.Data)
	}
}

func parseRemotes(raw string) []string {
	if raw == "" {
		return nil
	}

	var out []string
	start := 0

	for i := 0; i <= len(raw); i++ {
		if i == len(raw) || raw[i] == ',' {
			if i > start {
				out = append(out, raw[start:i])
			}
			start = i + 1
		}
	}

	return out
}

func main() {
	listenPort := flag.Int("listen", 15000, "UDP unicast listen port")
	mcastGroup := flag.String("mgroup", "239.10.0.1", "multicast group")
	mcastPort := flag.Int("mport", 5000, "multicast port")
	remotes := flag.String("remotes", "", "remote UDP listeners, comma separated. example: 1.2.3.4:15000,5.6.7.8:15000")
	iface := flag.String("iface", "", "network interface name. optional")
	loopTTL := flag.Duration("loopttl", 300*time.Millisecond, "payload hash loop guard ttl. 0 disables it")

	flag.Parse()

	_ = binary.LittleEndian

	bridge := NewBridge(
		*listenPort,
		*mcastGroup,
		*mcastPort,
		parseRemotes(*remotes),
		*iface,
		*loopTTL,
	)

	if err := bridge.Start(); err != nil {
		log.Fatal(err)
	}

	fmt.Println("UDP Raw Multicast Bridge running")
	fmt.Printf("unicast listen : 0.0.0.0:%d\n", *listenPort)
	fmt.Printf("multicast      : %s:%d\n", *mcastGroup, *mcastPort)
	fmt.Printf("remotes        : %v\n", parseRemotes(*remotes))

	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)
	<-sig
}
