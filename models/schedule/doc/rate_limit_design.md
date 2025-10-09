# 限流/熔断设计方案 V2（简化版）

## 一、设计目标
防止用户恶意发送大量垃圾消息，保护系统稳定性，采用最简单有效的限流机制。

## 二、核心设计原则
1. **极简设计**：只保留核心功能，去除一切不必要的复杂性
2. **二级处罚**：限流警告 → 临时熔断 → 生命周期内屏蔽
3. **静默处理**：熔断期间不回复任何消息，减少系统开销

## 三、简化后的处罚机制

### 第一级：限流警告
- **触发条件**：N秒内发送超过X条消息
- **处理方式**：回复提示消息，然后进入熔断状态
- **提示文案**："您短期内发送的消息过多，请稍后重试"

### 第二级：临时熔断
- **触发条件**：触发限流警告后自动进入
- **处罚时长**：Y分钟
- **处理方式**：静默丢弃所有消息，不处理、不回复
- **恢复机制**：Y分钟后自动恢复服务

### 第三级：生命周期内屏蔽
- **触发条件**：恢复服务后再次触发限流
- **处罚时长**：processor生命周期内（默认2小时）
- **处理方式**：静默丢弃所有消息，不处理、不回复
- **恢复机制**：等待processor自然过期重建

## 四、数据结构设计

```go
type UserRateLimitState struct {
    UserID           string      // 用户标识
    MessageCount     int         // 当前窗口消息数
    WindowStart      time.Time   // 窗口开始时间
    BlockUntil       time.Time   // 熔断截止时间
    IsLifetimeBlock  bool        // 是否生命周期内屏蔽
    ViolationCount   int         // 违规次数（0=正常, 1=首次违规, 2+=生命周期内屏蔽）
    LastAccess       time.Time   // 最后访问时间（用于内存回收）
}
```

## 五、实现逻辑

### 5.1 核心流程
```
收到消息
    ↓
检查是否生命周期内屏蔽 → 是 → 静默丢弃
    ↓ 否
检查是否在熔断期 → 是 → 静默丢弃
    ↓ 否
检查窗口消息数 → 超限 → 发送警告 → 设置熔断 → 记录违规
    ↓ 未超限
正常处理消息
```

### 5.2 违规处理逻辑
- **首次违规**（ViolationCount=0→1）：
  1. 发送警告消息
  2. 设置熔断时间（BlockUntil = now + Y分钟）
  3. ViolationCount++

- **再次违规**（ViolationCount≥1）：
  1. 不发送任何消息
  2. 设置生命周期内屏蔽标记（IsLifetimeBlock = true）
  3. ViolationCount++

### 5.3 并发安全与内存保护
```go
type RateLimiter struct {
    states map[string]*UserRateLimitState
    mu     sync.RWMutex  // 保护map的并发读写
    config *RateLimitConfig
    lru    *LRUCache     // LRU缓存，用于限制最大条目数
}

// LRU缓存简单实现
type LRUCache struct {
    maxEntries int
    ll         *list.List  // 双向链表
    cache      map[string]*list.Element
}
```

### 5.4 内存管理策略

#### 5.4.1 最大容量保护
- 设置最大状态条目数（默认10000，可配置）
- 当达到上限时，使用LRU策略淘汰最久未使用的用户状态
- 优先淘汰正常用户，保留被屏蔽的用户（防止恶意用户通过填满缓存来解除屏蔽）

#### 5.4.2 定期清理
- 启动后台goroutine，每小时扫描一次
- 清理超过24小时未访问的用户状态
- 生命周期内屏蔽的用户状态保留到processor生命周期结束

#### 5.4.3 LRU淘汰规则
```go
// 淘汰优先级（从高到低）：
// 1. 24小时未访问的正常用户
// 2. 最久未访问的正常用户  
// 3. 最久未访问的熔断用户
// 4. 生命周期内屏蔽用户（最后淘汰）
```

## 六、配置参数

```toml
[rateLimit]
enabled = true              # 是否启用限流
windowSeconds = 60          # 时间窗口N（秒）
maxMessages = 10            # 窗口内最大消息数X
blockMinutes = 5            # 熔断时长Y（分钟）
maxStateEntries = 10000     # 最大状态条目数（防止内存耗尽）
```

## 七、实现示例

