# 企微智能房产推荐机器人系统架构图

> 用于客户Presentation的详细系统架构说明文档
> 生成日期：2025-08-21
> 目标规模：100+ QPS，100人同时在线

## 1. 核心设计理念

### 🎯 四大核心理念

#### 1️⃣ **极简入口设计**
- **单Chatbot实例**：去除不必要的负载均衡，100 QPS规模单实例足够
- **快速响应**：接收即返回，不阻塞企微回调
- **轻量化**：仅负责解密、验证、转发，业务逻辑完全解耦

#### 2️⃣ **完全异步架构**
- **消息队列解耦**：Kafka作为核心中枢，实现接收与处理完全分离
- **无阻塞设计**：支持长时间AI处理，用户无需等待
- **弹性伸缩**：Consumer可独立扩展，不影响接收层

#### 3️⃣ **智能处理链**
- **单主机3进程并发**：同一台8C16G主机运行3个Consumer进程，充分利用多核
- **API串行依赖**：Keywords→高德地图/房源查询→MiniMax AI，数据逐层增强
- **AI输入优化**：汇聚关键词+房源数据，生成精准推荐

#### 4️⃣ **主动推送模式**
- **Reply Service独立**：专门负责消息推送，与处理逻辑解耦
- **重试机制**：失败自动重试，保证消息可达
- **异步通知**：处理完成后主动推送，用户体验更好

### 🏗️ 架构设计原则

| 原则 | 说明 | 实现方式 |
|------|------|----------|
| **简单性** | 避免过度设计 | 单Chatbot、去除Nginx |
| **解耦性** | 模块职责清晰 | Kafka消息队列隔离 |
| **可靠性** | 保证消息不丢失 | 3副本、幂等性、重试 |
| **扩展性** | 支持未来增长 | Consumer可独立扩展 |
| **成本效益** | 优化资源使用 | 精简配置，月成本6900元 |

### 🔒 分布式锁使用说明

**重要**：分布式锁仅用于保护Session更新操作，不包括API调用和AI处理
- **需要锁**：Session读取和更新（毫秒级操作）
- **不需要锁**：Keywords提取、地图查询、房源查询、AI生成（这些都是无状态操作）
- **锁粒度**：用户级别，确保同一用户的Session不会并发更新
- **锁时间**：实际持有时间仅几毫秒，超时设置30秒仅为防止死锁

## 2. 系统全景架构图

