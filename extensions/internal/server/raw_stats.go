package server

import (
	"bytes"
	"io"
)

// rawStatsReader observes bounded lines without hiding transport errors or reading ahead.
type rawStatsReader struct {
	io.Reader
	stats    *chatStatsReader
	pending  []byte
	skipping bool
}

func (r *rawStatsReader) Read(p []byte) (int, error) {
	n, err := r.Reader.Read(p)
	for rest := p[:n]; len(rest) > 0; {
		end := bytes.IndexByte(rest, '\n')
		part := rest
		if end >= 0 {
			part = rest[:end+1]
		}
		// Metrics are best effort; protocol validation owns the larger raw frame limit.
		if len(r.pending)+len(part) > 64<<10 {
			r.skipping = true
			r.pending = nil
		}
		if !r.skipping {
			r.pending = append(r.pending, part...)
		}
		if end < 0 {
			break
		}
		if !r.skipping {
			r.stats.parseSSELine(string(r.pending))
		}
		r.pending = r.pending[:0]
		r.skipping = false
		rest = rest[end+1:]
	}
	return n, err
}
