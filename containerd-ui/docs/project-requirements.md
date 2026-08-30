# Требования к проекту

Для корректной сборки и деплоя проект должен соответствовать базовым требованиям.

## 1. Compose-файл

В корне проекта должен быть файл `compose.yaml` или `docker-compose.yml`.

```text
project/
├── compose.yaml
├── backend/
├── frontend/
└── ...
```

Правильный корень проекта — это папка, в которой лежит compose-файл, а не отдельный сервис.

## 2. Сеть `soul-dialogue`

Проект должен содержать внешнюю сеть `soul-dialogue`:

```yaml
networks:
  soul-dialogue:
    external: true
    name: soul-dialogue
```

Сервисы должны быть подключены к этой сети:

```yaml
services:
  backend:
    networks:
      - soul-dialogue

  frontend:
    networks:
      - soul-dialogue
```

Если сеть отсутствует или объявлена как обычная, а не внешняя, деплой и маршрутизация будут работать некорректно.

## 3. Backend и frontend

Для деплоя нужно, чтобы проект имел корректно настроенные сервисы `backend` и `frontend`, а также внутренние порты, с которыми приложение будет работать.

Типичные параметры:

- backend service name: `backend`
- frontend service name: `frontend`
- backend port: `8000`
- frontend port: `80`

Эти значения сравниваются с настройками в `config.json`.

## 4. DNS и домен

Для публикации на домен DNS должен указывать на целевой сервер, а домен должен быть корректно настроен в выбранном proxy-режиме.

## 5. Когда использовать этот документ

Используйте этот раздел, если:

- проверяете проект перед сборкой;
- готовите compose-файл для деплоя;
- убеждаетесь, что сеть `soul-dialogue` объявлена корректно;
- проверяете названия сервисов и их внутренние порты.

См. также:

- [quickstart.md](quickstart.md)
- [deployment.md](deployment.md)
- [diagnostics.md](diagnostics.md)