```mermaid
graph TB
    subgraph "企业微信生态"
        WX[企业微信服务器]
        USER[企业用户<br/>100+在线]
    end
    
    subgraph "消息接收层"
        CB[Chatbot服务<br/>4C8G<br/>单实例]
        
        CB_FUNC[核心功能：<br/>• 消息解密验证<br/>• 首次会话判断<br/>• 异步转发Kafka]
    end
    
    subgraph "消息队列层 - 完全解耦"
        KAFKA[Kafka集群<br/>3 Broker]
        
        TOPIC1[qywx-messages<br/>6分区<br/>主消息队列]
        TOPIC2[qywx-reply<br/>6分区<br/>回复队列]
        TOPIC3[qywx-dlq<br/>3分区<br/>死信队列]
    end
    
    subgraph "智能处理层 - 核心业务"
        subgraph "MQ主机 [8C16G]"
            MQ1[Consumer进程1]
            MQ2[Consumer进程2]
            MQ3[Consumer进程3]
        end
        
        MQ_FUNC[🎯 核心处理流程：<br/>1. 幂等性检查<br/>2. 会话管理<br/>3. Keywords提取<br/>4. 外部API调用<br/>5. AI智能生成<br/>6. 发送到Reply]
    end
    
    subgraph "异步推送层"
        RS1[Reply Service-1<br/>2C4G]
        RS2[Reply Service-2<br/>2C4G]
        
        RS_FUNC[功能：<br/>• 消费Reply Topic<br/>• 调用企微API<br/>• 重试机制]
    end
    
    subgraph "数据存储层"
        REDIS[(Redis单实例<br/>4C8G<br/>缓存+分布式锁)]
        PG[(PostgreSQL<br/>1主1备<br/>8C16G<br/>业务数据)]
    end
    
    subgraph "AI服务层 - 智能化核心"
        KW[Keywords模块<br/>关键词提取]
        GD[高德地图API<br/>地理定位]
        PROP[房源专家系统<br/>房源查询]
        AI[🤖 MiniMax AI<br/>智能生成]
    end
    
    subgraph "监控运维"
        PROM[Prometheus<br/>指标采集]
        GRAF[Grafana<br/>可视化]
        ALERT[告警系统]
    end
    
    %% 用户交互流程
    USER -->|发送消息| WX
    WX -->|POST推送| CB
    
    %% Chatbot处理 - 核心理念1：快速响应
    CB -->|立即返回| WX
    CB -->|异步发送| TOPIC1
    
    %% MQ Consumer处理 - 核心理念2：并行处理
    TOPIC1 -->|并发消费| MQ1
    TOPIC1 -->|并发消费| MQ2
    TOPIC1 -->|并发消费| MQ3
    
    %% 外部API调用 - 核心理念3：智能处理链
    MQ1 -->|1.提取关键词| KW
    KW -->|关键词| GD
    KW -->|关键词| PROP
    GD -->|地理位置| PROP
    PROP -->|房源数据| AI
    KW -->|关键词| AI
    MQ1 ==>|最终处理| AI
    
    %% 数据存储
    MQ1 --> REDIS
    MQ1 --> PG
    CB --> REDIS
    
    %% Reply流程 - 核心理念4：异步推送
    MQ1 -->|发送回复| TOPIC2
    TOPIC2 -->|消费| RS1
    TOPIC2 -->|消费| RS2
    RS1 -->|主动推送| WX
    RS2 -->|主动推送| WX
    
    %% 死信队列
    MQ1 -->|失败消息| TOPIC3
    
    %% 监控
    CB -.->|指标| PROM
    MQ1 -.->|指标| PROM
    RS1 -.->|指标| PROM
    PROM --> GRAF
    PROM --> ALERT
    
    %% 用户接收
    WX -->|推送结果| USER
    
    %% 样式
    classDef userStyle fill:#e1f5fe,stroke:#01579b,stroke-width:3px
    classDef appStyle fill:#f3e5f5,stroke:#4a148c,stroke-width:2px
    classDef dataStyle fill:#fff3e0,stroke:#e65100,stroke-width:2px
    classDef extStyle fill:#e8f5e9,stroke:#1b5e20,stroke-width:2px
    classDef monitorStyle fill:#fce4ec,stroke:#880e4f,stroke-width:2px
    classDef coreStyle fill:#ffebee,stroke:#c62828,stroke-width:3px
    
    class USER,WX userStyle
    class CB,MQ1,MQ2,MQ3,RS1,RS2 appStyle
    class REDIS,PG,KAFKA,TOPIC1,TOPIC2,TOPIC3 dataStyle
    class KW,GD,PROP extStyle
    class AI coreStyle
    class PROM,GRAF,ALERT monitorStyle
```

## 3. 消息处理时序图

```mermaid
sequenceDiagram
    participant U as 用户
    participant WX as 企微服务器
    participant CB as Chatbot
    participant K as Kafka
    participant MQ as MQ Consumer
    participant EXT as 外部API组
    participant AI as MiniMax AI
    participant RS as Reply Service
    participant R as Redis
    participant PG as PostgreSQL
    
    Note over U,PG: 完整消息处理流程（异步处理）
    
    U->>WX: 发送消息
    WX->>CB: POST加密消息
    CB->>CB: 解密验证（<100ms）
    CB->>R: 检查首次会话
    R-->>CB: 会话状态
    
    alt 首次会话
        CB->>WX: 返回欢迎语
        WX->>U: 显示欢迎语
    else 非首次会话
        CB->>WX: 返回空响应
    end
    
    CB->>K: 发送到消息队列
    K->>MQ: 消费消息
    
    MQ->>R: 幂等性检查
    R-->>MQ: 检查结果
    
    alt 重复消息
        MQ-->>K: ACK（跳过处理）
    else 新消息
        rect rgb(255, 255, 240)
            Note over MQ,R: Session更新（需要锁保护）
            MQ->>R: 获取分布式锁（仅保护Session）
            R-->>MQ: 锁获取成功
            MQ->>R: 读取Session数据
            MQ->>R: 更新Session（首次标记等）
            MQ->>R: 释放分布式锁
        end
        
        rect rgb(240, 248, 255)
            Note over MQ,EXT: API串行调用（无锁）
            MQ->>EXT: 1. Keywords提取
            EXT-->>MQ: 关键词结果
            
            MQ->>EXT: 2. 高德地图查询（基于关键词）
            EXT-->>MQ: 地理位置
            
            MQ->>EXT: 3. 房源查询（基于关键词+位置）
            EXT-->>MQ: 房源列表
        end
        
        rect rgb(255, 240, 240)
            Note over MQ,AI: AI处理（无锁）
            MQ->>AI: 发送综合数据生成智能回复
            AI-->>MQ: AI生成结果
        end
        
        MQ->>PG: 保存消息记录
        MQ->>R: 更新处理结果缓存
        
        MQ->>K: 发送到Reply Topic
        K->>RS: 消费回复消息
        RS->>WX: 调用主动推送API
        WX->>U: 推送AI回复
    end
    
    Note over U,PG: 异步架构，用户无需等待
```

