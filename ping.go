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
	"bytes"
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
	MsgSize   int         // message body size (bytes)
	PLost     int         // total packets lost
}

// Initialize and return a new PingClient
func NewClient(addr string, msgSize int) (*PingClient, error) {
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
		MsgSize:   msgSize,
		PLost:     0,
	}, nil
}

// send a single ICMP echo request to server
func (pc *PingClient) Ping(ttl int) error {
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

	// set up ttl
	if pc.IPv4 {
		c.IPv4PacketConn().SetTTL(ttl)
	} else {
		c.IPv6PacketConn().SetHopLimit(ttl)
	}

	// make message
	messageData := bytes.Repeat([]byte("a"), pc.MsgSize)
	m := icmp.Message{
		Type: msgType, Code: 0,
		Body: &icmp.Echo{
			ID:   os.Getpid() & 0xffff, // example in docs does this
			Seq:  pc.Seq,
			Data: messageData,
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
	err = c.SetReadDeadline(time.Now().Add(5 * time.Second))
	if err != nil {
		return err
	}

	// read reply message
	n, _, err = c.ReadFrom(reply)
	if err != nil {
		return err
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
	if err != nil {
		return err
	}

	if n == 0 {
		fmt.Println("time limit exceeded")
	} else {
		pc.PacketIn++
		pLost := 0

		switch p := rMsg.Body.(type) {
		case *icmp.Echo:
			// definetly lost data
			if len(p.Data) < len(messageData) {
				pLost += len(messageData) - len(p.Data)
				for i := 0; i < len(p.Data); i++ {
					if messageData[i] != p.Data[i] {
						pLost++
					}
				}
			} else { // check if we lost data
				for i := 0; i < len(messageData); i++ {
					if messageData[i] != p.Data[i] {
						pLost++
					}
				}

				lossPercent := (float64(pLost) / float64(len(messageData))) * 100

				fmt.Printf("%d bytes recieved (%.1f%% loss) from %s icmp_seq=%d time=%.1f ms\n",
					len(p.Data), lossPercent, pc.IPAddr, pc.Seq, dur_ms)
			}
		}
	}

	return nil
}

func main() {
	var msgSize, ttl int

	flag.IntVar(&msgSize, "s", 64, "Size (in bytes) of ping message")
	flag.IntVar(&ttl, "t", 64, "Time to live, number L3 hops before packet dies")
	flag.Parse()

	addr := flag.Arg(0) // ./ping {addr = IP || DomainName}

	if flag.NArg() == 0 {
		fmt.Println("mising hostname")
		os.Exit(1)
	}

	// new ping client
	client, err := NewClient(addr, msgSize)
	if err != nil {
		fmt.Println(err)
		os.Exit(1)
	}

	// set up ctrl-c signal to exit
	sigchan := make(chan os.Signal, 1)
	signal.Notify(sigchan, os.Interrupt)
	go func(client *PingClient) {
		for _ = range sigchan {
			var loss float64 = 100
			if client.PacketIn > 0 {
				loss = (float64(client.PLost) / float64(client.PacketOut*client.MsgSize)) * 100
			}
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
