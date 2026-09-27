package wsl

import (
	"path/filepath"
	"strings"
	"testing"
	"time"

	"golang.org/x/text/encoding/charmap"
)

func TestCacheManagerInvalidateDoesNotRecurse(t *testing.T) {
	events := []CacheEventType{
		CacheEventContainers,
		CacheEventImages,
		CacheEventVolumes,
		CacheEventStats,
	}

	for _, eventType := range events {
		GlobalCacheManager.Invalidate(eventType, "test")
	}
}

func TestValidateDomainRejectsShellMetacharacters(t *testing.T) {
	bad := []string{
		"example.com;id",
		"example.com$(whoami)",
		"example.com|cat /etc/passwd",
		"example.com`uname -a`",
	}
	for _, d := range bad {
		if err := ValidateDomain(d); err == nil {
			t.Fatalf("ValidateDomain(%q) should reject dangerous input", d)
		}
	}

	if err := ValidateDomain("example.com"); err != nil {
		t.Fatalf("ValidateDomain(example.com) should pass: %v", err)
	}
}

func TestValidateRoutePrefixRejectsShellMetacharacters(t *testing.T) {
	bad := []string{
		"/api;rm -rf /",
		"/api$(whoami)",
		"/api|nc 127.0.0.1 4444",
		"/api`id`",
		"/api&&echo pwned",
	}
	for _, p := range bad {
		if err := validateRoutePrefix(p); err == nil {
			t.Fatalf("validateRoutePrefix(%q) should reject dangerous input", p)
		}
	}

	if err := validateRoutePrefix("/api/v1"); err != nil {
		t.Fatalf("validateRoutePrefix(/api/v1) should pass: %v", err)
	}
}

func TestValidateACMEEmail(t *testing.T) {
	if err := ValidateACMEEmail("admin@example.com"); err != nil {
		t.Fatalf("ValidateACMEEmail(admin@example.com) should pass: %v", err)
	}
	if err := ValidateACMEEmail("not-an-email"); err == nil {
		t.Fatal("ValidateACMEEmail(not-an-email) should reject invalid email")
	}
	if err := ValidateACMEEmail("  admin@example.com  "); err != nil {
		t.Fatalf("ValidateACMEEmail should trim whitespace: %v", err)
	}
}

func TestParseComposeServiceStatuses(t *testing.T) {
	statusJSON := `[
		{"Service":"backend","State":"running"},
		{"Service":"frontend","State":"exited (0)"},
		{"Service":"traefik","State":"up 3 minutes"}
	]`

	statuses := parseComposeServiceStatuses(statusJSON)
	if got := statuses["backend"]; !strings.HasPrefix(strings.ToLower(got), "running") {
		t.Fatalf("backend should be running, got %q", got)
	}
	if got := statuses["frontend"]; strings.HasPrefix(strings.ToLower(got), "running") {
		t.Fatalf("frontend should not look running, got %q", got)
	}
	if got := statuses["traefik"]; !strings.HasPrefix(strings.ToLower(got), "up") {
		t.Fatalf("traefik should be up, got %q", got)
	}
}

func TestValidateProjectComposeNetwork(t *testing.T) {
	valid := `services:
  backend:
    image: test
    networks:
      - soul-dialogue
networks:
  soul-dialogue:
    external: true
    name: soul-dialogue
`
	if err := validateProjectComposeNetworkFromText(valid); err != nil {
		t.Fatalf("valid compose should pass: %v", err)
	}

	invalid := `services:
  backend:
    image: test
`
	if err := validateProjectComposeNetworkFromText(invalid); err == nil {
		t.Fatal("compose without soul-dialogue network should fail validation")
	}
}

func TestRenderedComposeUsesRelativePathsFromComposeDirectory(t *testing.T) {
	traefik, err := renderTraefikCompose("admin@example.com")
	if err != nil {
		t.Fatalf("renderTraefikCompose() unexpected error: %v", err)
	}
	if strings.Contains(traefik, "./.containerd-data/traefik/") {
		t.Fatalf("traefik compose should not use nested .containerd-data path, got:\n%s", traefik)
	}
	if !strings.Contains(traefik, "./traefik/dynamic.yml") || !strings.Contains(traefik, "./traefik/acme.json") {
		t.Fatalf("traefik compose should mount files relative to .containerd-data dir, got:\n%s", traefik)
	}

	cloudflare, err := renderCloudflareCompose()
	if err != nil {
		t.Fatalf("renderCloudflareCompose() unexpected error: %v", err)
	}
	if strings.Contains(cloudflare, "./.containerd-data/cloudflare/") {
		t.Fatalf("cloudflare compose should not use nested .containerd-data path, got:\n%s", cloudflare)
	}
	if !strings.Contains(cloudflare, "./cloudflare/config.json") || !strings.Contains(cloudflare, "./cloudflare/credentials.json") {
		t.Fatalf("cloudflare compose should mount files relative to .containerd-data dir, got:\n%s", cloudflare)
	}
}

