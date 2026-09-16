# WorkBuddy2API Overlay

这是一个面向个人自托管的 WorkBuddy2API 定制仓库：固定的上游源码保存在
`upstream/`，新增能力放在 `extensions/` 和独立 `console/`，对上游文件的必要修改以
`patches/series` 中的有序补丁应用。构建不会改写 `upstream/`。

上游来源固定为 [Sliverkiss/workbuddy2api](https://github.com/Sliverkiss/workbuddy2api)，
具体 commit 和源码摘要见 `upstream.lock`。根目录 `LICENSE` 和 Git 历史保留原作者
版权及 MIT 许可信息。

## 启动

需要 Docker Engine 和 Docker Compose，目标服务器为 `linux/amd64`（Intel/AMD）。
服务器只需本仓库的 `docker-compose.yml` 和可选 `.env`；无需下载源码或安装构建工具：

```sh
docker compose up -d
docker compose logs console
```

`.env` 是可选的密钥覆盖入口，不需要启动脚本。没有 `.env`、文件中只有其他参数，
或两项密钥缺失/为空，都能直接启动：core 自动生成缺少的密钥，console 读取共享密钥文件。
只配置一项时保留该项、补齐另一项；重启复用持久化密钥，不会反写 `.env` 或宿主机环境变量。
已配置但无效的密钥（如管理密钥过短、两项相同）会明确报错，不会擅自替换。

Compose 从阿里云仓库 `registry.cn-hangzhou.aliyuncs.com/cateyes/go` 拉取
`wb2api-core-<时间戳>` 和 `wb2api-webui-<时间戳>`，不会构建。两个镜像使用相同时间戳。
仓库若为私有，服务器需先执行 `docker login registry.cn-hangzhou.aliyuncs.com`。
请使用发布脚本成功更新后的 Compose，其中的默认时间戳对应已验收并推送的镜像。
只有 console 映射宿主机端口，
固定访问 `http://服务器地址:7863/`；Compose 只有 core 和 console 两个服务。
core 镜像内的启动脚本先以 root 将三个存储目录的所有者设为 10001、权限设为 700，
不递归改写已有文件；失败立即退出，成功后以 `exec` 切换到 UID/GID 10001 运行程序。
两个业务进程均以普通用户运行；core 的 `docker compose exec` 默认仍为 root，需要普通
用户时显式加 `--user 10001:10001`。首次启动时 core
生成彼此独立的管理、公共 API 和内部桥接密钥，console 日志只显示需要交给管理员的
管理密钥，包括 `.env` 中手动设置的值。输入该密钥后才能进入控制台。
注意：能查看 console 日志的人也能获取管理登录密钥，请限制日志访问和转发范围；
API Key 和内部桥接密钥不会写入启动日志。

保留源码构建方式（需要完整仓库）：

```sh
docker compose -f docker-compose.build.yaml up -d --build
```

构建版复用运行版服务定义，使用本地 `:dev` 标签，同样构建 amd64。两个入口使用同一组
存储目录，不应同时启动。Apple Silicon 可借助 Docker Desktop 模拟运行；不提供 ARM
原生发布镜像。

首次添加账号仍需人工操作：在「账号管理」选择国内版或国际版并发起浏览器授权，
然后在上游页面完成登录、扫码或验证码；国际版如要求地区信息，还需在控制台选择真实
注册地区。服务不会代输密码、验证码或绕过激活流程。授权保存成功后账号会热加载，无需
重启。

「自动任务」展示六类任务的实际开关、北京时间排程、运行状态和历史。第一版只允许查看
和对全部符合条件账号立即执行；禁用任务不能从网页强行运行。只有上游明确返回的奖励才
显示为已确认奖励，余额差额和缺失值不会当作到账金额。

## 数据与配置

数据直接保存在 Compose 文件旁的三个目录，不再使用命名卷：

- `./runtime/wb2api/auths` → `/app/auths`：账号凭据；
- `./runtime/wb2api/data` → `/app/data`：账号池状态和任务历史；
- `./runtime/wb2api/keys` → `/run/wb2a`：管理、API、桥接密钥，console 只读挂载。

普通重启和重建会复用这些目录，首次启动自动创建。不要删除或用空目录替代已有数据。
`runtime/` 已被 Git 忽略，也不会进入镜像构建上下文；备份和搬迁时需要单独保存整个目录。
仅管理员密钥和 API Key 支持 `.env` 配置；留空或不配置时由应用自动生成并持久保存：

```dotenv
WB2A_ADMIN_KEY=
WB2A_API_KEY=
```

自动生成的基础密钥保存在 `runtime/wb2api/keys/keys.json`，不会生成 `.env`。
环境变量只覆盖当前生效值，不改写持久化基础密钥；移除覆盖后恢复使用基础密钥。
备份时请同时保存已有的 `.env` 和 `runtime/`。
当前镜像 `1789526563` 已支持在启动日志中显示手动设置的管理密钥；旧标签 `1789523951`
仅在未手动设置管理密钥时打印，旧部署需同步新版 Compose 后重新启动。

镜像完整标签、端口、时区、目录和 origin 均在 Compose 中写固定值，不读取旧的
`WB2A_VERSION`、`WB2A_PORT` 或存储路径环境变量。需要调整时直接编辑 Compose。
容器内部必需的 `environment` 字段保留固定值。公网部署应由 HTTPS 反向代理转发到
console，并在 Compose 中将 `WB2A_PUBLIC_ORIGIN` 设为浏览器实际
访问的完整 origin（不能带路径）。管理密钥至少 32 个字符；公共 API Key 可沿用原非空
值。两个服务必须使用同一组 `WB2A_ADMIN_KEY`/`WB2A_API_KEY` 覆盖，不能只在 core 的
`config.json` 中设置一个不同 API Key。

成品镜像内置默认空配置，无需宿主配置文件。源码构建版固定只读挂载
`./deploy/default-config.json`，不读取 `WB2A_CONFIG_FILE`。

需要自定义配置时，将 JSON 保存到 `./runtime/wb2api/config.json`，额外取得
`deploy/compose.config.yml` 并显式启用只读挂载（路径相对第一个 Compose 文件）：

```sh
docker compose -f docker-compose.yml -f deploy/compose.config.yml up -d
```

默认部署仍只需一个 Compose 文件；额外文件仅用于自定义配置。原 JSON 只读挂载到
`/app/config.json`，不会写进镜像或改写；缺失路径、目录或非法 JSON 拒绝启动。后续操作
同一自定义配置部署须保留相同 `-f` 参数。API Key 覆盖仍必须由两个服务共享。

新镜像和 Compose 均不定义 Docker 健康检查。console 在 core 启动后启动，并等待密钥文件。
`/livez`、`/healthz` 接口仍可手动诊断：空账号时分别返回 200、503，不会自动定时访问。
当前 Compose 已固定到通过验收的 `1789523951` 镜像，包含权限初始化脚本且无内置健康检查。
旧标签 `1789520619` 不支持此启动方式，不能直接配合删除 init 的 Compose 用于全新部署。

## 旧部署迁移

先只读解析旧容器实际使用的挂载，不能根据项目名猜卷名：

```sh
python3 deploy/migrate.py inspect --container OLD_CONTAINER > /tmp/wb2a-migration.json
python3 deploy/migrate.py backup \
  --manifest /tmp/wb2a-migration.json \
  --output /ABSOLUTE/NEW/BACKUP/DIRECTORY
```

运行中备份会明确标为非最终一致。正式切换前应确认维护窗口、停止已核实的旧实例（不删
卷），再生成并校验最终一致性备份。**旧命名卷不会自动迁移到新目录**，原 `.env` 中的
卷名也不再生效。应按备份清单另行迁移 auths/data/keys 到 `runtime/wb2api` 的对应目录，
保留文件内容和 UID 10001 的读写权限；或者先在 Compose 中明确保留旧挂载，不能直接
启动空目录替代原数据。宿主端口直接改 Compose；有旧自定义配置时按上一节显式挂载。
若旧 `api_key` 与持久密钥不同，必须显式设置共享
`WB2A_API_KEY`；不要删掉配置来绕过冲突。挂载、备份或密钥发生冲突时必须停止，
不能生成空数据继续。

旧 `console-keys.json` 保持原字节不变；新布局会复用原管理/API Key 并新增桥接密钥。
网页会话和进行中的 OAuth 流程在重建后失效，已保存账号不会因此丢失。

## 验证与源码维护

完整本地检查（不发布镜像）：

```sh
python3 -m unittest discover -s scripts -p 'test_*.py' -v
python3 -m unittest discover -s deploy -p 'test_*.py' -v
bash scripts/check.sh
docker compose config --quiet
docker compose -f docker-compose.build.yaml build
bash scripts/acceptance.sh
git diff --check
```

`scripts/check.sh` 在新的物化目录运行 Go 测试、vet、竞态测试以及 console、Node、Python
测试。`scripts/acceptance.sh` 先用仅有 Compose 的临时目录验证镜像启动，再检查隔离 mock
项目和有标签的临时卷，不读取本机已登录
账号，也不执行真实 OAuth、模型消费或领奖。

如果当前 Docker 主机只有显式代理才能构建，应由操作者在命令中传入，不要写成仓库
默认值。例如本机 Docker Desktop 可使用：

```sh
docker compose -f docker-compose.build.yaml build \
  --build-arg HTTP_PROXY=http://host.docker.internal:7890 \
  --build-arg HTTPS_PROXY=http://host.docker.internal:7890
WB2A_BUILD_HTTP_PROXY=http://host.docker.internal:7890 \
WB2A_BUILD_HTTPS_PROXY=http://host.docker.internal:7890 \
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

先审阅候选 diff 和提交，再由操作者明确从源码部署，或按下一节发布一个新镜像版本：

```sh
docker compose config --quiet
docker compose -f docker-compose.build.yaml up -d --build
```

更新失败时当前快照、锁文件和运行实例保持不变，诊断候选会保留。成功更新的旧
`upstream/` 与锁文件保存在忽略的 `.upstream-update-backup/`，但正式回退仍应通过 Git
生成可审阅提交；如果扩展或补丁也变更，必须回退同一组合：

```sh
git revert --no-commit UPSTREAM_UPDATE_COMMIT
git diff
git commit -m "revert: restore previous upstream combination"
docker compose -f docker-compose.build.yaml up -d --build
```

回退继续复用现有 auths/data/keys 目录，不删除已有数据。若相关源码有未提交改动，更新
入口会拒绝覆盖；先提交或另行保存，不要强制清理。

## 手动发布成品镜像

只在发布电脑执行，需要 Bash、Docker Buildx、Python 3、Go、Node、curl。
先登录阿里云镜像仓库，确保账号有 `cateyes/go` 的推送权限；不要把密码或令牌写进项目：

```sh
docker login registry.cn-hangzhou.aliyuncs.com
bash scripts/release.sh
```

Bash 入口复用 `scripts/release.py`，每次运行自动生成 Unix 秒级时间戳，两个镜像共用。
也可显式传入一个未使用的时间戳：`bash scripts/release.sh 1789519503`。
脚本要求构建相关文件已提交，执行回归检查、交叉编译、架构检查和隔离验收后才推送。
两个镜像都推送成功、回拉并核对镜像 ID 后，才原子更新 `docker-compose.yml` 中两个完整
镜像标签里的时间戳。请审阅并提交该文件，
再把它复制到服务器。脚本不会部署、推送 Git 或修改上游，也不会修改服务器密钥。

发布构建仍需拉取 Dockerfile 中的 `golang`/`alpine` 基础镜像；更换成品仓库不会改变
基础镜像来源。构建会重新解析基础镜像并在镜像内验证 x86_64，防止 ARM 缓存混入。
需要本机 Docker 构建代理时：

```sh
WB2A_BUILD_HTTP_PROXY=http://host.docker.internal:7890 \
WB2A_BUILD_HTTPS_PROXY=http://host.docker.internal:7890 bash scripts/release.sh
```

这两个变量只传入 Docker 构建步骤，不覆盖主机的 `HTTP_PROXY`/`HTTPS_PROXY`，避免影响
仓库查询与推送。Docker 后台拉取基础镜像所用代理仍由 Docker 自身配置管理。

脚本在开始和推送前均通过 Docker 检查标签是否已存在；鉴权或网络失败不会被当作标签
不存在。请串行发布，不要让多个发布者共用同一时间戳；检查与推送不是原子操作，严格的
并发防覆盖还需要仓库端提供不可变标签策略。脚本不修改仓库权限或标签策略。
不维护浮动 `latest`。后续每次重新运行脚本生成新时间戳，部署时执行：

```sh
docker compose pull
docker compose up -d
```

两个镜像推送不是原子操作：任一步失败时不切换服务器，不自动删标签；查明原因后使用
新时间戳重新发布。只有两个镜像都能回拉且匹配本次验收镜像才更新 Compose；该检查使用
发布者的 Docker 登录状态，不代表匿名可拉取。免登录部署需将仓库设为公开。
回退时把 Compose 中两个完整镜像标签改回同一已发布旧时间戳再执行上述命令，继续复用原目录；
自定义配置仍需原 `-f` 参数。
镜像只包含程序、默认配置和许可，不包含本机账号、密钥或数据卷。

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
