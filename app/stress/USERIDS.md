# 压测 UserID 管理

## 概述

为了确保压测的**可重复性**和**可靠性**，系统使用预生成的固定 UserID 集合，而不是每次运行时动态生成。

## 设计原理

### 为什么使用预生成的 UserID？

1. **可重复性**：每次压测使用完全相同的 UserID 集合，确保结果可对比
2. **一致性**：避免动态生成可能产生的随机性差异
3. **可验证性**：可以预先验证 UserID 的唯一性和分区分布
4. **调试友好**：出现问题时可以精确重现特定 UserID 的行为

### UserID 分布策略

- **总数**：1000 个唯一 UserID
- **Kafka 分区**：16 个分区
- **分布**：
  - 分区 0-7：每个 63 个用户
  - 分区 8-15：每个 62 个用户
  - 偏差：仅 1 个用户（最优分布）

### 生成算法

使用 **SplitMix64** 高质量伪随机算法配合 **MurmurHash2** 分区映射：

1. 对每个目标分区，使用 SplitMix64 生成候选后缀
2. 使用 MurmurHash2 计算候选 UserID 的分区映射
3. 如果映射到目标分区，则采用；否则继续尝试
4. 平均 16 次尝试即可找到匹配的 UserID

## 文件说明

### `userids.json`
预生成的 UserID 数据文件，包含：
```json
{
  "userids": ["wm6Gh8CQAAspvwHsWoLwkecb0Rk3n27X", ...],
  "partition_mapping": {"wm6Gh8CQAAspvwHsWoLwkecb0Rk3n27X": 0, ...},
  "partition_distribution": {"0": 63, "1": 63, ...}
}
```

- `userids`: 1000 个 UserID 数组
- `partition_mapping`: UserID → 分区号映射（用于验证）
- `partition_distribution`: 每个分区的 UserID 数量统计

### `generate_userids.go`
UserID 生成工具，仅在需要重新生成 UserID 时使用。

## 使用流程

### 首次使用（已生成）

`userids.json` 已经预生成并提交到代码仓库，可以直接使用：

```bash
# 验证 UserID 文件存在
ls -lh app/stress/userids.json

# 运行压测（会自动加载 userids.json）
./app/stress/run_stress.sh 10 1
```

### 重新生成 UserID（可选）

**⚠️ 注意**：重新生成会改变所有 UserID，影响测试的可重复性。仅在以下情况下执行：
- 需要更改分区数量
- 需要更改 UserID 总数
- 发现现有 UserID 有问题

```bash
# 1. 运行生成工具
go run app/stress/generate_userids.go

# 输出示例：
# ===========================================
#   Generate Stress Test UserIDs
# ===========================================
#
# Generating partition  0: 63 users... ✓
# Generating partition  1: 63 users... ✓
# ...
# ✓ Generated 1000 unique UserIDs
# ✓ UserIDs saved to: app/stress/userids.json

# 2. 验证生成结果
go run test/verify_unique_userids.go
go run test/verify_userid_distribution.go

# 3. 重新编译服务
make build_dynamic

# 4. 提交新的 userids.json（如果需要）
git add app/stress/userids.json
git commit -m "Regenerate stress test UserIDs"
```

## 验证工具

### 唯一性验证
```bash
go run test/verify_unique_userids.go
```
验证：
- 1000 个 UserID 完全唯一
- 每个 UserID 在多轮测试中收到正确数量的消息

### 分区分布验证
```bash
go run test/verify_userid_distribution.go
```
验证：
- 每个分区的 UserID 数量符合预期（62 或 63）
- 使用真实的 MurmurHash2 算法计算分区映射

## 技术细节

### UserID 格式
```
wm6Gh8CQAAsXXXXXXXXXXXXXXXXXXXXX
└─────┬─────┘└────────┬────────┘
    前缀          后缀（21字符）
  (固定)    (SplitMix64 生成)
```

- 前缀：`wm6Gh8CQAAs`（11 字符，固定）
- 后缀：21 字符，字符集 `[A-Za-z0-9]`（62 个字符）
- 总长度：32 字符