func TestLooksLikeBuildkitDaemonStartup(t *testing.T) {
	startupLog := `time="2026-09-14T19:52:57+00:00" level=info msg="found worker \"/var/lib...\""
level=info msg="running server on /run/buildkit/buildkitd.sock"
level=warning msg="failed to monitor changes: no such file or directory"`
	if !looksLikeBuildkitDaemonStartup(startupLog) {
		t.Fatalf("startup log should be recognized as BuildKit daemon startup")
	}

	buildErrorLog := `#1 [internal] load metadata for docker.io/library/python:3.11
#2 ERROR: failed to solve: python:3.11: pull access denied`
	if looksLikeBuildkitDaemonStartup(buildErrorLog) {
		t.Fatalf("real build error should not look like daemon startup")
	}

}

func TestBuildkitStopScriptKillsProcessesAndSockets(t *testing.T) {
	script := buildkitStopScript()
	for _, want := range []string{"pkill -x buildkitd", "ps -eo pid=,comm=", "/run/buildkit/buildkitd.sock"} {
		if !strings.Contains(script, want) {
			t.Fatalf("stop script should contain %q, got:\n%s", want, script)
		}
	}
}

func TestBuildkitStartScriptWaitsForReadiness(t *testing.T) {
	rootScript := buildkitdRootLaunchScript
	if !strings.Contains(rootScript, `buildctl --addr unix:///run/buildkit/buildkitd.sock debug workers`) {
		t.Fatalf("root launch script should wait for BuildKit readiness via the resolved binary")
	}
	if !strings.Contains(rootScript, "socket did not become ready within 20s") {
		t.Fatalf("root launch script should report readiness timeout clearly")
	}
}

func TestBuildkitStartScriptSearchesCommonInstallLocations(t *testing.T) {
	script := buildkitdRootLaunchScript
	for _, want := range []string{
		"if ! command -v buildkitd",
		"if ! command -v buildctl",
		"setsid nohup buildkitd",
		"buildctl --addr unix:///run/buildkit/buildkitd.sock debug workers",
	} {
		if !strings.Contains(script, want) {
			t.Fatalf("root launch script should resolve administrator binaries, missing %q; got:\n%s", want, script)
		}
	}
}

func TestBuildProjectImagesOneShotUsesRootShell(t *testing.T) {
	script := buildProjectImagesOneShotRootScript("/tmp/project", "/tmp/project/compose.yaml")
	if strings.Contains(script, "--oci-worker-no-process-sandbox") || strings.Contains(script, "--oci-worker-snapshotter=native") {
		t.Fatalf("root build script should not use user-worker flags; got:\n%s", script)
	}
	if !strings.Contains(script, "--root /var/lib/buildkit") {
		t.Fatalf("one-shot root build script should use root buildkit root directory; got:\n%s", script)
	}
	if !strings.Contains(script, "--oci-worker=true") || !strings.Contains(script, "--containerd-worker=false") {
		t.Fatalf("one-shot root build script should disable the rootless-sensitive containerd worker; got:\n%s", script)
	}
	if !strings.Contains(script, "--config /dev/null") || !strings.Contains(script, "--containerd-worker-addr /run/containerd/containerd.sock") {
		t.Fatalf("one-shot root build script should use explicit BuildKit configuration; got:\n%s", script)
	}
	if !strings.Contains(script, "\"$BCTL_BIN\" --addr \"unix://$SOCK\" debug workers") {
		t.Fatalf("one-shot root build script should wait for daemon readiness via resolved buildctl; got:\n%s", script)
	}
	if !strings.Contains(script, "containernetworking-plugins") {
		t.Fatalf("one-shot root build script should install missing CNI plugins before build; got:\n%s", script)
	}
	if !strings.Contains(script, "/usr/lib/cni/bridge") || !strings.Contains(script, "/usr/libexec/cni/bridge") {
		t.Fatalf("one-shot root build script should accept Debian CNI plugin locations; got:\n%s", script)
	}
	if !strings.Contains(script, "nerdctl --address unix:///run/containerd/containerd.sock --namespace 'default' compose -f") {
		t.Fatalf("root one-shot build script should include compose build; got:\n%s", script)
	}
}

