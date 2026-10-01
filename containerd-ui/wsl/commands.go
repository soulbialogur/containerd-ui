package wsl

import (
	"bufio"
	"bytes"
	"containerd-ui/i18n"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os/exec"
	"path"
	"sort"
	"strings"
	"sync"
	"syscall"
	"time"
	"unicode/utf16"
	"unicode/utf8"

	"golang.org/x/text/encoding/charmap"
)

type Container struct {
	ID     string `json:"ID"`
	Name   string `json:"Names"`
	Image  string `json:"Image"`
	Status string `json:"Status"`
	Ports  string `json:"Ports"`
}

type Network struct {
	Name       string   `json:"Name"`
	Driver     string   `json:"Driver"`
	Scope      string   `json:"Scope"`
	Containers []string `json:"Containers"`
}

// ShellQuote экранирует строку для sh. Экспортирован для внешних пакетов.
func ShellQuote(value string) string { return shellQuote(value) }

func shellQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "'\"'\"'") + "'"
}

func cleanWSLUserOutput(output string) string {
	lines := strings.Split(output, "\n")
	filtered := make([]string, 0, len(lines))
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		lower := strings.ToLower(trimmed)
		if strings.Contains(lower, "wsl:") || strings.Contains(lower, "wsl.exe:") {
			continue
		}
		filtered = append(filtered, line)
	}
	return strings.TrimSpace(strings.Join(filtered, "\n"))
}

func isInternalToolLog(line string) bool {
	return strings.HasPrefix(line, "time=\"") &&
		strings.Contains(line, " level=") &&
		strings.Contains(line, " msg=")
}

// CleanWSLUserOutput removes host-level WSL diagnostics from text shown in UI.
func CleanWSLUserOutput(output string) string { return cleanWSLUserOutput(output) }

// CleanCleanupOutput removes WSL diagnostics and internal tool logs from cleanup results.
func CleanCleanupOutput(output string) string {
	lines := strings.Split(cleanWSLUserOutput(output), "\n")
	filtered := make([]string, 0, len(lines))
	for _, line := range lines {
		if !isInternalToolLog(strings.ToLower(strings.TrimSpace(line))) {
			filtered = append(filtered, line)
		}
	}
	return strings.TrimSpace(strings.Join(filtered, "\n"))
}

func filterWSLDiagnosticBytes(raw []byte) []byte {
	lines := bytes.Split(raw, []byte{'\n'})
	filtered := make([][]byte, 0, len(lines))
	for _, line := range lines {
		if !isWSLDiagnosticBytes(line) {
			filtered = append(filtered, line)
		}
	}
	return bytes.Join(filtered, []byte{'\n'})
}

func isWSLDiagnosticBytes(line []byte) bool {
	line = bytes.TrimSpace(bytes.Trim(line, "\x00"))
	if len(line) >= 2 && line[0] == 0xFF && line[1] == 0xFE {
		line = line[2:]
	}
	compact := make([]byte, 0, len(line))
	for _, value := range line {
		if value != 0 {
			compact = append(compact, value)
		}
	}
	lower := strings.ToLower(string(compact))
	return strings.HasPrefix(lower, "wsl:") || strings.HasPrefix(lower, "wsl.exe:")
}

func isInternalToolLogBytes(line []byte) bool {
	compact := make([]byte, 0, len(line))
	for _, value := range line {
		if value == 0 || value == '\r' || value == '\t' || value == ' ' || value == '\xA0' {
			continue
		}
		if value >= 'A' && value <= 'Z' {
			value += 'a' - 'A'
		}
		compact = append(compact, value)
	}
	text := string(compact)
	return strings.Contains(text, "time=\"") &&
		strings.Contains(text, "level=") &&
		strings.Contains(text, "msg=")
}

// decodeWSLOutput приводит смешанный UTF-16/UTF-8 вывод WSL к обычной строке.
func decodeWSLOutput(raw []byte) string {
	if len(raw) == 0 {
		return ""
	}
	if looksLikeUTF16LE(raw) {
		return decodeUTF16LEBestEffort(raw)
	}
	if utf8.Valid(raw) {
		return string(raw)
	}
	if decoded, err := charmap.CodePage866.NewDecoder().Bytes(raw); err == nil {
		return string(decoded)
	}

	split := len(raw)
	for k := 0; k < len(raw); k++ {
		if utf8.Valid(raw[k:]) {
			split = k
			break
		}
	}

	var b strings.Builder
	if split > 0 {
		b.WriteString(decodeUTF16LEBestEffort(raw[:split]))
	}
	b.Write(raw[split:])
	return b.String()
}

func looksLikeUTF16LE(raw []byte) bool {
	if len(raw) >= 2 && raw[0] == 0xFF && raw[1] == 0xFE {
		return true
	}
	if len(raw) < 4 {
		return false
	}
	zeroOdd, zeroEven := 0, 0
	for i, value := range raw {
		if value != 0 {
			continue
		}
		if i%2 == 0 {
			zeroEven++
		} else {
			zeroOdd++
		}
	}
	return zeroOdd >= 2 && zeroOdd > zeroEven && zeroOdd*4 >= len(raw)
}

func decodeUTF16LEBestEffort(b []byte) string {
	if len(b) >= 2 && b[0] == 0xFF && b[1] == 0xFE {
		b = b[2:]
	}
	if len(b)%2 != 0 {
		b = b[:len(b)-1]
	}
	u16 := make([]uint16, 0, len(b)/2)
	for i := 0; i+1 < len(b); i += 2 {
		u16 = append(u16, uint16(b[i])|uint16(b[i+1])<<8)
	}
	return string(utf16.Decode(u16))
}

type Image struct {
	ID         string `json:"ID"`
	Repository string `json:"Repository"`
	Tag        string `json:"Tag"`
	Size       string `json:"Size"`
	CreatedAt  string `json:"CreatedAt"`
	sizeBytes  int64
}

type Volume struct {
	Name       string `json:"Name"`
	Driver     string `json:"Driver"`
	Mountpoint string `json:"Mountpoint"`
}

type ContainerStat struct {
	ID     string `json:"ID"`
	Name   string `json:"Name"`
	CPU    string `json:"CPUPerc"`
	Memory string `json:"MemUsage"`
	NetIO  string `json:"NetIO"`
	PIDs   string `json:"PIDs"`
}

type BuildPhase struct {
	Icon     string
	Title    string
	Progress float32
}

type buildPhaseDefinition struct {
	keywords        []string
	icon            string
	titleKey        string
	special         bool
	progressRange   [2]float32
	weight          int
	typicalDuration time.Duration
}

func (d buildPhaseDefinition) title() string {
	return i18n.T(d.titleKey)
}

var buildPhaseMap = []buildPhaseDefinition{
	{
		[]string{"Preparing", "preparing"},
		"🔍", "build_phase.preparing", false,
		[2]float32{0.00, 0.05}, 1, 5 * time.Second,
	},
	{
		[]string{"Resolving", "resolving", "resolving dependencies"},
		"📦", "build_phase.resolving_deps", false,
		[2]float32{0.03, 0.08}, 2, 10 * time.Second,
	},
	{
		[]string{"Using cache", "Cached", "cache hit"},
		"⚡", "build_phase.using_cache", false,
		[2]float32{0.05, 0.12}, 3, 3 * time.Second,
	},
	{
		[]string{"Pulling", "pulling", "downloading", "download"},
		"🌐", "build_phase.pulling", false,
		[2]float32{0.10, 0.25}, 4, 30 * time.Second,
	},
	{
		[]string{"Verifying", "verifying", "verif"},
		"✅", "build_phase.verifying", false,
		[2]float32{0.20, 0.30}, 3, 10 * time.Second,
	},
	{
		[]string{"Expanding", "expanding", "unpacking"},
		"📂", "build_phase.unpacking", false,
		[2]float32{0.25, 0.35}, 3, 15 * time.Second,
	},
	{
		[]string{"Building", "building", "compile", "compiling", "gcc", "g++", "rustc", "npm run", "pip install"},
		"🔨", "build_phase.compiling", false,
		[2]float32{0.30, 0.65}, 5, 60 * time.Second,
	},
	{
		[]string{"Linking", "linking"},
		"🔗", "build_phase.linking", false,
		[2]float32{0.60, 0.70}, 4, 15 * time.Second,
	},
	{
		[]string{"Finalizing", "finalizing", "optimizing", "compressing"},
		"✨", "build_phase.optimizing", false,
		[2]float32{0.70, 0.85}, 4, 20 * time.Second,
	},
	{
		[]string{"Saving", "saving", "pushing", "uploading"},
		"💾", "build_phase.saving", false,
		[2]float32{0.80, 0.95}, 4, 15 * time.Second,
	},
	{
		[]string{"Successfully", "success", "complete", "done", "Build complete"},
		"🎉", "build_phase.success", true,
		[2]float32{1.0, 1.0}, 10, 0,
	},
	{
		[]string{"Error", "error", "failed", "fail", "panic"},
		"❌", "build_phase.error", true,
		[2]float32{0.0, 0.0}, 10, 0,
	},
}

func detectBuildkitStepProgress(line string) float32 {
	open := strings.LastIndex(line, "[")
	if open < 0 {
		return -1
	}
	seg := line[open+1:]
	closing := strings.Index(seg, "]")
	if closing < 0 {
		return -1
	}
	seg = seg[:closing]
	slash := strings.Index(seg, "/")
	if slash <= 0 || slash == len(seg)-1 {
		return -1
	}
	before := strings.TrimRight(seg[:slash], " ")
	i := len(before)
	for i > 0 && before[i-1] >= '0' && before[i-1] <= '9' {
		i--
	}
	if i == len(before) {
		return -1
	}
	after := strings.TrimLeft(seg[slash+1:], " ")
	j := 0
	for j < len(after) && after[j] >= '0' && after[j] <= '9' {
		j++
	}
	if j == 0 {
		return -1
	}
	var step, total float32
	if _, err := fmt.Sscanf(before[i:], "%f", &step); err != nil {
		return -1
	}
	if _, err := fmt.Sscanf(after[:j], "%f", &total); err != nil {
		return -1
	}
	if total <= 0 || step < 0 || step > total {
		return -1
	}
	return step / total
}

func detectProgressFromBar(line string) float32 {
	if p := detectBuildkitStepProgress(line); p > 0 {
		return p
	}
	idx := strings.LastIndex(line, "%")
	if idx > 0 {
		start := idx - 1
		for start > 0 && ((line[start] >= '0' && line[start] <= '9') || line[start] == '.') {
			start--
		}
		start++
		if start <= idx {
			numStr := line[start:idx]
			var pct float32
			fmt.Sscanf(numStr, "%f", &pct)
			if pct >= 0 && pct <= 100 {
				return pct / 100.0
			}
		}
	}
	if slash := strings.Index(line, " / "); slash > 0 {
		var done, total float32
		n, _ := fmt.Sscanf(line[:slash], "%f", &done)
		if n == 1 {
			rest := line[slash+3:]
			end := strings.IndexAny(rest, " \t\n")
			if end < 0 {
				end = len(rest)
			}
			fmt.Sscanf(rest[:end], "%f", &total)
			if total > 0 {
				p := done / total
				if p >= 0 && p <= 1 {
					return p
				}
			}
		}
	}
	return -1
}

