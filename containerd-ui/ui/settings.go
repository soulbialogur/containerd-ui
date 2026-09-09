package ui

import (
	"containerd-ui/i18n"
	"containerd-ui/wsl"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/widget"
)

type ScrollableEntry struct {
	widget.Entry
}

func NewScrollableEntry() *ScrollableEntry {
	entry := &ScrollableEntry{}
	entry.Entry.ExtendBaseWidget(entry)
	return entry
}

func (e *ScrollableEntry) Scrolled(_ *fyne.ScrollEvent) {}

func makeSettingEntry(placeHolder string) *ScrollableEntry {
	entry := NewScrollableEntry()
	entry.SetPlaceHolder(placeHolder)
	return entry
}

func makeSettingRow(entry *ScrollableEntry) fyne.CanvasObject {
	return container.NewMax(entry)
}

func deploymentProxyUIValue(configValue string) string {
	switch configValue {
	case "cloudflare":
		return "Cloudflare Tunnel"
	default:
		return "Traefik + Let's Encrypt"
	}
}

func deploymentProxyConfigValue(uiValue string) string {
	switch uiValue {
	case "Cloudflare Tunnel":
		return "cloudflare"
	default:
		return "traefik"
	}
}

func BuildSettingsTab(win fyne.Window) fyne.CanvasObject {
	config, err := wsl.LoadConfig()
	if err != nil || config == nil {
		config = wsl.DefaultConfig()
	}

	// --- Переключатель языка ---
	langRadio := widget.NewRadioGroup([]string{i18n.T("app.lang_ru"), i18n.T("app.lang_en")}, nil)
	langRadio.Horizontal = true
	currentLang := i18n.GetCurrentLocale()
	if currentLang == i18n.LocaleEN {
		langRadio.SetSelected(i18n.T("app.lang_en"))
	} else {
		langRadio.SetSelected(i18n.T("app.lang_ru"))
	}
	langRadio.OnChanged = func(value string) {
		var locale i18n.Locale
		if value == i18n.T("app.lang_en") {
			locale = i18n.LocaleEN
		} else {
			locale = i18n.LocaleRU
		}
		i18n.SetLocale(locale)
		// Сохраняем язык в config.json
		cfg, err := wsl.LoadConfig()
		if err == nil {
			cfg.Language = string(locale)
			wsl.SaveConfig(cfg)
		}
		// Перезапускаем приложение для применения языка
		dlg := dialog.NewCustom(i18n.T("settings.lang_restart"), i18n.T("dialogs.ok"),
			widget.NewLabel(i18n.T("settings.lang_restart_msg")), win)
		dlg.Show()
		wsl.Shutdown()
	}
	langHint := widget.NewLabel(i18n.T("settings.lang_hint"))
	langHint.TextStyle = fyne.TextStyle{Italic: true}

	entryProjectName := makeSettingEntry(i18n.T("settings.project_name_placeholder"))

	entryPath := makeSettingEntry(i18n.T("settings.project_path_placeholder"))
	entryPath.SetText(wsl.GetActiveProjectPath())
	entryPath.OnChanged = func(text string) {
		projects := wsl.GetProjects()
		for _, p := range projects {
			if p.Path == text {
				entryProjectName.SetText(p.Name)
				return
			}
		}
	}

	projectsList := widget.NewList(
		func() int {
			return len(wsl.GetProjects())
		},
		func() fyne.CanvasObject {
			label := widget.NewLabel("template")
			label.Wrapping = fyne.TextTruncate
			return label
		},
		func(id widget.ListItemID, item fyne.CanvasObject) {
			projects := wsl.GetProjects()
			if id < len(projects) {
				label := item.(*widget.Label)
				label.SetText(projects[id].NameWithFallback())
			}
		},
	)

	btnAddProject := widget.NewButton(i18n.T("settings.add_project"), nil)
	btnRemoveProject := widget.NewButton(i18n.T("settings.remove_project"), nil)
	btnRenameProject := widget.NewButton(i18n.T("settings.rename_project"), nil)

	var selectedProjectPath string

	updateProjectsList := func() {
		projectsList.Refresh()
	}

	btnAddProject.OnTapped = func() {
		dialog.ShowFileOpen(func(u fyne.URIReadCloser, err error) {
			if err != nil || u == nil {
				return
			}
			path := u.URI().Path()
			if path == "" {
				u.Close()
				return
			}
			u.Close()

			composeExists := false
			for _, name := range []string{"compose.yaml", "docker-compose.yml"} {
				if _, err := os.Stat(filepath.Join(path, name)); err == nil {
					composeExists = true
					break
				}
			}

			if err := wsl.AddProject(path); err != nil {
				dialog.ShowError(err, win)
				return
			}

			entryPath.SetText(path)
			entryProjectName.SetText("")
			updateProjectsList()

			msg := "Проект добавлен: " + path
			if composeExists {
				msg += "\n✅ Найдён docker-compose.yml"
			} else {
				msg += "\n⚠️ Не найден docker-compose.yml"
			}
			dialog.ShowCustom("Проект добавлен", "ОК", widget.NewLabel(msg), win)
		}, win)
	}

	btnRemoveProject.OnTapped = func() {
		if selectedProjectPath == "" {
			dialog.ShowInformation(i18n.T("dialogs.warning"), i18n.T("settings.no_selection_remove"), win)
			return
		}

		confirmDialog := dialog.NewCustomConfirm(
			i18n.T("settings.confirm_remove_project"),
			i18n.T("settings.remove_project"),
			i18n.T("dialogs.cancel"),
			widget.NewLabel(i18n.T("settings.confirm_remove_project_text")+selectedProjectPath),
			func(confirmed bool) {
				if !confirmed {
					return
				}
				if err := wsl.RemoveProject(selectedProjectPath); err != nil {
					dialog.ShowError(err, win)
					return
				}
				selectedProjectPath = ""
				entryPath.SetText(wsl.GetActiveProjectPath())
				entryProjectName.SetText("")
				updateProjectsList()
				dialog.ShowCustom(i18n.T("settings.project_removed"), i18n.T("dialogs.ok"), widget.NewLabel(i18n.T("settings.project_removed_msg")), win)
			},
			win,
		)
		confirmDialog.Show()
	}

	btnRenameProject.OnTapped = func() {
		if selectedProjectPath == "" {
			dialog.ShowInformation(i18n.T("dialogs.warning"), i18n.T("settings.no_selection_rename"), win)
			return
		}

		renameEntry := widget.NewEntry()
		renameEntry.SetPlaceHolder(i18n.T("settings.rename_placeholder"))
		renameEntry.SetText(wsl.ActiveProject().NameWithFallback())

		dlg := dialog.NewCustomConfirm(i18n.T("settings.rename"), i18n.T("settings.rename"), i18n.T("dialogs.cancel"), renameEntry, func(ok bool) {
			if !ok || renameEntry.Text == "" {
				return
			}
			if err := wsl.RenameProject(selectedProjectPath, renameEntry.Text); err != nil {
				dialog.ShowError(err, win)
				return
			}
			updateProjectsList()
			dialog.ShowCustom(i18n.T("settings.renamed"), i18n.T("dialogs.ok"), widget.NewLabel(i18n.T("settings.renamed_msg")), win)
		}, win)
		dlg.Show()
	}

	projectsList.OnSelected = func(id widget.ListItemID) {
		projects := wsl.GetProjects()
		if id < len(projects) {
			selectedProjectPath = projects[id].Path
			entryPath.SetText(projects[id].Path)
			entryProjectName.SetText(projects[id].Name)
		}
	}
	projectsList.OnUnselected = func(id widget.ListItemID) {
		_ = id
	}

	entryDistro := makeSettingEntry(i18n.T("settings.wsl_distro_placeholder"))
	entryShell := makeSettingEntry(i18n.T("settings.shell_placeholder"))
	entryInitSystem := makeSettingEntry(i18n.T("settings.init_system_placeholder"))
	entryPkgManager := makeSettingEntry(i18n.T("settings.pkg_manager_placeholder"))
	entryPrivilegeCmd := makeSettingEntry(i18n.T("settings.privilege_cmd_placeholder"))
	entryCdPort := makeSettingEntry(i18n.T("settings.grpc_port_placeholder"))
	entryCdNamespace := makeSettingEntry(i18n.T("settings.namespace_placeholder"))
	entryLogTail := makeSettingEntry(i18n.T("settings.log_tail_placeholder"))
	entryCacheTTL := makeSettingEntry(i18n.T("settings.cache_ttl_placeholder"))
	entryMaxWSLCacheSize := makeSettingEntry(i18n.T("settings.max_cache_size_placeholder"))
	entryWSLCacheCleanupAt := makeSettingEntry(i18n.T("settings.cache_cleanup_placeholder"))
	entryRefreshInterval := makeSettingEntry(i18n.T("settings.auto_refresh_placeholder"))
	entryIdleStopMinutes := makeSettingEntry(i18n.T("settings.idle_stop_placeholder"))

	checkEconomyMode := widget.NewCheck(i18n.T("settings.economy_mode"), nil)
	checkEconomyMode.SetChecked(config.EconomyMode)

	entryCPU := makeSettingEntry(i18n.T("settings.cpu_placeholder"))
	entryMemory := makeSettingEntry(i18n.T("settings.memory_placeholder"))
	entryMaxParallel := makeSettingEntry(i18n.T("settings.parallel_builds_placeholder"))
	entryContainerConcurrency := makeSettingEntry(i18n.T("settings.container_concurrency_placeholder"))
	entryBuildkitTTL := makeSettingEntry(i18n.T("settings.buildkit_ttl_placeholder"))
	entryBuildkitSize := makeSettingEntry(i18n.T("settings.buildkit_max_size_placeholder"))
	entryBuildkitSize.SetText(config.BuildkitMaxSize)

	proxyRadio := widget.NewRadioGroup([]string{i18n.T("deploy.traefik"), i18n.T("deploy.cloudflare")}, nil)
	proxyRadio.Horizontal = true
	proxyRadio.SetSelected(deploymentProxyUIValue(config.DeploymentProxy))
	if config.DeploymentProxy == "" || (config.DeploymentProxy != "traefik" && config.DeploymentProxy != "cloudflare") {
		proxyRadio.SetSelected("Traefik + Let's Encrypt")
	}
	proxyHint := widget.NewLabel("💡 Traefik — бесплатный SSL через Let's Encrypt; Cloudflare — через Tunnel, без открытых портов")
	proxyHint.TextStyle = fyne.TextStyle{Italic: true}

	entryBackendService := makeSettingEntry(i18n.T("settings.backend_service_placeholder"))
	entryBackendService.SetText(config.DeployServiceBackend)
	if config.DeployServiceBackend == "" {
		entryBackendService.SetText("backend")
	}

	entryBackendPort := makeSettingEntry(i18n.T("settings.backend_port"))
	entryBackendPort.SetText(strconv.Itoa(config.DeployServiceBackendPort))
	if config.DeployServiceBackendPort == 0 {
		entryBackendPort.SetText("8000")
	}

	entryDeployEmail := makeSettingEntry(i18n.T("settings.deploy_email"))
	entryDeployEmail.SetPlaceHolder(i18n.T("settings.deploy_email_placeholder"))
	if config.DeployEmail != "" {
		entryDeployEmail.SetText(config.DeployEmail)
	}

	entryDeployNetwork := makeSettingEntry(i18n.T("settings.deploy_network_placeholder"))
	entryDeployNetwork.SetText(config.DeployNetwork)
	if config.DeployNetwork == "" {
		entryDeployNetwork.SetText("soul-dialogue")
	}

	entryFrontendService := makeSettingEntry(i18n.T("settings.frontend_service_placeholder"))
	entryFrontendService.SetText(config.DeployServiceFrontend)
	if config.DeployServiceFrontend == "" {
		entryFrontendService.SetText("frontend")
	}

	entryFrontendPort := makeSettingEntry(i18n.T("settings.frontend_port"))
	entryFrontendPort.SetText(strconv.Itoa(config.DeployServiceFrontendPort))
	if config.DeployServiceFrontendPort == 0 {
		entryFrontendPort.SetText("80")
	}
	serviceHint := widget.NewLabel(i18n.T("settings.project_name_hint"))
	serviceHint.TextStyle = fyne.TextStyle{Italic: true}

	checkSquash := widget.NewCheck(i18n.T("settings.squash_layers"), nil)
	checkSquash.SetChecked(config.SquashLayers)

	compressionRadio := widget.NewRadioGroup([]string{"gzip", "zstd", "none"}, nil)
	compressionRadio.Horizontal = true
	compressionRadio.SetSelected(config.Compression)

	compressionLevel := widget.NewSlider(1, 9)
	compressionLevel.SetValue(float64(config.CompressionLevel))
	compressionLevel.Step = 1
	if config.Compression == "gzip" || config.Compression == "zstd" {
		compressionLevel.Enable()
	} else {
		compressionLevel.Disable()
	}
	compressionLevelLabel := widget.NewLabel(i18n.T("settings.compression_level", float64(config.CompressionLevel)))

	compressionLevel.OnChanged = func(value float64) {
		compressionLevelLabel.SetText(i18n.T("settings.compression_level", value))
	}

	compressionRadio.OnChanged = func(value string) {
		if value == "gzip" || value == "zstd" {
			compressionLevel.Enable()
		} else {
			compressionLevel.Disable()
		}
	}

	btnOpenExplorer := widget.NewButton(i18n.T("settings.open_explorer"), nil)
	btnCheckPath := widget.NewButton(i18n.T("settings.check_path"), nil)
	btnDetect := widget.NewButton(i18n.T("settings.auto_detect"), nil)
	btnDetectEnv := widget.NewButton(i18n.T("settings.detect_env"), nil)
	btnSave := widget.NewButton(i18n.T("settings.save"), nil)
	btnReset := widget.NewButton(i18n.T("settings.reset"), nil)

	updateUI := func() {
		cfg, _ := wsl.LoadConfig()
		entryPath.SetText(wsl.GetActiveProjectPath())
		entryDistro.SetText(cfg.WslDistro)
		entryShell.SetText(cfg.Shell)
		entryInitSystem.SetText(cfg.InitSystem)
		entryPkgManager.SetText(cfg.PkgManager)
		entryPrivilegeCmd.SetText(cfg.PrivilegeCmd)
		entryCdPort.SetText(strconv.Itoa(cfg.CdPort))
		entryCdNamespace.SetText(cfg.CdNamespace)
		entryLogTail.SetText(strconv.Itoa(cfg.LogTail))
		entryCacheTTL.SetText(strconv.Itoa(cfg.WslCacheTTL))
		entryMaxWSLCacheSize.SetText(strconv.FormatInt(cfg.MaxWSLCacheSize, 10))
		entryWSLCacheCleanupAt.SetText(strconv.Itoa(cfg.WSLCacheCleanupAt))
		entryRefreshInterval.SetText(strconv.Itoa(cfg.AutoRefreshInterval))
		entryIdleStopMinutes.SetText(strconv.Itoa(cfg.IdleDaemonStopMinutes))
		checkEconomyMode.SetChecked(cfg.EconomyMode)
		entryCPU.SetText(cfg.DefaultCPU)
		entryMemory.SetText(cfg.DefaultMemory)
		entryMaxParallel.SetText(strconv.Itoa(cfg.MaxParallelism))
		entryContainerConcurrency.SetText(strconv.Itoa(cfg.ContainerOperationConcurrency))
		entryBuildkitTTL.SetText(strconv.Itoa(cfg.BuildkitCacheTTL))
		entryBuildkitSize.SetText(cfg.BuildkitMaxSize)
		checkSquash.SetChecked(cfg.SquashLayers)
		compressionRadio.SetSelected(cfg.Compression)
		compressionLevel.SetValue(float64(cfg.CompressionLevel))
		compressionLevelLabel.SetText(i18n.T("settings.compression_level", float64(cfg.CompressionLevel)))
		if cfg.Compression == "gzip" || cfg.Compression == "zstd" {
			compressionLevel.Enable()
		} else {
			compressionLevel.Disable()
		}
		proxyRadio.SetSelected(deploymentProxyUIValue(cfg.DeploymentProxy))
		if cfg.DeployEmail != "" {
			entryDeployEmail.SetText(cfg.DeployEmail)
		} else {
			entryDeployEmail.SetText("")
		}
		entryBackendService.SetText(cfg.DeployServiceBackend)
		entryBackendPort.SetText(strconv.Itoa(cfg.DeployServiceBackendPort))
		entryFrontendService.SetText(cfg.DeployServiceFrontend)
		entryFrontendPort.SetText(strconv.Itoa(cfg.DeployServiceFrontendPort))
		entryDeployNetwork.SetText(cfg.DeployNetwork)
	}

	btnOpenExplorer.OnTapped = func() {
		go func() {
			select {
			case <-wsl.AppContext().Done():
				return
			default:
			}
			cmd := exec.Command("explorer.exe")
			cmd.Start()
		}()
	}

	btnCheckPath.OnTapped = func() {
		path := entryPath.Text
		if path == "" {
			dialog.ShowInformation(i18n.T("dialogs.warning"), i18n.T("settings.no_path_msg"), win)
			return
		}

		path = filepath.Clean(path)

		dirInfo, err := os.Stat(path)
		if err != nil || !dirInfo.IsDir() {
			dialog.ShowCustom(i18n.T("settings.path_not_found"), i18n.T("dialogs.ok"), widget.NewLabel(i18n.T("settings.path_not_found_msg", path, err)), win)
			return
		}

		composeExists := false
		composeFiles := []string{"compose.yaml", "docker-compose.yml"}
		for _, name := range composeFiles {
			filePath := filepath.Join(path, name)
			if _, err := os.Stat(filePath); err == nil {
				composeExists = true
				break
			}
		}

		if composeExists {
			dialog.ShowCustom(i18n.T("settings.path_checked_ok"), i18n.T("dialogs.ok"),
				widget.NewLabel(i18n.T("settings.path_ok", path)),
				win)
		} else {
			dialog.ShowCustom(i18n.T("settings.path_checked_warn"), i18n.T("dialogs.ok"),
				widget.NewLabel(i18n.T("settings.path_warn", path)),
				win)
		}
	}

	btnDetect.OnTapped = func() {
		btnDetect.Disable()
		btnDetect.SetText(i18n.T("settings.searching"))
		btnDetect.Refresh()

		go func() {
			select {
			case <-wsl.AppContext().Done():
				return
			default:
			}

			path := wsl.DetectProjectPath()
			if path != "" {
				err := wsl.SetProjectPath(path)
				if err != nil {
					dialog.ShowError(err, win)
				} else {
					updateUI()
					dialog.ShowCustom(i18n.T("settings.detected"), i18n.T("dialogs.ok"), widget.NewLabel(i18n.T("settings.detected_msg", path)), win)
				}
			} else {
				dialog.ShowCustom(i18n.T("settings.not_detected"), i18n.T("dialogs.ok"), widget.NewLabel(i18n.T("settings.not_detected_msg")), win)
			}
			btnDetect.Enable()
			btnDetect.SetText(i18n.T("settings.auto_detect"))
			btnDetect.Refresh()
		}()
	}

	btnDetectEnv.OnTapped = func() {
		btnDetectEnv.Disable()
		btnDetectEnv.SetText(i18n.T("settings.searching"))
		btnDetectEnv.Refresh()

		go func() {
			select {
			case <-wsl.AppContext().Done():
				return
			default:
			}

			env := wsl.DetectEnvironment()
			msg := fmt.Sprintf("🔧 Оболочка: %s\n📦 Пакетный менеджер: %s\n⚙️ Init-система: %s\n🔑 Повышение привилегий: %s",
				env.Shell, env.PkgManager, env.InitSystem, env.PrivilegeCmd)
			dialog.ShowCustom(i18n.T("settings.env_detected"), i18n.T("dialogs.ok"), widget.NewLabel(msg), win)
			btnDetectEnv.Enable()
			btnDetectEnv.SetText(i18n.T("settings.detect_env"))
			btnDetectEnv.Refresh()
		}()
	}

	btnSave.OnTapped = func() {
		cfg, _ := wsl.LoadConfig()

		if entryPath.Text != "" {
			path := filepath.Clean(entryPath.Text)
			if err := wsl.AddProject(path); err != nil {
				if setErr := wsl.SetActiveProject(path); setErr != nil {
					dialog.ShowError(setErr, win)
					return
				}
			}
			entryPath.SetText(path)
		}
		if distro := strings.TrimSpace(entryDistro.Text); distro != "" {
			if !wsl.IsWslDistroAvailable(distro) {
				detected := wsl.DetectWslDistro()
				if detected == "" {
					dialog.ShowError(fmt.Errorf("WSL-дистрибутив %q не найден. Проверьте установленные дистрибутивы командой: wsl.exe -l -q", distro), win)
					return
				}
				entryDistro.SetText(detected)
				distro = detected
			}
			cfg.WslDistro = distro
		} else {
			cfg.WslDistro = wsl.DetectWslDistro()
		}
		if shell := entryShell.Text; shell != "" {
			cfg.Shell = shell
		}
		if initSys := entryInitSystem.Text; initSys != "" {
			cfg.InitSystem = initSys
		}
		if pkgMgr := entryPkgManager.Text; pkgMgr != "" {
			cfg.PkgManager = pkgMgr
		}
		if privCmd := entryPrivilegeCmd.Text; privCmd != "" {
			cfg.PrivilegeCmd = privCmd
		}
		if portStr := entryCdPort.Text; portStr != "" {
			if port, err := strconv.Atoi(portStr); err == nil {
				cfg.CdPort = port
			}
		}
		if namespace := strings.TrimSpace(entryCdNamespace.Text); namespace != "" {
			cfg.CdNamespace = namespace
		}
		if tailStr := entryLogTail.Text; tailStr != "" {
			if tail, err := strconv.Atoi(tailStr); err == nil {
				cfg.LogTail = tail
			}
		}
		if ttlStr := entryCacheTTL.Text; ttlStr != "" {
			if ttl, err := strconv.Atoi(ttlStr); err == nil {
				cfg.WslCacheTTL = ttl
			}
		}
		if maxSizeStr := entryMaxWSLCacheSize.Text; maxSizeStr != "" {
			if maxSize, err := strconv.ParseInt(maxSizeStr, 10, 64); err == nil && maxSize > 0 {
				cfg.MaxWSLCacheSize = maxSize
			}
		}
		if cleanupAtStr := entryWSLCacheCleanupAt.Text; cleanupAtStr != "" {
			if cleanupAt, err := strconv.Atoi(cleanupAtStr); err == nil && cleanupAt > 0 {
				cfg.WSLCacheCleanupAt = cleanupAt
			}
		}
		if refreshStr := entryRefreshInterval.Text; refreshStr != "" {
			if refresh, err := strconv.Atoi(refreshStr); err == nil {
				cfg.AutoRefreshInterval = refresh
			}
		}
		if idleStopStr := entryIdleStopMinutes.Text; idleStopStr != "" {
			if idleStop, err := strconv.Atoi(idleStopStr); err == nil && idleStop > 0 {
				cfg.IdleDaemonStopMinutes = idleStop
			}
		}
		cfg.EconomyMode = checkEconomyMode.Checked

		cfg.DefaultCPU = entryCPU.Text
		cfg.DefaultMemory = entryMemory.Text

		if parallelStr := entryMaxParallel.Text; parallelStr != "" {
			if parallel, err := strconv.Atoi(parallelStr); err == nil && parallel >= 0 {
				cfg.MaxParallelism = parallel
			}
		}
		if concurrencyStr := entryContainerConcurrency.Text; concurrencyStr != "" {
			if concurrency, err := strconv.Atoi(concurrencyStr); err == nil && concurrency >= 1 {
				cfg.ContainerOperationConcurrency = concurrency
			}
		}

		if ttlStr := entryBuildkitTTL.Text; ttlStr != "" {
			if ttl, err := strconv.Atoi(ttlStr); err == nil && ttl >= 0 {
				cfg.BuildkitCacheTTL = ttl
			}
		}
		cfg.BuildkitMaxSize = entryBuildkitSize.Text

		cfg.SquashLayers = checkSquash.Checked
		cfg.Compression = compressionRadio.Selected
		cfg.CompressionLevel = int(compressionLevel.Value)
		cfg.DeploymentProxy = deploymentProxyConfigValue(proxyRadio.Selected)
		cfg.DeployEmail = strings.TrimSpace(entryDeployEmail.Text)
		cfg.DeployServiceBackend = strings.TrimSpace(entryBackendService.Text)
		cfg.DeployServiceFrontend = strings.TrimSpace(entryFrontendService.Text)
		if network := strings.TrimSpace(entryDeployNetwork.Text); network != "" {
			cfg.DeployNetwork = network
		}
		if port, err := strconv.Atoi(entryBackendPort.Text); err == nil && port > 0 {
			cfg.DeployServiceBackendPort = port
		}
		if port, err := strconv.Atoi(entryFrontendPort.Text); err == nil && port > 0 {
			cfg.DeployServiceFrontendPort = port
		}

		if err := wsl.SaveConfig(cfg); err != nil {
			dialog.ShowError(err, win)
			return
		}

		wsl.InitConfigCache(cfg)
		SetEconomyMode(cfg.EconomyMode)
		if cfg.IdleDaemonStopMinutes > 0 {
			wsl.SetIdleDaemonThresholdForRuntime(cfg.IdleDaemonStopMinutes)
		}
		dialog.ShowCustom(i18n.T("settings.saved"), i18n.T("dialogs.ok"), widget.NewLabel(i18n.T("settings.saved_hint")), win)
	}

	btnReset.OnTapped = func() {
		confirmDialog := dialog.NewCustomConfirm(
			i18n.T("settings.confirm_reset"),
			i18n.T("settings.reset"),
			i18n.T("dialogs.cancel"),
			widget.NewLabel(i18n.T("settings.confirm_reset_text")),
			func(confirmed bool) {
				if !confirmed {
					return
				}
				cfg := wsl.DefaultConfig()
				if err := wsl.SaveConfig(cfg); err != nil {
					dialog.ShowError(err, win)
					return
				}
				wsl.InitConfigCache(cfg)
				SetEconomyMode(cfg.EconomyMode)
				updateUI()
				dialog.ShowCustom(i18n.T("settings.reset_done"), i18n.T("dialogs.ok"), widget.NewLabel(i18n.T("settings.reset_hint")), win)
			},
			win,
		)
		confirmDialog.Show()
	}

	infoLabel := widget.NewLabel(i18n.T("settings.info_text"))
	infoLabel.Wrapping = fyne.TextTruncate

	basicCard := widget.NewCard(i18n.T("settings.basic"), "",
		container.NewVBox(
			widget.NewLabelWithStyle(i18n.T("settings.project_path"), fyne.TextAlignLeading, fyne.TextStyle{Bold: true}),
			makeSettingRow(entryPath),
			container.NewHBox(btnOpenExplorer, btnCheckPath, btnDetect),
			widget.NewSeparator(),

			widget.NewLabelWithStyle(i18n.T("settings.wsl_distro"), fyne.TextAlignLeading, fyne.TextStyle{Bold: true}),
			makeSettingRow(entryDistro),
			widget.NewSeparator(),

			widget.NewLabelWithStyle(i18n.T("settings.grpc_port"), fyne.TextAlignLeading, fyne.TextStyle{Bold: true}),
			makeSettingRow(entryCdPort),
			widget.NewSeparator(),

			widget.NewLabelWithStyle(i18n.T("settings.namespace"), fyne.TextAlignLeading, fyne.TextStyle{Bold: true}),
			makeSettingRow(entryCdNamespace),
			widget.NewSeparator(),

			widget.NewLabelWithStyle(i18n.T("settings.log_tail"), fyne.TextAlignLeading, fyne.TextStyle{Bold: true}),
			makeSettingRow(entryLogTail),
			widget.NewSeparator(),

			widget.NewLabelWithStyle(i18n.T("settings.cache_ttl"), fyne.TextAlignLeading, fyne.TextStyle{Bold: true}),
			makeSettingRow(entryCacheTTL),
			widget.NewSeparator(),

			widget.NewLabelWithStyle(i18n.T("settings.max_cache_size"), fyne.TextAlignLeading, fyne.TextStyle{Bold: true}),
			makeSettingRow(entryMaxWSLCacheSize),
			widget.NewSeparator(),

			widget.NewLabelWithStyle(i18n.T("settings.cache_cleanup"), fyne.TextAlignLeading, fyne.TextStyle{Bold: true}),
			makeSettingRow(entryWSLCacheCleanupAt),
			widget.NewSeparator(),

			widget.NewLabelWithStyle(i18n.T("settings.auto_refresh"), fyne.TextAlignLeading, fyne.TextStyle{Bold: true}),
			makeSettingRow(entryRefreshInterval),
			widget.NewSeparator(),
			widget.NewLabelWithStyle(i18n.T("settings.idle_stop"), fyne.TextAlignLeading, fyne.TextStyle{Bold: true}),
			makeSettingRow(entryIdleStopMinutes),
		),
	)

	buildCard := widget.NewCard(i18n.T("settings.build"), "",
		container.NewVBox(
			checkSquash,
			widget.NewSeparator(),
			widget.NewLabelWithStyle(i18n.T("settings.compression"), fyne.TextAlignLeading, fyne.TextStyle{Bold: true}),
			compressionRadio,
			container.NewHBox(compressionLevel, compressionLevelLabel),
			container.NewHBox(
				widget.NewLabel(i18n.T("settings.compression_hint")),
			),
		),
	)

	cpuHint := widget.NewLabel(i18n.T("settings.cpu_hint"))
	cpuHint.TextStyle = fyne.TextStyle{Italic: true}
	memoryHint := widget.NewLabel(i18n.T("settings.memory_hint"))
	memoryHint.TextStyle = fyne.TextStyle{Italic: true}
	parallelHint := widget.NewLabel(i18n.T("settings.parallel_hint"))
	parallelHint.TextStyle = fyne.TextStyle{Italic: true}
	buildkitTTLLimit := widget.NewLabel(i18n.T("settings.buildkit_ttl_hint"))
	buildkitTTLLimit.TextStyle = fyne.TextStyle{Italic: true}
	buildkitSizeLimit := widget.NewLabel(i18n.T("settings.buildkit_size_hint"))
	buildkitSizeLimit.TextStyle = fyne.TextStyle{Italic: true}

	proxyCard := widget.NewCard(i18n.T("settings.proxy"), "",
		container.NewVBox(
			proxyRadio,
			proxyHint,
		),
	)

	serviceCard := widget.NewCard(i18n.T("settings.services"), i18n.T("settings.project_name_hint"),
		container.NewVBox(
			widget.NewLabelWithStyle(i18n.T("settings.deploy_network"), fyne.TextAlignLeading, fyne.TextStyle{Bold: true}),
			makeSettingRow(entryDeployNetwork),
			widget.NewLabel(i18n.T("settings.network_hint")),
			widget.NewSeparator(),
			widget.NewLabelWithStyle(i18n.T("settings.backend_service"), fyne.TextAlignLeading, fyne.TextStyle{Bold: true}),
			makeSettingRow(entryBackendService),
			widget.NewSeparator(),
			widget.NewLabelWithStyle(i18n.T("settings.frontend_service"), fyne.TextAlignLeading, fyne.TextStyle{Bold: true}),
			makeSettingRow(entryFrontendService),
			serviceHint,
		),
	)

	limitCard := widget.NewCard(i18n.T("settings.limits"), "",
		container.NewVBox(
			widget.NewLabelWithStyle(i18n.T("settings.cpu_limit"), fyne.TextAlignLeading, fyne.TextStyle{Bold: true}),
			makeSettingRow(entryCPU),
			cpuHint,
			widget.NewSeparator(),
			widget.NewLabelWithStyle(i18n.T("settings.memory_limit"), fyne.TextAlignLeading, fyne.TextStyle{Bold: true}),
			makeSettingRow(entryMemory),
			memoryHint,
			widget.NewSeparator(),
			widget.NewLabelWithStyle(i18n.T("settings.parallel_builds"), fyne.TextAlignLeading, fyne.TextStyle{Bold: true}),
			makeSettingRow(entryMaxParallel),
			parallelHint,
			widget.NewSeparator(),
			widget.NewLabelWithStyle(i18n.T("settings.container_concurrency"), fyne.TextAlignLeading, fyne.TextStyle{Bold: true}),
			makeSettingRow(entryContainerConcurrency),
			widget.NewLabel(i18n.T("settings.container_concurrency_hint")),
			widget.NewSeparator(),
			widget.NewLabelWithStyle(i18n.T("settings.buildkit_ttl"), fyne.TextAlignLeading, fyne.TextStyle{Bold: true}),
			makeSettingRow(entryBuildkitTTL),
			buildkitTTLLimit,
			widget.NewSeparator(),
			widget.NewLabelWithStyle(i18n.T("settings.buildkit_max_size"), fyne.TextAlignLeading, fyne.TextStyle{Bold: true}),
			makeSettingRow(entryBuildkitSize),
			buildkitSizeLimit,
		),
	)

	projectCard := widget.NewCard(i18n.T("settings.projects"), i18n.T("settings.project_list_hint"),
		container.NewVBox(
			widget.NewLabelWithStyle(i18n.T("settings.active_project"), fyne.TextAlignLeading, fyne.TextStyle{Bold: true}),
			container.NewVBox(
				makeSettingRow(entryPath),
				container.NewHBox(btnAddProject, btnRemoveProject, btnRenameProject),
				widget.NewSeparator(),
			),
			widget.NewLabelWithStyle(i18n.T("settings.project_list"), fyne.TextAlignLeading, fyne.TextStyle{Bold: true}),
			container.NewMax(projectsList),
			widget.NewLabel(i18n.T("settings.project_list_hint")),
		),
	)

	envCard := widget.NewCard(i18n.T("settings.environment"), i18n.T("settings.environment_hint"),
		container.NewVBox(
			widget.NewLabelWithStyle(i18n.T("settings.shell"), fyne.TextAlignLeading, fyne.TextStyle{Bold: true}),
			makeSettingRow(entryShell),
			widget.NewSeparator(),
			widget.NewLabelWithStyle(i18n.T("settings.init_system"), fyne.TextAlignLeading, fyne.TextStyle{Bold: true}),
			makeSettingRow(entryInitSystem),
			widget.NewSeparator(),
			widget.NewLabelWithStyle(i18n.T("settings.pkg_manager"), fyne.TextAlignLeading, fyne.TextStyle{Bold: true}),
			makeSettingRow(entryPkgManager),
			widget.NewSeparator(),
			widget.NewLabelWithStyle(i18n.T("settings.privilege_cmd"), fyne.TextAlignLeading, fyne.TextStyle{Bold: true}),
			makeSettingRow(entryPrivilegeCmd),
			container.NewHBox(btnDetectEnv),
		),
	)

	// Управление конфигурацией всегда находится в самом низу страницы.
	actionsCard := widget.NewCard("", "",
		container.NewVBox(
			container.NewHBox(btnSave, btnReset),
			checkEconomyMode,
			widget.NewSeparator(),
			infoLabel,
		),
	)

	langCard := widget.NewCard(i18n.T("settings.language"), "",
		container.NewVBox(
			langRadio,
			langHint,
		),
	)

	content := container.NewVBox(
		container.NewPadded(langCard),
		container.NewPadded(basicCard),
		container.NewPadded(envCard),
		container.NewPadded(buildCard),
		container.NewPadded(proxyCard),
		container.NewPadded(serviceCard),
		container.NewPadded(limitCard),
		container.NewPadded(projectCard),
		container.NewPadded(actionsCard),
	)

	updateUI()

	return container.NewBorder(
		nil, nil, nil, nil,
		container.NewScroll(content),
	)
}
