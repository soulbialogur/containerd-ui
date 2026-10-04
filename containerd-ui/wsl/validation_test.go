package wsl

import (
	"containerd-ui/i18n"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"golang.org/x/text/encoding/charmap"
	"gopkg.in/yaml.v3"
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

func TestCacheManagerRecentEventsHandlesNegativeLimit(t *testing.T) {
	manager := &CacheManager{maxEvents: 10}
	manager.Publish(CacheEvent{Type: CacheEventAll, Reason: "test"})

	if got := manager.GetRecentEvents(-1); len(got) != 0 {
		t.Fatalf("GetRecentEvents(-1) returned %d events, want 0", len(got))
	}

	if got := manager.GetSummary()["recentEvents"].([]CacheEvent); len(got) != 1 {
		t.Fatalf("GetSummary() returned %d recent events, want 1", len(got))
	}
}

func TestCDListContainersUsesFullCacheForBothViews(t *testing.T) {
	previous, wasCached := containersCache.Get()
	containersCache.Set([]Container{
		{ID: "running", Status: "running"},
		{ID: "stopped", Status: "exited"},
	})
	t.Cleanup(func() {
		if wasCached {
			containersCache.Set(previous)
		} else {
			containersCache.Invalidate()
		}
	})

	running, err := CDListContainers(false)
	if err != nil || len(running) != 1 || running[0].ID != "running" {
		t.Fatalf("CDListContainers(false) = (%v, %v), want only the running container", running, err)
	}

	all, err := CDListContainers(true)
	if err != nil || len(all) != 2 {
		t.Fatalf("CDListContainers(true) returned %d containers, %v; want running and stopped", len(all), err)
	}
}

func TestContainerdHealthProbeResetsAfterConsecutiveFailures(t *testing.T) {
	previous := cdHealthFailures.Swap(0)
	t.Cleanup(func() {
		cdHealthFailures.Store(previous)
	})

	for attempt := 1; attempt < containerdHealthFailureLimit; attempt++ {
		if shouldResetCDClientAfterPing(errors.New("containerd unavailable")) {
			t.Fatalf("health probe reset on failure %d, want %d failures", attempt, containerdHealthFailureLimit)
		}
	}
	if !shouldResetCDClientAfterPing(errors.New("containerd unavailable")) {
		t.Fatalf("health probe did not reset after %d failures", containerdHealthFailureLimit)
	}
	if shouldResetCDClientAfterPing(errors.New("containerd unavailable")) {
		t.Fatal("health failure counter was not reset after reaching the threshold")
	}
	if shouldResetCDClientAfterPing(nil) {
		t.Fatal("successful health probe requested a client reset")
	}
	if shouldResetCDClientAfterPing(errors.New("containerd unavailable")) {
		t.Fatal("successful health probe did not clear the failure counter")
	}
}

func TestParseContainerStatsOutput(t *testing.T) {
	output := `{"ID":"123456789abcdef","Name":"containerd-api","CPUPerc":"1.2%","MemUsage":"2MiB / 4MiB","NetIO":"0B / 0B","PIDs":"0"}`
	stats := parseContainerStatsOutput(output)
	if len(stats) != 1 {
		t.Fatalf("parseContainerStatsOutput() returned %d rows, want 1", len(stats))
	}
	got := stats[0]
	if got.ID != "123456789abc" || got.Name != "api" || got.CPU != "1.2%" || got.PIDs != "—" {
		t.Fatalf("parseContainerStatsOutput() = %+v", got)
	}
}

func TestParseSystemResources(t *testing.T) {
	output := "Mem: 8G 2G 6G 0B 0B 0B\n---CPU---\n4\n0.10 0.15 0.20 1/100 123\n---DISK---\n/dev/sda 100G 20G 80G 20% /"
	resources, err := parseSystemResources(output)
	if err != nil {
		t.Fatalf("parseSystemResources() error = %v", err)
	}
	if resources.RAMTotal != "8G" || resources.RAMUsed != "2G" || resources.CPUCores != "4" || resources.DiskFree != "80G" {
		t.Fatalf("parseSystemResources() = %+v", resources)
	}
}

func TestSplitContainerLogTimestamp(t *testing.T) {
	timestamp := "2026-10-01T18:24:17.123456789Z"
	gotTimestamp, message, ok := splitContainerLogTimestamp(timestamp + " application started")
	if !ok || gotTimestamp != timestamp || message != "application started" {
		t.Fatalf("splitContainerLogTimestamp() = (%q, %q, %t)", gotTimestamp, message, ok)
	}
	if _, message, ok := splitContainerLogTimestamp("plain log line"); ok || message != "plain log line" {
		t.Fatalf("plain log line should be preserved without a timestamp, got (%q, %t)", message, ok)
	}
	if timestamp, message, ok := stripContainerLogTimestamp("invalid-time application started"); !ok || timestamp != "invalid-time" || message != "application started" {
		t.Fatalf("stripContainerLogTimestamp() = (%q, %q, %t)", timestamp, message, ok)
	}
}

func TestParseContainerLogEntries(t *testing.T) {
	output := "2026-10-01T18:24:17.123456789Z first event\n2026-10-01T18:24:18.123456789Z second event"
	entries := parseContainerLogEntries(output)
	if len(entries) != 2 {
		t.Fatalf("parseContainerLogEntries() returned %d entries, want 2", len(entries))
	}
	if entries[0].Timestamp != "2026-10-01T18:24:17.123456789Z" || entries[0].Message != "first event" {
		t.Fatalf("first entry = %+v", entries[0])
	}
	if entries[1].Timestamp != "2026-10-01T18:24:18.123456789Z" || entries[1].Message != "second event" {
		t.Fatalf("second entry = %+v", entries[1])
	}
}

func TestCacheManagerConcurrentSubscribeAndPublish(t *testing.T) {
	manager := &CacheManager{maxEvents: 100}
	var callbackCount atomic.Int64
	manager.Subscribe(func(CacheEvent) {
		callbackCount.Add(1)
	})

	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		for range 100 {
			manager.Subscribe(func(CacheEvent) {
				callbackCount.Add(1)
			})
		}
	}()
	go func() {
		defer wg.Done()
		for range 100 {
			manager.Publish(CacheEvent{Type: CacheEventAll})
		}
	}()
	wg.Wait()

	if got := callbackCount.Load(); got < 100 {
		t.Fatalf("callbacks invoked = %d, want at least one per publish", got)
	}
}

