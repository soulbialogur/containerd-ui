# Диагностика окружения

Этот файл является единственным справочником по проверкам окружения и командной диагностике. В установке, деплое и troubleshooting оставлены только короткие ссылки на него, чтобы не повторять один и тот же набор команд в нескольких местах.

## 1. Проверка WSL

```powershell
wsl --list --verbose
```

Если дистрибутива нет:

```powershell
wsl --install Ubuntu-24.04
```

Проверка внутри WSL:

```powershell
wsl -d Ubuntu-24.04 -- nerdctl version
wsl -d Ubuntu-24.04 -- systemctl is-active containerd
wsl -d Ubuntu-24.04 -- systemctl is-active buildkit
```

## 2. Проверка containerd и nerdctl

```bash
nerdctl info
systemctl status containerd
```

Если контейнерный runtime не запущен:

```bash
sudo systemctl enable containerd
sudo systemctl start containerd
sudo systemctl status containerd
```

## 3. Проверка BuildKit

```bash
sudo systemctl status buildkit
sudo systemctl is-active buildkit
```

Если сервис не запущен:

```bash
sudo systemctl start buildkit
```

Если нужен прямой старт:

```bash
sudo buildkitd --addr unix:///run/buildkit/buildkitd.sock
```

## 4. Проверка инструментов деплоя

Для деплоя должны быть доступны:

- `nerdctl`
- `containerd`
- `buildctl`
- `cloudflared` (если выбран Cloudflare Tunnel)

Проверка:

```bash
cloudflared --version
cloudflared tunnel list --help
```

Если нужна валидация credentials:

```bash
cloudflared tunnel list --credentials-file /path/to/credentials.json
```

## 5. Проверка портов 80 и 443

Windows:

```powershell
netstat -ano | findstr :80
netstat -ano | findstr :443
```

Linux внутри WSL:

```bash
sudo ss -tulpn | grep ':80\|:443'
```

Для Traefik порты `80` и `443` должны быть свободны.

## 6. Проверка DNS

```bash
nslookup example.com
```

или:

```bash
getent hosts example.com
```

Перед деплоем домен должен корректно резолвиться и указывать на целевой сервер.

## 7. Проверка сети `soul-dialogue`

Проверить, что сеть существует:

```bash
nerdctl network ls
```

Если её нет, можно создать вручную:

```bash
nerdctl network create --driver bridge soul-dialogue
```

Но в compose-файле она всё равно должна быть объявлена как external:

```yaml
networks:
  soul-dialogue:
    external: true
    name: soul-dialogue
```

## 8. Проверка project path

Путь к проекту должен указывать на корень проекта, а не на файл `compose.yaml`.

Проверить можно по фактической структуре:

```text
project/
├── compose.yaml
├── backend/
├── frontend/
└── ...
```

## 9. Когда использовать этот файл

Используйте этот документ, если нужно быстро проверить:

- WSL и Linux-среду;
- `containerd`, `nerdctl`, `buildkitd`;
- порты и DNS;
- Cloudflare credentials;
- сеть `soul-dialogue`.

Подробнее:

- [installation.md](installation.md)
- [deployment.md](deployment.md)
- [troubleshooting.md](troubleshooting.md)
- [project-requirements.md](project-requirements.md)