type buildProgressTracker struct {
	mu             sync.Mutex
	startTime      time.Time
	lastUpdateTime time.Time
	currentPhase   string
	phaseStartTime time.Time
	lastProgress   float32
}

var globalBuildTracker = &buildProgressTracker{}

func ResetBuildProgress() {
	globalBuildTracker.mu.Lock()
	defer globalBuildTracker.mu.Unlock()
	globalBuildTracker.startTime = time.Now()
	globalBuildTracker.lastUpdateTime = time.Now()
	globalBuildTracker.currentPhase = ""
	globalBuildTracker.phaseStartTime = time.Now()
	globalBuildTracker.lastProgress = 0
}

func (t *buildProgressTracker) getPhaseProgress(phase buildPhaseDefinition, elapsed time.Duration) float32 {
	if phase.typicalDuration == 0 {
		return phase.progressRange[1]
	}
	phaseProgress := float32(elapsed.Seconds() / phase.typicalDuration.Seconds())
	if phaseProgress > 1.0 {
		phaseProgress = 1.0
	}
	rangeSize := phase.progressRange[1] - phase.progressRange[0]
	return phase.progressRange[0] + phaseProgress*rangeSize
}

func DetermineBuildPhaseWithTime(output string) BuildPhase {
	if pct := detectMostRecentProgress(output); pct > 0 {
		// Шаги BuildKit [N/M] дают реальный процент; не даём ему
		// откатываться назад при переходе между изображениями.
		globalBuildTracker.mu.Lock()
		if pct < globalBuildTracker.lastProgress {
			pct = globalBuildTracker.lastProgress
		}
		globalBuildTracker.lastProgress = pct
		globalBuildTracker.mu.Unlock()
		return BuildPhase{"⏳", i18n.T("build_phase.building"), pct}
	}
	phaseDef, _ := determinePhaseByKeywords(output)
	if phaseDef != nil {
		globalBuildTracker.mu.Lock()
		defer globalBuildTracker.mu.Unlock()
		now := time.Now()
		phaseName := phaseDef.titleKey
		if globalBuildTracker.currentPhase != phaseName {
			globalBuildTracker.currentPhase = phaseName
			globalBuildTracker.phaseStartTime = now
		}
		elapsed := now.Sub(globalBuildTracker.phaseStartTime)
		progress := globalBuildTracker.getPhaseProgress(*phaseDef, elapsed)
		if globalBuildTracker.lastProgress > 0 {
			minAllowed := globalBuildTracker.lastProgress - 0.10
			if progress < minAllowed {
				progress = minAllowed
			}
		}
		globalBuildTracker.lastProgress = progress
		return BuildPhase{
			Icon:     phaseDef.icon,
			Title:    phaseDef.title(),
			Progress: progress,
		}
	}
	return estimateProgressByOutputLength(output)
}

func detectMostRecentProgress(output string) float32 {
	lines := strings.Split(output, "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		line := strings.TrimSpace(lines[i])
		if line == "" {
			continue
		}
		pct := detectProgressFromBar(line)
		if pct > 0 {
			return pct
		}
	}
	return -1
}

func determinePhaseByKeywords(output string) (*buildPhaseDefinition, float32) {
	lower := strings.ToLower(output)
	var bestPhase *buildPhaseDefinition
	bestScore := 0
	for i := range buildPhaseMap {
		def := &buildPhaseMap[i]
		if def.special {
			continue
		}
		score := 0
		for _, kw := range def.keywords {
			if strings.Contains(lower, strings.ToLower(kw)) {
				score += def.weight
			}
		}
		if score > bestScore {
			bestScore = score
			bestPhase = def
		}
	}
	if bestPhase == nil {
		return nil, 0
	}
	return bestPhase, float32(bestScore)
}

func estimateProgressByOutputLength(output string) BuildPhase {
	outputLen := len(output)
	switch {
	case outputLen > 10000:
		return BuildPhase{"🔨", i18n.T("build_phase.building"), 0.85}
	case outputLen > 5000:
		return BuildPhase{"🔨", i18n.T("build_phase.compiling"), 0.65}
	case outputLen > 2000:
		return BuildPhase{"🔨", i18n.T("build_phase.compiling"), 0.40}
	case outputLen > 500:
		return BuildPhase{"📦", i18n.T("build_phase.preparing"), 0.20}
	default:
		return BuildPhase{"⏳", i18n.T("build_phase.preparing"), 0.05}
	}
}

// tailLines возвращает последние n строк вывода (для анализа текущей фазы).
func tailLines(output string, n int) string {
	lines := strings.Split(output, "\n")
	if len(lines) <= n {
		return output
	}
	return strings.Join(lines[len(lines)-n:], "\n")
}

// lastNonEmptyLine возвращает последнюю непустую строку в нижнем регистре.
func lastNonEmptyLine(output string) string {
	lines := strings.Split(output, "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		if l := strings.TrimSpace(lines[i]); l != "" {
			return strings.ToLower(l)
		}
	}
	return ""
}

var buildFailureMarkers = []string{
	"build failed",
	"failed to solve",
	"failed to build",
	"exit code:",
	"process \"/bin/sh\" did not complete successfully",
	"dockerfile parse error",
	"panic:",
}

var buildSuccessMarkers = []string{
	"build complete",
	"successfully built",
	"successfully exported",
}

func DetectBuildPhase(output string) BuildPhase {
	last := lastNonEmptyLine(output)
	for _, m := range buildFailureMarkers {
		if strings.Contains(last, m) {
			return BuildPhase{"❌", i18n.T("build_phase.error"), 0.0}
		}
	}
	for _, m := range buildSuccessMarkers {
		if strings.Contains(last, m) {
			return BuildPhase{"🎉", i18n.T("build_phase.success"), 1.0}
		}
	}
	return DetermineBuildPhaseWithTime(tailLines(output, 15))
}

func FormatBuildStatus(phase BuildPhase) string {
	return fmt.Sprintf("%s %s", phase.Icon, phase.Title)
}

const buildkitdStartTimeout = 15 * time.Second

const defaultBuildkitAddr = "unix:///run/buildkit/buildkitd.sock"

const rootContainerdAddr = "unix:///run/containerd/containerd.sock"

var buildkitHostCache = struct {
	sync.Mutex
	addr    string
	expires time.Time
}{}

func BuildkitHostAddr() string {
	return defaultBuildkitAddr
}

// InvalidateBuildkitHostCache сбрасывает кэш адреса сокета (после запуска или
// остановки демона, чтобы следующий детект увидел актуальное состояние).
func InvalidateBuildkitHostCache() {
	buildkitHostCache.Lock()
	buildkitHostCache.addr = ""
	buildkitHostCache.expires = time.Time{}
	buildkitHostCache.Unlock()
}

func waitForBuildkitReady(check func() bool, logPath, label string) error {
	deadline := time.Now().Add(buildkitdStartTimeout)
	for time.Now().Before(deadline) {
		time.Sleep(500 * time.Millisecond)
		if check() {
			InvalidateBuildkitHostCache()
			return nil
		}
	}

	logTail, _ := runWSLWithTimeout("tail -n 15 "+shellQuote(logPath)+" 2>/dev/null", 5*time.Second)
	if logTail != "" {
		return fmt.Errorf("%s не ответил в течение %s\nЛог демона (%s):\n%s", label, buildkitdStartTimeout, logPath, logTail)
	}
	return fmt.Errorf("%s не ответил в течение %s", label, buildkitdStartTimeout)
}

func runWSLWithTimeout(command string, timeout time.Duration) (string, error) {
	distro := strings.TrimSpace(GetWslDistro())
	if distro == "" {
		return "", fmt.Errorf("WSL-дистрибутив не выбран или не найден")
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, WslExecutable(), "-d", distro, "--exec", GetShell(), "-c", command)
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &out
	err := cmd.Run()
	if ctx.Err() == context.DeadlineExceeded {
		return strings.TrimSpace(decodeWSLOutput(filterWSLDiagnosticBytes(out.Bytes()))), fmt.Errorf("таймаут WSL-команды (%s)", timeout)
	}
	return strings.TrimSpace(decodeWSLOutput(filterWSLDiagnosticBytes(out.Bytes()))), err
}

// runWSLAsRootWithTimeout выполняет команду от root через `wsl -u root`.
// В стандартном WSL2 root доступен напрямую, без sudo и без пароля.
func rootWSLSetupEnv() string {
	return `unset XDG_RUNTIME_DIR XDG_DATA_HOME XDG_CACHE_HOME XDG_CONFIG_HOME;
export HOME=/root;
export XDG_DATA_HOME=/root/.local/share;
export XDG_CACHE_HOME=/root/.cache;
export XDG_CONFIG_HOME=/root/.config`
}

func runWSLAsRootWithTimeout(command string, timeout time.Duration) (string, error) {
	distro := strings.TrimSpace(GetWslDistro())
	if distro == "" {
		return "", fmt.Errorf("WSL-дистрибутив не выбран или не найден")
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, WslExecutable(), "-d", distro, "-u", "root", "--exec", GetShell(), "-c", rootWSLSetupEnv()+"; "+command)
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &out
	err := cmd.Run()
	if ctx.Err() == context.DeadlineExceeded {
		return strings.TrimSpace(decodeWSLOutput(filterWSLDiagnosticBytes(out.Bytes()))), fmt.Errorf("таймаут WSL-команды (%s)", timeout)
	}
	return strings.TrimSpace(decodeWSLOutput(filterWSLDiagnosticBytes(out.Bytes()))), err
}

// RunWSLAsRootWithTimeout выполняет диагностическую или служебную команду
// в том же root-контексте, что и операции containerd/nerdctl.
func RunWSLAsRootWithTimeout(command string, timeout time.Duration) (string, error) {
	return runWSLAsRootWithTimeout(command, timeout)
}

func CheckBuildkitd() bool {
	script := `
set -e
export PATH="/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin:${PATH}"
if ! command -v buildctl >/dev/null 2>&1; then
    exit 127
fi

if [ ! -S /run/buildkit/buildkitd.sock ]; then
	exit 1
fi

buildctl --addr ` + defaultBuildkitAddr + ` debug workers </dev/null >/dev/null 2>&1`
	_, err := runWSLAsRootWithTimeout(script, 10*time.Second)
	return err == nil
}

const buildkitDiagScript = `(command -v buildkitd >/dev/null 2>&1 || test -x /usr/local/bin/buildkitd) && echo "DIAG:BKBIN:OK" || echo "DIAG:BKBIN:NO"`

