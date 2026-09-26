// The root package accepts both legacy RawMessage input and protocol v2 calls.
package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"

	"gb26875-dahua/gb26875"
)

func run(input io.Reader, output io.Writer) error {
	var encoded json.RawMessage
	if err := json.NewDecoder(input).Decode(&encoded); err != nil {
		return fmt.Errorf("decode worker input: %w", err)
	}
	var request gb26875.Request
	if err := json.Unmarshal(encoded, &request); err != nil {
		return fmt.Errorf("decode worker request: %w", err)
	}
	if request.Version == 0 && request.Operation == "" {
		var raw gb26875.RawMessage
		if err := json.Unmarshal(encoded, &raw); err != nil {
			return err
		}
		request = gb26875.Request{Version: 2, Operation: "decode", Raw: &raw}
	}
	return json.NewEncoder(output).Encode(gb26875.Handle(request))
}

// serve handles newline-delimited requests from one resident process and echoes
// each requestId; the platform enables it with IOT_PROTOCOL_WORKER_MODE=serve.
func serve(input io.Reader, output io.Writer) error {
	scanner := bufio.NewScanner(input)
	scanner.Buffer(make([]byte, 64<<10), 4<<20)
	for scanner.Scan() {
		var id struct {
			RequestID string `json:"requestId"`
		}
		_ = json.Unmarshal(scanner.Bytes(), &id)
		var response bytes.Buffer
		if err := run(bytes.NewReader(scanner.Bytes()), &response); err != nil {
			response.Reset()
			_ = json.NewEncoder(&response).Encode(map[string]string{"error": err.Error()})
		}
		// Prepend requestId without re-encoding, so numbers keep their exact form.
		body := bytes.TrimSpace(response.Bytes())
		requestID, _ := json.Marshal(id.RequestID)
		line := append([]byte(`{"requestId":`), requestID...)
		if len(body) > 2 {
			line = append(line, ',')
		}
		line = append(append(line, body[1:]...), '\n')
		if _, err := output.Write(line); err != nil {
			return err
		}
	}
	return scanner.Err()
}

func main() {
	if os.Getenv("IOT_PROTOCOL_WORKER_MODE") == "serve" {
		if err := serve(os.Stdin, os.Stdout); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		return
	}
	if err := run(os.Stdin, os.Stdout); err != nil {
		_ = json.NewEncoder(os.Stdout).Encode(map[string]string{"error": err.Error()})
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