## 4. 数据流向图

```mermaid
graph TB
    subgraph "输入层"
        D1[用户消息<br/>• 文本内容<br/>• 用户ID<br/>• 群组ID<br/>• 时间戳]
    end
    
    subgraph "预处理层"
        D2[消息解密<br/>AES-256-CBC]
        D3[格式转换<br/>企微→统一格式]
    end
    
    subgraph "智能处理链"
        D4[Keywords模块<br/>关键词提取<br/>• 地点<br/>• 价格<br/>• 户型]
        D5[高德地图API<br/>地理编码<br/>• 经纬度<br/>• 范围计算]
        D6[房源专家系统<br/>房源查询<br/>• 匹配条件<br/>• 排序筛选]
        D7[MiniMax AI<br/>智能生成<br/>• 综合分析<br/>• 推荐报告]
    end
    
    subgraph "数据存储"
        D10[Redis单实例<br/>• 会话缓存<br/>• 分布式锁]
        D11[PostgreSQL<br/>• 消息记录<br/>• 业务数据]
        D12[Kafka<br/>• 消息队列<br/>• 异步处理]
    end
    
    subgraph "输出层"
        D13[推送消息<br/>• 房源推荐<br/>• 分析报告<br/>• 联系方式]
    end
    
    %% 主流程
    D1 --> D2
    D2 --> D3
    D3 --> D4
    
    %% 智能处理链
    D4 -->|关键词| D5
    D4 -->|关键词| D6
    D5 -->|地理位置| D6
    D4 -->|关键词| D7
    D6 -->|房源数据| D7
    
    %% 数据存储
    D3 --> D10
    D3 --> D11
    D3 --> D12
    D7 --> D11
    D7 --> D13
    
    %% 样式
    style D1 fill:#e3f2fd
    style D4 fill:#fff3e0
    style D5 fill:#f3e5f5
    style D6 fill:#fce4ec
    style D7 fill:#ffebee,stroke:#c62828,stroke-width:3px
    style D13 fill:#e8f5e9
```

## 5. 部署架构图

```mermaid
graph TB
    subgraph "阿里云/腾讯云"
        subgraph "网络层"
            SLB[云负载均衡器<br/>高可用]
            SEC[Web应用防火墙<br/>安全防护]
        end
        
        subgraph "计算资源"
            subgraph "ECS集群"
                APP1[应用服务器1<br/>8C16G]
                APP2[应用服务器2<br/>8C16G]
            end
        end
        
        subgraph "容器编排"
            DOCKER[Docker Swarm<br/>或 K8s]
            
            subgraph "服务实例"
                CB[Chatbot×1]
                MQ[Consumer×5]
                RS[Reply×2]
            end
        end
        
        subgraph "数据服务"
            subgraph "Kafka集群"
                K1[Broker-1]
                K2[Broker-2]
                K3[Broker-3]
            end
            
            subgraph "Redis服务"
                R1[单实例<br/>4C8G]
            end
            
            subgraph "PostgreSQL"
                PG1[主库]
                PG2[备库]
            end
        end
        
        subgraph "监控服务"
            MON[Prometheus<br/>+Grafana]
            LOG[日志服务<br/>ELK/云日志]
            TRACE[链路追踪<br/>Jaeger]
        end
    end
    
    SLB --> APP1
    SLB --> APP2
    
    APP1 --> DOCKER
    APP2 --> DOCKER
    
    DOCKER --> CB
    DOCKER --> MQ
    DOCKER --> RS
    
    CB --> K1
    MQ --> K1
    RS --> K1
    
    MQ --> R1
    MQ --> PG1
    
    K1 -.-> K2
    K2 -.-> K3
    PG1 -.-> PG2
    
    CB --> MON
    MQ --> MON
    RS --> MON
    
    style SLB fill:#bbdefb
    style DOCKER fill:#c8e6c9
    style MON fill:#ffccbc
```

