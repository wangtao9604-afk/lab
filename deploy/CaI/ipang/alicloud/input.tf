variable "region" {
  type = string
}

variable "first_available_zone" {
  type        = string
  description = "first avaliable zone"
}

variable "second_available_zone" {
  type        = string
  description = "second avaliable zone"
}

variable "business_name" {
  type        = string
  description = "business name"
}

variable "k8s_version" {
  type    = string
  default = "1.34.1-aliyun.1"
}

# Node pools instance types
variable "core_instance_type" {
  type    = string
  default = "ecs.g5ne.large"
}

variable "consumers_instance_type" {
  type    = string
  default = "ecs.g5ne.xlarge"
}

variable "system_instance_type" {
  type    = string
  default = "ecs.g5ne.large"
}

variable "rds_password" {
  type      = string
  sensitive = true
}
