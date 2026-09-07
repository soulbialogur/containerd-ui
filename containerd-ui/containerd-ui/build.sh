#!/bin/bash
# Скрипт для сборки containerd-ui в Windows .exe

set -euo pipefail

# Определяем директорию скрипта (абсолютный путь, устойчивый к относительным вызовам и симлинкам)
SCRIPT_DIR="$(cd "$(dirname "$(readlink -f "${BASH_SOURCE[0]}")")" && pwd)"

echo "📦 Проверка зависимостей..."
echo "   Директория: $SCRIPT_DIR"

# Устанавливаем MinGW если его нет
if ! command -v x86_64-w64-mingw32-gcc &> /dev/null; then
    echo "⚙️ Установка MinGW для кросс-компиляции..."
    apt-get update > /dev/null 2>&1 || true
    apt-get install -y mingw-w64 > /dev/null 2>&1 || true
fi

echo "📦 Загрузка зависимостей Go..."
cd "$SCRIPT_DIR"
go mod tidy

echo "🔨 Компиляция для Windows (GOOS=windows GOARCH=amd64)..."
GOOS=windows GOARCH=amd64 CGO_ENABLED=1 CC=x86_64-w64-mingw32-gcc CXX=x86_64-w64-mingw32-g++ \
    go build -ldflags="-s -w -H windowsgui" -o containerd-ui.exe .

echo "✅ Сборка завершена! Файл: containerd-ui.exe"
ls -lh containerd-ui.exe