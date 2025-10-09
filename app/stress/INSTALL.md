# 压测系统安装指南

## 部署 UserID 文件

压测系统需要在 `/etc/qywx/userids.json` 放置 UserID 数据文件。

### 安装步骤

```bash
# 1. 确保目标目录存在
sudo mkdir -p /etc/qywx

# 2. 复制 userids.json 到系统目录
sudo cp app/stress/userids.json /etc/qywx/userids.json

# 3. 验证文件已正确安装
ls -lh /etc/qywx/userids.json

# 4. 验证文件内容完整性
jq '.userids | length' /etc/qywx/userids.json
# 应该输出: 1000
```

### 验证安装

确认文件权限正确（服务可读）：

```bash
# 检查权限
ls -l /etc/qywx/userids.json

# 如果需要，调整权限
sudo chmod 644 /etc/qywx/userids.json
```

### 更新 UserID 文件

如果需要重新生成 UserID：

```bash
# 1. 生成新的 userids.json
go run app/stress/generate_userids.go

# 2. 复制到系统目录
sudo cp app/stress/userids.json /etc/qywx/userids.json

# 3. 重启服务
sudo systemctl restart qywx-producer
sudo systemctl restart qywx-consumer
```

## 生产环境部署

### Docker 部署

在 Dockerfile 中添加：

```dockerfile
# 复制 userids.json 到容器
COPY app/stress/userids.json /etc/qywx/userids.json
```

### Kubernetes 部署

使用 ConfigMap 挂载：

```yaml
apiVersion: v1
kind: ConfigMap
metadata:
  name: qywx-stress-userids
data:
  userids.json: |
    # 这里粘贴 userids.json 的完整内容
---
apiVersion: v1
kind: Pod
metadata:
  name: qywx-producer
spec:
  containers:
  - name: producer
    image: qywx-producer:latest
    volumeMounts:
    - name: userids
      mountPath: /etc/qywx/userids.json
      subPath: userids.json
  volumes:
  - name: userids
    configMap:
      name: qywx-stress-userids
```

或使用 Secret（如果需要保密）：

```bash
# 创建 Secret
kubectl create secret generic qywx-stress-userids \
  --from-file=userids.json=app/stress/userids.json

# 在 Pod 中挂载
kubectl apply -f - <<EOF
apiVersion: v1
kind: Pod
metadata:
  name: qywx-producer
spec:
  containers:
  - name: producer
    image: qywx-producer:latest
    volumeMounts:
    - name: userids
      mountPath: /etc/qywx/userids.json
      subPath: userids.json
  volumes:
  - name: userids
    secret:
      secretName: qywx-stress-userids
EOF
```

### Ansible 部署

```yaml
- name: Deploy qywx stress userids
  copy:
    src: app/stress/userids.json
    dest: /etc/qywx/userids.json
    owner: qywx
    group: qywx
    mode: '0644'
```

## 故障排查

### 问题：服务启动失败，提示找不到 userids.json

**错误信息**：
```
Failed to load UserIDs: read userids file /etc/qywx/userids.json: no such file or directory

Please ensure /etc/qywx/userids.json exists and contains 1000 valid UserIDs.
To generate: go run app/stress/generate_userids.go
```

**解决方法**：
```bash
# 复制文件到正确位置
sudo cp app/stress/userids.json /etc/qywx/userids.json
```

### 问题：权限被拒绝

**错误信息**：
```
Failed to load UserIDs: read userids file /etc/qywx/userids.json: permission denied
```

**解决方法**：
```bash
# 调整文件权限
sudo chmod 644 /etc/qywx/userids.json

# 或者调整所有者（如果服务以特定用户运行）
sudo chown qywx:qywx /etc/qywx/userids.json
```

### 问题：UserID 数量不正确

**错误信息**：
```
Failed to load UserIDs: invalid userids count: got 500, expected 1000
```

**解决方法**：
```bash
# 重新生成完整的 userids.json
go run app/stress/generate_userids.go

# 复制到系统目录
sudo cp app/stress/userids.json /etc/qywx/userids.json
```

## 安全注意事项

1. **文件权限**
   - 建议使用 `644` 权限（所有者可读写，其他人只读）
   - 不需要可执行权限

2. **所有者**
   - 如果服务以非 root 用户运行，确保该用户有读取权限
   - 推荐：`chown qywx:qywx /etc/qywx/userids.json`

3. **备份**
   - 在更新前备份现有文件
   - 保留旧版本以便回滚

```bash
# 备份
sudo cp /etc/qywx/userids.json /etc/qywx/userids.json.backup.$(date +%Y%m%d)

# 回滚
sudo cp /etc/qywx/userids.json.backup.20251009 /etc/qywx/userids.json
```

## 验证安装成功

运行以下命令确认一切正常：

```bash
# 1. 文件存在且可读
test -r /etc/qywx/userids.json && echo "✓ File exists and readable"

# 2. JSON 格式正确
jq empty /etc/qywx/userids.json && echo "✓ JSON format valid"

# 3. UserID 数量正确
[ $(jq '.userids | length' /etc/qywx/userids.json) -eq 1000 ] && echo "✓ 1000 UserIDs present"

# 4. 无重复 UserID
[ $(jq -r '.userids[]' /etc/qywx/userids.json | sort -u | wc -l) -eq 1000 ] && echo "✓ All UserIDs unique"

# 5. 测试服务启动（如果已部署）
./bin/producer &
PID=$!
sleep 2
kill $PID
echo "✓ Service can load UserIDs successfully"
```

如果所有检查都通过，说明 UserID 文件已正确安装！
