variable "cluster_name" {
  type = string
}

variable "k8s_version" {
  type = string
}

variable "vpc_id" {
  type = string
}

variable "worker_vswitch_ids" {
  type = list(string)
}

variable "pod_vswitch_ids" {
  type = list(string)
}

variable "core_instance_type" {
  type = string
}

variable "consumers_instance_type" {
  type = string
}


variable "system_instance_type" {
  type = string
}

variable "node_pay_type" {
  type    = string
  default = "PostPaid"
}

variable "node_period" {
  type    = number
  default = 12
}

variable "node_period_unit" {
  type    = string
  default = "Month"
}

variable "node_auto_renew" {
  type    = bool
  default = false
}

variable "create_new_nat_gateway" {
  type    = bool
  default = true
}

variable "alb_ingress_controller_version" {
  type    = string
  default = "v2.18.0-aliyun.1"
}

variable "first_az" {
  type = string
}


variable "second_az" {
  type = string
}

variable "public_vswitch_id_in_first_az" {
  type = string
}

variable "public_vswitch_id_in_second_az" {
  type = string
}
