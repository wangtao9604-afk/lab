variable "vpc_id" {
  type = string
}

variable "vswitch_ids" {
  type = list(string)
  validation {
    condition     = length(var.vswitch_ids) > 0
    error_message = "Provide at least one vswitch ID for RDS."
  }
}

variable "zone_id" {
  type = string
}

variable "zone_id_slave_a" {
  type    = string
  default = null
}

variable "zone_id_slave_b" {
  type    = string
  default = null
}

variable "enable_multi_az" {
  type    = bool
  default = false
}

variable "rds_engine_version" {
  type    = string
  default = "8.0"
}

variable "rds_instance_type" {
  type    = string
  default = "rds.mysql.s2.large"
}

variable "rds_storage_gb" {
  type    = number
  default = 600
}

variable "rds_account" {
  type = string
}


variable "rds_password" {
  type      = string
  sensitive = true
}

variable "security_cidrs" {
  type = list(string)
}

variable "db_name" {
  type = string
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
