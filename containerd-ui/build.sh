#!/bin/sh
# Скрипт для сборки containerd-ui в Windows .exe
# Сборка выполняется в Alpine/WSL2 и выпускает Windows .exe

set -eu

# Добавляем Go в PATH если он уже установлен вне системного PATH.
if ! command -v go >/dev/null 2>&1; then
    export PATH="/usr/local/go/bin:$PATH"
fi

# Определяем директорию скрипта (абсолютный путь, устойчивый к относительным вызовам и симлинкам)
SCRIPT_DIR="$(cd "$(dirname "$(readlink -f "$0")")" && pwd)"
OUTPUT_DIR="$SCRIPT_DIR/dist"
OUTPUT_FILE="$OUTPUT_DIR/containerd-ui.exe"

echo "📦 Проверка зависимостей..."
echo "   Директория: $SCRIPT_DIR"

# Устанавливаем Go и MinGW для кросс-компиляции, если их нет.
if ! command -v go >/dev/null 2>&1 || ! command -v x86_64-w64-mingw32-gcc >/dev/null 2>&1; then
    if ! command -v apk >/dev/null 2>&1; then
        echo "❌ Сборка требует Alpine Linux с apk, Go и MinGW-w64."
        exit 1
    fi
    echo "⚙️ Установка Go и MinGW-w64..."
    if [ "$(id -u)" -eq 0 ]; then
        apk add --no-cache go mingw-w64-gcc
    elif command -v doas >/dev/null 2>&1; then
        doas apk add --no-cache go mingw-w64-gcc
    else
        echo "❌ Запустите сборку от root или настройте doas для установки зависимостей."
        exit 1
    fi
fi

echo "📦 Загрузка зависимостей Go..."
cd "$SCRIPT_DIR"
go mod tidy

echo "🔨 Компиляция для Windows (GOOS=windows GOARCH=amd64)..."
mkdir -p "$OUTPUT_DIR"
GOOS=windows GOARCH=amd64 CGO_ENABLED=1 CC=x86_64-w64-mingw32-gcc CXX=x86_64-w64-mingw32-g++ \
    go build -ldflags="-s -w -H windowsgui" -o "$OUTPUT_FILE" .

echo "✅ Сборка завершена! Файл: $OUTPUT_FILE"
ls -lh "$OUTPUT_FILE"