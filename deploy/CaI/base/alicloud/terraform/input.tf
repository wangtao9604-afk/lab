variable "vpc_cidr" {
  type        = string
  description = "The VPC cidr block"
  default     = "10.66.0.0/16"
}

variable "vpc_pod_secondary_cidr" {
  type        = string
  description = "Secondary CIDR block attached to VPC for pod network"
  default     = "10.70.0.0/16"
}

variable "vsw_public_cidr" {
  type        = string
  description = "public subnet"
  default     = "10.66.1.0/24"
}

variable "vsw_first_private_cidr" {
  type        = string
  description = "private subnet a"
  default     = "10.66.2.0/24"
}

variable "vsw_second_private_cidr" {
  type        = string
  description = "private subnet b"
  default     = "10.66.3.0/24"
}

variable "vsw_alb_public_cidr" {
  type        = string
  description = "private subnet b"
  default     = "10.66.4.0/24"
}

variable "vsw_first_pod_cidr" {
  type        = string
  description = "pod subnet a"
  default     = "10.70.10.0/24"
}

variable "vsw_second_pod_cidr" {
  type        = string
  description = "pod subnet b"
  default     = "10.70.11.0/24"
}

variable "use_multiaz" {
  type        = bool
  description = "Whether to provision secondary private/pod subnets in a second availability zone"
  default     = false
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
