package wsl

import (
	"strings"
	"sync"
	"sync/atomic"
)

// Environment описывает окружение WSL-дистрибутива: оболочку, init-систему,
// пакетный менеджер и способ повышения привилегий. Значения детектируются
// автоматически и могут быть переопределены через config.json.
type Environment struct {
	Shell        string `json:"shell"`         // "bash" | "sh"
	InitSystem   string `json:"init_system"`   // "systemd" | "openrc" | "none"
	PkgManager   string `json:"pkg_manager"`   // "apt" | "apk" | "none"
	PrivilegeCmd string `json:"privilege_cmd"` // "sudo" | "doas" | ""
}

const (
	ShellBash = "bash"
	ShellSh   = "sh"

	InitSystemd = "systemd"
	InitOpenRC  = "openrc"
	InitNone    = "none"

	PkgApt  = "apt"
	PkgApk  = "apk"
	PkgNone = "none"

	PrivSudo = "sudo"
	PrivDoas = "doas"
)

// detectEnvScript — POSIX-совместимый скрипт детекта окружения.
// Запускается через sh (есть в любом дистрибутиве, включая busybox).
const detectEnvScript = `
command -v bash >/dev/null 2>&1 && echo "SHELL:bash" || echo "SHELL:sh"
if command -v systemctl >/dev/null 2>&1; then
	echo "INIT:systemd"
elif command -v rc-service >/dev/null 2>&1; then
	echo "INIT:openrc"
else
	echo "INIT:none"
fi
if command -v apk >/dev/null 2>&1; then
	echo "PKG:apk"
elif command -v apt-get >/dev/null 2>&1; then
	echo "PKG:apt"
else
	echo "PKG:none"
fi
if command -v sudo >/dev/null 2>&1; then
	echo "PRIV:sudo"
elif command -v doas >/dev/null 2>&1; then
	echo "PRIV:doas"
else
	echo "PRIV:none"
fi
`

var (
	envDetected atomic.Bool
	envCached   Environment
	envDetectMu sync.Mutex
)

// DetectEnvironment определяет окружение дистрибутива одним вызовом WSL.
// При ошибке возвращается консервативный дефолт (bash/systemd/apt/sudo) —
// это сохраняет поведение для существующих Debian-инсталляций.
func DetectEnvironment() Environment {
	envDetectMu.Lock()
	defer envDetectMu.Unlock()
	if envDetected.Load() {
		return envCached
	}

	env := Environment{
		Shell:        ShellBash,
		InitSystem:   InitSystemd,
		PkgManager:   PkgApt,
		PrivilegeCmd: PrivSudo,
	}

	out, err := runWSLDirect(ShellSh, detectEnvScript)
	if err == nil {
		for _, line := range strings.Split(out, "\n") {
			line = strings.TrimSpace(line)
			key, value, found := strings.Cut(line, ":")
			if !found {
				continue
			}
			switch key {
			case "SHELL":
				if value == ShellBash || value == ShellSh {
					env.Shell = value
				}
			case "INIT":
				if value == InitSystemd || value == InitOpenRC || value == InitNone {
					env.InitSystem = value
				}
			case "PKG":
				if value == PkgApt || value == PkgApk || value == PkgNone {
					env.PkgManager = value
				}
			case "PRIV":
				if value == PrivSudo || value == PrivDoas {
					env.PrivilegeCmd = value
				} else {
					env.PrivilegeCmd = ""
				}
			}
		}
	}

	envCached = env
	envDetected.Store(true)
	return env
}

// CurrentEnvironment возвращает окружение с учётом переопределений из config.json.
func CurrentEnvironment() Environment {
	env := DetectEnvironment()
	config := GetConfig()
	if config == nil {
		return env
	}
	if config.Shell == ShellBash || config.Shell == ShellSh {
		env.Shell = config.Shell
	}
	switch config.InitSystem {
	case InitSystemd, InitOpenRC, InitNone:
		env.InitSystem = config.InitSystem
	}
	switch config.PkgManager {
	case PkgApt, PkgApk, PkgNone:
		env.PkgManager = config.PkgManager
	}
	switch config.PrivilegeCmd {
	case PrivSudo, PrivDoas:
		env.PrivilegeCmd = config.PrivilegeCmd
	case "":
		// авто-детект
	default:
		env.PrivilegeCmd = ""
	}
	return env
}

// GetShell возвращает оболочку для выполнения команд в WSL.
func GetShell() string {
	return CurrentEnvironment().Shell
}

// PrivilegePrefix возвращает префикс повышения привилегий ("sudo ", "doas " или "").
func PrivilegePrefix() string {
	priv := CurrentEnvironment().PrivilegeCmd
	if priv == "" {
		return ""
	}
	return priv + " "
}

// PrivilegeWrap оборачивает команду в повышение привилегий, если оно доступно.
func PrivilegeWrap(command string) string {
	prefix := PrivilegePrefix()
	if prefix == "" {
		return command
	}
	return prefix + command
}

// IsServiceActiveCommand возвращает POSIX-фрагмент для проверки активности сервиса
// с учётом init-системы. Результат можно встраивать в составные команды.
func IsServiceActiveCommand(service string) string {
	switch CurrentEnvironment().InitSystem {
	case InitOpenRC:
		return "rc-service " + service + " status >/dev/null 2>&1"
	case InitNone:
		return "kill -0 $(cat /var/run/" + service + ".pid 2>/dev/null) 2>/dev/null"
	default:
		return "systemctl is-active " + service + " >/dev/null 2>&1"
	}
}

// IsServiceActive проверяет, активен ли сервис, через соответствующую init-систему.
func IsServiceActive(service string) bool {
	out, err := RunWSL(IsServiceActiveCommand(service) + " && echo ACTIVE || echo INACTIVE")
	return err == nil && strings.Contains(out, "ACTIVE")
}

// StartServiceCommand возвращает команду запуска сервиса для текущей init-системы.
func StartServiceCommand(service string) string {
	priv := PrivilegePrefix()
	switch CurrentEnvironment().InitSystem {
	case InitOpenRC:
		return priv + "rc-service " + service + " start"
	case InitNone:
		return priv + "containerd >/dev/null 2>&1 &"
	default:
		return priv + "systemctl start " + service
	}
}

// StartService запускает сервис через соответствующую init-систему.
func StartService(service string) error {
	_, err := RunWSL(StartServiceCommand(service))
	return err
}

// PkgInstallCommand возвращает команду установки пакетов для текущего дистрибутива.
func PkgInstallCommand(packages ...string) string {
	priv := PrivilegePrefix()
	joined := strings.Join(packages, " ")
	switch CurrentEnvironment().PkgManager {
	case PkgApk:
		return priv + "apk add --no-cache " + joined
	case PkgApt:
		return priv + "apt update && " + priv + "apt install -y " + joined
	default:
		return ""
	}
}
