# Политика подписи (policy)

Статус: **реализовано** — проверяется при каждой подписи (`internal/policy`, подписанты `infra/ethereum`, `infra/tron`, `features/signing`). Не реализованы только лимиты за период и проверка аргументов (§7).

Политика — правила роли, которые решают, можно ли подписать транзакцию ключом этой роли. Она привязана к роли, а не к ключу: новый ключ после ротации и тысячи deposit-ключей получают одни и те же правила.

Цель — чтобы взлом сервиса не давал подписать что угодно: газовый кошелёк не может перевести токены, executor не может вызвать чужой контракт, депозитный кошелёк не может отправить газ на чужой адрес.

## 1. Операция

Подписант (`DecodeTx`) разбирает `raw_tx` и превращает её в одну операцию. Политика проверяет операцию, а не сырую транзакцию.

```
Operation = { op, from, to, contract, selector, amount, value }
```

| op | ETH / Arbitrum | TRON | Поля |
|---|---|---|---|
| `native` | `data` пустой | `TransferContract` | `to`, `amount` |
| `token` | `data` = `transfer(address,uint256)` (селектор `a9059cbb`, длина 68), `value = 0` | `TriggerSmartContract` с тем же селектором, `call_value = 0` | `contract` (токен), `to` (arg0), `amount` (arg1) |
| `contract_call` | любой другой `data` | любой другой `TriggerSmartContract` | `contract`, `selector`, `value` |
| запрещено всегда | создание контракта (`to = nil`) | все остальные типы контрактов: `AccountPermissionUpdateContract`, `FreezeBalanceV2Contract`, `VoteWitnessContract`, `TransferAssetContract`… | — |

`AccountPermissionUpdateContract` позволяет передать управление адресом другому ключу, поэтому неизвестные типы запрещены всегда, независимо от политики.

Проверки **до** политики, от неё не зависят:

- `from` = адрес ключа (ETH — по построению, TRON — `owner_address`);
- сеть и chainID совпадают с ролью и конфигом.

## 2. Формат

```json
{
  "version": 1,
  "rules": [
    {
      "op": "native | token | contract_call",
      "contracts": ["<address>"],
      "methods": ["transferScaled(address,uint256)"],
      "to": ["*", "internal", "internal:<purpose>", "<address>"],
      "max_amount": "1000000000000000000",
      "value": "zero | any"
    }
  ]
}
```

| Поле | `native` | `token` | `contract_call` | Смысл |
|---|---|---|---|---|
| `op` | обязательно | обязательно | обязательно | тип операции |
| `contracts` | нельзя | обязательно | обязательно | адреса токена / контракта |
| `methods` | нельзя | нельзя | обязательно | сигнатуры методов, при записи считаются селекторы |
| `to` | обязательно | обязательно | нельзя | получатель |
| `max_amount` | можно | можно | нельзя | максимум за одну транзакцию, в минимальных единицах (wei, sun, база токена), строкой |
| `value` | нельзя | нельзя | можно, по умолчанию `zero` | можно ли прикладывать нативную монету к вызову |

Значения `to`:

| Значение | Подходит |
|---|---|
| `"*"` | любой адрес |
| `"internal"` | адрес любого ключа этого же сервиса в этой же сети, статус `active` / `rotating` / `pending` |
| `"internal:<purpose>"` | то же, но только ключи ролей с этим `purpose`, например `internal:executor` |
| `"0x…"` / `"T…"` | конкретный адрес |

## 3. Как принимается решение

- **Запрещено по умолчанию.** `{}` или пустой `rules` — подписывать нельзя ничего.
- **Разрешено, если подошло хотя бы одно правило.** Порядок правил не важен.
- **Правило подходит, если совпали все его поля:**
  - `op` совпадает;
  - `contract` ∈ `contracts`;
  - `selector` ∈ селекторы `methods`;
  - `to` подходит хотя бы под одно значение из `to`;
  - `amount ≤ max_amount`, если задан;
  - при `value: zero` — `value = 0`.
- **Адреса сравниваются нормализованными:** ETH — без учёта регистра, TRON — base58 переводится в hex `41…`.
- **Отказ** — 403 `POLICY_VIOLATION`. В лог пишутся решение, номер сработавшего правила и операция (op, contract, selector, to, amount).

## 4. Проверка при записи

Политика проверяется при `create-role` и при обновлении, чтобы опечатка не расширила права молча:

