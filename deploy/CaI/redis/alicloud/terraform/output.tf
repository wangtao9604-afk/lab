output "connection_string" {
  description = "私网连接串，若实例尚未创建则返回 null。"
  value       = try(data.alicloud_kvstore_connections.redis.connections[0].connection_string, null)
}
