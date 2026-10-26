# Wallet Service: архитектура

## Назначение

Wallet Service хранит приватные ключи кошельков (Ethereum, Arbitrum, Tron) и подписывает ими транзакции по запросу других сервисов (exchange, banknote, cedex-retail).

Принципы:

- приватный ключ не покидает Wallet Service: его нет ни в ответах API, ни в логах, ни в других сервисах;
- транзакцию собирает и отправляет в сеть вызывающий сервис (nonce, газ, ретраи, статус остаются у него); Wallet Service проверяет транзакцию и подписывает;
- каждая подпись проверяется по правилам кошелька: что и кому этот кошелёк может отправлять;
- ключи можно ротировать без изменений в сервисах.

## Участники

```
сервис (exchange, cedex, ...) ── ключ сервиса ──▶ Wallet Service ──▶ БД (зашифрованные ключи)
админ ── CLI / admin API ───────────────────────▶ Wallet Service
```

- **Сервис** — получает адрес своего кошелька, собирает неподписанную транзакцию, отдаёт её на подпись, отправляет подписанную в сеть.
- **Админ** — заводит сервисы, роли кошельков и их правила, создаёт и ротирует ключи. К приватным ключам доступа не имеет.

## Основные понятия

### Сервис

Внешний потребитель (exchange, banknote, cedex-retail). Аутентифицируется своим ключом (`X-Service-Key`). Видит только свои кошельки.

### Роль

Роль описывает назначение кошелька в сервисе. Сервис работает с ролью, а не с конкретным адресом: адрес может смениться при ротации, роль остаётся.

| Поле | Обязательное | Описание |
|---|---|---|
| `service` | да | сервис-владелец: `exchange`, `banknote`, `cedex-retail` |
| `purpose` | да | назначение, свободная строка: `gas_station`, `executor`, `deposit`, `owner`, `payout`, `collector`, ... |
| `label` | нет | уточнение назначения, когда у сервиса несколько кошельков одного назначения: токен (`atoken`, `usdt_low_risk`, `usdt_high_risk`), продукт; пустая строка — единственная роль этого назначения |
| `network` | да | `ethereum`, `tron`, `arbitrum` |
| `kind` | да | тип роли: `single`, `pool`, `deposit` (ниже) |
| `policy` | нет | правила подписи для всех ключей роли; без правил подписывать нельзя ничего |

`service` + `purpose` + `label` + `network` уникальны: это и есть адрес роли, по которому сервис получает свой ключ.

`purpose` не перечисление: новые назначения не требуют изменений Wallet Service. Что кошельку разрешено, определяет политика, а не название.

#### Тип роли (`kind`)

Тип определяет, сколько ключей у роли, кто их создаёт и как сервис их получает.

| | `single` | `pool` | `deposit` |
|---|---|---|---|
| Активных ключей | ровно один | сколько угодно | сколько угодно, по одному на `external_ref` |
| Кто создаёт ключи | админ | админ | админ (в дальнейшем — сервис по `external_ref`) |
| Как сервис получает ключ | текущий ключ роли | список ключей роли | по `external_ref` (в дальнейшем) |
| `external_ref` | запрещён | запрещён | обязателен, уникален в роли |
| Ротация | да | да, по каждому ключу | нет |
| Примеры | газовый кошелёк, owner контракта, payout, collector | executor'ы, кошельки выплат | депозитный адрес заказа |

**`single`** — кошелёк, который у сервиса один: газовый кошелёк, владелец контракта. Второй активный ключ создать нельзя; заменить ключ можно только ротацией. Сервис всегда получает «текущий ключ роли», поэтому смена адреса для него прозрачна.

**`pool`** — несколько взаимозаменяемых кошельков для параллельной работы: executor'ы, которые отправляют транзакции одновременно, каждый со своим nonce. Сервис получает список ключей и сам выбирает свободный (в дальнейшем — эксклюзивная выдача ключа одному экземпляру сервиса). Ротация — по одному ключу, остальные в это время работают.

**`deposit`** — кошелёк на один заказ: пользователь отправляет на него средства, затем сервис выводит их. Ключ привязан к `external_ref` (id заказа), повторное создание с тем же `external_ref` запрещено. Ключей много и они короткоживущие, поэтому не ротируются; список по роли не отдаётся.

#### Примеры ролей

| Сервис | Кошелёк | purpose / label | kind | Политика по смыслу |
|---|---|---|---|---|
| exchange | газовый кошелёк | `gas_station` | single | ETH только на свои депозитные адреса |
| exchange | executor'ы | `executor` | pool | только `transferTokensWithNative` у ExecutorManager |
| exchange | депозитные адреса | `deposit` | deposit | вывод A7A5 на ExecutorManager, рефанд пользователю |
| exchange | владелец ExecutorManager (Tron) | `owner` | single | только `withdrawTokens` у ExecutorManager |
| banknote | executor'ы | `executor` | pool | только `commitPayment`, `executePayment` у контракта банкнот |
| cedex-retail | газовые кошельки агрегации | `gas_station` / `atoken`, `usdt_low_risk`, `usdt_high_risk` | single | TRX/ETH только на свои кошельки |
| cedex-retail | payout | `payout` / `usdt` | single | токен только на свои executor'ы |
| cedex-retail | пул выплат | `executor` / `payout_usdt` | pool | токен пользователям, газ в collector |
| cedex-retail | settling tank | `collector` | single | нативная монета на любой адрес (бридж) |

### Ключ

Приватный ключ и адрес, принадлежащий роли. Статусы:

