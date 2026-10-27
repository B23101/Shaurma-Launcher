package p2p

import "crypto/rand"

// cryptoRandReader — тонка обгортка над crypto/rand.Reader, щоб host.go
// не тягнув окремий імпорт лише заради одного io.ReadFull виклику.
type cryptoRandReader struct{}

func (cryptoRandReader) Read(p []byte) (int, error) { return rand.Read(p) }
