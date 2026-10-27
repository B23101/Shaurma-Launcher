// Package shortcuts — підтримка ярликів збірок на робочому столі Windows
// (аналог Modrinth App: «Create desktop shortcut»).
//
// Тут лише ЧИСТІ допоміжні функції без cgo і без Windows API — їх можна
// тестувати на будь-якій платформі. Сама реалізація ярликів (Desktop-тека,
// .lnk) живе в backend package main (shortcuts.go / shortcuts_windows.go).
package shortcuts

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
)

// PNGToICO загортає PNG-дані в ICO-файл, який Windows розуміє як іконку.
//
// Оскільки Windows Vista+ підтримує PNG-стиснуті записи всередині ICO,
// достатньо обгорнути PNG у контейнер ICONDIR + ICONDIRENTRY — жодної
// перекодування пікселів не потрібно. Це стандартний трюк, який
// використовують багато застосунків для генерації .ico з PNG «на льоту».
//
// Розмір PNG не обмежуємо (збірки мають іконки 256×256), але санітизуємо:
// > 4 МБ не буває у здорових іконок — відкидаємо, щоб не писати мусор.
func PNGToICO(png []byte) ([]byte, error) {
	if len(png) == 0 {
		return nil, errors.New("PNGToICO: порожні PNG дані")
	}
	if len(png) > 4*1024*1024 {
		return nil, fmt.Errorf("PNGToICO: PNG завеликий (%d байт)", len(png))
	}

	// ICONDIR (6 байт): reserved=0, type=1 (icon), count=1.
	var out bytes.Buffer
	out.Write([]byte{0, 0, 1, 0, 1, 0})

	// ICONDIRENTRY (16 байт):
	//   width/height: 0 = 256 (максимальний розмір в ICO-полі),
	//   colorCount=0, reserved=0, planes=1, bitCount=32,
	//   bytesInRes = len(png), imageOffset = 6 + 16 = 22.
	entry := make([]byte, 16)
	entry[0] = 0                                 // width 0 => 256
	entry[1] = 0                                 // height 0 => 256
	entry[2] = 0                                 // colorCount
	entry[3] = 0                                 // reserved
	binary.LittleEndian.PutUint16(entry[4:], 1)  // planes
	binary.LittleEndian.PutUint16(entry[6:], 32) // bitCount
	binary.LittleEndian.PutUint32(entry[8:], uint32(len(png)))
	binary.LittleEndian.PutUint32(entry[12:], 22) // offset до PNG
	out.Write(entry)
	out.Write(png)

	return out.Bytes(), nil
}