| Статус | Подписывает | Смысл |
|---|---|---|
| `active` | да | текущий ключ роли |
| `pending` | да | новый ключ во время ротации, ещё не основной |
| `rotating` | да | старый ключ после ротации: дописывает начатое и выводит остатки |
| `disabled` | нет | временно отключён, можно включить |
| `revoked` | нет | отозван навсегда |

Ключи не удаляются: отозванные остаются для истории.

## Хранение ключей

Envelope encryption на AES-256-GCM:

- для каждого ключа генерируется свой ключ шифрования данных (DEK), им шифруется приватный ключ;
- DEK шифруется мастер-ключом (KEK); в БД лежат только оба шифротекста и версия KEK;
- KEK задаётся конфигурацией сервиса (в дальнейшем — KMS/HSM).

Приватный ключ расшифровывается только на время одной подписи и сразу затирается в памяти.

## Подпись транзакции

Запрос сервиса: `key_id`, сеть, неподписанная транзакция.

1. **Разбор.** Транзакция приводится к операции: перевод нативной монеты, перевод токена (`transfer`) или вызов контракта; определяются получатель, контракт, метод, сумма.
   - Ethereum / Arbitrum: legacy и EIP-1559; chain id должен совпадать с сетью сервиса; создание контрактов запрещено.
   - Tron: подписывается именно `raw_data`; один контракт в транзакции, только перевод TRX и вызов смарт-контракта (остальные типы, в том числе изменение прав аккаунта, запрещены), срок действия не истёк.
2. **Ключ.** Принадлежит сервису, сеть совпадает, статус позволяет подписывать. Ключ чужого сервиса неотличим от несуществующего.
3. **Проверки до расшифровки ключа:**
   - Tron: отправитель в транзакции — адрес ключа;
   - правило ротации (см. ниже);
   - политика роли.
4. **Подпись** и затирание ключа.
5. **Ответ:** подписанная транзакция в формате для отправки в сеть и её хэш.

Подпись детерминирована: повтор запроса с той же транзакцией даёт ту же подпись, поэтому повторы безопасны.

## Политика

Список правил роли. Подпись разрешена, если операция подходит хотя бы под одно правило; правил нет — запрещено всё.

Правило задаёт:

- тип операции: `native`, `token`, `contract_call`;
- для токена и вызова контракта — разрешённые контракты, для вызова — разрешённые методы и можно ли прикладывать нативную монету;
- получателей: любой, конкретные адреса, `internal` — любой кошелёк этого же сервиса в этой сети, `internal:<назначение>` — кошельки сервиса с указанным назначением;
- максимальную сумму одной транзакции.

Пример — газовый кошелёк пополняет только свои депозитные адреса, не больше 0.05 ETH за раз:

```json
{
  "version": 1,
  "rules": [
    {
      "op": "native",
      "to": [
        "internal:deposit"
      ],
      "max_amount": "50000000000000000"
    }
  ]
}
```

Политика проверяется при записи: неизвестные поля, адреса не той сети, неканонические сигнатуры методов отклоняются.

### Поля правила

| Поле | `native` | `token` | `contract_call` | Описание |
|---|---|---|---|---|
| `op` | обязательно | обязательно | обязательно | тип операции |
| `contracts` | — | обязательно | обязательно | адреса токенов / контрактов |
| `methods` | — | — | обязательно | канонические сигнатуры: `transfer(address,uint256)` |
| `to` | обязательно | обязательно | — | получатели: `*`, адрес, `internal`, `internal:<назначение>` |
| `max_amount` | можно | можно | — | максимум за одну транзакцию в минимальных единицах (wei, sun, единицы токена), строкой |
| `value` | — | — | можно | `zero` (по умолчанию) — нельзя прикладывать нативную монету, `any` — можно |

`token` — это только стандартный `transfer(address,uint256)` без нативной монеты; любой другой вызов токена — `contract_call`.

### Примеры

Executor: только доставка токенов через ExecutorManager.

```json
{
  "version": 1,
  "rules": [
    {
      "op": "contract_call",
      "contracts": ["0x3Ef3D8bA38EBe18DB133cEc108f4D14CE00Dd9Ae"],
      "methods": ["transferTokensWithNative(address,uint256,uint256)"]
    }
  ]
}
```

Депозитный адрес exchange: вывод A7A5 на ExecutorManager через `transferScaled` и рефанд пользователю обычным `transfer`.

```json
{
  "version": 1,
  "rules": [
    {
      "op": "contract_call",
      "contracts": ["0x33c264dc5e849172C5C87E598B17F8683d1D5b5C"],
      "methods": ["transferScaled(address,uint256)"]
    },
    {
      "op": "token",
      "contracts": ["0x33c264dc5e849172C5C87E598B17F8683d1D5b5C"],
      "to": ["*"]
    }
  ]
}
```

Владелец ExecutorManager (Tron): только вывод комиссии.

```json
{
  "version": 1,
  "rules": [
    {
      "op": "contract_call",
      "contracts": ["<адрес ExecutorManager в Tron>"],
      "methods": ["withdrawTokens(address,uint256)"]
    }
  ]
}
```

Пул выплат cedex: USDT пользователям на любой адрес, не больше 10 000 USDT за транзакцию; остаток газа — только в settling tank.

```json
{
  "version": 1,
  "rules": [
    {
      "op": "token",
      "contracts": ["<адрес USDT>"],
      "to": ["*"],
      "max_amount": "10000000000"
    },
    {
      "op": "native",
      "to": ["internal:collector"]
    }
  ]
}
```

Payout cedex: раздаёт USDT только своим кошелькам пула и выводит в казну на фиксированный адрес.

