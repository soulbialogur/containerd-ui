package ui

import (
	"containerd-ui/i18n"
	"containerd-ui/wsl"
	"context"
	"fmt"
	"strings"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/widget"
)

func BuildCleanTab() fyne.CanvasObject {
	lblResult := widget.NewLabel("")
	lblResult.TextStyle = fyne.TextStyle{Bold: false}
	lblResult.Wrapping = fyne.TextWrapWord

	resultScroll := container.NewScroll(lblResult)
	resultScroll.SetMinSize(fyne.NewSize(0, 200))

	var formatCleanupError func(string, error) string

	formatCacheResult := func(result string, err error) string {
		result = wsl.CleanCleanupOutput(result)
		if err != nil {
			return formatCleanupError("кэш и dangling-образы", fmt.Errorf("%s\n%s", err.Error(), result))
		}

		lines := make([]string, 0)
		for _, line := range strings.Split(strings.TrimSpace(result), "\n") {
			line = strings.TrimSpace(line)
			if line != "" {
				lines = append(lines, line)
			}
		}
		if len(lines) == 0 {
			lines = append(lines, i18n.T("clean.nothing_to_remove"))
		}

		return i18n.T("clean.cleaned") + "\n\n" +
			i18n.T("clean.cache") + "\n" +
			strings.Join(lines, "\n")
	}

	formatCleanupError = func(operation string, err error) string {
		message := strings.ToLower(wsl.CleanCleanupOutput(err.Error()))
		switch {
		case strings.Contains(message, "buildkit is not running"), strings.Contains(message, "no buildkit host"), strings.Contains(message, "buildkitd не запущен"), strings.Contains(message, "buildctl needs to be installed"), strings.Contains(message, "buildkitd.sock"):
			return "⚠️ " + operation + ": BuildKit не запущен, очистка пропущена"
		case strings.Contains(message, "failed to ping to host"):
			return "⚠️ " + operation + ": сервис containerd недоступен"
		default:
			return i18n.T("common.error") + "\n\n" + wsl.CleanCleanupOutput(err.Error())
		}
	}

	formatCleanupResult := func(operation, result string, err error) string {
		if err != nil {
			return formatCleanupError(operation, err)
		}
		cleaned := wsl.CleanCleanupOutput(result)
		lower := strings.ToLower(cleaned)
		if cleaned == "" || strings.Contains(lower, "не найдены") || strings.Contains(lower, "нечего") {
			return "✅ " + operation + ": нечего удалять"
		}
		if strings.Contains(lower, "не удалось удалить сеть") {
			return "ℹ️ " + operation + ": сеть используется контейнерами"
		}
		return "✅ " + operation + " завершено"
	}

	btnCache := widget.NewButton(i18n.T("clean.cache"), nil)
	btnCache.OnTapped = func() {
		safeUI(func() {
			lblResult.SetText(i18n.T("clean.cleaning_cache"))
			btnCache.Disable()
		})
		go func() {
			defer func() {
				if r := recover(); r != nil {
					safeUI(func() {
						lblResult.SetText(i18n.T("clean.panic", r))
						btnCache.Enable()
						btnCache.Refresh()
						lblResult.Refresh()
					})
				}
			}()
			select {
			case <-wsl.AppContext().Done():
				return
			default:
			}
			res, err := wsl.CleanNerdctlCache()
			safeUI(func() {
				lblResult.SetText(formatCacheResult(res, err))
				btnCache.Enable()
				btnCache.Refresh()
				lblResult.Refresh()
			})
		}()
	}

	btnVolumes := widget.NewButton(i18n.T("clean.volumes"), nil)
	btnVolumes.OnTapped = func() {
		safeUI(func() {
			lblResult.SetText(i18n.T("clean.cleaning_volumes"))
			btnVolumes.Disable()
		})
		go func() {
			defer func() {
				if r := recover(); r != nil {
					safeUI(func() {
						lblResult.SetText(i18n.T("clean.panic", r))
						btnVolumes.Enable()
						btnVolumes.Refresh()
						lblResult.Refresh()
					})
				}
			}()
			select {
			case <-wsl.AppContext().Done():
				return
			default:
			}
			ctx, cancel := context.WithCancel(wsl.AppContext())
			defer cancel()
			res, err := wsl.CleanUnusedVolumes(ctx)
			safeUI(func() {
				if err != nil {
					lblResult.SetText(formatCleanupResult("Неиспользуемые тома", res, err))
				} else {
					lblResult.SetText(formatCleanupResult("Неиспользуемые тома", res, nil))
				}
				btnVolumes.Enable()
				btnVolumes.Refresh()
				lblResult.Refresh()
			})
		}()
	}

	btnNetworks := widget.NewButton(i18n.T("clean.networks"), nil)
	btnNetworks.OnTapped = func() {
		safeUI(func() {
			lblResult.SetText(i18n.T("clean.cleaning_networks"))
			btnNetworks.Disable()
		})
		go func() {
			defer func() {
				if r := recover(); r != nil {
					safeUI(func() {
						lblResult.SetText(i18n.T("clean.panic", r))
						btnNetworks.Enable()
						btnNetworks.Refresh()
						lblResult.Refresh()
					})
				}
			}()
			select {
			case <-wsl.AppContext().Done():
				return
			default:
			}
			ctx, cancel := context.WithCancel(wsl.AppContext())
			defer cancel()
			res, err := wsl.CleanUnusedNetworks(ctx)
			safeUI(func() {
				if err != nil {
					lblResult.SetText(formatCleanupResult("Неиспользуемые сети", res, err))
				} else {
					lblResult.SetText(formatCleanupResult("Неиспользуемые сети", res, nil))
				}
				btnNetworks.Enable()
				btnNetworks.Refresh()
				lblResult.Refresh()
			})
		}()
	}

	btnImages := widget.NewButton(i18n.T("clean.images"), nil)
	btnImages.OnTapped = func() {
		safeUI(func() {
			lblResult.SetText(i18n.T("clean.cleaning_images"))
			btnImages.Disable()
		})
		go func() {
			defer func() {
				if r := recover(); r != nil {
					safeUI(func() {
						lblResult.SetText(i18n.T("clean.panic", r))
						btnImages.Enable()
						btnImages.Refresh()
						lblResult.Refresh()
					})
				}
			}()
			select {
			case <-wsl.AppContext().Done():
				return
			default:
			}
			ctx, cancel := context.WithCancel(wsl.AppContext())
			defer cancel()
			res, err := wsl.CleanUntaggedImages(ctx)
			safeUI(func() {
				if err != nil {
					lblResult.SetText(formatCleanupResult("Образы без тегов", res, err))
				} else {
					lblResult.SetText(formatCleanupResult("Образы без тегов", res, nil))
				}
				btnImages.Enable()
				btnImages.Refresh()
				lblResult.Refresh()
			})
		}()
	}

	btnBuildkit := widget.NewButton(i18n.T("clean.buildkit"), nil)
	btnBuildkit.OnTapped = func() {
		safeUI(func() {
			lblResult.SetText(i18n.T("clean.cleaning_buildkit"))
			btnBuildkit.Disable()
		})
		go func() {
			defer func() {
				if r := recover(); r != nil {
					safeUI(func() {
						lblResult.SetText(i18n.T("clean.panic", r))
						btnBuildkit.Enable()
						btnBuildkit.Refresh()
						lblResult.Refresh()
					})
				}
			}()
			select {
			case <-wsl.AppContext().Done():
				return
			default:
			}
			ctx, cancel := context.WithCancel(wsl.AppContext())
			defer cancel()
			res, err := wsl.CleanBuildkitCache(ctx)
			safeUI(func() {
				if err != nil {
					lblResult.SetText(formatCleanupResult("Кэш BuildKit", res, err))
				} else {
					lblResult.SetText(formatCleanupResult("Кэш BuildKit", res, nil))
				}
				btnBuildkit.Enable()
				btnBuildkit.Refresh()
				lblResult.Refresh()
			})
		}()
	}

	btnFull := widget.NewButton(i18n.T("clean.full"), nil)
	btnFull.OnTapped = func() {
		safeUI(func() {
			lblResult.SetText(i18n.T("clean.cleaning_full"))
			btnFull.Disable()
		})
		go func() {
			defer func() {
				if r := recover(); r != nil {
					safeUI(func() {
						lblResult.SetText(i18n.T("clean.panic", r))
						btnFull.Enable()
						btnFull.Refresh()
						lblResult.Refresh()
					})
				}
			}()
			select {
			case <-wsl.AppContext().Done():
				return
			default:
			}
			ctx, cancel := context.WithCancel(wsl.AppContext())
			defer cancel()

			var results []string

			if res, err := wsl.CleanNerdctlCache(); err == nil {
				results = append(results, i18n.T("clean.cache")+": "+wsl.CleanCleanupOutput(res))
			}
			if res, err := wsl.CleanContainerdLogs(); err == nil {
				results = append(results, "Логи: "+wsl.CleanCleanupOutput(res))
			}
			if res, err := wsl.CleanUnusedVolumes(ctx); err == nil {
				results = append(results, i18n.T("clean.volumes")+": "+wsl.CleanCleanupOutput(res))
			}
			if res, err := wsl.CleanUnusedNetworks(ctx); err == nil {
				results = append(results, i18n.T("clean.networks")+": "+wsl.CleanCleanupOutput(res))
			}
			if res, err := wsl.CleanUntaggedImages(ctx); err == nil {
				results = append(results, i18n.T("clean.images")+": "+wsl.CleanCleanupOutput(res))
			}
			if res, err := wsl.CleanBuildkitCache(ctx); err == nil {
				results = append(results, i18n.T("clean.buildkit")+":\n"+wsl.CleanCleanupOutput(res))
			}

			safeUI(func() {
				if len(results) > 0 {
					lblResult.SetText(i18n.T("clean.cleaned") + ":\n" + strings.Join(results, "\n"))
				} else {
					lblResult.SetText(i18n.T("clean.cleaned_all"))
				}
				btnFull.Enable()
				btnFull.Refresh()
				lblResult.Refresh()
			})
		}()
	}

	infoLabel := widget.NewLabel(i18n.T("clean.info"))

	return withResponsiveScroll(container.NewVBox(
		widget.NewLabelWithStyle(i18n.T("clean.title"), fyne.TextAlignLeading, fyne.TextStyle{Bold: true}),
		infoLabel,
		container.NewVBox(
			container.NewHBox(btnCache, btnBuildkit),
			container.NewHBox(btnVolumes, btnImages),
			container.NewBorder(nil, nil, nil, nil, btnFull),
		),
		widget.NewSeparator(),
		container.NewVBox(
			widget.NewLabelWithStyle(i18n.T("clean.result_title"), fyne.TextAlignLeading, fyne.TextStyle{Bold: true}),
			resultScroll,
		),
	))
}
