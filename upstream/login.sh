#!/usr/bin/env bash
# login.sh — WorkBuddy OAuth 登录 → 落盘 auth 文件
#
# 用法:
#   ./login.sh                 # 交互式选域（默认 1/国内版 cn）
#   ./login.sh --realm=global  # 传参优先，直达国际版
#   ./login.sh --realm=cn      # 传参优先，直达国内版
#   非交互（管道/cron）→ 回落默认 cn
#
# 流程:
#   1. 选域：--realm 传参优先；否则 stdin 是 tty → 交互式选域（go 侧 login realm
#      子命令提示 1)国内版 2)国际版，默认 1/cn）；非交互 → 回落 cn
#   2. POST /v2/plugin/auth/state 拿授权 URL（无 PKCE，state 由服务端签发）
#   3. 你在浏览器打开 URL 完成登录
#   4. 回到这里按 y → poll 拿 token+uid+nickname → （仅 CN）签到 → 落盘 auths/workbuddy-<uid>.json
#   5. 重启 workbuddy2api 容器加载新账号
set -euo pipefail

cd "$(dirname "$0")"
AUTH_DIR="./auths"
CONTAINER="workbuddy2api"

mkdir -p "$AUTH_DIR"

# login 工具：不存在才编译（源码改动后手动 go build -o login ./cmd/login）
LOGIN_BIN="./login"
if [[ ! -x "$LOGIN_BIN" ]]; then
    go build -o "$LOGIN_BIN" ./cmd/login
fi

