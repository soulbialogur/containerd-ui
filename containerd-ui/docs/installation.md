# Установка окружения

## Требования

- Windows 10/11
- WSL2
- Ubuntu 24.04
- доступ к PowerShell
- права на установку пакетов в WSL
- Go 1.26+ — требуется для сборки приложения из исходников

## Установка WSL

```powershell
wsl --install Ubuntu-24.04
```

Проверка:

```powershell
wsl --list --verbose
```

## Проверка контейнерной среды

Для полного списка команд и сценариев проверки используйте [diagnostics.md](diagnostics.md). Там собраны проверки WSL, `containerd`, `nerdctl`, `buildkitd`, портов, DNS и Cloudflare credentials.

Если чего-то нет — установите или запустите сервисы вручную и затем повторно сверяйте состояние по диагностическому разделу.

## Установка containerd и nerdctl

```bash
sudo apt update
sudo apt install -y containerd nerdctl
```

После установки проверьте:

```bash
nerdctl version
nerdctl info
```

## Установка и запуск BuildKit

BuildKit обязателен для сборки образов в приложении. Установите пакет и убедитесь, что сервис доступен в WSL:

```bash
sudo apt install -y buildkit
sudo systemctl enable buildkit
sudo systemctl start buildkit
```

Полный набор команд проверки и сценариев старта/ошибок собран в [diagnostics.md](diagnostics.md). Если демон не запускается или падает, см. также [troubleshooting.md](troubleshooting.md). Приложение умеет запускать `buildkitd` автоматически в момент сборки, если он не активен.

## Запуск containerd

```bash
sudo systemctl enable containerd
sudo systemctl start containerd
sudo systemctl status containerd
```

## Установка Cloudflare Tunnel

Если планируется работать через Cloudflare Tunnel, установите `cloudflared` по официальной инструкции:

- https://developers.cloudflare.com/cloudflare-one/connections/connect-networks/downloads/

Важно: `cloudflared` должен быть доступен в `PATH` внутри WSL. Если бинарник установлен, но не виден в shell, приложение не сможет корректно проверить токен и деплой будет остановлен до запуска прокси.

Полная проверка токена и сценарий настройки описаны в [deployment.md](deployment.md). В этом документе достаточно установить бинарник и убедиться, что он доступен в `PATH`.

## Проверка портов

Для Traefik + Let's Encrypt порты `80` и `443` должны быть свободны. Подробная логика проверки и объяснение, почему конфликт может быть на уровне Windows и WSL, описаны в [deployment.md](deployment.md). Для диагностики и команд см. также [diagnostics.md](diagnostics.md).

Если порты заняты, сначала освободите их или остановите конфликтующий сервис.

## Проверка внешней сети проекта

Требования к сети и compose-файлу собраны в [project-requirements.md](project-requirements.md). Там описаны внешний `network`, привязка сервисов `backend`/`frontend` и корректный корень проекта.

Если сеть отсутствует, приложение может создать её автоматически, но в compose-файле она всё равно должна быть объявлена как `external: true` с именем из `deploy_network`, иначе окружение будет работать некорректно или деплой не запустится.

Для быстрых проверок окружения и команд см. [diagnostics.md](diagnostics.md).

## Рекомендуемая схема окружения

```text
Windows
└── WSL Ubuntu 24.04
    ├── containerd
    ├── nerdctl
    ├── buildkitd
    ├── cloudflared
    └── app project
```

## Как приложение обращается к контейнерам

Основная схема работы с контейнерами и fallback-логика описаны в [concepts.md](concepts.md). В этом разделе достаточно помнить, что приоритет у gRPC API containerd, а WSL + nerdctl используется как резервный путь при сбоях или недоступности gRPC.

## Где хранится конфигурация

Все настройки деплоя, пути к проекту и параметры окружения сохраняются в `config.json` рядом с исполняемым файлом `containerd-ui.exe`. Это включает WSL-дистрибутив, proxy, домены, сервисы и внутренние порты приложений.

## Что дальше

После подготовки окружения переходите к [Быстрому старту](quickstart.md) или [Конфигурации](configuration.md).
