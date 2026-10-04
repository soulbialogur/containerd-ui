package ui

import (
	"containerd-ui/wsl"
	"strings"
	"testing"
	"time"
)

func TestParseWindowsWslVersion(t *testing.T) {
	if got := parseWindowsWslVersion("WSL version: 2.6.3.0\nKernel version: 6.6.87.2"); got != "v2.6.3" {
		t.Fatalf("parseWindowsWslVersion() = %q, want v2.6.3", got)
	}
}

func TestShortVersionPreservesUnrecognizedInput(t *testing.T) {
	input := "development build for Alpine"
	if got := shortVersion(input); got != input {
		t.Fatalf("shortVersion(%q) = %q, want the original input", input, got)
	}
}

func TestParseComponentVersionsHandlesLeadingNUL(t *testing.T) {
	output := "\x00CONTAINERD_VERSION=containerd github.com/containerd/containerd 1.7.24~ds1\n" +
		"BUILDKIT_VERSION=buildctl github.com/moby/buildkit v0.33.0"
	versions := parseComponentVersions(output)
	if versions["Containerd"] != "v1.7.24" {
		t.Fatalf("Containerd version = %q, want v1.7.24", versions["Containerd"])
	}
	if versions["Buildkitd"] != "v0.33.0" {
		t.Fatalf("Buildkitd version = %q, want v0.33.0", versions["Buildkitd"])
	}
}

func TestStatusSnapshotScriptCombinesStatusVersionsAndMetrics(t *testing.T) {
	script := statusSnapshotScript("containerd")
	for _, expected := range []string{
		"CONTAINERD:OK",
		"CONTAINERD_VERSION=",
		"BUILDKIT_VERSION=",
		"NERDCTL_VERSION=",
		"CONTAINERS_TOTAL;",
		"CONTAINERS_RUNNING;",
		"IMAGES;",
		"VOLUMES;",
		"NETWORKS;",
	} {
		if !strings.Contains(script, expected) {
			t.Errorf("status snapshot script is missing %q", expected)
		}
	}
}

func TestParseSystemMetricsOutput(t *testing.T) {
	got := parseSystemMetricsOutput("CONTAINERS_TOTAL;4\nCONTAINERS_RUNNING;2\nIMAGES;7\nVOLUMES;3\nNETWORKS;5")
	want := map[string]string{
		"containers_total":   "4",
		"containers_running": "2",
		"images":             "7",
		"volumes":            "3",
		"networks":           "5",
	}
	for key, value := range want {
		if got[key] != value {
			t.Errorf("metric %q = %q, want %q", key, got[key], value)
		}
	}
}

func TestCompactComponentStatusIncludesWSLDistro(t *testing.T) {
	got := compactComponentStatus(ComponentStatus{
		Name:    "WSL",
		Version: "Alpine",
		Icon:    "✅",
		Detail:  "Active",
	})
	if got != "✅ Alpine" {
		t.Fatalf("compactComponentStatus() = %q, want WSL distro", got)
	}
}

func TestFullComponentStatusOmitsRedundantWSLDetail(t *testing.T) {
	got := fullComponentStatus(ComponentStatus{
		Name:    "WSL",
		Version: "Alpine-ContainerdUI",
		Icon:    "✅",
		Detail:  "Активен",
	})
	if got != "✅ Alpine-ContainerdUI" {
		t.Fatalf("fullComponentStatus() = %q, want WSL status without redundant detail", got)
	}
}

func TestResponsiveStatusCardKeepsLastStatusWhileLoading(t *testing.T) {
	card := newResponsiveStatusCard("WSL")
	card.SetStatus("✅ Alpine (Active)", "✅ Alpine")
	card.SetLoading(true)
	if card.label.Text != "✅ Alpine (Active)" {
		t.Fatalf("loading replaced the full status with %q", card.label.Text)
	}

	card.compact = true
	card.updateText()
	card.SetLoading(true)
	if card.label.Text != "✅ Alpine" {
		t.Fatalf("loading replaced compact distro status with %q", card.label.Text)
	}
}

func TestResponsiveStatusCardKeepsFormattedLoadingText(t *testing.T) {
	card := newResponsiveStatusCard("Containerd")
	card.SetStatus("⏳ Loading...", "⏳ Loading...")
	card.SetLoading(true)
	if card.label.Text != "⏳ Loading..." {
		t.Fatalf("loading replaced formatted text with %q", card.label.Text)
	}
}

func TestEconomyModeListenerReturnsUnsubscribe(t *testing.T) {
	previous := economyMode.Load()
	SetEconomyMode(!previous)
	defer SetEconomyMode(previous)

	calls := 0
	unsubscribe := RegisterEconomyModeListener(func(bool) {
		calls++
	})
	if calls != 1 {
		t.Fatalf("listener initial calls = %d, want 1", calls)
	}

	unsubscribe()
	SetEconomyMode(previous)
	if calls != 1 {
		t.Fatalf("listener called after unsubscribe: %d total calls", calls)
	}
}

func TestSelectingTabDoesNotEnableDisabledAutoRefresh(t *testing.T) {
	tab := newTabActive(false, time.Hour, func() {})
	tab.SetAutoRefreshEnabled(false)
	tab.SetActive(true)
	if tab.ticker != nil {
		t.Fatal("selecting tab started ticker while auto-refresh is disabled")
	}

	tab.SetAutoRefreshEnabled(true)
	if tab.ticker == nil {
		t.Fatal("enabling auto-refresh did not start ticker on active tab")
	}
	tab.SetAutoRefreshEnabled(false)
	if tab.ticker != nil {
		t.Fatal("disabling auto-refresh did not stop ticker")
	}
	tab.Stop()
}

func TestRuntimeComponentsAreInstalledChecksAlpineVersions(t *testing.T) {
	probe := "NERDCTL_VERSION=nerdctl version 2.4.0\nBUILDKIT_VERSION=buildctl github.com/moby/buildkit v0.33.0\nALL_OK"
	alpine := wsl.Environment{PkgManager: wsl.PkgApk}
	if !runtimeComponentsAreInstalled(probe, alpine) {
		t.Fatal("supported Alpine versions should pass the component check")
	}
	nulPrefixedProbe := "\x00" + strings.ReplaceAll(probe, "\n", "\n\x00")
	if !runtimeComponentsAreInstalled(nulPrefixedProbe, alpine) {
		t.Fatal("supported Alpine versions with WSL NUL prefixes should pass the component check")
	}

	oldProbe := "NERDCTL_VERSION=nerdctl version 2.3.9\nBUILDKIT_VERSION=buildctl github.com/moby/buildkit v0.32.9\nALL_OK"
	if runtimeComponentsAreInstalled(oldProbe, alpine) {
		t.Fatal("old Alpine tools should trigger the installer")
	}
	missingVersion := "NERDCTL_VERSION=\nBUILDKIT_VERSION=\nALL_OK"
	if runtimeComponentsAreInstalled(missingVersion, alpine) {
		t.Fatal("missing Alpine tool versions should trigger the installer")
	}
}
