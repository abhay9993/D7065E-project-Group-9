# Config API

`GET /api/config` returns the public runtime configuration used by both
viewers.

```bash
curl http://127.0.0.1:9090/api/config
```

```json
{
  "edit_mode": false,
  "edit_persistent": false,
  "ui": "modern",
  "host": "127.0.0.1"
}
```

| Field | Type | Meaning |
|---|---|---|
| `edit_mode` | boolean | Editor routes and controls are enabled |
| `edit_persistent` | boolean | Saves are exported under `--edit-output` |
| `ui` | string | Selected root viewer: `modern` or `classic` |
| `host` | string | Configured listen host |

The default host is loopback-only. To let another machine on a trusted LAN
connect, start BuildSim with `--all-interfaces`; the reported host will then be
`0.0.0.0`. This flag is equivalent to `--host 0.0.0.0` and cannot be combined
with `--host`.
