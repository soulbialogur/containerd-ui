package ui

import (
	"containerd-ui/wsl"
	"strings"
	"testing"
)

func TestParseWindowsWslVersion(t *testing.T) {
	if got := parseWindowsWslVersion("WSL version: 2.6.3.0\nKernel version: 6.6.87.2"); got != "v2.6.3" {
		t.Fatalf("parseWindowsWslVersion() = %q, want v2.6.3", got)
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