```json
{
  "version": 1,
  "rules": [
    {
      "op": "token",
      "contracts": ["<адрес USDT>"],
      "to": ["internal:executor", "<адрес казны>"]
    }
  ]
}
```

Settling tank cedex: нативная монета на любой адрес (депозитные адреса бриджа каждый раз новые).

```json
{
  "version": 1,
  "rules": [
    {
      "op": "native",
      "to": ["*"]
    }
  ]
}
```

Владелец контракта с двухшаговой передачей владения: передать права новому ключу и принять их — нужно на время ротации ключа.

```json
{
  "version": 1,
  "rules": [
    {
      "op": "contract_call",
      "contracts": ["<адрес контракта>"],
      "methods": ["transferOwnership(address)", "acceptOwnership()"]
    }
  ]
}
```

### Как читается решение

| Транзакция газового кошелька с политикой `native → internal:deposit` | Результат |
|---|---|
| ETH на свой депозитный адрес | подписано |
| ETH на депозитный адрес другого сервиса | `403 POLICY_VIOLATION` |
| ETH на произвольный адрес | `403 POLICY_VIOLATION` |
| перевод токена | `403 POLICY_VIOLATION` |
| ETH на адрес своей замены во время ротации | подписано (правило ротации) |

## Ротация ключа сервиса

Ключ сервиса (`X-Service-Key`) — секрет, с которым сервис ходит в Wallet Service. Хранится только его хэш.

### Состояния

| Состояние | Принимается |
|---|---|
| основной | всегда; у сервиса ровно один основной ключ |
| в периоде перекрытия | до своего срока окончания |
| истёкший | нет |
| отозванный | нет |

### Создание

Первый ключ сервиса выдаётся один раз и показывается один раз. Повторное создание, пока у сервиса есть основной ключ, запрещено — ключ меняется только ротацией.

### Плановая ротация

```
ротация (период перекрытия, например 24 часа)
   ├─ создаётся новый ключ — он становится основным
   ├─ старый получает срок окончания = сейчас + период перекрытия
   └─ у старого фиксируется ссылка на новый
   │
   ▼  оба ключа принимаются
выкатка сервиса с новым ключом (поды переходят постепенно, ошибок нет)
   │
   ▼  срок окончания прошёл
старый ключ перестаёт приниматься автоматически
```

- Смена основного ключа атомарна: в любой момент у сервиса один основной ключ.
- Если старый ключ больше не нужен раньше срока, его можно отозвать вручную.
- Одновременные ротации одного сервиса не создают двух основных ключей: выигрывает одна, остальные получают ошибку.
- Цепочка ротаций сохраняется: по ссылкам видно, какой ключ каким заменён.

### Компрометация

- Ротация с нулевым периодом перекрытия: новый ключ выдан, старый перестаёт приниматься сразу. Сервис не работает, пока не выкатится с новым ключом.
- Отзыв конкретного ключа: перестаёт приниматься сразу, без выдачи нового.

## Ротация ключа кошелька

Новый ключ — это новый адрес. На старом адресе остаются средства, права в контрактах (owner, signer) и, возможно, транзакции, которые уже отправлены в сеть. Поэтому ротация идёт в несколько шагов, и на каждом шаге сервисы продолжают работать.

### Шаги

```
          rotate              activate              revoke
active(A) ──────▶ active(A)   ────────▶ rotating(A) ──────▶ revoked(A)
                  pending(B)            active(B)            active(B)
```

1. **Начало.** Для активного ключа A создаётся B в статусе `pending` в той же роли; у A фиксируется, что его заменяет B. Сервисы по-прежнему получают A как текущий ключ.
2. **Подготовка B.**
   - B пополняется газом. A может отправить средства на B: это разрешено правилом ротации, даже если политика роли такие переводы не разрешает.
   - Передаются права в контрактах: A подписывает передачу владения на B, B подписывает принятие (в статусе `pending` B уже подписывает). Эти методы должны быть разрешены политикой роли.
3. **Активация.** Атомарно: B становится `active`, A — `rotating`. Сервисы при следующем запросе текущего ключа получают B. A продолжает подписывать: транзакции, собранные до переключения, проходят.
4. **Вывод остатков.** После переходного периода A отправляет оставшиеся средства на B (то же правило ротации).
5. **Отзыв.** A становится `revoked` и больше ничего не подписывает.

**Отмена** до активации: отзыв B. У A снимается ссылка на замену, A остаётся активным, ротацию можно начать заново.

### Правило ротации

Ключ, у которого есть замена, может отправить нативную монету и токены **только на адрес своей замены**, независимо от политики роли. Все остальные операции по-прежнему проверяются политикой. Так средства переносятся без расширения прав старого ключа.

### Инварианты

- У single-роли в любой момент не больше одного `active` ключа; активация и включение ключа проверяют это под блокировкой роли.
- У ключа не больше одной незавершённой замены: повторная ротация до активации или отмены отклоняется; одновременные ротации одного ключа создают ровно одну замену.
- Ротировать можно только `active` ключ; активировать — только `pending`.
- Даты ротации и отзыва сохраняются; ключи не удаляются.

### По типам ролей

- `single` — полный цикл.
- `pool` — тот же цикл для каждого ключа пула; остальные ключи пула в это время работают.
- `deposit` — не ротируются: ключ живёт один заказ.

### Что требуется от сервисов

- Не хранить `key_id` в конфигурации; получать текущий ключ роли перед операцией (допустим короткий кэш).
- Если ключ перестал подписывать (отозван или отключён), запросить текущий ключ заново и пересобрать транзакцию.
- Ключ в `rotating` продолжает подписывать, поэтому закэшированный ключ переживает ротацию без деплоя.