## 6. 系统性能指标

```mermaid
graph LR
    subgraph "性能指标"
        P1[系统QPS<br/>目标：100+<br/>峰值：150]
        P2[处理能力<br/>并发用户：100<br/>日消息量：5000+]
        P3[消息吞吐<br/>Kafka 6分区<br/>3 Consumer进程]
    end
    
    subgraph "可用性指标"
        A1[系统SLA<br/>99.9%]
        A2[故障恢复<br/>RTO：5分钟<br/>RPO：1分钟]
        A3[数据可靠性<br/>99.99%]
    end
    
    subgraph "资源配置"
        R1[Chatbot<br/>1实例×4C8G]
        R2[Consumer<br/>1主机×8C16G<br/>3进程并发]
        R3[Reply<br/>2实例×2C4G]
        R4[Kafka<br/>3节点×4C8G]
        R5[Redis<br/>单实例×4C8G]
        R6[PostgreSQL<br/>2节点×8C16G]
    end
    
    P1 --> R1
    P2 --> R2
    P3 --> R3
    
    A1 --> R4
    A2 --> R5
    A3 --> R6
```

## 7. 关键技术决策

```mermaid
graph TB
    subgraph "架构层决策"
        A1[消息架构]
        A1 --> A11[完全异步模式]
        A11 --> A111[解耦接收与处理]
        A11 --> A112[支持长时间处理]
        A11 --> A113[提升系统稳定性]
        
        A2[消息中间件]
        A2 --> A21[Apache Kafka]
        A21 --> A211[高吞吐量]
        A21 --> A212[持久化存储]
        A21 --> A213[3副本保障]
        
        A3[分区策略]
        A3 --> A31[6分区设计]
        A31 --> A311[主队列6分区]
        A31 --> A312[回复队列6分区]
        A31 --> A313[DLQ 3分区]
    end
    
    subgraph "数据层决策"
        B1[数据库选型]
        B1 --> B11[PostgreSQL]
        B11 --> B111[JSONB扩展]
        B11 --> B112[强事务支持]
        B11 --> B113[主备高可用]
        
        B2[缓存方案]
        B2 --> B21[Redis单实例]
        B21 --> B211[会话缓存]
        B21 --> B212[分布式锁]
        B21 --> B213[幂等性记录]
        
        B3[数据策略]
        B3 --> B31[TTL管理]
        B31 --> B311[Session 30分钟]
        B31 --> B312[幂等性24小时]
        B31 --> B313[缓存1小时]
    end
    
    subgraph "并发控制决策"
        C1[分布式锁]
        C1 --> C11[Redsync/Redlock]
        C11 --> C111[仅保护Session]
        C11 --> C112[毫秒级锁定]
        C11 --> C113[30秒超时防死锁]
        
        C2[幂等性保证]
        C2 --> C21[企微MsgID]
        C21 --> C211[全局唯一]
        C21 --> C212[Redis去重]
        C21 --> C213[24小时窗口]
        
        C3[并发策略]
        C3 --> C31[3进程并发]
        C31 --> C311[单机部署]
        C31 --> C312[进程隔离]
        C31 --> C313[负载均衡]
    end
    
    subgraph "容错机制决策"
        D1[降级策略]
        D1 --> D11[多级降级]
        D11 --> D111[AI服务降级]
        D11 --> D112[API缓存降级]
        D11 --> D113[预设回复兜底]
        
        D2[重试机制]
        D2 --> D21[指数退避]
        D21 --> D211[最多3次]
        D21 --> D212[递增延迟]
        D21 --> D213[熔断保护]
        
        D3[错误处理]
        D3 --> D31[死信队列]
        D31 --> D311[隔离问题消息]
        D31 --> D312[人工干预]
        D31 --> D313[监控告警]
    end
    
    subgraph "技术栈决策"
        E1[开发语言]
        E1 --> E11[Golang 1.19+]
        
        E2[核心框架]
        E2 --> E21[Gin Web框架]
        E2 --> E22[Confluent Kafka]
        E2 --> E23[go-redis/v9]
        
        E3[关键库]
        E3 --> E31[pgx/v5 PostgreSQL]
        E3 --> E32[redsync/v4 分布式锁]
        E3 --> E33[zap 日志框架]
    end
    
    %% 样式定义
    classDef decisionStyle fill:#e3f2fd,stroke:#1976d2,stroke-width:2px,color:#000
    classDef choiceStyle fill:#fff3e0,stroke:#f57c00,stroke-width:2px,color:#000
    classDef detailStyle fill:#f3e5f5,stroke:#7b1fa2,stroke-width:1px,color:#000
    
    class A1,A2,A3,B1,B2,B3,C1,C2,C3,D1,D2,D3,E1,E2,E3 decisionStyle
    class A11,A21,A31,B11,B21,B31,C11,C21,C31,D11,D21,D31,E11,E21,E22,E23,E31,E32,E33 choiceStyle
    class A111,A112,A113,A211,A212,A213,A311,A312,A313,B111,B112,B113,B211,B212,B213,B311,B312,B313,C111,C112,C113,C211,C212,C213,C311,C312,C313,D111,D112,D113,D211,D212,D213,D311,D312,D313 detailStyle
```