### 分区映射算法
使用与 Kafka 完全一致的 MurmurHash2 算法：
```go
hashValue := murmur2([]byte(userID))
partition := int(int32(hashValue)) % numPartitions
if partition < 0 {
    partition = -partition
}
```

### 加载机制
服务启动时，`infrastructures/stress/userids.go` 会：
1. 查找项目根目录（包含 `go.mod`）
2. 读取 `app/stress/userids.json`
3. 解析 JSON 并验证数量（必须是 1000 个）
4. 加载到内存（使用 `sync.Once` 确保只加载一次）

如果文件不存在或格式错误，服务会 panic 并提示：
```
Failed to load UserIDs: read userids file .../app/stress/userids.json: no such file or directory

Please generate userids.json first:
  cd /path/to/qywx
  go run app/stress/generate_userids.go
```

## 故障排查

### 问题 1：找不到 userids.json

**错误信息**：
```
Failed to load UserIDs: read userids file .../app/stress/userids.json: no such file or directory
```

**解决方法**：
```bash
# 生成 userids.json
go run app/stress/generate_userids.go

# 或者从 Git 仓库拉取
git checkout app/stress/userids.json
```

### 问题 2：UserID 数量不正确

**错误信息**：
```
Failed to load UserIDs: invalid userids count: got 500, expected 1000
```

**解决方法**：
```bash
# 重新生成
go run app/stress/generate_userids.go
```

### 问题 3：JSON 格式错误

**错误信息**：
```
Failed to load UserIDs: parse userids JSON: invalid character ...
```

**解决方法**：
```bash
# 验证 JSON 格式
jq . app/stress/userids.json

# 如果损坏，重新生成
go run app/stress/generate_userids.go
```

## 最佳实践

### ✅ 推荐做法

1. **使用版本控制的 userids.json**
   - 将 `userids.json` 提交到 Git 仓库
   - 团队成员使用相同的 UserID 集合
   - 便于跨环境比较测试结果

2. **压测前验证 UserID**
   ```bash
   go run test/verify_unique_userids.go
   go run test/verify_userid_distribution.go
   ```

3. **记录 UserID 版本**
   - 如果重新生成，在 commit message 中说明原因
   - 保留旧版本的测试结果供对比

### ❌ 避免做法

1. **不要手动编辑 userids.json**
   - 可能破坏分区分布
   - 可能引入重复 UserID

2. **不要频繁重新生成**
   - 每次重新生成会改变所有 UserID
   - 影响测试结果的可比性

3. **不要在生产环境生成**
   - 生成工具应该在开发环境运行
   - 生成后提交到代码仓库

## 性能指标

### 生成性能
- **1000 个 UserID**：< 1 秒
- **平均尝试次数**：~16 次/UserID
- **内存占用**：< 1 MB

### 加载性能
- **读取 JSON**：< 10 ms
- **解析和验证**：< 5 ms
- **总启动开销**：< 15 ms

## 扩展性

如需调整 UserID 数量或分区数：

1. 修改 `generate_userids.go` 中的常量：
   ```go
   const (
       userIDCount    = 2000  // 改为 2000
       partitionCount = 32    // 改为 32
   )
   ```

2. 修改 `infrastructures/stress/userids.go` 中的常量：
   ```go
   const (
       userIDCount = 2000  // 保持一致
   )
   ```

3. 重新生成并验证：
   ```bash
   go run app/stress/generate_userids.go
   go run test/verify_unique_userids.go
   go run test/verify_userid_distribution.go
   ```

## 参考资料

- 主 README: `/Users/peter/projs/qywx/app/stress/README.md`
- 生成工具: `/Users/peter/projs/qywx/app/stress/generate_userids.go`
- 加载实现: `/Users/peter/projs/qywx/infrastructures/stress/userids.go`
- 验证工具:
  - `/Users/peter/projs/qywx/test/verify_unique_userids.go`
  - `/Users/peter/projs/qywx/test/verify_userid_distribution.go`
