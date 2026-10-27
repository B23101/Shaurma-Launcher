package export

import (
	"crypto/sha1"
	"crypto/sha512"
	"encoding/hex"
	"io"
	"os"
)

// fileHashes — sha1 і sha512 файлу (обидва потрібні для mrpack files[]).
type fileHashes struct {
	Sha1   string
	Sha512 string
	Size   int64
}

// hashFile рахує sha1, sha512 і розмір файлу одним проходом.
func hashFile(path string) (fileHashes, error) {
	f, err := os.Open(path)
	if err != nil {
		return fileHashes{}, err
	}
	defer f.Close()

	h1 := sha1.New()
	h512 := sha512.New()
	size, err := io.Copy(io.MultiWriter(h1, h512), f)
	if err != nil {
		return fileHashes{}, err
	}
	return fileHashes{
		Sha1:   hex.EncodeToString(h1.Sum(nil)),
		Sha512: hex.EncodeToString(h512.Sum(nil)),
		Size:   size,
	}, nil
}
