# 企微智能机器人系统指标Mermaid图表

## 1. 性能指标仪表盘

```mermaid
graph TB
    subgraph "性能指标监控"
        subgraph "QPS监控"
            QPS[系统QPS<br/>目标: 100+<br/>告警: >150]
        end
        
        subgraph "延迟监控"
            P50[P50延迟<br/>目标: <15秒<br/>告警: >20秒]
            P95[P95延迟<br/>目标: <25秒<br/>告警: >35秒]
            P99[P99延迟<br/>目标: <30秒<br/>告警: >45秒]
        end
        
        subgraph "中间件性能"
            KAFKA[Kafka消费延迟<br/>目标: <1000条<br/>告警: >2000条]
            REDIS[Redis响应<br/>目标: <10ms<br/>告警: >50ms]
            PG[PostgreSQL查询<br/>目标: <100ms<br/>告警: >500ms]
        end
    end
    
    style QPS fill:#90EE90
    style P50 fill:#90EE90
    style P95 fill:#FFD700
    style P99 fill:#FFA500
    style KAFKA fill:#87CEEB
    style REDIS fill:#87CEEB
    style PG fill:#87CEEB
```

## 2. 可用性指标雷达图

```mermaid
graph LR
    subgraph "可用性监控 (目标/告警)"
        A[系统总体<br/>99.9% / <99.5%] --> B[服务层]
        B --> B1[Chatbot服务<br/>99.9% / <99.5%]
        B --> B2[MQ Consumer<br/>99.9% / <99.5%]
        B --> B3[Reply Service<br/>99.9% / <99.5%]
        
        A --> C[中间件层]
        C --> C1[Kafka集群<br/>99.95% / <99.9%]
        C --> C2[Redis<br/>99.95% / <99.9%]
        C --> C3[PostgreSQL<br/>99.95% / <99.9%]
    end
    
    style A fill:#4169E1,color:#fff
    style B fill:#32CD32
    style C fill:#32CD32
    style B1 fill:#90EE90
    style B2 fill:#90EE90
    style B3 fill:#90EE90
    style C1 fill:#87CEEB
    style C2 fill:#87CEEB
    style C3 fill:#87CEEB
```

## 3. 资源使用率监控

```mermaid
graph TD
    subgraph "资源使用监控"
        subgraph "计算资源"
            CPU[CPU使用率<br/>正常: <60%<br/>告警: >80%<br/>━━━━━━━━<br/>当前: 45%]
            MEM[内存使用率<br/>正常: <70%<br/>告警: >85%<br/>━━━━━━━━<br/>当前: 55%]
        end
        
        subgraph "存储资源"
            DISK[磁盘使用率<br/>正常: <70%<br/>告警: >85%<br/>━━━━━━━━<br/>当前: 42%]
            KAFKA_DISK[Kafka磁盘<br/>正常: <100GB<br/>告警: >150GB<br/>━━━━━━━━<br/>当前: 65GB]
        end
        
        subgraph "网络资源"
            NET[网络带宽<br/>正常: <50Mbps<br/>告警: >80Mbps<br/>━━━━━━━━<br/>当前: 25Mbps]
        end
        
        subgraph "连接资源"
            REDIS_MEM[Redis内存<br/>正常: <2GB<br/>告警: >3GB<br/>━━━━━━━━<br/>当前: 1.2GB]
            DB_CONN[数据库连接<br/>正常: <100<br/>告警: >150<br/>━━━━━━━━<br/>当前: 67]
        end
    end
    
    style CPU fill:#90EE90
    style MEM fill:#90EE90
    style DISK fill:#90EE90
    style KAFKA_DISK fill:#FFD700
    style NET fill:#90EE90
    style REDIS_MEM fill:#90EE90
    style DB_CONN fill:#FFD700
```

## 4. 错误与异常指标流程图

```mermaid
graph LR
    subgraph "错误监控体系"
        A[消息入口] --> B{处理结果}
        B -->|成功| C[正常流程<br/>目标: >99%]
        B -->|失败| D[失败处理<br/>目标: <1%<br/>告警: >3%]
        
        D --> E{重试机制}
        E -->|重试| F[重试次数<br/>目标: <100/h<br/>告警: >500/h]
        E -->|DLQ| G[死信队列<br/>目标: <10/h<br/>告警: >50/h]
        
        D --> H{错误类型}
        H -->|超时| I[超时错误<br/>目标: <1%<br/>告警: >3%]
        H -->|API失败| J[API失败<br/>目标: <2%<br/>告警: >5%]
        H -->|熔断| K[熔断触发<br/>目标: <5/h<br/>告警: >20/h]
        H -->|异常| L[系统异常<br/>目标: <10/h<br/>告警: >50/h]
    end
    
    style C fill:#90EE90
    style D fill:#FFA500
    style G fill:#FF6347
    style F fill:#FFD700
    style I fill:#FFD700
    style J fill:#FFD700
    style K fill:#FFA500
    style L fill:#FF6347
```

## 5. 性能指标趋势图

```mermaid
graph LR
    subgraph "24小时性能趋势"
        A[00:00] --> B[06:00]
        B --> C[12:00]
        C --> D[18:00]
        D --> E[24:00]
        
        A -.QPS:50.-> B
        B -.QPS:80.-> C
        C -.QPS:120.-> D
        D -.QPS:90.-> E
        
        A --P99:10s--> B
        B --P99:15s--> C
        C --P99:25s--> D
        D --P99:20s--> E
    end
```

