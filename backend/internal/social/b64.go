package social

import "encoding/base64"

func b64URLDecode(s string) ([]byte, error) {
	return base64.RawURLEncoding.DecodeString(trimPad(s))
}

func trimPad(s string) string {
	for len(s) > 0 && s[len(s)-1] == '=' {
		s = s[:len(s)-1]
	}
	return s
}

// b64StdEncode — СТАНДАРТНИЙ base64 (з padding). Саме його приймає Worker
// (isLikelyBase64 = /^[A-Za-z0-9+/]+=*$/), на відміну від base64url для JWT.
func b64StdEncode(b []byte) string { return base64.StdEncoding.EncodeToString(b) }

// b64StdDecodeExact декодує стандартний base64 і вимагає рівно n байтів.
func b64StdDecodeExact(s string, n int) ([]byte, error) {
	raw, err := base64.StdEncoding.DecodeString(s)
	if err != nil || len(raw) != n {
		return nil, ErrCryptoInvalidKey
	}
	return raw, nil
}
