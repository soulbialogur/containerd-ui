package main

import (
	"image/color"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/theme"
)

// ---------------------------------------------------------------------------
// Цветовая палитра — современный тёмный дизайн
// ---------------------------------------------------------------------------

var (
	bgPrimary     = color.NRGBA{0x0f, 0x11, 0x1a, 0xff} // Фон основной
	bgSecondary   = color.NRGBA{0x16, 0x1b, 0x28, 0xff} // Фон панелей
	bgTertiary    = color.NRGBA{0x1e, 0x25, 0x38, 0xff} // Фон карточек / строк
	bgHover       = color.NRGBA{0x25, 0x2d, 0x42, 0xff} // Hover строк
	bgActive      = color.NRGBA{0x2a, 0x35, 0x50, 0xff} // Активная строка

	accentPrimary = color.NRGBA{0x6c, 0x5c, 0xe7, 0xff} // Акцент — фиолетовый
	accentSuccess = color.NRGBA{0x00, 0xb8, 0x94, 0xff} // Успех — бирюзовый
	accentWarning = color.NRGBA{0xff, 0xc7, 0x00, 0xff} // Предупреждение — янтарный
	accentDanger  = color.NRGBA{0xff, 0x6b, 0x6b, 0xff} // Опасность — красный

	textPrimary   = color.NRGBA{0xe8, 0xec, 0xf1, 0xff} // Текст основной
	textSecondary = color.NRGBA{0x8b, 0x95, 0xa5, 0xff} // Текст вторичный
	textMuted     = color.NRGBA{0x5a, 0x65, 0x78, 0xff} // Текст приглушённый

	borderColor  = color.NRGBA{0x2a, 0x30, 0x40, 0xff} // Рамки
	borderAccent = color.NRGBA{0x6c, 0x5c, 0xe7, 0xff} // Акцентная рамка
)

// ---------------------------------------------------------------------------
// Кастомная тёмная тема
// ---------------------------------------------------------------------------

type darkTheme struct{}

func (d *darkTheme) Color(name fyne.ThemeColorName, v fyne.ThemeVariant) color.Color {
	switch name {
	case theme.ColorNameBackground:
		return bgPrimary
	case theme.ColorNameForeground:
		return textPrimary
	case theme.ColorNamePrimary:
		return accentPrimary
	case theme.ColorNameButton:
		return bgTertiary
	case theme.ColorNameInputBackground:
		return bgSecondary
	case theme.ColorNameHover:
		return bgHover
	case theme.ColorNameDisabled:
		return textMuted
	case theme.ColorNameScrollBar:
		return borderColor
	default:
		return theme.DefaultTheme().Color(name, v)
	}
}

func (d *darkTheme) Icon(name fyne.ThemeIconName) fyne.Resource {
	return theme.DefaultTheme().Icon(name)
}

func (d *darkTheme) Font(style fyne.TextStyle) fyne.Resource {
	return theme.DefaultTheme().Font(style)
}

func (d *darkTheme) Size(name fyne.ThemeSizeName) float32 {
	switch name {
	case theme.SizeNamePadding:
		return 12
	case theme.SizeNameInnerPadding:
		return 8
	default:
		return theme.DefaultTheme().Size(name)
	}
}
