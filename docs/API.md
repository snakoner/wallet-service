# Wallet Service: что уже сделано в API

Обновлено: 23.10.2026. Документ обновляется после каждого изменения ручек.

## Общее

- Base URL: `http://<host>:8080`, все ручки под `/api/v1`.
- Swagger: `/swagger/index.html` (кроме production).
- Health: `GET /healthz` → `{"status":"ok"}`.
- Сети: `ethereum`, `tron`, `arbitrum`.

### Формат ответа

```json
{"success": true, "data": { ... }}
{"success": false, "error": {"code": "KEY_NOT_FOUND", "message": "key not found"}}
{"success": false, "error": {"code": "VALIDATION_FAILED", "message": "validation failed", "fields": {"network": "unsupported network"}}}
```

### Авторизация сервисов

- Заголовок `X-Service-Key`. Ключ выдаёт `wallet-admin create-service-client -service <name>`, показывается один раз, в базе хранится только хэш.
- У сервиса один основной ключ: повторный `create-service-client` → `SERVICE_CLIENT_EXISTS`.
- Смена ключа без простоя: `rotate-service-client -service <name> [-grace 24h]` выдаёт новый ключ, старый принимается ещё `grace` (по умолчанию 24 часа) — за это время сервис выкатывается с новым. `-grace 0` отключает старый сразу.
- При утечке: `revoke-service-client -id <id>` — ключ перестаёт приниматься сразу.
- Имя сервиса берётся только из ключа, не из тела запроса. Сервис видит только свои роли и ключи.

### Модель

