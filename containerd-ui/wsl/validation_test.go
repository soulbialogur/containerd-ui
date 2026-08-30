package wsl

import (
	"strings"
	"testing"
)

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