- неизвестные поля — ошибка, `version` = `1`;
- обязательные и запрещённые поля — по таблице раздела 2;
- адреса валидны **для сети роли** (TRON-адрес в ETH-роли — ошибка);
- `methods` — каноническая сигнатура `name(type,type)` без пробелов и имён параметров;
- `max_amount` — целое неотрицательное число строкой;
- в базу пишется нормализованная форма: адреса приведены к одному виду, селекторы посчитаны.

## 5. Где проверяется

```
POST /api/v1/keys/{id}/sign-transaction
  1. signer.DecodeTx(raw_tx) → Operation                 400 INVALID_TRANSACTION
  2. key_storage.UseSigningKey(service, key_id, network, check, sign)
       a. ключ сервиса, статус, сеть                       404 / 409 / 400
       b. check: policy.Allow(operation, isInternal)       403 POLICY_VIOLATION
       c. расшифровка → подпись → затирание ключа
```

Политика проверяется **до расшифровки ключа**: транзакция, которую нельзя подписывать, не достаёт ключ из шифра.

```go
type Rule struct {
	Op        string   `json:"op"`
	Contracts []string `json:"contracts,omitempty"`
	Methods   []string `json:"methods,omitempty"`
	To        []string `json:"to,omitempty"`
	MaxAmount *string  `json:"max_amount,omitempty"`
	Value     string   `json:"value,omitempty"`
}

type Policy struct {
	Version int    `json:"version"`
	Rules   []Rule `json:"rules"`
}

// Allow returns index of matched rule. isInternal reports whether address is own key of purpose ("" - any purpose).
func (p *Policy) Allow(op Operation, isInternal func(address, purpose string) bool) (int, bool)
```

## 6. Примеры

### Газовый кошелёк atoken (ETH)

Пополняет только свои executor'ы и deposit-кошельки, не больше 0.05 ETH за раз.

```json
{"version":1,"rules":[
  {"op":"native","to":["internal:executor","internal:deposit"],"max_amount":"50000000000000000"}
]}
```

### Executor atoken

Только доставка через ExecutorManager.

```json
{"version":1,"rules":[
  {"op":"contract_call","contracts":["<ExecutorManager>"],"methods":["transferTokensWithNative(address,uint256,uint256)"]}
]}
```

### Owner ExecutorManager atoken (TRON)

Только вывод комиссии.

```json
{"version":1,"rules":[
  {"op":"contract_call","contracts":["<ExecutorManager>"],"methods":["withdrawTokens(address,uint256)"]}
]}
```

### Deposit-кошелёк atoken

Вывод на ExecutorManager, рефанд пользователю, возврат остатка газа.

```json
{"version":1,"rules":[
  {"op":"contract_call","contracts":["<A7A5>"],"methods":["transferScaled(address,uint256)"]},
  {"op":"token","contracts":["<A7A5>"],"to":["*"]},
  {"op":"native","to":["internal:gas_station"]}
]}
```

### Executor банкнот (TRON)

```json
{"version":1,"rules":[
  {"op":"contract_call","contracts":["<BanknoteContract>"],"methods":["commitPayment(address,bytes32)","executePayment((address,uint256,uint256,uint256,string,bytes))"]}
]}
```

### Payout-pool cedex

Выплаты пользователям в токене, остаток газа — в settling tank.

```json
{"version":1,"rules":[
  {"op":"token","contracts":["<USDT>"],"to":["*"]},
  {"op":"native","to":["internal:collector"]}
]}
```

### Settling tank cedex

Адреса депозитов RocketX каждый раз новые.

```json
{"version":1,"rules":[{"op":"native","to":["*"]}]}
```

## 7. Что не входит в `rules`

- **Лимиты за период и частота.** Нужно состояние (сумма подписей за сутки). Будет отдельный блок `"limits"` рядом с `rules`, проверяется после сработавшего правила, вместе с таблицей `sign_requests` (аудит).
- **Проверка аргументов `contract_call`** (например, что arg0 `transferScaled` — ExecutorManager). Сейчас ограничиваются контракт и метод. При необходимости — `"args": [{"index": 0, "in": ["…"]}]` в `version: 2`.

## 8. Как задать политику

- при создании роли: `POST /api/v1/admin/roles` с полем `policy` или `wallet-admin create-role ... -policy policy.json`;
- заменить: `PUT /api/v1/admin/roles/{id}/policy` или `wallet-admin set-policy -role <id> -policy policy.json` (в production — только CLI).

Роль без политики ничего не подписывает: перед переводом сервиса на роль ей нужно задать правила.

В базе политика хранится как `JSONB`, поэтому порядок ключей в ответе может отличаться от отправленного — на смысл это не влияет.
