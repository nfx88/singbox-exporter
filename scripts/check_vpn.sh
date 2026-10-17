#!/bin/sh
# Companion self-healing script for sing-box on OpenWrt/FriendlyWrt (reference).
# Deployment-specific: adjust LOG/UPDATER/STATE paths and the proxy port to match
# your host before using. Cron example: */2 * * * * /opt/configs/sing-box/check_vpn.sh
#
# Мониторинг VPN-прокси sing-box.
# Анти-флап: recovery только после 3 подряд неудачных проб.
# Две пробы (gstatic + ip-api) для снижения ложных срабатываний.
# Эскалация: подписка (новые серверы) -> рестарт контейнера -> backoff 5/15/30/60 мин.
# Состояние: /tmp/singbox-vpn.state (strikes backoff_until attempt).
LOG="/var/log/singbox-vpn.log"
UPDATER="/opt/configs/sing-box/update_subscription.py"
STATE="/tmp/singbox-vpn.state"
TEST1="https://www.gstatic.com/generate_204"
TEST2="http://ip-api.com/json/"

ts() { date '+%Y-%m-%d %H:%M:%S'; }

proxy_ok() {
    curl -s -o /dev/null --connect-timeout 5 --max-time 10 --socks5-hostname 127.0.0.1:7890 "$1"
}

STRIKES=0
BACKOFF_UNTIL=0
ATTEMPT=0
if [ -f "$STATE" ]; then
    read -r STRIKES BACKOFF_UNTIL ATTEMPT < "$STATE"
    STRIKES=${STRIKES:-0}
    BACKOFF_UNTIL=${BACKOFF_UNTIL:-0}
    ATTEMPT=${ATTEMPT:-0}
fi
NOW=$(date +%s)

# VPN работает — сбрасываем состояние (и пишем о восстановлении, если были сбои)
if proxy_ok "$TEST1" && proxy_ok "$TEST2"; then
    if [ "$STRIKES" -ne 0 ]; then
        echo "[$(ts)] VPN восстановлен (strikes сброшены)." >> "$LOG"
    fi
    printf '0 0 0\n' > "$STATE"
    exit 0
fi

STRIKES=$((STRIKES + 1))

# Ещё в окне backoff — просто копим strike, recovery не запускаем
if [ "$NOW" -lt "$BACKOFF_UNTIL" ]; then
    printf '%d %d %d\n' "$STRIKES" "$BACKOFF_UNTIL" "$ATTEMPT" > "$STATE"
    exit 0
fi

# Меньше 3 подряд неудач — подозрение, ждём
if [ "$STRIKES" -lt 3 ]; then
    printf '%d %d %d\n' "$STRIKES" "$BACKOFF_UNTIL" "$ATTEMPT" > "$STATE"
    echo "[$(ts)] VPN подозрение ($STRIKES/3), жду следующей проверки." >> "$LOG"
    exit 0
fi

# --- recovery ---
ATTEMPT=$((ATTEMPT + 1))
echo "[$(ts)] VPN недоступен ($STRIKES пробы подряд), попытка восстановления #$ATTEMPT..." >> "$LOG"

if ! docker inspect -f '{{.State.Running}}' sing-box 2>/dev/null | grep -q true; then
    echo "[$(ts)] Контейнер sing-box не запущен, поднимаю..." >> "$LOG"
    docker start sing-box >> "$LOG" 2>&1
    sleep 5
fi

# Шаг 1: свежая подписка/серверы (updater сам рестартует sing-box при изменении конфига)
echo "[$(ts)] Обновляю подписку и конфиг..." >> "$LOG"
if timeout 120 python3 "$UPDATER" >> "$LOG" 2>&1; then
    sleep 6
    if proxy_ok "$TEST1"; then
        echo "[$(ts)] VPN восстановлен после обновления подписки." >> "$LOG"
        printf '0 0 0\n' > "$STATE"
        exit 0
    fi
fi

# Шаг 2: жёсткий рестарт контейнера (сброс состояния/выборов)
echo "[$(ts)] Рестартую sing-box..." >> "$LOG"
docker restart sing-box >> "$LOG" 2>&1
sleep 6
if proxy_ok "$TEST1"; then
    echo "[$(ts)] VPN восстановлен после рестарта." >> "$LOG"
    printf '0 0 0\n' > "$STATE"
    exit 0
fi

# Не помогло — откат назад с нарастающей паузой
case "$ATTEMPT" in
    1) BACKOFF=300 ;;
    2) BACKOFF=900 ;;
    3) BACKOFF=1800 ;;
    *) BACKOFF=3600 ;;
esac
BACKOFF_UNTIL=$((NOW + BACKOFF))
echo "[$(ts)] ВНИМАНИЕ: VPN всё ещё недоступен после восстановления. Следующая попытка через ${BACKOFF}с." >> "$LOG"
printf '%d %d %d\n' "$STRIKES" "$BACKOFF_UNTIL" "$ATTEMPT" > "$STATE"
exit 1
