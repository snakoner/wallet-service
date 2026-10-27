# Wallet Service: что уже сделано в API

Обновлено: 26.10.2026. Документ обновляется после каждого изменения ручек.

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
- **Статусы ключа**: `active`, `rotating`, `pending`, `disabled`, `revoked`. Подписывают `active`, `pending` (подготовка ротации) и `rotating` (дописать начатое, вывести остатки). Ротация — раздел [Ротация ключей](#ротация-ключей).
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

### `POST /api/v1/roles/{purpose}/deposit-key` — deposit-ключ по `external_ref`

Авторизация: `X-Service-Key`. Только для своей `deposit`-роли. Первый вызов создаёт ключ, повторные с тем же `external_ref` возвращают тот же ключ, поэтому ручку можно безопасно ретраить. Параллельные вызовы с одним `external_ref` тоже получат один ключ.

| Поле | Обязательно | |
|---|---|---|
| `network` | да | `ethereum`, `tron`, `arbitrum` |
| `external_ref` | да | id заказа, до 128 символов |
| `label` | нет | уточнение роли |
| `alias` | нет | применяется только при создании |

```bash
curl -s -X POST http://localhost:8080/api/v1/roles/deposit/deposit-key \
  -H "X-Service-Key: $SERVICE_KEY" \
  -H "Content-Type: application/json" \
  -d '{"network":"ethereum","external_ref":"order-1"}'
```

```json
{"success":true,"data":{"id":"…","service":"exchange","purpose":"deposit","label":"","network":"ethereum","kind":"deposit","address":"0x…","external_ref":"order-1","status":"active","created_at":"…"}}
```

Ключ возвращается в текущем статусе: если админ его отозвал, придёт `revoked`, нового ключа не создаётся.

Ошибки: `ROLE_NOT_FOUND` (404), `INVALID_PARAMS` (400, роль не deposit), `VALIDATION_FAILED` (400), `UNAUTHORIZED` (401).

### `POST /api/v1/keys/{id}/sign-transaction` — подписать транзакцию

Авторизация: `X-Service-Key`.

Транзакцию собирает и отправляет сервис, wallet только проверяет и подписывает.

```json
{"network": "ethereum", "raw_tx": "0x…", "lease_id": "…"}
```

`lease_id` обязателен для ключей pool-роли; для single/deposit — только если ключ сейчас арендован.

| Сеть | `raw_tx` | `signed_tx` в ответе | `tx_hash` |
|---|---|---|---|
| `ethereum`, `arbitrum` | `types.Transaction.MarshalBinary()` неподписанной tx (legacy или EIP-1559) | для `eth_sendRawTransaction` | `0x…` |
| `tron` | `raw_data_hex` из ответа ноды (`createtransaction` / `triggersmartcontract`) | protobuf `Transaction` для `/wallet/broadcasthex` | txID без `0x` |

Проверки перед подписью (все — до расшифровки ключа):
- tx разбирается в операцию `native` / `token` / `contract_call`:
  - ETH/Arbitrum: chainID EIP-1559 tx совпадает с `ETH_CHAIN_ID` / `ARBITRUM_CHAIN_ID`, создание контракта запрещено;
  - TRON: ровно один контракт, `expiration` не прошёл, только `TransferContract` и `TriggerSmartContract` (остальные типы, в т.ч. `AccountPermissionUpdateContract`, запрещены), `call_token_value` (TRC10) запрещён;
- ключ принадлежит сервису (иначе 404), сеть ключа совпадает с `network`, статус `active`, `pending` или `rotating`;
- аренда: pool-ключ — только с живой арендой (`lease_id`); арендованный ключ любой роли — только с её `lease_id`;
- TRON: `owner_address` tx = адрес ключа;
- операция разрешена политикой роли ([POLICY.md](POLICY.md)).

```json
{"success":true,"data":{"signed_tx":"0x…","tx_hash":"0x…"}}
```

Ошибки: `INVALID_TRANSACTION` (400, в т.ч. чужой `owner_address`), `UNSUPPORTED_NETWORK` (400), `INVALID_PARAMS` (400, сеть ключа другая), `VALIDATION_FAILED` (400), `POLICY_VIOLATION` (403), `KEY_NOT_FOUND` (404), `KEY_NOT_ACTIVE` (409), `LEASE_REQUIRED` (409, pool-ключ без аренды), `LEASE_EXPIRED` (409, аренда истекла или чужая), `KEY_LEASED` (409, ключ арендован другим экземпляром), `UNAUTHORIZED` (401), `INTERNAL_ERROR` (500, в т.ч. не удалось записать аудит).

Каждый запрос, дошедший до подписи (после авторизации и разбора hex), пишется в таблицу `signing_audit`: `service_client_id`, `key_id` как в запросе, `network`, `raw_tx`, `tx_hash` для подписанных, `error_code` для отклонённых. Если запись не удалась, подпись не возвращается (500); отказ при недоступном аудите возвращается со своим кодом, ошибка аудита пишется в лог.

### Аренда ключей (lease)

Ключ арендуется одним экземпляром сервиса на время «выбрал → собрал tx → подписал → отправил». Пока аренда жива, подписать ключом можно только с её `lease_id`. Упавший экземпляр держит ключ не дольше TTL.

#### `POST /api/v1/roles/{purpose}/lease` — арендовать свободный ключ

```json
{"network": "ethereum", "label": "", "owner": "exchange-7d9f-abc", "ttl_seconds": 60, "exclude": ["<key_id>"]}
```

| Поле | |
|---|---|
| `network` | обязательно |
| `label` | роль, по умолчанию `''` |
| `owner` | кто держит, для отладки (имя пода), до 128 символов |
| `ttl_seconds` | по умолчанию 60, максимум 600 |
| `exclude` | ключи, которые не выдавать (например, без газа) |

Выдаётся `active`-ключ без живой аренды, первым — дольше всех не использованный. Для `single` и `pool`; deposit — `INVALID_PARAMS`.

```json
{"success":true,"data":{"lease_id":"…","expires_at":"…","key":{"id":"…","address":"0x…","status":"active","…":"…"}}}
```

Ошибки: `NO_FREE_KEY` (409, с заголовком `Retry-After`), `ROLE_NOT_FOUND` (404), `VALIDATION_FAILED` (400), `INVALID_PARAMS` (400, deposit-роль).

#### `POST /api/v1/leases/{lease_id}/extend` — продлить

Тело необязательное: `{"ttl_seconds": 60}`. Ответ: `{"expires_at": "…"}`. Ошибка: `LEASE_EXPIRED` (409) — аренда истекла, освобождена или чужая.

#### `DELETE /api/v1/leases/{lease_id}` — освободить

Ошибка: `LEASE_EXPIRED` (409) — аренда уже освобождена или чужая.

Порядок работы сервиса: lease → проверить газ (мало — release и lease с `exclude`) → nonce с тегом `pending` → sign с `lease_id` → отправить → release. Ждать подтверждения под арендой не нужно.

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

### Жизненный цикл ключа

`POST /api/v1/admin/keys/{id}/rotate | activate | revoke | disable | enable` — тело не нужно, ответ — ключ (для `rotate` — новый замещающий ключ).

| Действие | Из статуса | Что делает | Ошибки |
|---|---|---|---|
| `rotate` | `active` | создаёт замещающий ключ в `pending` в той же роли, у старого `replaced_by` | `INVALID_KEY_STATUS` (409), `ROTATION_IN_PROGRESS` (409), deposit-ключ — `INVALID_PARAMS` (400) |
| `activate` | `pending` | замещающий → `active`, замещаемый → `rotating` (`rotated_at`), атомарно | `INVALID_KEY_STATUS`, `ACTIVE_KEY_EXISTS` (409) |
| `revoke` | любой, кроме `revoked` | → `revoked` (`revoked_at`), подписывать больше нельзя. Отзыв `pending` отменяет ротацию: у старого ключа снимается `replaced_by` | `INVALID_KEY_STATUS` |
| `disable` / `enable` | `active` ↔ `disabled` | временно запретить подпись | `INVALID_KEY_STATUS`, для single при другом активном — `ACTIVE_KEY_EXISTS` |

`replaced_by` есть и в ответах сервисных ручек `/roles/{purpose}/key(s)`.

### `POST /api/v1/admin/keys/{id}/release-lease` — снять аренду

Освобождает ключ, который держит зависший экземпляр сервиса. Ошибка: `KEY_NOT_FOUND` (404).

### `POST /api/v1/admin/roles/{id}/keys/import` — импортировать существующий ключ

```json
{"private_key": "0x…", "alias": "migrated from env", "external_ref": "order-1"}
```

`private_key` — hex, с `0x` или без; `external_ref` — только для deposit. Ключ создаётся `active`. Ошибки как у создания ключа, плюс `INVALID_PARAMS` (400) для неверного ключа и `KEY_EXISTS` (409), если адрес уже есть. Приватный ключ передаётся в теле: только по TLS и без логирования тел запросов.

### Ключи сервисов (`X-Service-Key`)

| Метод | Путь | Тело | Ответ |
|---|---|---|---|
| POST | `/api/v1/admin/service-clients` | `{"service": "exchange"}` | `service_key` (показывается один раз) и `client` |
| POST | `/api/v1/admin/service-clients/rotate` | `{"service": "exchange", "grace_seconds": 86400}` | новый `service_key`, `client`, `old_client` с `expires_at` |
| POST | `/api/v1/admin/service-clients/{id}/revoke` | — | |
| GET | `/api/v1/admin/service-clients?service=` | — | список без хэшей |

`grace_seconds` по умолчанию 86400 (24 часа); `0` — старый ключ отключается сразу. `client.state`: `primary`, `expiring`, `expired`, `revoked`.

Ошибки: `SERVICE_CLIENT_EXISTS` (409, у сервиса уже есть основной ключ — нужна ротация), `SERVICE_CLIENT_NOT_FOUND` (404), `VALIDATION_FAILED` (400).

## Ротация ключей

Новый ключ — новый адрес, поэтому ротация идёт по шагам, без простоя сервисов:

1. `rotate-key -id <A>` — создан `B` (`pending`). Сервисы по-прежнему получают `A` из `/roles/{purpose}/key`.
2. Подготовка `B`: газ, роли в контрактах (`transferOwnership` от `A`, `acceptOwnership` от `B` — по политике роли).
   **Правило ротации:** ключ с `replaced_by` может отправить `native` и `token` на адрес своего замещающего ключа, даже если политика роли это запрещает. Поэтому `B` можно пополнить прямо с `A`. Другие адреса для `A` по-прежнему закрыты политикой.
3. `activate-key -id <B>` — `B` становится `active`, `A` — `rotating`. Сервисы при следующем запросе ключа получают `B`; `A` дописывает начатое.
4. Вывести остатки `A` на `B` (то же правило ротации).
5. `revoke-key -id <A>` — `A` больше не подписывает.

Отмена до активации: `revoke-key -id <B>`.

Для сервисов: не хранить `key_id` в конфиге, получать текущий ключ через `/roles/{purpose}/key` (можно кэшировать на минуту); на `409 KEY_NOT_ACTIVE` получить ключ заново и пересобрать транзакцию.

- `single` (gas station, owner): полный цикл.
- `pool` (executor'ы): тот же цикл для каждого ключа пула, остальные ключи работают.
- `deposit`: не ротируются (ключ живёт один заказ).

Пример для газового кошелька:

```bash
wallet-admin rotate-key -id <A>                        # B pending
gas-station-send -to <B> -amount 0.05 -send            # пополнить B с A
wallet-admin activate-key -id <B>
gas-station-send -to <B> -amount <остаток> -send        # после переходного периода
wallet-admin revoke-key -id <A>
```

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
wallet-admin rotate-key | activate-key | revoke-key | disable-key | enable-key -id <key_id>
wallet-admin release-lease -id <key_id>
```

`import-key` — перенос существующих ключей из env сервисов. В production admin API нет, там всё делается через CLI.

## Инструменты

### `cmd/gas-station-send` — отправка ETH с газового кошелька

Делает то же, что будет делать сервис: берёт текущий ключ газового кошелька (`GET /roles/gas_station/key`), собирает неподписанную EIP-1559 tx по данным RPC (chainId, pending nonce, комиссии), подписывает через `sign-transaction` и, с `-send`, отправляет в сеть.

```bash
WALLET_SERVICE_KEY=... ETH_RPC_URL=https://... go run ./cmd/gas-station-send -to 0x... -amount 0.001 [-purpose gas_station] [-label atoken] [-send]
```

Без `-send` только печатает `tx_hash` и подписанную tx. Получатель должен быть разрешён политикой газового кошелька, иначе `403 POLICY_VIOLATION`. Справка — запуск без флагов.

### `cmd/tron-gas-station-send` — отправка TRX с газового кошелька

То же для Tron: берёт текущий ключ (`GET /roles/gas_station/key?network=tron`), собирает перевод TRX через ноду (`/wallet/createtransaction`), подписывает `raw_data_hex` через `sign-transaction`, сверяет `tx_hash` с `txID` ноды и, с `-send`, отправляет через `/wallet/broadcasthex`.

```bash
WALLET_SERVICE_KEY=... TRON_NODE_URL=https://nile.trongrid.io go run ./cmd/tron-gas-station-send -to T... -amount 1 [-purpose gas_station] [-label atoken] [-node-key <TronGrid key>] [-send]
```

Сумма в TRX, до 6 знаков после запятой. Адрес газового кошелька должен существовать в сети (иметь баланс), иначе нода не соберёт транзакцию. Перевод на ещё не активированный адрес дополнительно сжигает около 1 TRX за активацию. Транзакция действительна около минуты после сборки: без `-send` её нужно отправить сразу.

## Тесты

```bash
make test               # unit-тесты
make test-integration   # интеграционные: поднимает docker-compose.test.yml, гоняет тесты, удаляет окружение
```

`docker-compose.test.yml` (отдельный compose-проект `wallet-service-test`, порты 15433 / 18090 / 18545): Postgres, миграции up → down → up (проверка down-миграций), приложение и anvil (локальный Ethereum, chain id 31337). Тесты под тегом `integration`, обычный `go test ./...` их не запускает.

- `internal/features/*/repository_integration_test.go` — SQL против настоящего Postgres: уникальности, гонка 20 `CreateKey` в single-роль, `IsServiceAddress`, фильтры `ListKeys`, ротация и параллельные ротации service key.
- `test/integration` — API end-to-end: авторизация, роли, подпись ETH (legacy и EIP-1559) с отправкой в anvil и проверкой receipt, отказы (политика, chain id, статус ключа, чужой сервис), записи `signing_audit` для подписи и отказов, подпись TRON с восстановлением адреса из подписи, deposit-ключи (повтор и гонка по одному `external_ref`, вывод на газовый кошелёк), admin API.

## Ещё не сделано

| Что | Статус |
|---|---|
| Лимиты за период и проверка аргументов `contract_call` в политике | после аудита, см. [POLICY.md](POLICY.md) §7 |
| Несколько KEK и перешифровка (`rewrap-keys`) | следующий шаг |
| Go-клиент `pkg/client` для сервисов | шаг 6 |
| Перевод atoken-exchange, cedex-retail, banknote на wallet-service | шаг 7 |
| SSO-авторизация admin API | не начато |
| Аудит действий админа | не начато, аудит подписей сделан |
| `SignMessage` | не нужно (подписи банкнот остаются в banknote) |
