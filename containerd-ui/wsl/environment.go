package wsl

import (
	"fmt"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
)

// Environment описывает окружение WSL-дистрибутива: оболочку, init-систему,
// пакетный менеджер и способ повышения привилегий. Значения детектируются
// автоматически и могут быть переопределены через config.json.
type Environment struct {
	Shell        string `json:"shell"`         // "bash" | "sh"
	InitSystem   string `json:"init_system"`   // "openrc" | "none"
	PkgManager   string `json:"pkg_manager"`   // "apk" | "none"
	PrivilegeCmd string `json:"privilege_cmd"` // "sudo" | "doas" | ""
}

const (
	ShellBash = "bash"
	ShellSh   = "sh"

	InitOpenRC = "openrc"
	InitNone   = "none"

	PkgApk  = "apk"
	PkgNone = "none"

	PrivSudo = "sudo"
	PrivDoas = "doas"
)

const (
	MinimumAlpineNerdctlVersion  = "2.4.0"
	MinimumAlpineBuildkitVersion = "0.33.0"
)

// detectEnvScript — POSIX-совместимый скрипт детекта окружения.
// Запускается через sh (есть в любом дистрибутиве, включая busybox).
const detectEnvScript = `
command -v bash >/dev/null 2>&1 && echo "SHELL:bash" || echo "SHELL:sh"
if command -v rc-service >/dev/null 2>&1; then
	echo "INIT:openrc"
else
	echo "INIT:none"
fi
if command -v apk >/dev/null 2>&1; then
	echo "PKG:apk"
else
	echo "PKG:none"
fi
if command -v doas >/dev/null 2>&1; then
	echo "PRIV:doas"
else
	echo "PRIV:none"
fi
`

var (
	envDetected     atomic.Bool
	envCached       Environment
	envCachedDistro string
	envDetectMu     sync.Mutex
)

// DetectEnvironment определяет окружение дистрибутива одним вызовом WSL.
// При ошибке возвращаются консервативные defaults Alpine/OpenRC.
func DetectEnvironment() Environment {
	distro := GetWslDistro()
	envDetectMu.Lock()
	defer envDetectMu.Unlock()
	if envDetected.Load() && envCachedDistro == distro {
		return envCached
	}

	env := Environment{Shell: ShellSh, InitSystem: InitOpenRC, PkgManager: PkgApk}

	out, err := runWSLDirect(ShellSh, detectEnvScript)
	if err == nil {
		env = parseDetectedEnvironment(out, env)
	}

	envCached = env
	envCachedDistro = distro
	envDetected.Store(true)
	return env
}

func parseDetectedEnvironment(out string, env Environment) Environment {
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
			if value == InitOpenRC || value == InitNone {
				env.InitSystem = value
			}
		case "PKG":
			if value == PkgApk || value == PkgNone {
				env.PkgManager = value
			}
		case "PRIV":
			if value == PrivDoas {
				env.PrivilegeCmd = value
			} else {
				env.PrivilegeCmd = ""
			}
		}
	}
	return env
}

