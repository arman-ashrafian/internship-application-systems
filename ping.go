/*
Cloudflare internship application.

Arman Ashrafian
4/14/2020

This application does not use any external libraries to ping. If this was not a
job application I would build a ping cli application with the
github.com/sparrc/go-ping package.
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
func (pc *PingClient) Ping() error {
	var proto int
	var network string
	var msgType icmp.Type

	if pc.IPv4 {
		proto = ProtocolICMP
		network = "ip4:icmp"
		msgType = ipv4.ICMPTypeEcho
	} else {
		proto = ProtocolICMPv6
		network = "ip6:ipv6-icmp"
		msgType = ipv6.ICMPTypeEchoRequest
	}

	// listen to icmp replies
	c, err := icmp.ListenPacket(network, "0.0.0.0")
	if err != nil {
		return err
	}
	defer c.Close()

	// make message
	m := icmp.Message{
		Type: msgType, Code: 0,
		Body: &icmp.Echo{
			ID:   os.Getpid() & 0xffff,   // example in docs does this
			Seq:  pc.Seq,                 // TODO: keep track of seq number
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
	err = c.SetReadDeadline(time.Now().Add(10 * time.Second)) // TODO: TTL set to 10 seconds
	if err != nil {
		return err
	}
	n, _, err = c.ReadFrom(reply)
	if err != nil {
		return err
	}
	duration := time.Since(start)
	dur_ms := duration.Seconds() * 1e3

	if dur_ms < pc.RTTMin {
		pc.RTTMin = dur_ms
	}
	if dur_ms > pc.RTTMax {
		pc.RTTMax = dur_ms
	}
	pc.TotalTime += dur_ms

	rMsg, err := icmp.ParseMessage(proto, reply[:n])
	rMsgLen := rMsg.Body.Len(proto)
	if err != nil {
		return err
	}

	switch rMsg.Type {
	case ipv4.ICMPTypeEchoReply:
		pc.PacketIn++
		fmt.Printf("%d bytes recieved from %s icmp_seq=%d time=%.1f ms\n",
			rMsgLen, pc.IPAddr, pc.Seq, dur_ms)
		return nil
	default:
		return fmt.Errorf("error")
	}

}

func main() {
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
			fmt.Println("\n------ Ping Statistics ------")
			fmt.Printf("packets sent: %d, packets received: %d\n"+
				"rtt min/avg/max = %.1f/%.1f/%.1f ms\n",
				client.PacketOut, client.PacketIn,
				client.RTTMin, client.TotalTime/float64(client.PacketIn), client.RTTMax)
			os.Exit(0)
		}
	}(client)

	// MAIN LOOP
	// Continuously pings the server until ctrl-c is entered, which
	// then prints the ping statistics
	for {
		err = client.Ping()
		if err != nil {
			fmt.Println(err)
		}
		time.Sleep(time.Second * 1) // ping once per second
	}

}
