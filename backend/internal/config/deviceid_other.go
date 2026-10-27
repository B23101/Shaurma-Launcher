//go:build !windows

package config

import "os"

// machineSeed на не-Windows платформах: /etc/machine-id на Linux (стабільний
// ідентифікатор дистрибутива, немає прав не потрібно). На інших ОС (macOS
// білд наразі не постачається) повертає nil — DeviceID тоді ґрунтується
// лише на персистентній солі (менш стійко до перевстановлення ОС, але
// лаунчер основно working на Windows, тож це прийнятний фолбек).
func machineSeed() []byte {
	if data, err := os.ReadFile("/etc/machine-id"); err == nil {
		return data
	}
	return nil
}
