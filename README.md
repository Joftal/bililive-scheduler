# Bililive Scheduler

Bililive-go 的定时录制调度工具。通过 bililive-go 的 REST API 实现定时录制任务的管理，不直接连接任何直播平台。
<img width="1335" height="414" alt="image" src="https://github.com/user-attachments/assets/9f3ca73b-a693-48a8-b177-cbdf2eb70290" />

## 功能

- **定时录制**: 按星期和时间设置录制计划，支持同一天多段录制
- **录制时长控制**: 可设置每个时段的录制时长，0 表示录制直到主播下播
- **监控等待**: 触发时若房间未开播，在设定时间内持续检测等待开播
- **断流重连**: 录制中断流后，在监控窗口内自动等待主播重新开播并恢复录制
- **自动重试**: 任务失败后按指数退避重试（1→2→4→8→15 分钟），可配置最大重试次数
- **房间删除检测**: bililive-go 侧删除房间后，对应的任务自动清理
- **Web UI**: 内嵌可视化管理界面，支持任务创建、编辑、删除、重试
- **热配置**: 运行时调整检查间隔（5-300 秒），无需重启

## 工作原理

```
bililive-go (父进程)
  ├── 录制引擎 (拉流、录制、保存文件)
  ├── REST API (/api/lives/*)
  └── 反向代理 (/scheduler/ → localhost:{随机端口})
         │
         ▼
bililive-scheduler (子进程)
  ├── Cron 引擎 (定时触发任务)
  ├── REST API (任务管理)
  └── Web UI (管理界面)
```

Scheduler 不直接连接直播平台，所有操作通过 bililive-go 的 API 完成：

| Scheduler 操作 | 调用的 bililive-go API |
|---|---|
| 查询房间列表 | `GET /api/lives` |
| 检查房间是否在直播 | `GET /api/lives/{id}` |
| 开始录制 | `GET /api/lives/{id}/start` |
| 停止录制 | `GET /api/lives/{id}/stop` |

## 任务状态

```
pending → waiting → recording → completed → waiting (循环)
              ↓                       ↓
            error ←────── (失败重试) ──┘
```

- **pending**: 新创建，等待首次评估
- **waiting**: 已评估，等待下次触发时间
- **recording**: 正在录制
- **completed**: 录制完成，等待下次调度
- **error**: 执行失败，自动重试或等待手动重试

## 执行流程

每个 engine tick（默认 15 秒）执行四个阶段：

1. **触发到期任务**: 检查到期的 waiting 任务，查询房间状态，开始录制
2. **检查进行中录制**: 监控时长限制、检测流是否结束
3. **重新调度**: 将 completed/error 任务计算下次触发时间
4. **清理历史**: 每日自动清理执行历史，每个任务最多保留 60 条记录

### 监控模式

配置了监控时长（monitor_min > 0）时：

- **触发时未开播**: 在监控窗口内每 tick 检测，开播即录制
- **录制中断流**: 在监控窗口内等待重连，重新开播自动恢复录制
- **窗口过期**: 正常结束，等待下次计划时间

### 自动重试

任务失败后按指数退避重试：

| 重试次数 | 等待时间 |
|---------|---------|
| 第 1 次 | 1 分钟 |
| 第 2 次 | 2 分钟 |
| 第 3 次 | 4 分钟 |
| 第 4 次 | 8 分钟 |
| 第 5+ 次 | 15 分钟（封顶） |

退避时间和下次计划触发时间取较晚的那个。超过最大重试次数后进入错误状态，需手动重试。

## 接入方式

作为 bililive-go 的子进程自动启动，无需手动配置。bililive-go 负责：

1. 下载 scheduler 二进制
2. 启动进程并传入 API 地址和数据库路径
3. 通过端口文件发现 scheduler 监听端口
4. 在 `/scheduler/` 路径下反向代理到 scheduler 的 Web UI

## API 端点

| 方法 | 路径 | 说明 |
|------|------|------|
| GET | /api/tasks | 任务列表（支持 ?state=&enabled= 过滤） |
| POST | /api/tasks | 创建任务 |
| GET | /api/tasks/{id} | 获取任务详情 |
| PUT | /api/tasks/{id} | 更新任务 |
| DELETE | /api/tasks/{id} | 删除任务 |
| POST | /api/tasks/{id}/enable | 启用任务 |
| POST | /api/tasks/{id}/disable | 禁用任务 |
| POST | /api/tasks/{id}/retry | 重试错误任务 |
| GET | /api/tasks/{id}/history | 执行历史 |
| GET | /api/status | 调度器状态 |
| GET | /api/rooms | bililive-go 房间列表 |
| GET | /api/config | 获取配置 |
| PUT | /api/config | 更新配置 |
| GET | /health | 健康检查 |

## CLI 参数

| 参数 | 默认值 | 说明 |
|------|--------|------|
| --port | 0 | HTTP 端口（0 = 随机） |
| --api-url | http://localhost:8080 | bililive-go API 地址 |
| --db-path | 平台默认路径 | SQLite 数据库路径 |
| --api-key | 空 | API 认证密钥（空 = 不启用） |
| --allowed-origins | * | CORS 允许的来源 |
| --rate-limit | 30 | 每 IP 每秒请求数限制 |
| --rate-burst | 60 | 限流突发大小 |
| --version | - | 打印版本号 |

## 构建

```bash
# 前端
cd web && npm install && npm run build

# 后端（当前平台）
go build -o bililive-scheduler ./cmd/scheduler/

# 交叉编译
GOOS=windows GOARCH=amd64 CGO_ENABLED=0 go build -ldflags="-s -w" -o bililive-scheduler.exe ./cmd/scheduler/
```

## 技术栈

- **后端**: Go, gorilla/mux, robfig/cron/v3, modernc.org/sqlite
- **前端**: React 18, TypeScript, Ant Design 5, Vite
- **数据库**: SQLite (WAL 模式)