### Отключение

Для подозрения на компрометацию без немедленной замены: `active` → `disabled` останавливает подписи; ключ можно включить обратно. Для single-роли включение запрещено, если уже есть другой активный ключ.

## Аренда ключей (lease)

У адреса один nonce, поэтому ключом пула в каждый момент должен пользоваться один отправитель. Сервис может работать в нескольких экземплярах, и блокировка «в памяти» у каждого своя. Аренда даёт ключ одному экземпляру сервиса на время «выбрал ключ → собрал транзакцию → подписал → отправил».

### Правила

- В аренду выдаются только `active`-ключи без живой аренды; первым — тот, который дольше всех не использовался.
- Конкурирующие запросы получают разные ключи; если свободных нет — ошибка «нет свободного ключа», запрос повторяется позже.
- У аренды есть срок (по умолчанию 60 секунд, максимум 10 минут). Аренду можно продлить и освободить. Упавший экземпляр держит ключ не дольше срока.
- Подпись:
  - ключ `pool`-роли подписывает только с идентификатором своей живой аренды;
  - ключ `single`-роли подписывает без аренды, но если он арендован — только с её идентификатором. Так экземпляры сервиса могут при необходимости брать эксклюзивно и газовый кошелёк;
  - аренда проверяется до расшифровки ключа.
- Ключи `deposit` не арендуются.
- Аренда, взятая до ротации, продолжает действовать, когда ключ становится `rotating`: экземпляр дописывает начатое.
- Время аренды считается по часам базы, расхождение часов экземпляров не влияет.

### Работа сервиса с пулом

```
1. арендовать ключ роли                → ключ, адрес, идентификатор аренды
2. проверить газ на адресе; мало       → освободить, арендовать другой, исключив этот
3. nonce = количество транзакций адреса с учётом mempool; собрать транзакцию
4. подписать с идентификатором аренды
5. отправить в сеть
6. освободить аренду; если шаги 3–5 долгие — продлевать
```

Ждать подтверждения транзакции под арендой не нужно: следующий арендатор возьмёт nonce с учётом транзакций в mempool. Если аренда истекла после подписи, но до отправки, транзакцию можно отправить — подпись уже получена.

Wallet Service не знает балансов (у него нет доступа к сети), поэтому выбор ключа «с газом» остаётся за сервисом через исключение ключей.

### Админ

Видит, кто и до какого времени держит ключ, и может принудительно снять аренду с ключа зависшего экземпляра.

Аренда хранится вместе с ключом, а не во внешнем хранилище блокировок: подпись проверяет ключ и его аренду в одном месте, и аренда живёт между запросами, а не в рамках одного соединения.

## API

### Общее

- Все ручки под `/api/v1`, формат JSON.
- Успех: `{"success": true, "data": ...}`.
- Ошибка: `{"success": false, "error": {"code": "...", "message": "...", "fields": {...}}}`; `fields` — только для ошибок валидации.
- Сети: `ethereum`, `tron`, `arbitrum`.
- Ручки сервисов требуют заголовок `X-Service-Key`; сервис работает только со своими ролями и ключами.

Объект **ключа** в ответах:

| Поле | |
|---|---|
| `id` | id ключа |
| `service`, `purpose`, `label`, `network`, `kind` | роль ключа |
| `alias` | человекочитаемое имя, если задано |
| `address` | адрес в сети |
| `status` | `active`, `pending`, `rotating`, `disabled`, `revoked` |
| `replaced_by` | id замены, если идёт или прошла ротация |
| `lease_owner`, `leased_until` | кто арендовал ключ и до какого времени |
| `created_at` | |

Объект **роли**: `id`, `service`, `purpose`, `label`, `network`, `kind`, `policy`, `created_at`.

Приватные ключи и их шифротексты не возвращаются ни одной ручкой.

### Ручки для сервисов

#### `GET /api/v1/roles/{purpose}/key` — текущий ключ роли

| Параметр | Где | |
|---|---|---|
| `purpose` | путь | обязательный |
| `network` | query | обязательный |
| `label` | query | необязательный, по умолчанию пустой |

Только для `single`-роли. Ответ — объект ключа в статусе `active`.

| Ошибка | |
|---|---|
| `401 UNAUTHORIZED` | нет или неверный ключ сервиса |
| `400 VALIDATION_FAILED` | нет `network` или неизвестная сеть |
| `400 INVALID_PARAMS` | роль не `single` |
| `404 ROLE_NOT_FOUND` | у сервиса нет такой роли |
| `404 KEY_NOT_FOUND` | у роли нет активного ключа |

Пример:

```bash
curl -s "http://localhost:8080/api/v1/roles/gas_station/key?network=ethereum" \
  -H "X-Service-Key: $SERVICE_KEY"
```

```json
{
  "success": true,
  "data": {
    "id": "01a17752-6600-77f0-bef0-080a2aa21d03",
    "service": "exchange",
    "purpose": "gas_station",
    "label": "",
    "network": "ethereum",
    "kind": "single",
    "address": "0x6648fD38dce6f060999bAB2AaB38e0E0E1ab2a88",
    "status": "active",
    "created_at": "2026-10-26T08:27:31Z"
  }
}
```

```json
{
  "success": false,
  "error": {
    "code": "ROLE_NOT_FOUND",
    "message": "role not found"
  }
}
```

#### `GET /api/v1/roles/{purpose}/keys` — все ключи роли

