package ui

import (
	"containerd-ui/i18n"
	"context"
	"errors"
	"io/fs"
	"strings"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/widget"
)

func localizedWslError(err error) string {
	return localizedErrorMessage(err)
}

func localizedErrorMessage(err error) string {
	if err == nil {
		return ""
	}
	message := strings.TrimSpace(err.Error())
	lower := strings.ToLower(message)
	switch {
	case strings.Contains(lower, "wsl-дистрибутив не выбран или не найден"), strings.Contains(lower, "no wsl distribution is selected"):
		return i18n.T("common.wsl_distro_missing")
	case errors.Is(err, fs.ErrNotExist), strings.Contains(lower, "no such file or directory"), strings.Contains(lower, "the system cannot find the file specified"):
		return i18n.T("common.file_not_found") + "\n\n" + i18n.T("common.error_details", message)
	case errors.Is(err, fs.ErrPermission), strings.Contains(lower, "permission denied"), strings.Contains(lower, "access is denied"):
		return i18n.T("common.permission_denied") + "\n\n" + i18n.T("common.error_details", message)
	case errors.Is(err, context.DeadlineExceeded), strings.Contains(lower, "timed out"), strings.Contains(lower, "timeout"):
		return i18n.T("common.operation_timeout") + "\n\n" + i18n.T("common.error_details", message)
	default:
		return i18n.T("common.error_details", message)
	}
}

func showAppError(win fyne.Window, err error) {
	if err == nil {
		return
	}
	showErrorDialog(win, localizedErrorMessage(err))
}

func showAppInfo(win fyne.Window, title, message string) {
	dialog.NewCustom(title, i18n.T("dialogs.ok"), widget.NewLabel(message), win).Show()
}