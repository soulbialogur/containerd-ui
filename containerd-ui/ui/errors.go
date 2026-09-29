package ui

import (
	"containerd-ui/i18n"
	"strings"
)

func localizedWslError(err error) string {
	if err == nil {
		return ""
	}
	if strings.Contains(strings.ToLower(err.Error()), "wsl-дистрибутив не выбран или не найден") {
		return i18n.T("common.wsl_distro_missing")
	}
	return err.Error()
}