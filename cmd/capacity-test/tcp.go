package main

import (
	"context"
	"encoding/binary"
	"errors"
	"flag"
	"io"
	mrand "math/rand/v2"
	"net"
	"net/http"
	"sync"
	"time"

	"iot-platform/internal/parser"
)

var tcpAddr = flag.String("tcp", "127.0.0.1:26875", "tcp: GB26875 listener address")

// GB26875 devices send BCD times without a zone; the protocol reads them as UTC+08:00.
var gbLocation = time.FixedZone("UTC+8", 8*60*60)

type tcpDevice struct {
	conn       net.Conn
	source     [6]byte
	seq        uint16
	registered bool
}

func readGBFrame(c net.Conn) ([]byte, error) {
	head := make([]byte, 27)
	if _, err := io.ReadFull(c, head); err != nil {
		return nil, err
	}
	size := int(binary.LittleEndian.Uint16(head[24:26]))
	if size > 512 {
		return nil, errors.New("invalid frame length")
	}
	tail := make([]byte, size+3)
	if _, err := io.ReadFull(c, tail); err != nil {
		return nil, err
	}
	return append(head, tail...), nil
}

// tcpMode treats each worker as one device with its own connection: it
// registers once, then sends component status frames and waits for the ACK the
// platform returns after archiving the raw frame. -levels is the connection count.
func tcpMode() {
	var mu sync.Mutex
	devices := map[int]*tcpDevice{}
	save(runLevels(func(ctx context.Context, _ *http.Client, w int) (bool, string, int64) {
		mu.Lock()
		d := devices[w]
		if d == nil {
			d = &tcpDevice{source: [6]byte{0x90, 0x01}}
			binary.BigEndian.PutUint32(d.source[2:], uint32(0x10000000+w))
			devices[w] = d
		}
		mu.Unlock()
		if d.conn == nil {
			c, err := net.DialTimeout("tcp", *tcpAddr, *timeout)
			if err != nil {
				time.Sleep(200 * time.Millisecond)
				return false, shortErr(err.Error()), 0
			}
			d.conn, d.registered = c, false
		}
		d.seq++
		now := time.Now().In(gbLocation)
		frame := parser.BuildGB26875RegistrationFrame(d.seq, d.source, now)
		if d.registered {
			status, desc := uint16(1), "stress normal"
			if mrand.Float64() < *alarmFrac {
				status, desc = 2, "stress fire alarm"
			}
			frame = parser.BuildGB26875ComponentStatusFrame(d.seq, d.source, 1, 1, 23, uint16(w%64), uint16(mrand.IntN(200)), status, desc, now)
		}
		fail := func(prefix string, err error) (bool, string, int64) {
			d.conn.Close()
			d.conn = nil
			if ctx.Err() != nil {
				return false, "timeout", 0
			}
			return false, prefix + shortErr(err.Error()), 0
		}
		deadline, _ := ctx.Deadline()
		_ = d.conn.SetDeadline(deadline)
		if _, err := d.conn.Write(frame); err != nil {
			return fail("write-", err)
		}
		for {
			reply, err := readGBFrame(d.conn)
			if err != nil {
				return fail("read-", err)
			}
			// Command 0x03 is the platform ACK; it echoes the frame sequence.
			if reply[26] == 0x03 && binary.LittleEndian.Uint16(reply[2:4]) == d.seq {
				d.registered = true
				return true, "ack", int64(len(frame))
			}
		}
	}))
	for _, d := range devices {
		if d.conn != nil {
			d.conn.Close()
		}
	}
}
