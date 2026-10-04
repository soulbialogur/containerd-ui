package ui

import (
	"context"
	"errors"
	"io/fs"
	"strings"
	"testing"

	"containerd-ui/i18n"
)

func TestLocalizedErrorMessageTranslatesCommonSystemErrors(t *testing.T) {
	tests := []struct {
		name string
		err  error
		key  string
	}{
		{name: "missing file", err: fs.ErrNotExist, key: "common.file_not_found"},
		{name: "permission denied", err: fs.ErrPermission, key: "common.permission_denied"},
		{name: "timeout", err: context.DeadlineExceeded, key: "common.operation_timeout"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := localizedErrorMessage(test.err)
			if !strings.Contains(got, i18n.T(test.key)) {
				t.Fatalf("localizedErrorMessage() = %q, want translation %q", got, i18n.T(test.key))
			}
			detailsPrefix := strings.TrimSpace(strings.TrimSuffix(i18n.T("common.error_details", "__details__"), "__details__"))
			if !strings.Contains(got, detailsPrefix) {
				t.Fatalf("localizedErrorMessage() = %q, want localized details label", got)
			}
		})
	}
}

func TestLocalizedErrorMessagePreservesWslDistributionTranslation(t *testing.T) {
	err := errors.New("WSL-дистрибутив не выбран или не найден")
	if got := localizedErrorMessage(err); got != i18n.T("common.wsl_distro_missing") {
		t.Fatalf("localizedErrorMessage() = %q, want translated WSL message", got)
	}
}