const buildkitdRootLaunchScript = `set -e
 
# В WSL-окружении запуск через -c или sudo может терять стандартный PATH.
# Восстанавливаем его явно, чтобы buildkitd всегда находился по известным
# путям, даже если среда пришла из Windows/NAT-режима.
export PATH="/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin:${PATH}"
 
echo "DIAG: root BuildKit launcher v4" >&2
echo "DIAG: uid=0 shell=$0" >&2
echo "DIAG: distro=WSL root environment" >&2
echo "DIAG: PATH=$PATH" >&2
if ! command -v buildkitd >/dev/null 2>&1; then
	echo "DIAG: buildkitd executable not found" >&2
	exit 127
fi
if ! command -v buildctl >/dev/null 2>&1; then
	echo "DIAG: buildctl executable not found" >&2
	exit 127
fi

echo "DIAG: buildkitd found in PATH" >&2
echo "DIAG: buildctl found in PATH" >&2
 
mkdir -p /run/buildkit
chmod 777 /run/buildkit
 
pkill -x buildkitd 2>/dev/null || true
 
rm -f /run/buildkit/buildkitd.sock
 
setsid nohup buildkitd \
	--addr unix:///run/buildkit/buildkitd.sock \
	--root /var/lib/buildkit \
	--oci-worker-net=host \
	</dev/null >/tmp/buildkitd.log 2>&1 &
BK_PID=$!
 
# Ждём появления сокета (до 10с) без command substitution и арифметики.
for _attempt in 1 2 3 4 5 6 7 8 9 10 11 12 13 14 15 16 17 18 19 20; do
	[ -S /run/buildkit/buildkitd.sock ] && break
	sleep 0.5
done
 
if [ ! -S /run/buildkit/buildkitd.sock ]; then
	echo "DIAG: buildkitd socket not created within 10s" >&2
	if ! kill -0 "$BK_PID" 2>/dev/null; then
		echo "DIAG: buildkitd process exited during startup" >&2
	fi
	echo "DIAG: tail of /tmp/buildkitd.log:" >&2
	tail -n 20 /tmp/buildkitd.log >&2 2>&1 || true
	exit 1
fi
 
# Проверяем не просто наличие сокета, а готовность самого BuildKit —
# если daemon запустился, но ещё не ответил на debug workers, он не готов
# к приёму задач. Это критично для WSL2 и для корректного fallback.
for _i in 1 2 3 4 5 6 7 8 9 10 11 12 13 14 15 16 17 18 19 20; do
	buildctl --addr unix:///run/buildkit/buildkitd.sock debug workers >/dev/null 2>&1 && break
	sleep 0.5
done
if ! buildctl --addr unix:///run/buildkit/buildkitd.sock debug workers >/dev/null 2>&1; then
	echo "DIAG: buildkitd socket did not become ready within 20s" >&2
	echo "DIAG: tail of /tmp/buildkitd.log:" >&2
	tail -n 20 /tmp/buildkitd.log >&2 2>&1 || true
	exit 1
fi

# Проверяем, что daemon не завершился сразу после первого успешного ответа.
# BK_PID принадлежит промежуточному setsid, поэтому проверяем сам daemon.
sleep 1
if ! pgrep -x buildkitd >/dev/null 2>&1; then
	echo "DIAG: buildkitd exited immediately after becoming ready" >&2
	echo "DIAG: tail of /tmp/buildkitd.log:" >&2
	tail -n 40 /tmp/buildkitd.log >&2 2>&1 || true
	exit 1
fi
 
chmod 666 /run/buildkit/buildkitd.sock 2>/dev/null || true
echo 'launch_ok'
`

func StartBuildkitdAsRoot() error {
	out, err := runWSLAsRootWithTimeout(buildkitdRootLaunchScript, 25*time.Second)
	if err != nil {
		return fmt.Errorf("`wsl -u root` завершился ошибкой: %w\n%s",
			err, strings.TrimSpace(out))
	}
	if !strings.Contains(out, "launch_ok") {
		return fmt.Errorf("`wsl -u root`: скрипт запуска не подтвердил старт\n%s",
			strings.TrimSpace(out))
	}
	// Root-скрипт уже дождался сокета и успешно выполнил
	// `buildctl debug workers`. Повторный вызов CheckBuildkitd здесь запускает
	// отдельную WSL-команду и может ложно сообщить о таймауте после успешного старта.
	InvalidateBuildkitHostCache()
	return nil
}

func StartBuildkitd() error {
	if CheckBuildkitd() {
		return nil
	}
	return StartBuildkitdAsRoot()
}

func buildkitStopScript() string {
	return `
set -e
for _p in $(ps -eo pid=,comm= 2>/dev/null | awk '$2=="buildkitd" {print $1}'); do
    kill "$_p" 2>/dev/null || true
    kill -9 "$_p" 2>/dev/null || true
done
pkill -x buildkitd 2>/dev/null || true
rm -f \
	/run/buildkit/buildkitd.sock \
    2>/dev/null || true
echo ok
`
}

func StopBuildkitd() {
	_, _ = runWSLAsRootWithTimeout(buildkitStopScript(), 10*time.Second)
	InvalidateBuildkitHostCache()
}

const maxWSLCacheSize = 10 * 1024 * 1024

var wslCache = struct {
	sync.RWMutex
	m         map[string]wslCacheEntry
	totalSize int64
	maxSize   int64
	cleanupAt int
}{m: make(map[string]wslCacheEntry), maxSize: maxWSLCacheSize, cleanupAt: 25}

type wslCacheEntry struct {
	output    string
	err       error
	timestamp time.Time
	size      int64
}

func runWSLDirect(shell, command string) (string, error) {
	distro := strings.TrimSpace(GetWslDistro())
	if distro == "" {
		return "", fmt.Errorf("WSL-дистрибутив не выбран или не найден")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, WslExecutable(), "-d", distro, "--exec", shell, "-c", command)
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	var out bytes.Buffer
	cmd.Stdout = &out
	err := cmd.Run()
	if ctx.Err() == context.DeadlineExceeded {
		return strings.TrimSpace(decodeWSLOutput(filterWSLDiagnosticBytes(out.Bytes()))), fmt.Errorf("таймаут выполнения WSL-команды (10s)")
	}
	return strings.TrimSpace(decodeWSLOutput(filterWSLDiagnosticBytes(out.Bytes()))), err
}

func RunWSL(command string) (string, error) {
	distro := strings.TrimSpace(GetWslDistro())
	if distro == "" {
		return "", fmt.Errorf("WSL-дистрибутив не выбран или не найден")
	}

	wslCache.RLock()
	ttl := time.Duration(wslCacheTTL.Load()) * time.Second
	cacheKey := distro + "\x00" + command
	if entry, ok := wslCache.m[cacheKey]; ok && time.Since(entry.timestamp) < ttl {
		wslCache.RUnlock()
		return entry.output, entry.err
	}
	wslCache.RUnlock()

	// Таймаут защищает UI от вечной блокировки при зависшей WSL-VM.
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, WslExecutable(), "-d", distro, "--exec", GetShell(), "-c", command)
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	var out bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &stderr

	err := cmd.Run()
	if ctx.Err() == context.DeadlineExceeded {
		err = fmt.Errorf("таймаут выполнения WSL-команды (120s)")
	}
	result := strings.TrimSpace(decodeWSLOutput(filterWSLDiagnosticBytes(out.Bytes())))
	resultSize := int64(len(result))

	wslCache.Lock()
	for k, v := range wslCache.m {
		if time.Since(v.timestamp) > ttl {
			wslCache.totalSize -= v.size
			delete(wslCache.m, k)
		}
	}
	for (wslCache.totalSize+resultSize > wslCache.maxSize ||
		(wslCache.cleanupAt > 0 && len(wslCache.m) >= wslCache.cleanupAt)) && len(wslCache.m) > 0 {
		var oldestKey string
		var oldestTime time.Time
		for k, v := range wslCache.m {
			if oldestKey == "" || v.timestamp.Before(oldestTime) {
				oldestKey = k
				oldestTime = v.timestamp
			}
		}
		if oldestKey != "" {
			wslCache.totalSize -= wslCache.m[oldestKey].size
			delete(wslCache.m, oldestKey)
		}
	}
	wslCache.m[cacheKey] = wslCacheEntry{
		output:    result,
		err:       err,
		timestamp: time.Now(),
		size:      resultSize,
	}
	wslCache.totalSize += resultSize
	wslCache.Unlock()

	return result, err
}

func RunWSLWithCancel(ctx context.Context, command string) (string, error) {
	if isBuildCommand(command) {
		return executeWSLCommand(ctx, command, true)
	}
	distro := strings.TrimSpace(GetWslDistro())
	if distro == "" {
		return "", fmt.Errorf("WSL-дистрибутив не выбран или не найден")
	}

	wslCache.RLock()
	ttl := time.Duration(wslCacheTTL.Load()) * time.Second
	cacheKey := distro + "\x00" + command
	if entry, ok := wslCache.m[cacheKey]; ok && time.Since(entry.timestamp) < ttl {
		wslCache.RUnlock()
		return entry.output, entry.err
	}
	wslCache.RUnlock()
	return executeWSLCommand(ctx, command, false)
}

func executeWSLCommand(ctx context.Context, command string, skipCache bool) (string, error) {
	distro := strings.TrimSpace(GetWslDistro())
	if distro == "" {
		return "", fmt.Errorf("WSL-дистрибутив не выбран или не найден")
	}

	cmd := exec.CommandContext(ctx, WslExecutable(), "-d", distro, "--exec", GetShell(), "-c", command)
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	var out bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &stderr

	err := cmd.Run()
	result := strings.TrimSpace(decodeWSLOutput(filterWSLDiagnosticBytes(out.Bytes())))
	errOutput := strings.TrimSpace(decodeWSLOutput(filterWSLDiagnosticBytes(stderr.Bytes())))

	if ctx.Err() == nil && !skipCache {
		resultSize := int64(len(result))
		if resultSize < 1024*1024 {
			wslCache.Lock()
			cacheKey := distro + "\x00" + command
			// Ключ мог остаться в кэше с истёкшим TTL — вычитаем размер
			// старой записи, иначе totalSize будет завышаться.
			if old, ok := wslCache.m[cacheKey]; ok {
				wslCache.totalSize -= old.size
			}
			wslCache.m[cacheKey] = wslCacheEntry{
				output:    result,
				err:       err,
				timestamp: time.Now(),
				size:      resultSize,
			}
			wslCache.totalSize += resultSize
			if len(wslCache.m) > 100 {
				var oldest string
				var oldestTime time.Time
				for k, v := range wslCache.m {
					if oldestTime.IsZero() || v.timestamp.Before(oldestTime) {
						oldest = k
						oldestTime = v.timestamp
					}
				}
				if oldest != "" {
					wslCache.totalSize -= wslCache.m[oldest].size
					delete(wslCache.m, oldest)
				}
			}
			wslCache.Unlock()
		}
	}

	if err != nil && errOutput != "" {
		fullOutput := result
		if fullOutput != "" && errOutput != "" {
			fullOutput += "\n" + errOutput
		} else if errOutput != "" {
			fullOutput = errOutput
		}
		return fullOutput, err
	}
	return result, err
}

