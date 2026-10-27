//go:build windows

package shaurmaid

import "golang.org/x/sys/windows/registry"

func regString(root registry.Key, path, name string) string {
	k, err := registry.OpenKey(root, path, registry.QUERY_VALUE|registry.WOW64_64KEY)
	if err != nil {
		return ""
	}
	defer k.Close()
	v, _, err := k.GetStringValue(name)
	if err != nil {
		return ""
	}
	return v
}

func collectHWIDParts() map[string]string {
	const bios = `HARDWARE\DESCRIPTION\System\BIOS`
	return map[string]string{
		"machine": regString(registry.LOCAL_MACHINE, `SOFTWARE\Microsoft\Cryptography`, "MachineGuid"),
		"board":   regString(registry.LOCAL_MACHINE, bios, "BaseBoardProduct"),
		"vendor":  regString(registry.LOCAL_MACHINE, bios, "BaseBoardManufacturer"),
		"cpu":     regString(registry.LOCAL_MACHINE, `HARDWARE\DESCRIPTION\System\CentralProcessor\0`, "ProcessorNameString"),
	}
}
