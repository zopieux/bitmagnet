package main

import (
	"bufio"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/bitmagnet-io/bitmagnet/internal/model"
)

func processHex(raw string) error {
	raw = strings.TrimSpace(raw)
	raw = strings.TrimPrefix(raw, "0x")
	raw = strings.TrimPrefix(raw, "X'")
	raw = strings.TrimPrefix(raw, "x'")
	raw = strings.TrimSuffix(raw, "'")
	if raw == "" {
		return nil
	}

	data, err := hex.DecodeString(raw)
	if err != nil {
		return fmt.Errorf("invalid hex: %w", err)
	}

	summary, err := model.DecodeContentSummary(data)
	if err != nil {
		return fmt.Errorf("failed to decode summary: %w", err)
	}

	formatted, err := json.MarshalIndent(summary, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal json: %w", err)
	}

	fmt.Println(string(formatted))
	return nil
}

func main() {
	if len(os.Args) > 1 {
		for _, arg := range os.Args[1:] {
			if err := processHex(arg); err != nil {
				_, _ = fmt.Fprintf(os.Stderr, "error decoding %q: %s\n", arg, err)
			}
		}
		return
	}

	scanner := bufio.NewScanner(os.Stdin)
	for scanner.Scan() {
		line := scanner.Text()
		if err := processHex(line); err != nil {
			_, _ = fmt.Fprintf(os.Stderr, "error: %s\n", err)
		}
	}
}