func TestRootWSLSetupResetsUserRuntimeEnvironment(t *testing.T) {
	envSetup := rootWSLSetupEnv()
	if strings.Contains(envSetup, ";;") {
		t.Fatalf("root WSL env setup must not create an invalid double command separator: %q", envSetup)
	}
	for _, want := range []string{
		"unset XDG_RUNTIME_DIR XDG_DATA_HOME XDG_CACHE_HOME XDG_CONFIG_HOME",
		"export HOME=/root",
		"export XDG_DATA_HOME=/root/.local/share",
		"export XDG_CACHE_HOME=/root/.cache",
		"export XDG_CONFIG_HOME=/root/.config",
	} {
		if !strings.Contains(envSetup, want) {
			t.Fatalf("root WSL env setup should contain %q, got:\n%s", want, envSetup)
		}
	}
}

func TestCleanWSLUserOutputRemovesHostWarnings(t *testing.T) {
	input := "wsl: proxy warning\nУдалено образов без тегов: 2\nwsl.exe: another warning"
	if got := cleanWSLUserOutput(input); got != "Удалено образов без тегов: 2" {
		t.Fatalf("cleanWSLUserOutput() = %q", got)
	}
}

func TestCleanWSLUserOutputRemovesInternalToolLogs(t *testing.T) {
	input := "time=\"2026-09-16T21:43:30+05:00\" level=warning msg=\"BuildKit is not running\"\nОбразы без тегов не найдены"
	if got := CleanCleanupOutput(input); got != "Образы без тегов не найдены" {
		t.Fatalf("CleanCleanupOutput() = %q", got)
	}
}

func TestDecodeWSLOutputDecodesUTF16LEWithoutBOM(t *testing.T) {
	raw := []byte{'w', 0, 's', 0, 'l', 0, ':', 0, ' ', 0, 'o', 0, 'k', 0}
	if got := decodeWSLOutput(raw); got != "wsl: ok" {
		t.Fatalf("decodeWSLOutput() = %q", got)
	}
}

func TestDecodeWSLOutputDecodesCP866(t *testing.T) {
	raw, err := charmap.CodePage866.NewEncoder().Bytes([]byte("проблема"))
	if err != nil {
		t.Fatalf("encode CP866: %v", err)
	}
	if got := decodeWSLOutput(raw); got != "проблема" {
		t.Fatalf("decodeWSLOutput() = %q", got)
	}
}

func TestFilterWSLDiagnosticBytesRemovesUTF16LEWarning(t *testing.T) {
	raw := []byte{'w', 0, 's', 0, 'l', 0, ':', 0, ' ', 0, 'o', 0, 'k', 0, '\n', 'O', 'K'}
	got := filterWSLDiagnosticBytes(raw)
	if string(got) != "OK" {
		t.Fatalf("filterWSLDiagnosticBytes() = %q", got)
	}
}

func TestParseNetworkContainersRemovesNULBytes(t *testing.T) {
	containers, err := parseNetworkContainers("{\x00\"id\": {\x00\"Name\": \"web\"\x00}\x00}\x00")
	if err != nil {
		t.Fatalf("parseNetworkContainers() unexpected error: %v", err)
	}
	if len(containers) != 1 || containers[0] != "web" {
		t.Fatalf("parseNetworkContainers() = %#v, want [web]", containers)
	}
}

func TestParseImageLinePreservesCreatedAt(t *testing.T) {
	image, err := parseImageLine(`{"ID":"7378c","Repository":"backend","Tag":"latest","Size":"1.454G","CreatedAt":"2026-09-20 12:34:56 +0000 UTC"}`)
	if err != nil {
		t.Fatalf("parseImageLine() unexpected error: %v", err)
	}
	if image.CreatedAt == "" {
		t.Fatal("parseImageLine() should preserve CreatedAt")
	}
	if got := FormatDateShort(image.CreatedAt); got != "2026-09-20 12:34" {
		t.Fatalf("FormatDateShort(CreatedAt) = %q, want %q", got, "2026-09-20 12:34")
	}
}

func TestParseVolumeLinePreservesMountpoint(t *testing.T) {
	volume, err := parseVolumeLine(`{"Name":"containerd_data","Driver":"local","Mountpoint":"/var/lib/containerd/volumes/containerd_data"}`)
	if err != nil {
		t.Fatalf("parseVolumeLine() unexpected error: %v", err)
	}
	if volume.Mountpoint != "/var/lib/containerd/volumes/containerd_data" {
		t.Fatalf("parseVolumeLine() mountpoint = %q", volume.Mountpoint)
	}
}