func TestCacheManagerSubscribeReturnsUnsubscribe(t *testing.T) {
	manager := &CacheManager{maxEvents: 10}
	var calls atomic.Int64
	unsubscribe := manager.Subscribe(func(CacheEvent) {
		calls.Add(1)
	})

	manager.Publish(CacheEvent{Type: CacheEventAll})
	unsubscribe()
	unsubscribe()
	manager.Publish(CacheEvent{Type: CacheEventAll})

	if got := calls.Load(); got != 1 {
		t.Fatalf("subscriber called %d times, want 1", got)
	}
}

func TestContainerStatusStoreEvictsOldestByTimestamp(t *testing.T) {
	store := newContainerStatusCache(time.Hour)
	store.maxLen = 3
	store.set("oldest", "old")
	store.set("middle", "middle")
	store.set("newest", "new")

	store.mu.Lock()
	now := time.Now()
	for id, age := range map[string]time.Duration{
		"oldest": 3 * time.Minute,
		"middle": 2 * time.Minute,
		"newest": time.Minute,
	} {
		entry := store.data[id]
		entry.timestamp = now.Add(-age)
		store.data[id] = entry
	}
	store.mu.Unlock()

	store.set("middle", "updated")
	store.set("latest", "latest")

	if _, ok := store.get("oldest"); ok {
		t.Fatal("oldest status entry was not evicted")
	}
	if got, ok := store.get("middle"); !ok || got != "updated" {
		t.Fatalf("updated entry = (%q, %t), want (updated, true)", got, ok)
	}
	if _, ok := store.get("newest"); !ok {
		t.Fatal("newest existing entry was evicted")
	}
	if _, ok := store.get("latest"); !ok {
		t.Fatal("new entry was not stored")
	}
}

func TestGetConfigReturnsIndependentSnapshot(t *testing.T) {
	configCache.Lock()
	previous := configCache.config
	configCache.config = &AppConfig{
		DeployNetwork: "original-network",
		Projects:      []ProjectInfo{{Path: `C:\projects\app`, Name: "original"}},
	}
	configCache.Unlock()
	defer func() {
		configCache.Lock()
		configCache.config = previous
		configCache.Unlock()
	}()

	snapshot := GetConfig()
	snapshot.DeployNetwork = "mutated-network"
	snapshot.Projects[0].Name = "mutated"

	got := GetConfig()
	if got.DeployNetwork != "original-network" || got.Projects[0].Name != "original" {
		t.Fatalf("GetConfig() snapshot mutation leaked into cache: %+v", got)
	}
}