func RunWSLWithCancelStream(ctx context.Context, command string, onLine func(string)) (string, error) {
	distro := strings.TrimSpace(GetWslDistro())
	if distro == "" {
		return "", fmt.Errorf("WSL-дистрибутив не выбран или не найден")
	}

	cmd := exec.CommandContext(ctx, WslExecutable(), "-d", distro, "--exec", GetShell(), "-c", command)
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}

	stdoutPipe, err := cmd.StdoutPipe()
	if err != nil {
		return "", err
	}
	stderrPipe, err := cmd.StderrPipe()
	if err != nil {
		return "", err
	}

	if err := cmd.Start(); err != nil {
		return "", err
	}

	var mu sync.Mutex
	var out strings.Builder
	var errOut strings.Builder

	readLines := func(r io.Reader, dest *strings.Builder, stream bool) {
		scanner := bufio.NewScanner(r)
		scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
		for scanner.Scan() {
			line := decodeWSLOutput(filterWSLDiagnosticBytes([]byte(scanner.Text())))
			mu.Lock()
			dest.WriteString(line)
			dest.WriteByte('\n')
			mu.Unlock()
			if stream && onLine != nil {
				onLine(line)
			}
		}
	}

	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()

		readLines(stdoutPipe, &out, true)
	}()
	go func() {
		defer wg.Done()
		readLines(stderrPipe, &errOut, true)
	}()
	wg.Wait()

	runErr := cmd.Wait()

	mu.Lock()
	result := strings.TrimSpace(out.String())
	stderrText := strings.TrimSpace(errOut.String())
	mu.Unlock()

	if runErr != nil && stderrText != "" {
		if result != "" {
			result += "\n" + stderrText
		} else {
			result = stderrText
		}
	}
	return result, runErr
}

func runWSLAsRootWithCancelStream(ctx context.Context, script string, onLine func(string)) (string, error) {
	distro := strings.TrimSpace(GetWslDistro())
	if distro == "" {
		return "", fmt.Errorf("WSL-дистрибутив не выбран или не найден")
	}

	cmd := exec.CommandContext(ctx, WslExecutable(), "-d", distro, "-u", "root", "--exec", GetShell(), "-s", "--")
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	cmd.Stdin = strings.NewReader(rootWSLSetupEnv() + "\n" + script + "\n")

	stdoutPipe, err := cmd.StdoutPipe()
	if err != nil {
		return "", err
	}
	stderrPipe, err := cmd.StderrPipe()
	if err != nil {
		return "", err
	}
	if err := cmd.Start(); err != nil {
		return "", err
	}

	var mu sync.Mutex
	var out strings.Builder
	readLines := func(r io.Reader) {
		scanner := bufio.NewScanner(r)
		scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
		for scanner.Scan() {
			line := decodeWSLOutput(filterWSLDiagnosticBytes([]byte(scanner.Text())))
			mu.Lock()
			out.WriteString(line)
			out.WriteByte('\n')
			mu.Unlock()
			if onLine != nil {
				onLine(line)
			}
		}
	}

	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		readLines(stdoutPipe)
	}()
	go func() {
		defer wg.Done()
		readLines(stderrPipe)
	}()
	wg.Wait()
	runErr := cmd.Wait()

	mu.Lock()
	result := strings.TrimSpace(out.String())
	mu.Unlock()
	return result, runErr
}

func isBuildCommand(command string) bool {
	lower := strings.ToLower(command)
	return strings.Contains(lower, "compose build") ||
		strings.Contains(lower, "compose up") ||
		strings.Contains(lower, "nerdctl build") ||
		strings.Contains(lower, "nerdctl push") ||
		strings.Contains(lower, "docker_buildkit=0")
}

func InvalidateWSLCache() {
	wslCache.Lock()
	for k := range wslCache.m {
		delete(wslCache.m, k)
	}
	wslCache.Unlock()
}

func CheckService() map[string]interface{} {
	status := map[string]interface{}{"wsl": false, "nerdctl": false, "containerd": false, "error": ""}

	// Сначала убеждаемся, что есть реально установленный distro.
	distro := strings.TrimSpace(GetWslDistro())
	if distro == "" {
		status["error"] = "WSL-дистрибутив не найден. Установите хотя бы один дистрибутив через Windows WSL."
		return status
	}

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	_, err := RunWSLWithCancel(ctx, "echo ok")
	if err != nil || ctx.Err() != nil {
		ctx2, cancel2 := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel2()
		_, err = RunWSLWithCancel(ctx2, "echo ok")
		if err != nil || ctx2.Err() != nil {
			status["error"] = fmt.Sprintf("WSL '%s' не найден или не отвечает", GetWslDistro())
			return status
		}
	}
	status["wsl"] = true
	if cdAvailable.Load() || CDCheck() == nil {
		status["containerd"] = true
		status["nerdctl"] = true
		return status
	}
	out, _ := RunWSL("echo '---'; which nerdctl 2>/dev/null")
	parts := strings.Split(out, "---")
	if IsServiceActive(GetContainerdService()) {
		status["containerd"] = true
	}
	if len(parts) > 1 && strings.TrimSpace(parts[1]) != "" {
		status["nerdctl"] = true
	}
	return status
}

func ListContainers(all bool) ([]Container, error) {
	return CDListContainers(all)
}

func ListNetworks(ctx context.Context) ([]Network, error) {
	out, err := runRootNerdctl(ctx, "network ls --format '{{json .}}' 2>/dev/null")
	if err != nil {
		return nil, err
	}
	var networks []Network
	for _, line := range strings.Split(out, "\n") {
		var network Network
		if json.Unmarshal([]byte(strings.TrimSpace(line)), &network) == nil && network.Name != "" {
			networks = append(networks, network)
		}
	}
	return networks, nil
}

func GetNetworkContainers(ctx context.Context, name string) ([]string, error) {
	command := fmt.Sprintf("network inspect %s --format '{{json .Containers}}' 2>/dev/null", shellQuote(name))
	out, err := runRootNerdctl(ctx, command)
	if err != nil {
		return nil, err
	}
	return parseNetworkContainers(out)
}

func parseNetworkContainers(out string) ([]string, error) {
	var entries map[string]struct {
		Name string `json:"Name"`
	}
	if err := json.Unmarshal([]byte(strings.ReplaceAll(out, "\x00", "")), &entries); err != nil {
		return nil, err
	}
	containers := make([]string, 0, len(entries))
	for id, entry := range entries {
		if entry.Name != "" {
			containers = append(containers, entry.Name)
		} else {
			containers = append(containers, id)
		}
	}
	sort.Strings(containers)
	return containers, nil
}

func CreateNetwork(ctx context.Context, name, driver string) error {
	_, err := runRootNerdctl(ctx, fmt.Sprintf("network create --driver %s %s", shellQuote(driver), shellQuote(name)))
	return err
}

func RemoveNetwork(ctx context.Context, name string) error {
	_, err := runRootNerdctl(ctx, fmt.Sprintf("network rm %s", shellQuote(name)))
	return err
}

func rootNerdctlCommand(args string) string {
	return "nerdctl --address " + shellQuote(rootContainerdAddr) +
		" --namespace " + shellQuote(GetCdNamespace()) + " " + args
}

func volumeRemoveCommand(name string) string {
	return "volume rm -f " + shellQuote(name)
}

// IsProtectedVolumeName reports whether automated cleanup must never remove
// the volume. Project stack volumes (database data in particular) survive a
// full "unused volumes" sweep.
func IsProtectedVolumeName(name string) bool {
	name = strings.TrimSpace(name)
	base := strings.TrimPrefix(name, "containerd_")
	return strings.HasPrefix(base, "soul-dialogue-") ||
		base == GetDBVolumeName() ||
		strings.HasPrefix(name, "soul-dialogue-") ||
		name == GetDBVolumeName() ||
		strings.HasPrefix(name, "containerd_soul-dialogue-")
}

// listUnusedVolumes returns volumes that no container references. nerdctl's
// dangling filter is authoritative; if the installed nerdctl does not support
// it, unused volumes are computed from the containerd container specs instead.
func listUnusedVolumes(ctx context.Context) ([]Volume, error) {
	out, err := runRootNerdctl(ctx, "volume ls --filter dangling=true --format '{{json .}}' 2>/dev/null")
	if err == nil {
		return parseVolumeLines(out), nil
	}

	all, listErr := CDListVolumes()
	if listErr != nil {
		return nil, listErr
	}
	used, usedErr := CDGetUsedVolumes(ctx)
	if usedErr != nil {
		// Without reliable usage data an automated sweep is unsafe.
		return nil, usedErr
	}
	var unused []Volume
	for _, volume := range all {
		if !used[volume.Name] {
			unused = append(unused, volume)
		}
	}
	return unused, nil
}

func volumeRemovalTargets(name, mountpoint string) []string {
	seen := make(map[string]struct{})
	paths := make([]string, 0, 8)
	add := func(value string) {
		value = strings.TrimSpace(value)
		if value == "" {
			return
		}
		if _, ok := seen[value]; ok {
			return
		}
		seen[value] = struct{}{}
		paths = append(paths, value)
	}
	add(mountpoint)
	if mountpoint != "" {
		add(strings.TrimSuffix(mountpoint, "/_data"))
		if strings.HasSuffix(mountpoint, "/_data") {
			add(strings.TrimSuffix(mountpoint, "/_data"))
		}
	}
	add("/var/lib/nerdctl/" + GetCdNamespace() + "/volumes/" + name)
	add("/var/lib/nerdctl/" + GetCdNamespace() + "/volumes/" + name + "/_data")
	add("/var/lib/containerd/volumes/" + name)
	add("/var/lib/containerd/volumes/" + name + "/_data")
	return paths
}

func staleVolumeSnapshotCandidates(name, snapshotOutput string) []string {
	name = strings.TrimSpace(name)
	if name == "" {
		return nil
	}
	pattern := "volume/" + name
	seen := make(map[string]struct{})
	result := make([]string, 0, 4)
	for _, line := range strings.Split(snapshotOutput, "\n") {
		key := strings.TrimSpace(line)
		if key == "" {
			continue
		}
		if strings.HasPrefix(key, "{") {
			var entry struct {
				Key string `json:"Key"`
			}
			if err := json.Unmarshal([]byte(key), &entry); err != nil {
				continue
			}
			key = strings.TrimSpace(entry.Key)
		}
		if key == pattern {
			if _, exists := seen[key]; !exists {
				seen[key] = struct{}{}
				result = append(result, key)
			}
		}
	}
	return result
}

func cleanupDeadVolumeSnapshot(name string) bool {
	name = strings.TrimSpace(name)
	if name == "" {
		return false
	}
	out, err := runWSLAsRootWithTimeout(
		"ctr --address "+shellQuote(rootContainerdAddr)+" --namespace "+shellQuote(GetCdNamespace())+" snapshots list --quiet 2>/dev/null || true",
		TimeoutMedium,
	)
	if err != nil || strings.TrimSpace(out) == "" {
		return false
	}
	candidates := staleVolumeSnapshotCandidates(name, out)
	if len(candidates) == 0 {
		return false
	}
	for _, snapshot := range candidates {
		_, _ = runWSLAsRootWithTimeout(
			"ctr --address "+shellQuote(rootContainerdAddr)+" --namespace "+shellQuote(GetCdNamespace())+" snapshots rm "+shellQuote(snapshot)+" 2>/dev/null || true",
			TimeoutMedium,
		)
	}
	return true
}