### 7.1 核心限流检查
```go
func (r *RateLimiter) Check(userID string) (allowed bool, message string) {
    r.mu.Lock()
    defer r.mu.Unlock()
    
    state := r.getOrCreateState(userID)
    state.LastAccess = time.Now()
    
    // 更新LRU访问顺序
    r.lru.Touch(userID)
    
    // 1. 检查生命周期内屏蔽
    if state.IsLifetimeBlock {
        return false, ""  // 静默丢弃
    }
    
    // 2. 检查熔断期
    if time.Now().Before(state.BlockUntil) {
        return false, ""  // 静默丢弃
    }
    
    // 3. 检查窗口消息数
    if r.isNewWindow(state) {
        state.MessageCount = 0
        state.WindowStart = time.Now()
    }
    
    if state.MessageCount >= r.config.MaxMessages {
        // 触发限流
        state.ViolationCount++
        
        if state.ViolationCount == 1 {
            // 首次违规：警告+熔断
            state.BlockUntil = time.Now().Add(time.Duration(r.config.BlockMinutes) * time.Minute)
            return false, "您短期内发送的消息过多，请稍后重试"
        } else {
            // 再次违规：生命周期内屏蔽
            state.IsLifetimeBlock = true
            return false, ""  // 静默丢弃
        }
    }
    
    state.MessageCount++
    return true, ""
}

// 获取或创建用户状态（带LRU保护）
func (r *RateLimiter) getOrCreateState(userID string) *UserRateLimitState {
    if state, exists := r.states[userID]; exists {
        return state
    }
    
    // 检查是否达到最大容量
    if len(r.states) >= r.config.MaxStateEntries {
        // 执行LRU淘汰
        evictedID := r.findEvictionCandidate()
        if evictedID != "" {
            delete(r.states, evictedID)
            r.lru.Remove(evictedID)
            log.Debug("Evicted user state due to capacity limit: ", evictedID)
        }
    }
    
    // 创建新状态
    state := &UserRateLimitState{
        UserID:      userID,
        WindowStart: time.Now(),
        LastAccess:  time.Now(),
    }
    r.states[userID] = state
    r.lru.Add(userID)
    
    return state
}

// 查找淘汰候选者
func (r *RateLimiter) findEvictionCandidate() string {
    now := time.Now()
    oldThreshold := now.Add(-24 * time.Hour)
    
    // 按优先级查找淘汰对象
    // 1. 先找24小时未访问的正常用户
    for userID, state := range r.states {
        if !state.IsLifetimeBlock && 
           state.BlockUntil.Before(now) && 
           state.LastAccess.Before(oldThreshold) {
            return userID
        }
    }
    
    // 2. 找最久未访问的正常用户（使用LRU）
    for elem := r.lru.ll.Back(); elem != nil; elem = elem.Prev() {
        userID := elem.Value.(string)
        state := r.states[userID]
        if !state.IsLifetimeBlock && state.BlockUntil.Before(now) {
            return userID
        }
    }
    
    // 3. 找最久未访问的熔断用户
    for elem := r.lru.ll.Back(); elem != nil; elem = elem.Prev() {
        userID := elem.Value.(string)
        state := r.states[userID]
        if !state.IsLifetimeBlock && !state.BlockUntil.Before(now) {
            return userID
        }
    }
    
    // 4. 最后才淘汰生命周期内屏蔽用户（尽量保留）
    if elem := r.lru.ll.Back(); elem != nil {
        return elem.Value.(string)
    }
    
    return ""
}
```

## 八、在Processor中集成

```go
func (p *Processor) processMessage(msg *kefu.KFRecvMessage) error {
    // 限流检查
    allowed, replyMsg := p.rateLimiter.Check(msg.ExternalUserID)
    if !allowed {
        if replyMsg != "" {
            // 发送警告消息
            return p.sendReply(msg, replyMsg)
        }
        // 静默丢弃
        return nil
    }
    
    // 正常处理消息
    return p.handleMessage(msg)
}
```

## 九、监控指标

1. **限流触发次数**：统计每个用户的违规次数
2. **当前熔断用户数**：处于临时熔断状态的用户数
3. **生命周期内屏蔽用户数**：被生命周期内屏蔽的用户数
4. **丢弃消息数**：被静默丢弃的消息总数

## 十、测试场景

### 场景1：正常用户
- 发送消息频率正常，不触发限流
- 验证：消息正常处理

### 场景2：首次超限
- 60秒内发送11条消息
- 验证：第11条收到警告，后续5分钟内消息被丢弃

### 场景3：二次违规
- 首次超限恢复后，再次超限
- 验证：生命周期内屏蔽，processor生命周期内不再服务