## 8. 安全架构

```mermaid
graph TB
    subgraph "安全边界"
        subgraph "外部防护"
            WAF[Web应用防火墙]
            DDOS[DDoS防护]
            SSL[SSL/TLS加密]
        end
        
        subgraph "认证授权"
            AUTH1[企微签名验证]
            AUTH2[消息加密AES-256]
            AUTH3[Token管理]
        end
        
        subgraph "数据安全"
            SEC1[敏感数据加密]
            SEC2[数据脱敏]
            SEC3[访问控制]
        end
        
        subgraph "审计监控"
            AUD1[操作日志]
            AUD2[访问审计]
            AUD3[异常检测]
        end
    end
    
    WAF --> AUTH1
    AUTH1 --> SEC1
    SEC1 --> AUD1
```

## 9. 成本效益分析

| 项目 | 配置 | 月成本(元) | 说明 |
|------|------|------------|------|
| **计算资源** | | | |
| Chatbot服务器 | 1台×4C8G | 500 | 消息接收 |
| Consumer服务器 | 1台×8C16G | 1,000 | 3进程并行处理 |
| Reply服务器 | 1台×4C8G | 500 | 消息推送 |
| **存储资源** | | | |
| 云数据库PostgreSQL | 8C16G高可用版 | 1,200 | 业务数据 |
| 云数据库Redis | 4C8G单实例 | 400 | 缓存服务 |
| Kafka消息队列 | 3节点集群 | 1,500 | 消息中间件 |
| **网络资源** | | | |
| 公网带宽 | 50Mbps | 500 | 数据传输 |
| **外部服务** | | | |
| MiniMax AI | 按调用量 | 2,000 | AI生成 |
| 高德地图API | 按调用量 | 300 | 地理服务 |
| **总计** | | **6,900** | 预估月成本 |

## 10. 项目实施路线图

```mermaid
gantt
    title 三周实施计划
    dateFormat  YYYY-MM-DD
    section 第一周：基础搭建
    环境准备           :a1, 2025-01-01, 2d
    Chatbot模块开发    :a2, after a1, 2d
    MQ Consumer基础    :a3, after a2, 1d
    
    section 第二周：核心功能
    业务逻辑实现       :b1, 2025-01-06, 2d
    Reply Service开发  :b2, after b1, 2d
    错误处理机制       :b3, after b2, 1d
    
    section 第三周：完善部署
    监控运维搭建       :c1, 2025-01-13, 2d
    测试优化          :c2, after c1, 2d
    上线准备          :c3, after c2, 1d
```

## 11. 演示要点总结

### 核心亮点
1. **完全异步架构**：支持长时间AI处理，用户体验流畅
2. **智能房产推荐**：集成多个外部API，提供精准推荐
3. **高可用设计**：多实例部署，99.9% SLA保障
4. **成本优化**：极简部署，月成本仅6900元

### 技术优势
1. **成熟技术栈**：Golang + Kafka + Redis + PostgreSQL
2. **容错机制完善**：降级、重试、熔断三重保障
3. **监控体系完整**：Prometheus + Grafana实时监控
4. **易于扩展**：可平滑升级到更大规模

### 业务价值
1. **用户体验提升**：智能对话，精准推荐
2. **运营效率提高**：自动化处理，减少人工
3. **数据驱动决策**：完整的数据采集和分析
4. **快速落地**：3周即可完成开发部署

---

> 本文档用于客户Presentation，包含完整的系统架构设计和实施方案