# ─── realm 选域：--realm=cn|global 传参优先（跳过询问）──────────────
# 无传参：stdin 是 tty → 交互式选域（login realm 子命令负责提示+读数，go 侧逻辑可测）；
#         非交互（管道/cron，[ -t 0 ] 为假）→ 回落默认 cn 并提示。
REALM=""
if [[ $# -gt 0 && "$1" == --realm=* ]]; then
    REALM="${1#--realm=}"
fi
if [[ -z "$REALM" ]]; then
    if [[ -t 0 ]]; then
        REALM=$("$LOGIN_BIN" realm)
    else
        REALM="cn"
        echo "（非交互 stdin，默认国内版 cn；可用 --realm=global 指定国际版）" >&2
    fi
fi
# global 时跳过 CN 签到、auth 文件写 realm=global（见下方签到分支）

echo "============================================================"
echo "  WorkBuddy OAuth 登录"
echo "============================================================"
echo ""

AUTH_URL=$("$LOGIN_BIN" "--realm=$REALM" url)

echo "请在浏览器中打开以下链接完成登录："
echo ""
echo "  $AUTH_URL"
echo ""

if command -v xclip &>/dev/null; then
    echo -n "$AUTH_URL" | xclip -selection clipboard 2>/dev/null && echo "(已复制到剪贴板)"
elif command -v xsel &>/dev/null; then
    echo -n "$AUTH_URL" | xsel --clipboard 2>/dev/null && echo "(已复制到剪贴板)"
fi

echo ""
read -rp "完成登录后按 y 继续: " ans
if [[ "$ans" != "y" && "$ans" != "Y" ]]; then
    echo "已取消"
    exit 1
fi

echo ""
echo "正在获取 token..."

RESULT=$("$LOGIN_BIN" "--realm=$REALM" poll) || {
    echo ""
    echo "获取 token 失败。可能原因："
    echo "  - 登录还没完成就按了 y（重新运行 ./login.sh 再试）"
    echo "  - 登录页报错（把报错截图发出来排查）"
    exit 1
}

TOKEN=$(echo "$RESULT" | python3 -c "import json,sys; print(json.load(sys.stdin)['access_token'])")
REFRESH=$(echo "$RESULT" | python3 -c "import json,sys; print(json.load(sys.stdin)['refresh_token'])")
EXPIRES_IN=$(echo "$RESULT" | python3 -c "import json,sys; print(json.load(sys.stdin)['expires_in'])")
DOMAIN=$(echo "$RESULT" | python3 -c "import json,sys; print(json.load(sys.stdin).get('domain',''))")
USER_ID=$(echo "$RESULT" | python3 -c "import json,sys; print(json.load(sys.stdin).get('uid',''))")
ENT_ID=$(echo "$RESULT" | python3 -c "import json,sys; print(json.load(sys.stdin).get('enterprise_id',''))")
NICKNAME=$(echo "$RESULT" | python3 -c "import json,sys; print(json.load(sys.stdin).get('nickname',''))")

if [[ -z "$USER_ID" ]]; then
    echo "无法获取 uid，请检查 token 是否有效"
    exit 1
fi

EXPIRES_AT=$(( $(date +%s) + EXPIRES_IN ))

# ─── 签到（仅 CN：POST codebuddy.cn/v2/billing/meter/daily-checkin，幂等不阻塞。
#      global realm 跳过——国际版计费端点与签到端点未实测，避免误打 CN 端点）───
if [[ "$REALM" == "global" ]]; then
    echo "签到: global realm 跳过（国际版签到端点未实测）"
else
python3 - <<PYEOF
import json, urllib.request, urllib.error

req = urllib.request.Request(
    "https://www.codebuddy.cn/v2/billing/meter/daily-checkin",
    method="POST", data=b"{}",
    headers={
        "Authorization": "Bearer $TOKEN",
        "Accept": "application/json",
        "Content-Type": "application/json",
        "X-User-Id": "$USER_ID",
        **({"X-Enterprise-Id": "$ENT_ID", "X-Tenant-Id": "$ENT_ID"} if "$ENT_ID" else {}),
        **({"X-Domain": "$DOMAIN"} if "$DOMAIN" else {}),
    })
try:
    with urllib.request.urlopen(req, timeout=15) as r:
        body = json.loads(r.read().decode() or "{}")
    if body.get("code") == 0:
        data = body.get("data") or {}
        print(f"签到: 成功 {json.dumps(data, ensure_ascii=False)[:150]}")
    else:
        print(f"签到: {body.get('msg', json.dumps(body)[:150])}")
except urllib.error.HTTPError as e:
    # 已签到等业务错误也走 4xx（实测 code=10001 "今天已签到"）
    try:
        body = json.loads(e.read().decode() or "{}")
        print(f"签到: {body.get('msg', 'http %d' % e.code)}")
    except Exception:
        print(f"签到: http {e.code}")
except Exception as e:
    print(f"签到: {e}")
PYEOF
fi

# ─── 落盘 auth 文件（与 internal/auth 读取格式一致）─────────────────
AUTH_FILE="$AUTH_DIR/workbuddy-${USER_ID}.json"
if [[ -f "$AUTH_FILE" ]]; then
    echo "账号已存在（uid=${USER_ID}），将覆盖更新凭证"
    ACTION="覆盖"
else
    echo "新账号（uid=${USER_ID}），新增 auth 文件"
    ACTION="新增"
fi
python3 - <<PYEOF
import json

auth = {
    "account": {
        "uid": "$USER_ID",
        "enterpriseId": "$ENT_ID",
        "nickname": "$NICKNAME"
    },
    "auth": {
        "accessToken": "$TOKEN",
        "refreshToken": "$REFRESH",
        "expiresAt": $EXPIRES_AT,
        "domain": "$DOMAIN",
        "realm": "$REALM"
    }
}
with open("$AUTH_FILE", "w") as f:
    json.dump(auth, f, indent=1)
print(f"已保存（${ACTION}）: $AUTH_FILE")
PYEOF

# ─── 国际版注册激活 + trial 领取（仅 global；token 已落盘，失败只提示不阻断）──────
#
# 根因（ANALYSIS-workbuddy-client-reverse.md）：新 global 账号需先完善注册地区
# （/login/register/user/complete）再调 register 接口激活 Trial，chat 才不报 14017。
# register/trial 均幂等。region required 时**不再提示开网页**——直接调
# scripts/global_region.py 逆向后端自动完善：拉地区列表 → 终端选项单 → 提交地区
# → 重新 register 验证。任何失败不回退登录结果（auth 文件已写好）。
if [[ "$REALM" == "global" ]]; then
python3 - <<PYEOF
import json, os, sys, urllib.request, urllib.error

# scripts/global_region.py 定位：仓库根 scripts/（host；login.sh 已 cd 到仓库根）或
# /app/scripts/（容器）。heredoc stdin 脚本无 __file__，用 cwd 定位。
sys.path.insert(0, os.path.join(os.getcwd(), "scripts"))
sys.path.insert(0, "/app/scripts")
try:
    import global_region as gr
except ImportError as e:
    gr = None
    print(f"注册地区: 未加载 global_region.py（{e}），跳过完善+trial 步骤")

GLOBAL_BASE = "https://www.workbuddy.ai"
ACCOUNT_UID = "$USER_ID"
TOKEN = "$TOKEN"


def _register_inline():
    """降级路径（global_region.py 缺失时）：仅 register 查询，无 region/trial。"""
    headers = {
        "Authorization": "Bearer " + TOKEN,
        "Accept": "application/json",
        "Content-Type": "application/json",
        "X-User-Id": ACCOUNT_UID,
        "Origin": GLOBAL_BASE,
        "Referer": GLOBAL_BASE + "/",
    }
    try:
        req = urllib.request.Request(
            GLOBAL_BASE + "/auth/realms/copilot/overseas/user/register?userId=" + ACCOUNT_UID,
            method="GET", headers=headers)
        with urllib.request.urlopen(req, timeout=15) as r:
            return json.loads(r.read().decode() or "{}")
    except Exception as e:
        return {"_net_error": str(e)}


# 1) register 激活（幂等）：成功/已激活 → (True,False)；region 缺失 → (False,True)。
#    注意用 ACCOUNT_UID 而非 UID：UID 是 bash 只读内置变量，赋值/传址会得到 0。
needs_region = False
if gr is not None:
    okr, needs, msg = gr.activate_region(TOKEN, ACCOUNT_UID, base=GLOBAL_BASE)
    if okr:
        print("注册激活: 成功")
    elif needs:
        needs_region = True
        print("注册激活: 需完善注册地区")
    else:
        print(f"注册激活: {msg}")
else:
    r = _register_inline()
    if "_net_error" in r:
        print(f"注册激活: 网络失败（不阻断登录）: {r['_net_error']}")
    elif r.get("code") == 200:
        print("注册激活: 成功")
    else:
        print(f"注册激活: {r.get('msg', json.dumps(r)[:120])}")

# 1b) 需完善地区 → 自动出选项单并提交（无需打开网页）。
if needs_region and gr is not None:
    try:
        ok, countries, msg = gr.fetch_countries(base=GLOBAL_BASE)
        if not ok:
            print(f"  拉取地区列表失败: {msg}")
        else:
            # 检测当前地区（若已设置，直接复用，不出选项单）。
            _, cur_ios2, _, _ = gr.detect_region(TOKEN, base=GLOBAL_BASE)
            picked = None
            if cur_ios2:
                picked = next((c for c in countries if c["IOS2"] == cur_ios2), None)
                if picked:
                    print(f"  检测到当前地区: {picked['IOS2']} {picked['EnName']}，跳过选项单")
            if picked is None:
                picked = gr.interactive_menu(countries, cur_ios2,
                                             title="国际版账号需完善注册地区")
            if picked is None:
                print("  未选择地区，跳过完善（后续可重跑登录完善）")
            else:
                okc, msgc = gr.complete_flow(TOKEN, ACCOUNT_UID, pick=picked,
                                             call_register=False, base=GLOBAL_BASE)
                if okc:
                    print(f"  地区已完善: {picked['IOS2']} {picked['EnName']}（register 重新验证通过）")
                else:
                    print(f"  地区完善失败: {msgc}")
    except Exception as e:
        print(f"  自动完善地区异常（不阻断登录）: {e}")

# 2) trial 领取（幂等）：POST /billing/ide/trial；14051=已领过，不算失败。
if gr is not None:
    oktr, already, msgtr = gr.trial(TOKEN, base=GLOBAL_BASE)
    if oktr:
        suffix = "（已领取过）" if already else ""
        print(f"国际版 trial: 已激活，可以开始对话{suffix}")
    else:
        print(f"国际版 trial: {msgtr}（不阻断登录）")
else:
    headers = {
        "Authorization": "Bearer " + TOKEN,
        "Accept": "application/json",
        "Content-Type": "application/json",
        "X-User-Id": ACCOUNT_UID,
        "Origin": GLOBAL_BASE,
        "Referer": GLOBAL_BASE + "/",
    }
    try:
        req = urllib.request.Request(GLOBAL_BASE + "/billing/ide/trial", method="POST",
                                     data=b"{}", headers=headers)
        with urllib.request.urlopen(req, timeout=15) as r:
            trial = json.loads(r.read().decode() or "{}")
    except urllib.error.HTTPError as e:
        try:
            trial = json.loads(e.read().decode() or "{}")
        except Exception:
            trial = {"_http_error": True, "code": e.code}
    except Exception as e:
        trial = {"_net_error": str(e)}

    if "_net_error" in trial:
        print(f"国际版 trial: 网络失败（不阻断登录）: {trial['_net_error']}")
    elif trial.get("code") == 0 or "14051" in json.dumps(trial, ensure_ascii=False):
        print("国际版 trial: 已激活，可以开始对话")
    else:
        print(f"国际版 trial: {trial.get('msg', json.dumps(trial)[:150])}")
PYEOF
fi
echo ""
if docker ps --format '{{.Names}}' | grep -q "^${CONTAINER}$"; then
    echo "重启 $CONTAINER 加载新账号..."
    docker restart "$CONTAINER" >/dev/null
    sleep 2
    # API_KEY 从 config.json 读取（该变量在脚本中未定义，fallback 仅为占位，不会通过鉴权）
    API_KEY=$(python3 -c "import json; print(json.load(open('config.json')).get('api_key',''))" 2>/dev/null)
    COUNT=$(curl -s http://127.0.0.1:7863/status -H "Authorization: Bearer ${API_KEY:-test_key}" 2>/dev/null | python3 -c "import json,sys; print(len(json.load(sys.stdin).get('accounts',[])))" 2>/dev/null || echo "?")
    echo "服务已重启，当前账号数: $COUNT"
else
    echo "容器 $CONTAINER 未运行，auth 文件已保存，下次启动自动加载"
fi

echo ""
echo "============================================================"
echo "  登录完成！"
echo "  UID: $USER_ID"
echo "  Realm: $REALM"
echo "  Nickname: ${NICKNAME:-（未获取到）}"
echo "  Token: ${TOKEN:0:30}..."
echo "  有效期: $(date -d "@$EXPIRES_AT" '+%Y-%m-%d %H:%M' 2>/dev/null || echo "$EXPIRES_AT")"
echo "============================================================"
