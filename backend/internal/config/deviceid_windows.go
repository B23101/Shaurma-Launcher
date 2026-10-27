//go:build windows

package config

import "golang.org/x/sys/windows/registry"

// machineSeed повертає MachineGuid з реєстру (HKLM\SOFTWARE\Microsoft\
// Cryptography) — стабільний ідентифікатор інсталяції Windows, що
// переживає перевстановлення застосунків (лишається доти, доки не
// перевстановлено саму Windows). Читання не потребує прав адміністратора.
func machineSeed() []byte {
	k, err := registry.OpenKey(registry.LOCAL_MACHINE, `SOFTWARE\Microsoft\Cryptography`, registry.QUERY_VALUE|registry.WOW64_64KEY)
	if err != nil {
		return nil
	}
	defer k.Close()
	guid, _, err := k.GetStringValue("MachineGuid")
	if err != nil {
		return nil
	}
	return []byte(guid)
}