func TestParseVolumeLineUsesDirectoryFallback(t *testing.T) {
	volume, err := parseVolumeLine(`{"Name":"containerd_data","Directory":"/var/lib/containerd/volumes/containerd_data"}`)
	if err != nil {
		t.Fatalf("parseVolumeLine() unexpected error: %v", err)
	}
	if volume.Mountpoint != "/var/lib/containerd/volumes/containerd_data" || volume.Driver != "local" {
		t.Fatalf("parseVolumeLine() = %#v", volume)
	}
}

func TestParseVolumeLinesSkipsWslNoise(t *testing.T) {
	out := "wsl: Starting the distribution...\r\n" +
		`{"Name":"app-data","Mountpoint":"/var/lib/nerdctl/1935db59/volumes/default/app-data/_data"}` + "\r\n" +
		"\r\n" +
		`{"Name":"","Mountpoint":"/tmp/not-a-volume"}` + "\r\n" +
		"time=\"level=fatal\" msg=\"some volumes could not be removed\"\r\n"

	volumes := parseVolumeLines(out)
	if len(volumes) != 1 {
		t.Fatalf("parseVolumeLines() = %#v, want single volume", volumes)
	}
	if volumes[0].Name != "app-data" || volumes[0].Mountpoint != "/var/lib/nerdctl/1935db59/volumes/default/app-data/_data" {
		t.Fatalf("parseVolumeLines()[0] = %#v", volumes[0])
	}
}

func TestIsProtectedVolumeNameGuardsProjectVolumes(t *testing.T) {
	for _, name := range []string{"soul-dialogue-postgres-data", "soul-dialogue-redis-data", GetDBVolumeName()} {
		if !IsProtectedVolumeName(name) {
			t.Fatalf("IsProtectedVolumeName(%q) = false, want true", name)
		}
	}
	for _, name := range []string{"app-cache", "ef52253603f82cdb3cc4f39e8ad0889797cd56077f7860b05f877e991114ab07"} {
		if IsProtectedVolumeName(name) {
			t.Fatalf("IsProtectedVolumeName(%q) = true, want false", name)
		}
	}
}

func TestVolumeNamesAreNormalizedBeforeProtectionAndRemoval(t *testing.T) {
	volume, err := parseVolumeLine(`{"Name":"  soul-dialogue-postgres-data  ","Driver":"local","Mountpoint":" /var/lib/containerd/volumes/soul-dialogue-postgres-data "}`)
	if err != nil {
		t.Fatalf("parseVolumeLine() unexpected error: %v", err)
	}
	if volume.Name != "soul-dialogue-postgres-data" {
		t.Fatalf("parseVolumeLine() name = %q, want trimmed name", volume.Name)
	}
	if volume.Mountpoint != "/var/lib/containerd/volumes/soul-dialogue-postgres-data" {
		t.Fatalf("parseVolumeLine() mountpoint = %q, want trimmed mountpoint", volume.Mountpoint)
	}
	if !IsProtectedVolumeName("  soul-dialogue-postgres-data  ") {
		t.Fatalf("IsProtectedVolumeName() should trim whitespace before matching protected volume names")
	}
}

func TestCleanContainerLogPathRemovesNULBytes(t *testing.T) {
	got := cleanContainerLogPath("\x00/var/lib/nerdctl/container-json.log\x00")
	if got != "/var/lib/nerdctl/container-json.log" {
		t.Fatalf("cleanContainerLogPath() = %q", got)
	}
}

func TestCleanContainerLogPathRejectsInvalidOutput(t *testing.T) {
	for _, raw := range []string{"", "wsl: warning", "/tmp/log\nextra"} {
		if got := cleanContainerLogPath(raw); got != "" {
			t.Fatalf("cleanContainerLogPath(%q) = %q, want empty", raw, got)
		}
	}
}

func TestRootFallbackStartsStackWithRootComposeCommand(t *testing.T) {
	script := projectStackStartScript("/tmp/project", "/tmp/scripts")
	wantParts := []string{
		"unset XDG_RUNTIME_DIR CONTAINERD_ROOTLESS_ROOTLESSKIT_FLAGS",
		"export XDG_RUNTIME_DIR=/run/user/0",
		"export CONTAINERD_ADDRESS=unix:///run/containerd/containerd.sock",
		"cd '/tmp/project'",
		". backend/config/.env",
		"nerdctl() { command nerdctl --address \"$CONTAINERD_ADDRESS\" --namespace \"$CONTAINERD_NAMESPACE\" \"$@\"; }",
		"nerdctl compose -f \"$compose_file\" down --remove-orphans || true",
		"nerdctl compose -f \"$compose_file\" up -d",
		"nerdctl start soul-dialogue-postgres soul-dialogue-redis soul-dialogue-backend soul-dialogue-worker soul-dialogue-frontend",
		"[ \"$postgres_health\" = healthy ] && [ \"$redis_health\" = healthy ]",
		"worker_state=$(nerdctl inspect --format '{{.State.Status}}' \"$worker_id\" 2>/dev/null || true)",
		"[ \"$backend_state\" = running ] && [ \"$worker_state\" = running ] && [ \"$backend_health\" = healthy ]",
		"nerdctl ps --format '{{.Names}}\\t{{.Status}}'",
	}
	for _, want := range wantParts {
		if !strings.Contains(script, want) {
			t.Fatalf("root fallback stack command should contain %q, got:\n%s", want, script)
		}
	}
}

