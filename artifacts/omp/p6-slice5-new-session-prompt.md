背景（已通过实验台账验证，勿重复）
仓库 /Users/yingliangzhang/Projects/ananke-p0a-schema-codegen，分支 feat/task-proposal-core，HEAD 19f01e0（已推送 origin）。
P6 Slice 4 已完结并已提交：P1 verifier provenance + P2 installation authority binding 修复完成，fresh isolated hard review ACCEPT（0 findings），258 vectors 全绿。完整证据见 docs/experiment-ledger.md 2026-07-31 条目。
开始任何工作前，先完整阅读 docs/experiment-ledger.md 中 P6 全部段落（从「P6a controlled repair foundation candidate」到「Slice 4 ACCEPTED」），以及 docs/plans/2026-07-26-p6a-controlled-repair-foundation.md 的 Slice 5 段（约 98–111 行）。这是恢复工作的第一手事实源。

当前状态
已跟踪tracked文件干净；工作区仅剩未跟踪的 OMP artifacts 和 .playwright-cli/（运行产物，永不提交）。
P6 已接受的 slice 进度：Slice 1–2 ACCEPT、Slice 3 ACCEPT (Repair A+B)、Slice 4 ACCEPT (Repair C + Repair P1/P2)。剩余 Slice 5–9 未开始。

本次任务（P6 Contract Slice 5 — Adapter sandbox and UID terminal proof）
按计划文档 Slice 5 冻结规格实现候选合约（仅合约层，不含运行时进程启动/沙箱执行）：

冻结要点（来自 plan 98–111 行）：
1. adapter 仅子进程；无生产 in-process 接口。
2. 专用 attempt UID 或从封闭的 release-provisioned UID 池获取独占租约；spawn 前日志化 UID 租约，不允许并发 attempt 共享同一 UID。
3. Seatbelt profile 身份和精确的 write/read/network/exec 能力集。
4. provider 凭据仅通过独立审查的 broker 通道传递，绝不经过 child argv/raw policy/evidence。
5. 退出/超时处理：reap leader → TERM/KILL 原 PGID → 枚举/kill 每个 leased-UID 进程（含 new sessions）→ 重复直到 UID-empty → 关闭描述符 → 冻结 roots → 持久化 terminal proof → 然后且仅然后继续。
6. terminal proof 绑定 UID 租约、leader、PGID、UID-empty 观测、sandbox hash、root identities、descriptor closure、cleanup result。
7. 强制向量：ignored context、double-fork、setsid、closed stdio、delayed write/ref update、restart with child alive、UID reuse/contention、stale PID/epoch、broker/network escape。

⚠️ 操作前提：Slice 5 要求预配专用 macOS repair runtime users/UID 池。这需要一次手动管理员认证步骤（passwordless sudo 不可用）。在实现前先确认此前提是否已满足；如未满足，实现纯合约层（UID pool 定义、lease grammar、terminal proof schema、seatbelt profile schema）并标注 runtime 前提未满足，不启动任何实际进程。

工程规则
代码/注释/commit 用英文；向我汇报用中文（紧凑 + 表格）。
不 reset/clean 未跟踪产物；不动 .playwright-cli/；不动其他 worktree（~/Projects/ananke 是 GUI 分支）。
遵循已接受 slice 的合约范式：canonical self-hashed record types、closed string enums、mustDerive... panic-on-mismatch init、frozen release-pinned verifier authority、VerifyReleaseTrust 前置、capability intact re-derivation。
不新增 storage migration、process launch、production command；合约接受前不实现运行时效果。
提交前门禁：go build ./... && go vet ./... + 相关 focused 套件（-run '^TestP6Slice5' -count=1, -count=10, race -count=3）+ 全包 ./internal/repaircontract -count=1 + gofmt + diff-check + tagged 编译（ananke_real_provider_canary ananke_test_runtime_authority）。
commit/push 仅限本特性分支，门禁绿 + 独立复审 ACCEPT 后可直接执行。
已知环境问题：全量 internal/trustedsupervisor 的 TestProductionServer* deadline-exceeded 簇在纯净 HEAD 同样复现，属预存环境问题，非回归；如需对照用 git worktree add 纯净检出验证。
报告必须附真实命令输出证据（exit code / 耗时 / 哈希），禁止无证据的质量断言。

完成定义
P6 Slice 5 候选合约实现 → candidate manifest 重生成（两次字节一致）→ fresh isolated hard review ACCEPT → 作为独立原子提交提交并推送 → 更新 docs/experiment-ledger.md 记录全部证据。遇 P3+ 级新发现先报告再决定是否扩大范围。
