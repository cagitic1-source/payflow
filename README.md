# payflow

HTTP API для приёма платежей на Go: создание платежа с ключом идемпотентности,
получение платежа по id, ошибки в формате RFC 9457 (`application/problem+json`).

Данные пока хранятся в памяти процесса и пропадают при перезапуске.

## Быстрый старт

Нужны Go 1.27.1+ и, для линтера, [golangci-lint](https://golangci-lint.run/) v2.14.0.

```sh
make run        # сервер на :8080
```

Создать платёж:

```sh
curl -i -X POST localhost:8080/v1/payments \
  -H 'Content-Type: application/json' \
  -H 'Idempotency-Key: order-42' \
  -d '{"merchant_id":"m-1","amount_minor":10000,"currency":"RUB"}'
```

```http
HTTP/1.1 202 Accepted
Content-Type: application/json
Location: /v1/payments/01a10c4f-76a6-7271-b9d5-ea0c410052b0
X-Request-Id: 01a10c4f-76a6-706f-9077-a0281b1dc938

{"id":"01a10c4f-76a6-7271-b9d5-ea0c410052b0","merchant_id":"m-1","amount_minor":10000,"currency":"RUB","status":"pending","created_at":"2026-10-05T13:44:56.742153529Z","updated_at":"2026-10-05T13:44:56.742153529Z"}
```

Готовые запросы для REST Client в VS Code лежат в [api/payments.http](api/payments.http).

Сервер останавливается по Ctrl+C или SIGTERM: новые соединения не принимаются,
начатые запросы получают до 20 секунд на завершение.

## Настройка

Через переменные окружения. Незаданная переменная - значение по умолчанию.

| Переменная                    | По умолчанию | Что задаёт                                   |
|-------------------------------|--------------|----------------------------------------------|
| `PAYFLOW_ADDR`                | `:8080`      | адрес, на котором слушает сервер             |
| `PAYFLOW_READ_HEADER_TIMEOUT` | `5s`         | чтение заголовков запроса                    |
| `PAYFLOW_READ_TIMEOUT`        | `10s`        | чтение всего запроса вместе с телом          |
| `PAYFLOW_WRITE_TIMEOUT`       | `15s`        | запись ответа                                |
| `PAYFLOW_IDLE_TIMEOUT`        | `60s`        | простой keep-alive соединения                |
| `PAYFLOW_SHUTDOWN_TIMEOUT`    | `20s`        | сколько ждать начатые запросы при остановке  |

Таймауты пишутся в формате Go: `500ms`, `10s`, `1m`. Ноль, отрицательное или
неразборчивое значение - ошибка при запуске.

```sh
PAYFLOW_ADDR=:9090 PAYFLOW_SHUTDOWN_TIMEOUT=5s make run
```

## API

| Метод | Путь                | Что делает                     |
|-------|---------------------|--------------------------------|
| POST  | `/v1/payments`      | создаёт платёж                 |
| GET   | `/v1/payments/{id}` | возвращает платёж              |
| GET   | `/healthz`          | проверка живости, отвечает `ok` |

### POST /v1/payments

Заголовок `Idempotency-Key` обязателен, от 1 до 255 символов.

Тело запроса:

| Поле           | Тип    | Правила                                         |
|----------------|--------|-------------------------------------------------|
| `merchant_id`  | string | не пустой                                       |
| `amount_minor` | int64  | сумма в минимальных единицах (копейки, центы), > 0 |
| `currency`     | string | `RUB`, `USD` или `EUR`, только заглавными       |

Неизвестные поля, данные после JSON-объекта и тело больше 1 МБ считаются
ошибкой (400).

Ответ `202 Accepted`: тело платежа и заголовок `Location` с его адресом.
Платёж создаётся в статусе `pending`.

### GET /v1/payments/{id}

Ответ `200 OK` с телом платежа или `404` с `payment-not-found`.

### Платёж

| Поле             | Описание                                              |
|------------------|-------------------------------------------------------|
| `id`             | UUIDv7                                                |
| `merchant_id`    | мерчант                                               |
| `amount_minor`   | сумма в минимальных единицах                          |
| `currency`       | код валюты ISO 4217                                   |
| `status`         | `pending`, `processing`, `approved`, `declined`, `failed` |
| `failure_reason` | причина отказа; есть только у `declined` и `failed`   |
| `created_at`     | время создания, UTC                                   |
| `updated_at`     | время последней смены статуса, UTC                    |

Разрешённые переходы статусов:

```
pending -> processing -> approved
                      -> declined
                      -> failed
```

`approved`, `declined` и `failed` конечные. Переходы описаны в доменной модели,
но обработки платежей ещё нет, так что через API сейчас видны только платежи
в `pending`.

### Идемпотентность

Ключ действует в пределах одного мерчанта и живёт 24 часа. Повтор запроса
с тем же ключом:

| Ситуация                                  | Ответ                                           |
|-------------------------------------------|-------------------------------------------------|
| то же тело, первый запрос завершён        | `202` с тем же платежом и `Idempotent-Replayed: true` |
| то же тело, первый запрос ещё выполняется | `409` `idempotency-request-in-progress`          |
| другое тело                               | `422` `idempotency-key-reused`                   |

«То же тело» значит совпадают `merchant_id`, `amount_minor` и `currency`.
Если создание платежа сорвалось, ключ освобождается и запрос можно повторить.
Истёкшие ключи удаляются раз в час.

### Ошибки

Все ошибки приходят как `application/problem+json`:

```json
{
  "type": "https://payflow.dev/problems/invalid-amount",
  "title": "Invalid amount",
  "status": 422,
  "detail": "amount_minor must be positive",
  "instance": "/v1/payments",
  "request_id": "01a10c4f-76b0-7c7a-8aad-b7a5086cfae4",
  "field": "amount_minor"
}
```

`field` есть, только если ошибка относится к конкретному полю. Последняя часть
`type`:

| Статус | `type`                            | Когда                                     |
|--------|-----------------------------------|-------------------------------------------|
| 400    | `malformed-request`               | невалидный JSON, неизвестное поле, лишние данные, тело больше 1 МБ |
| 400    | `missing-idempotency-key`         | нет `Idempotency-Key` или он длиннее 255  |
| 404    | `payment-not-found`               | платежа с таким id нет                    |
| 409    | `idempotency-request-in-progress` | запрос с этим ключом ещё выполняется      |
| 422    | `empty-merchant-id`               | пустой `merchant_id`                      |
| 422    | `invalid-amount`                  | `amount_minor` <= 0                       |
| 422    | `empty-currency`                  | пустая `currency`                         |
| 422    | `unsupported-currency`            | валюта не из списка                       |
| 422    | `idempotency-key-reused`          | ключ уже использован с другим телом       |
| 422    | `validation-error`                | прочие ошибки валидации                   |
| 503    | `service-overloaded`              | сервис перегружен; есть `Retry-After: 1`  |
| 500    | `about:blank`                     | внутренняя ошибка                         |

### Request ID

У каждого ответа есть заголовок `X-Request-Id`, его же содержит поле
`request_id` в ошибках и логах. Входящий `X-Request-Id` сохраняется, если он
из 1-64 символов `[a-zA-Z0-9_-]`; иначе сервер генерирует UUIDv7.

## Устройство

```
cmd/payment-api/              точка входа: сборка зависимостей, запуск сервера
features/payment/
  service/                    сценарии: создание и получение платежа, идемпотентность
  memory/                     хранилища в памяти: платежи и ключи идемпотентности
  transport/httptransport/    HTTP-обработчики, DTO, таблица ошибок -> ответы
internal/
  app/http/middleware/        RequestID и Recover (паника -> 500)
  app/http/v1/router/         маршруты API v1
  app/http/v1/server/         HTTP-сервер с таймаутами и корректной остановкой
  core/domain/payment/        модель платежа, проверки, статусы и переходы
  core/errors/payment_errors/ доменные ошибки
  core/requestctx/            request id и логгер в context
api/payments.http             примеры запросов
```

Зависимости направлены внутрь: транспорт знает о сервисе, сервис о домене
и о своих интерфейсах `PaymentRepository` и `IdempotencyStore`, домен ни о ком.
Хранилище в памяти реализует эти интерфейсы, и его можно заменить на БД,
не трогая сервис.

## Разработка

```sh
make fmt     # форматирование (gofmt, goimports)
make lint    # golangci-lint
make check   # fmt + lint + go test ./... -race
```

CI ([.github/workflows/ci.yml](.github/workflows/ci.yml)) на каждый pull request
и push в `main` запускает линтер и тесты с `-race` и считает покрытие.

## Ограничения

- Хранилище только в памяти, данные теряются при перезапуске.
- Аутентификация отсутствует сознательно: `merchant_id` берётся из тела запроса, а платёж
  по id может получить любой клиент.
- Нет обработки платежей: платёж остаётся в `pending`.