Параметры те же. Для `single` и `pool`. Ответ — список ключей во всех статусах; новые транзакции собираются с `active`, `rotating` только дописывают начатое.

| Ошибка | |
|---|---|
| `400 INVALID_PARAMS` | роль `deposit` |
| остальные | как у `/key` |

Пример:

```bash
curl -s "http://localhost:8080/api/v1/roles/executor/keys?network=ethereum" \
  -H "X-Service-Key: $SERVICE_KEY"
```

```json
{
  "success": true,
  "data": [
    {
      "id": "01a168da-7b2c-7d41-9e0a-3f5c2b1d8e77",
      "service": "exchange",
      "purpose": "executor",
      "label": "",
      "network": "ethereum",
      "kind": "pool",
      "address": "0xB3A460482e7aDAB99C0365964c430a6Fbf5e7c8d",
      "status": "active",
      "created_at": "2026-10-23T12:58:36Z"
    },
    {
      "id": "01a168da-7c11-7a02-8b3e-91d4c6a0f512",
      "service": "exchange",
      "purpose": "executor",
      "label": "",
      "network": "ethereum",
      "kind": "pool",
      "address": "0x519112D944b19da098dA220C8980117d03c065a7",
      "status": "rotating",
      "replaced_by": "01a17752-661d-7066-a8a2-5d3ccb86241f",
      "created_at": "2026-10-23T12:58:37Z"
    }
  ]
}
```

#### `POST /api/v1/keys/{id}/sign-transaction` — подписать транзакцию

```json
{
  "network": "ethereum",
  "raw_tx": "0x...",
  "lease_id": "01a17c2e-4b1a-7f30-9c6e-2d8a51f0b7c4"
}
```

`lease_id` — обязателен для ключа `pool`-роли; для остальных — только если ключ арендован.

| Сеть | `raw_tx` | `signed_tx` | `tx_hash` |
|---|---|---|---|
| `ethereum`, `arbitrum` | неподписанная транзакция в бинарной кодировке (legacy или EIP-1559) | для `eth_sendRawTransaction` | `0x…` |
| `tron` | `raw_data_hex` из ответа ноды | транзакция для `/wallet/broadcasthex` | txID без `0x` |

Ответ: `{"signed_tx": "0x...", "tx_hash": "..."}`.

| Ошибка | |
|---|---|
| `400 VALIDATION_FAILED` | нет `network` / `raw_tx`, неизвестная сеть |
| `400 INVALID_TRANSACTION` | транзакция не разбирается, другой chain id, создание контракта, неподдерживаемый тип Tron-контракта, истёк срок, отправитель Tron-транзакции не адрес ключа; в сообщении указана причина |
| `400 INVALID_PARAMS` | сеть ключа не совпадает с `network` |
| `401 UNAUTHORIZED` | |
| `403 POLICY_VIOLATION` | операция не разрешена политикой роли |
| `404 KEY_NOT_FOUND` | ключа нет или он принадлежит другому сервису |
| `409 KEY_NOT_ACTIVE` | ключ `disabled` или `revoked` |
| `409 LEASE_REQUIRED` | ключ `pool`-роли без `lease_id` |
| `409 LEASE_EXPIRED` | аренда истекла, освобождена или принадлежит другому экземпляру |
| `409 KEY_LEASED` | ключ арендован другим экземпляром, а `lease_id` не передан |

Пример (Ethereum):

```bash
curl -s -X POST "http://localhost:8080/api/v1/keys/01a17752-6600-77f0-bef0-080a2aa21d03/sign-transaction" \
  -H "X-Service-Key: $SERVICE_KEY" -H "Content-Type: application/json" \
  -d '{"network":"ethereum","raw_tx":"0x02f20180843b9aca008506fc23ac0082520894111111111111111111111111111111111111111187038d7ea4c6800080c0808080"}'
```

```json
{
  "success": true,
  "data": {
    "signed_tx": "0x02f87283aa36a780830257fc8302581a825208943680e8e83a89fd340c0f8d4cc3844e22962cccb087038d7ea4c6800080c001a0…",
    "tx_hash": "0x7af155721828bddd17b03471c0de9f2bb553329b406a30ea5d9c75a863a5c9ec"
  }
}
```

Отказ по политике и неверная транзакция:

```json
{
  "success": false,
  "error": {
    "code": "POLICY_VIOLATION",
    "message": "transaction is not allowed by role policy"
  }
}

{
  "success": false,
  "error": {
    "code": "INVALID_TRANSACTION",
    "message": "invalid transaction: chain id mismatch: tx 11155111, want 1"
  }
}
```

Пример (Tron):

```bash
curl -s -X POST "http://localhost:8080/api/v1/keys/$KEY_ID/sign-transaction" \
  -H "X-Service-Key: $SERVICE_KEY" -H "Content-Type: application/json" \
  -d '{"network":"tron","raw_tx":"<raw_data_hex из /wallet/createtransaction>"}'
```

```json
{
  "success": true,
  "data": {
    "signed_tx": "0x0a…12410…",
    "tx_hash": "5f1c…e9"
  }
}
```

#### `POST /api/v1/roles/{purpose}/lease` — арендовать ключ

| Поле | Обязательное | |
|---|---|---|
| `network` | да | |
| `label` | нет | по умолчанию пустой |
| `owner` | нет | кто держит ключ (имя пода), до 128 символов |
| `ttl_seconds` | нет | по умолчанию 60, максимум 600 |
| `exclude` | нет | id ключей, которые не выдавать |

Для `single` и `pool`.

