package ui

import (
	"context"
	"containerd-ui/wsl"
	"strings"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/widget"
)

// Функция safeUI объявлена в containers.go, здесь она не нужна

func BuildCleanTab() fyne.CanvasObject {
	lblResult := widget.NewLabel("")
	lblResult.TextStyle = fyne.TextStyle{Bold: false}
	lblResult.Wrapping = fyne.TextTruncate
	
	// Оборачиваем в скролл-контейнер с фиксированной высотой
	resultScroll := container.NewScroll(lblResult)
	resultScroll.SetMinSize(fyne.NewSize(0, 200))

	// Кнопка 1: Очистка кэша и dangling-образов
	btnCache := widget.NewButton("Очистить кэш и dangling-образы", nil)
	btnCache.OnTapped = func() {
		safeUI(func() {
			lblResult.SetText("Выполняется очистка кэша...")
			btnCache.Disable()
		})
		go func() {
			select {
			case <-wsl.AppContext().Done():
				return
			default:
			}

			res, err := wsl.CleanNerdctlCache()
			safeUI(func() {
				if err != nil {
					lblResult.SetText("Ошибка: " + err.Error())
				} else {
					lblResult.SetText("✅ " + res)
				}
				btnCache.Enable()
				btnCache.Refresh()
				lblResult.Refresh()
			})
		}()
	}

	// Кнопка 2: Очистка неиспользуемых томов
	btnVolumes := widget.NewButton("Очистить неиспользуемые тома", nil)
	btnVolumes.OnTapped = func() {
		safeUI(func() {
			lblResult.SetText("Выполняется очистка томов...")
			btnVolumes.Disable()
		})
		go func() {
			ctx, cancel := context.WithCancel(wsl.AppContext())
			defer cancel()

			res, err := wsl.CleanUnusedVolumes(ctx)
			safeUI(func() {
				if err != nil {
					lblResult.SetText("Ошибка: " + err.Error())
				} else {
					lblResult.SetText("✅ " + res)
				}
				btnVolumes.Enable()
				btnVolumes.Refresh()
				lblResult.Refresh()
			})
		}()
	}

	// Кнопка 3: Очистка неиспользуемых сетей
	btnNetworks := widget.NewButton("Очистить неиспользуемые сети", nil)
	btnNetworks.OnTapped = func() {
		safeUI(func() {
			lblResult.SetText("Выполняется очистка сетей...")
			btnNetworks.Disable()
		})
		go func() {
			ctx, cancel := context.WithCancel(wsl.AppContext())
			defer cancel()

			res, err := wsl.CleanUnusedNetworks(ctx)
			safeUI(func() {
				if err != nil {
					lblResult.SetText("Ошибка: " + err.Error())
				} else {
					lblResult.SetText("✅ " + res)
				}
				btnNetworks.Enable()
				btnNetworks.Refresh()
				lblResult.Refresh()
			})
		}()
	}

	// Кнопка 4: Очистка образов без тегов
	btnImages := widget.NewButton("Очистить образы без тегов", nil)
	btnImages.OnTapped = func() {
		safeUI(func() {
			lblResult.SetText("Выполняется очистка образов...")
			btnImages.Disable()
		})
		go func() {
			ctx, cancel := context.WithCancel(wsl.AppContext())
			defer cancel()

			res, err := wsl.CleanUntaggedImages(ctx)
			safeUI(func() {
				if err != nil {
					lblResult.SetText("Ошибка: " + err.Error())
				} else {
					lblResult.SetText("✅ " + res)
				}
				btnImages.Enable()
				btnImages.Refresh()
				lblResult.Refresh()
			})
		}()
	}

	// Кнопка 5: Очистка кэша BuildKit
	btnBuildkit := widget.NewButton("Очистить кэш BuildKit", nil)
	btnBuildkit.OnTapped = func() {
		safeUI(func() {
			lblResult.SetText("Выполняется очистка кэша BuildKit...")
			btnBuildkit.Disable()
		})
		go func() {
			ctx, cancel := context.WithCancel(wsl.AppContext())
			defer cancel()

			res, err := wsl.CleanBuildkitCache(ctx)
			safeUI(func() {
				if err != nil {
					lblResult.SetText("Ошибка: " + err.Error())
				} else {
					lblResult.SetText(res)
				}
				btnBuildkit.Enable()
				btnBuildkit.Refresh()
				lblResult.Refresh()
			})
		}()
	}

	// Кнопка 6: Полная очистка
	btnFull := widget.NewButton("Полная очистка (все)", nil)
	btnFull.OnTapped = func() {
		safeUI(func() {
			lblResult.SetText("Выполняется полная очистка...")
			btnFull.Disable()
		})
		go func() {
			ctx, cancel := context.WithCancel(wsl.AppContext())
			defer cancel()

			var results []string

			// Очищаем кэш
			if res, err := wsl.CleanNerdctlCache(); err == nil {
				results = append(results, "🗑️ Кэш: "+res)
			}

			// Очищаем тома
			if res, err := wsl.CleanUnusedVolumes(ctx); err == nil {
				results = append(results, "📦 Тома: "+res)
			}

			// Очищаем сети
			if res, err := wsl.CleanUnusedNetworks(ctx); err == nil {
				results = append(results, "🌐 Сети: "+res)
			}

			// Очищаем образы без тегов
			if res, err := wsl.CleanUntaggedImages(ctx); err == nil {
				results = append(results, "🖼️ Образы: "+res)
			}

			// Очищаем кэш BuildKit
			if res, err := wsl.CleanBuildkitCache(ctx); err == nil {
				results = append(results, "🔨 BuildKit:\n"+res)
			}

			safeUI(func() {
				if len(results) > 0 {
					lblResult.SetText("✅ Полная очистка завершена:\n" + strings.Join(results, "\n"))
				} else {
					lblResult.SetText("✅ Всё чисто! Нечего удалять.")
				}

				btnFull.Enable()
				btnFull.Refresh()
				lblResult.Refresh()
			})
		}()
	}

	infoLabel := widget.NewLabel("Доступные операции:\n\n" +
		"🗑️ Кэш и dangling-образы — удаляет временные файлы, кэш сборки nerdctl, логи старше 7 дней\n\n" +
		"🔨 Кэш BuildKit — удаляет кэш сборки BuildKit старше 24 часов и неиспользуемые ресурсы\n\n" +
		"📦 Неиспользуемые тома — удаляет тома, которые не подключены ни к одному контейнеру\n\n" +
		"🌐 Неиспользуемые сети — удаляет пользовательские сети, не используемые контейнерами\n\n" +
		"🖼️ Образы без тегов — удаляет все образы без тегов (не только dangling, но и все незафиксированные)\n\n" +
		"⚠️ Внимание: перед полной очисткой рекомендуется проверить список ресурсов.")

	return container.NewVBox(
		widget.NewLabelWithStyle("Очистка системы", fyne.TextAlignLeading, fyne.TextStyle{Bold: true}),
		infoLabel,
		container.NewVBox(
			container.NewHBox(btnCache, btnBuildkit),
			container.NewHBox(btnVolumes, btnImages),
			container.NewBorder(nil, nil, nil, nil, btnFull),
		),
		widget.NewSeparator(),
		container.NewVBox(
			widget.NewLabelWithStyle("Результат:", fyne.TextAlignLeading, fyne.TextStyle{Bold: true}),
			resultScroll,
		),
	)
}