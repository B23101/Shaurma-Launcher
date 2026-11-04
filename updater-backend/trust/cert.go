package trust

import (
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"time"
)

// CertFormat — версія формату master.cert (§3.1).
const CertFormat = 1

// CertMagic — поле-маркер файлу сертифіката: за ним (а не за ім'ям файлу)
// setup і лаунчер впізнають сертифікат («master (1).cert» теж підійде).
const CertMagic = "shaurma_cert"

// MaxCertSize — сертифікат крихітний; усе більше відкидається без розбору.
const MaxCertSize = 16 << 10

// CertEnvelope — вміст файлу master.cert:
//
//	{"shaurma_cert":1,"payload":"<base64 JSON>","sig":"<base64>"}
//
// Підпис root над ТОЧНИМИ байтами payload — однаково перевіряється в Go і JS.
type CertEnvelope struct {
	Magic   int    `json:"shaurma_cert"`
	Payload string `json:"payload"`
	Sig     string `json:"sig"`
}

// Cert — розібраний payload сертифіката.
type Cert struct {
	V        int       `json:"v"`
	Epoch    int       `json:"epoch"`
	IssuedAt time.Time `json:"issued_at"`
	Note     string    `json:"note,omitempty"`
	Alg      string    `json:"alg"`
	KID      string    `json:"kid"`
}

// ErrNotCert — файл не є сертифікатом ШАУРМА (чужий .cert, сміття).
var ErrNotCert = errors.New("це не сертифікат ШАУРМА")

// ParseCert перевіряє маркер, підпис root і формат. Файл, що не пройшов,
// ніколи не відправляється на сервер (§3.2).
func ParseCert(rootKeys []ed25519.PublicKey, data []byte) (*Cert, error) {
	if len(data) == 0 || len(data) > MaxCertSize {
		return nil, ErrNotCert
	}
	var env CertEnvelope
	if err := json.Unmarshal(trimBOM(data), &env); err != nil || env.Magic != CertFormat || env.Payload == "" {
		return nil, ErrNotCert
	}
	payload, err := base64.StdEncoding.DecodeString(env.Payload)
	if err != nil {
		return nil, ErrNotCert
	}
	if err := Verify(rootKeys, ContextCert, payload, env.Sig); err != nil {
		return nil, fmt.Errorf("сертифікат: %w", err)
	}
	var c Cert
	if err := json.Unmarshal(payload, &c); err != nil {
		return nil, ErrNotCert
	}
	if c.V != CertFormat || c.Alg != AlgEd25519 || c.Epoch < 1 {
		return nil, fmt.Errorf("сертифікат: невідомий формат")
	}
	return &c, nil
}

// IssueCert — створення сертифіката (лише інструментом розробника з root).
func IssueCert(rootPriv ed25519.PrivateKey, c Cert) ([]byte, error) {
	c.V = CertFormat
	c.Alg = AlgEd25519
	c.KID = KeyID(rootPriv.Public().(ed25519.PublicKey))
	if c.Epoch < 1 {
		return nil, errors.New("epoch має бути >= 1")
	}
	payload, err := json.Marshal(c)
	if err != nil {
		return nil, err
	}
	env := CertEnvelope{
		Magic:   CertFormat,
		Payload: base64.StdEncoding.EncodeToString(payload),
		Sig:     Sign(rootPriv, ContextCert, payload),
	}
	return json.MarshalIndent(env, "", "  ")
}