| Ошибка | |
|---|---|
| `409 NO_FREE_KEY` | все ключи арендованы или исключены; заголовок `Retry-After` |
| `400 INVALID_PARAMS` | роль `deposit` |
| `404 ROLE_NOT_FOUND` | |
| `400 VALIDATION_FAILED` | |

Пример:

```bash
curl -s -X POST "http://localhost:8080/api/v1/roles/executor/lease" \
  -H "X-Service-Key: $SERVICE_KEY" -H "Content-Type: application/json" \
  -d '{"network":"ethereum","owner":"exchange-7d9f-abc","ttl_seconds":60}'
```

```json
{
  "success": true,
  "data": {
    "lease_id": "01a17c2e-4b1a-7f30-9c6e-2d8a51f0b7c4",
    "expires_at": "2026-10-26T09:41:12Z",
    "key": {
      "id": "01a168da-7b2c-7d41-9e0a-3f5c2b1d8e77",
      "service": "exchange",
      "purpose": "executor",
      "label": "",
      "network": "ethereum",
      "kind": "pool",
      "address": "0xB3A460482e7aDAB99C0365964c430a6Fbf5e7c8d",
      "status": "active",
      "lease_owner": "exchange-7d9f-abc",
      "leased_until": "2026-10-26T09:41:12Z",
      "created_at": "2026-10-23T12:58:36Z"
    }
  }
}
```

```json
{
  "success": false,
  "error": {
    "code": "NO_FREE_KEY",
    "message": "no free key in role, retry later"
  }
}
```

#### `POST /api/v1/leases/{lease_id}/extend` — продлить аренду

Тело необязательное: `{"ttl_seconds": 60}`. Ошибка: `409 LEASE_EXPIRED`.

```bash
curl -s -X POST "http://localhost:8080/api/v1/leases/01a17c2e-4b1a-7f30-9c6e-2d8a51f0b7c4/extend" \
  -H "X-Service-Key: $SERVICE_KEY" -H "Content-Type: application/json" -d '{"ttl_seconds":120}'
```

```json
{
  "success": true,
  "data": {
    "expires_at": "2026-10-26T09:43:12Z"
  }
}
```

#### `DELETE /api/v1/leases/{lease_id}` — освободить аренду

Ошибка: `409 LEASE_EXPIRED` — уже освобождена или чужая.

```bash
curl -s -X DELETE "http://localhost:8080/api/v1/leases/01a17c2e-4b1a-7f30-9c6e-2d8a51f0b7c4" \
  -H "X-Service-Key: $SERVICE_KEY"
```

### Ручки для админа

Без ключа сервиса; доступ к admin API ограничивается отдельно (см. «Окружения и доступ»).

#### `POST /api/v1/admin/roles` — создать роль

```json
{
  "service": "exchange",
  "purpose": "gas_station",
  "label": "",
  "network": "ethereum",
  "kind": "single",
  "policy": {
    "version": 1,
    "rules": [...]
  }
}
```

`service`, `purpose`, `network`, `kind` обязательны. Ответ — роль с нормализованной политикой.

Ошибки: `400 VALIDATION_FAILED`, `400 INVALID_PARAMS` (невалидная политика, в сообщении — какое правило и поле), `409 ROLE_EXISTS`.

Пример:

```bash
curl -s -X POST "http://localhost:8080/api/v1/admin/roles" -H "Content-Type: application/json" \
  -d '{"service":"exchange","purpose":"gas_station","network":"ethereum","kind":"single","policy":{"version":1,"rules":[{"op":"native","to":["internal:deposit"]}]}}'
```

```json
{
  "success": true,
  "data": {
    "id": "01a17337-39a9-7687-bc3d-7c10fb0f50fc",
    "service": "exchange",
    "purpose": "gas_station",
    "label": "",
    "network": "ethereum",
    "kind": "single",
    "policy": {
      "version": 1,
      "rules": [
        {
          "op": "native",
          "to": [
            "internal:deposit"
          ]
        }
      ]
    },
    "created_at": "2026-10-25T13:20:11Z"
  }
}
```

```json
{
  "success": false,
  "error": {
    "code": "INVALID_PARAMS",
    "message": "invalid policy: rule 0: token rule requires contracts and supports only to and max_amount"
  }
}
```

#### `GET /api/v1/admin/roles?service=` — список ролей

Без `service` — роли всех сервисов.

```bash
curl -s "http://localhost:8080/api/v1/admin/roles?service=exchange"
```

```json
{
  "success": true,
  "data": [
    {
      "id": "01a17337-39a9-7687-bc3d-7c10fb0f50fc",
      "service": "exchange",
      "purpose": "gas_station",
      "label": "",
      "network": "ethereum",
      "kind": "single",
      "policy": {
        "version": 1,
        "rules": [
          {
            "op": "native",
            "to": [
              "internal:deposit"
            ]
          }
        ]
      },
      "created_at": "2026-10-25T13:20:11Z"
    }
  ]
}
```

#### `PUT /api/v1/admin/roles/{id}/policy` — заменить политику роли

```json
{
  "policy": {
    "version": 1,
    "rules": [...]
  }
}
```

Применяется сразу ко всем ключам роли. Ошибки: `400 VALIDATION_FAILED`, `400 INVALID_PARAMS`, `404 ROLE_NOT_FOUND`.

```bash
curl -s -X PUT "http://localhost:8080/api/v1/admin/roles/01a17337-39a9-7687-bc3d-7c10fb0f50fc/policy" -H "Content-Type: application/json" \
  -d '{"policy":{"version":1,"rules":[{"op":"native","to":["internal:deposit"],"max_amount":"50000000000000000"}]}}'
```