func containsString(values []string, needle string) bool {
	for _, value := range values {
		if value == needle {
			return true
		}
	}
	return false
}

func runRootNerdctl(ctx context.Context, args string) (string, error) {
	return runWSLAsRootWithCancelStream(ctx, rootNerdctlCommand(args), nil)
}

func StartContainer(id string) error {
	err := CDStartContainer(id)
	if err == nil {
		CDInvalidateContainersCache()
	}
	return err
}

func StopContainer(id string) error {
	err := CDStopContainer(id)
	if err == nil {
		CDInvalidateContainersCache()
	}
	return err
}

func ForceStopContainer(id string) error {
	_, err := runRootNerdctl(context.Background(), "stop -t 2 "+shellQuote(id))
	if err != nil {
		_, err = runRootNerdctl(context.Background(), "kill "+shellQuote(id))
	}
	if err == nil {
		CDInvalidateContainersCache()
	}
	return err
}

func RestartContainer(id string) error {
	err := CDRestartContainer(id)
	if err == nil {
		CDInvalidateContainersCache()
	}
	return err
}

func RemoveContainer(id string) error {
	err := CDRemoveContainer(id)
	if err == nil {
		CDInvalidateContainersCache()
	}
	return err
}

func CleanupAfterBuild() error {
	// Общий бюджет на container/image/volume prune и логи: на больших системах
	// volume prune может не уложиться в TimeoutMedium и молча ничего не удалить,
	// поэтому используем TimeoutSlow.
	ctx, cancel := context.WithTimeout(context.Background(), TimeoutSlow)
	defer cancel()
	var cleanupErrors []string
	for _, command := range []string{
		"nerdctl container prune --force",
		"nerdctl image prune --force",
	} {
		if _, err := RunWSLWithCancel(ctx, command); err != nil {
			cleanupErrors = append(cleanupErrors, fmt.Sprintf("%s: %v", command, err))
		}
	}
	if _, err := runRootNerdctl(ctx, "volume prune --force"); err != nil {
		cleanupErrors = append(cleanupErrors, fmt.Sprintf("nerdctl volume prune: %v", err))
	} else {
		CDInvalidateVolumesCache()
	}
	if _, err := CleanContainerdLogs(); err != nil {
		cleanupErrors = append(cleanupErrors, fmt.Sprintf("логи: %v", err))
	}
	if len(cleanupErrors) > 0 {
		return fmt.Errorf("очистка после сборки не выполнена: %s", strings.Join(cleanupErrors, "; "))
	}
	return nil
}

func BuildProject(ctx context.Context, tag string) (string, error) {
	defer func() {
		if err := CleanupAfterBuild(); err != nil {
			fmt.Printf("⚠️ %v\n", err)
		}
	}()
	return RunWSLWithCancel(ctx, BuildComposeCommand("build"))
}

// RunProject запускает стек. onLine (может быть nil) получает строки вывода
// по мере их появления.
func RunProject(ctx context.Context, onLine func(string)) (string, error) {
	projectPath := GetProjectPathWSL()
	if projectPath == "" {
		return "", fmt.Errorf("⚠️ Путь к проекту не настроен!\n\nПерейдите в Настройки и укажите путь к папке с docker-compose.yml")
	}
	return startProjectStackAsRoot(ctx, projectPath, onLine)
}

func ensureCNIPluginsInstalled() error {
	checkScript := `for _c in /opt/cni/bin/bridge /usr/lib/cni/bridge /usr/libexec/cni/bridge; do [ -x "$_c" ] && { echo OK; exit 0; }; done; echo MISSING`
	out, err := runWSLWithTimeout(checkScript, 10*time.Second)
	if err == nil && strings.TrimSpace(out) == "OK" {
		return nil
	}

	environment := CurrentEnvironment()
	cniPackage := CNIPluginPackage()
	if cniPackage == "" {
		fmt.Printf("⚠️ CNI bridge plugin missing; auto-install skipped for unsupported package manager.\n")
		return nil
	}

	priv := PrivilegePrefixNonInteractive()
	if priv == "" {
		fmt.Printf("⚠️ CNI bridge plugin missing; auto-install skipped because no non-interactive privilege method is available.\n")
		return nil
	}

	installScript, commandErr := cniPluginInstallCommand(environment, cniPackage, priv)
	if commandErr != nil {
		return commandErr
	}
	out, installErr := runWSLWithTimeout(installScript, 180*time.Second)
	if installErr != nil {
		low := strings.ToLower(out)
		if strings.Contains(low, "password") || strings.Contains(low, "sudo") || strings.Contains(low, "authentication failure") || strings.Contains(low, "try again") {
			fmt.Printf("⚠️ CNI bridge plugin missing; sudo auth required but not fatal for build flow.\n")
			return nil
		}
		fmt.Printf("⚠️ CNI bridge plugin missing after install attempt; continuing without hard-fail. Details: %s\n", strings.TrimSpace(out))
		return nil
	}

	checkAgain, err := runWSLWithTimeout(checkScript, 10*time.Second)
	if err != nil || strings.TrimSpace(checkAgain) != "OK" {
		fmt.Printf("⚠️ CNI bridge plugin still missing after install attempt; continuing without hard-fail.\n")
	}
	return nil
}

func BuildAndRunProject(ctx context.Context, onLine func(string)) (string, error) {
	projectPath := GetProjectPathWSL()
	if projectPath == "" {
		return "", fmt.Errorf("⚠️ Путь к проекту не настроен!\n\nПерейдите в Настройки и укажите путь к папке с docker-compose.yml")
	}
	if err := ensureCNIPluginsInstalled(); err != nil {
		return "", err
	}
	defer func() {
		if err := CleanupAfterBuild(); err != nil {
			fmt.Printf("⚠️ %v\n", err)
		}
	}()

	notify := func(text string) {
		fmt.Println(text)
		if onLine != nil {
			onLine("#1 [internal] " + text)
		}
	}

	notify("🔨 Сборка и запуск только через root (wsl -u root)...")
	notify("🔍 Запуск root BuildKit...")
	out, err := buildProjectImagesAsRoot(ctx, projectPath, onLine)
	if err != nil {
		if ctx.Err() != nil {
			return out, fmt.Errorf("⛔ Сборка отменена пользователем")
		}
		return out, formatBuildError(out, err)
	}
	notify("✅ Сборка завершена (root one-shot через wsl -u root)")
	notify("🚀 Запускаю стек через root (wsl -u root)...")
	return startProjectStackAsRoot(ctx, projectPath, onLine)
}

// formatBuildError упаковывает вывод сборки в человекочитаемую ошибку
// (последние 30 строк + статус).
func looksLikeBuildkitDaemonStartup(out string) bool {
	if strings.TrimSpace(out) == "" {
		return false
	}
	startupHints := []string{
		"found worker",
		"running server on",
		"using host network as the default",
		"failed to monitor changes",
		"failed check for fsverity support",
		"using default network",
		"buildkitd.sock",
	}

	hintsFound := 0
	for _, hint := range startupHints {
		if strings.Contains(strings.ToLower(out), hint) {
			hintsFound++
		}
	}
	if hintsFound == 0 {
		return false
	}

	buildErrorHints := []string{
		"error: failed to solve",
		"failed to solve",
		"executor failed running",
		"compose build",
		"=> [internal]",
		"build failed",
		"#1 [internal]",
		"#2 [internal]",
	}
	for _, hint := range buildErrorHints {
		if strings.Contains(strings.ToLower(out), hint) {
			return false
		}
	}

	return true
}

func formatBuildError(out string, err error) error {
	errorMsg := "❌ Ошибка сборки\n\n"
	if out != "" {
		lines := strings.Split(out, "\n")
		start := 0
		if len(lines) > 30 {
			start = len(lines) - 30
		}
		errorMsg += "Вывод (последние 30 строк):\n" + strings.Join(lines[start:], "\n") + "\n\n"
	} else {
		errorMsg += "WSL завершился с кодом ошибки, но в stdout/stderr не было полезного текста.\n"
		errorMsg += "Это часто означает: buildkitd не запустился, CNI/сеть не инициализирована, или команда compose завершилась без диагностик.\n\n"
		errorMsg += "Проверьте в WSL:\n"
		errorMsg += "  - buildctl debug workers\n"
		errorMsg += "  - ls -l /usr/lib/cni /opt/cni/bin 2>/dev/null\n"
		errorMsg += "  - nerdctl compose config\n\n"
	}
	if err != nil {
		errorMsg += "Статус: " + err.Error()
	}
	return fmt.Errorf("%s", errorMsg)
}

func formatLaunchError(err error, out string) error {
	base := "❌ Ошибка запуска:\n\n"
	if strings.TrimSpace(out) != "" {
		base += strings.TrimSpace(out) + "\n\n"
	} else {
		base += "Команда завершилась с ошибкой, но не вернула полезного вывода.\n"
		base += "Проверьте, что WSL-дистрибутив запущен, buildkitd/nerdctl доступен и не висит старый процесс.\n\n"
	}
	if err != nil {
		base += "Статус: " + err.Error() + "\n"
	}
	return fmt.Errorf("%s", base)
}

// startProjectStack поднимает стек проекта после сборки образов.
func startProjectStack(ctx context.Context, projectPath string, onLine func(string)) (string, error) {
	fmt.Println("🚀 Запуск стека через nerdctl compose...")
	startCmd := projectStackStartScript(projectPath, GetScriptsPath())
	result, err := func() (string, error) {
		if onLine != nil {
			return RunWSLWithCancelStream(ctx, startCmd, onLine)
		}
		return RunWSLWithCancel(ctx, startCmd)
	}()
	if err != nil {
		if ctx.Err() != nil {
			return result, fmt.Errorf("⛔ Запуск отменён пользователем")
		}
		return result, formatLaunchError(err, result)
	}
	CDInvalidateContainersCache()
	CDInvalidateImagesCache()
	return result, nil
}

func startProjectStackAsRoot(ctx context.Context, projectPath string, onLine func(string)) (string, error) {
	fmt.Println("🚀 Запуск стека через nerdctl compose (root)...")
	startScript := "set -e; " +
		`if [ "$(id -u)" != "0" ]; then echo "FATAL: root shell expected, got uid=$(id -u)" >&2; exit 1; fi; ` +
		projectStackStartScript(projectPath, GetScriptsPath())
	result, err := runWSLAsRootWithCancelStream(ctx, startScript, onLine)
	if err != nil {
		if ctx.Err() != nil {
			return result, fmt.Errorf("⛔ Запуск отменён пользователем")
		}
		return result, formatLaunchError(err, result)
	}
	CDInvalidateContainersCache()
	CDInvalidateImagesCache()
	return result, nil
}

