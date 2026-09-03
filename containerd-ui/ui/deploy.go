package ui

import (
	"containerd-ui/wsl"
	"context"
	"strings"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/widget"
)

func BuildDeployTab(win fyne.Window) fyne.CanvasObject {
	domainEntry := widget.NewEntry()
	domainEntry.SetPlaceHolder("example.com")
	backendCheck := widget.NewCheck("backend", nil)
	backendCheck.SetChecked(true)
	frontendCheck := widget.NewCheck("frontend", nil)
	frontendCheck.SetChecked(true)
	backendPrefix := widget.NewEntry()
	backendPrefix.SetText("/api")
	backendPrefix.SetPlaceHolder("/api")
	tokenEntry := widget.NewEntry()
	tokenEntry.SetPlaceHolder("eyJhIjoi...")
	tokenEntry.Hidden = true

	httpsCheck := widget.NewCheck("Включить HTTPS (Let's Encrypt)", nil)
	httpsCheck.SetChecked(true)

	proxyRadio := widget.NewRadioGroup([]string{"Traefik + Let's Encrypt", "Cloudflare Tunnel"}, nil)
	proxyRadio.Horizontal = true

	proxy := wsl.GetDeploymentProxy()
	if proxy == "cloudflare" {
		proxyRadio.SetSelected("Cloudflare Tunnel")
		tokenEntry.Show()
		httpsCheck.Hide()
	} else {
		proxyRadio.SetSelected("Traefik + Let's Encrypt")
	}

	status := widget.NewLabel("Введите домен и проверьте DNS")
	status.Wrapping = fyne.TextWrapWord
	logs := widget.NewMultiLineEntry()
	logs.Disable()
	logs.Wrapping = fyne.TextWrapWord
	logs.SetPlaceHolder("Логи деплоя появятся здесь")
	logs.SetMinRowsVisible(10)

	appendLog := func(line string) {
		logs.SetText(strings.TrimSpace(logs.Text + "\n" + line))
		logs.Refresh()
	}

	// --- Объявляем переменные ДО использования в proxyOptionsContainer ---
	proxyHint := widget.NewLabel("💡 Traefik — бесплатный SSL через Let's Encrypt; Cloudflare — через Tunnel, без открытых портов (нужен cloudflared в WSL)")
	proxyHint.TextStyle = fyne.TextStyle{Italic: true}

	cfPrefixHint := widget.NewLabel("⚠️ Cloudflare Tunnel не удаляет префикс пути. Если backend ожидает /api без префикса, он должен сам обработать этот маршрут; иначе используйте Traefik.")
	cfPrefixHint.TextStyle = fyne.TextStyle{Italic: true}
	cfPrefixHint.Wrapping = fyne.TextWrapWord
	cfPrefixHint.Hide()

	cfTokenHint := widget.NewLabel("🔑 Токен можно получить: Cloudflare Dashboard → Zero Trust → Networks → Tunnels → Save or manage → JSON token")
	cfTokenHint.TextStyle = fyne.TextStyle{Italic: true}
	cfTokenHint.Wrapping = fyne.TextWrapWord

	btnSaveToken := widget.NewButton("Сохранить токен", func() {
		projectPath := wsl.GetProjectPath()
		if projectPath == "" {
			status.SetText("Сначала укажите путь к проекту в настройках")
			return
		}
		token := strings.TrimSpace(tokenEntry.Text)
		if token == "" {
			status.SetText("Вставьте токен в поле выше")
			return
		}

		go func() {
			err := wsl.SaveCloudflareToken(projectPath, token)
			safeUI(func() {
				if err != nil {
					status.SetText("Ошибка сохранения токена: " + err.Error())
				} else {
					status.SetText("✅ Токен сохранён")
					tokenEntry.SetText("")
				}
			})
		}()
	})

	proxyOptionsContainer := container.NewVBox(httpsCheck, tokenEntry, cfTokenHint, cfPrefixHint, btnSaveToken)

	proxyRadio.OnChanged = func(value string) {
		if value == "Traefik + Let's Encrypt" {
			httpsCheck.Show()
			tokenEntry.Hide()
			cfTokenHint.Hide()
			cfPrefixHint.Hide()
			btnSaveToken.Hide()
			wsl.SetDeploymentProxy("traefik")
		} else {
			httpsCheck.Hide()
			tokenEntry.Show()
			cfTokenHint.Show()
			cfPrefixHint.Show()
			btnSaveToken.Show()
			wsl.SetDeploymentProxy("cloudflare")
		}
	}

	btnDNS := widget.NewButton("Проверить DNS", func() {
		domain := strings.TrimSpace(domainEntry.Text)
		if err := wsl.ValidateDomain(domain); err != nil {
			status.SetText("DNS не подтверждён: " + err.Error())
			return
		}
		status.SetText("DNS подтверждён: " + domain)
	})

	btnPorts := widget.NewButton("Проверить порты 80/443", func() {
		go func() {
			ctx, cancel := context.WithCancel(wsl.AppContext())
			defer cancel()

			port80, port443, err := wsl.CheckPorts(ctx)
			safeUI(func() {
				if err != nil {
					status.SetText("Ошибка проверки портов: " + err.Error())
					return
				}

				var msg string
				if port80 && port443 {
					msg = "✅ Порты 80 и 443 свободны"
				} else {
					msg = "⚠️ Занятые порты: "
					if !port80 {
						msg += "80"
					}
					if !port443 {
						if msg != "⚠️ Занятые порты: " {
							msg += ", "
						}
						msg += "443"
					}
					msg += ". Освободите перед деплоем Traefik"
				}
				status.SetText(msg)
			})
		}()
	})

	btnTools := widget.NewButton("Проверить инструменты", func() {
		go func() {
			ctx, cancel := context.WithCancel(wsl.AppContext())
			defer cancel()

			err := wsl.CheckDeploymentPrerequisites(ctx)
			safeUI(func() {
				if err != nil {
					status.SetText("❌ " + err.Error())
				} else {
					proxy := wsl.GetDeploymentProxy()
					msg := "✅ Все необходимые инструменты найдены"
					if proxy == "cloudflare" {
						msg += " (Traefik + Cloudflare)"
					}
					status.SetText(msg)
				}
			})
		}()
	})

	var btnDeploy *widget.Button
	btnDeploy = widget.NewButton("Деплой", func() {
		domain := strings.TrimSpace(domainEntry.Text)
		if domain == "" {
			status.SetText("Укажите домен")
			return
		}
		proxy := wsl.GetDeploymentProxy()
		if proxy == "cloudflare" && strings.TrimSpace(tokenEntry.Text) == "" {
			status.SetText("Введите токен Cloudflare Tunnel")
			return
		}
		btnDeploy.Disable()
		appendLog("Проверка DNS...")
		go func() {
			ctx, cancel := context.WithCancel(wsl.AppContext())
			defer cancel()
			if err := wsl.ValidateDomain(domain); err != nil {
				safeUI(func() { status.SetText("Ошибка DNS: " + err.Error()); btnDeploy.Enable() })
				return
			}

			proxy := wsl.GetDeploymentProxy()
			if proxy == "cloudflare" {
				projectPath := wsl.GetProjectPath()
				if projectPath != "" {
					safeUI(func() { appendLog("Проверка токена Tunnel...") })
					if err := wsl.CheckCloudflareToken(projectPath); err != nil {
						safeUI(func() { status.SetText("Ошибка: " + err.Error()); btnDeploy.Enable() })
						return
					}
				}
			}

			safeUI(func() {
				appendLog("Генерация конфигурации...")
			})
			result, err := wsl.DeployDomain(ctx, domain, strings.TrimSpace(backendPrefix.Text), backendCheck.Checked, frontendCheck.Checked, httpsCheck.Checked)
			safeUI(func() {
				if err != nil {
					status.SetText("Деплой завершён с ошибкой")
					appendLog("Ошибка: " + err.Error())
				} else {
					if proxy == "cloudflare" {
						status.SetText("Деплой успешно завершён: https://" + domain + " (Cloudflare Tunnel)")
						appendLog("Tunnel настроен, DNS автоматически привязан к домену")
						appendLog("Убедитесь, что cloudflared установлен в WSL")
					} else {
						status.SetText("Деплой успешно завершён: https://" + domain)
						appendLog("Получение SSL-сертификата выполняется Traefik автоматически")
					}
					if result != "" {
						appendLog(result)
					}
				}
				btnDeploy.Enable()
			})
		}()
	})

	btnRollback := widget.NewButton("Откатить", func() {
		go func() {
			ctx, cancel := context.WithCancel(wsl.AppContext())
			defer cancel()
			result, err := wsl.RollbackDomain(ctx)
			safeUI(func() {
				if err != nil {
					status.SetText("Ошибка отката: " + err.Error())
				} else {
					proxy := wsl.GetDeploymentProxy()
					if proxy == "cloudflare" {
						status.SetText("Cloudflare Tunnel остановлен")
					} else {
						status.SetText("Traefik остановлен")
					}
					appendLog(result)
				}
			})
		}()
	})

	btnLogs := widget.NewButton("Логи прокси", func() {
		go func() {
			ctx, cancel := context.WithCancel(wsl.AppContext())
			defer cancel()
			result, err := wsl.DomainProxyLogs(ctx)
			safeUI(func() {
				if err != nil {
					logs.SetText("Ошибка: " + err.Error())
				} else {
					logs.SetText(result)
				}
				logs.Refresh()
			})
		}()
	})
	_ = win

	// --- Карточки для группировки ---
	cardConfig := widget.NewCard("Конфигурация", "", container.NewVBox(
		container.NewBorder(nil, nil, nil, btnDNS, domainEntry),
		container.NewHBox(btnPorts, btnTools),
		// ИСПРАВЛЕНИЕ: Слева метки и чекбоксы, справа — поле ввода (занимает всё свободное место)
		container.NewBorder(
			nil, nil,
			container.NewHBox(
				widget.NewLabel("Сервисы:"),
				backendCheck,
				frontendCheck,
				widget.NewLabel("Префикс backend:"),
			),
			nil,
			backendPrefix,
		),
	))

	cardProxy := widget.NewCard("Прокси и SSL", "", container.NewVBox(
		proxyRadio,
		proxyHint,
		proxyOptionsContainer,
	))

	cardActions := widget.NewCard("Действия", "", container.NewVBox(
		container.NewAdaptiveGrid(3, btnDeploy, btnRollback, btnLogs),
		status,
	))

	cardLogs := widget.NewCard("Логи деплоя", "", logs)

	content := container.NewVBox(
		container.NewPadded(cardConfig),
		container.NewPadded(cardProxy),
		container.NewPadded(cardActions),
		container.NewPadded(cardLogs),
	)

	return container.NewVScroll(content)
}