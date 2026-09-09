// Package testserial provides an OS pseudo-terminal Modbus fixture for integration tests.
package testserial

import (
	"bufio"
	"context"
	"os/exec"
	"runtime"
	"testing"
	"time"
)

func Start(t *testing.T) string {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("pseudo-terminal serial simulator requires Unix; Windows hardware test not executed")
	}
	python, err := exec.LookPath("python3")
	if err != nil {
		t.Skip("python3 unavailable; OS serial integration not executed")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cmd := exec.CommandContext(ctx, python, "-u", "-c", `
import os, pty, select, tty
master, slave = pty.openpty()
tty.setraw(slave)
print(os.ttyname(slave), flush=True)
def crc(data):
    v=65535
    for b in data:
        v ^= b
        for _ in range(8): v=(v>>1)^40961 if v&1 else v>>1
    return bytes([v&255,v>>8])
buffer=b''
while True:
    if not select.select([master],[],[],1)[0]: continue
    buffer += os.read(master,256)
    while len(buffer)>=8:
        request,buffer=buffer[:8],buffer[8:]
        if request[-2:] != crc(request[:-2]): continue
        unit,fn=request[:2]
        qty=int.from_bytes(request[4:6],'big')
        if fn not in (3,4) or qty != 1:
            response=bytes([unit,fn|128,2])
        else:
            response=bytes([unit,fn,2,0,42])
        os.write(master,response+crc(response))
`)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		cancel()
		t.Fatal(err)
	}
	if err = cmd.Start(); err != nil {
		cancel()
		t.Fatal(err)
	}
	t.Cleanup(func() { cancel(); _ = cmd.Wait() })
	line := make(chan string, 1)
	go func() {
		scan := bufio.NewScanner(stdout)
		if scan.Scan() {
			line <- scan.Text()
		}
	}()
	select {
	case port := <-line:
		return port
	case <-time.After(5 * time.Second):
		t.Fatal("serial simulator startup timeout")
	}
	return ""
}
