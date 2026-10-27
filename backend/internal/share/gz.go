package share

import (
	"compress/gzip"
	"io"
)

func gzipReader(r io.Reader) (io.Reader, error) { return gzip.NewReader(r) }
