//go:build !windows

package recorder

import "time"

// fileCreationTime на не-Windows платформах недоступна без statx:
// возвращаем false, чтобы использовать номинальную привязку по имени.
func fileCreationTime(_ string) (time.Time, bool) {
	return time.Time{}, false
}