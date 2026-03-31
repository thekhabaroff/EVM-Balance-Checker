# EVM Balance Checker

Простой EVM Balance Checker на Go для проверки нативных балансов и ERC-20 токенов по приватным ключам из `keys.txt`.

## Структура

```text
EVM Balance Checker
├── go.mod
├── bot.go
├── balance.txt
├── keys.txt
├── config.ini
└── README.md
```

## Что делает

- Загружает сети из `config.ini`
- Показывает список сетей
- Даёт выбрать одну сеть или все сети
- Читает приватные ключи из `keys.txt`
- Получает EVM-адрес для каждого ключа
- Проверяет нативный баланс
- Проверяет ERC-20 токены из секции `[tokens]`
- Пишет результат в `balance.txt`

Формат строки результата:

```text
PRIVATE_KEY:ADDRESS:BALANCE TOKEN (NETWORK)
```

Пример:

```text
<private_key>:0xE829a10B28A6F00Ab4131A31868D5dD6E0e8Aa1c:0.00016960411564595 ETH (Base)
```

## Подготовка

1. Установи Go
2. Положи приватные ключи в `keys.txt`, по одному на строку
3. При необходимости отредактируй `config.ini`

## Запуск

```bash
go mod tidy
go run bot.go
```

Или сборка бинаря:

```bash
go build -o checker bot.go
./checker
```

## Формат config.ini

### Сети

Секции `[mainnets]` и `[testnets]` используют формат:

```ini
NetworkName=https://rpc.url|chainId|NATIVE_SYMBOL
```

Пример:

```ini
Base=https://base-rpc.publicnode.com|8453|ETH
```

### Токены

Секция `[tokens]` использует формат:

```ini
TOKEN NETWORK=0xTokenAddress
```

Пример:

```ini
USDT Arbitrum=0xfd086bc7cd5c481dcc9c85ebe478a1c0b69fcbb9
```

### Настройки

```ini
[settings]
rpc_timeout_seconds=10
retries=3
append_output=true
```

- `rpc_timeout_seconds` — таймаут RPC-запроса
- `retries` — число повторов при ошибке RPC
- `append_output` — если `true`, результат дописывается в `balance.txt`; если `false`, файл очищается перед запуском

## Примечания

- Скрипт работает только с EVM-приватными ключами в hex-формате
- Ключ можно указывать с `0x` или без него
- Solana и другие не-EVM сети здесь не поддерживаются
- `decimals()` для ERC-20 кэшируется в рамках проверки сети и кошелька
- В `balance.txt` записываются только ненулевые балансы