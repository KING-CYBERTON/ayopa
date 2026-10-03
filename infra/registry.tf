locals { repos = ["app", "caddy"] }

resource "aws_ecr_repository" "app" {
  name         = "${var.project}-app"
  force_delete = var.allow_destroy
  image_scanning_configuration { scan_on_push = true }
}

resource "aws_ecr_repository" "caddy" {
  name         = "${var.project}-caddy"
  force_delete = var.allow_destroy
  image_scanning_configuration { scan_on_push = true }
}

resource "aws_ecr_lifecycle_policy" "keep_last_10" {
  for_each   = { app = aws_ecr_repository.app.name, caddy = aws_ecr_repository.caddy.name }
  repository = each.value
  policy = jsonencode({
    rules = [{
      rulePriority = 1
      description  = "keep last 10 images"
      selection    = { tagStatus = "any", countType = "imageCountMoreThan", countNumber = 10 }
      action       = { type = "expire" }
    }]
  })
}