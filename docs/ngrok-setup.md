# Ngrok内网穿透配置指南

## 1. 安装Ngrok

### macOS
```bash
brew install ngrok/ngrok/ngrok
```

### 其他系统
访问 https://ngrok.com/download 下载对应版本

## 2. 注册并获取AuthToken

1. 访问 https://ngrok.com 注册账号
2. 登录后访问 https://dashboard.ngrok.com/get-started/your-authtoken
3. 复制您的authtoken

## 3. 配置Ngrok

方法一：使用命令行配置
```bash
ngrok config add-authtoken YOUR_AUTH_TOKEN_HERE
```

方法二：编辑配置文件
编辑 `.ngrok/ngrok.yml`，将 `YOUR_NGROK_AUTH_TOKEN` 替换为您的实际token

## 4. 启动服务

### 启动Go服务
```bash
go run main.go
```
服务将在本地11112端口启动

### 启动Ngrok隧道

直接使用配置文件：
```bash
ngrok start --config ./.ngrok/ngrok.yml qywx
```

或使用一键启动脚本：
```bash
chmod +x dev.sh
./dev.sh  # 同时启动Go服务和ngrok
```

## 5. 获取公网地址

启动ngrok后，会显示类似信息：
```
Forwarding  https://xxx-xxx-xxx.ngrok.io -> http://localhost:11112
```

复制 `https://xxx-xxx-xxx.ngrok.io` 这个地址

## 6. 配置企业微信

### 配置回调URL

1. 登录企业微信管理后台
2. 进入应用管理 -> 选择您的应用
3. 在"接收消息"设置中：
   - URL: `https://xxx-xxx-xxx.ngrok.io/message`
   - Token: `QrMocWqTeMTY` (与config.go中保持一致)
   - EncodingAESKey: `GW6F4FUF2ntV6spTShVxfSJs83grS1d1n66KmeNGlYA`

### 配置可信域名

1. 在应用配置中找到"网页授权及JS-SDK"
2. 添加可信域名：`xxx-xxx-xxx.ngrok.io`
3. 下载验证文件并放入 `resources/` 目录

## 7. 测试连接

### 验证URL
访问：`https://xxx-xxx-xxx.ngrok.io/message?msg_signature=xxx&timestamp=xxx&nonce=xxx&echostr=xxx`

### 发送测试消息
在企业微信客户端向应用发送消息，查看服务端日志

## 8. 调试技巧

### 查看请求详情
访问 http://localhost:4040 查看ngrok的Web界面，可以看到：
- 所有请求的详细信息
- 请求/响应的body内容
- 请求重放功能

### 日志查看
```bash
# 查看Go服务日志
tail -f qywx.log

# 查看ngrok日志
# 直接在终端查看ngrok输出
```

## 9. 常见问题

### Q: ngrok连接不稳定
A: 免费版ngrok会定期更换URL，建议：
- 使用付费版获取固定域名
- 或使用其他内网穿透工具如frp、花生壳等

### Q: 企业微信验证失败
A: 检查：
1. Token和EncodingAESKey是否正确
2. URL路径是否正确（/message）
3. 服务是否正常运行

### Q: 收不到消息
A: 确认：
1. 应用权限是否开启
2. 回调配置是否保存成功
3. 查看ngrok Web界面是否收到请求

## 10. 生产环境部署

⚠️ **注意**：ngrok仅用于开发调试，生产环境应：
- 使用真实域名和SSL证书
- 部署到云服务器或企业服务器
- 配置反向代理（nginx）
- 使用防火墙和安全组规则