func TestApplyConfigToCachesAppliesZeroLimits(t *testing.T) {
	previousTTL := wslCacheTTL.Load()
	wslCache.Lock()
	previousMaxSize := wslCache.maxSize
	previousCleanupAt := wslCache.cleanupAt
	previousEntries := wslCache.m
	previousOrder := wslCache.order
	previousTotalSize := wslCache.totalSize
	wslCache.m = make(map[string]wslCacheEntry)
	wslCache.order.Init()
	wslCache.totalSize = 0
	wslCache.Unlock()
	t.Cleanup(func() {
		wslCache.Lock()
		wslCache.maxSize = previousMaxSize
		wslCache.cleanupAt = previousCleanupAt
		wslCache.m = previousEntries
		wslCache.order = previousOrder
		wslCache.totalSize = previousTotalSize
		wslCache.Unlock()
		wslCacheTTL.Store(previousTTL)
	})

	config := DefaultConfig()
	config.MaxWSLCacheSize = 1024
	config.WSLCacheCleanupAt = 0
	ApplyConfigToCaches(config)
	storeWSLResult("Alpine", "probe-1", "one")
	storeWSLResult("Alpine", "probe-2", "two")
	wslCache.RLock()
	countWithoutLimit := len(wslCache.m)
	wslCache.RUnlock()
	if countWithoutLimit != 2 {
		t.Fatalf("entries with cleanup limit 0 = %d, want 2", countWithoutLimit)
	}

	config.MaxWSLCacheSize = 0
	ApplyConfigToCaches(config)
	storeWSLResult("Alpine", "probe-3", "three")
	wslCache.RLock()
	maxSize := wslCache.maxSize
	entryCount := len(wslCache.m)
	wslCache.RUnlock()
	if maxSize != 0 || entryCount != 0 {
		t.Fatalf("cache with max size 0 = (size %d, entries %d), want disabled and empty", maxSize, entryCount)
	}
}

func TestWSLCacheEvictsOldestEntryInInsertionOrder(t *testing.T) {
	wslCache.Lock()
	previousMaxSize := wslCache.maxSize
	previousCleanupAt := wslCache.cleanupAt
	previousEntries := wslCache.m
	previousOrder := wslCache.order
	previousTotalSize := wslCache.totalSize
	wslCache.maxSize = 1024
	wslCache.cleanupAt = 2
	wslCache.m = make(map[string]wslCacheEntry)
	wslCache.order.Init()
	wslCache.totalSize = 0
	wslCache.Unlock()
	t.Cleanup(func() {
		wslCache.Lock()
		wslCache.maxSize = previousMaxSize
		wslCache.cleanupAt = previousCleanupAt
		wslCache.m = previousEntries
		wslCache.order = previousOrder
		wslCache.totalSize = previousTotalSize
		wslCache.Unlock()
	})

	storeWSLResult("Alpine", "first", "1")
	storeWSLResult("Alpine", "second", "2")
	storeWSLResult("Alpine", "third", "3")

	wslCache.RLock()
	_, firstExists := wslCache.m["Alpine\x00first"]
	_, secondExists := wslCache.m["Alpine\x00second"]
	_, thirdExists := wslCache.m["Alpine\x00third"]
	wslCache.RUnlock()
	if firstExists || !secondExists || !thirdExists {
		t.Fatalf("unexpected WSL cache entries after FIFO eviction: first=%t second=%t third=%t", firstExists, secondExists, thirdExists)
	}
}

func TestBoundedStringCacheExpiresFromQueueHeadAndEvictsOldest(t *testing.T) {
	cache := newBoundedStringCache(time.Hour, 2)
	cache.Set("expired", "old")
	cache.mu.Lock()
	expired := cache.data["expired"]
	expired.timestamp = time.Now().Add(-2 * cache.defaultTTL)
	cache.data["expired"] = expired
	cache.mu.Unlock()

	cache.Set("first", "1")
	cache.Set("second", "2")
	cache.Set("third", "3")

	cache.mu.RLock()
	_, expiredExists := cache.data["expired"]
	_, firstExists := cache.data["first"]
	_, secondExists := cache.data["second"]
	_, thirdExists := cache.data["third"]
	orderLength := cache.order.Len()
	cache.mu.RUnlock()
	if expiredExists || firstExists || !secondExists || !thirdExists || orderLength != 2 {
		t.Fatalf("unexpected bounded string cache state: expired=%t first=%t second=%t third=%t order=%d", expiredExists, firstExists, secondExists, thirdExists, orderLength)
	}
}

func TestParseDetectedEnvironmentOpenRC(t *testing.T) {
	got := parseDetectedEnvironment("SHELL:sh\nINIT:openrc\nPKG:apk\nPRIV:doas\n", Environment{})
	if got.Shell != ShellSh || got.InitSystem != InitOpenRC || got.PkgManager != PkgApk || got.PrivilegeCmd != PrivDoas {
		t.Fatalf("parseDetectedEnvironment() = %+v", got)
	}
}