func InvalidateEnvironmentCache() {
	envDetectMu.Lock()
	envCached = Environment{}
	envCachedDistro = ""
	envDetected.Store(false)
	envDetectMu.Unlock()
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
	case InitOpenRC, InitNone:
		env.InitSystem = config.InitSystem
	}
	switch config.PkgManager {
	case PkgApk, PkgNone:
		env.PkgManager = config.PkgManager
	}
	switch config.PrivilegeCmd {
	case PrivDoas:
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

func PrivilegePrefixNonInteractive() string {
	priv := CurrentEnvironment().PrivilegeCmd
	switch priv {
	case PrivSudo:
		return "sudo -n "
	case PrivDoas:
		return "doas -n "
	default:
		return ""
	}
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
	return serviceActiveCommand(service, CurrentEnvironment().InitSystem)
}

func serviceActiveCommand(service, initSystem string) string {
	switch initSystem {
	case InitOpenRC:
		return "rc-service " + service + " status >/dev/null 2>&1"
	case InitNone:
		return "kill -0 $(cat /var/run/" + service + ".pid 2>/dev/null) 2>/dev/null"
	default:
		return "rc-service " + service + " status >/dev/null 2>&1"
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
		return priv + "rc-service " + service + " start"
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
	default:
		return ""
	}
}

func BuildRuntimeInstallCommand(env Environment) (string, error) {
	privilegePrefix := ""
	switch env.PrivilegeCmd {
	case PrivDoas:
		privilegePrefix = "doas -n "
	case "":
	default:
		return "", fmt.Errorf("неподдерживаемая команда повышения привилегий: %s", env.PrivilegeCmd)
	}

	if env.PkgManager != PkgApk {
		return "", fmt.Errorf("неподдерживаемый пакетный менеджер: %s", env.PkgManager)
	}
	packageCommand := "apk update && apk add --no-cache bash ca-certificates containerd containerd-openrc nerdctl buildkit " + CNIPluginPackageForManager(env.PkgManager) + " curl doas findutils jq logrotate procps-ng socat tzdata"

	switch env.InitSystem {
	case InitOpenRC:
		installScript := strings.Join([]string{
			"set -e",
			packageCommand,
			AlpineDoasSetupCommand(),
			AlpineToolchainUpgradeCommand(),
			AlpineContainerdConfigCommand(),
			AlpineContainerdUIProxySetupCommand(),
			AlpineWSLBootCommand(),
			"rc-update add containerd default",
			"rc-service containerd start",
			"rc-update add containerd-ui-grpc-proxy default",
			"rc-service containerd-ui-grpc-proxy start",
		}, "\n")
		if privilegePrefix != "" {
			return privilegePrefix + "sh -c " + ShellQuote(installScript), nil
		}
		return installScript, nil
	default:
		return "", fmt.Errorf("неподдерживаемая init-система: %s", env.InitSystem)
	}
}

func AlpineContainerdConfigCommand() string {
	return `set -e
config=/etc/containerd/config.toml
mkdir -p /etc/containerd
if [ ! -s "$config" ]; then
	containerd config default > "$config"
	fi
	tmp="$(mktemp)"
	awk '
function flush_section() {
	if (in_grpc && !seen_grpc_address) print "    address = \"/run/containerd/containerd.sock\""
	if (in_tcp && !seen_tcp_address) print "    address = \"\""
}
{
	lower = tolower($0)
	if (lower ~ /^[[:space:]]*\[/) {
		flush_section()
		in_grpc = (index(lower, "io.containerd.server.v1.grpc") > 0 && index(lower, "grpc-tcp") == 0)
		in_legacy_grpc = (lower ~ /^[[:space:]]*\[grpc\][[:space:]]*$/)
		in_tcp = (index(lower, "io.containerd.server.v1.grpc-tcp") > 0)
		if (in_grpc || in_legacy_grpc) seen_grpc = 1
		if (in_tcp) seen_tcp = 1
		if (in_grpc) { print "  [plugins.\"io.containerd.server.v1.grpc\"]"; next }
		if (in_tcp) { print "  [plugins.\"io.containerd.server.v1.grpc-tcp\"]"; next }
		print
		next
	}
	if ((in_grpc || in_legacy_grpc) && lower ~ /^[[:space:]]*address[[:space:]]*=/) {
		if (!seen_grpc_address) print "    address = \"/run/containerd/containerd.sock\""
		seen_grpc_address = 1
		next
	}
	if (in_tcp && lower ~ /^[[:space:]]*address[[:space:]]*=/) {
		if (!seen_tcp_address) print "    address = \"\""
		seen_tcp_address = 1
		next
	}
	print
}
END {
	flush_section()
	if (!seen_grpc) print "\n[plugins.\"io.containerd.server.v1.grpc\"]\n    address = \"/run/containerd/containerd.sock\""
	if (!seen_tcp) print "\n[plugins.\"io.containerd.server.v1.grpc-tcp\"]\n    address = \"\""
}' "$config" > "$tmp"
	mv "$tmp" "$config"
`
}

func AlpineContainerdUIProxySetupCommand() string {
	return `set -e
cat > /etc/init.d/containerd-ui-grpc-proxy <<'RC'
#!/sbin/openrc-run
description="Containerd UI gRPC TCP proxy"
command="/usr/bin/socat"
command_args="TCP-LISTEN:50051,bind=0.0.0.0,reuseaddr,fork UNIX-CONNECT:/run/containerd/containerd.sock"
command_background=yes
pidfile="/run/containerd-ui-grpc-proxy.pid"

depend() {
	need containerd
	use net
}
RC
chmod 0755 /etc/init.d/containerd-ui-grpc-proxy`
}

func AlpineWSLBootCommand() string {
	return `set -e
config=/etc/wsl.conf
boot_command='mkdir -p /run/openrc; touch /run/openrc/softlevel; rc-service containerd start; rc-service containerd-ui-grpc-proxy start'
proxy_command='rc-service containerd-ui-grpc-proxy start'
tmp="$(mktemp)"
if [ -f "$config" ]; then
	awk -v boot_command="$boot_command" -v proxy_command="$proxy_command" '
function flush_boot() {
	if (in_boot && !has_command) print "command = " boot_command
}
{
	section = tolower($0)
	if (section ~ /^[[:space:]]*\[/) {
		flush_boot()
		in_boot = (section ~ /^[[:space:]]*\[boot\][[:space:]]*$/)
		if (in_boot) has_boot = 1
		print
		next
	}
	if (in_boot && tolower($0) ~ /^[[:space:]]*command[[:space:]]*=/) {
		existing = $0
		sub(/^[^=]*=[[:space:]]*/, "", existing)
		if (index(existing, proxy_command) == 0) existing = existing "; " proxy_command
		print "command = " existing
		has_command = 1
		next
	}
	print
}
END {
	flush_boot()
	if (!has_boot) print "\n[boot]\ncommand = " boot_command
}' "$config" > "$tmp"
else
	printf '%s\n' '[boot]' "command = $boot_command" > "$tmp"
fi
mv "$tmp" "$config"
touch /run/openrc/softlevel`
}

func AlpineToolchainVersionsSupported(nerdctlVersion, buildkitVersion string) bool {
	return versionAtLeast(nerdctlVersion, MinimumAlpineNerdctlVersion) &&
		versionAtLeast(buildkitVersion, MinimumAlpineBuildkitVersion)
}

func versionAtLeast(actual, minimum string) bool {
	parse := func(value string) []int {
		start := strings.IndexFunc(value, func(char rune) bool { return char >= '0' && char <= '9' })
		if start < 0 {
			return nil
		}
		value = value[start:]
		end := strings.IndexFunc(value, func(char rune) bool { return (char < '0' || char > '9') && char != '.' })
		if end >= 0 {
			value = value[:end]
		}
		parts := strings.Split(value, ".")
		parsed := make([]int, 3)
		for index := 0; index < len(parts) && index < len(parsed); index++ {
			parsed[index], _ = strconv.Atoi(parts[index])
		}
		return parsed
	}

	actualParts, minimumParts := parse(actual), parse(minimum)
	if actualParts == nil || minimumParts == nil {
		return false
	}
	for index := range actualParts {
		if actualParts[index] != minimumParts[index] {
			return actualParts[index] > minimumParts[index]
		}
	}
	return true
}

func AlpineToolchainUpgradeCommand() string {
	command := `set -e
export PATH="/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin:$PATH"
case "$(uname -m)" in
	x86_64) arch=amd64 ;;
	aarch64) arch=arm64 ;;
 	*) echo "Unsupported Alpine architecture: $(uname -m)" >&2; exit 1 ;;
esac
version_lt() {
	awk -v actual="$1" -v required="$2" 'BEGIN {
		gsub(/^[^0-9]*/, "", actual)
		split(actual, a, /[.]/); split(required, r, /[.]/)
		for (i = 1; i <= 3; i++) {
			if ((a[i] + 0) < (r[i] + 0)) exit 0;
			if ((a[i] + 0) > (r[i] + 0)) exit 1;
		}
		exit 1
	}'
}
tmpdir="$(mktemp -d)"
trap 'rm -rf "$tmpdir"' EXIT
upgrade_tool() {
	tool="$1"; repo="$2"; minimum="$3"; asset_kind="$4"
	current="$("$tool" --version 2>/dev/null | grep -Eo 'v?[0-9]+([.][0-9]+){2}' | head -n 1)"
	if ! version_lt "$current" "$minimum"; then return 0; fi
	release="$(curl -fsSL "https://api.github.com/repos/$repo/releases/latest")"
	tag="$(printf '%s' "$release" | jq -er '.tag_name')"
	if [ "$asset_kind" = nerdctl ]; then
		asset="nerdctl-${tag#v}-linux-${arch}.tar.gz"
	else
		asset="buildkit-${tag}.linux-${arch}.tar.gz"
	fi
	asset_data="$(printf '%s' "$release" | jq -er --arg name "$asset" '.assets[] | select(.name == $name) | [.browser_download_url, .digest] | @tsv')"
	url="$(printf '%s' "$asset_data" | cut -f1)"
	digest="$(printf '%s' "$asset_data" | cut -f2)"
	case "$digest" in sha256:*) digest="${digest#sha256:}" ;; *) echo "Missing SHA-256 for $asset" >&2; return 1 ;; esac
	archive="$tmpdir/$asset"
	curl -fsSL "$url" -o "$archive"
	printf '%s  %s\n' "$digest" "$archive" | sha256sum -c -
	if [ "$asset_kind" = nerdctl ]; then
		tar -xzf "$archive" -C /usr/local/bin nerdctl
	else
		tar -xzf "$archive" -C /usr/local bin/buildctl bin/buildkitd
	fi
	"$tool" --version
}
upgrade_tool nerdctl containerd/nerdctl @NERDCTL_MIN@ nerdctl
upgrade_tool buildctl moby/buildkit @BUILDKIT_MIN@ buildkit`
	return strings.NewReplacer(
		"@NERDCTL_MIN@", MinimumAlpineNerdctlVersion,
		"@BUILDKIT_MIN@", MinimumAlpineBuildkitVersion,
	).Replace(command)
}

func AlpineDoasSetupCommand() string {
	return `default_user="$(awk -F= '
/^\[user\][[:space:]]*$/ { in_user=1; next }
/^\[/ { in_user=0 }
in_user && $1 ~ /^[[:space:]]*default[[:space:]]*$/ { gsub(/[[:space:]]/, "", $2); print $2; exit }
' /etc/wsl.conf 2>/dev/null)"
if [ -n "$default_user" ] && [ "$default_user" != root ]; then
	addgroup "$default_user" wheel 2>/dev/null || true
fi
touch /etc/doas.conf
grep -q '^permit nopass :wheel$' /etc/doas.conf || printf '%s\n' 'permit nopass :wheel' >> /etc/doas.conf
grep -q '^permit nopass root$' /etc/doas.conf || printf '%s\n' 'permit nopass root' >> /etc/doas.conf
chmod 0400 /etc/doas.conf`
}

func CNIPluginPackage() string {
	return CNIPluginPackageForManager(CurrentEnvironment().PkgManager)
}

func CNIPluginPackageForManager(packageManager string) string {
	switch packageManager {
	case PkgApk:
		return "cni-plugins"
	default:
		return ""
	}
}

func CNIPluginInstallCommand(env Environment, privilegePrefix string) (string, error) {
	return cniPluginInstallCommand(env, CNIPluginPackageForManager(env.PkgManager), privilegePrefix)
}

func cniPluginInstallCommand(env Environment, packageName, privilegePrefix string) (string, error) {
	if packageName == "" {
		return "", fmt.Errorf("неизвестный пакетный менеджер для CNI-плагинов: %s", env.PkgManager)
	}

	switch env.PkgManager {
	case PkgApk:
		return privilegePrefix + "apk add --no-cache " + packageName, nil
	default:
		return "", fmt.Errorf("неизвестный пакетный менеджер для CNI-плагинов: %s", env.PkgManager)
	}
}
