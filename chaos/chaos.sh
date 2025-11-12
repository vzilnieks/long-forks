#!/usr/bin/env bash
set -euo pipefail

MODE="${MODE:-drop}"           # drop | loss | delay
BLOCK_TARGETS="${BLOCK_TARGETS:-}"
ON_SEC="${ON_SEC:-30}"
OFF_SEC="${OFF_SEC:-60}"

dev="$(ip route | awk '/default/ {print $5; exit}')"
echo "[chaos] iface=$dev mode=$MODE targets=$BLOCK_TARGETS on=${ON_SEC}s off=${OFF_SEC}s"

setup_drop() {
  iptables -N CHAOSIN  2>/dev/null || true
  iptables -N CHAOSOUT 2>/dev/null || true
  iptables -C INPUT  -j CHAOSIN  2>/dev/null || iptables -I INPUT  -j CHAOSIN
  iptables -C OUTPUT -j CHAOSOUT 2>/dev/null || iptables -I OUTPUT -j CHAOSOUT
  IFS=',' read -ra ARR <<< "$BLOCK_TARGETS"
  for ip in "${ARR[@]}"; do
    [ -z "$ip" ] && continue
    iptables -C CHAOSIN  -s "$ip" -j DROP 2>/dev/null || iptables -A CHAOSIN  -s "$ip" -j DROP
    iptables -C CHAOSOUT -d "$ip" -j DROP 2>/dev/null || iptables -A CHAOSOUT -d "$ip" -j DROP
  done
}

clear_drop() {
  iptables -D INPUT  -j CHAOSIN  2>/dev/null || true
  iptables -D OUTPUT -j CHAOSOUT 2>/dev/null || true
  iptables -F CHAOSIN  2>/dev/null || true
  iptables -F CHAOSOUT 2>/dev/null || true
  iptables -X CHAOSIN  2>/dev/null || true
  iptables -X CHAOSOUT 2>/dev/null || true
}

setup_loss() { tc qdisc replace dev "$dev" root netem loss 100%; }
setup_delay(){ tc qdisc replace dev "$dev" root netem delay 2000ms 1000ms 25%; }
clear_tc()   { tc qdisc del dev "$dev" root 2>/dev/null || true; }

while true; do
  case "$MODE" in
    drop)  setup_drop ;;
    loss)  setup_loss ;;
    delay) setup_delay ;;
    *)     echo "unknown MODE=$MODE"; exit 1 ;;
  esac

  sleep "$ON_SEC"

  case "$MODE" in
    drop)  clear_drop ;;
    *)     clear_tc ;;
  esac

  sleep "$OFF_SEC"
done
