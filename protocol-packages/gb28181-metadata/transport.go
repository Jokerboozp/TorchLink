package gbmetadata

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"io"
	"net"
	"strconv"
	"strings"
	"sync"
	"time"
)

type packet struct {
	data          []byte
	peer, network string
	send          sender
}
type outbound struct {
	data []byte
	send sender
}

// Serve bounds socket connections, packet rate, parse workers and pending writes.
// The same port is used for TCP and UDP, as advertised in outbound SIP Via.
func (r *Registrar) Serve(ctx context.Context) error {
	udp, err := net.ListenPacket("udp", r.cfg.Listen)
	if err != nil {
		return err
	}
	defer udp.Close()
	tcp, err := net.Listen("tcp", r.cfg.Listen)
	if err != nil {
		return err
	}
	defer tcp.Close()
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	go func() { <-ctx.Done(); udp.Close(); tcp.Close() }()
	in := make(chan packet, 128)
	out := make(chan outbound, 128)
	var workers sync.WaitGroup
	for i := 0; i < 8; i++ {
		workers.Add(2)
		go func() {
			defer workers.Done()
			for {
				select {
				case <-ctx.Done():
					return
				case p := <-in:
					r.Handle(p.data, p.peer, p.network, p.send)
				}
			}
		}()
		go func() {
			defer workers.Done()
			for {
				select {
				case <-ctx.Done():
					return
				case p := <-out:
					_ = p.send(p.data)
				}
			}
		}()
	}
	defer func() { cancel(); workers.Wait() }()
	queue := func(write sender) sender {
		return func(data []byte) error {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case out <- outbound{append([]byte(nil), data...), write}:
				return nil
			default:
				return errors.New("SIP write queue full")
			}
		}
	}
	// Reject unknown source networks before parsing or allocating transactions.
	go func() {
		buf := make([]byte, (64<<10)+1)
		window := time.Now()
		count := 0
		for {
			n, peer, err := udp.ReadFrom(buf)
			if err != nil {
				return
			}
			if time.Since(window) >= time.Second {
				window = time.Now()
				count = 0
			}
			count++
			if count > 64 || n > 64<<10 || !r.allowed(peer.String()) {
				continue
			}
			destination := peer
			p := packet{append([]byte(nil), buf[:n]...), peer.String(), "udp", queue(func(data []byte) error { _, err := udp.WriteTo(data, destination); return err })}
			select {
			case in <- p:
			default:
			}
		}
	}()
	connections := make(chan struct{}, 64)
	go r.Run(ctx)
	for {
		conn, err := tcp.Accept()
		if err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			return err
		}
		if !r.allowed(conn.RemoteAddr().String()) {
			conn.Close()
			continue
		}
		select {
		case connections <- struct{}{}:
		default:
			conn.Close()
			continue
		}
		go func(conn net.Conn) {
			defer conn.Close()
			defer func() { <-connections }()
			stop := context.AfterFunc(ctx, func() { conn.Close() })
			defer stop()
			var writeMu sync.Mutex
			send := queue(func(data []byte) error {
				writeMu.Lock()
				defer writeMu.Unlock()
				conn.SetWriteDeadline(time.Now().Add(time.Second))
				for len(data) > 0 {
					n, err := conn.Write(data)
					if err != nil {
						return err
					}
					if n == 0 {
						return io.ErrShortWrite
					}
					data = data[n:]
				}
				return nil
			})
			reader := bufio.NewReaderSize(conn, 4096)
			for {
				conn.SetReadDeadline(time.Now().Add(90 * time.Second))
				data, err := readSIPFrame(reader)
				if err != nil {
					return
				}
				select {
				case in <- packet{data, conn.RemoteAddr().String(), "tcp", send}:
				case <-ctx.Done():
					return
				default:
					return
				}
			}
		}(conn)
	}
}

func readSIPFrame(reader *bufio.Reader) ([]byte, error) {
	var header bytes.Buffer
	length := -1
	for {
		line, err := reader.ReadSlice('\n')
		if err != nil {
			return nil, err
		}
		if !bytes.HasSuffix(line, []byte("\r\n")) {
			return nil, errors.New("SIP headers require CRLF")
		}
		if header.Len() == 0 && bytes.Equal(line, []byte("\r\n")) {
			continue
		}
		header.Write(line)
		if header.Len() > 16<<10 {
			return nil, errors.New("SIP header limit exceeded")
		}
		if bytes.Equal(line, []byte("\r\n")) {
			break
		}
		key, value, ok := strings.Cut(string(line), ":")
		if ok && (strings.EqualFold(strings.TrimSpace(key), "Content-Length") || strings.EqualFold(strings.TrimSpace(key), "l")) {
			if length != -1 {
				return nil, errors.New("duplicate SIP content length")
			}
			length, err = strconv.Atoi(strings.TrimSpace(value))
			if err != nil || length < 0 || length > 48<<10 {
				return nil, errors.New("invalid SIP content length")
			}
		}
	}
	if length < 0 {
		return nil, errors.New("SIP TCP requires content length")
	}
	body := make([]byte, length)
	if _, err := io.ReadFull(reader, body); err != nil {
		return nil, err
	}
	return append(header.Bytes(), body...), nil
}
