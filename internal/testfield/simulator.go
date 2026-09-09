package testfield

import (
	"bufio"
	"context"
	"encoding/json"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

type Fixture struct {
	BACnetPort  int    `json:"bacnetPort"`
	OPCPort     int    `json:"opcPort"`
	SNMPPort    int    `json:"snmpPort"`
	NodeID      string `json:"nodeId"`
	Certificate string `json:"certificateFile"`
	PrivateKey  string `json:"privateKeyFile"`
	SHA256      string `json:"serverSha256"`
}

func Start(t *testing.T) Fixture {
	t.Helper()
	python := os.Getenv("IOT_TEST_FIELD_PYTHON")
	if python == "" {
		t.Skip("field protocol simulator not configured; authenticated servers not tested")
	}
	ctx, cancel := context.WithCancel(context.Background())
	command := exec.CommandContext(ctx, python, filepath.Join("..", "..", "scripts", "tests", "field-protocol-simulator.py"), t.TempDir())
	stdout, err := command.StdoutPipe()
	if err != nil {
		cancel()
		t.Fatal(err)
	}
	command.Stderr = os.Stderr
	if err = command.Start(); err != nil {
		cancel()
		t.Fatal(err)
	}
	t.Cleanup(func() { cancel(); _ = command.Wait() })
	line := make(chan []byte, 1)
	go func() {
		scan := bufio.NewScanner(stdout)
		if scan.Scan() {
			line <- append([]byte(nil), scan.Bytes()...)
		}
	}()
	var fixture Fixture
	select {
	case data := <-line:
		if err = json.Unmarshal(data, &fixture); err != nil {
			t.Fatal(err)
		}
	case <-time.After(15 * time.Second):
		t.Fatal("protocol simulator startup deadline")
	}
	go io.Copy(io.Discard, stdout)
	return fixture
}