- **Роль** — что за кошелёк: `service` + `purpose` + `label` + `network`. `purpose` — свободная строка (`gas_station`, `executor`, `owner`, `deposit`…), `label` уточняет (`atoken`, `usdt_high_risk`).
- **kind** роли:
  - `single` — один активный ключ (газовый кошелёк, owner);
  - `pool` — несколько взаимозаменяемых ключей (executor'ы);
  - `deposit` — ключ на каждый `external_ref` (кошелёк под заказ).
- **policy** роли — правила подписи, проверяются при каждой подписи. Формат — [POLICY.md](POLICY.md). Без правил (`{}`, по умолчанию) роль **ничего не подписывает**.
- **Статусы ключа**: `active`, `rotating`, `pending`, `disabled`, `revoked`. Подписывают только `active` и `rotating`.
- Приватные ключи хранятся зашифрованными (envelope: DEK на ключ + KEK из `KEY_STORAGE_KEK`) и никогда не отдаются наружу.

## Ручки для сервисов

### `GET /api/v1/roles/{purpose}/key` — текущий ключ своей single-роли

Авторизация: `X-Service-Key`.

| Параметр | Где | Обязательный | |
|---|---|---|---|
| `purpose` | path | да | `gas_station` |
| `network` | query | да | `ethereum` / `tron` / `arbitrum` |
| `label` | query | нет | `atoken`, по умолчанию `''` |

```bash
curl -s "http://localhost:8080/api/v1/roles/gas_station/key?label=atoken&network=tron" \
  -H "X-Service-Key: $SERVICE_KEY"
```

```json
{"success":true,"data":{"id":"…","service":"exchange","purpose":"gas_station","label":"atoken","network":"tron","kind":"single","address":"T…","status":"active","created_at":"…"}}
```

Ошибки: `ROLE_NOT_FOUND` (404), `KEY_NOT_FOUND` (404, у роли нет активного ключа), `INVALID_PARAMS` (400, роль не single), `VALIDATION_FAILED` (400), `UNAUTHORIZED` (401).

`key_id` меняется после ротации — не сохраняйте его в конфиге, получайте этой ручкой.

### `GET /api/v1/roles/{purpose}/keys` — все ключи своей роли

Авторизация: `X-Service-Key`. Для `single` и `pool` ролей, например список executor'ов. Параметры те же, что у `/key`: `purpose` (path), `network` (обязателен), `label`.

```bash
curl -s "http://localhost:8080/api/v1/roles/executor/keys?network=ethereum" \
  -H "X-Service-Key: $SERVICE_KEY"
```

```json
{"success":true,"data":[
  {"id":"…","service":"exchange","purpose":"executor","label":"","network":"ethereum","kind":"pool","address":"0x…","status":"active","created_at":"…"},
  {"id":"…","service":"exchange","purpose":"executor","label":"","network":"ethereum","kind":"pool","address":"0x…","status":"rotating","created_at":"…"}
]}
```

Возвращаются ключи во всех статусах. Новые транзакции собирайте с `active`; `rotating` только дописывают уже начатое.

Ошибки: `ROLE_NOT_FOUND` (404), `INVALID_PARAMS` (400, deposit-роль: её ключи по списку не отдаются), `VALIDATION_FAILED` (400), `UNAUTHORIZED` (401).

### `POST /api/v1/keys/{id}/sign-transaction` — подписать транзакцию

Авторизация: `X-Service-Key`. **Сейчас в роутере `serviceAuth` на группе `/keys` закомментирован, поэтому ручка всегда отвечает 401.**

Транзакцию собирает и отправляет сервис, wallet только проверяет и подписывает.

```json
{"network": "ethereum", "raw_tx": "0x…"}
```

| Сеть | `raw_tx` | `signed_tx` в ответе | `tx_hash` |
|---|---|---|---|
| `ethereum`, `arbitrum` | `types.Transaction.MarshalBinary()` неподписанной tx (legacy или EIP-1559) | для `eth_sendRawTransaction` | `0x…` |
| `tron` | `raw_data_hex` из ответа ноды (`createtransaction` / `triggersmartcontract`) | protobuf `Transaction` для `/wallet/broadcasthex` | txID без `0x` |

Проверки перед подписью (все — до расшифровки ключа):
- tx разбирается в операцию `native` / `token` / `contract_call`:
  - ETH/Arbitrum: chainID EIP-1559 tx совпадает с `ETH_CHAIN_ID` / `ARBITRUM_CHAIN_ID`, создание контракта запрещено;
  - TRON: ровно один контракт, `expiration` не прошёл, только `TransferContract` и `TriggerSmartContract` (остальные типы, в т.ч. `AccountPermissionUpdateContract`, запрещены), `call_token_value` (TRC10) запрещён;
- ключ принадлежит сервису (иначе 404), сеть ключа совпадает с `network`, статус `active` или `rotating`;
- TRON: `owner_address` tx = адрес ключа;
- операция разрешена политикой роли ([POLICY.md](POLICY.md)).

```json
{"success":true,"data":{"signed_tx":"0x…","tx_hash":"0x…"}}
```

Ошибки: `INVALID_TRANSACTION` (400, в т.ч. чужой `owner_address`), `UNSUPPORTED_NETWORK` (400), `INVALID_PARAMS` (400, сеть ключа другая), `VALIDATION_FAILED` (400), `POLICY_VIOLATION` (403), `KEY_NOT_FOUND` (404), `KEY_NOT_ACTIVE` (409), `UNAUTHORIZED` (401).

## Admin API

**Только вне production** (в production маршруты не регистрируются). **Авторизации пока нет** — будет SSO.

### `POST /api/v1/admin/roles` — создать роль

```json
{"service":"exchange","purpose":"gas_station","label":"atoken","network":"tron","kind":"single","policy":{"rules":[{"op":"native","to":"internal"}]}}
```

`service`, `purpose`, `network`, `kind` обязательны; `policy` необязательна, по умолчанию `{"version":1,"rules":[]}` (подписывать нельзя ничего). Политика проверяется и нормализуется при записи. Ответ — роль с `id`. Ошибки: `ROLE_EXISTS` (409), `VALIDATION_FAILED` / `INVALID_PARAMS` (400, в т.ч. невалидная политика).

### `PUT /api/v1/admin/roles/{id}/policy` — заменить политику роли

```json
{"policy": {"version":1,"rules":[{"op":"native","to":["internal:executor"],"max_amount":"50000000000000000"}]}}
```

Применяется сразу ко всем ключам роли. Ответ — роль с нормализованной политикой. Ошибки: `INVALID_PARAMS` (400, текст ошибки говорит, какое правило и поле неверны), `VALIDATION_FAILED` (400, нет `policy`), `ROLE_NOT_FOUND` (404).

### `GET /api/v1/admin/roles?service=exchange` — список ролей

Без `service` — роли всех сервисов.

### `POST /api/v1/admin/roles/{id}/keys` — сгенерировать ключ роли

Тело необязательное: `{"alias":"…","external_ref":"order-1"}`.

- `single`: второй активный ключ → `ACTIVE_KEY_EXISTS` (409);
- `deposit`: `external_ref` обязателен, повтор → `KEY_EXISTS` (409);
- `single`/`pool` с `external_ref` → `INVALID_PARAMS` (400);
- нет роли → `ROLE_NOT_FOUND` (404).

### `GET /api/v1/admin/roles/{id}/keys` — ключи роли

Адреса и статусы, без шифротекстов.

### `GET /api/v1/admin/keys` — ключи всех сервисов с фильтрами

Все параметры необязательные. Фильтр применяется, только если параметр передан: `?label=` — ключи с пустым label. Ключи от новых к старым, без шифротекстов.

| Параметр | |
|---|---|
| `service`, `purpose`, `label` | точное совпадение |
| `network` | `ethereum` / `tron` / `arbitrum` |
| `status` | `active` / `rotating` / `pending` / `disabled` / `revoked` |
| `external_ref` | id заказа deposit-ключа |
| `limit` | по умолчанию 100, максимум 1000 |

```bash
curl -s "http://localhost:8080/api/v1/admin/keys?service=exchange&purpose=executor&network=ethereum&status=active"
```

Ошибки: `VALIDATION_FAILED` (400, неизвестные network/status, limit вне 1..1000).

## Admin CLI (`wallet-admin`)

Полная справка: `wallet-admin help`.

```bash
wallet-admin create-service-client -service exchange
wallet-admin rotate-service-client -service exchange [-grace 24h]
wallet-admin revoke-service-client -id <service_client_id>
wallet-admin list-service-clients [-service exchange]
wallet-admin create-role -service exchange -purpose gas_station -label atoken -network tron -kind single [-policy policy.json]
wallet-admin list-roles [-service exchange]
wallet-admin list-keys [-service exchange] [-purpose executor] [-label ""] [-network ethereum] [-status active] [-external-ref order-1] [-limit 100]
wallet-admin set-policy -role <role_id> -policy policy.json
wallet-admin create-key -role <role_id> [-alias name] [-external-ref order-1]
echo "<private key hex>" | wallet-admin import-key -role <role_id> [-alias name] [-external-ref order-1]
```

`import-key` — перенос существующих ключей из env сервисов. По HTTP импорта нет специально. В production admin API нет, политику там меняют только через `set-policy`.

## Инструменты

### `cmd/gas-station-send` — отправка ETH с газового кошелька

Делает то же, что будет делать сервис: берёт текущий ключ газового кошелька (`GET /roles/gas_station/key`), собирает неподписанную EIP-1559 tx по данным RPC (chainId, pending nonce, комиссии), подписывает через `sign-transaction` и, с `-send`, отправляет в сеть.

```bash
WALLET_SERVICE_KEY=... ETH_RPC_URL=https://... go run ./cmd/gas-station-send -to 0x... -amount 0.001 [-purpose gas_station] [-label atoken] [-send]
```

Без `-send` только печатает `tx_hash` и подписанную tx. Получатель должен быть разрешён политикой газового кошелька, иначе `403 POLICY_VIOLATION`. Справка — запуск без флагов.

## Ещё не сделано

| Что | Статус |
|---|---|
| Лимиты за период и проверка аргументов `contract_call` в политике | после аудита, см. [POLICY.md](POLICY.md) §7 |
| Lease для pool-ролей (executor'ы) | шаг 3 |
| Создание deposit-ключей сервисом через API, поиск по `external_ref` | шаг 4 |
| Ротация ключей (`rotate` / `activate` / `revoke`), несколько KEK | шаг 5 |
| Go-клиент `pkg/client` для сервисов | шаг 6 |
| Перевод atoken-exchange, cedex-retail, banknote на wallet-service | шаг 7 |
| SSO-авторизация admin API | не начато |
| Интеграционные тесты (Postgres + anvil) | не начато |
| `SignMessage` | не нужно (подписи банкнот остаются в banknote) |
