variable "region" { default = "eu-central-1" }
variable "project" { default = "ayopa" }
variable "github_repo" { description = "owner/repo" }
variable "allow_destroy" { default = true } # set false once real data exists
variable "budget_email" { description = "aws.budgets@ayopa.co.ke" }
variable "budget_usd" { default = 20 }
variable "github_oidc_subject_prefix" {
  description = "Repo portion of the GitHub OIDC sub claim, as printed by the debug step"
  default     = "repo:KING-CYBERTON@82710182/ayopa@1402852750"
}