func TestNormalizeScriptsPathAvoidsDuplicateSegments(t *testing.T) {
	projectPath := filepath.Join("C:", "work", "containerd-ui")
	for _, tc := range []struct {
		name string
		given string
		want string
	}{
		{name: "relative", given: "scripts/containerd", want: filepath.Join(projectPath, "scripts", "containerd")},
		{name: "already absolute", given: filepath.Join("C:", "work", "containerd-ui", "scripts", "containerd"), want: filepath.Join("C:", "work", "containerd-ui", "scripts", "containerd")},
		{name: "duplicate prefix", given: filepath.Join(projectPath, "scripts", "containerd"), want: filepath.Join(projectPath, "scripts", "containerd")},
	} {
		if got := NormalizeScriptsPath(projectPath, tc.given); got != tc.want {
			t.Fatalf("NormalizeScriptsPath(%q, %q) = %q, want %q", projectPath, tc.given, got, tc.want)
		}
	}
}

func TestRootNerdctlCommandUsesSystemContainerd(t *testing.T) {
	command := rootNerdctlCommand("volume ls --format '{{json .}}'")
	for _, want := range []string{
		"nerdctl --address 'unix:///run/containerd/containerd.sock'",
		"--namespace 'default'",
		"volume ls",
	} {
		if !strings.Contains(command, want) {
			t.Fatalf("root nerdctl command should contain %q, got: %s", want, command)
		}
	}
}

func TestVolumeRemoveCommandUsesRootNerdctlAndPreservesNames(t *testing.T) {
	for _, tc := range []struct {
		name string
		want string
	}{
		{name: "postgres-data", want: "volume rm -f 'postgres-data'"},
		{name: "db volume", want: "volume rm -f 'db volume'"},
	} {
		cmd := volumeRemoveCommand(tc.name)
		if !strings.Contains(cmd, tc.want) {
			t.Fatalf("volumeRemoveCommand(%q) = %q, want %q", tc.name, cmd, tc.want)
		}
		if strings.Contains(cmd, "rm -rf /var/lib/nerdctl/") {
			t.Fatalf("volumeRemoveCommand(%q) should not bypass nerdctl with raw filesystem deletion: %q", tc.name, cmd)
		}
	}
}

func TestVolumeRemovalTargetsIncludeActualMountpoint(t *testing.T) {
	targets := volumeRemovalTargets("containerd_soul-dialogue-postgres-data", "/var/lib/containerd/volumes/containerd_soul-dialogue-postgres-data")
	if !containsString(targets, "/var/lib/containerd/volumes/containerd_soul-dialogue-postgres-data") {
		t.Fatalf("expected real containerd mountpoint in deletion targets: %#v", targets)
	}
	if !containsString(targets, "/var/lib/nerdctl/default/volumes/containerd_soul-dialogue-postgres-data") {
		t.Fatalf("expected nerdctl fallback path in deletion targets: %#v", targets)
	}
}

func TestInvalidateWSLCacheClearsVolumeListingEntries(t *testing.T) {
	wslCache.Lock()
	wslCache.m["Debian\x00nerdctl volume ls --format '{{json .}}'"] = wslCacheEntry{output: "old", timestamp: time.Now()}
	wslCache.totalSize = 3
	wslCache.Unlock()

	InvalidateWSLCache()

	wslCache.RLock()
	_, ok := wslCache.m["Debian\x00nerdctl volume ls --format '{{json .}}'"]
	wslCache.RUnlock()
	if ok {
		t.Fatal("InvalidateWSLCache() should clear cached volume listings")
	}
}

func TestInstallScriptIncludesCNIPluginPackage(t *testing.T) {
	installScript := "sudo apt update && sudo apt install -y containerd nerdctl buildkit containernetworking-plugins && sudo systemctl enable --now containerd && sudo systemctl enable --now buildkit"
	if !strings.Contains(installScript, "containernetworking-plugins") {
		t.Fatalf("install script should install containernetworking-plugins package")
	}
}