## 6. 系统健康状态总览

```mermaid
pie title 系统健康状态分布
    "健康(Normal)" : 85
    "警告(Warning)" : 10
    "异常(Critical)" : 5
```

## 7. 告警级别分布

```mermaid
graph TD
    subgraph "告警分级体系"
        P0[P0-紧急<br/>系统不可用<br/>立即响应<br/>电话+短信+企微]
        P1[P1-严重<br/>功能异常<br/>15分钟内<br/>短信+企微]
        P2[P2-警告<br/>性能下降<br/>1小时内<br/>企微通知]
        P3[P3-提醒<br/>接近阈值<br/>工作时间<br/>企微通知]
        
        P0 --> CASE1[可用性<99.5%]
        P0 --> CASE2[数据丢失风险]
        
        P1 --> CASE3[错误率>3%]
        P1 --> CASE4[P99>45秒]
        
        P2 --> CASE5[CPU>80%]
        P2 --> CASE6[DLQ>50/h]
        
        P3 --> CASE7[资源使用70%]
        P3 --> CASE8[重试>100/h]
    end
    
    style P0 fill:#FF0000,color:#fff
    style P1 fill:#FFA500,color:#fff
    style P2 fill:#FFD700
    style P3 fill:#90EE90
```

## 8. 监控数据流向图

```mermaid
graph TB
    subgraph "数据采集层"
        APP[应用程序<br/>Prometheus SDK]
        MW[中间件<br/>原生Metrics]
        LOG[日志系统<br/>ELK Stack]
    end
    
    subgraph "存储层"
        PROM[Prometheus<br/>时序数据库]
        ES[ElasticSearch<br/>日志存储]
    end
    
    subgraph "展示层"
        GRAF[Grafana<br/>可视化]
        ALERT[AlertManager<br/>告警管理]
    end
    
    subgraph "通知层"
        WX[企业微信]
        SMS[短信]
        EMAIL[邮件]
    end
    
    APP --> PROM
    MW --> PROM
    LOG --> ES
    
    PROM --> GRAF
    PROM --> ALERT
    ES --> GRAF
    
    ALERT --> WX
    ALERT --> SMS
    ALERT --> EMAIL
    
    style PROM fill:#4169E1,color:#fff
    style GRAF fill:#32CD32,color:#fff
    style ALERT fill:#FFA500,color:#fff
```

## 9. 性能基线对比

```mermaid
graph LR
    subgraph "性能基线对比"
        subgraph "目标值"
            T1[QPS: 100+]
            T2[P50: <15s]
            T3[P95: <25s]
            T4[P99: <30s]
        end
        
        subgraph "当前值"
            C1[QPS: 85]
            C2[P50: 12s]
            C3[P95: 22s]
            C4[P99: 28s]
        end
        
        subgraph "告警值"
            A1[QPS: >150]
            A2[P50: >20s]
            A3[P95: >35s]
            A4[P99: >45s]
        end
        
        T1 -.85%.-> C1
        T2 -.80%.-> C2
        T3 -.88%.-> C3
        T4 -.93%.-> C4
    end
    
    style T1 fill:#90EE90
    style T2 fill:#90EE90
    style T3 fill:#90EE90
    style T4 fill:#90EE90
    style C1 fill:#87CEEB
    style C2 fill:#87CEEB
    style C3 fill:#87CEEB
    style C4 fill:#87CEEB
    style A1 fill:#FFB6C1
    style A2 fill:#FFB6C1
    style A3 fill:#FFB6C1
    style A4 fill:#FFB6C1
```

## 10. 资源使用率仪表盘

```mermaid
graph TB
    subgraph "资源使用率实时监控"
        subgraph "CPU (目标<60%)"
            CPU_BAR[▓▓▓▓▓▓▓▓▓░░░░░░ 45%]
        end
        
        subgraph "内存 (目标<70%)"
            MEM_BAR[▓▓▓▓▓▓▓▓▓▓▓░░░░ 55%]
        end
        
        subgraph "磁盘 (目标<70%)"
            DISK_BAR[▓▓▓▓▓▓▓▓░░░░░░░ 42%]
        end
        
        subgraph "网络 (目标<50Mbps)"
            NET_BAR[▓▓▓▓▓░░░░░░░░░░ 25Mbps]
        end
        
        subgraph "状态"
            STATUS[✅ 所有资源正常]
        end
    end
    
    style CPU_BAR fill:#90EE90
    style MEM_BAR fill:#90EE90
    style DISK_BAR fill:#90EE90
    style NET_BAR fill:#90EE90
    style STATUS fill:#32CD32,color:#fff
```

## 说明

这些Mermaid图表展示了企微智能机器人系统的关键监控指标：

1. **性能指标仪表盘** - 展示QPS、延迟分位数和中间件性能
2. **可用性指标雷达图** - 展示服务和中间件的可用性目标
3. **资源使用率监控** - 实时展示各类资源的使用情况
4. **错误与异常流程图** - 展示错误处理和监控流程
5. **性能趋势图** - 24小时性能变化趋势
6. **健康状态饼图** - 系统整体健康状态分布
7. **告警分级体系** - P0-P3告警级别和触发条件
8. **监控数据流向** - 从采集到通知的完整链路
9. **性能基线对比** - 目标值、当前值、告警值对比
10. **资源使用率仪表盘** - 直观的资源使用率展示

这些图表可以直接嵌入到监控大屏或文档中，提供直观的系统状态可视化。