func projectStackStartScript(projectPath, scriptsPath string) string {
	scriptsPath = NormalizeScriptsPath(projectPath, scriptsPath)
	composeFile := findProjectComposeFile(projectPath)
	if composeFile == "" {
		composeFile = path.Join(scriptsPath, "compose.yaml")
	}
	return fmt.Sprintf(`unset XDG_RUNTIME_DIR CONTAINERD_ROOTLESS_ROOTLESSKIT_FLAGS CONTAINERD_ROOTLESS_ROOTLESSKIT_STATE_DIR CONTAINERD_ROOTLESS_ROOTLESSKIT_NET CONTAINERD_ROOTLESS_ROOTLESSKIT_PORT_DRIVER;
export XDG_RUNTIME_DIR=/run/user/0; mkdir -p /run/user/0; chmod 0700 /run/user/0;
export CONTAINERD_ADDRESS=%s CONTAINERD_NAMESPACE=%s;
cd %s && if [ -f backend/config/.env ]; then set -a; . backend/config/.env; set +a; fi;
nerdctl() { command nerdctl --address "$CONTAINERD_ADDRESS" --namespace "$CONTAINERD_NAMESPACE" "$@"; };
compose_file=%s;
nerdctl compose -f "$compose_file" down --remove-orphans || true;
nerdctl compose -f "$compose_file" up -d;
for _svc in soul-dialogue-postgres soul-dialogue-redis soul-dialogue-backend soul-dialogue-worker soul-dialogue-frontend; do
    nerdctl inspect "$_svc" >/dev/null 2>&1 && nerdctl start "$_svc" 2>/dev/null || true;
done;
for attempt in $(seq 1 60); do
	postgres_id=$(nerdctl ps -q --filter label=com.docker.compose.service=postgres | head -n 1);
	redis_id=$(nerdctl ps -q --filter label=com.docker.compose.service=redis | head -n 1);
	postgres_state=$(nerdctl inspect --format '{{.State.Status}}' "$postgres_id" 2>/dev/null || true);
	redis_state=$(nerdctl inspect --format '{{.State.Status}}' "$redis_id" 2>/dev/null || true);
	postgres_health=$(nerdctl inspect --format '{{.State.Health.Status}}' "$postgres_id" 2>/dev/null || true);
	redis_health=$(nerdctl inspect --format '{{.State.Health.Status}}' "$redis_id" 2>/dev/null || true);
	if [ "$postgres_state" = running ] && [ "$redis_state" = running ] && { [ "$postgres_health" = healthy ] || [ -z "$postgres_health" ] || [ "$postgres_health" = starting ]; } && { [ "$redis_health" = healthy ] || [ -z "$redis_health" ] || [ "$redis_health" = starting ]; }; then
		break;
	fi;
	if [ "$attempt" = 60 ]; then echo 'База данных или Redis ещё не готовы, но контейнеры уже запущены; продолжаем запуск' >&2; break; fi;
    sleep 1;
done;
for attempt in $(seq 1 90); do
	backend_id=$(nerdctl ps -q --filter label=com.docker.compose.service=backend | head -n 1);
	worker_id=$(nerdctl ps -q --filter label=com.docker.compose.service=worker | head -n 1);
	backend_state=$(nerdctl inspect --format '{{.State.Status}}' "$backend_id" 2>/dev/null || true);
	worker_state=$(nerdctl inspect --format '{{.State.Status}}' "$worker_id" 2>/dev/null || true);
	backend_health=$(nerdctl inspect --format '{{.State.Health.Status}}' "$backend_id" 2>/dev/null || true);
	if [ "$backend_state" = running ] && [ "$worker_state" = running ] && { [ "$backend_health" = healthy ] || [ -z "$backend_health" ] || [ "$backend_health" = starting ]; }; then
		break;
	fi;
	if [ "$attempt" = 90 ]; then echo 'Backend/worker уже запущены, health ещё не выставлен; продолжаем без фатального exit' >&2; break; fi;
    sleep 1;
done
	nerdctl ps --format '{{.Names}}\t{{.Status}}'`,
		rootContainerdAddr, GetCdNamespace(), shellQuote(projectPath), shellQuote(composeFile))
}

// oneShotLaunchFailMarker — маркер в выводе скрипта: сам демон не поднялся
// (в отличие от реальной ошибки сборки, когда демон работал).
const oneShotLaunchFailMarker = "LAUNCH_FAIL:"

func buildProjectImagesOneShotRootScript(projectPath, composeFile string) string {
	environment := CurrentEnvironment()
	return buildProjectImagesOneShotRootScriptWithEnvironmentAndCNIPackage(projectPath, composeFile, environment, CNIPluginPackage())
}

func buildProjectImagesOneShotRootScriptWithEnvironment(projectPath, composeFile string, environment Environment) string {
	return buildProjectImagesOneShotRootScriptWithEnvironmentAndCNIPackage(
		projectPath,
		composeFile,
		environment,
		CNIPluginPackageForManager(environment.PkgManager),
	)
}

func buildProjectImagesOneShotRootScriptWithEnvironmentAndCNIPackage(projectPath, composeFile string, environment Environment, cniPackage string) string {
	cniInstallCommand, err := cniPluginInstallCommand(environment, cniPackage, "")
	if err != nil {
		cniInstallCommand = ":"
	}
	return fmt.Sprintf(`
set -e
export PATH="/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin:${PATH}"

# Сборка выполняется только от root через wsl -u root.
if [ "$(id -u)" != "0" ]; then
    echo "FATAL: build script must run as root (current uid=$(id -u), user=$(id -un))" >&2
    exit 1
fi

# Используем только системный containerd и его namespace.

SOCK_DIR="/run/buildkit"
SOCK="$SOCK_DIR/buildkitd.sock"
mkdir -p "$SOCK_DIR"
chmod 777 "$SOCK_DIR"

# BuildKit без CNI-плагинов не может создать default bridge-сеть,
# но отсутствие bridge не должно ломать весь проект у всех пользователей.
if [ ! -x /opt/cni/bin/bridge ] && [ ! -x /usr/lib/cni/bridge ] && [ ! -x /usr/libexec/cni/bridge ]; then
	%[7]s >/dev/null 2>&1 || true
fi
if [ ! -x /opt/cni/bin/bridge ] && [ ! -x /usr/lib/cni/bridge ] && [ ! -x /usr/libexec/cni/bridge ]; then
    echo "WARN_CNI_MISSING: bridge plugin not found, continuing without hard-fail"
fi

# Убиваем "зависшие" с прошлых запусков процессы buildkitd — иначе новый
# демон не сможет захватить flock на /var/lib/buildkit/buildkitd.lock.
for _p in $(ps -eo pid=,comm= 2>/dev/null | awk '$2=="buildkitd" {print $1}'); do
    kill "$_p" 2>/dev/null || true
done
pkill -x buildkitd 2>/dev/null || true
for _i in $(seq 1 10); do
    pgrep -x buildkitd >/dev/null 2>&1 || break
    sleep 0.5
done
pkill -9 -x buildkitd 2>/dev/null || true

rm -f "$SOCK"
BK_BIN=""
if [ -x /usr/local/bin/buildkitd ]; then
	BK_BIN=/usr/local/bin/buildkitd
fi
for _candidate in \
    /usr/local/bin/buildkitd \
    /usr/bin/buildkitd \
    /usr/local/sbin/buildkitd \
    /usr/sbin/buildkitd \
    /usr/lib/buildkit/bin/buildkitd \
    /usr/libexec/buildkit/buildkitd \
    /usr/libexec/buildkit/bin/buildkitd \
    /opt/buildkit/bin/buildkitd \
    /opt/buildkitd/bin/buildkitd; do
	if [ -z "$BK_BIN" ] && [ -f "$_candidate" ] && [ -x "$_candidate" ]; then
		BK_BIN="$_candidate"
        break
    fi
done
if [ -z "$BK_BIN" ]; then
    for _root in /usr /opt /root /home; do
        [ -d "$_root" ] || continue
        _found="$(find "$_root" -type f -name buildkitd 2>/dev/null | head -n 1 || true)"
        if [ -n "$_found" ] && [ -x "$_found" ]; then
            BK_BIN="$(readlink -f "$_found" 2>/dev/null || echo "$_found")"
            break
        fi
    done
fi
if [ -z "$BK_BIN" ] && command -v buildkitd >/dev/null 2>&1; then
	BK_BIN="$(command -v buildkitd 2>/dev/null || true)"
fi
BCTL_BIN=""
for _candidate in \
    /usr/local/bin/buildctl \
    /usr/bin/buildctl \
    /usr/local/sbin/buildctl \
    /usr/sbin/buildctl \
    /usr/lib/buildkit/bin/buildctl \
    /usr/libexec/buildkit/buildctl \
    /usr/libexec/buildkit/bin/buildctl \
    /opt/buildkit/bin/buildctl \
    /opt/buildkitd/bin/buildctl; do
    if [ -e "$_candidate" ] && [ -x "$_candidate" ]; then
		BCTL_BIN="$_candidate"
        break
    fi
done
if [ -z "$BCTL_BIN" ] && command -v buildctl >/dev/null 2>&1; then
    BCTL_BIN="$(readlink -f "$(command -v buildctl 2>/dev/null || true)" 2>/dev/null || command -v buildctl 2>/dev/null || true)"
fi
[ -n "$BK_BIN" ] && [ -x "$BK_BIN" ] || { echo "%[1]s root buildkitd not found in PATH"; exit 127; }
[ -n "$BCTL_BIN" ] && [ -x "$BCTL_BIN" ] || { echo "%[1]s root buildctl not found in PATH"; exit 127; }
"$BK_BIN" \
    --addr "unix://$SOCK" \
	--config /dev/null \
    --root /var/lib/buildkit \
	--oci-worker=true \
	--containerd-worker=false \
	--containerd-worker-addr /run/containerd/containerd.sock \
    --oci-worker-net=host \
    >/tmp/buildkitd-root.log 2>&1 &
BK_PID=$!
cleanup() { kill "$BK_PID" 2>/dev/null; wait "$BK_PID" 2>/dev/null || true; }
trap cleanup EXIT INT TERM
for _ in $(seq 1 40); do
    [ -S "$SOCK" ] && break
    kill -0 "$BK_PID" 2>/dev/null || { echo "%[1]s root buildkitd crashed during startup"; tail -n 40 /tmp/buildkitd-root.log; exit 1; }
    sleep 0.5
done
[ -S "$SOCK" ] || { echo "%[1]s root buildkitd socket did not appear within 20s"; tail -n 40 /tmp/buildkitd-root.log; exit 1; }
for _ in $(seq 1 20); do
    "$BCTL_BIN" --addr "unix://$SOCK" debug workers >/dev/null 2>&1 && break
    sleep 0.5
done
"$BCTL_BIN" --addr "unix://$SOCK" debug workers >/dev/null 2>&1 || { echo "%[1]s root buildkitd did not become ready"; tail -n 40 /tmp/buildkitd-root.log; exit 1; }

export BUILDKIT_HOST="unix://$SOCK"
export BUILDKIT_STEP_LOG_MAX_SIZE=10000000 BUILDKIT_STEP_LOG_MAX_SPEED=1000000

cd %[2]s
nerdctl --address %[4]s --namespace %[5]s compose -f %[3]s build --progress=plain
%[6]s
`, oneShotLaunchFailMarker, shellQuote(projectPath), shellQuote(composeFile), rootContainerdAddr, shellQuote(GetCdNamespace()), buildkitCleanupScript(), cniInstallCommand)
}