Ответ — роль с новой политикой.

#### `POST /api/v1/admin/roles/{id}/keys` — создать ключ роли

```json
{
  "alias": "gas-1",
  "external_ref": "order-1"
}
```

Тело необязательное; `external_ref` — только для `deposit` и для него обязателен. Ключ создаётся в статусе `active`. Ответ — ключ.

| Ошибка | |
|---|---|
| `400 INVALID_PARAMS` | `external_ref` не по типу роли |
| `404 ROLE_NOT_FOUND` | |
| `409 ACTIVE_KEY_EXISTS` | у `single`-роли уже есть активный ключ |
| `409 KEY_EXISTS` | `external_ref` уже занят |

Пример:

```bash
curl -s -X POST "http://localhost:8080/api/v1/admin/roles/01a17337-39a9-7687-bc3d-7c10fb0f50fc/keys"
```

```json
{
  "success": true,
  "data": {
    "id": "01a17752-6600-77f0-bef0-080a2aa21d03",
    "service": "exchange",
    "purpose": "gas_station",
    "label": "",
    "network": "ethereum",
    "kind": "single",
    "address": "0x6648fD38dce6f060999bAB2AaB38e0E0E1ab2a88",
    "status": "active",
    "created_at": "2026-10-26T08:27:31Z"
  }
}
```

Депозитный ключ:

```bash
curl -s -X POST "http://localhost:8080/api/v1/admin/roles/$DEPOSIT_ROLE_ID/keys" -H "Content-Type: application/json" \
  -d '{"external_ref":"order-1"}'
```

#### `POST /api/v1/admin/roles/{id}/keys/import` — импортировать существующий ключ

Для переноса ключей, которые сейчас лежат в окружении сервисов.

| Поле | Обязательное | |
|---|---|---|
| `private_key` | да | hex, с `0x` или без |
| `alias` | нет | |
| `external_ref` | для `deposit` | |

Ключ создаётся `active`; ограничения роли те же, что при создании ключа. Приватный ключ передаётся в теле запроса, поэтому вызов допустим только по TLS до сервиса и без логирования тел запросов на пути.

| Ошибка | |
|---|---|
| `400 INVALID_PARAMS` | неверный приватный ключ, `external_ref` не по типу роли |
| `404 ROLE_NOT_FOUND` | |
| `409 ACTIVE_KEY_EXISTS` | у `single`-роли уже есть активный ключ |
| `409 KEY_EXISTS` | такой адрес уже хранится |

```bash
curl -s -X POST "http://localhost:8080/api/v1/admin/roles/01a17337-39a9-7687-bc3d-7c10fb0f50fc/keys/import" \
  -H "Content-Type: application/json" \
  -d '{"private_key":"0x<64 hex>","alias":"migrated from env"}'
```

```json
{
  "success": true,
  "data": {
    "id": "01a17d41-0e2b-7c55-a1f3-6b9e2c47d018",
    "service": "exchange",
    "purpose": "gas_station",
    "label": "",
    "network": "ethereum",
    "kind": "single",
    "alias": "migrated from env",
    "address": "0x6648fD38dce6f060999bAB2AaB38e0E0E1ab2a88",
    "status": "active",
    "created_at": "2026-10-26T10:12:44Z"
  }
}
```

#### `GET /api/v1/admin/roles/{id}/keys` — ключи роли

Ошибка: `404 ROLE_NOT_FOUND`.

```bash
curl -s "http://localhost:8080/api/v1/admin/roles/01a17337-39a9-7687-bc3d-7c10fb0f50fc/keys"
```

Ответ — список ключей, как у `/roles/{purpose}/keys`.

#### `GET /api/v1/admin/keys` — ключи всех сервисов

| Параметр | |
|---|---|
| `service`, `purpose`, `label`, `network`, `status`, `external_ref` | фильтры, применяются только если переданы; `label=` — ключи с пустым label |
| `limit` | по умолчанию 100, максимум 1000 |

Сортировка — от новых к старым. Ошибка: `400 VALIDATION_FAILED`.

```bash
curl -s "http://localhost:8080/api/v1/admin/keys?service=exchange&purpose=executor&network=ethereum&status=active&limit=50"
```

Ответ — список ключей.

#### Жизненный цикл ключа

`POST /api/v1/admin/keys/{id}/{action}`, тело не нужно, ответ — ключ.

| `action` | Из статуса | Результат | Ошибки |
|---|---|---|---|
| `rotate` | `active` | новый ключ `pending` в той же роли (возвращается он), у старого — `replaced_by` | `400 INVALID_PARAMS` (deposit), `409 INVALID_KEY_STATUS`, `409 ROTATION_IN_PROGRESS` |
| `activate` | `pending` | ключ `active`, ключ, который он заменяет, — `rotating` | `409 INVALID_KEY_STATUS`, `409 ACTIVE_KEY_EXISTS` |
| `revoke` | любой, кроме `revoked` | `revoked`; отзыв `pending` отменяет ротацию | `409 INVALID_KEY_STATUS` |
| `disable` | `active` | `disabled` | `409 INVALID_KEY_STATUS` |
| `enable` | `disabled` | `active` | `409 INVALID_KEY_STATUS`, `409 ACTIVE_KEY_EXISTS` |

Для всех: `404 KEY_NOT_FOUND`.

`POST /api/v1/admin/keys/{id}/release-lease` — снять аренду с ключа зависшего экземпляра сервиса.

```bash
curl -s -X POST "http://localhost:8080/api/v1/admin/keys/01a168da-7b2c-7d41-9e0a-3f5c2b1d8e77/release-lease"
```

