/*
Cloudflare internship application.

Arman Ashrafian
4/14/2020

This application does not use any external libraries to ping. If this was not a
job application I would build a ping cli application with the
github.com/sparrc/go-ping package.

Supports both IPv4 and IPv6 addresses
*/

package main

import (
	"flag"
	"fmt"
	"net"
	"os"
	"os/signal"
	"time"

	"golang.org/x/net/icmp"
	"golang.org/x/net/ipv4"
	"golang.org/x/net/ipv6"
)

const (
	ProtocolICMP   = 1
	ProtocolICMPv6 = 58 //https://godoc.org/golang.org/x/net/internal/iana
)

// We use this client to send ICMP echo requests to the server
type PingClient struct {
	IPAddr    *net.IPAddr // IP addr of server being pinged
	Addr      string      // domain name or IP addr of server being pinged
	PacketOut int         // number of packets sent
	PacketIn  int         // number of packets recieved
	IPv4      bool        // server addr is IPv4
	Seq       int         // icmp sequence number
	TotalTime float64     // total rtt time for average
	RTTMax    float64     // max rtt time
	RTTMin    float64     // min rtt time
}

// Initialize and return a new PingClient
func NewClient(addr string) (*PingClient, error) {
	// resolve ip address
	ipaddr, err := net.ResolveIPAddr("ip", addr)

	if err != nil {
		return nil, err
	}

	// determine ipv4 or ipv6
	isIPv4 := (len(ipaddr.IP) == net.IPv4len)

	fmt.Printf("PING %s (%s)\n", addr, ipaddr)

	return &PingClient{
		IPAddr:    ipaddr,
		Addr:      addr,
		PacketOut: 0,
		PacketIn:  0,
		IPv4:      isIPv4,
		Seq:       0,
		TotalTime: 0,
		RTTMax:    -1e5,
		RTTMin:    1e5,
	}, nil
}

// send a single ICMP echo request to server
func (pc *PingClient) Ping(ttl int) error {
	var proto int
	var network string
	var msgType icmp.Type
	var replyType icmp.Type

	if pc.IPv4 {
		proto = ProtocolICMP
		network = "ip4:icmp"
		msgType = ipv4.ICMPTypeEcho
		replyType = ipv4.ICMPTypeEchoReply
	} else {
		proto = ProtocolICMPv6
		network = "ip6:ipv6-icmp"
		msgType = ipv6.ICMPTypeEchoRequest
		replyType = ipv6.ICMPTypeEchoReply
	}

	// listen to icmp replies
	c, err := icmp.ListenPacket(network, "0.0.0.0")

	// turn on ttl flag
	if pc.IPv4 {
		c.IPv4PacketConn().SetControlMessage(ipv4.FlagTTL, true)
	} else {
		c.IPv6PacketConn().SetControlMessage(ipv6.FlagHopLimit, true)
	}

	if err != nil {
		return err
	}

	// make message
	m := icmp.Message{
		Type: msgType, Code: 0,
		Body: &icmp.Echo{
			ID:   os.Getpid() & 0xffff, // example in docs does this
			Seq:  pc.Seq,
			Data: []byte("ping message"), // TODO: parameterize msg/msgsize
		},
	}
	pc.Seq++
	pc.PacketOut++

	marsh, err := m.Marshal(nil)
	if err != nil {
		return err
	}

	// send the message
	start := time.Now()
	n, err := c.WriteTo(marsh, pc.IPAddr)
	if err != nil {
		return err
	} else if n != len(marsh) {
		return fmt.Errorf("error marshalling message\n")
	}

	// wait for reply
	reply := make([]byte, 500)
	err = c.SetReadDeadline(time.Now().Add(10 * time.Second))
	if err != nil {
		return err
	}

	// read reply message
	var pttl int
	if pc.IPv4 {
		_, cm, _, err := c.IPv4PacketConn().ReadFrom(reply)
		if err != nil {
			return err
		}
		pttl = cm.TTL
	} else {
		_, cm, _, err := c.IPv6PacketConn().ReadFrom(reply)
		if err != nil {
			return err
		}
		pttl = cm.HopLimit

	}

	if pttl > ttl {
		fmt.Println("timer exceeded")
		return nil
	}

	duration := time.Since(start)
	dur_ms := duration.Seconds() * 1e3

	// keep track of max/min RTT times
	if dur_ms < pc.RTTMin {
		pc.RTTMin = dur_ms
	}
	if dur_ms > pc.RTTMax {
		pc.RTTMax = dur_ms
	}
	pc.TotalTime += dur_ms

	// parse reply
	rMsg, err := icmp.ParseMessage(proto, reply[:n])
	rMsgLen := rMsg.Body.Len(proto)
	if err != nil {
		return err
	}

	switch rMsg.Type {
	case replyType:
		pc.PacketIn++
		fmt.Printf("%d bytes recieved (0%% loss) from %s icmp_seq=%d time=%.1f ms\n",
			rMsgLen, pc.IPAddr, pc.Seq, dur_ms)
		return nil
	default:
		return fmt.Errorf("error")
	}

}

func main() {
	var ttl int

	flag.IntVar(&ttl, "t", 64, "Time to live in ms")
	flag.Parse()
	addr := flag.Arg(0) // ./ping {addr = IP || DomainName}

	// new ping client
	client, err := NewClient(addr)
	if err != nil {
		fmt.Println(err)
		os.Exit(1)
	}

	// set up ctrl-c signal to exit
	sigchan := make(chan os.Signal, 1)
	signal.Notify(sigchan, os.Interrupt)
	go func(client *PingClient) {
		for _ = range sigchan {
			loss := (float64(client.PacketOut-client.PacketIn) / float64(client.PacketOut)) * 100
			fmt.Println("\n------ Ping Statistics ------")
			fmt.Printf("packets sent: %d, packets received: %d, %.0f%% loss\n",
				client.PacketOut, client.PacketIn, loss)
			if client.PacketIn > 0 {
				fmt.Printf("rtt min/avg/max = %.1f/%.1f/%.1f ms\n",
					client.RTTMin, client.TotalTime/float64(client.PacketIn), client.RTTMax)
			}
			os.Exit(0)
		}
	}(client)

	// MAIN LOOP
	// Continuously pings the server until ctrl-c is entered, which
	// then prints the ping statistics
	for {
		err = client.Ping(ttl)
		if err != nil {
			fmt.Println(err)
		}
		time.Sleep(time.Second * 1) // ping once per second
	}

}
