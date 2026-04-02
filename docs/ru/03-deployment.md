# Развертывание

## Требования

- Хост Proxmox VE с ВМ QEMU, использующими серийные консоли (`--serial0 socket`)
- ВМ должны иметь `--vga serial0` для перенаправления вывода на серийный сокет
- Сетевой доступ к хосту PVE (напрямую или через Teleport `tsh`)

## Сборка

```bash
# Сборка через Podman (локальный Go не нужен)
make build

# Результат: ./serial-proxy (статический ELF, ~12МБ)
```

## Установка на Proxmox VE

### Автоматическая

```bash
make deploy
```

### Ручная

```bash
# Копируем бинарник
scp serial-proxy root@pve-host:/usr/local/bin/serial-proxy
chmod +x /usr/local/bin/serial-proxy

# Создаем каталог данных
mkdir -p /var/lib/serial-proxy

# Устанавливаем systemd unit
cp deployments/systemd/serial-proxy.service /etc/systemd/system/
systemctl daemon-reload
systemctl enable --now serial-proxy

# Проверяем
systemctl status serial-proxy
curl -s http://localhost:8800/health
```

## Systemd Unit

Включенный unit (`deployments/systemd/serial-proxy.service`) обеспечивает:

- Автоматический перезапуск при сбое (задержка 5с)
- Структурированное JSON-логирование в journald
- Усиление безопасности:
  - `NoNewPrivileges=true`
  - `ProtectSystem=strict`
  - `ProtectHome=true`
  - `PrivateTmp=true`
  - `ReadWritePaths=/var/lib/serial-proxy` (для SQLite истории)

### Настройка VMID

Отредактируйте строку `ExecStart` в unit-файле:

```ini
ExecStart=/usr/local/bin/serial-proxy --rest --vmid 700 --vmid 600 --vmid 601 --port 8800
```

## Мониторинг

### Логи

```bash
# Следить за структурированными логами
journalctl -u serial-proxy -f -o cat

# Фильтр по агенту
journalctl -u serial-proxy -o cat | jq 'select(.agent == "my-agent")'

# Фильтр ошибок
journalctl -u serial-proxy -o cat | jq 'select(.level == "ERROR")'
```

### Статистика

```bash
curl -s http://localhost:8800/stats | jq .
curl -s 'http://localhost:8800/history?limit=10' | jq .
```

## Безопасность

- REST API по умолчанию привязан к `127.0.0.1` (только localhost)
- Аутентификация отсутствует — контроль доступа через сетевой/SSH уровень хоста
- Доступ к серийной консоли эквивалентен физическому доступу к консоли ВМ
- SQLite история может содержать чувствительный вывод CLI — ограничьте доступ к `/var/lib/serial-proxy/`
