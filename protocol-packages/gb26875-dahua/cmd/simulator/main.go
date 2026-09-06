// A standalone device fixture: it never creates platform protocols or bindings.
package main

import (
	"encoding/hex"
	"flag"
	"fmt"
	"gb26875-dahua/gb26875"
	"io"
	"net"
	"os"
	"time"
)

func main() {
	address := flag.String("address", "127.0.0.1:26875", "generic listener address")
	network := flag.String("network", "tcp", "tcp or udp")
	source := flag.String("source", "123456789012", "six-byte device address in hex")
	hold := flag.Duration("hold", 0, "keep online and acknowledge time-sync commands, e.g. 1m")
	flag.Parse()
	if *network != "tcp" && *network != "udp" {
		fail(fmt.Errorf("network must be tcp or udp"))
	}
	data, err := hex.DecodeString(*source)
	fail(err)
	if len(data) != 6 {
		fail(fmt.Errorf("source must be six bytes"))
	}
	var device [6]byte
	copy(device[:], data)
	conn, err := net.DialTimeout(*network, *address, 5*time.Second)
	fail(err)
	defer conn.Close()
	read := func() []byte {
		if *network == "udp" {
			buf := make([]byte, 1024)
			n, err := conn.Read(buf)
			fail(err)
			return buf[:n]
		}
		head := make([]byte, 27)
		_, err := io.ReadFull(conn, head)
		fail(err)
		size := int(head[24]) + int(head[25])*256
		if size > 512 {
			fail(fmt.Errorf("invalid frame length"))
		}
		tail := make([]byte, size+3)
		_, err = io.ReadFull(conn, tail)
		fail(err)
		return append(head, tail...)
	}
	for _, frame := range [][]byte{gb26875.BuildGB26875RegistrationFrame(1, device, time.Now().In(gb26875.DeviceLocation)), gb26875.BuildGB26875ComponentStatusFrame(2, device, 128, 1, 23, 2, 7, 2, "manual alarm", time.Now().In(gb26875.DeviceLocation))} {
		_ = conn.SetDeadline(time.Now().Add(10 * time.Second))
		_, err = conn.Write(frame)
		fail(err)
		ack, err := gb26875.DecodeFrame(read())
		fail(err)
		if ack.Command != 3 {
			fail(fmt.Errorf("expected ACK, got %02X", ack.Command))
		}
		fmt.Printf("ACK sequence=%d\n", ack.Sequence)
	}
	if *hold <= 0 {
		return
	}
	_ = conn.SetDeadline(time.Now().Add(*hold))
	fmt.Printf("online device=gb26875_%s; waiting for commands\n", *source)
	for {
		frame, err := gb26875.DecodeFrame(read())
		fail(err)
		if frame.Command != 1 || len(frame.Data) != 8 || frame.Data[0] != 0x5a {
			fail(fmt.Errorf("unsupported downlink"))
		}
		// Build a device-originated ACK by replacing the frame's two addresses.
		reply := gb26875.BuildGB26875AckFrame(frame.Sequence, [6]byte{255, 255, 255, 255, 255, 255}, time.Now().In(gb26875.DeviceLocation))
		copy(reply[12:18], device[:])
		var sum byte
		for _, v := range reply[2:27] {
			sum += v
		}
		reply[27] = sum
		_, err = conn.Write(reply)
		fail(err)
		fmt.Printf("confirmed time-sync sequence=%d\n", frame.Sequence)
	}
}

func fail(err error) {
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