func TestGetDefaultWslDistroName(t *testing.T) {
	if got := GetDefaultWslDistroName(); got != "Alpine-ContainerdUI" {
		t.Fatalf("GetDefaultWslDistroName() = %q, want Alpine-ContainerdUI", got)
	}
	if got := GetBundledWslDistroName(); got != "Alpine-ContainerdUI" {
		t.Fatalf("GetBundledWslDistroName() = %q, want Alpine-ContainerdUI", got)
	}
	if !IsSupportedWslDistro("Alpine-ContainerdUI") || !IsSupportedWslDistro("alpine-containerdui") {
		t.Fatal("the bundled distro should be accepted case-insensitively")
	}
	if IsSupportedWslDistro("Alpine") || IsSupportedWslDistro("Ubuntu") {
		t.Fatal("only Alpine-ContainerdUI should be an accepted runtime distro")
	}
}

func TestDetectWslDistroFromList(t *testing.T) {
	for _, tc := range []struct {
		name    string
		distros []string
		want    string
	}{
		{name: "select bundled distro", distros: []string{"Ubuntu", "Alpine-ContainerdUI"}, want: "Alpine-ContainerdUI"},
		{name: "ignore legacy Alpine", distros: []string{"Alpine"}, want: ""},
		{name: "prefer bundled distro when legacy Alpine exists", distros: []string{"Alpine", "Alpine-ContainerdUI"}, want: "Alpine-ContainerdUI"},
		{name: "do not fall back to unsupported distros", distros: []string{"Ubuntu", "Fedora"}, want: ""},
		{name: "ignore unsupported distros", distros: []string{"docker-desktop", "rancher-desktop-data", "Ubuntu"}, want: ""},
		{name: "ignore infrastructure only", distros: []string{"docker-desktop", "rancher-desktop"}, want: ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := detectWslDistroFromList(tc.distros); got != tc.want {
				t.Fatalf("detectWslDistroFromList(%v) = %q, want %q", tc.distros, got, tc.want)
			}
		})
	}
}

func TestBuildRuntimeInstallCommandForAlpineOpenRC(t *testing.T) {
	tests := []struct {
		name string
		env  Environment
		want []string
	}{{
		name: "alpine openrc",
		env:  Environment{PkgManager: PkgApk, InitSystem: InitOpenRC, PrivilegeCmd: PrivDoas},
		want: []string{"doas -n sh -c", "apk update", "containerd-openrc", "cni-plugins", "rc-update add containerd default", "rc-service containerd start"},
	}}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			command, err := BuildRuntimeInstallCommand(test.env)
			if err != nil {
				t.Fatalf("BuildRuntimeInstallCommand() error = %v", err)
			}
			for _, want := range test.want {
				if !strings.Contains(command, want) {
					t.Errorf("command %q does not contain %q", command, want)
				}
			}
		})
	}
}

func TestAlpineInstallCommandAddsCompatibilityPackagesAndDoasRules(t *testing.T) {
	command, err := BuildRuntimeInstallCommand(Environment{PkgManager: PkgApk, InitSystem: InitOpenRC})
	if err != nil {
		t.Fatalf("BuildRuntimeInstallCommand() error = %v", err)
	}
	for _, want := range []string{
		"bash", "ca-certificates", "findutils", "logrotate", "procps-ng", "socat", "tzdata", "doas",
		"/etc/doas.conf", "permit nopass :wheel", "permit nopass root",
		"curl -fsSL", "sha256sum -c -", "nerdctl-", "buildkit-",
		"containerd-ui-grpc-proxy", "[boot]", "touch /run/openrc/softlevel",
		"rc-service containerd start", "rc-service containerd-ui-grpc-proxy start",
		MinimumAlpineNerdctlVersion, MinimumAlpineBuildkitVersion,
	} {
		if !strings.Contains(command, want) {
			t.Errorf("Alpine install command does not contain %q", want)
		}
	}
}

func TestAlpineWSLBootCommandPreservesExistingConfig(t *testing.T) {
	command := AlpineWSLBootCommand()
	for _, want := range []string{
		"/etc/wsl.conf",
		"in_boot",
		"-v proxy_command=\"$proxy_command\"",
		"existing \"; \" proxy_command",
		"mkdir -p /run/openrc; touch /run/openrc/softlevel; rc-service containerd start; rc-service containerd-ui-grpc-proxy start",
	} {
		if !strings.Contains(command, want) {
			t.Errorf("AlpineWSLBootCommand() does not contain %q", want)
		}
	}
}