### 场景4：Processor重建
- 用户被生命周期内屏蔽后，等待processor过期重建
- 验证：新processor中用户状态重置，可以重新服务

## 十一、优势分析

1. **实现简单**：逻辑清晰，代码量少，易于维护
2. **资源节省**：熔断期静默处理，不占用处理资源
3. **防护有效**：二次违规生命周期内屏蔽，有效阻止恶意用户
4. **自动恢复**：依靠processor生命周期自然重置，无需额外机制

## 十二、注意事项

1. **参数调优**：根据实际业务场景调整N、X、Y的值
2. **日志记录**：记录所有限流事件，便于事后分析
3. **告警机制**：生命周期内屏蔽用户数异常增长时及时告警
4. **用户教育**：在产品界面适当提示消息发送频率限制
5. **容量监控**：监控states map的大小，接近maxStateEntries时告警
6. **淘汰日志**：记录LRU淘汰事件，分析是否需要调整容量上限

## 十三、Redis扩展方案（预留设计）

### 13.1 分库设计原则
当未来需要使用Redis实现分布式限流时，必须遵循以下分库原则：

#### Redis数据库分配规范
| DB编号 | 用途 | 存储内容 | 负责模块 |
|--------|------|----------|----------|
| DB 0 | 默认库 | 避免使用 | - |
| DB 1 | 认证凭据 | access_token, suite_token, corp_token, user_id, session | tokenstore |
| DB 2 | 限流熔断 | 用户限流状态, 违规记录, 屏蔽列表 | ratelimit |
| DB 3-15 | 预留 | 未来扩展使用 | - |

#### 分库原则
1. **数据隔离**：不同类型数据必须使用不同DB
2. **安全边界**：认证数据(DB1)与业务数据(DB2+)严格分离
3. **故障隔离**：某个DB出问题不影响其他模块
4. **清理便利**：可以直接FLUSHDB清理特定类型数据

### 13.2 Redis实现接口（预留）
```go
// RedisRateLimiter 分布式限流器（未来实现）
type RedisRateLimiter struct {
    client *redis.Client
    config *RateLimitConfig
}

// 初始化时指定DB 2
func NewRedisRateLimiter(cfg *config.QywxConfig) *RedisRateLimiter {
    // 从配置的redises map中获取限流专用的Redis配置
    redisCfg, exists := cfg.Redises["ratelimit"]
    if !exists {
        // 如果没有专门配置，使用默认Redis但指定DB 2
        redisCfg = cfg.Redises["default"]
        redisCfg.DB = 2  // 强制使用DB 2
    }
    
    client := redis.NewClient(&redis.Options{
        Addr:     redisCfg.Addr,
        Password: redisCfg.Password,
        DB:       2,  // 始终使用DB 2存储限流数据
    })
    // ...
}

// Key命名规范
const (
    keyPrefix = "ratelimit:"
    userStateKey = "ratelimit:user:%s"        // 用户状态
    violationKey = "ratelimit:violation:%s"   // 违规记录
    blockListKey = "ratelimit:blocklist"      // 屏蔽列表
)
```

### 13.3 数据隔离好处
1. **职责分离**：认证数据与限流数据完全隔离
2. **安全性提升**：即使限流模块出问题，不影响认证系统
3. **运维便利**：可独立备份、监控、清理不同类型数据
4. **扩展性好**：未来可将不同DB部署到不同Redis实例

### 13.4 切换时机建议
保持当前内存实现，仅在以下情况切换到Redis：
- 需要多实例部署，要求限流状态全局共享
- 需要限流状态持久化，重启后保留
- 用户规模超过单机内存承载能力

## 十四、内存保护机制总结

### 14.1 防御策略
- **硬性上限**：通过maxStateEntries限制最大用户状态数
- **智能淘汰**：LRU策略确保活跃用户不受影响
- **优先级保护**：生命周期内屏蔽用户最后淘汰，防止恶意用户利用容量限制解除屏蔽

### 14.2 配置建议
- **小型服务**：maxStateEntries = 1000-5000
- **中型服务**：maxStateEntries = 10000（默认）
- **大型服务**：maxStateEntries = 50000-100000
- 根据实际内存资源和用户规模调整

### 14.3 极端情况处理
当遭遇大规模UserID伪造攻击时：
1. LRU机制会自动淘汰旧的、不活跃的攻击者状态
2. 真实的活跃用户和已屏蔽的恶意用户会被优先保留
3. 系统内存得到保护，不会被耗尽
4. 通过监控发现异常后，可临时调低maxStateEntries应急