func buildkitCleanupScript() string {
	ttl := GetBuildkitCacheTTL()
	maxSize := GetBuildkitMaxSize()
	var commands []string
	if ttl > 0 {
		commands = append(commands, fmt.Sprintf(`echo "BuildKit: удаление кэша старше %dh"; "$BCTL_BIN" --addr "unix://$SOCK" prune --all --filter "until<%dh" || true`, ttl, ttl))
	}
	if maxSize != "" {
		commands = append(commands, fmt.Sprintf(`echo "BuildKit: ограничение кэша %s"; limit_str=%s; limit_bytes=0; case "$limit_str" in *[0-9]g) limit_bytes=$(( ${limit_str%%g} * 1024 * 1024 * 1024 )) ;; *[0-9]m) limit_bytes=$(( ${limit_str%%m} * 1024 * 1024 )) ;; *[0-9]k) limit_bytes=$(( ${limit_str%%k} * 1024 )) ;; esac; [ "$limit_bytes" -gt 0 ] && "$BCTL_BIN" --addr "unix://$SOCK" prune --all --keep-storage="$limit_bytes" || true`, maxSize, shellQuote(maxSize)))
	}
	if len(commands) == 0 {
		return `echo "BuildKit: автоматическая очистка отключена"`
	}
	return strings.Join(commands, "\n")
}

func buildProjectImagesAsRoot(ctx context.Context, projectPath string, onLine func(string)) (string, error) {
	composeFile := findProjectComposeFile(projectPath)
	if composeFile == "" {
		composeFile = path.Join(NormalizeScriptsPath(projectPath, GetScriptsPath()), "compose.yaml")
	}
	script := buildProjectImagesOneShotRootScript(projectPath, composeFile)
	return runWSLAsRootWithCancelStream(ctx, script, onLine)
}

func GetDBInfo(volumeName string) (string, []string, error) {
	return CDGetDBInfo(volumeName)
}

func ListImages() ([]Image, error) {
	return CDListImages()
}

func RemoveImage(id string) error {
	err := CDRemoveImage(id)
	if err == nil {
		CDInvalidateImagesCache()
		ClearImageSizeCache()
	}
	return err
}

func ListVolumes() ([]Volume, error) {
	return CDListVolumes()
}

func RemoveVolume(name string) error {
	return CDRemoveVolume(name)
}

func GetContainerLogs(id string, tail int) (string, error) {
	return CDGetContainerLogs(id, tail)
}

func GetContainerStartupLogs(id string, tail int) (string, error) {
	return CDGetContainerStartupLogs(id, tail)
}

var statusCache = newBoundedStringCache(1*time.Hour, 500)

func TranslateStatus(status string) string {
	status = strings.ToLower(strings.TrimSpace(status))
	if translated, ok := statusCache.Get(status); ok {
		return translated
	}
	var result string
	switch {
	case strings.Contains(status, "healthy"):
		result = i18n.T("container_status.healthy")
	case strings.Contains(status, "unhealthy"):
		result = i18n.T("container_status.unhealthy")
	case strings.Contains(status, "running") || strings.Contains(status, "up"):
		result = i18n.T("container_status.running")
	case strings.Contains(status, "created"):
		result = i18n.T("container_status.created")
	case strings.Contains(status, "restarting"):
		result = i18n.T("container_status.restarting")
	case strings.Contains(status, "removing"):
		result = i18n.T("container_status.removing")
	case strings.Contains(status, "paused"):
		result = i18n.T("container_status.paused")
	case strings.Contains(status, "exited"):
		result = i18n.T("container_status.exited")
	case strings.Contains(status, "dead"):
		result = i18n.T("container_status.dead")
	default:
		result = status
	}
	statusCache.Set(status, result)
	return result
}

func ClearContainerLogs(id string) error {
	return CDClearContainerLogs(id)
}

func CleanContainerdLogs() (string, error) {
	out, err := runWSLAsRootWithTimeout(containerdLogsCleanupCommand(), TimeoutMedium)
	return out, err
}

func containerdLogsCleanupCommand() string {
	return `
find /var/log -type f -name '*.log' -mtime +7 -delete 2>/dev/null
if command -v logrotate >/dev/null 2>&1 && [ -f /etc/logrotate.conf ]; then
	logrotate -s /run/logrotate.status /etc/logrotate.conf 2>/dev/null || true
	log_status='logrotate applied'
else
	log_status='logrotate unavailable'
fi
find /var/lib/nerdctl -type f -name '*-json.log' -size +50M -print 2>/dev/null | while IFS= read -r log; do
	tail -c 52428800 "$log" > "$log.trim" && mv "$log.trim" "$log"
done
printf 'Logs limited: %s; container logs 50M\n' "$log_status"
`
}

var cleanCache = struct {
	sync.RWMutex
	data      string
	err       error
	timestamp time.Time
}{
	timestamp: time.Now().Add(-20 * time.Second),
}

func CleanNerdctlCache() (string, error) {
	cleanCache.RLock()
	if cleanCache.data != "" && time.Since(cleanCache.timestamp) < 10*time.Second {
		result := cleanCache.data
		err := cleanCache.err
		cleanCache.RUnlock()
		return result, err
	}
	cleanCache.RUnlock()

	if !CheckBuildkitd() {
		err := fmt.Errorf("BuildKitd не запущен: запустите BuildKitd и повторите очистку кэша")
		cleanCache.Lock()
		cleanCache.data = ""
		cleanCache.err = err
		cleanCache.timestamp = time.Now()
		cleanCache.Unlock()
		return "", err
	}

	res, err := runWSLAsRootWithTimeout("nerdctl system prune --force 2>&1", 120*time.Second)
	res = CleanCleanupOutput(res)
	if err == nil && strings.TrimSpace(res) == "" {
		res = "Система чиста — нечего удалять"
	}
	cleanCache.Lock()
	cleanCache.data = res
	cleanCache.err = err
	cleanCache.timestamp = time.Now()
	cleanCache.Unlock()
	return res, err
}

func CleanUnusedVolumes(ctx context.Context) (string, error) {
	unused, err := listUnusedVolumes(ctx)
	if err != nil {
		return "", fmt.Errorf("не удалось получить список томов через wsl.exe: %w", err)
	}

	var lines []string
	removed, skipped := 0, 0
	for _, volume := range unused {
		select {
		case <-ctx.Done():
			return strings.Join(lines, "\n"), ctx.Err()
		default:
		}

		if IsProtectedVolumeName(volume.Name) {
			lines = append(lines, "Пропущен защищённый том: "+volume.Name)
			skipped++
			continue
		}

		// Путь читаем до удаления: после volume rm том исчезает из списка,
		// и остатки на диске чистить будет уже нечем.
		mountpoint := volume.Mountpoint

		// nerdctl volume rm -f отказывается удалять занятый том — это и есть
		// защита от потери данных живого контейнера.
		if _, err := runRootNerdctl(ctx, volumeRemoveCommand(volume.Name)); err != nil {
			if ctx.Err() != nil {
				return strings.Join(lines, "\n"), ctx.Err()
			}
			lines = append(lines, "Пропущен том (используется): "+volume.Name)
			skipped++
			continue
		}

		removed++
		lines = append(lines, "Удалён том: "+volume.Name)
		for _, path := range volumeRemovalTargets(volume.Name, mountpoint) {
			if _, err := runWSLAsRootWithTimeout("if [ -e "+shellQuote(path)+" ]; then rm -rf "+shellQuote(path)+"; fi; true", TimeoutMedium); err != nil {
				lines = append(lines, "Не удалось удалить файлы тома "+volume.Name+": "+err.Error())
			}
		}
	}

	InvalidateWSLCache()
	CDInvalidateVolumesCache()
	CDInvalidateContainersCache()
	GlobalCacheManager.Invalidate(CacheEventVolumes, "clean-unused-volumes")

	if removed == 0 && skipped == 0 {
		return "Неиспользуемые тома не найдены", nil
	}
	return strings.Join(lines, "\n"), nil
}

func CleanUnusedNetworks(ctx context.Context) (string, error) {
	script := unusedNetworkCleanupScript()
	result, err := runWSLAsRootWithCancelStream(ctx, script, nil)
	result = CleanCleanupOutput(result)
	if err != nil {
		return result, err
	}
	CDInvalidateContainersCache()
	return result, nil
}

var protectedNetworkPrefixes = []string{"soul-dialogue", "containerd_soul-dialogue"}

func unusedNetworkCleanupScript() string {
	var protectedCases []string
	for _, prefix := range protectedNetworkPrefixes {
		protectedCases = append(protectedCases, prefix, prefix+"-*")
	}
	return fmt.Sprintf(`
all_nets=$(nerdctl network ls --format '{{.Name}}' 2>/dev/null)
if [ -z "$all_nets" ]; then
	echo "Нет сетей для очистки"
	exit 0
fi
used_nets=$(nerdctl ps -a --format '{{json .}}' 2>/dev/null | \
	awk -F'"NetworkMode":' '{if(NF>1){gsub(/[^a-zA-Z0-9_.-]/,"",$2);print $2}}' | \
	awk -F'"Networks":' '{if(NF>1){gsub(/[{}\[\]" ]/,"",$2);print $2}}' | \
	tr ',' '\n' | sort -u)
skip_nets="bridge host none default"
removed=0
for net in $all_nets; do
	net=$(echo "$net" | xargs)
	[ -z "$net" ] && continue
	case "$net" in
		%s)
			echo "Пропущена защищённая сеть: $net"
			continue
			;;
	esac
	skip=0
	for s in $skip_nets; do
		[ "$net" = "$s" ] && skip=1
	done
	[ $skip -eq 1 ] && continue
	if echo "$used_nets" | grep -qx "$net"; then
		continue
	fi
	if nerdctl network rm "$net" >/dev/null 2>&1; then
		echo "Удалена сеть: $net"
		removed=$((removed+1))
	else
		echo "Не удалось удалить сеть: $net"
	fi
done
if [ $removed -eq 0 ]; then
	echo "Неиспользуемые сети не найдены"
else
	echo "Удалено неиспользуемых сетей: $removed"
fi
	`, strings.Join(protectedCases, "|"))
}

func CleanUntaggedImages(ctx context.Context) (string, error) {
	script := `
untagged=$(nerdctl images --format '{{.ID}}\t{{.Repository}}\t{{.Tag}}' 2>&1 | \
	awk -F'\t' '($2 == "<none>" || $3 == "<none>") && $1 != "" {print $1}')
if [ -z "$untagged" ]; then
	echo "Образы без тегов не найдены"
	exit 0
fi
removed=0
for img_id in $untagged; do
	if nerdctl rmi -f "$img_id" >/dev/null 2>&1; then
		echo "Удалён образ: $img_id"
		removed=$((removed+1))
	else
		echo "Не удалось удалить образ: $img_id"
	fi
done
if [ $removed -gt 0 ]; then
	echo "Удалено образов без тегов: $removed"
else
	echo "Не удалось удалить ни одного образа"
fi
`
	result, err := runWSLAsRootWithCancelStream(ctx, script, nil)
	result = CleanCleanupOutput(result)
	if err != nil {
		return result, err
	}
	if strings.Contains(result, "Удалено образов") {
		CDInvalidateImagesCache()
		ClearImageSizeCache()
	}
	return result, nil
}

