#!/bin/sh
# ============================================================================
# go-tsfile-api-test 统一入口
# 测试框架: go test（一个用例 = 一个子测试，CASE_ID 形如 RN145-005）
# ============================================================================
set -u

CMD="${1:-help}"
TGT="${2:-}"
REPORTS_DIR="${REPORT_DIR:-${TSFILE_REPORT_DIR:-reports}}"
BUILD_DIR="build"
BIN="$BUILD_DIR/tsuite.test"
GO_BIN="${GO_BIN:-/usr/local/go/bin/go}"
[ -x "$GO_BIN" ] || GO_BIN="go"

usage() { cat >&2 <<'EOF'
Usage: sh start.sh <command> [target]
  prepare              编译测试程序（go build + go test -c）
  all                  运行全部用例（go test -json → JUnit XML）
  page <name>          writer | tablet | reader | result
  case <case-id>       单条用例（例: RN145-005）
  cases <id1,id2>      批量（逗号分隔）
  list-cases --json    用例清单（JSON：自然测试节点；已导入 PLM 时附带 caseId）
  help                 帮助
EOF
}

need_bin() { [ -x "$BIN" ] || { echo "ERROR: 未编译，先 sh start.sh prepare" >&2; exit 1; }; }

prepare() {
  echo "=== go build ==="
  "$GO_BIN" build ./... || return 1
  mkdir -p "$BUILD_DIR"
  echo "=== go test -c ==="
  "$GO_BIN" test -c -o "$BIN" ./tsuite || return 1
  ls -lh "$BIN"
}

# 运行过滤表达式并生成 JUnit XML；$1=filter $2=输出前缀 $3=可选 CASE_ID（写进 case_id 属性）
run_filter() {
  local filter="$1" prefix="$2" cid="${3:-}"
  mkdir -p "$REPORTS_DIR"
  local jsonl="$REPORTS_DIR/$prefix.jsonl"
  "$GO_BIN" test -json -count=1 -timeout 60m -run "$filter" ./tsuite > "$jsonl" 2>&1
  local rc=$?
  python3 xml_from_gojson.py "$jsonl" "$REPORTS_DIR/$prefix.xml" "$cid" || return 1
  tail -5 "$jsonl"
  return $rc
}

all()   { need_bin; run_filter '.*' all; }
page()  {
  need_bin
  case "${1:-}" in
    writer) run_filter '^Test(NewWriter|WriteTableTablet|WriteArrowBatch|WriteAddProperty|WriterClose)$' page-writer;;
    tablet) run_filter '^Test(NewTablet|TabletAddTimestamp|TabletValueBounds)$' page-tablet;;
    reader) run_filter '^Test(NewReader|QueryTable|QueryColumns|QueryOptions|WithTimeRange|WithOffset|WithLimit|OffsetAndLimit|WithBatchSize|TagFilterConstruct|TagFilterOperators|TagFilterInvalid|GetTableSchema|GetAllTableSchemas)$' page-reader;;
    result) run_filter '^Test(ResultSetNext|ResultSetIsNull|ResultSetGetter|NullSemantics|ConsumeMode|QueryCombos|CloseSemantics|ErrorModel)$' page-result;;
    *) echo "Unknown page: ${1:-}" >&2; usage; return 2;;
  esac
}

case_one() {
  need_bin; [ -z "${1:-}" ] && { usage; return 2; }
  local node filter
  node=$(python3 map_lookup.py "$1" 2>/dev/null)
  if [ -z "$node" ]; then echo "[start.sh] CASE_ID 未在映射表中: $1" >&2; return 5; fi
  filter="^${node%%/*}\$/^${node##*/}\$"
  run_filter "$filter" "case-$1" "$1"
}

cases_batch() {
  need_bin; [ -z "${1:-}" ] && { usage; return 2; }
  local filters="" missing="" id node
  _oldifs="$IFS"; IFS=','
  for id in $1; do
    id=$(echo "$id" | sed 's/^[[:space:]]*//;s/[[:space:]]*$//')
    [ -z "$id" ] && continue
    node=$(python3 map_lookup.py "$id" 2>/dev/null)
    if [ -n "$node" ]; then
      f="^${node%%/*}\$/^${node##*/}\$"
      filters="${filters:+$filters|}$f"
    else
      missing="${missing:+$missing }$id"
    fi
  done
  IFS="$_oldifs"
  if [ -z "$filters" ]; then echo "[start.sh] 无匹配用例" >&2; return 5; fi
  [ -n "$missing" ] && echo "[start.sh] WARNING: 未映射的 CASE_ID: $missing" >&2
  run_filter "$filters" "cases"
}

list_cases() {
  mkdir -p "$REPORTS_DIR"
  python3 - <<'PY'
import json
cat = json.load(open('.plm-case-catalog.json', encoding='utf-8'))
try:
    m = json.load(open('.plm-case-id-map.json', encoding='utf-8'))
    nmap = m.get('nmap', m) or {}
except Exception:
    nmap = {}
rev = {}
for cid, nodes in nmap.items():
    for n in (nodes if isinstance(nodes, list) else [nodes]):
        rev[n] = cid
try:
    cn = json.load(open('.plm-cn-names.json', encoding='utf-8')) or {}
except Exception:
    cn = {}
out = []
for c in cat:
    node = c['nodeId']
    cid = rev.get(node)
    item = {
        'nodeId': node,
        'automationType': c.get('automationType', 'ts-go'),
        'sourceFile': c.get('sourceFile', 'tsuite/'),
        'name': cn.get(cid, c.get('name', node.split('/', 1)[-1])),
    }
    if cid:
        item['caseId'] = cid
    out.append(item)
print(json.dumps(out, ensure_ascii=False, indent=2))
PY
}

case "$CMD" in
  prepare) prepare;;
  all) all;;
  page) page "$TGT";;
  case) case_one "$TGT";;
  cases) cases_batch "$TGT";;
  list-cases) [ "$TGT" = "--json" ] || { usage; exit 2; }; list_cases;;
  help|-h|--help) usage;;
  *) usage; exit 2;;
esac
