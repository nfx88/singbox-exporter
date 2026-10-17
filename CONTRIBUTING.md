# Участие в разработке

Спасибо, что решили помочь! Этот файл объясняет, как эффективно работать над
`singbox-exporter`. Проект маленький и сфокусированный, поэтому мы предпочитаем
несколько простых файлов большому количеству структуры.

## Структура репозитория

| Путь | Назначение |
|---|---|
| `main.go` | entrypoint, флаги, HTTP-сервер, graceful shutdown |
| `client.go` | HTTP-клиент Clash API sing-box и авторизация |
| `types.go` | JSON-типы payload'ов Clash API |
| `store.go` | секционный снэпшот и рендер Prometheus-текста |
| `collect.go` | сбор, фильтрация и агрегация метрик |
| `logging.go` | leveled-логгер (`-log-level`, `-log-format`) и backoff ошибок |
| `*_test.go` | unit/e2e-тесты (фейковый Clash API через `net/http/httptest`) |
| `docs/clash-api.md` (RU) · `docs/clash-api.en.md` (EN) | reference Clash API |
| `README.md` (RU) · `README.en.md` (EN) | главная документация |

## Окружение

- Go ≥ 1.27 (модуль таргетит `go 1.27`, тулчейн пинится `toolchain go1.27.2`; свежее toolchain тоже подойдёт).
- Без сторонних Go-зависимостей — проект намеренно **stdlib only**.
  Пожалуйста, сохраняйте это, если нет очень веских причин.
- `make` — опционально, но удобно для типовых задач ниже.

## Типовые команды

```sh
go build ./...          # компиляция
go vet ./...            # статический анализ
go test ./...           # прогон тестов
go test -race ./...     # прогон тестов с race-детектором
gofmt -l .              # список неотформатированных файлов
golangci-lint run       # опционально, см. .golangci.yml
make                    # vet + test + build одним заходом
make release            # кросс-компиляция статических бинарников в dist/
```

## Добавление метрики

1. Изучите соответствующий handler Clash API в
   [исходниках sing-box](https://github.com/SagerNet/sing-box/tree/testing/experimental/clashapi) —
   имена полей и семантика берутся из реализации, а не из памяти.
2. Добавьте поле payload'а в `types.go`, если оно ещё не разбирается.
3. Эмитируйте sample в соответствующем коллекторе `collect.go` (connections, proxies
   или version) — либо в новом коллекторе, если нужен новый эндпоинт.
4. Следуйте соглашениям об именовании:
   - точки в логическом имени → подчёркивания (`traffic_download_bytes_total`);
   - суффиксы `_total` (counter), `_bytes`, `_seconds`, `_info`, `_up` используются
     консистентно;
   - тип хранится в поле `typ` sample'а — держите его в синхроне.
5. Обновите `collect_test.go` реалистичным payload-ассертом и задокументируйте
   метрику в `README.md`/`README.en.md` и примерах в `examples/`.
6. Добавьте строку в `CHANGELOG.md` (и `CHANGELOG.en.md`) в раздел *Unreleased*.

> Документация ведётся на русском (основные файлы) и на английском (файлы `*.en.md`).
> При изменении одного языка обновляйте и другой, иначе они разойдутся.

## Pull requests

- Держите изменения маленькими и сфокусированными; несвязанную работу разносите по PR.
- Убедитесь, что `gofmt`, `go vet ./...` и `go test ./...` проходят локально (если доступен
  `golangci-lint run` — и он тоже).
- Ссылайтесь на issue, которое закрывает PR, например `Fixes #12`.
- Пишите понятные коммит-сообщения: короткое императивное резюме, при необходимости
  — пустая строка и более длинное объяснение, *зачем* сделано изменение.

## Стиль коммит-сообщений

```
Fix singbox_proxy_down false positives for direct outbound

/version /proxies returns every tagged outbound including direct, block
and DNS-only entries, which have no URL-test history. Only real proxy
types are now considered for delay/down metrics.
```

## Трианжирование issues

- **Баг-репорты**: укажите флаги экспортера, версию sing-box и, если возможно,
  сырой ответ одного эндпоинта Clash API (`curl -s <api>/proxies`).
- **Фича-реквесты**: опишите нужную метрику, запрос, который вы хотите выполнять,
  и форму вашего sing-box-конфига.

## Код поведения

Все участники обязаны следовать [Кодексу поведения](CODE_OF_CONDUCT.md).
