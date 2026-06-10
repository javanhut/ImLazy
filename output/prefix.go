package output

import (
	"bytes"
	"fmt"
	"io"
	"sync"
)

// prefixColors is the palette cycled through for parallel command labels.
var prefixColors = []string{Cyan, Magenta, Yellow, Green, Blue}

// prefixMu serializes writes from concurrent commands so lines never
// interleave mid-line.
var prefixMu sync.Mutex

// PrefixWriter prefixes every line written through it with a colored label,
// foreman-style: "api  | listening on :8080".
type PrefixWriter struct {
	dst    io.Writer
	prefix string
	buf    bytes.Buffer
}

// NewPrefixWriter creates a PrefixWriter with the given (pre-padded) label,
// colored by colorIndex (cycles through the palette).
func NewPrefixWriter(dst io.Writer, label string, colorIndex int) *PrefixWriter {
	color := prefixColors[colorIndex%len(prefixColors)]
	return &PrefixWriter{
		dst:    dst,
		prefix: colorize(color, label+" | "),
	}
}

// Write buffers input and emits complete lines with the prefix attached.
func (p *PrefixWriter) Write(data []byte) (int, error) {
	prefixMu.Lock()
	defer prefixMu.Unlock()

	p.buf.Write(data)
	for {
		line, err := p.buf.ReadString('\n')
		if err != nil {
			// Partial line: keep it buffered for the next write.
			p.buf.WriteString(line)
			break
		}
		if _, werr := fmt.Fprintf(p.dst, "%s%s", p.prefix, line); werr != nil {
			return len(data), werr
		}
	}
	return len(data), nil
}

// Flush writes any buffered partial line (call when the command exits).
func (p *PrefixWriter) Flush() {
	prefixMu.Lock()
	defer prefixMu.Unlock()

	if p.buf.Len() > 0 {
		fmt.Fprintf(p.dst, "%s%s\n", p.prefix, p.buf.String())
		p.buf.Reset()
	}
}