#### Ключи сервисов

Объект **ключа сервиса** в ответах (хэш ключа не возвращается):

| Поле | |
|---|---|
| `id` | |
| `service` | |
| `state` | `primary` — основной, `expiring` — в периоде перекрытия, `expired` — срок вышел, `revoked` — отозван |
| `expires_at` | конец периода перекрытия |
| `replaced_by` | id ключа, который его заменил |
| `created_at`, `revoked_at` | |

##### `POST /api/v1/admin/service-clients` — выдать первый ключ сервиса

```bash
curl -s -X POST "http://localhost:8080/api/v1/admin/service-clients" \
  -H "Content-Type: application/json" -d '{"service":"exchange"}'
```

```json
{
  "success": true,
  "data": {
    "service_key": "5d2c0a7e9b4f1386a2e7c0d94b1f6a3e8c5d2b7f0a9e4c1d6b3f8a2e5c7d0b91",
    "client": {
      "id": "01a1733e-ba7c-7595-afb1-ef3e2e61e87c",
      "service": "exchange",
      "state": "primary",
      "created_at": "2026-10-25T13:27:33Z"
    }
  }
}
```

`service_key` показывается один раз. Если у сервиса уже есть основной ключ — `409 SERVICE_CLIENT_EXISTS`: ключ меняется только ротацией.

##### `POST /api/v1/admin/service-clients/rotate` — ротация ключа сервиса

| Поле | |
|---|---|
| `service` | обязательно |
| `grace_seconds` | сколько принимается старый ключ; по умолчанию 86400 (24 часа), `0` — отключить сразу |

```bash
curl -s -X POST "http://localhost:8080/api/v1/admin/service-clients/rotate" \
  -H "Content-Type: application/json" -d '{"service":"exchange","grace_seconds":86400}'
```

```json
{
  "success": true,
  "data": {
    "service_key": "9f2b61c0d4e8a7b35c1f0e6d2a9b8c7d4e3f2a1b0c9d8e7f6a5b4c3d2e1f0a9b",
    "client": {
      "id": "01a17d52-7a10-7e61-b0c2-3d4e5f6a7b8c",
      "service": "exchange",
      "state": "primary",
      "created_at": "2026-10-26T10:30:00Z"
    },
    "old_client": {
      "id": "01a1733e-ba7c-7595-afb1-ef3e2e61e87c",
      "service": "exchange",
      "state": "expiring",
      "expires_at": "2026-10-27T10:30:00Z",
      "replaced_by": "01a17d52-7a10-7e61-b0c2-3d4e5f6a7b8c",
      "created_at": "2026-10-25T13:27:33Z"
    }
  }
}
```

Ошибки: `404 SERVICE_CLIENT_NOT_FOUND` — у сервиса нет основного ключа (или его одновременно ротировал другой запрос), `400 VALIDATION_FAILED`.

##### `POST /api/v1/admin/service-clients/{id}/revoke` — отозвать ключ сервиса

Ключ перестаёт приниматься сразу. Ошибка: `404 SERVICE_CLIENT_NOT_FOUND` — нет такого активного ключа.

```bash
curl -s -X POST "http://localhost:8080/api/v1/admin/service-clients/01a1733e-ba7c-7595-afb1-ef3e2e61e87c/revoke"
```

##### `GET /api/v1/admin/service-clients?service=` — ключи сервисов

Без `service` — все сервисы, от новых к старым.

```bash
curl -s "http://localhost:8080/api/v1/admin/service-clients?service=exchange"
```

```json
{
  "success": true,
  "data": [
    {
      "id": "01a17d52-7a10-7e61-b0c2-3d4e5f6a7b8c",
      "service": "exchange",
      "state": "primary",
      "created_at": "2026-10-26T10:30:00Z"
    },
    {
      "id": "01a1733e-ba7c-7595-afb1-ef3e2e61e87c",
      "service": "exchange",
      "state": "expiring",
      "expires_at": "2026-10-27T10:30:00Z",
      "replaced_by": "01a17d52-7a10-7e61-b0c2-3d4e5f6a7b8c",
      "created_at": "2026-10-25T13:27:33Z"
    }
  ]
}
```

Пример ротации:

```bash
# начать ротацию A: в ответе новый ключ B (pending)
curl -s -X POST "http://localhost:8080/api/v1/admin/keys/01a17752-6600-77f0-bef0-080a2aa21d03/rotate"
```

```json
{
  "success": true,
  "data": {
    "id": "01a17752-661d-7066-a8a2-5d3ccb86241f",
    "service": "exchange",
    "purpose": "gas_station",
    "label": "",
    "network": "ethereum",
    "kind": "single",
    "address": "0x3680e8E83A89Fd340C0F8d4cC3844e22962CCCb0",
    "status": "pending",
    "created_at": "2026-10-26T08:27:31Z"
  }
}
```

```bash
# переключить на B
curl -s -X POST "http://localhost:8080/api/v1/admin/keys/01a17752-661d-7066-a8a2-5d3ccb86241f/activate"

# после вывода остатков отозвать A
curl -s -X POST "http://localhost:8080/api/v1/admin/keys/01a17752-6600-77f0-bef0-080a2aa21d03/revoke"
```

```json
{
  "success": false,
  "error": {
    "code": "ROTATION_IN_PROGRESS",
    "message": "key already has pending replacement"
  }
}
```


## Так же сделать
- Создание одноразовых ключей (deposit) самим сервисом по `external_ref`.
- Ротация ключей, запускаемая самим сервисом (например, смена payout-пула в cedex).
- Аудит
