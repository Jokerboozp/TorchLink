// The root package accepts both legacy RawMessage input and protocol v2 calls.
package main

import (
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

func main() {
	if err := run(os.Stdin, os.Stdout); err != nil {
		_ = json.NewEncoder(os.Stdout).Encode(map[string]string{"error": err.Error()})
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
