package assets

import (
	"bytes"
	_ "embed"
	"fmt"
)

//go:embed registry.yaml
var embeddedYAML []byte

// EmbeddedYAML returns the raw bytes of the embedded registry.
func EmbeddedYAML() []byte {
	out := make([]byte, len(embeddedYAML))
	copy(out, embeddedYAML)
	return out
}

func mustEmbed() Registry {
	r, err := Parse(bytes.NewReader(embeddedYAML))
	if err != nil {
		panic(fmt.Errorf("permafrost: embedded asset registry is invalid: %w", err))
	}
	return r
}
