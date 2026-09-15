# WorkBuddy2API Overlay

这是一个面向个人自托管的 WorkBuddy2API 定制仓库：固定的上游源码保存在
`upstream/`，新增能力放在 `extensions/` 和独立 `console/`，对上游文件的必要修改以
`patches/series` 中的有序补丁应用。构建不会改写 `upstream/`。

上游来源固定为 [Sliverkiss/workbuddy2api](https://github.com/Sliverkiss/workbuddy2api)，
具体 commit 和源码摘要见 `upstream.lock`。根目录 `LICENSE` 和 Git 历史保留原作者
版权及 MIT 许可信息。

## 启动

需要 Docker Engine 和 Docker Compose。取得完整仓库后运行：

```sh
docker compose up -d
docker compose logs console
```

Compose 会从本地锁定快照构建 `core` 和 `console`。只有 console 映射宿主机端口，
默认访问 `http://服务器地址:7863/`；两个服务都以 UID 10001 运行。首次启动时 core
生成彼此独立的管理、公共 API 和内部桥接密钥，console 日志只显示需要交给管理员的
管理密钥。输入该密钥后才能进入控制台。

首次添加账号仍需人工操作：在「账号管理」选择国内版或国际版并发起浏览器授权，
然后在上游页面完成登录、扫码或验证码；国际版如要求地区信息，还需在控制台选择真实
注册地区。服务不会代输密码、验证码或绕过激活流程。授权保存成功后账号会热加载，无需
重启。

「自动任务」展示六类任务的实际开关、北京时间排程、运行状态和历史。第一版只允许查看
和对全部符合条件账号立即执行；禁用任务不能从网页强行运行。只有上游明确返回的奖励才
显示为已确认奖励，余额差额和缺失值不会当作到账金额。

## 数据与配置

默认使用三个命名卷：

- `workbuddy2api_auths`：账号凭据；
- `workbuddy2api_data`：账号池状态和任务历史；
- `workbuddy2api_keys`：管理、API、桥接密钥。

普通重启和重建会复用这些卷。不要运行 `docker compose down -v`，也不要删除或用空卷
替代账号卷。可在 `.env` 中设置：

```dotenv
WB2A_PORT=7863
WB2A_BIND_ADDRESS=0.0.0.0
WB2A_PUBLIC_ORIGIN=
WB2A_ADMIN_KEY=
WB2A_API_KEY=
WB2A_CONFIG_FILE=
WB2A_AUTHS_VOLUME=workbuddy2api_auths
WB2A_DATA_VOLUME=workbuddy2api_data
WB2A_KEYS_VOLUME=workbuddy2api_keys
```

公网部署应由 HTTPS 反向代理转发到 console，并将 `WB2A_PUBLIC_ORIGIN` 设为浏览器实际
访问的完整 origin（不能带路径）。管理密钥至少 32 个字符；公共 API Key 可沿用原非空
值。两个服务必须使用同一组 `WB2A_ADMIN_KEY`/`WB2A_API_KEY` 覆盖，不能只在 core 的
`config.json` 中设置一个不同 API Key。

未设置 `WB2A_CONFIG_FILE` 时会只读挂载仓库的 `deploy/default-config.json`（空对象，使用
上游默认配置），全新启动无需创建用户配置。需要保留旧排程、任务开关或 global 设置时，
将 `WB2A_CONFIG_FILE` 设为原配置文件的绝对路径；只读挂载到 core 的 `/app/config.json`，
不会复制进镜像或改写原文件。显式选择的路径不存在、不是文件或 JSON 无效时启动失败，
不会创建目录或悄悄退回默认。环境变量仍覆盖对应配置；API Key 覆盖必须由两个服务共享。

`/livez` 表示进程存活，空账号也返回 200；`/healthz` 表示是否已有可服务账号，空账号
返回 503 是正常状态。

## 旧部署迁移

先只读解析旧容器实际使用的挂载，不能根据项目名猜卷名：

```sh
python3 deploy/migrate.py inspect --container OLD_CONTAINER > /tmp/wb2a-migration.json
python3 deploy/migrate.py backup \
  --manifest /tmp/wb2a-migration.json \
  --output /ABSOLUTE/NEW/BACKUP/DIRECTORY
```

运行中备份会明确标为非最终一致。正式切换前应确认维护窗口、停止已核实的旧实例（不删
卷），再生成并校验最终一致性备份。随后把 inspect 得到的账号卷、状态卷和原宿主端口写
入 `.env`，同时将 `WB2A_CONFIG_FILE` 指向已核实并保留的旧配置文件，再执行
`docker compose up -d --build`。若旧 `api_key` 与持久密钥不同，必须显式设置共享
`WB2A_API_KEY`；不要删掉配置来绕过冲突。挂载、备份或密钥发生冲突时必须停止，
不能生成空数据继续。

旧 `console-keys.json` 保持原字节不变；新布局会复用原管理/API Key 并新增桥接密钥。
网页会话和进行中的 OAuth 流程在重建后失效，已保存账号不会因此丢失。

## 验证与源码维护

完整本地检查：

```sh
python3 -m unittest discover -s scripts -p test_overlay.py -v
python3 -m unittest discover -s deploy -p test_migrate.py -v
python3 -m unittest discover -s deploy -p test_compose.py -v
bash scripts/check.sh
docker compose config --quiet
docker compose build
bash scripts/acceptance.sh
git diff --check
```

`scripts/check.sh` 在新的物化目录运行 Go 测试、vet、竞态测试以及 console、Node、Python
测试。`scripts/acceptance.sh` 只创建隔离 mock 项目和有标签的临时卷，不读取本机已登录
账号，也不执行真实 OAuth、模型消费或领奖。

如果当前 Docker 主机只有显式代理才能构建，应由操作者在命令中传入，不要写成仓库
默认值。例如本机 Docker Desktop 可使用：

```sh
docker compose build \
  --build-arg HTTP_PROXY=http://host.docker.internal:7890 \
  --build-arg HTTPS_PROXY=http://host.docker.internal:7890
HTTP_PROXY=http://host.docker.internal:7890 \
HTTPS_PROXY=http://host.docker.internal:7890 \
  bash scripts/acceptance.sh
```

## 手动更新上游

更新必须显式指定 ref。工具在临时候选目录应用当前扩展/补丁并完成检查和隔离验收；成功
只修改 `upstream/` 和 `upstream.lock`，不会提交、推送、发布镜像或部署：

```sh
python3 scripts/overlay.py update --ref COMMIT_OR_TAG
git diff -- upstream upstream.lock
git diff -- patches extensions deploy console scripts
git add upstream upstream.lock
git commit -m "build: update pinned upstream"
```

先审阅候选 diff 和提交，再由操作者明确部署：

```sh
docker compose config --quiet
docker compose up -d --build
```

更新失败时当前快照、锁文件和运行实例保持不变，诊断候选会保留。成功更新的旧
`upstream/` 与锁文件保存在忽略的 `.upstream-update-backup/`，但正式回退仍应通过 Git
生成可审阅提交；如果扩展或补丁也变更，必须回退同一组合：

```sh
git revert --no-commit UPSTREAM_UPDATE_COMMIT
git diff
git commit -m "revert: restore previous upstream combination"
docker compose up -d --build
```

回退继续复用现有 auths/data/keys 卷，不执行 `down -v`。若相关源码有未提交改动，更新
入口会拒绝覆盖；先提交或另行保存，不要强制清理。

## 开发目录

```text
upstream/    固定、可校验的上游普通源码快照
extensions/  注入 core 的新增文件和测试
patches/     对既有上游文件的有序补丁及维护说明
console/     独立管理页面、会话和代理服务
deploy/      两个镜像、Compose 验收和迁移工具
scripts/     overlay、检查和隔离验收入口
```

需要查看物化后的 core 时，使用一个尚不存在的输出路径：

```sh
python3 scripts/overlay.py prepare --output .build/core-review
go -C .build/core-review test ./...
```

不要直接修改 `upstream/`，不要把真实 `.env`、`config.json`、`auths/`、`data/` 或密钥
提交进仓库。

## 安全、合规与许可

本项目是非官方个人网关，仅应使用本人授权账号并遵守目标平台服务条款及所在地法律。
不得共享凭据、绕过验证或将服务暴露给未授权用户。使用、账号、数据、上游条款和服务
中断风险由部署者自行承担。

本仓库按根目录 [MIT License](LICENSE) 提供。再分发源码或二进制时须保留许可证、原作者
版权声明，并注明上游来源 `https://github.com/Sliverkiss/workbuddy2api`。
