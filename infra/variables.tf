variable "region"        { default = "eu-central-1" }
variable "project"       { default = "ayopa" }
variable "github_repo"   { description = "owner/repo" }
variable "allow_destroy" { default = true } # set false once real data exists
variable "budget_email"  { description = "aws.budgets@ayopa.co.ke" }
variable "budget_amount" { default = 20 }