func TestAlpineContainerdUIProxyUsesSocketRelay(t *testing.T) {
	command := AlpineContainerdUIProxySetupCommand()
	for _, want := range []string{
		"/etc/init.d/containerd-ui-grpc-proxy",
		"command=\"/usr/bin/socat\"",
		"TCP-LISTEN:50051,bind=0.0.0.0,reuseaddr,fork",
		"UNIX-CONNECT:/run/containerd/containerd.sock",
		"need containerd",
	} {
		if !strings.Contains(command, want) {
			t.Errorf("AlpineContainerdUIProxySetupCommand() does not contain %q", want)
		}
	}
}

func TestAlpineContainerdConfigExposesApplicationGRPCPort(t *testing.T) {
	command := AlpineContainerdConfigCommand()
	for _, want := range []string{
		"containerd config default",
		"io.containerd.server.v1.grpc-tcp",
		"/run/containerd/containerd.sock",
	} {
		if !strings.Contains(command, want) {
			t.Errorf("Alpine containerd config should contain %q; got:\n%s", want, command)
		}
	}
	if !strings.Contains(command, "awk") || !strings.Contains(command, "tmp=\"$(mktemp)\"") {
		t.Fatalf("Alpine containerd config should update existing configuration without overwriting other settings; got:\n%s", command)
	}
}

func TestAlpineToolchainVersionsSupported(t *testing.T) {
	if !AlpineToolchainVersionsSupported("nerdctl version v2.4.0", "buildctl github.com/moby/buildkit v0.33.0") {
		t.Fatal("minimum supported Alpine versions should pass")
	}
	if AlpineToolchainVersionsSupported("nerdctl version 2.3.9", "buildctl github.com/moby/buildkit v0.33.0") {
		t.Fatal("old nerdctl version should fail the minimum check")
	}
	if AlpineToolchainVersionsSupported("nerdctl version 2.4.0", "buildctl github.com/moby/buildkit v0.32.9") {
		t.Fatal("old BuildKit version should fail the minimum check")
	}
}

func TestCNIPluginPackageForAlpine(t *testing.T) {
	if got := CNIPluginPackageForManager(PkgApk); got != "cni-plugins" {
		t.Errorf("CNIPluginPackageForManager(%q) = %q, want cni-plugins", PkgApk, got)
	}
	if got := CNIPluginPackageForManager("unknown"); got != "" {
		t.Errorf("CNIPluginPackageForManager(unknown) = %q, want empty", got)
	}
}

func TestCNIPluginInstallCommandByManager(t *testing.T) {
	alpineCommand, err := CNIPluginInstallCommand(Environment{PkgManager: PkgApk}, "doas -n ")
	if err != nil || alpineCommand != "doas -n apk add --no-cache cni-plugins" {
		t.Fatalf("Alpine CNI install command = %q, error = %v", alpineCommand, err)
	}
}

func TestServiceActiveCommandForOpenRC(t *testing.T) {
	got := serviceActiveCommand("containerd", InitOpenRC)
	want := "rc-service containerd status >/dev/null 2>&1"
	if got != want {
		t.Fatalf("serviceActiveCommand() = %q, want %q", got, want)
	}
}

func TestContainerdLogsCleanupReportsOpenRCFallback(t *testing.T) {
	script := containerdLogsCleanupCommand()
	for _, want := range []string{"command -v logrotate", "logrotate applied", "logrotate unavailable"} {
		if !strings.Contains(script, want) {
			t.Errorf("containerdLogsCleanupCommand() does not contain %q", want)
		}
	}
	if strings.Contains(script, "journalctl") || strings.Contains(script, "/etc/systemd") {
		t.Fatal("Alpine log cleanup should not depend on systemd or journald")
	}
}

func TestCheckPortsCommandHasFallback(t *testing.T) {
	command := checkPortsCommand()
	for _, want := range []string{"command -v ss", "ss -tlnp", "command -v netstat", "netstat -tln", "PORT_CHECK_UNAVAILABLE"} {
		if !strings.Contains(command, want) {
			t.Errorf("checkPortsCommand() does not contain %q", want)
		}
	}
}

