package ui

import (
	"containerd-ui/i18n"
	"containerd-ui/wsl"
	"context"
	"strings"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/widget"
)

func BuildDeployTab(win fyne.Window) fyne.CanvasObject {
	domainEntry := widget.NewEntry()
	domainEntry.SetPlaceHolder(i18n.T("deploy.domain_placeholder"))
	backendCheck := widget.NewCheck(i18n.T("deploy.backend"), nil)
	backendCheck.SetChecked(true)
	frontendCheck := widget.NewCheck(i18n.T("deploy.frontend"), nil)
	frontendCheck.SetChecked(true)
	backendPrefix := widget.NewEntry()
	backendPrefix.SetText("/api")
	backendPrefix.SetPlaceHolder("/api")
	tokenEntry := widget.NewEntry()
	tokenEntry.SetPlaceHolder(i18n.T("deploy.token_placeholder"))
	tokenEntry.Hidden = true

	httpsCheck := widget.NewCheck(i18n.T("deploy.https"), nil)
	httpsCheck.SetChecked(true)

	proxyRadio := widget.NewRadioGroup([]string{i18n.T("deploy.traefik"), i18n.T("deploy.cloudflare")}, nil)
	proxyRadio.Horizontal = true

	proxy := wsl.GetDeploymentProxy()
	if proxy == "cloudflare" {
		proxyRadio.SetSelected(i18n.T("deploy.cloudflare"))
		tokenEntry.Show()
		httpsCheck.Hide()
	} else {
		proxyRadio.SetSelected(i18n.T("deploy.traefik"))
	}

	status := widget.NewLabel(i18n.T("deploy.status_placeholder"))
	logs := widget.NewMultiLineEntry()
	logs.Disable()
	logs.Wrapping = fyne.TextWrapWord
	logs.SetPlaceHolder(i18n.T("deploy.logs_placeholder"))
	logs.SetMinRowsVisible(10)

	appendLog := func(line string) {
		logs.SetText(strings.TrimSpace(logs.Text + "\n" + line))
		logs.Refresh()
	}

	proxyHint := widget.NewLabel(i18n.T("deploy.proxy_hint"))
	proxyHint.TextStyle = fyne.TextStyle{Italic: true}

	cfPrefixHint := widget.NewLabel(i18n.T("deploy.cf_prefix_hint"))
	cfPrefixHint.TextStyle = fyne.TextStyle{Italic: true}
	cfPrefixHint.Wrapping = fyne.TextWrapWord
	cfPrefixHint.Hide()

	cfTokenHint := widget.NewLabel(i18n.T("deploy.cf_token_hint"))
	cfTokenHint.TextStyle = fyne.TextStyle{Italic: true}
	cfTokenHint.Wrapping = fyne.TextWrapWord

	btnSaveToken := widget.NewButton(i18n.T("deploy.save_token"), func() {
		projectPath := wsl.GetProjectPath()
		if projectPath == "" {
			status.SetText(i18n.T("deploy.path_not_set"))
			return
		}
		token := strings.TrimSpace(tokenEntry.Text)
		if token == "" {
			status.SetText(i18n.T("deploy.insert_token"))
			return
		}

		go func() {
			err := wsl.SaveCloudflareToken(projectPath, token)
			safeUI(func() {
				if err != nil {
					status.SetText(i18n.T("deploy.token_save_error", err.Error()))
				} else {
					status.SetText(i18n.T("deploy.token_saved"))
					tokenEntry.SetText("")
				}
			})
		}()
	})

	proxyOptionsContainer := container.NewVBox(httpsCheck, tokenEntry, cfTokenHint, cfPrefixHint, btnSaveToken)

	proxyRadio.OnChanged = func(value string) {
		if value == i18n.T("deploy.traefik") {
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

	btnDNS := widget.NewButton(i18n.T("deploy.check_dns"), func() {
		domain := strings.TrimSpace(domainEntry.Text)
		if err := wsl.ValidateDomain(domain); err != nil {
			status.SetText(i18n.T("deploy.dns_error", err.Error()))
			return
		}
		status.SetText(i18n.T("deploy.dns_ok", domain))
	})

	btnPorts := widget.NewButton(i18n.T("deploy.check_ports"), func() {
		go func() {
			ctx, cancel := context.WithCancel(wsl.AppContext())
			defer cancel()

			port80, port443, err := wsl.CheckPorts(ctx)
			safeUI(func() {
				if err != nil {
					status.SetText(i18n.T("deploy.proxy_log_error", err.Error()))
					return
				}

				var msg string
				if port80 && port443 {
					msg = i18n.T("deploy.ports_free")
				} else {
					var busyPorts []string
					if !port80 {
						busyPorts = append(busyPorts, "80")
					}
					if !port443 {
						busyPorts = append(busyPorts, "443")
					}
					msg = i18n.T("deploy.ports_busy", strings.Join(busyPorts, ", "))
				}
				status.SetText(msg)
			})
		}()
	})

	btnTools := widget.NewButton(i18n.T("deploy.check_tools"), func() {
		go func() {
			ctx, cancel := context.WithCancel(wsl.AppContext())
			defer cancel()

			err := wsl.CheckDeploymentPrerequisites(ctx)
			safeUI(func() {
				if err != nil {
					status.SetText("❌ " + err.Error())
				} else {
					proxy := wsl.GetDeploymentProxy()
					msg := i18n.T("deploy.tools_found")
					if proxy == "cloudflare" {
						msg += " (Traefik + Cloudflare)"
					}
					status.SetText(msg)
				}
			})
		}()
	})

	var btnDeploy *widget.Button
	btnDeploy = widget.NewButton(i18n.T("deploy.deploy"), func() {
		domain := strings.TrimSpace(domainEntry.Text)
		if domain == "" {
			status.SetText(i18n.T("deploy.specify_domain"))
			return
		}
		proxy := wsl.GetDeploymentProxy()
		if proxy == "cloudflare" && strings.TrimSpace(tokenEntry.Text) == "" {
			status.SetText(i18n.T("deploy.specify_token"))
			return
		}
		btnDeploy.Disable()
		appendLog(i18n.T("deploy.check_dns_deploy"))
		go func() {
			ctx, cancel := context.WithCancel(wsl.AppContext())
			defer cancel()
			if err := wsl.ValidateDomain(domain); err != nil {
				safeUI(func() { status.SetText(i18n.T("deploy.dns_error", err.Error())); btnDeploy.Enable() })
				return
			}

			proxy := wsl.GetDeploymentProxy()
			if proxy == "cloudflare" {
				projectPath := wsl.GetProjectPath()
				if projectPath != "" {
					safeUI(func() { appendLog(i18n.T("deploy.check_token")) })
					if err := wsl.CheckCloudflareToken(projectPath); err != nil {
						safeUI(func() { status.SetText(i18n.T("common.error") + " " + err.Error()); btnDeploy.Enable() })
						return
					}
				}
			}

			safeUI(func() {
				appendLog(i18n.T("deploy.generate_config"))
			})
			result, err := wsl.DeployDomain(ctx, domain, strings.TrimSpace(backendPrefix.Text), backendCheck.Checked, frontendCheck.Checked, httpsCheck.Checked)
			safeUI(func() {
				if err != nil {
					status.SetText(i18n.T("deploy.deploy_error"))
					appendLog(i18n.T("common.error") + ": " + err.Error())
				} else {
					if proxy == "cloudflare" {
						status.SetText(i18n.T("deploy.deploy_success_cf", domain))
						appendLog(i18n.T("deploy.tunnel_configured"))
						appendLog(i18n.T("deploy.cloudflared_needed"))
					} else {
						status.SetText(i18n.T("deploy.deploy_success", domain))
						appendLog(i18n.T("deploy.ssl_auto"))
					}
					if result != "" {
						appendLog(result)
					}
				}
				btnDeploy.Enable()
			})
		}()
	})

	btnRollback := widget.NewButton(i18n.T("deploy.rollback"), func() {
		go func() {
			ctx, cancel := context.WithCancel(wsl.AppContext())
			defer cancel()
			result, err := wsl.RollbackDomain(ctx)
			safeUI(func() {
				if err != nil {
					status.SetText(i18n.T("deploy.rollback_error", err.Error()))
				} else {
					proxy := wsl.GetDeploymentProxy()
					if proxy == "cloudflare" {
						status.SetText(i18n.T("deploy.rollback_success_cf"))
					} else {
						status.SetText(i18n.T("deploy.rollback_success"))
					}
					appendLog(result)
				}
			})
		}()
	})

	btnLogs := widget.NewButton(i18n.T("deploy.proxy_logs"), func() {
		go func() {
			ctx, cancel := context.WithCancel(wsl.AppContext())
			defer cancel()
			result, err := wsl.DomainProxyLogs(ctx)
			safeUI(func() {
				if err != nil {
					logs.SetText(i18n.T("deploy.proxy_log_error", err.Error()))
				} else {
					logs.SetText(result)
				}
				logs.Refresh()
			})
		}()
	})
	_ = win

	cardConfig := widget.NewCard(i18n.T("deploy.config_card"), "", container.NewVBox(
		container.NewBorder(nil, nil, nil, btnDNS, domainEntry),
		container.NewHBox(btnPorts, btnTools),
		container.NewBorder(
			nil, nil,
			container.NewHBox(
				widget.NewLabel(i18n.T("deploy.services")),
				backendCheck,
				frontendCheck,
				widget.NewLabel(i18n.T("deploy.backend_prefix")),
			),
			nil,
			backendPrefix,
		),
	))

	cardProxy := widget.NewCard(i18n.T("deploy.proxy"), "", container.NewVBox(
		proxyRadio,
		proxyHint,
		proxyOptionsContainer,
	))

	cardActions := widget.NewCard(i18n.T("deploy.actions_card"), "", container.NewVBox(
		container.NewAdaptiveGrid(3, btnDeploy, btnRollback, btnLogs),
		status,
	))

	cardLogs := widget.NewCard(i18n.T("deploy.logs_card"), "", logs)

	content := container.NewVBox(
		container.NewPadded(cardConfig),
		container.NewPadded(cardProxy),
		container.NewPadded(cardActions),
		container.NewPadded(cardLogs),
	)

	return container.NewVScroll(content)
}