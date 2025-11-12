
set -euo pipefail

NETWORK_ID=${NETWORK_ID:-1337}
PERIOD=${PERIOD:-1}
EPOCH=${EPOCH:-30000}
SIGNERS=${SIGNERS:-5}
CHAIN_DIR=/chain
NODES_DIR=/nodes
PASSFILE="$CHAIN_DIR/password"
prefix="${P2P_NET_PREFIX:-172.28.0.}"

mkdir -p "$CHAIN_DIR/signers" "$CHAIN_DIR/nodekeys" "$NODES_DIR" "$CHAIN_DIR/bin"
: > "$PASSFILE"
: > "$CHAIN_DIR/addresses_no0x.txt"
: > "$CHAIN_DIR/addresses_0x.txt"

for i in $(seq 1 "$SIGNERS"); do
  dd="$NODES_DIR/signer$i"
  mkdir -p "$dd"
  addr_raw=$(geth --datadir "$dd" account new --password "$PASSFILE" 2>/dev/null | awk -F'[{ }]' '/Public address of the key/{print $8}' | sed 's/^0[xX]//')
  echo "$addr_raw"                       >> "$CHAIN_DIR/addresses_no0x.txt"
  echo "0x$addr_raw"                    >> "$CHAIN_DIR/addresses_0x.txt"
  printf '0x%s' "$addr_raw"               >  "$CHAIN_DIR/signers/signer$i.address"
  openssl rand -hex 32 > "$CHAIN_DIR/nodekeys/signer$i.key"
  echo "Prepared signer$i: 0x$addr_raw"
done

sort "$CHAIN_DIR/addresses_no0x.txt" | awk '{print tolower($0)}' > "$CHAIN_DIR/addresses_no0x.sorted"

make_zeros() {
  n="$1"; i=0; while [ "$i" -lt "$n" ]; do printf '0'; i=$((i+1)); done
}
VANITY=$(make_zeros 64)
SEAL=$(make_zeros 130)
SIGNERS_HEX=""
while IFS= read -r a; do
  [ -z "$a" ] && continue
  if [ ${#a} -ne 40 ]; then echo "bad signer len: $a" >&2; exit 1; fi
  if echo "$a" | grep -qi '[^0-9a-f]'; then echo "non-hex in signer: $a" >&2; exit 1; fi
  SIGNERS_HEX="${SIGNERS_HEX}${a}"
done < "$CHAIN_DIR/addresses_no0x.sorted"
EXTRADATA="0x${VANITY}${SIGNERS_HEX}${SEAL}"
HEX_BODY="${EXTRADATA#0x}"
exp_len=$((64 + 40*SIGNERS + 130))
if [ ${#HEX_BODY} -ne "$exp_len" ]; then
  echo "extradata length ${#HEX_BODY} != expected $exp_len (SIGNERS=$SIGNERS)" >&2; exit 1
fi
if echo "$HEX_BODY" | grep -qi '[^0-9a-f]'; then echo "extradata has non-hex" >&2; exit 1; fi

sort "$CHAIN_DIR/addresses_0x.txt" > "$CHAIN_DIR/addresses_0x.sorted"
alloc_entries=""; first=1
while IFS= read -r a0x; do
  [ -z "$a0x" ] && continue
  if [ "$first" -eq 0 ]; then alloc_entries="$alloc_entries , "; fi
  first=0
  alloc_entries="$alloc_entries\"$a0x\": { \"balance\": \"0x3635C9ADC5DEA0000000\" }"
done < "$CHAIN_DIR/addresses_0x.sorted"

sed \
  -e "s/{{CHAIN_ID}}/$NETWORK_ID/g" \
  -e "s/{{PERIOD}}/$PERIOD/g" \
  -e "s/{{EPOCH}}/$EPOCH/g" \
  -e "s#{{EXTRADATA}}#$EXTRADATA#g" \
  -e "s#{{ALLOC}}#$alloc_entries#g" \
  genesis.tmpl.json > "$CHAIN_DIR/genesis.json"

mkdir -p "$CHAIN_DIR/config"

ENODES_TOML=""
for i in $(seq 1 "$SIGNERS"); do
  ip="${prefix}$((10+i))"             # 11..15
  pubhex=$(enodefromkey "$CHAIN_DIR/nodekeys/signer$i.key")
  enode="enode://$pubhex@${ip}:30303"
  [ -n "$ENODES_TOML" ] && ENODES_TOML="$ENODES_TOML,
    \"$enode\"" || ENODES_TOML="\"$enode\""
done

for i in $(seq 1 "$SIGNERS"); do
  ADDR=$(cat "$CHAIN_DIR/signers/signer$i.address")
  cat >"$CHAIN_DIR/config/signer$i.toml" <<EOF
  [Node.P2P]
    StaticNodes = [
        $ENODES_TOML
    ]
EOF
done

cat >"$CHAIN_DIR/bin/run-signer.sh" <<'EOF'
#!/bin/sh
set -eu
ID="${1:?usage: run-signer.sh signerN}"
DATADIR=/data
mkdir -p "$DATADIR/geth"
if [ -f /chain/static-nodes.json ]; then cp -f /chain/static-nodes.json "$DATADIR/geth/static-nodes.json"; fi
if [ ! -d "$DATADIR/geth/chaindata" ]; then geth init --datadir "$DATADIR" /chain/genesis.json; fi
ADDR="$(tr -d '\n' </chain/signers/${ID}.address)"
exec geth \
  --datadir "$DATADIR" \
  --networkid "${NETWORK_ID:-1337}" \
  --port 30303 \
  --http --http.port 8545 --http.api "eth,net,web3,personal,miner,clique" \
  --http.addr 0.0.0.0 \
  --http.corsdomain "*" \
  --ws --ws.addr 0.0.0.0 --ws.port  8546 --ws.api  "eth,net,web3,clique,txpool" --ws.origins "*" \
  --nodiscover \
  --nat "any" \
  --config /chain/config/${ID}.toml \
  --gpo.ignoreprice=0 \
  --allow-insecure-unlock --unlock "$ADDR" --password /chain/password \
  --rpc.allow-unprotected-txs=true \
  --mine --miner.gasprice 0 --miner.etherbase "$ADDR" \
  --verbosity 3 \
  --nodekey "/chain/nodekeys/${ID}.key"
EOF
chmod +x "$CHAIN_DIR/bin/run-signer.sh"

jq -r .extradata "$CHAIN_DIR/genesis.json" | awk '{printf("EXTRADATA length (chars): %d\n", length($0)); print $0}'

ls -l "$NODES_DIR"
ls -l "$CHAIN_DIR"
cat "$CHAIN_DIR/addresses_0x.txt"
cat "$CHAIN_DIR/genesis.json"
cat "$CHAIN_DIR/bin/run-signer.sh"
cat "$CHAIN_DIR/config/signer1.toml"
cat "$CHAIN_DIR/nodekeys/signer1.key"
echo "Init completed: genesis & static-nodes created (no bootnode)."