func TestDetectProjectPathContextStopsWhenCanceled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	if path := DetectProjectPathContext(ctx); path != "" {
		t.Fatalf("DetectProjectPathContext() = %q after cancellation, want empty path", path)
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
		`/api"quoted`,
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

func TestValidateProjectComposeNetworkAcceptsEquivalentYAMLForms(t *testing.T) {
	tests := map[string]string{
		"default external name and flow sequence": `services:
  backend:
    image: test
    networks: [soul-dialogue]
networks:
  soul-dialogue:
    external: true
`,
		"external network alias and mapping attachment": `services:
  backend:
    image: test
    networks:
      public: {}
networks:
  public:
    external: true
    name: soul-dialogue
`,
	}
	for name, compose := range tests {
		t.Run(name, func(t *testing.T) {
			if err := validateProjectComposeNetworkFromText(compose, "backend"); err != nil {
				t.Fatalf("valid Compose network form rejected: %v", err)
			}
		})
	}
}

func TestValidateProjectComposeNetworkRejectsCommentsAndMissingServiceAttachment(t *testing.T) {
	commentOnly := `services:
  backend:
    image: test
# networks:
#   soul-dialogue:
#     external: true
#     name: soul-dialogue
#     - soul-dialogue
`
	if err := validateProjectComposeNetworkFromText(commentOnly, "backend"); err == nil {
		t.Fatal("network references in comments must not satisfy Compose validation")
	}

	missingServiceAttachment := `services:
  backend:
    image: test
    networks: [soul-dialogue]
  frontend:
    image: test
networks:
  soul-dialogue:
    external: true
`
	if err := validateProjectComposeNetworkFromText(missingServiceAttachment, "backend", "frontend"); err == nil {
		t.Fatal("every selected service must attach to the required external network")
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

func TestRenderedDeploymentComposeUsesSpacesForYAMLIndentation(t *testing.T) {
	traefik, err := renderTraefikCompose("admin@example.com")
	if err != nil {
		t.Fatalf("renderTraefikCompose() unexpected error: %v", err)
	}
	cloudflare, err := renderCloudflareCompose()
	if err != nil {
		t.Fatalf("renderCloudflareCompose() unexpected error: %v", err)
	}
	if strings.Contains(traefik, "\t") {
		t.Fatal("Traefik Compose must not contain tabs, which YAML forbids for indentation")
	}
	if strings.Contains(cloudflare, "\t") {
		t.Fatal("Cloudflare Compose must not contain tabs, which YAML forbids for indentation")
	}
	for name, rendered := range map[string]string{"Traefik": traefik, "Cloudflare": cloudflare} {
		var compose map[string]any
		if err := yaml.Unmarshal([]byte(rendered), &compose); err != nil {
			t.Errorf("%s Compose is invalid YAML: %v", name, err)
		}
	}
}

func TestCloudflareConfigOrdersBackendPathBeforeFrontendCatchAll(t *testing.T) {
	config, err := renderCloudflareConfig("example.com", "/api", true, true)
	if err != nil {
		t.Fatalf("renderCloudflareConfig() unexpected error: %v", err)
	}
	if !json.Valid([]byte(config)) {
		t.Fatalf("renderCloudflareConfig() generated invalid JSON:\n%s", config)
	}
	backendRoute := strings.Index(config, `"service": "http://backend:8000"`)
	frontendRoute := strings.Index(config, `"service": "http://frontend:80"`)
	if backendRoute < 0 || frontendRoute < 0 {
		t.Fatalf("expected both backend and frontend ingress routes, got:\n%s", config)
	}
	if backendRoute > frontendRoute {
		t.Fatalf("backend path route must precede frontend catch-all, got:\n%s", config)
	}
}

func TestFormatBuildErrorPreservesStructureAndRendersLastOutputLines(t *testing.T) {
	previousLocale := i18n.GetCurrentLocale()
	i18n.SetLocale(i18n.LocaleRU)
	defer i18n.SetLocale(previousLocale)

	lines := make([]string, 31)
	for index := range lines {
		lines[index] = fmt.Sprintf("step-%02d", index+1)
	}
	status := errors.New("build exited with code 1")
	formatted := formatBuildError(strings.Join(lines, "\n"), status)

	var buildErr *BuildError
	if !errors.As(formatted, &buildErr) {
		t.Fatalf("formatBuildError() type = %T, want *BuildError", formatted)
	}
	if buildErr.Status != status || len(strings.Split(buildErr.Output, "\n")) != 31 || buildErr.Hint != "" {
		t.Fatalf("BuildError fields were not preserved: %+v", buildErr)
	}
	rendered := buildErr.Error()
	if strings.Contains(rendered, "step-01") || !strings.Contains(rendered, "step-02") || !strings.Contains(rendered, "step-31") {
		t.Fatalf("BuildError should render only the last 30 output lines:\n%s", rendered)
	}
	if !strings.Contains(rendered, i18n.T("build_error.status", status.Error())) {
		t.Fatalf("BuildError should render localized status:\n%s", rendered)
	}
}

func TestFormatBuildErrorAddsHintWhenOutputIsEmpty(t *testing.T) {
	previousLocale := i18n.GetCurrentLocale()
	i18n.SetLocale(i18n.LocaleEN)
	defer i18n.SetLocale(previousLocale)

	formatted := formatBuildError("", errors.New("exit status 1"))
	var buildErr *BuildError
	if !errors.As(formatted, &buildErr) {
		t.Fatalf("formatBuildError() type = %T, want *BuildError", formatted)
	}
	if buildErr.Output != "" || buildErr.Hint == "" || !strings.Contains(formatted.Error(), i18n.T("build_error.default_hint")) {
		t.Fatalf("BuildError did not render its empty-output hint: %+v\n%s", buildErr, formatted)
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

func TestShutdownCancelsAppContextAndIsIdempotent(t *testing.T) {
	ctx := AppContext()
	select {
	case <-ctx.Done():
		t.Fatal("app context should still be active before shutdown")
	default:
	}

	Shutdown()

	select {
	case <-ctx.Done():
	default:
		t.Fatal("app context should be canceled after shutdown")
	}

	Shutdown()
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
	script := buildProjectImagesOneShotRootScriptWithEnvironment("/tmp/project", "/tmp/project/compose.yaml", Environment{PkgManager: PkgApk})
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
	if !strings.Contains(script, "apk add --no-cache cni-plugins") {
		t.Fatalf("one-shot root build script should install missing Alpine CNI plugins before build; got:\n%s", script)
	}
	if !strings.Contains(script, "/usr/lib/cni/bridge") || !strings.Contains(script, "/usr/libexec/cni/bridge") {
		t.Fatalf("one-shot root build script should accept Alpine CNI plugin locations; got:\n%s", script)
	}
	if !strings.Contains(script, "nerdctl --address unix:///run/containerd/containerd.sock --namespace 'default' compose -f") {
		t.Fatalf("root one-shot build script should include compose build; got:\n%s", script)
	}
}

func TestBuildProjectImagesOneShotUsesAlpineCNIPackage(t *testing.T) {
	script := buildProjectImagesOneShotRootScriptWithEnvironment("/tmp/project", "/tmp/project/compose.yaml", Environment{PkgManager: PkgApk})
	if !strings.Contains(script, "apk add --no-cache cni-plugins") {
		t.Fatalf("Alpine one-shot root build script should install cni-plugins; got:\n%s", script)
	}
	if strings.Contains(script, "apt-get") {
		t.Fatalf("Alpine one-shot root build script should not contain non-Alpine CNI commands; got:\n%s", script)
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

func TestParseImageLineNormalizesCreatedAtAndSize(t *testing.T) {
	image, err := parseImageLine(`{"ID":"7378c","Repository":"backend","Tag":"latest","Size":"1.454G","CreatedAt":"2026-09-20 12:34:56 +0000 UTC"}`)
	if err != nil {
		t.Fatalf("parseImageLine() unexpected error: %v", err)
	}
	if image.CreatedAt != "2026-09-20T12:34:56Z" {
		t.Fatalf("parseImageLine() CreatedAt = %q, want full RFC3339 timestamp", image.CreatedAt)
	}
	if image.Size != "1.45 GB" {
		t.Fatalf("parseImageLine() Size = %q, want normalized human-readable size", image.Size)
	}

	bytesImage, err := parseImageLine(`{"Size":"1536","CreatedAt":"bad-date"}`)
	if err != nil {
		t.Fatalf("parseImageLine() unexpected error for byte size: %v", err)
	}
	if bytesImage.Size != "1.5 KB" {
		t.Fatalf("parseImageLine() raw byte Size = %q, want 1.5 KB", bytesImage.Size)
	}
	if bytesImage.CreatedAt != "bad-date" {
		t.Fatalf("parseImageLine() should preserve unrecognized CreatedAt, got %q", bytesImage.CreatedAt)
	}
}

func TestFormatDateShortRemovesSecondsAndTimezone(t *testing.T) {
	got := FormatDateShort("2026-09-30T15:25:56+00:00")
	if got != "2026-09-30 15:25" {
		t.Fatalf("FormatDateShort() = %q, want compact timestamp", got)
	}
}

func TestSplitContainerStatusExtractsUptimeAndHealth(t *testing.T) {
	status, uptime, health := splitContainerStatus("Up 2 minutes (unhealthy)")
	if status != "Up 2 minutes" || uptime != "2 minutes" || health != "unhealthy" {
		t.Fatalf("splitContainerStatus() = (%q, %q, %q)", status, uptime, health)
	}

	status, uptime, health = splitContainerStatus("Up 5 seconds (healthy)")
	if status != "Up 5 seconds" || uptime != "5 seconds" || health != "healthy" {
		t.Fatalf("splitContainerStatus() = (%q, %q, %q)", status, uptime, health)
	}

	status, uptime, health = splitContainerStatus("Exited (1) 2 minutes ago")
	if status != "Exited (1) 2 minutes ago" || uptime != "" || health != "" {
		t.Fatalf("stopped status should not produce uptime/health: (%q, %q, %q)", status, uptime, health)
	}

	if got := TranslateStatus("unhealthy"); got != i18n.T("container_status.unhealthy") {
		t.Fatalf("TranslateStatus(unhealthy) = %q, want unhealthy translation", got)
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
		"for _svc in soul-dialogue-postgres soul-dialogue-redis soul-dialogue-backend soul-dialogue-worker soul-dialogue-frontend; do",
		"nerdctl start \"$_svc\" 2>/dev/null || true",
		"[ \"$postgres_state\" = running ] && [ \"$redis_state\" = running ] && { [ \"$postgres_health\" = healthy ] || [ -z \"$postgres_health\" ] || [ \"$postgres_health\" = starting ]; } && { [ \"$redis_health\" = healthy ] || [ -z \"$redis_health\" ] || [ \"$redis_health\" = starting ]; }",
		"backend_state=$(nerdctl inspect --format '{{.State.Status}}' \"$backend_id\" 2>/dev/null || true)",
		"[ \"$backend_state\" = running ] && [ \"$worker_state\" = running ] && { [ \"$backend_health\" = healthy ] || [ -z \"$backend_health\" ] || [ \"$backend_health\" = starting ]; }",
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
		name  string
		given string
		want  string
	}{
		{name: "relative", given: "scripts/containerd", want: filepath.ToSlash(filepath.Join(projectPath, "scripts", "containerd"))},
		{name: "already absolute", given: filepath.ToSlash(filepath.Join("C:", "work", "containerd-ui", "scripts", "containerd")), want: filepath.ToSlash(filepath.Join("C:", "work", "containerd-ui", "scripts", "containerd"))},
		{name: "duplicate prefix", given: filepath.ToSlash(filepath.Join(projectPath, "scripts", "containerd")), want: filepath.ToSlash(filepath.Join(projectPath, "scripts", "containerd"))},
	} {
		if got := NormalizeScriptsPath(projectPath, tc.given); got != tc.want {
			t.Fatalf("NormalizeScriptsPath(%q, %q) = %q, want %q", projectPath, tc.given, got, tc.want)
		}
	}
}

func TestFindProjectComposeFilePrefersProjectRoot(t *testing.T) {
	tmpDir := t.TempDir()
	composePath := filepath.Join(tmpDir, "compose.yaml")
	if err := os.WriteFile(composePath, []byte("services: {}\n"), 0o644); err != nil {
		t.Fatalf("write compose.yaml: %v", err)
	}
	if got := findProjectComposeFile(tmpDir); filepath.ToSlash(got) != filepath.ToSlash(composePath) {
		t.Fatalf("findProjectComposeFile(%q) = %q, want %q", tmpDir, got, composePath)
	}
}

func TestFindProjectComposeFileResolvesParentFolder(t *testing.T) {
	tmpDir := t.TempDir()
	composePath := filepath.Join(tmpDir, "compose.yaml")
	if err := os.WriteFile(composePath, []byte("services: {}\n"), 0o644); err != nil {
		t.Fatalf("write compose.yaml: %v", err)
	}
	nestedPath := filepath.Join(tmpDir, "scripts", "containerd")
	if err := os.MkdirAll(nestedPath, 0o755); err != nil {
		t.Fatalf("mkdir nested: %v", err)
	}
	if got := findProjectComposeFile(nestedPath); filepath.ToSlash(got) != filepath.ToSlash(composePath) {
		t.Fatalf("findProjectComposeFile(%q) = %q, want %q", nestedPath, got, composePath)
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

func TestUnusedNetworkCleanupProtectsProjectNetworks(t *testing.T) {
	script := unusedNetworkCleanupScript()
	for _, want := range []string{
		"soul-dialogue|soul-dialogue-*",
		"containerd_soul-dialogue|containerd_soul-dialogue-*",
		"Пропущена защищённая сеть: $net",
	} {
		if !strings.Contains(script, want) {
			t.Errorf("network cleanup script should contain %q", want)
		}
	}
}

func TestInvalidateWSLCacheClearsVolumeListingEntries(t *testing.T) {
	wslCache.Lock()
	wslCache.m["Alpine\x00nerdctl volume ls --format '{{json .}}'"] = wslCacheEntry{output: "old", timestamp: time.Now()}
	wslCache.totalSize = 3
	wslCache.Unlock()

	InvalidateWSLCache()

	wslCache.RLock()
	_, ok := wslCache.m["Alpine\x00nerdctl volume ls --format '{{json .}}'"]
	wslCache.RUnlock()
	if ok {
		t.Fatal("InvalidateWSLCache() should clear cached volume listings")
	}
}
