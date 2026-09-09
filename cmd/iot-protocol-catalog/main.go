// iot-protocol-catalog signs an existing catalog payload for controlled publishing.
package main

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"iot-platform/internal/protocolcatalog"
	"os"
	"runtime"
	"time"
)

func read(path string, limit int64) ([]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	b, err := io.ReadAll(io.LimitReader(f, limit+1))
	if err != nil || int64(len(b)) > limit {
		return nil, errors.New("input exceeds size limit")
	}
	return b, nil
}
func exclusive(path string, data []byte, mode os.FileMode) error {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, mode)
	if err != nil {
		return err
	}
	if _, err = f.Write(data); err != nil {
		f.Close()
		return err
	}
	if err = f.Sync(); err != nil {
		f.Close()
		return err
	}
	return f.Close()
}
func run() error {
	generate := flag.Bool("generate-key", false, "创建新的本机签名密钥；不覆盖现有文件")
	keyPath := flag.String("key", "", "私有签名文件路径")
	keyID := flag.String("key-id", "", "目录签名公钥标识")
	input := flag.String("input", "", "待签名目录 JSON")
	output := flag.String("output", "", "输出目录文件，不覆盖现有文件")
	flag.Parse()
	if *keyPath == "" {
		return errors.New("--key is required")
	}
	if *generate {
		pub, priv, err := ed25519.GenerateKey(rand.Reader)
		if err != nil {
			return err
		}
		if err = exclusive(*keyPath, []byte(base64.StdEncoding.EncodeToString(priv)), 0600); err != nil {
			return err
		}
		fmt.Println("publicKey=" + base64.StdEncoding.EncodeToString(pub))
		return nil
	}
	info, err := os.Stat(*keyPath)
	if err != nil || !info.Mode().IsRegular() || (runtime.GOOS != "windows" && info.Mode().Perm()&0077 != 0) {
		return errors.New("signing key requires a private regular file")
	}
	raw, err := read(*keyPath, 4096)
	if err != nil {
		return err
	}
	private, err := base64.StdEncoding.DecodeString(string(raw))
	if err != nil || len(private) != ed25519.PrivateKeySize {
		return errors.New("invalid private signing key")
	}
	payload, err := read(*input, 6<<20)
	if err != nil {
		return err
	}
	var catalog protocolcatalog.Payload
	if json.Unmarshal(payload, &catalog) != nil {
		return errors.New("invalid catalog JSON")
	}
	if err := protocolcatalog.Validate(catalog, time.Now()); err != nil {
		return err
	}
	if *keyID == "" || len(*keyID) > 128 {
		return errors.New("--key-id is required within 128 bytes")
	}
	envelope := protocolcatalog.Envelope{KeyID: *keyID, Payload: base64.StdEncoding.EncodeToString(payload), Signature: base64.StdEncoding.EncodeToString(ed25519.Sign(private, payload))}
	data, err := json.Marshal(envelope)
	if err != nil {
		return err
	}
	return exclusive(*output, data, 0644)
}
func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
