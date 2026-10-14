# Operations runbook

## Edit the landing page copy

Each mall has a landing page at its root URL (`https://<mall domain>/`). The
page shows the mall name, an optional tagline, optional body copy, and an
optional contact email. All fields are plain text; HTML is never interpreted.

### Command

```
server set-mall-content \
  -mall <mall domain> \
  [-tagline "Short headline"] \
  [-description "Longer body copy."] \
  [-contact operator@example.com]
```

Only the flags you pass are updated; omitted flags keep their current values.
To clear a field, pass an empty string: `-tagline ""`.

### Character limits

| Field | Limit |
|---|---|
| tagline | 140 characters (rune count) |
| description | 600 characters (rune count) |
| contact email | 254 characters; must be a valid RFC 5322 address, or empty to clear |

### Examples

Set all three fields:

```
server set-mall-content \
  -mall ayopa.co.ke \
  -tagline "Kenya's online mall" \
  -description "Browse shops across fashion, electronics, and groceries." \
  -contact hello@ayopa.co.ke
```

Update only the tagline, leaving description and contact unchanged:

```
server set-mall-content -mall ayopa.co.ke -tagline "New season, new deals"
```

Clear the contact email so the footer link disappears:

```
server set-mall-content -mall ayopa.co.ke -contact ""
```

### Where the command runs

On the production host via SSM:

```
aws ssm start-session --target $(terraform -chdir=infra output -raw instance_id)
sudo docker compose -f /opt/ayopa/compose.yml run --rm app \
  server set-mall-content -mall ayopa.co.ke -tagline "..."
```

The `DATABASE_URL` environment variable is read from the container environment;
you do not need to supply it.

### Verification

Visit `https://<mall domain>/` in a browser. Changes take effect immediately;
there is no cache to purge (responses are served with `Cache-Control: no-store`).