func GetHostResources() (string, error) {
	return RunWSL("free -h | grep Mem && echo '---' && cat /proc/loadavg")
}

func GetStats() ([]ContainerStat, error) {
	return CDGetStats()
}

type SystemResources struct {
	RAMTotal  string
	RAMUsed   string
	RAMFree   string
	CPUCores  string
	CPULoad   string
	DiskTotal string
	DiskUsed  string
	DiskFree  string
}

var sysResCache = struct {
	sync.RWMutex
	data      *SystemResources
	timestamp time.Time
}{
	timestamp: time.Now().Add(-10 * time.Second),
}

func GetSystemResources() (*SystemResources, error) {
	sysResCache.RLock()
	if sysResCache.data != nil && time.Since(sysResCache.timestamp) < 5*time.Second {
		result := *sysResCache.data
		sysResCache.RUnlock()
		return &result, nil
	}
	sysResCache.RUnlock()

	out, err := RunWSL(
		"free -h | grep Mem && echo '---CPU---' && awk '/^processor[[:space:]]*:/ { count++ } END { print count+0 }' /proc/cpuinfo && cat /proc/loadavg && echo '---DISK---' && df -h / | tail -1",
	)
	if err != nil {
		return nil, err
	}
	parts := strings.Split(out, "---CPU---")
	if len(parts) < 2 {
		return nil, fmt.Errorf("не удалось распарсить системные ресурсы")
	}
	ramFields := strings.Fields(strings.TrimSpace(parts[0]))
	var ramTotal, ramUsed, ramFree string
	if len(ramFields) >= 4 {
		ramTotal = ramFields[1]
		ramUsed = ramFields[2]
		ramFree = ramFields[3]
	}
	cpuParts := strings.Split(parts[1], "---DISK---")
	var cpuCores, cpuLoad string
	if len(cpuParts) >= 1 {
		cpuLines := strings.Split(strings.TrimSpace(cpuParts[0]), "\n")
		if len(cpuLines) >= 2 {
			cpuCores = strings.TrimSpace(cpuLines[0])
			loadFields := strings.Fields(cpuLines[1])
			if len(loadFields) >= 3 {
				cpuLoad = loadFields[0] + " " + loadFields[1] + " " + loadFields[2]
			}
		}
	}
	var diskTotal, diskUsed, diskFree string
	if len(cpuParts) >= 2 {
		diskFields := strings.Fields(strings.TrimSpace(cpuParts[1]))
		if len(diskFields) >= 5 {
			diskTotal = diskFields[1]
			diskUsed = diskFields[2]
			diskFree = diskFields[3]
		}
	}
	result := &SystemResources{
		RAMTotal:  ramTotal,
		RAMUsed:   ramUsed,
		RAMFree:   ramFree,
		CPUCores:  cpuCores,
		CPULoad:   cpuLoad,
		DiskTotal: diskTotal,
		DiskUsed:  diskUsed,
		DiskFree:  diskFree,
	}
	sysResCache.Lock()
	sysResCache.data = result
	sysResCache.timestamp = time.Now()
	sysResCache.Unlock()
	return result, nil
}

var dateCache = newBoundedStringCache(1*time.Hour, 500)

func FormatDateShort(dateStr string) string {
	if formatted, ok := dateCache.Get(dateStr); ok {
		return formatted
	}
	formatted := formatDateShort(dateStr)
	dateCache.Set(dateStr, formatted)
	return formatted
}

func formatDateShort(dateStr string) string {
	t, err := time.Parse("2006-01-02 15:04:05 -0700", dateStr)
	if err == nil {
		return t.Format("2006-01-02 15:04")
	}
	t, err = time.Parse(time.RFC3339, dateStr)
	if err == nil {
		return t.Format("2006-01-02 15:04")
	}
	if len(dateStr) > 16 {
		return dateStr[:16]
	}
	return dateStr
}

func GetContainerConfig(id string) (*ContainerConfig, error) {
	return CDGetContainerConfig(id)
}

func UpdateContainerImage(id string, newImage string) (string, error) {
	var logs []string
	logs = append(logs, "📋 Получение конфигурации контейнера...")
	config, err := GetContainerConfig(id)
	if err != nil {
		return strings.Join(logs, "\n"), fmt.Errorf("не удалось получить конфигурацию: %w", err)
	}
	logs = append(logs, "✅ Конфигурация получена: "+config.Name)

	logs = append(logs, "🛑 Остановка контейнера...")
	if err := CDStopContainer(id); err != nil {
		logs = append(logs, "⚠️ Не удалось остановить через gRPC, пробуем принудительно...")
		_, _ = RunWSL(fmt.Sprintf("nerdctl kill %s 2>/dev/null", shellQuote(id)))
		time.Sleep(1 * time.Second)
	}

	logs = append(logs, "🗑️ Удаление старого контейнера...")
	if err := CDRemoveContainer(id); err != nil {
		_, err = RunWSL(fmt.Sprintf("nerdctl rm -f %s 2>/dev/null", shellQuote(id)))
		if err != nil {
			return strings.Join(logs, "\n"), fmt.Errorf("не удалось удалить контейнер: %w", err)
		}
	}

	if config.Image != "" && config.Image != newImage {
		logs = append(logs, "🗑️ Удаление старого образа...")
		if err := CDRemoveImage(config.Image); err != nil {
			logs = append(logs, "⚠️ Не удалось удалить старый образ — продолжим")
		}
	}
	CDInvalidateContainersCache()
	CDInvalidateImagesCache()

	logs = append(logs, "⬇️  Загрузка нового образа: "+newImage)
	pullOut, err := RunWSL(fmt.Sprintf("nerdctl pull %s", newImage))
	if err != nil {
		return strings.Join(logs, "\n"), fmt.Errorf("не удалось загрузить образ: %w", err)
	}
	if pullOut != "" {
		lines := strings.Split(pullOut, "\n")
		for _, line := range lines {
			if strings.Contains(line, "Pulling") || strings.Contains(line, "Downloading") {
				logs = append(logs, "  "+line)
			}
		}
	}
	logs = append(logs, "✅ Образ загружен")

	logs = append(logs, "🔄 Пересоздание контейнера...")
	runCmd := fmt.Sprintf("nerdctl run -d --name %s", config.Name)
	for _, vol := range config.Volumes {
		runCmd += fmt.Sprintf(" -v %s", vol)
	}
	if config.Ports != "" {
		runCmd += fmt.Sprintf(" -p %s", config.Ports)
	}
	for _, env := range config.Env {
		runCmd += fmt.Sprintf(" -e %s", env)
	}
	for k, v := range config.Labels {
		runCmd += fmt.Sprintf(" --label %s=%s", k, v)
	}
	if config.Network != "" && config.Network != "default" {
		runCmd += fmt.Sprintf(" --network %s", config.Network)
	}
	if cpu := GetDefaultCPU(); cpu != "" {
		runCmd += fmt.Sprintf(" --cpus=%s", cpu)
	}
	if mem := GetDefaultMemory(); mem != "" {
		runCmd += fmt.Sprintf(" --memory=%s", mem)
	}
	runCmd += " " + newImage
	logs = append(logs, "🚀 Выполняю: "+runCmd)
	_, err = RunWSL(runCmd)
	if err != nil {
		return strings.Join(logs, "\n"), fmt.Errorf("не удалось пересоздать контейнер: %w", err)
	}
	logs = append(logs, "✅ Контейнер пересоздан успешно!")
	return strings.Join(logs, "\n"), nil
}

func CleanBuildkitCache(ctx context.Context) (string, error) {
	ttl := GetBuildkitCacheTTL()
	maxSize := GetBuildkitMaxSize()
	if ttl == 0 && maxSize == "" {
		return "Очистка кэша BuildKit отключена в настройках", nil
	}
	startedForCleanup := false
	if !CheckBuildkitd() {
		if err := StartBuildkitdAsRoot(); err != nil {
			return "", fmt.Errorf("не удалось временно запустить BuildKit для очистки: %w", err)
		}
		startedForCleanup = true
	}
	if startedForCleanup {
		defer StopBuildkitd()
	}

	addr := BuildkitHostAddr()
	dataDir := "/var/lib/buildkit"

	if addr != defaultBuildkitAddr && strings.Contains(addr, "/buildkit/buildkitd.sock") {
		dataDir = "$HOME/.local/share/buildkit"
	}
	script := fmt.Sprintf(`
echo "🔨 Очистка кэша BuildKit"
echo "========================"
addr=%s
data_dir=%s
%s
echo ""
echo "🧹 Очистка неиспользуемых ресурсов..."
out=$(buildctl --addr "$addr" prune --all 2>/dev/null)
if [ -n "$out" ]; then
	echo "  $out"
else
	echo "  ✅ Нечего очищать"
fi
echo ""
echo "📊 Размер кэша BuildKit:"
du -sh "$data_dir" 2>/dev/null || echo "  Кэш не найден"
%s
`,
		shellQuote(addr),

		`"`+dataDir+`"`,
		func() string {
			if ttl > 0 {
				return fmt.Sprintf(`
echo ""
echo "🧹 Очистка кэша старше %d часов..."
out=$(buildctl --addr "$addr" prune --filter "until<%dh" --all 2>/dev/null)
if [ -n "$out" ]; then
	echo "  $out"
else
	echo "  ✅ Кэш очищен"
fi`, ttl, ttl)
			}
			return "echo \"Пропуск очистки по времени (отключено)\""
		}(),
		func() string {
			if maxSize == "" {
				return "echo \"Пропуск ограничения размера (не задано)\""
			}
			return fmt.Sprintf(`
echo ""
echo "⚙️ Ограничение кэша: %s"
size_bytes=$(du -sb "$data_dir" 2>/dev/null | awk '{print $1}' || echo 0)
limit_str="%s"
limit_bytes=0
case "$limit_str" in
  *[0-9]g) limit_bytes=$(( ${limit_str%%g} * 1024 * 1024 * 1024 )) ;;
  *[0-9]m) limit_bytes=$(( ${limit_str%%m} * 1024 * 1024 )) ;;
  *[0-9]k) limit_bytes=$(( ${limit_str%%k} * 1024 )) ;;
esac
if [ "$size_bytes" -gt "$limit_bytes" ] && [ "$limit_bytes" -gt 0 ]; then
	echo "  ⚠️ Кэш превышает лимит! Принудительная очистка..."
	buildctl --addr "$addr" prune --all --keep-storage="$limit_bytes" 2>/dev/null
else
	echo "  ✅ Кэш в пределах лимита"
fi`, maxSize, maxSize)
		}(),
	)
	results, err := RunWSLWithCancel(ctx, script)
	if err != nil {
		return results, err
	}
	return results, nil
}
