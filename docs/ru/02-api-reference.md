# Справочник API

## REST Эндпоинты

Все ответы в формате `application/json`. Все эндпоинты принимают `?agent=` или заголовок `X-Agent-ID` для трекинга.

### GET /exec

Выполнить одну CLI-команду.

| Параметр | Тип | По умолчанию | Описание |
|----------|-----|--------------|----------|
| `cmd` | string | **обязательно** | CLI-команда для выполнения |
| `vmid` | int | первый подключенный | ID виртуальной машины QEMU |
| `wait` | float | 3.0 | Секунды ожидания ответа |
| `prompt` | string | `#` | Строка промпта для определения конца вывода |
| `agent` | string | `anonymous` | Идентификатор агента |

**Ответ:**

```json
{
  "vmid": 700,
  "cmd": "show version",
  "output": "Cisco Adaptive Security Appliance...",
  "rid": "6b391544",
  "agent": "my-agent",
  "duration_s": 0.522
}
```

### POST /exec

Выполнить несколько команд последовательно на одной ВМ.

**Тело запроса:**

```json
{
  "vmid": 700,
  "commands": ["show version", "show license all"],
  "wait": 3.0,
  "prompt": "#",
  "agent": "batch-runner"
}
```

**Ответ:**

```json
{
  "vmid": 700,
  "results": [
    {"cmd": "show version", "output": "...", "duration_s": 0.52},
    {"cmd": "show license all", "output": "...", "duration_s": 0.48}
  ],
  "rid": "a1b2c3d4",
  "agent": "batch-runner",
  "duration_s": 1.01
}
```

### GET /enable

Войти в привилегированный режим (enable).

| Параметр | Тип | По умолчанию | Описание |
|----------|-----|--------------|----------|
| `vmid` | int | первый подключенный | ID ВМ |
| `password` | string | пусто | Пароль enable |

### GET /status

Показать подключения пула и доступные сокеты.

### GET /connect?vmid=NNN

Подключиться к новому серийному сокету ВМ.

### GET /disconnect?vmid=NNN

Отключиться от ВМ.

### GET /history

Запросить историю выполнения команд.

| Параметр | Тип | По умолчанию | Описание |
|----------|-----|--------------|----------|
| `agent` | string | все | Фильтр по агенту |
| `vmid` | int | все | Фильтр по VMID |
| `limit` | int | 50 | Максимум записей |

### GET /stats

Агрегированная статистика по агентам.

```json
{
  "total_commands": 42,
  "agents": [
    {"agent": "static-analyst", "count": 30, "avg_duration_s": 0.512}
  ]
}
```

### GET /health

Возвращает `{"ok": true}`.

---

## MCP Инструменты

Используются в режиме MCP stdio. Схемы ввода используют теги `jsonschema`.

| Инструмент | Описание |
|------------|----------|
| `serial_exec` | Выполнить CLI-команду на серийной консоли ВМ |
| `serial_status` | Показать статус подключений и доступные сокеты |
| `serial_enable` | Войти в привилегированный режим с паролем |
| `serial_raw` | Отправить сырые данные на серийную консоль |
