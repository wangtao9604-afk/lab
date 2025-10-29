variable "vpc_id" {
  type = string
}

variable "vswitch_ids" {
  type = list(string)

  validation {
    condition     = length(var.vswitch_ids) > 0
    error_message = "At least one vswitch ID must be provided."
  }
}

variable "security_cidrs" {
  type = list(string)
}

variable "fallback_zone_id" {
  type = string

  validation {
    condition     = length(trimspace(var.fallback_zone_id)) > 0
    error_message = "fallback_zone_id 不能为空。请传入与首选 vswitch 对应的可用区 ID。"
  }
}

variable "instance_name" {
  type = string
}

variable "redis_instance_class" {
  type    = string
  default = "redis.master.large.default"
}

variable "redis_engine_version" {
  type    = string
  default = "5.0"
}

variable "redis_password_free" {
  type    = bool
  default = true
}

variable "pay_type" {
  type    = string
  default = "Postpaid"
}

variable "period" {
  type    = number
  default = 12
}

variable "auto_renew" {
  type    = bool
  default = false
}
